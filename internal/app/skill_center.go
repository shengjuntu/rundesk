package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/skillcatalog"
	"github.com/shengjuntu/rundesk/internal/skillsafety"
	"github.com/shengjuntu/rundesk/internal/store"
)

// The center reuses the existing object store and bundle installer. Packs are
// frozen acquisition snapshots, never a second set of live agent settings.
type SkillPack struct {
	ID        string                        `json:"id"`
	Name      string                        `json:"name"`
	SourceURL string                        `json:"sourceUrl"`
	Revision  string                        `json:"revision,omitempty"`
	Digest    string                        `json:"digest"`
	CreatedAt string                        `json:"createdAt"`
	Files     []SkillFile                   `json:"files"`
	Scan      skillsafety.Report            `json:"scan"`
	Reports   map[string]skillsafety.Report `json:"reports"`
}

type SkillScanJob struct {
	ID         string              `json:"id"`
	PackID     string              `json:"packId"`
	Digest     string              `json:"digest"`
	Engine     string              `json:"engine"`
	Status     string              `json:"status"`
	CreatedAt  string              `json:"createdAt"`
	StartedAt  string              `json:"startedAt,omitempty"`
	FinishedAt string              `json:"finishedAt,omitempty"`
	Report     *skillsafety.Report `json:"report,omitempty"`
	Error      string              `json:"error,omitempty"`
}

type SkillInstallReceipt struct {
	ID           string `json:"id"`
	PackID       string `json:"packId,omitempty"`
	Name         string `json:"name"`
	WorkspaceID  string `json:"workspaceId"`
	InstanceID   string `json:"instanceId"`
	Scope        string `json:"scope"`
	Digest       string `json:"digest"`
	SourceURL    string `json:"sourceUrl,omitempty"`
	Revision     string `json:"revision,omitempty"`
	InstalledAt  string `json:"installedAt"`
	Acknowledged bool   `json:"acknowledged"`
	Reason       string `json:"reason,omitempty"`
}

type skillCenterState struct {
	mu      sync.Mutex
	client  *skillcatalog.Client
	cancels map[string]context.CancelFunc
}

type skillSearchCache struct {
	ID     string                    `json:"id"`
	At     time.Time                 `json:"at"`
	Result skillcatalog.SearchResult `json:"result"`
}

type skillCenterError struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *skillCenterError) Error() string { return e.Message }

// Existing application configuration routes use the common API writer; retain
// the status/code there even when they do not expose the center's full details.
func (e *skillCenterError) Unwrap() error { return failure(e.Status, e.Code, e.Message) }

var skillCenterID = regexp.MustCompile(`^[a-f0-9]{24}$`)

func (m *Manager) skillsCenter() (*skillCenterState, error) {
	m.skillCenterMu.Lock()
	defer m.skillCenterMu.Unlock()
	if m.ctx.Err() != nil {
		return nil, failure(503, "skill_center_stopped", "后台正在停止")
	}
	if m.skillCenter != nil {
		return m.skillCenter, nil
	}
	rows, e := m.Store.List("skill-center-job")
	if e != nil {
		return nil, e
	}
	for _, row := range rows {
		var job SkillScanJob
		if e = json.Unmarshal(row, &job); e != nil {
			return nil, e
		}
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "interrupted"
			job.FinishedAt = store.Now()
			job.Error = "后台已重启，扫描未自动重跑"
			if e = m.Store.Put("skill-center-job", job.ID, job); e != nil {
				return nil, e
			}
		}
	}
	m.skillCenter = &skillCenterState{client: skillcatalog.NewClient(), cancels: map[string]context.CancelFunc{}}
	return m.skillCenter, nil
}

func safetyFiles(files []skillAsset) []skillsafety.File {
	out := make([]skillsafety.File, 0, len(files))
	for _, f := range files {
		out = append(out, skillsafety.File{Path: f.Path, Data: f.Data, Mode: f.Mode})
	}
	return out
}

func (m *Manager) SkillPacks(query string) ([]SkillPack, error) {
	if _, e := m.skillsCenter(); e != nil {
		return nil, e
	}
	rows, e := m.Store.List("skill-center-pack")
	if e != nil {
		return nil, e
	}
	out := []SkillPack{}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, row := range rows {
		var p SkillPack
		if e = json.Unmarshal(row, &p); e != nil {
			return nil, e
		}
		if query == "" || strings.Contains(strings.ToLower(p.Name+" "+p.SourceURL), query) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (m *Manager) SkillPack(id string) (SkillPack, error) {
	var p SkillPack
	if !skillCenterID.MatchString(id) {
		return p, failure(404, "skill_pack_not_found", "技能快照不存在")
	}
	e := m.Store.Get("skill-center-pack", id, &p)
	if errors.Is(e, sql.ErrNoRows) {
		e = failure(404, "skill_pack_not_found", "技能快照不存在")
	}
	return p, e
}

func (m *Manager) StageSkillPack(bundle skillcatalog.Bundle) (SkillPack, error) {
	center, e := m.skillsCenter()
	if e != nil {
		return SkillPack{}, e
	}
	center.mu.Lock()
	defer center.mu.Unlock()
	assets := make([]skillAsset, 0, len(bundle.Assets))
	for _, a := range bundle.Assets {
		assets = append(assets, skillAsset{a.Path, a.Data, a.Mode})
	}
	assets, e = normalizeSkillAssets(assets)
	if e != nil {
		return SkillPack{}, e
	}
	report := skillsafety.Scan(safetyFiles(assets))
	digest, e := skillsafety.Digest(safetyFiles(assets))
	if e != nil {
		return SkillPack{}, e
	}
	rows, e := m.Store.List("skill-center-pack")
	if e != nil {
		return SkillPack{}, e
	}
	for _, row := range rows {
		var p SkillPack
		if e = json.Unmarshal(row, &p); e != nil {
			return p, e
		}
		if p.Digest == digest && p.SourceURL == bundle.SourceURL && p.Revision == bundle.Revision {
			// Re-fetch repairs a damaged local snapshot instead of reusing its
			// stale metadata. A valid frozen pack keeps its prior reports.
			if _, _, err := m.skillPackAssets(p.ID); err == nil {
				return p, nil
			}
		}
	}
	if len(rows) >= 500 {
		return SkillPack{}, failure(409, "skill_catalog_full", "技能库已达 500 个快照，请先整理现有记录")
	}
	id := store.ID()
	relative := "skill-center/packs/" + id
	root, e := os.OpenRoot(m.Data)
	if e != nil {
		return SkillPack{}, e
	}
	defer root.Close()
	if e = root.MkdirAll(relative, 0700); e != nil {
		return SkillPack{}, e
	}
	committed := false
	defer func() {
		if !committed {
			_ = root.RemoveAll(relative)
		}
	}()
	for _, file := range assets {
		name := relative + "/" + file.Path
		if e = root.MkdirAll(filepath.Dir(name), 0700); e != nil {
			return SkillPack{}, e
		}
		mode := fs.FileMode(0600)
		if file.Mode&0111 != 0 {
			mode = 0700
		}
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return SkillPack{}, err
		}
		_, err = f.Write(file.Data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return SkillPack{}, err
		}
		if closeErr != nil {
			return SkillPack{}, closeErr
		}
	}
	name := strings.TrimSpace(bundle.Name)
	if name == "" {
		name = "skill"
	}
	if len([]rune(name)) > 200 {
		name = string([]rune(name)[:200])
	}
	p := SkillPack{ID: id, Name: name, SourceURL: bundle.SourceURL, Revision: bundle.Revision, Digest: digest, CreatedAt: store.Now(), Files: bundleManifest(name, assets).Files, Scan: report, Reports: map[string]skillsafety.Report{"builtin": report}}
	if e = m.Store.Put("skill-center-pack", id, p); e != nil {
		return SkillPack{}, e
	}
	committed = true
	return p, nil
}

func (m *Manager) skillPackAssets(id string) (SkillPack, []skillAsset, error) {
	p, e := m.SkillPack(id)
	if e != nil {
		return p, nil, e
	}
	root, e := os.OpenRoot(m.Data)
	if e != nil {
		return p, nil, e
	}
	defer root.Close()
	relative := "skill-center/packs/" + id
	for _, parent := range []string{"skill-center", "skill-center/packs", relative} {
		info, err := root.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return p, nil, failure(409, "skill_snapshot_changed", "技能快照目录已变化，请重新获取")
		}
	}
	assets := []skillAsset{}
	total := int64(0)
	for _, file := range p.Files {
		if !validSkillPath(file.Path) {
			return p, nil, failure(409, "skill_snapshot_changed", "技能快照包含无效路径")
		}
		parts := strings.Split(file.Path, "/")
		prefix := relative
		for _, part := range parts {
			prefix += "/" + part
			info, err := root.Lstat(prefix)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return p, nil, failure(409, "skill_snapshot_changed", "技能快照文件已变化，请重新获取")
			}
		}
		info, err := root.Lstat(prefix)
		if err != nil || !info.Mode().IsRegular() || info.Size() > skillBundleLimit-total {
			return p, nil, failure(409, "skill_snapshot_changed", "技能快照大小或类型已变化")
		}
		f, err := root.Open(prefix)
		if err != nil {
			return p, nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, skillBundleLimit-total+1))
		_ = f.Close()
		if err != nil {
			return p, nil, err
		}
		total += int64(len(data))
		if total > skillBundleLimit {
			return p, nil, failure(413, "skill_bundle_too_large", "技能快照超限")
		}
		mode := info.Mode().Perm()
		if runtime.GOOS == "windows" {
			// Windows does not persist POSIX execute bits. The frozen manifest
			// retains source intent; Unix hosts additionally verify actual mode.
			mode = 0600
			if file.Executable {
				mode = 0700
			}
		}
		assets = append(assets, skillAsset{file.Path, data, mode})
	}
	digest, e := skillsafety.Digest(safetyFiles(assets))
	if e != nil || digest != p.Digest {
		return p, nil, failure(409, "skill_snapshot_changed", "内容已改变，旧扫描报告不能用于安装；请重新获取")
	}
	return p, assets, nil
}

func (m *Manager) SearchSkills(ctx context.Context, query string, sources []string) (skillcatalog.SearchResult, error) {
	center, e := m.skillsCenter()
	if e != nil {
		return skillcatalog.SearchResult{}, e
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > 200 {
		return skillcatalog.SearchResult{}, failure(400, "invalid_query", "搜索词最多 200 字符")
	}
	allowed := map[string]bool{"github": true, "clawhub": true, "skillsmp": true, "skills.sh": true}
	for _, source := range sources {
		if !allowed[source] {
			return skillcatalog.SearchResult{}, failure(400, "invalid_source", "未知搜索来源")
		}
	}
	sorted := append([]string{}, sources...)
	sort.Strings(sorted)
	hash := sha256.Sum256([]byte(query + "\x00" + strings.Join(sorted, ",")))
	key := hex.EncodeToString(hash[:])
	var cached skillSearchCache
	if e = m.Store.Get("skill-center-search", key, &cached); e == nil && time.Since(cached.At) < 10*time.Minute {
		for index := range cached.Result.Providers {
			cached.Result.Providers[index].Status = "cached"
			cached.Result.Providers[index].Message = "使用 10 分钟内的搜索缓存"
		}
		return cached.Result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	result, e := center.client.Search(ctx, query, sources)
	if e != nil {
		return result, e
	}
	if result.Complete {
		cached.ID = key
		cached.At = time.Now()
		cached.Result = result
		if e = m.Store.Put("skill-center-search", key, cached); e != nil {
			return result, e
		}
		// Bound persistent cache growth; only successful complete queries enter it.
		center.mu.Lock()
		rows, err := m.Store.List("skill-center-search")
		if err == nil && len(rows) > 300 {
			records := []skillSearchCache{}
			for _, row := range rows {
				var v skillSearchCache
				if json.Unmarshal(row, &v) == nil && v.ID != "" {
					records = append(records, v)
				}
			}
			sort.Slice(records, func(i, j int) bool { return records[i].At.Before(records[j].At) })
			for index := 0; index < len(records)-300; index++ {
				_ = m.Store.DeleteObject("skill-center-search", records[index].ID)
			}
		}
		center.mu.Unlock()
	}
	return result, nil
}

func (m *Manager) FetchSkillPack(ctx context.Context, url string) (SkillPack, error) {
	center, e := m.skillsCenter()
	if e != nil {
		return SkillPack{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	bundle, e := center.client.Fetch(ctx, url)
	if e != nil {
		return SkillPack{}, e
	}
	return m.StageSkillPack(bundle)
}

func (m *Manager) SkillScanJob(id string) (SkillScanJob, error) {
	var job SkillScanJob
	if !skillCenterID.MatchString(id) {
		return job, failure(404, "skill_scan_not_found", "扫描任务不存在")
	}
	if _, e := m.skillsCenter(); e != nil {
		return job, e
	}
	e := m.Store.Get("skill-center-job", id, &job)
	if errors.Is(e, sql.ErrNoRows) {
		e = failure(404, "skill_scan_not_found", "扫描任务不存在")
	}
	return job, e
}

func (m *Manager) StartSkillScan(id, engine string, allowLLM bool) (SkillScanJob, error) {
	if engine != "builtin" && engine != "skillspector" {
		return SkillScanJob{}, failure(400, "invalid_scan_engine", "未知扫描引擎")
	}
	center, e := m.skillsCenter()
	if e != nil {
		return SkillScanJob{}, e
	}
	p, _, e := m.skillPackAssets(id)
	if e != nil {
		return SkillScanJob{}, e
	}
	m.skillCenterMu.Lock()
	defer m.skillCenterMu.Unlock()
	if m.ctx.Err() != nil {
		return SkillScanJob{}, failure(503, "skill_center_stopped", "后台正在停止")
	}
	center.mu.Lock()
	defer center.mu.Unlock()
	if len(center.cancels) >= 2 {
		return SkillScanJob{}, failure(429, "skill_scan_busy", "最多同时运行两个扫描任务")
	}
	for active := range center.cancels {
		var old SkillScanJob
		if m.Store.Get("skill-center-job", active, &old) == nil && old.PackID == id {
			return SkillScanJob{}, failure(409, "skill_scan_running", "该技能已有扫描任务正在运行")
		}
	}
	job := SkillScanJob{ID: store.ID(), PackID: id, Digest: p.Digest, Engine: engine, Status: "queued", CreatedAt: store.Now()}
	if e = m.Store.Put("skill-center-job", job.ID, job); e != nil {
		return job, e
	}
	if engine != "builtin" {
		if p.Reports == nil {
			p.Reports = map[string]skillsafety.Report{}
		}
		// A newly requested external check invalidates its previous verdict
		// immediately. Failure, cancellation or restart must not reveal an old
		// successful verdict as if this attempt had completed.
		p.Reports[engine] = skillsafety.Report{Engine: engine, Digest: p.Digest, Status: "incomplete", Recommendation: "incomplete", Findings: []skillsafety.Finding{}, Warnings: []string{"本次扫描尚未完成；不能沿用先前扫描结论"}}
		if e = m.Store.Put("skill-center-pack", p.ID, p); e != nil {
			job.Status, job.Error, job.FinishedAt = "failed", "无法保存扫描状态", store.Now()
			_ = m.Store.Put("skill-center-job", job.ID, job)
			return job, e
		}
	}
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	center.cancels[job.ID] = cancel
	m.wg.Add(1)
	go m.runSkillScan(ctx, center, job, allowLLM)
	return job, nil
}

func (m *Manager) runSkillScan(ctx context.Context, center *skillCenterState, job SkillScanJob, allowLLM bool) {
	defer m.wg.Done()
	defer func() {
		center.mu.Lock()
		if cancel := center.cancels[job.ID]; cancel != nil {
			cancel()
		}
		delete(center.cancels, job.ID)
		center.mu.Unlock()
	}()
	center.mu.Lock()
	var before SkillScanJob
	_ = m.Store.Get("skill-center-job", job.ID, &before)
	if before.Status == "canceled" {
		center.mu.Unlock()
		return
	}
	job.Status = "running"
	job.StartedAt = store.Now()
	e := m.Store.Put("skill-center-job", job.ID, job)
	center.mu.Unlock()
	var report skillsafety.Report
	if e == nil {
		_, files, err := m.skillPackAssets(job.PackID)
		e = err
		if e == nil && job.Engine == "builtin" {
			report = skillsafety.Scan(safetyFiles(files))
		}
		if e == nil && job.Engine == "skillspector" {
			var temp string
			temp, e = os.MkdirTemp("", "rundesk-skill-scan-")
			if e == nil {
				defer os.RemoveAll(temp)
				for _, file := range files {
					path := filepath.Join(temp, filepath.FromSlash(file.Path))
					if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
						break
					}
					mode := fs.FileMode(0600)
					if file.Mode&0111 != 0 {
						mode = 0700
					}
					if e = os.WriteFile(path, file.Data, mode); e != nil {
						break
					}
				}
				if e == nil {
					report, e = skillsafety.RunSkillSpector(ctx, temp, os.Getenv("RUNDESK_SKILLSPECTOR_BIN"), allowLLM)
				}
			}
		}
	}
	center.mu.Lock()
	defer center.mu.Unlock()
	// Cancel wins once accepted, even if a tool races to emit its final report.
	var current SkillScanJob
	_ = m.Store.Get("skill-center-job", job.ID, &current)
	job.FinishedAt = store.Now()
	if current.Status == "canceled" || errors.Is(ctx.Err(), context.Canceled) {
		job.Status = "canceled"
		job.Error = "扫描已取消"
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		job.Status = "failed"
		job.Error = "扫描超时"
	} else {
		if report.Engine != "" {
			report.Digest = job.Digest
			report.Raw = nil
			job.Report = &report
		}
		if report.Status == "unavailable" {
			job.Status = "unavailable"
		} else if e != nil || report.Status == "error" {
			job.Status = "failed"
		} else {
			job.Status = "completed"
		}
		if e != nil {
			job.Error = diagnosticText(e.Error())
		}
		if job.Report != nil {
			p, err := m.SkillPack(job.PackID)
			if err == nil && p.Digest == job.Digest {
				if p.Reports == nil {
					p.Reports = map[string]skillsafety.Report{}
				}
				p.Reports[job.Engine] = report
				if job.Engine == "builtin" {
					p.Scan = report
				}
				if err = m.Store.Put("skill-center-pack", p.ID, p); err != nil {
					job.Status = "failed"
					job.Error = "扫描报告保存失败"
				}
			}
		}
	}
	_ = m.Store.Put("skill-center-job", job.ID, job)
}

func (m *Manager) CancelSkillScan(id string) (SkillScanJob, error) {
	center, e := m.skillsCenter()
	if e != nil {
		return SkillScanJob{}, e
	}
	center.mu.Lock()
	defer center.mu.Unlock()
	var job SkillScanJob
	if e = m.Store.Get("skill-center-job", id, &job); e != nil {
		return job, failure(404, "skill_scan_not_found", "扫描任务不存在")
	}
	if job.Status == "queued" || job.Status == "running" {
		job.Status = "canceled"
		job.FinishedAt = store.Now()
		job.Error = "扫描已取消"
		if e = m.Store.Put("skill-center-job", id, job); e != nil {
			return job, e
		}
		if cancel := center.cancels[id]; cancel != nil {
			cancel()
		}
	}
	return job, nil
}

func scanNeedsReview(report skillsafety.Report) bool {
	return report.Status != "completed" || report.Recommendation != "no_findings"
}

func summarizeSkillReport(report skillsafety.Report) skillsafety.Report {
	// Error responses are bounded to fit the WebUI's 32 KiB diagnostic reader.
	if len(report.Findings) > 5 {
		report.Findings = report.Findings[:5]
		report.Warnings = append(report.Warnings, "此处仅列前五项；技能详情保留完整报告")
	}
	if len(report.Coverage.Skipped) > 5 {
		report.Coverage.Skipped = report.Coverage.Skipped[:5]
	}
	if len(report.Warnings) > 5 {
		report.Warnings = report.Warnings[:5]
	}
	// Do not mutate slices shared with the persisted full report. External
	// reports can contain long text, so item counts alone do not bound JSON.
	clip := func(value string) string {
		if runes := []rune(value); len(runes) > 256 {
			return string(runes[:256]) + "…"
		}
		return value
	}
	report.Engine, report.EngineVersion = clip(report.Engine), clip(report.EngineVersion)
	report.Status, report.Recommendation, report.Digest = clip(report.Status), clip(report.Recommendation), clip(report.Digest)
	report.Findings = append([]skillsafety.Finding{}, report.Findings...)
	for index := range report.Findings {
		f := &report.Findings[index]
		f.ID, f.RuleID, f.Severity, f.Category = clip(f.ID), clip(f.RuleID), clip(f.Severity), clip(f.Category)
		f.File, f.Evidence, f.Reason, f.Recommendation = clip(f.File), clip(f.Evidence), clip(f.Reason), clip(f.Recommendation)
	}
	report.Coverage.Skipped = append([]skillsafety.Skipped{}, report.Coverage.Skipped...)
	for index := range report.Coverage.Skipped {
		report.Coverage.Skipped[index].File = clip(report.Coverage.Skipped[index].File)
		report.Coverage.Skipped[index].Reason = clip(report.Coverage.Skipped[index].Reason)
	}
	report.Warnings = append([]string{}, report.Warnings...)
	for index := range report.Warnings {
		report.Warnings[index] = clip(report.Warnings[index])
	}
	report.Raw = nil
	for {
		data, _ := json.Marshal(report)
		if len(data) <= 8<<10 {
			break
		}
		if len(report.Findings) > 0 {
			report.Findings = report.Findings[:len(report.Findings)-1]
		} else if len(report.Coverage.Skipped) > 0 {
			report.Coverage.Skipped = report.Coverage.Skipped[:len(report.Coverage.Skipped)-1]
		} else if len(report.Warnings) > 0 {
			report.Warnings = report.Warnings[:len(report.Warnings)-1]
		} else {
			break
		}
	}
	return report
}

func (m *Manager) importReviewedSkill(wid, name string, files []skillAsset, replace, ack bool, reason string, pack *SkillPack, opts ...string) (SkillBundle, SkillInstallReceipt, error) {
	if pack != nil {
		center, e := m.skillsCenter()
		if e != nil {
			return SkillBundle{}, SkillInstallReceipt{}, e
		}
		// Serialize admission and report publication with installation. A tool
		// must finish before the user can act on its verdict for this pack.
		center.mu.Lock()
		defer center.mu.Unlock()
		for active := range center.cancels {
			var job SkillScanJob
			if m.Store.Get("skill-center-job", active, &job) == nil && job.PackID == pack.ID {
				return SkillBundle{}, SkillInstallReceipt{}, failure(409, "skill_scan_running", "请等待该技能的扫描任务结束后再安装")
			}
		}
		latest, e := m.SkillPack(pack.ID)
		if e != nil {
			return SkillBundle{}, SkillInstallReceipt{}, e
		}
		pack = &latest
	}
	files, e := normalizeSkillAssets(files)
	if e != nil {
		return SkillBundle{}, SkillInstallReceipt{}, e
	}
	report := skillsafety.Scan(safetyFiles(files))
	review := scanNeedsReview(report)
	if pack != nil {
		for engine, r := range pack.Reports {
			if engine != "builtin" && r.Digest == pack.Digest && scanNeedsReview(r) {
				review = true
			}
		}
	}
	reason = strings.TrimSpace(reason)
	if review && (!ack || len([]rune(reason)) < 4 || len(reason) > 2000) {
		summaries := map[string]skillsafety.Report{}
		if pack != nil {
			for engine, r := range pack.Reports {
				summaries[engine] = summarizeSkillReport(r)
			}
		}
		return SkillBundle{}, SkillInstallReceipt{}, &skillCenterError{Status: 409, Code: "skill_review_required", Message: "此技能存在待复核项或检查不完整，请查看报告并说明继续安装的理由", Details: map[string]any{"report": summarizeSkillReport(report), "digest": report.Digest, "reports": summaries}}
	}
	if pack != nil && report.Digest != pack.Digest {
		return SkillBundle{}, SkillInstallReceipt{}, failure(409, "skill_snapshot_changed", "安装内容与检查快照不一致")
	}
	result, e := m.ImportSkillBundle(wid, name, files, replace, opts...)
	if e != nil {
		return result, SkillInstallReceipt{}, e
	}
	iid, scope := DefaultInstance, "instance"
	if len(opts) > 0 && opts[0] != "" {
		iid = opts[0]
	}
	if len(opts) > 1 && opts[1] != "" {
		scope = opts[1]
	}
	receipt := SkillInstallReceipt{ID: store.ID(), Name: name, WorkspaceID: wid, InstanceID: iid, Scope: scope, Digest: report.Digest, InstalledAt: store.Now(), Acknowledged: review && ack}
	if receipt.Acknowledged {
		receipt.Reason = reason
	}
	if pack != nil {
		receipt.PackID = pack.ID
		receipt.SourceURL = pack.SourceURL
		receipt.Revision = pack.Revision
	}
	if e = m.Store.Put("skill-center-install", receipt.ID, receipt); e != nil {
		return result, receipt, &skillCenterError{Status: 500, Code: "skill_install_receipt_failed", Message: "技能已写入，但安装回执未能保存；请先检查目标目录，不要直接重复安装", Details: map[string]any{"installed": true, "digest": report.Digest}}
	}
	result.Digest = report.Digest
	result.Scan = &report
	return result, receipt, nil
}

func writeSkillCenterError(w http.ResponseWriter, e error) {
	var local *skillCenterError
	var remote *skillcatalog.Error
	if errors.As(e, &local) {
		writeJSON(w, local.Status, map[string]any{"error": local.Message, "code": local.Code, "details": local.Details, "retryable": false, "requestId": w.Header().Get("X-Request-ID")})
		return
	}
	if errors.As(e, &remote) {
		writeJSON(w, 422, map[string]any{"error": remote.Message, "code": remote.Code, "details": remote.Details, "retryable": false, "requestId": w.Header().Get("X-Request-ID")})
		return
	}
	respond(w, nil, e)
}

func (s *Server) skillCenterRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if principal(r) != nil || human(r) != nil {
				respond(w, nil, failure(403, "skill_center_admin_required", "技能中心由管理员配置"))
				return
			}
			handler(w, r)
		})
	}
	register("GET /api/skill-center/sources", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": []map[string]string{{"id": "clawhub", "label": "ClawHub", "description": "公开关键词搜索"}, {"id": "skillsmp", "label": "SkillsMP", "description": "匿名搜索有配额，可配置 SKILLSMP_API_KEY"}, {"id": "skills.sh", "label": "skills.sh", "description": "官方 CLI 的公开搜索接口；可选 SKILLS_SH_TOKEN"}, {"id": "github", "label": "GitHub", "description": "候选仓库搜索，可选 GITHUB_TOKEN"}}})
	})
	register("GET /api/skill-center/engines", func(w http.ResponseWriter, r *http.Request) {
		binary := os.Getenv("RUNDESK_SKILLSPECTOR_BIN")
		if binary == "" {
			binary = "skillspector"
		}
		_, e := exec.LookPath(binary)
		message := "命令已发现，实际可用性以扫描结果为准"
		if e != nil {
			message = "未安装；配置 RUNDESK_SKILLSPECTOR_BIN 或将 skillspector 加入 PATH"
		}
		writeJSON(w, 200, map[string]any{"items": []map[string]any{{"id": "builtin", "available": true, "message": "Go 静态快速检查，不执行技能"}, {"id": "skillspector", "available": e == nil, "message": message}}})
	})
	register("GET /api/skill-center/search", func(w http.ResponseWriter, r *http.Request) {
		sources := []string{"clawhub", "skillsmp", "skills.sh"}
		if raw, ok := r.URL.Query()["sources"]; ok {
			sources = []string{}
			for _, source := range strings.Split(raw[0], ",") {
				if source = strings.TrimSpace(source); source != "" {
					sources = append(sources, source)
				}
			}
		}
		v, e := s.Manager.SearchSkills(r.Context(), r.URL.Query().Get("q"), sources)
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	register("GET /api/skill-center/packs", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.SkillPacks(r.URL.Query().Get("q"))
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": v})
	})
	register("POST /api/skill-center/fetch", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.FetchSkillPack(r.Context(), in.URL)
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"pack": v, "scan": v.Scan})
	})
	register("GET /api/skill-center/packs/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.SkillPack(r.PathValue("id"))
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	register("GET /api/skill-center/packs/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		_, assets, e := s.Manager.skillPackAssets(r.PathValue("id"))
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		for _, file := range assets {
			if file.Path != r.URL.Query().Get("path") {
				continue
			}
			if !utf8.Valid(file.Data) || strings.ContainsRune(string(file.Data), 0) {
				writeJSON(w, 200, map[string]any{"path": file.Path, "binary": true, "size": len(file.Data)})
				return
			}
			text := file.Data
			truncated := len(text) > 128<<10
			if truncated {
				text = text[:128<<10]
			}
			writeJSON(w, 200, map[string]any{"path": file.Path, "text": string(text), "truncated": truncated})
			return
		}
		respond(w, nil, failure(404, "skill_file_not_found", "文件不存在"))
	})
	register("POST /api/skill-center/packs/{id}/scan", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Engine   string `json:"engine"`
			AllowLLM bool   `json:"allowLLM"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Engine == "" {
			in.Engine = "builtin"
		}
		job, e := s.Manager.StartSkillScan(r.PathValue("id"), in.Engine, in.AllowLLM)
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 202, map[string]any{"job": job})
	})
	register("GET /api/skill-center/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		job, e := s.Manager.SkillScanJob(r.PathValue("id"))
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, job)
	})
	register("POST /api/skill-center/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		job, e := s.Manager.CancelSkillScan(r.PathValue("id"))
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, job)
	})
	register("POST /api/skill-center/packs/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			WorkspaceID string `json:"workspaceId"`
			InstanceID  string `json:"instanceId"`
			Scope       string `json:"scope"`
			Name        string `json:"name"`
			Replace     bool   `json:"replace"`
			Digest      string `json:"digest"`
			Acknowledge bool   `json:"acknowledge"`
			Reason      string `json:"reason"`
		}
		if !decode(w, r, &in) {
			return
		}
		pack, files, e := s.Manager.skillPackAssets(r.PathValue("id"))
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		if in.Digest != pack.Digest {
			respond(w, nil, failure(409, "skill_digest_mismatch", "请确认当前技能内容摘要后安装"))
			return
		}
		if in.Scope == "" {
			in.Scope = "project"
		}
		result, receipt, e := s.Manager.importReviewedSkill(in.WorkspaceID, in.Name, files, in.Replace, in.Acknowledge, in.Reason, &pack, in.InstanceID, in.Scope)
		if e != nil {
			writeSkillCenterError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"bundle": result, "receipt": receipt})
	})
	register("GET /api/skill-center/installations", func(w http.ResponseWriter, r *http.Request) {
		rows, e := s.Manager.Store.List("skill-center-install")
		if e != nil {
			respond(w, nil, e)
			return
		}
		items := []SkillInstallReceipt{}
		for _, row := range rows {
			var v SkillInstallReceipt
			if json.Unmarshal(row, &v) == nil {
				items = append(items, v)
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].InstalledAt > items[j].InstalledAt })
		writeJSON(w, 200, map[string]any{"items": items})
	})
}
