package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
	buildtools "github.com/shengjuntu/rundesk/plugins/rundesk-build-tools"
)

// An assistant run uses a new workspace and one queued Codex task. A referenced
// project is metadata only: no existing application, HOME or workspace is edited.
type BuildAssistantInput struct {
	SourceBuildID string `json:"sourceBuildId,omitempty"`
	Prompt        string `json:"prompt"`
	InstanceID    string `json:"instanceId,omitempty"`
	WorkspaceID   string `json:"workspaceId,omitempty"`
	RequestID     string `json:"requestId"`
}
type BuildAssistantRun struct {
	RuntimeKind           string `json:"runtimeKind,omitempty"`
	SourceBuildID         string `json:"sourceBuildId,omitempty"`
	ID                    string `json:"id"`
	InstanceID            string `json:"instanceId"`
	WorkspaceID           string `json:"workspaceId"`
	ParentWorkspaceID     string `json:"parentWorkspaceId,omitempty"`
	SessionID             string `json:"sessionId"`
	TaskID                string `json:"taskId"`
	Status                string `json:"status"`
	Created               string `json:"created"`
	ExpiresAt             string `json:"expiresAt"`
	RevokedAt             string `json:"revokedAt,omitempty"`
	SkillBootstrap        bool   `json:"skillBootstrap"`
	NativePluginInstalled bool   `json:"nativePluginInstalled"`
	Error                 string `json:"error,omitempty"`
}
type buildAssistantRecord struct {
	BuildAssistantRun
	RuntimeFingerprint string `json:"runtimeFingerprint,omitempty"`
	Fingerprint        string `json:"fingerprint"`
	CapabilityHash     string `json:"capabilityHash"`
	Endpoint           string `json:"endpoint"`
	WorkspacePath      string `json:"workspacePath"`
}
type buildAssistantCapability struct {
	SourceBuildID string `json:"sourceBuildId,omitempty"`
	RunID         string `json:"runId"`
	SessionID     string `json:"sessionId"`
	TaskID        string `json:"taskId"`
	InstanceID    string `json:"instanceId"`
	WorkspaceID   string `json:"workspaceId"`
	WorkspacePath string `json:"workspacePath"`
	ExpiresAt     string `json:"expiresAt"`
	RevokedAt     string `json:"revokedAt,omitempty"`
}

var buildAssistantMu sync.Mutex

func buildAssistantDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (m *Manager) buildAssistantSecretPath(id string) string {
	return filepath.Join(m.Data, "build-assistants", id, "capability")
}
func buildAssistantOrigin(raw string, docker bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", failure(400, "invalid_build_mcp_origin", "构建 MCP 回调必须是没有凭据、路径和查询参数的 HTTP(S) 来源")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if docker && (strings.EqualFold(host, "localhost") || ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
		return "", failure(400, "assistant_callback_unreachable", "Docker 构建助手需要可达的非 loopback 回调；请配置 RUNDESK_BUILD_MCP_ORIGIN 或公开 HTTPS 来源")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}
func (s *Server) buildAssistantCallback(r *http.Request, i Instance) (string, error) {
	origin := os.Getenv("RUNDESK_BUILD_MCP_ORIGIN")
	if origin == "" {
		origin = s.PublicOrigin
	}
	docker := i.Execution.normalized().Mode == "docker"
	if docker && i.Execution.normalized().Network == "none" {
		return "", failure(400, "assistant_callback_unreachable", "此 Docker 实例禁用了网络，不能连接构建 MCP")
	}
	if origin == "" {
		if docker {
			return "", failure(400, "assistant_callback_unreachable", "Docker 构建助手需要显式配置可达的 RUNDESK_BUILD_MCP_ORIGIN 或公开 HTTPS 来源")
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	origin, err := buildAssistantOrigin(origin, docker)
	if err != nil {
		return "", err
	}
	return origin + "/api/build-center/mcp", nil
}
func bootstrapBuildSkill(path string) error {
	return fs.WalkDir(buildtools.Files, "skills/add-app-docker-image", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("skills", p)
		if err != nil {
			return err
		}
		dest := filepath.Join(path, ".agents", "skills", rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, err := buildtools.Files.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, b, 0600)
	})
}

// The workspace identity is deterministic and request-owned. A crash after its
// creation but before task admission must not create a second alias on retry.
func (m *Manager) ensureBuildAssistantWorkspace(runID string) (Workspace, error) {
	id := buildAssistantDigest("assistant-workspace:" + runID)[:24]
	path := filepath.Join(m.Data, "build-assistant-workspaces", runID)
	if existing, err := m.Workspace(id); err == nil {
		if existing.Path != path {
			return Workspace{}, failure(409, "assistant_workspace_changed", "构建助手工作区标识已被修改")
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil || real != path {
			return Workspace{}, failure(409, "assistant_workspace_changed", "构建助手工作区路径已被替换")
		}
		return existing, nil
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return Workspace{}, err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return Workspace{}, failure(409, "assistant_workspace_changed", "构建助手工作区不能使用符号链接")
	}
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	if err := m.checkPresetWorkspacePath(path); err != nil {
		return Workspace{}, err
	}
	w := Workspace{ID: id, Name: "构建助手 · " + runID[:8], Path: path}
	if err := m.Store.Put("workspace", id, w); err != nil {
		return w, err
	}
	m.mu.Lock()
	m.workspaces[id] = &w
	m.mu.Unlock()
	return w, nil
}

func (m *Manager) CreateBuildAssistantRun(input BuildAssistantInput, endpoint string) (BuildAssistantRun, error) {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" || len(input.Prompt) > 64<<10 {
		return BuildAssistantRun{}, failure(400, "invalid_assistant_prompt", "请填写 1–64 KiB 的应用构建要求")
	}
	if err := validateBuildCenterRequestID(input.RequestID); err != nil {
		return BuildAssistantRun{}, err
	}
	if input.InstanceID == "" {
		input.InstanceID = DefaultInstance
	}
	fingerprint := buildAssistantDigest(input)
	id := buildAssistantDigest("assistant:" + input.RequestID)[:24]
	buildAssistantMu.Lock()
	defer buildAssistantMu.Unlock()
	var existing buildAssistantRecord
	if err := m.Store.Get("build-assistant", id, &existing); err == nil {
		if existing.Fingerprint != fingerprint {
			return BuildAssistantRun{}, failure(409, "request_id_conflict", "requestId 已用于其他构建助手要求")
		}
		return m.BuildAssistantRun(id)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return BuildAssistantRun{}, err
	}
	i, err := m.Instance(input.InstanceID)
	if err != nil {
		return BuildAssistantRun{}, err
	}
	kind := runtimeKind(i.AgentRuntime.Kind)
	if kind != "codex" && kind != "kun" {
		return BuildAssistantRun{}, failure(400, "assistant_requires_agent", "构建助手需要 Codex 或 Kun 后端")
	}
	if kind == "kun" && (i.Execution.normalized().Mode != "local" || !i.AgentRuntime.AllowWrite) {
		return BuildAssistantRun{}, failure(400, "assistant_kun_tools_required", "Kun 构建助手需要本机执行及工作区写入能力")
	}
	if input.SourceBuildID != "" {
		source, err := m.BuildCenterBuild(input.SourceBuildID)
		if err != nil {
			return BuildAssistantRun{}, err
		}
		if source.Scope == "image-test" || (source.Status != "failed" && source.Status != "interrupted" && source.Status != "canceled") {
			return BuildAssistantRun{}, failure(400, "repair_source_required", "请选择一条失败或中断的构建记录")
		}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Path != "/api/build-center/mcp" {
		return BuildAssistantRun{}, failure(400, "invalid_build_mcp_origin", "构建 MCP 回调路径无效")
	}
	origin := *parsed
	origin.Path = ""
	if _, err = buildAssistantOrigin(origin.String(), i.Execution.normalized().Mode == "docker"); err != nil {
		return BuildAssistantRun{}, err
	}
	if i.Execution.normalized().Mode == "docker" && i.Execution.normalized().Network == "none" {
		return BuildAssistantRun{}, failure(400, "assistant_callback_unreachable", "此 Docker 实例禁用了网络")
	}
	if input.WorkspaceID != "" {
		if _, err = m.Workspace(input.WorkspaceID); err != nil {
			return BuildAssistantRun{}, err
		}
	}
	var admission struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := m.Store.Get("build-assistant-admission", id, &admission); err == nil {
		if admission.Fingerprint != fingerprint {
			return BuildAssistantRun{}, failure(409, "request_id_conflict", "requestId 已用于其他构建助手要求")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return BuildAssistantRun{}, err
	} else {
		admission.Fingerprint = fingerprint
		if err := m.Store.Put("build-assistant-admission", id, admission); err != nil {
			return BuildAssistantRun{}, err
		}
	}
	w, err := m.ensureBuildAssistantWorkspace(id)
	if err != nil {
		return BuildAssistantRun{}, err
	}
	if err = bootstrapBuildSkill(w.Path); err != nil {
		return BuildAssistantRun{}, err
	}
	if err = os.MkdirAll(filepath.Join(w.Path, "artifacts"), 0700); err != nil {
		return BuildAssistantRun{}, err
	}
	if input.SourceBuildID != "" {
		if err = m.bootstrapBuildRepair(w, input.SourceBuildID); err != nil {
			return BuildAssistantRun{}, err
		}
	}
	instructions := "使用本工作区 .agents/skills/add-app-docker-image/SKILL.md 的 $add-app-docker-image Skill，实现下面的应用镜像要求。只使用 rundesk_build_center MCP 提交宿主构建；将配方、ZIP、来源与许可证审查、验证记录写入 artifacts/。保持既有应用不变，不自动发布、启动或替换实例。此工作区是专用构建任务，引用的项目仅为上下文编号，不授予其文件访问。完整跟进构建日志并进行适当的 smoke 检查；如遇外部阻塞，保留可审阅的产物并说明。\n\n用户要求：\n" + input.Prompt
	if input.SourceBuildID != "" {
		instructions += "\n\n修复来源构建：" + input.SourceBuildID + "。原始冻结上下文已复制到 artifacts/context/，原配方在 artifacts/recipe.json，失败记录在 artifacts/source-build.json。读取来源日志、检索适用经验后再修改。使用 build_context_pack 打包，无需 Bash。必须保留原 smoke 断言，通过 offline 和 smoke 测试后用 build_experience_record 保存真实修复证据；停止前写明各次假设、改动和结果。"
	}
	spec := TaskSpec{Title: "添加应用镜像", InstanceID: i.ID, WorkspaceID: w.ID, Source: SessionSource{Kind: "human"}, Input: Input{Text: instructions}}
	t, session, err := m.prepareTask(spec, true)
	if err != nil {
		return BuildAssistantRun{}, err
	}
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return BuildAssistantRun{}, err
	}
	token := "rd-build-" + hex.EncodeToString(secretBytes)
	tokenHash := keyHash(token)
	expires := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339Nano)
	run := BuildAssistantRun{ID: id, RuntimeKind: kind, SourceBuildID: input.SourceBuildID, InstanceID: i.ID, WorkspaceID: w.ID, ParentWorkspaceID: input.WorkspaceID, SessionID: session.ID, TaskID: t.ID, Status: t.Status, Created: store.Now(), ExpiresAt: expires, SkillBootstrap: true, NativePluginInstalled: false}
	record := buildAssistantRecord{BuildAssistantRun: run, RuntimeFingerprint: buildAssistantRuntimeFingerprint(i), Fingerprint: fingerprint, CapabilityHash: tokenHash, Endpoint: endpoint, WorkspacePath: w.Path}
	cap := buildAssistantCapability{SourceBuildID: input.SourceBuildID, RunID: id, SessionID: session.ID, TaskID: t.ID, InstanceID: i.ID, WorkspaceID: w.ID, WorkspacePath: w.Path, ExpiresAt: expires}
	secretPath := m.buildAssistantSecretPath(id)
	if err = os.MkdirAll(filepath.Dir(secretPath), 0700); err != nil {
		return BuildAssistantRun{}, err
	}
	if err = os.WriteFile(secretPath, []byte(token), 0600); err != nil {
		return BuildAssistantRun{}, err
	}
	admitted := false
	defer func() {
		if !admitted {
			_ = os.Remove(secretPath)
		}
	}()
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if m.ctx.Err() != nil {
		return BuildAssistantRun{}, failure(503, "runtime_unavailable", "后台正在停止")
	}
	if m.pendingCountLocked() >= 1000 {
		return BuildAssistantRun{}, failure(429, "queue_full", "待处理任务最多 1000 项")
	}
	if err = m.Store.PutMany(store.Record{Kind: "build-assistant", ID: id, Value: record}, store.Record{Kind: "build-assistant-session", ID: session.ID, Value: map[string]string{"id": id}}, store.Record{Kind: "build-assistant-capability", ID: tokenHash, Value: cap}, store.Record{Kind: "task", ID: t.ID, Value: t}, store.Record{Kind: "session", ID: session.ID, Value: session}); err != nil {
		_ = os.Remove(secretPath)
		return BuildAssistantRun{}, err
	}
	m.tasks[t.ID] = t
	m.mu.Lock()
	m.sessions[session.ID] = &session
	m.mu.Unlock()
	admitted = true
	return run, nil
}
func (m *Manager) BuildAssistantRun(id string) (BuildAssistantRun, error) {
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", id, &record); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BuildAssistantRun{}, failure(404, "assistant_run_not_found", "构建助手任务不存在")
		}
		return BuildAssistantRun{}, err
	}
	result := record.BuildAssistantRun
	if task, err := m.Task(result.TaskID); err == nil {
		result.Status = task.Status
		result.Error = task.Reason
	}
	if session, err := m.Session(result.SessionID); err == nil && session.Error != "" {
		result.Error = session.Error
	}
	result.Error = sanitizeBuildAssistantText(result.Error, "")
	var cap buildAssistantCapability
	if err := m.Store.Get("build-assistant-capability", record.CapabilityHash, &cap); err == nil {
		if cap.RevokedAt == "" && m.checkBuildAssistantCapability(cap) != nil {
			_ = m.revokeBuildAssistantCapability(record.CapabilityHash)
			_ = m.Store.Get("build-assistant-capability", record.CapabilityHash, &cap)
		}
		result.RevokedAt = cap.RevokedAt
	}
	return result, nil
}
func (m *Manager) RevokeBuildAssistantRun(id string) (BuildAssistantRun, error) {
	buildAssistantMu.Lock()
	defer buildAssistantMu.Unlock()
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", id, &record); err != nil {
		return BuildAssistantRun{}, failure(404, "assistant_run_not_found", "构建助手任务不存在")
	}
	if err := m.revokeBuildAssistantCapability(record.CapabilityHash); err != nil {
		return BuildAssistantRun{}, err
	}
	return m.BuildAssistantRun(id)
}
func (m *Manager) revokeBuildAssistantCapability(hash string) error {
	var cap buildAssistantCapability
	if err := m.Store.Get("build-assistant-capability", hash, &cap); err != nil {
		return err
	}
	if cap.RevokedAt == "" {
		cap.RevokedAt = store.Now()
		if err := m.Store.Put("build-assistant-capability", hash, cap); err != nil {
			return err
		}
	}
	_ = os.Remove(m.buildAssistantSecretPath(cap.RunID))
	return nil
}
func (m *Manager) buildAssistantCapability(token string) (buildAssistantCapability, error) {
	denied := func() (buildAssistantCapability, error) {
		return buildAssistantCapability{}, failure(401, "invalid_build_capability", "构建任务凭据无效、已过期或已撤销")
	}
	if len(token) != 73 || !strings.HasPrefix(token, "rd-build-") {
		return denied()
	}
	hash := keyHash(token)
	var cap buildAssistantCapability
	if err := m.Store.Get("build-assistant-capability", hash, &cap); err != nil {
		return denied()
	}
	if err := m.checkBuildAssistantCapability(cap); err != nil {
		_ = m.revokeBuildAssistantCapability(hash)
		return denied()
	}
	return cap, nil
}
func (m *Manager) checkBuildAssistantCapability(cap buildAssistantCapability) error {
	// The caller may have admitted the HTTP request just before a concurrent
	// revoke. Read the authoritative grant again at every effect boundary.
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", cap.RunID, &record); err != nil {
		return err
	}
	var current buildAssistantCapability
	if err := m.Store.Get("build-assistant-capability", record.CapabilityHash, &current); err != nil {
		return err
	}
	if current != cap || record.SessionID != cap.SessionID || record.TaskID != cap.TaskID || record.WorkspaceID != cap.WorkspaceID || record.InstanceID != cap.InstanceID || record.WorkspacePath != cap.WorkspacePath {
		return errors.New("capability binding changed")
	}
	expires, err := time.Parse(time.RFC3339Nano, cap.ExpiresAt)
	if err != nil || !expires.After(time.Now()) || cap.RevokedAt != "" {
		return errors.New("expired or revoked")
	}
	task, err := m.Task(cap.TaskID)
	if err != nil || !taskPending(task.Status) || task.SessionID != cap.SessionID || task.Spec.InstanceID != cap.InstanceID || task.Spec.WorkspaceID != cap.WorkspaceID {
		return errors.New("task binding changed")
	}
	session, err := m.Session(cap.SessionID)
	if err != nil || session.TaskID != cap.TaskID || session.InstanceID != cap.InstanceID || session.WorkspaceID != cap.WorkspaceID || runtimeKind(session.RuntimeKind) != runtimeKind(record.RuntimeKind) || record.SourceBuildID != cap.SourceBuildID {
		return errors.New("session binding changed")
	}
	if record.RuntimeFingerprint != "" {
		instance, err := m.Instance(cap.InstanceID)
		if err != nil || buildAssistantRuntimeFingerprint(instance) != record.RuntimeFingerprint {
			return errors.New("assistant instance configuration or permissions changed")
		}
	}
	// Session completion precedes the queue's reconciliation tick. Do not leave
	// a brief authorization window while its task still says "running".
	if session.Status != "idle" && !active(session.Status) {
		return errors.New("session finished")
	}
	w, err := m.Workspace(cap.WorkspaceID)
	if err != nil || w.Path != cap.WorkspacePath {
		return errors.New("workspace binding changed")
	}
	real, err := filepath.EvalSymlinks(w.Path)
	if err != nil || real != cap.WorkspacePath {
		return errors.New("workspace path changed")
	}
	return m.checkSessionRuntimeBinding(session, false)
}

// The shared build worker calls this before starting queued work. It must not
// acquire buildMu: the worker already holds that lock. Running Docker commands
// are canceled through the ordinary build cancellation API, never replayed.
func (m *Manager) checkBuildAssistantBuildExecution(build ImageBuild) error {
	if build.OwnerRunID == "" {
		return nil
	}
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", build.OwnerRunID, &record); err != nil {
		return failure(409, "build_assistant_finished", "构建助手授权不存在，未启动排队构建")
	}
	var capability buildAssistantCapability
	if err := m.Store.Get("build-assistant-capability", record.CapabilityHash, &capability); err != nil {
		return err
	}
	if err := m.checkBuildAssistantCapability(capability); err != nil {
		return failure(409, "build_assistant_finished", "构建助手授权已结束，未启动排队构建")
	}
	return nil
}
func (m *Manager) checkBuildAssistantExecution(session Session) error {
	var link struct {
		ID string `json:"id"`
	}
	if err := m.Store.Get("build-assistant-session", session.ID, &link); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", link.ID, &record); err != nil {
		return err
	}
	var cap buildAssistantCapability
	if err := m.Store.Get("build-assistant-capability", record.CapabilityHash, &cap); err != nil {
		return err
	}
	if err := m.checkBuildAssistantCapability(cap); err != nil {
		_ = m.revokeBuildAssistantCapability(record.CapabilityHash)
		return failure(409, "build_assistant_finished", "构建助手授权已结束；请通过构建中心发起新任务")
	}
	return nil
}

// Called only for a new/resumed native thread; overrides never touch config.toml.
func (m *Manager) configureBuildAssistant(session Session, w Workspace, params map[string]any) error {
	var link struct {
		ID string `json:"id"`
	}
	if err := m.Store.Get("build-assistant-session", session.ID, &link); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if err := m.checkBuildAssistantExecution(session); err != nil {
		return err
	}
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", link.ID, &record); err != nil {
		return err
	}
	if w.ID != record.WorkspaceID || w.Path != record.WorkspacePath {
		return failure(409, "assistant_workspace_changed", "构建助手工作区已变化")
	}
	secret, err := os.ReadFile(m.buildAssistantSecretPath(record.ID))
	if err != nil {
		return fmt.Errorf("build capability unavailable")
	}
	if keyHash(string(secret)) != record.CapabilityHash {
		return errors.New("build capability changed")
	}
	config, _ := params["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
	}
	config["mcp_servers.rundesk_build_center"] = map[string]any{"url": record.Endpoint, "http_headers": map[string]string{"Authorization": "Bearer " + string(secret)}, "enabled": true, "required": true, "startup_timeout_sec": 15, "tool_timeout_sec": 60}
	params["config"] = config
	params["sandbox"] = "workspace-write"
	return nil
}
func (s *Server) buildAssistantRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/build-center/assistant-runs", func(w http.ResponseWriter, r *http.Request) {
		var input BuildAssistantInput
		if !decode(w, r, &input) {
			return
		}
		i, err := s.Manager.Instance(input.InstanceID)
		if err != nil {
			respond(w, nil, err)
			return
		}
		endpoint, err := s.buildAssistantCallback(r, i)
		if err != nil {
			respond(w, nil, err)
			return
		}
		result, err := s.Manager.CreateBuildAssistantRun(input, endpoint)
		respond(w, result, err)
	})
	mux.HandleFunc("GET /api/build-center/assistant-runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.Manager.BuildAssistantRun(r.PathValue("id"))
		respond(w, result, err)
	})
	mux.HandleFunc("POST /api/build-center/assistant-runs/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.Manager.RevokeBuildAssistantRun(r.PathValue("id"))
		respond(w, result, err)
	})
	mux.HandleFunc("POST /api/build-center/mcp", s.buildAssistantMCP)
}
