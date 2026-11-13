package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/skillcheck"
	"github.com/shengjuntu/rundesk/internal/skillsafety"
	"github.com/shengjuntu/rundesk/internal/store"
)

type SkillDiagnosticCheck struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Version    string `json:"version,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

type SkillDiagnostic struct {
	ID           string                 `json:"id"`
	Target       string                 `json:"target"`
	InstanceID   string                 `json:"instanceId,omitempty"`
	ImageID      string                 `json:"imageId,omitempty"`
	Overall      string                 `json:"overall"`
	CheckedAt    string                 `json:"checkedAt"`
	Checks       []SkillDiagnosticCheck `json:"checks"`
	CleanupJobID string                 `json:"cleanupJobId,omitempty"`
}

type SkillDiagnosticRequest struct {
	Target     string `json:"target"`
	InstanceID string `json:"instanceId,omitempty"`
}

const skillDiagnosticEnvironmentName = "Skill-Up 工具诊断"

var spectorDiagnosticVersion = regexp.MustCompile(`(?i)^SkillSpector\s+v?([0-9]+\.[0-9]+\.[0-9]+)$`)
var skillUpDiagnosticVersion = regexp.MustCompile(`(?i)^skill-up\s+version\s+v?([0-9]+\.[0-9]+\.[0-9]+)$`)

func skillDiagnosticOverall(checks []SkillDiagnosticCheck) string {
	overall := "ready"
	required := map[string]bool{"version": false, "protocol": false}
	for _, c := range checks {
		if _, exists := required[c.ID]; exists && c.Status == "ok" {
			required[c.ID] = true
		}
		switch c.Status {
		case "failed":
			return "failed"
		case "incompatible":
			overall = "incompatible"
		case "missing":
			if overall != "incompatible" {
				overall = "unavailable"
			}
		case "unknown":
			if overall == "ready" {
				overall = "incomplete"
			}
		}
	}
	if overall == "ready" && (!required["version"] || !required["protocol"]) {
		return "incomplete"
	}
	return overall
}

func diagnosticKey(target, instance string) string {
	sum := sha256.Sum256([]byte(target + "\x00" + instance))
	return hex.EncodeToString(sum[:])
}

// A canary's temporary recovery record is not ordinary user test history.
// Require every identifying marker before excluding or retiring one.
func isSkillDiagnosticJob(job SkillCheckJob) bool {
	return job.PackID == "" && job.Digest == "" && job.SuiteDigest == "" && job.ReportID == "" &&
		job.Action == "validate" && job.EnvironmentName == skillDiagnosticEnvironmentName &&
		skillCenterID.MatchString(job.ID) && job.ContainerName == "rundesk-skillcheck-"+job.ID
}

func confirmSkillDiagnosticCleanup(result *SkillDiagnostic) {
	result.CleanupJobID = ""
	for i := range result.Checks {
		if result.Checks[i].ID == "cleanup" {
			result.Checks[i].Status, result.Checks[i].Message = "ok", "独立诊断容器的清理已确认；其他检查仍以原时间结果为准"
		}
	}
	result.Overall = skillDiagnosticOverall(result.Checks)
}

// Called under skillCheckMu after recovery and only when no execution owns the
// slot. Retain every unresolved record; successful retry/restart recovery can
// leave a terminal canary record which the next explicit diagnosis retires.
func (m *Manager) retireSkillDiagnosticJobsLocked() error {
	rows, err := m.Store.List("skill-check-job")
	if err != nil {
		return err
	}
	for _, row := range rows {
		var job SkillCheckJob
		if err := json.Unmarshal(row, &job); err != nil {
			return err
		}
		if !isSkillDiagnosticJob(job) || job.Status == "queued" || job.Status == "running" || job.CleanupPending || job.CleanupError != "" {
			continue
		}
		// Preserve durable cleanup evidence before removing its temporary job.
		var latest SkillDiagnostic
		key := diagnosticKey("skillup", job.InstanceID)
		if err := m.Store.Get("skill-diagnostic", key, &latest); err == nil {
			if latest.CleanupJobID == job.ID {
				confirmSkillDiagnosticCleanup(&latest)
				if err := m.Store.Put("skill-diagnostic", key, latest); err != nil {
					return err
				}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := m.Store.Delete("skill-check-job", job.ID); err != nil {
			return err
		}
	}
	return nil
}

// SkillDiagnostics reads only persisted evidence. It never probes a command or
// touches Docker, including when an older result has unresolved cleanup.
func (m *Manager) SkillDiagnostics() ([]SkillDiagnostic, error) {
	rows, err := m.Store.List("skill-diagnostic")
	if err != nil {
		return nil, err
	}
	items := []SkillDiagnostic{}
	for _, row := range rows {
		var result SkillDiagnostic
		if err := json.Unmarshal(row, &result); err != nil {
			return nil, err
		}
		if result.CleanupJobID != "" {
			var job SkillCheckJob
			if m.Store.Get("skill-check-job", result.CleanupJobID, &job) == nil && !job.CleanupPending && job.CleanupError == "" {
				confirmSkillDiagnosticCleanup(&result)
			}
		}
		items = append(items, result)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CheckedAt > items[j].CheckedAt })
	return items, nil
}

// DiagnoseSkillTools is an explicit, bounded trusted-canary operation. It shares
// effect-check admission and Close's wg barrier; there is never a host fallback
// for Skill-Up. Its ordinary execution deadline is 24s with at most 5s of final
// independent container cleanup, keeping subprocess work under 30 seconds.
func (m *Manager) DiagnoseSkillTools(parent context.Context, in SkillDiagnosticRequest) (result SkillDiagnostic, err error) {
	if in.Target != "skillspector" && in.Target != "skillup" {
		return result, failure(400, "invalid_skill_diagnostic_target", "请选择 SkillSpector 或 Skill-Up 诊断目标")
	}
	if in.Target == "skillspector" && in.InstanceID != "" {
		return result, failure(400, "invalid_skill_diagnostic_instance", "主机静态扫描诊断不接受测试环境参数")
	}
	var diagnosticInstance Instance
	if in.Target == "skillup" {
		in.InstanceID = instanceID([]string{in.InstanceID})
		if !slug.MatchString(in.InstanceID) {
			return result, failure(400, "invalid_skill_diagnostic_instance", "测试环境 ID 必须为不超过 64 字节的字母、数字、下划线或短横线")
		}
		diagnosticInstance, err = m.Instance(in.InstanceID)
		if err != nil {
			return result, err
		}
		in.InstanceID = diagnosticInstance.ID
	}
	ctx, cancel := context.WithTimeout(parent, 24*time.Second)
	defer cancel()
	m.skillCheckMu.Lock()
	if err = m.initSkillChecksLocked(); err != nil {
		m.skillCheckMu.Unlock()
		return result, err
	}
	if len(m.skillChecks.cancels) > 0 {
		m.skillCheckMu.Unlock()
		return result, failure(429, "skill_diagnostic_busy", "已有测试、诊断或未完成的容器清理，请先处理现有任务")
	}
	if err = m.retireSkillDiagnosticJobsLocked(); err != nil {
		m.skillCheckMu.Unlock()
		return result, err
	}
	result = SkillDiagnostic{ID: store.ID(), Target: in.Target, InstanceID: in.InstanceID, Checks: []SkillDiagnosticCheck{}}
	stopManagerCancel := context.AfterFunc(m.ctx, cancel)
	defer stopManagerCancel()
	m.skillChecks.cancels[result.ID] = cancel
	m.wg.Add(1)
	m.skillCheckMu.Unlock()
	defer m.wg.Done()
	var internalJob *SkillCheckJob
	defer func() {
		result.CheckedAt = store.Now()
		if ctx.Err() != nil {
			result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "deadline", Label: "诊断完成状态", Status: "failed", Message: "诊断已取消或超时，未完成项目不能视为通过"})
		}
		result.Overall = skillDiagnosticOverall(result.Checks)
		m.skillCheckMu.Lock()
		defer m.skillCheckMu.Unlock()
		if internalJob != nil {
			internalJob.FinishedAt = result.CheckedAt
			internalJob.Status = "completed"
			if result.Overall != "ready" && result.Overall != "incomplete" {
				internalJob.Status = "failed"
			}
			if ctx.Err() != nil {
				internalJob.Status = "canceled"
			}
			internalJob.CleanupPending = result.CleanupJobID != ""
			if putErr := m.Store.Put("skill-check-job", internalJob.ID, *internalJob); putErr != nil && err == nil {
				err = putErr
			}
			if result.CleanupJobID == "" && err == nil {
				// Cleanup has been confirmed: only the latest diagnostic evidence
				// needs to survive, not a synthetic ordinary effect-test history row.
				if deleteErr := m.Store.Delete("skill-check-job", internalJob.ID); deleteErr != nil {
					err = deleteErr
				}
			}
		}
		if err != nil {
			result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "storage", Label: "诊断记录完整性", Status: "failed", Message: "诊断记录未能完整保存，不能视为检查通过"})
			result.Overall = skillDiagnosticOverall(result.Checks)
		}
		if result.CleanupJobID == "" {
			delete(m.skillChecks.cancels, result.ID)
		} else {
			m.skillChecks.cancels[result.ID] = func() {}
		}
		if putErr := m.Store.Put("skill-diagnostic", diagnosticKey(result.Target, result.InstanceID), result); putErr != nil && err == nil {
			err = putErr
		}
	}()
	if in.Target == "skillspector" {
		m.diagnoseSkillSpector(ctx, &result)
		return result, nil
	}
	if runtime.GOOS != "linux" {
		result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "docker", Label: "隔离运行环境", Status: "missing", Message: "Skill-Up 诊断需要运行 RunDesk 的 Linux 主机及本机 Docker Engine"})
		appendSkillModelUnknown(&result)
		return result, nil
	}
	endpoint := os.Getenv("RUNDESK_DOCKER_HOST")
	if endpoint != "" && (!strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\r\n\x00")) {
		result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "docker", Label: "隔离运行环境", Status: "incompatible", Message: "诊断只允许本机 Docker Unix socket"})
		appendSkillModelUnknown(&result)
		return result, nil
	}
	i := diagnosticInstance
	if !i.Managed || i.Execution.normalized().Mode != "docker" {
		result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "environment", Label: "测试环境配置", Status: "missing", Message: "请选择已有的受管 Docker 测试环境"})
		appendSkillModelUnknown(&result)
		return result, nil
	}
	result.InstanceID = i.ID
	image, imageErr := m.diagnosticImage(ctx, i)
	if imageErr != nil {
		result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "docker", Label: "本机 Docker 与镜像", Status: "missing", Message: "无法核对本机测试镜像；请检查 Docker 服务、权限、镜像平台和挂载声明"})
		appendSkillModelUnknown(&result)
		return result, nil
	}
	result.ImageID = image.ID
	result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "docker", Label: "本机 Docker 与镜像", Status: "ok", Message: "已核对本机不可变镜像身份；尚未代表工具或模型可运行"})
	source, sourceErr := makeSkillDiagnosticCanary()
	if sourceErr != nil {
		result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "canary", Label: "受控协议样本", Status: "failed", Message: "无法准备可信诊断样本"})
		appendSkillModelUnknown(&result)
		return result, nil
	}
	defer os.RemoveAll(filepath.Dir(source))
	job := SkillCheckJob{ID: result.ID, Action: "validate", InstanceID: i.ID, EnvironmentName: skillDiagnosticEnvironmentName, ImageID: image.ID, ContainerName: "rundesk-skillcheck-" + result.ID, Status: "running", CreatedAt: store.Now(), StartedAt: store.Now(), ExitCode: -1, CleanupPending: true}
	internalJob = &job
	m.skillCheckMu.Lock()
	putErr := m.Store.Put("skill-check-job", job.ID, job)
	m.skillCheckMu.Unlock()
	if putErr != nil {
		return result, putErr
	}
	run := func(action string) (SkillCheckExecution, error) {
		if ctx.Err() != nil {
			return SkillCheckExecution{ExitCode: -1}, ctx.Err()
		}
		probeJob := job
		probeJob.Action = action
		execution, execErr := m.executeSkillCheckWithCleanup(ctx, probeJob, source, 5*time.Second)
		job.ExitCode = execution.ExitCode
		if execution.CleanupError != "" {
			job.CleanupError = "诊断容器清理未确认，请重试回收"
			result.CleanupJobID = job.ID
			result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "cleanup", Label: "独立容器清理", Status: "failed", Message: "清理未确认，已阻断新测试；请重试回收此诊断任务"})
		}
		return execution, execErr
	}
	began := time.Now()
	versionRun, versionErr := run("version")
	versionCheck := SkillDiagnosticCheck{ID: "version", Label: "Skill-Up 版本", DurationMS: time.Since(began).Milliseconds()}
	match := skillUpDiagnosticVersion.FindStringSubmatch(strings.TrimSpace(versionRun.Output))
	switch {
	case versionErr != nil || versionRun.ExitCode != 0:
		versionCheck.Status, versionCheck.Message = "failed", "容器内 Skill-Up 版本命令未完整成功；请检查安装、执行权限及依赖"
		if versionRun.ExitCode == 127 {
			versionCheck.Status, versionCheck.Message = "missing", "容器内未找到可运行的 Skill-Up 工具"
		}
	case len(match) != 2:
		versionCheck.Status, versionCheck.Message = "incompatible", "Skill-Up 未返回可识别的版本协议"
	case !strings.HasPrefix(match[1], "0.15."):
		versionCheck.Status, versionCheck.Message, versionCheck.Version = "incompatible", "此版本不在已验证的 Skill-Up 0.15.x 兼容范围", match[1]
	default:
		versionCheck.Status, versionCheck.Message, versionCheck.Version = "ok", "版本位于已验证的 Skill-Up 0.15.x 协议范围", match[1]
	}
	result.Checks = append(result.Checks, versionCheck)
	if versionCheck.Status != "ok" || result.CleanupJobID != "" {
		appendSkillModelUnknown(&result)
		return result, nil
	}
	began = time.Now()
	validated, validationErr := run("validate")
	validation, parseErr := skillcheck.ParseValidationOutput([]byte(validated.Output), validated.ExitCode)
	protocol := SkillDiagnosticCheck{ID: "protocol", Label: "可信用例校验协议", DurationMS: time.Since(began).Milliseconds()}
	if validationErr != nil || parseErr != nil || validation.Status != "success" || validation.Cases != 1 {
		protocol.Status, protocol.Message = "failed", "受控单用例校验未完成；请检查 Skill-Up 版本、依赖及只读样本访问权限"
	} else {
		protocol.Status, protocol.Message = "ok", "可信单用例校验协议通过；未启动 Agent、Judge 脚本或模型调用"
	}
	result.Checks = append(result.Checks, protocol)
	appendSkillModelUnknown(&result)
	return result, nil
}

func appendSkillModelUnknown(result *SkillDiagnostic) {
	result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "model", Label: "模型与认证", Status: "unknown", Message: "未验证模型连接、认证或真实 Agent 效果；需要单独执行明确授权的效果测试"}, SkillDiagnosticCheck{ID: "network", Label: "测试网络", Status: "unknown", Message: "诊断容器固定断网；未发送模型或外部服务请求"})
}

func (m *Manager) diagnoseSkillSpector(ctx context.Context, result *SkillDiagnostic) {
	binary := os.Getenv("RUNDESK_SKILLSPECTOR_BIN")
	began := time.Now()
	banner, err := skillsafety.ProbeVersion(ctx, binary)
	version := SkillDiagnosticCheck{ID: "version", Label: "SkillSpector 版本", DurationMS: time.Since(began).Milliseconds()}
	match := spectorDiagnosticVersion.FindStringSubmatch(banner)
	switch {
	case errors.Is(err, skillsafety.ErrVersionUnavailable):
		version.Status, version.Message = "missing", "主机未找到已配置的 SkillSpector；请安装工具并配置服务进程环境"
	case err != nil:
		version.Status, version.Message = "failed", "SkillSpector 版本命令未完整成功；请检查可执行程序及依赖"
	case len(match) != 2:
		version.Status, version.Message = "incompatible", "SkillSpector 未返回可识别的版本协议"
	case !strings.HasPrefix(match[1], "2.13."):
		version.Status, version.Message, version.Version = "incompatible", "此版本不在已验证的 SkillSpector 2.13.x 兼容范围", match[1]
	default:
		version.Status, version.Message, version.Version = "ok", "版本位于已验证的 SkillSpector 2.13.x 协议范围", match[1]
	}
	result.Checks = append(result.Checks, version)
	if version.Status == "ok" {
		source, err := makeSkillDiagnosticCanary()
		if err != nil {
			result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "protocol", Label: "可信静态扫描协议", Status: "failed", Message: "无法准备可信诊断样本"})
			return
		}
		// The scanner gets only our minimal markdown, not Skill-Up YAML or source.
		_ = os.RemoveAll(filepath.Join(source, "evals"))
		defer os.RemoveAll(filepath.Dir(source))
		began = time.Now()
		report, scanErr := skillsafety.RunSkillSpector(ctx, source, binary, false)
		check := SkillDiagnosticCheck{ID: "protocol", Label: "可信静态扫描协议", DurationMS: time.Since(began).Milliseconds()}
		if scanErr != nil || report.Status != "completed" || report.Recommendation != "no_findings" || report.Coverage.FilesSeen != 1 || report.Coverage.FilesScanned != 1 || report.EngineVersion != version.Version {
			check.Status, check.Message = "failed", "可信单文件静态协议未完整通过；请检查依赖、版本一致性和扫描器覆盖报告"
		} else {
			check.Status, check.Message = "ok", "可信单文件静态协议通过；这不是用户 Skill 的扫描结果或安全证书"
		}
		result.Checks = append(result.Checks, check)
	}
	result.Checks = append(result.Checks, SkillDiagnosticCheck{ID: "llm", Label: "模型辅助扫描", Status: "skipped", Message: "本次固定关闭 LLM；未验证模型配置、认证或语义层效果"})
}

func makeSkillDiagnosticCanary() (string, error) {
	parent, err := os.MkdirTemp("", "rundesk-tool-canary-")
	if err != nil {
		return "", err
	}
	source := filepath.Join(parent, "source")
	if err = os.MkdirAll(filepath.Join(source, "evals", "cases"), 0755); err != nil {
		_ = os.RemoveAll(parent)
		return "", err
	}
	files := map[string]string{
		"SKILL.md":             "---\nname: rundesk-tool-canary\ndescription: Summarize user-provided documents with source references.\n---\nSummarize only the text supplied by the user and cite source references.\n",
		"evals/eval.yaml":      "schema_version: v1alpha1\nenvironment:\n  type: none\nengine:\n  name: codex\ncases:\n  files:\n    - evals/cases/one.yaml\n  defaults:\n    timeout_seconds: 5\n    max_turns: 1\n  parallelism: 1\nreport:\n  formats: [json]\n",
		"evals/cases/one.yaml": "id: diagnostic-canary\ntitle: Trusted configuration-only canary\ninput:\n  prompt: Reply with the word OK.\nexpect:\n  must_contain:\n    - OK\n",
	}
	for name, data := range files {
		if err = os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte(data), 0644); err != nil {
			_ = os.RemoveAll(parent)
			return "", err
		}
	}
	return source, nil
}

func (m *Manager) diagnosticImage(ctx context.Context, i Instance) (imageInspection, error) {
	var image imageInspection
	spec := i.Execution.normalized()
	ref := spec.Image
	if spec.ImageID != "" {
		ref = spec.ImageID
	}
	if !imageReference.MatchString(ref) {
		return image, errors.New("invalid image reference")
	}
	data, err := m.docker.Run(ctx, "image", "inspect", ref)
	if err != nil {
		return image, err
	}
	var images []imageInspection
	if json.Unmarshal(data, &images) != nil || len(images) != 1 {
		return image, errors.New("invalid image response")
	}
	image = images[0]
	if !skillCheckImageID.MatchString(image.ID) || image.Os != "linux" || image.Architecture != runtime.GOARCH || len(image.Config.Volumes) > 0 || (strings.HasPrefix(ref, "sha256:") && image.ID != ref) {
		return image, errors.New("unsupported image identity or policy")
	}
	return image, nil
}

func (s *Server) skillDiagnosticRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if principal(r) != nil || human(r) != nil {
				respond(w, nil, failure(403, "skill_center_admin_required", "技能工具诊断由管理员发起"))
				return
			}
			handler(w, r)
		})
	}
	register("GET /api/skill-center/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.SkillDiagnostics()
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
	register("POST /api/skill-center/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		var in SkillDiagnosticRequest
		if !decode(w, r, &in) {
			return
		}
		result, err := s.Manager.DiagnoseSkillTools(r.Context(), in)
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
}
