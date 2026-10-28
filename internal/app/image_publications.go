package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

// Publication uploads an already validated immutable image. It does not build,
// mutate an instance, make a registry public, or modify the release catalog.
type ImagePublicationInput struct {
	SourceBuildID string `json:"sourceBuildId"`
	Target        string `json:"target"`
	RequestID     string `json:"requestId"`
	Confirm       bool   `json:"confirm"`
}
type ImagePublicationReadiness struct {
	Eligible       bool   `json:"eligible"`
	SourceBuildID  string `json:"sourceBuildId,omitempty"`
	ImageID        string `json:"imageId,omitempty"`
	Reason         string `json:"reason,omitempty"`
	OfflineTestID  string `json:"offlineTestId,omitempty"`
	SmokeTestID    string `json:"smokeTestId,omitempty"`
	ProfileID      string `json:"profileId,omitempty"`
	ProfileVersion string `json:"profileVersion,omitempty"`
	CodexVersion   string `json:"codexVersion,omitempty"`
	Platform       string `json:"platform,omitempty"`
}
type ImagePublication struct {
	ID                   string         `json:"id"`
	SourceBuildID        string         `json:"sourceBuildId"`
	Target               string         `json:"target"`
	RequestID            string         `json:"requestId"`
	Status               string         `json:"status"`
	ImageID              string         `json:"imageId"`
	OfflineTestID        string         `json:"offlineTestId"`
	SmokeTestID          string         `json:"smokeTestId"`
	ProfileID            string         `json:"profileId"`
	ProfileVersion       string         `json:"profileVersion"`
	CodexVersion         string         `json:"codexVersion"`
	Platform             string         `json:"platform"`
	Digest               string         `json:"digest,omitempty"`
	Reference            string         `json:"reference,omitempty"`
	PublicImage          map[string]any `json:"publicImage,omitempty"`
	PublicAccessVerified bool           `json:"publicAccessVerified"`
	PublicCheckedAt      string         `json:"publicCheckedAt,omitempty"`
	PublicAccessError    string         `json:"publicAccessError,omitempty"`
	PublicChecking       bool           `json:"publicChecking"`
	Created              string         `json:"created"`
	Updated              string         `json:"updated"`
	Error                string         `json:"error,omitempty"`
	LogTruncated         bool           `json:"logTruncated"`
}
type imagePublicationRecord struct {
	ImagePublication
	Fingerprint       string `json:"fingerprint"`
	SourceFingerprint string `json:"sourceFingerprint"`
}

var publicationTarget = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)(?::[0-9]{1,5})?/(?:[a-z0-9]+(?:[._-][a-z0-9]+)*/)*[a-z0-9]+(?:[._-][a-z0-9]+)*:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var publicationDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var publicationPushDigest = regexp.MustCompile(`(?m)(?:^|[ \t])digest:[ \t]*(sha256:[a-f0-9]{64})(?:[ \t]|$)`)

func publicationRepository(target string) string { return target[:strings.LastIndex(target, ":")] }
func publicationSourceFingerprint(b ImageBuild) string {
	raw, _ := json.Marshal(b)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func normalizePublicationTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > 512 || !publicationTarget.MatchString(value) {
		return "", failure(400, "invalid_publication_target", "请输入完整仓库地址和固定版本标签，例如 ghcr.io/owner/news2douyin:1.1.2")
	}
	host := strings.SplitN(value, "/", 2)[0]
	if !strings.ContainsAny(host, ".:") && host != "localhost" {
		return "", failure(400, "invalid_publication_target", "镜像地址必须显式包含仓库主机，如 docker.io 或 ghcr.io")
	}
	if strings.EqualFold(value[strings.LastIndex(value, ":")+1:], "latest") {
		return "", failure(400, "publication_fixed_version_required", "发布预设需要固定版本标签，不能使用 latest")
	}
	if host == "index.docker.io" || host == "registry-1.docker.io" {
		value = "docker.io/" + strings.SplitN(value, "/", 2)[1]
	}
	if strings.HasPrefix(value, "docker.io/") && strings.Count(value, "/") == 1 {
		value = "docker.io/library/" + strings.TrimPrefix(value, "docker.io/")
	}
	return value, nil
}
func (m *Manager) publicationReadiness(source ImageBuild, inspect bool) (ImagePublicationReadiness, error) {
	out := ImagePublicationReadiness{SourceBuildID: source.ID, ImageID: source.ImageID, ProfileID: source.ProfileID, CodexVersion: source.CodexVersion, Platform: source.PlatformOS + "/" + source.PlatformArch}
	reject := func(reason string) (ImagePublicationReadiness, error) { out.Reason = reason; return out, nil }
	if source.Status != "succeeded" || source.Scope == "image-test" || !validBuildCenterImageID(source.ImageID) {
		return reject("请先选择成功生成不可变镜像的构建")
	}
	switch source.ProfileID {
	case "news2douyin", "people-search", "video-build", "hyperframes", "video-use", "openmontage":
	default:
		return reject("此构建缺少受支持的预设身份，无法作为公开预设发布")
	}
	if source.RuntimeIdentity == nil || !profileRelease.MatchString(source.RuntimeIdentity.ProfileVersion) || !profileRelease.MatchString(source.CodexVersion) || source.PlatformOS != "linux" || source.PlatformArch == "" || len(source.ContextSHA256) != 64 {
		return reject("构建缺少冻结的预设、平台、版本或上下文证据，请重新构建并验证")
	}
	out.ProfileVersion = source.RuntimeIdentity.ProfileVersion
	rows, err := m.buildsLocked("")
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, test := range rows {
		if test.Scope != "image-test" || test.SourceBuildID != source.ID || test.ImageID != source.ImageID || test.ContextSHA256 != source.ContextSHA256 || test.RecipeSHA != source.RecipeSHA || test.PlatformOS != source.PlatformOS || test.PlatformArch != source.PlatformArch || !reflect.DeepEqual(test.RuntimeIdentity, source.RuntimeIdentity) || !reflect.DeepEqual(test.CodexRuntime, source.CodexRuntime) || test.CodexVersion != source.CodexVersion {
			continue
		}
		if seen[test.TestKind] {
			continue
		}
		seen[test.TestKind] = true
		if test.Status != "succeeded" {
			continue
		}
		switch test.TestKind {
		case "offline":
			out.OfflineTestID = test.ID
		case "smoke":
			if source.RecipeSmoke != nil && len(source.RecipeSmoke.Command) > 0 && reflect.DeepEqual(test.RecipeSmoke, source.RecipeSmoke) {
				out.SmokeTestID = test.ID
			}
		}
	}
	if out.OfflineTestID == "" || out.SmokeTestID == "" {
		return reject("请在构建中心对同一构建镜像完成 Offline 检查和 Smoke 测试；仅构建成功不能发布")
	}
	if inspect {
		image, err := m.inspectImage(source.ImageID)
		if err != nil {
			return reject(diagnosticText(err.Error()))
		}
		if image.ID != source.ImageID || image.Os != source.PlatformOS || image.Architecture != source.PlatformArch || !runtimeImageMatches(source, image) {
			return reject("本机镜像与已验证构建的身份不一致")
		}
	}
	out.Eligible = true
	return out, nil
}
func (m *Manager) ImagePublicationReadiness(key string) (ImagePublicationReadiness, error) {
	image, err := m.BuildCenterImage(key)
	if err != nil {
		return ImagePublicationReadiness{}, err
	}
	rows, err := m.buildsLocked("")
	if err != nil {
		return ImagePublicationReadiness{}, err
	}
	out := ImagePublicationReadiness{ImageID: image.ImageID, Reason: "此镜像没有可发布的成功构建及验证记录"}
	for _, source := range rows {
		if source.ImageID != image.ImageID || source.Scope == "image-test" || source.Status != "succeeded" {
			continue
		}
		candidate, e := m.publicationReadiness(source, true)
		if e != nil {
			return out, e
		}
		if candidate.Eligible {
			return candidate, nil
		}
		if out.SourceBuildID == "" {
			out = candidate
		}
	}
	return out, nil
}
func (m *Manager) imagePublicationsLocked() ([]imagePublicationRecord, error) {
	rows, err := m.Store.List("image-publication")
	out := []imagePublicationRecord{}
	if err != nil {
		return nil, err
	}
	for _, raw := range rows {
		var v imagePublicationRecord
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}
func (m *Manager) ImagePublications() ([]ImagePublication, error) {
	m.publicationMu.Lock()
	defer m.publicationMu.Unlock()
	rows, err := m.imagePublicationsLocked()
	out := []ImagePublication{}
	for _, v := range rows {
		out = append(out, v.ImagePublication)
	}
	return out, err
}
func (m *Manager) publicationLocked(id string) (imagePublicationRecord, error) {
	var v imagePublicationRecord
	if !buildCenterID.MatchString(id) {
		return v, failure(404, "publication_not_found", "发布任务不存在")
	}
	err := m.Store.Get("image-publication", id, &v)
	if errors.Is(err, sql.ErrNoRows) {
		err = failure(404, "publication_not_found", "发布任务不存在")
	}
	return v, err
}
func (m *Manager) ImagePublication(id string) (ImagePublication, error) {
	m.publicationMu.Lock()
	defer m.publicationMu.Unlock()
	v, e := m.publicationLocked(id)
	return v.ImagePublication, e
}
func (m *Manager) StartImagePublication(in ImagePublicationInput) (ImagePublication, error) {
	var zero ImagePublication
	if !in.Confirm {
		return zero, failure(400, "publication_confirmation_required", "请确认将已验证镜像上传到指定仓库")
	}
	if !buildCenterID.MatchString(in.RequestID) || !buildCenterID.MatchString(in.SourceBuildID) {
		return zero, failure(400, "invalid_publication_request", "发布需要有效的 sourceBuildId 和 requestId")
	}
	target, err := normalizePublicationTarget(in.Target)
	if err != nil {
		return zero, err
	}
	in.Target = target
	data, _ := json.Marshal(in)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(data))
	m.publicationMu.Lock()
	defer m.publicationMu.Unlock()
	if err = m.ctx.Err(); err != nil {
		return zero, err
	}
	rows, err := m.imagePublicationsLocked()
	if err != nil {
		return zero, err
	}
	active := 0
	for _, v := range rows {
		if v.RequestID == in.RequestID {
			if v.Fingerprint != fingerprint {
				return zero, failure(409, "publication_request_conflict", "同一 requestId 已用于其他发布参数")
			}
			return v.ImagePublication, nil
		}
		if buildActive(v.Status) {
			active++
			if v.Target == target {
				return zero, failure(409, "publication_target_busy", "此目标已有发布任务，请等待或查看原任务")
			}
		}
	}
	if active >= 8 {
		return zero, failure(429, "publication_queue_full", "发布队列已满，请等待当前任务完成")
	}
	source, err := m.BuildCenterBuild(in.SourceBuildID)
	if err != nil {
		return zero, err
	}
	ready, err := m.publicationReadiness(source, true)
	if err != nil {
		return zero, err
	}
	if !ready.Eligible {
		return zero, failure(409, "publication_not_ready", ready.Reason)
	}
	for _, v := range rows {
		if v.Target == target && v.ImageID != source.ImageID {
			return zero, failure(409, "publication_tag_conflict", "此版本标签已用于另一镜像的发布请求，请使用新的版本标签")
		}
	}
	now := store.Now()
	v := imagePublicationRecord{ImagePublication: ImagePublication{ID: store.ID(), RequestID: in.RequestID, SourceBuildID: source.ID, Target: target, Status: "queued", ImageID: source.ImageID, OfflineTestID: ready.OfflineTestID, SmokeTestID: ready.SmokeTestID, ProfileID: ready.ProfileID, ProfileVersion: ready.ProfileVersion, CodexVersion: ready.CodexVersion, Platform: ready.Platform, Created: now, Updated: now}, Fingerprint: fingerprint, SourceFingerprint: publicationSourceFingerprint(source)}
	if err = os.MkdirAll(m.publicationDir(v.ID), 0700); err != nil {
		return zero, err
	}
	err = m.Store.Put("image-publication", v.ID, v)
	return v.ImagePublication, err
}
func (m *Manager) publicationDir(id string) string {
	return filepath.Join(m.Data, "image-publications", id)
}
func (m *Manager) initImagePublications() error {
	m.publicationCancels = map[string]context.CancelFunc{}
	rows, err := m.imagePublicationsLocked()
	if err != nil {
		return err
	}
	for _, v := range rows {
		if v.PublicChecking {
			v.PublicChecking = false
			v.PublicAccessVerified = false
			v.PublicAccessError = "匿名访问检查因 RunDesk 重启而中断，请重新检查。"
			if err = m.Store.Put("image-publication", v.ID, v); err != nil {
				return err
			}
		}
		if !buildActive(v.Status) {
			continue
		}
		v.Status = "interrupted"
		v.Error = "RunDesk 已重启；远端可能已有部分或完整上传，请检查仓库后再创建新发布。不会自动重新推送。"
		v.Updated = store.Now()
		if err = m.Store.Put("image-publication", v.ID, v); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) imagePublicationLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.nextImagePublication()
		}
	}
}
func (m *Manager) nextImagePublication() {
	m.publicationMu.Lock()
	rows, err := m.imagePublicationsLocked()
	if err != nil {
		m.publicationMu.Unlock()
		return
	}
	var selected imagePublicationRecord
	for index := len(rows) - 1; index >= 0; index-- {
		if rows[index].Status == "queued" {
			selected = rows[index]
			break
		}
	}
	if selected.ID == "" || m.ctx.Err() != nil {
		m.publicationMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, 60*time.Minute)
	selected.Status = "running"
	selected.Updated = store.Now()
	if err = m.Store.Put("image-publication", selected.ID, selected); err != nil {
		cancel()
		m.publicationMu.Unlock()
		return
	}
	m.publicationCancels[selected.ID] = cancel
	m.publicationMu.Unlock()
	digest, truncated, err := m.executeImagePublication(ctx, selected)
	cancel()
	m.publicationMu.Lock()
	defer m.publicationMu.Unlock()
	delete(m.publicationCancels, selected.ID)
	current, e := m.publicationLocked(selected.ID)
	if e != nil {
		return
	}
	current.LogTruncated = truncated
	current.Updated = store.Now()
	switch {
	case m.ctx.Err() != nil:
		current.Status = "interrupted"
		current.Error = "RunDesk 停止，远端上传结果未确认；不会自动重新推送。"
	case current.Status == "canceling":
		current.Status = "canceled"
		current.Error = "已停止本机发布任务；远端可能已有部分或完整上传，请检查仓库。"
	case err != nil:
		current.Status = "failed"
		current.Error = diagnosticText(err.Error())
	default:
		current.Status = "succeeded"
		current.Digest = digest
		current.Reference = publicationRepository(current.Target) + "@" + digest
		current.PublicImage = map[string]any{"id": current.ProfileID + "-" + current.ID, "profileId": current.ProfileID, "profileVersion": current.ProfileVersion, "codexVersion": current.CodexVersion, "platform": current.Platform, "sources": []map[string]string{{"id": "published", "name": strings.SplitN(current.Target, "/", 2)[0], "reference": current.Reference}}}
	}
	_ = m.Store.Put("image-publication", current.ID, current)
}
func (m *Manager) executeImagePublication(ctx context.Context, v imagePublicationRecord) (digest string, truncated bool, err error) {
	f, err := os.OpenFile(filepath.Join(m.publicationDir(v.ID), "publish.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	bounded := &buildLogWriter{f: f}
	out := newBuildLogRedactor(bounded)
	defer func() {
		if e := out.Flush(); err == nil {
			err = e
		}
		truncated = bounded.truncated || out.Truncated()
	}()
	source, err := m.BuildCenterBuild(v.SourceBuildID)
	if err != nil {
		return "", false, err
	}
	if publicationSourceFingerprint(source) != v.SourceFingerprint {
		return "", false, failure(409, "publication_source_changed", "来源构建记录已改变，请重新审阅")
	}
	m.executionMu.RLock()
	defer m.executionMu.RUnlock()
	ready, err := m.publicationReadiness(source, true)
	if err != nil {
		return "", false, err
	}
	if !ready.Eligible || ready.OfflineTestID != v.OfflineTestID || ready.SmokeTestID != v.SmokeTestID {
		return "", false, failure(409, "publication_evidence_changed", "镜像或验证记录已改变，停止发布，请重新检查")
	}
	fmt.Fprintf(out, "上传已验证镜像 %s 到 %s\n", v.ImageID, v.Target)
	if err = m.runtimeDockerCommand(ctx, out, "tag", v.ImageID, v.Target); err != nil {
		return "", false, err
	}
	image, err := m.inspectImage(v.Target)
	if err != nil || image.ID != v.ImageID {
		return "", false, failure(409, "publication_tag_changed", "发布前目标标签不再指向已验证镜像")
	}
	// The parser observes only Docker's bounded trailing output; credentials are
	// never returned, persisted here, or placed in command arguments.
	tail := &publicationTail{}
	if err = m.runtimeDockerCommand(ctx, io.MultiWriter(out, tail), "push", v.Target); err != nil {
		return "", false, fmt.Errorf("镜像上传失败；请检查仓库、服务账户 Docker 登录及网络。远端可能已有数据：%w", err)
	}
	matches := publicationPushDigest.FindAllSubmatch(tail.data, -1)
	if len(matches) == 0 {
		return "", false, failure(502, "publication_digest_unconfirmed", "上传命令完成，但没有可核对的仓库 Digest；请检查仓库，不要自动重推")
	}
	digest = string(matches[len(matches)-1][1])
	image, err = m.inspectImage(v.Target)
	if err != nil || image.ID != v.ImageID {
		return "", false, failure(409, "publication_image_changed", "上传后镜像标签身份已改变，不能确认发布结果")
	}
	wanted := publicationRepository(v.Target) + "@" + digest
	for _, reference := range image.RepoDigests {
		if canonicalImageDigestReference(reference) == canonicalImageDigestReference(wanted) {
			return digest, false, nil
		}
	}
	return "", false, failure(502, "publication_digest_mismatch", "Docker 上传摘要与本机 RepoDigests 不一致；远端可能已上传，请检查仓库")
}

type publicationTail struct {
	mu   sync.Mutex
	data []byte
}

func (w *publicationTail) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if n >= 64<<10 {
		w.data = append(w.data[:0], p[n-(64<<10):]...)
	} else {
		w.data = append(w.data, p...)
		if len(w.data) > 64<<10 {
			w.data = append(w.data[:0], w.data[len(w.data)-(64<<10):]...)
		}
	}
	return n, nil
}
func (m *Manager) CancelImagePublication(id string) (ImagePublication, error) {
	m.publicationMu.Lock()
	defer m.publicationMu.Unlock()
	v, err := m.publicationLocked(id)
	if err != nil {
		return v.ImagePublication, err
	}
	switch v.Status {
	case "queued":
		v.Status = "canceled"
		v.Error = "发布在开始上传前取消。"
	case "running", "canceling":
		v.Status = "canceling"
		if cancel := m.publicationCancels[id]; cancel != nil {
			cancel()
		}
	default:
		return v.ImagePublication, nil
	}
	v.Updated = store.Now()
	err = m.Store.Put("image-publication", id, v)
	return v.ImagePublication, err
}
func (m *Manager) ImagePublicationLog(id string, after int64) (any, error) {
	v, err := m.ImagePublication(id)
	if err != nil {
		return nil, err
	}
	if after < 0 || after > buildLogLimit {
		return nil, failure(400, "invalid_log_offset", "日志偏移无效")
	}
	f, err := os.Open(filepath.Join(m.publicationDir(id), "publish.log"))
	if os.IsNotExist(err) {
		return map[string]any{"text": "", "next": 0, "status": v.Status, "truncated": v.LogTruncated}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var clean bytes.Buffer
	redactor := newBuildLogRedactor(&clean)
	if _, err = io.Copy(redactor, io.LimitReader(f, buildLogLimit)); err != nil {
		return nil, err
	}
	if err = redactor.Flush(); err != nil {
		return nil, err
	}
	data := clean.Bytes()
	if after > int64(len(data)) {
		after = int64(len(data))
	}
	end := after + (64 << 10)
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return map[string]any{"text": strings.ToValidUTF8(string(data[after:end]), "�"), "next": end, "status": v.Status, "truncated": v.LogTruncated || redactor.Truncated()}, nil
}
func (s *Server) imagePublicationRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/build-center/images/{key}/publication-readiness", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ImagePublicationReadiness(r.PathValue("key"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/build-center/publications", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ImagePublications()
		if e != nil {
			respond(w, nil, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": v})
	})
	mux.HandleFunc("POST /api/build-center/publications", func(w http.ResponseWriter, r *http.Request) {
		var in ImagePublicationInput
		if !decode(w, r, &in) {
			return
		}
		v, e := m.StartImagePublication(in)
		if e != nil {
			respond(w, nil, e)
			return
		}
		writeJSON(w, http.StatusAccepted, v)
	})
	mux.HandleFunc("GET /api/build-center/publications/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ImagePublication(r.PathValue("id"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/build-center/publications/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.CancelImagePublication(r.PathValue("id"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/build-center/publications/{id}/check-public", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.CheckImagePublicationPublic(r.Context(), r.PathValue("id"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/build-center/publications/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		var after int64
		var e error
		if raw := r.URL.Query().Get("after"); raw != "" {
			after, e = strconv.ParseInt(raw, 10, 64)
		}
		if e != nil {
			respond(w, nil, failure(400, "invalid_log_offset", "日志偏移无效"))
			return
		}
		v, e := m.ImagePublicationLog(r.PathValue("id"), after)
		respond(w, v, e)
	})
}

func (m *Manager) CheckImagePublicationPublic(parent context.Context, id string) (ImagePublication, error) {
	m.publicationMu.Lock()
	value, err := m.publicationLocked(id)
	if err != nil {
		m.publicationMu.Unlock()
		return value.ImagePublication, err
	}
	if value.Status != "succeeded" || !publicationDigest.MatchString(value.Digest) || value.Reference != publicationRepository(value.Target)+"@"+value.Digest {
		m.publicationMu.Unlock()
		return value.ImagePublication, failure(409, "publication_not_pushed", "请先完成上传并核对 Digest")
	}
	if value.PublicChecking {
		m.publicationMu.Unlock()
		return value.ImagePublication, failure(409, "publication_check_in_progress", "匿名访问检查正在进行")
	}
	if err = m.ctx.Err(); err != nil {
		m.publicationMu.Unlock()
		return value.ImagePublication, err
	}
	values, e := m.imagePublicationsLocked()
	if e != nil {
		m.publicationMu.Unlock()
		return value.ImagePublication, e
	}
	running := 0
	for _, v := range values {
		if v.PublicChecking {
			running++
		}
	}
	if running >= 2 {
		m.publicationMu.Unlock()
		return value.ImagePublication, failure(429, "publication_check_busy", "最多同时检查两个公开镜像，请稍后重试")
	}
	value.PublicChecking = true
	value.PublicAccessVerified = false
	value.PublicAccessError = ""
	value.Updated = store.Now()
	if err = m.Store.Put("image-publication", id, value); err != nil {
		m.publicationMu.Unlock()
		return value.ImagePublication, err
	}
	m.wg.Add(1)
	m.publicationMu.Unlock()
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	err = m.checkPublicationAnonymous(ctx, value.ImagePublication)
	m.publicationMu.Lock()
	defer m.publicationMu.Unlock()
	current, e := m.publicationLocked(id)
	if e != nil {
		return current.ImagePublication, e
	}
	current.PublicChecking = false
	current.PublicCheckedAt = store.Now()
	current.Updated = current.PublicCheckedAt
	current.PublicAccessVerified = err == nil
	if err != nil {
		current.PublicAccessError = diagnosticText(err.Error())
	} else {
		current.PublicAccessError = ""
	}
	e = m.Store.Put("image-publication", id, current)
	return current.ImagePublication, e
}
func (m *Manager) checkPublicationAnonymous(ctx context.Context, value ImagePublication) error {
	directory, err := os.MkdirTemp(m.Data, "anonymous-registry-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err = os.WriteFile(filepath.Join(directory, "config.json"), []byte("{\"auths\":{}}\n"), 0600); err != nil {
		return err
	}
	capture := &publicationCapture{}
	// --config is a Docker GLOBAL option. No login or credential helper is copied
	// into this directory; unlike pull, manifest inspection does not use daemon auth.
	err = m.runtimeDockerCommand(ctx, capture, "--config", directory, "manifest", "inspect", "--verbose", value.Reference)
	if err != nil {
		return fmt.Errorf("无法匿名读取固定 Digest；请检查仓库是否已公开、网络及 Docker manifest 支持：%w", err)
	}
	var descriptor struct {
		Digest     string
		Platform   struct{ OS, Architecture string }
		Descriptor struct {
			Digest   string
			Platform struct{ OS, Architecture string }
		}
	}
	if err = json.Unmarshal(capture.data, &descriptor); err != nil {
		return failure(502, "publication_public_manifest_invalid", "匿名响应不是可核对的单平台镜像 manifest")
	}
	digest, osName, arch := descriptor.Digest, descriptor.Platform.OS, descriptor.Platform.Architecture
	if digest == "" {
		digest, osName, arch = descriptor.Descriptor.Digest, descriptor.Descriptor.Platform.OS, descriptor.Descriptor.Platform.Architecture
	}
	if digest != value.Digest || osName+"/"+arch != value.Platform {
		return failure(502, "publication_public_identity_mismatch", "匿名访问返回的 Digest 或平台与已发布镜像不符")
	}
	return nil
}

type publicationCapture struct {
	mu   sync.Mutex
	data []byte
}

func (w *publicationCapture) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.data)+len(p) > 2<<20 {
		return 0, fmt.Errorf("registry manifest response exceeds 2 MiB")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}
