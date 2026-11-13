package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/skillcheck"
	"github.com/shengjuntu/rundesk/internal/skillsafety"
	"github.com/shengjuntu/rundesk/internal/store"
)

const skillCheckRecordLimit = 200

type SkillCheckJob struct {
	ID              string                 `json:"id"`
	PackID          string                 `json:"packId"`
	Digest          string                 `json:"digest"`
	SuiteDigest     string                 `json:"suiteDigest"`
	Action          string                 `json:"action"`
	InstanceID      string                 `json:"instanceId"`
	EnvironmentName string                 `json:"environmentName"`
	ImageID         string                 `json:"imageId"`
	ContainerName   string                 `json:"containerName"`
	AllowNetwork    bool                   `json:"allowNetwork"`
	Status          string                 `json:"status"`
	CreatedAt       string                 `json:"createdAt"`
	StartedAt       string                 `json:"startedAt,omitempty"`
	FinishedAt      string                 `json:"finishedAt,omitempty"`
	ReportID        string                 `json:"reportId,omitempty"`
	Output          string                 `json:"output,omitempty"`
	ExitCode        int                    `json:"exitCode"`
	Error           string                 `json:"error,omitempty"`
	CleanupError    string                 `json:"cleanupError,omitempty"`
	CleanupPending  bool                   `json:"cleanupPending"`
	Validation      *skillcheck.Validation `json:"validation,omitempty"`
}

type SkillCheckReportRecord struct {
	ID           string            `json:"id"`
	PackID       string            `json:"packId"`
	Digest       string            `json:"digest"`
	SuiteDigest  string            `json:"suiteDigest,omitempty"`
	Source       string            `json:"source"`
	JobID        string            `json:"jobId,omitempty"`
	InstanceID   string            `json:"instanceId,omitempty"`
	ImageID      string            `json:"imageId,omitempty"`
	CreatedAt    string            `json:"createdAt"`
	CasesOmitted bool              `json:"casesOmitted"`
	Report       skillcheck.Report `json:"report"`
}

type skillCheckState struct {
	cancels map[string]context.CancelFunc
}

// Called only under skillCheckMu, which also closes admission before wg.Wait.
func (m *Manager) initSkillChecksLocked() error {
	if m.ctx.Err() != nil {
		return failure(503, "skill_check_stopped", "后台正在停止")
	}
	if m.skillChecks != nil {
		return nil
	}
	rows, err := m.Store.List("skill-check-job")
	if err != nil {
		return err
	}
	state := &skillCheckState{cancels: map[string]context.CancelFunc{}}
	for _, row := range rows {
		var job SkillCheckJob
		if err := json.Unmarshal(row, &job); err != nil {
			return err
		}
		if job.Status != "queued" && job.Status != "running" && !job.CleanupPending && job.CleanupError == "" {
			continue
		}
		needsCleanup := job.StartedAt != "" || job.CleanupError != ""
		job.Status, job.Error, job.FinishedAt = "interrupted", "后台已重启，测试未自动重跑", store.Now()
		job.CleanupPending = false
		// Only the label-qualified cleanup helper may remove a prior owned job.
		if needsCleanup && skillCenterID.MatchString(job.ID) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.cleanupSkillCheck(ctx, job); err != nil {
				job.CleanupError = diagnosticText(err.Error())
				job.CleanupPending = true
				job.Error += "；独立容器清理未确认，请检查 Docker 状态"
				state.cancels[job.ID] = func() {}
			} else {
				job.CleanupError = ""
			}
			cancel()
		}
		if err := m.Store.Put("skill-check-job", job.ID, job); err != nil {
			return err
		}
	}
	m.skillChecks = state
	return nil
}

func (m *Manager) SkillCheckJobs(packID string) ([]SkillCheckJob, error) {
	if _, err := m.SkillPack(packID); err != nil {
		return nil, err
	}
	m.skillCheckMu.Lock()
	defer m.skillCheckMu.Unlock()
	if err := m.initSkillChecksLocked(); err != nil {
		return nil, err
	}
	rows, err := m.Store.List("skill-check-job")
	if err != nil {
		return nil, err
	}
	out := []SkillCheckJob{}
	for _, row := range rows {
		var v SkillCheckJob
		if err := json.Unmarshal(row, &v); err != nil {
			return nil, err
		}
		if v.PackID == packID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (m *Manager) SkillCheckJob(id string) (SkillCheckJob, error) {
	var job SkillCheckJob
	if !skillCenterID.MatchString(id) {
		return job, failure(404, "skill_check_not_found", "测试任务不存在")
	}
	m.skillCheckMu.Lock()
	defer m.skillCheckMu.Unlock()
	if err := m.initSkillChecksLocked(); err != nil {
		return job, err
	}
	err := m.Store.Get("skill-check-job", id, &job)
	if errors.Is(err, sql.ErrNoRows) {
		err = failure(404, "skill_check_not_found", "测试任务不存在")
	}
	return job, err
}

type SkillCheckRequest struct {
	Digest               string `json:"digest"`
	Action               string `json:"action"`
	InstanceID           string `json:"instanceId"`
	AllowNetwork         bool   `json:"allowNetwork"`
	AcknowledgeExecution bool   `json:"acknowledgeExecution"`
}

func (m *Manager) StartSkillCheck(packID string, in SkillCheckRequest) (SkillCheckJob, error) {
	if in.Action != "validate" && in.Action != "run" {
		return SkillCheckJob{}, failure(400, "invalid_skill_check_action", "请选择校验用例或执行测试")
	}
	if !in.AcknowledgeExecution {
		return SkillCheckJob{}, failure(409, "skill_check_execution_confirmation_required", "请明确确认将在独立容器中读取用例或执行 Agent、工具和脚本")
	}
	pack, files, err := m.skillPackAssets(packID)
	if err != nil {
		return SkillCheckJob{}, err
	}
	if in.Digest != pack.Digest {
		return SkillCheckJob{}, failure(409, "skill_digest_mismatch", "请确认当前技能内容摘要后测试")
	}
	found := false
	for _, file := range files {
		found = found || file.Path == "evals/eval.yaml"
	}
	if !found {
		return SkillCheckJob{}, failure(400, "skill_check_suite_missing", "该快照没有 evals/eval.yaml，请获取包含用例的完整技能目录")
	}
	if runtime.GOOS != "linux" {
		return SkillCheckJob{}, failure(503, "skill_check_unavailable", "隔离测试目前需要运行 RunDesk 的 Linux 主机及本机 Docker Engine")
	}
	instance, err := m.Instance(in.InstanceID)
	if err != nil {
		return SkillCheckJob{}, err
	}
	spec := instance.Execution.normalized()
	if !instance.Managed || spec.Mode != "docker" || spec.Image == "" {
		return SkillCheckJob{}, failure(400, "skill_check_environment_required", "请选择已有的受管 Docker 制作环境；不会在宿主直接执行测试")
	}
	ref := spec.Image
	if spec.ImageID != "" {
		ref = spec.ImageID
	}
	image, err := m.inspectImage(ref)
	if err != nil {
		return SkillCheckJob{}, &apiError{Status: 503, Code: "skill_check_unavailable", Message: "无法确认本机测试镜像，未启动测试：" + diagnosticText(err.Error()), Cause: err}
	}
	if !skillCheckImageID.MatchString(image.ID) {
		return SkillCheckJob{}, failure(502, "skill_check_image_identity", "本机镜像未返回有效的不可变身份")
	}
	m.skillCheckMu.Lock()
	defer m.skillCheckMu.Unlock()
	if err := m.initSkillChecksLocked(); err != nil {
		return SkillCheckJob{}, err
	}
	for id := range m.skillChecks.cancels {
		var active SkillCheckJob
		if m.Store.Get("skill-check-job", id, &active) == nil && active.PackID == packID {
			return SkillCheckJob{}, failure(409, "skill_check_running", "该快照已有测试正在运行，请查看原任务")
		}
	}
	if len(m.skillChecks.cancels) > 0 {
		return SkillCheckJob{}, failure(429, "skill_check_busy", "当前已有一个隔离测试任务，请稍后重试")
	}
	rows, err := m.Store.List("skill-check-job")
	if err != nil {
		return SkillCheckJob{}, err
	}
	historyCount := 0
	for _, row := range rows {
		var previous SkillCheckJob
		if err := json.Unmarshal(row, &previous); err != nil {
			return SkillCheckJob{}, err
		}
		if !isSkillDiagnosticJob(previous) {
			historyCount++
		}
	}
	if historyCount >= 1000 {
		return SkillCheckJob{}, failure(409, "skill_check_history_full", "测试记录已达上限，请先整理历史记录")
	}
	id := store.ID()
	inputs := []skillAsset{}
	for _, file := range files {
		if file.Path != "SKILL.md" {
			inputs = append(inputs, file)
		}
	}
	// The CLI result does not identify every script Judge implementation. In
	// this MVP only a root SKILL.md revision may differ across comparable runs;
	// all tests, scripts, references and assets must retain their identities.
	suiteDigest, err := skillsafety.Digest(safetyFiles(inputs))
	if err != nil {
		return SkillCheckJob{}, err
	}
	job := SkillCheckJob{ID: id, PackID: packID, Digest: pack.Digest, SuiteDigest: suiteDigest, Action: in.Action, InstanceID: instance.ID, EnvironmentName: instance.Name, ImageID: image.ID, ContainerName: "rundesk-skillcheck-" + id, AllowNetwork: in.AllowNetwork, Status: "queued", CreatedAt: store.Now(), ExitCode: -1}
	if err := m.Store.Put("skill-check-job", id, job); err != nil {
		return job, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	m.skillChecks.cancels[id] = cancel
	m.wg.Add(1)
	go m.runSkillCheck(ctx, job, files)
	return job, nil
}

func (m *Manager) runSkillCheck(ctx context.Context, job SkillCheckJob, files []skillAsset) {
	defer m.wg.Done()
	defer func() {
		m.skillCheckMu.Lock()
		if cancel := m.skillChecks.cancels[job.ID]; cancel != nil {
			cancel()
			var latest SkillCheckJob
			if m.Store.Get("skill-check-job", job.ID, &latest) != nil || (!latest.CleanupPending && latest.CleanupError == "") {
				delete(m.skillChecks.cancels, job.ID)
			}
		}
		m.skillCheckMu.Unlock()
	}()
	m.skillCheckMu.Lock()
	var before SkillCheckJob
	err := m.Store.Get("skill-check-job", job.ID, &before)
	if err == nil && before.Status == "canceled" {
		before.CleanupPending = false // still queued: no container was created
		_ = m.Store.Put("skill-check-job", job.ID, before)
		m.skillCheckMu.Unlock()
		return
	}
	job.Status, job.StartedAt = "running", store.Now()
	job.CleanupPending = true
	if err == nil {
		err = m.Store.Put("skill-check-job", job.ID, job)
	}
	m.skillCheckMu.Unlock()
	execution := SkillCheckExecution{ExitCode: -1}
	if err == nil {
		parent := filepath.Join(m.Data, "skill-center", "check-work")
		err = os.MkdirAll(parent, 0700)
		if err == nil {
			var temp string
			temp, err = os.MkdirTemp(parent, job.ID+"-")
			if err == nil {
				defer os.RemoveAll(temp)
				source := filepath.Join(temp, "source")
				err = os.Mkdir(source, 0755)
				for _, f := range files {
					if err != nil {
						break
					}
					name := filepath.Join(source, filepath.FromSlash(f.Path))
					err = os.MkdirAll(filepath.Dir(name), 0755)
					mode := fs.FileMode(0444)
					if f.Mode&0111 != 0 {
						mode = 0555
					}
					if err == nil {
						err = os.WriteFile(name, f.Data, mode)
					}
				}
				if err == nil {
					execution, err = m.executeSkillCheck(ctx, job, source)
				}
			}
		}
	}
	var parsed *skillcheck.Report
	if ctx.Err() == nil && job.Action == "run" && len(execution.Report) > 0 {
		value, parseErr := skillcheck.ParseReport(execution.Report)
		if parseErr == nil {
			value.SuiteDigest = job.SuiteDigest
			if (execution.ExitCode != 0 || err != nil) && value.Status == "pass" {
				value.Status = "incomplete"
				value.Warnings = append(value.Warnings, "CLI、连接或容器清理未完整成功，不能把保留的 PASS 项作为完整通过")
			}
			parsed = &value
		} else if err == nil {
			err = parseErr
		}
	}
	m.skillCheckMu.Lock()
	defer m.skillCheckMu.Unlock()
	var current SkillCheckJob
	_ = m.Store.Get("skill-check-job", job.ID, &current)
	job.FinishedAt, job.ExitCode, job.Output = store.Now(), execution.ExitCode, skillsafety.Redact(execution.Output)
	if len(job.Output) > 64<<10 {
		job.Output = job.Output[:64<<10]
	}
	if job.Action == "validate" && ctx.Err() == nil {
		value, _ := skillcheck.ParseValidationOutput([]byte(execution.Output), execution.ExitCode)
		job.Validation = &value
	}
	switch {
	case current.Status == "canceled" || errors.Is(ctx.Err(), context.Canceled):
		job.Status, job.Error = "canceled", "测试已取消"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		job.Status, job.Error = "failed", "隔离测试超时"
	case parsed != nil:
		job.Status = "completed"
		record := SkillCheckReportRecord{ID: store.ID(), PackID: job.PackID, Digest: job.Digest, SuiteDigest: job.SuiteDigest, Source: "docker_run", JobID: job.ID, InstanceID: job.InstanceID, ImageID: job.ImageID, CreatedAt: store.Now(), Report: *parsed}
		if saveErr := m.saveSkillCheckReportLocked(record); saveErr != nil {
			job.Status, job.Error = "failed", "测试报告保存失败："+diagnosticText(saveErr.Error())
		} else {
			job.ReportID = record.ID
			if err != nil {
				job.Error = "执行未完整成功，保留报告供复核：" + diagnosticText(err.Error())
			}
		}
	case err != nil:
		job.Status, job.Error = "failed", diagnosticText(err.Error())
	case job.Action == "validate" && job.Validation != nil && job.Validation.Status == "success":
		job.Status = "completed"
	default:
		job.Status, job.Error = "failed", "未获得有效测试报告，不能视为通过"
	}
	job.CleanupError = execution.CleanupError
	job.CleanupPending = execution.CleanupError != ""
	if job.CleanupError != "" {
		job.Error += "；容器清理未确认，请重试取消以完成清理"
		if job.Status == "completed" {
			job.Status = "failed"
		}
	}
	if cancel := m.skillChecks.cancels[job.ID]; cancel != nil {
		cancel()
		if job.CleanupError == "" {
			delete(m.skillChecks.cancels, job.ID)
		}
	}
	_ = m.Store.Put("skill-check-job", job.ID, job)
}

func (m *Manager) CancelSkillCheck(id string) (SkillCheckJob, error) {
	var job SkillCheckJob
	if !skillCenterID.MatchString(id) {
		return job, failure(404, "skill_check_not_found", "测试任务不存在")
	}
	m.skillCheckMu.Lock()
	defer m.skillCheckMu.Unlock()
	if err := m.initSkillChecksLocked(); err != nil {
		return job, err
	}
	if err := m.Store.Get("skill-check-job", id, &job); err != nil {
		return job, failure(404, "skill_check_not_found", "测试任务不存在")
	}
	if job.Status == "queued" || job.Status == "running" {
		job.Status, job.FinishedAt, job.Error = "canceled", store.Now(), "测试已取消，正在回收独立容器"
		job.CleanupPending = true
		if err := m.Store.Put("skill-check-job", id, job); err != nil {
			return job, err
		}
		if cancel := m.skillChecks.cancels[id]; cancel != nil {
			cancel()
		}
	} else if job.CleanupError != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := m.cleanupSkillCheck(ctx, job)
		cancel()
		if err != nil {
			job.CleanupError = diagnosticText(err.Error())
		} else {
			job.CleanupError, job.Error = "", "任务已结束，独立容器清理已确认"
			job.CleanupPending = false
			delete(m.skillChecks.cancels, job.ID)
		}
		if err := m.Store.Put("skill-check-job", id, job); err != nil {
			return job, err
		}
	}
	return job, nil
}

func (m *Manager) saveSkillCheckReportLocked(record SkillCheckReportRecord) error {
	rows, err := m.Store.List("skill-check-report-index")
	if err != nil {
		return err
	}
	if len(rows) >= skillCheckRecordLimit {
		return failure(409, "skill_check_reports_full", "已保存 200 份报告，请先整理历史记录")
	}
	if err := m.Store.Put("skill-check-report", record.ID, record); err != nil {
		return err
	}
	summary := record
	summary.CasesOmitted = true
	summary.Report.Cases = []skillcheck.Case{}
	if err := m.Store.Put("skill-check-report-index", record.ID, summary); err != nil {
		_ = m.Store.DeleteObject("skill-check-report", record.ID)
		return err
	}
	return nil
}

func (m *Manager) ImportSkillCheckReport(packID, digest string, data []byte, acknowledge bool) (SkillCheckReportRecord, error) {
	if !acknowledge {
		return SkillCheckReportRecord{}, failure(409, "skill_check_import_confirmation_required", "请确认外部报告来源未经本次执行验证；它不会作为安全检查通过依据")
	}
	pack, _, err := m.skillPackAssets(packID)
	if err != nil {
		return SkillCheckReportRecord{}, err
	}
	if digest != pack.Digest {
		return SkillCheckReportRecord{}, failure(409, "skill_digest_mismatch", "导入报告的目标快照摘要不一致")
	}
	report, err := skillcheck.ParseReport(data)
	if err != nil {
		return SkillCheckReportRecord{}, failure(400, "skill_check_report_invalid", err.Error())
	}
	report.Warnings = append(report.Warnings, "外部导入结果仅按用户声明关联此快照；未验证原始执行环境、输入或技能内容")
	record := SkillCheckReportRecord{ID: store.ID(), PackID: packID, Digest: digest, Source: "external_unverified", CreatedAt: store.Now(), Report: report}
	m.skillCheckMu.Lock()
	defer m.skillCheckMu.Unlock()
	if err := m.initSkillChecksLocked(); err != nil {
		return record, err
	}
	return record, m.saveSkillCheckReportLocked(record)
}

func (m *Manager) SkillCheckReports(packID string) ([]SkillCheckReportRecord, error) {
	if packID != "" {
		if _, err := m.SkillPack(packID); err != nil {
			return nil, err
		}
	}
	rows, err := m.Store.List("skill-check-report-index")
	if err != nil {
		return nil, err
	}
	items := []SkillCheckReportRecord{}
	for _, row := range rows {
		var record SkillCheckReportRecord
		if err := json.Unmarshal(row, &record); err != nil {
			return nil, err
		}
		if packID == "" || record.PackID == packID {
			items = append(items, record)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

func (m *Manager) SkillCheckReport(id string) (SkillCheckReportRecord, error) {
	var record SkillCheckReportRecord
	if !skillCenterID.MatchString(id) {
		return record, failure(404, "skill_check_report_not_found", "测试报告不存在")
	}
	err := m.Store.Get("skill-check-report", id, &record)
	if errors.Is(err, sql.ErrNoRows) {
		err = failure(404, "skill_check_report_not_found", "测试报告不存在")
	}
	return record, err
}

func (m *Manager) CompareSkillChecks(packID, baselineID, candidateID string) (any, error) {
	pack, err := m.SkillPack(packID)
	if err != nil {
		return nil, err
	}
	before, err := m.SkillCheckReport(baselineID)
	if err != nil {
		return nil, err
	}
	after, err := m.SkillCheckReport(candidateID)
	if err != nil {
		return nil, err
	}
	if after.PackID != packID || after.Digest != pack.Digest {
		return nil, failure(403, "skill_check_scope_mismatch", "候选报告必须属于当前技能快照")
	}
	comparison := skillcheck.Compare(before.Report, after.Report)
	unknown := func(reason string) {
		comparison.Comparable, comparison.Verdict, comparison.Reason = false, "incomplete", reason
		comparison.Summary = skillcheck.ChangeSummary{Unknown: len(comparison.Cases)}
		for index := range comparison.Cases {
			comparison.Cases[index].Change = "unknown"
			comparison.Cases[index].Reason = reason
		}
	}
	if before.Source == "external_unverified" || after.Source == "external_unverified" {
		comparison.Verdict = "incomplete"
		comparison.Warnings = append(comparison.Warnings, "对比包含外部导入报告，仅比较声明结果，不验证其原始执行来源")
	}
	if before.SuiteDigest == "" || after.SuiteDigest == "" {
		comparison.Verdict = "incomplete"
		comparison.Warnings = append(comparison.Warnings, "缺少独立执行时冻结的用例及规则摘要，不能作为回归通过门禁")
	} else if before.SuiteDigest != after.SuiteDigest {
		unknown("用例或辅助文件已经变化，无法将差异归因于根 SKILL.md 修改")
	}
	if before.ImageID != after.ImageID {
		unknown("运行镜像已经变化，不能把差异归因于技能修改")
	}
	for _, report := range []skillcheck.Report{before.Report, after.Report} {
		for _, item := range report.Cases {
			for _, assertion := range item.Assertions {
				// Source carries a user-defined judge ID, not a verified type.
				// An ID ending in ':rule' cannot establish a built-in evaluator.
				if strings.HasPrefix(assertion.Source, "judge:") {
					unknown("脚本、Agent 或未知 Judge 的有效实现身份未得到独立验证；仅展示声明结果")
				}
			}
		}
	}
	comparison.Warnings = append(comparison.Warnings, "自动可比范围只允许根 SKILL.md 变化，其他文件均冻结；脚本、Agent 与未知 Judge 不作为自动回归通过依据，远程模型及业务效果另需验证")
	return map[string]any{"baselineId": baselineID, "candidateId": candidateID, "comparison": comparison}, nil
}

func (m *Manager) SkillCheckEngines() any {
	items := []map[string]any{}
	available, message := false, "请先准备包含 Skill-Up 与所需 Agent 的受管 Docker 制作环境"
	if runtime.GOOS != "linux" {
		message = "隔离测试目前需要运行 RunDesk 的 Linux 主机和本机 Docker Engine"
	} else if _, err := m.DockerStatus(); err != nil {
		message = "Docker 当前不可用；仍可导入和比较外部报告"
	} else {
		available = true
		message = "Docker 已连接；工具、模型与用例可用性以任务实际结果为准"
	}
	for _, instance := range m.Instances() {
		spec := instance.Execution.normalized()
		if instance.Managed && spec.Mode == "docker" && spec.Image != "" {
			items = append(items, map[string]any{"instanceId": instance.ID, "name": instance.Name, "image": spec.Image, "available": available, "message": message})
		}
	}
	if len(items) == 0 {
		available = false
		message = "没有已配置的受管 Docker 制作环境；仍可导入和比较外部报告"
	}
	return map[string]any{"available": available, "message": message, "items": items}
}

func (s *Server) skillCheckRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if principal(r) != nil || human(r) != nil {
				respond(w, nil, failure(403, "skill_center_admin_required", "效果测试由管理员配置"))
				return
			}
			handler(w, r)
		})
	}
	register("GET /api/skill-center/check-engines", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Manager.SkillCheckEngines()) })
	register("GET /api/skill-center/packs/{id}/checks", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.SkillCheckJobs(r.PathValue("id"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
	register("POST /api/skill-center/packs/{id}/checks", func(w http.ResponseWriter, r *http.Request) {
		var in SkillCheckRequest
		if !decode(w, r, &in) {
			return
		}
		job, err := s.Manager.StartSkillCheck(r.PathValue("id"), in)
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 202, map[string]any{"job": job})
	})
	register("GET /api/skill-center/check-jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		job, err := s.Manager.SkillCheckJob(r.PathValue("id"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, job)
	})
	register("POST /api/skill-center/check-jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		job, err := s.Manager.CancelSkillCheck(r.PathValue("id"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, job)
	})
	register("GET /api/skill-center/packs/{id}/check-reports", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.SkillCheckReports(r.PathValue("id"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
	register("POST /api/skill-center/packs/{id}/check-reports", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Digest            string          `json:"digest"`
			Report            json.RawMessage `json:"report"`
			AcknowledgeImport bool            `json:"acknowledgeImport"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, skillcheck.MaxReportBytes+16384)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			respond(w, nil, failure(400, "invalid_skill_check_report", "报告请求无效或超限"))
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			respond(w, nil, failure(400, "invalid_skill_check_report", "只接受单个 JSON 对象"))
			return
		}
		record, err := s.Manager.ImportSkillCheckReport(r.PathValue("id"), in.Digest, in.Report, in.AcknowledgeImport)
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, record)
	})
	register("GET /api/skill-center/check-reports", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.SkillCheckReports("")
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
	register("GET /api/skill-center/check-reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		record, err := s.Manager.SkillCheckReport(r.PathValue("id"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, record)
	})
	register("GET /api/skill-center/packs/{id}/check-compare", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.Manager.CompareSkillChecks(r.PathValue("id"), r.URL.Query().Get("baseline"), r.URL.Query().Get("candidate"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, value)
	})
}
