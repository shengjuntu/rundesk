package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

//go:embed application_runtime_probe.py
var applicationRuntimeProbe string

type ApplicationDiagnosticChallenge struct {
	Path  string `json:"path"`
	Nonce string `json:"nonce"`
}

type ApplicationRuntimeDiagnosticsInput struct {
	InstanceID     string                          `json:"instanceId"`
	WorkspaceID    string                          `json:"workspaceId"`
	Nonce          string                          `json:"nonce"`
	Challenge      *ApplicationDiagnosticChallenge `json:"challenge,omitempty"`
	MCPImageServer string                          `json:"mcpImageServer,omitempty"`
}

type diagnosticRuntime struct {
	Status                 string            `json:"status"`
	ProfileID              string            `json:"profileId"`
	ProfileVersion         string            `json:"profileVersion"`
	AdapterContractVersion int               `json:"adapterContractVersion"`
	VideoUseCommit         string            `json:"videoUseCommit"`
	PythonVersion          string            `json:"pythonVersion"`
	FFmpegVersion          string            `json:"ffmpegVersion"`
	FFprobeVersion         string            `json:"ffprobeVersion"`
	Checks                 map[string]string `json:"checks"`
}

type diagnosticChallengeResult struct {
	Status        string `json:"status"`
	Path          string `json:"path"`
	ResponsePath  string `json:"responsePath"`
	Nonce         string `json:"nonce"`
	RequestSHA256 string `json:"requestSHA256"`
}

type diagnosticImageResult struct {
	State       string `json:"state"`
	Nonce       string `json:"nonce"`
	ImageSHA256 string `json:"imageSHA256,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Code        string `json:"code,omitempty"`
}

var diagnosticNonce = regexp.MustCompile(`^[0-9a-f]{24}$`)
var diagnosticChallengePath = regexp.MustCompile(`^outputs/([A-Za-z0-9_-]{1,128})/\.rundesk-probes/[0-9a-f]{24}/request\.json$`)

type diagnosticRuntimeMetadata struct {
	Probed       bool
	AccountReady bool
	Servers      map[string][]string
	Errors       map[string]string
}

func (m *Manager) diagnosticMetadata(ctx context.Context, w Workspace, i Instance) diagnosticRuntimeMetadata {
	result := diagnosticRuntimeMetadata{Servers: map[string][]string{}, Errors: map[string]string{}}
	m.mu.Lock()
	h := m.handles["config-"+i.ID+"-"+w.ID]
	m.mu.Unlock()
	if h == nil || !h.op.TryLock() {
		result.Errors["account"], result.Errors["mcp"] = "模型配置连接未初始化或正在使用，请检查应用连接", "MCP 配置连接未初始化或正在使用，请检查应用连接"
		return result
	}
	defer h.op.Unlock()
	h.mu.Lock()
	client := h.client
	h.mu.Unlock()
	if client == nil {
		result.Errors["account"], result.Errors["mcp"] = "模型配置连接未初始化，请检查应用连接", "MCP 配置连接未初始化，请检查应用连接"
		return result
	}
	result.Probed = true
	raw, e := client.Call(ctx, "account/read", map[string]any{"refreshToken": false})
	if e != nil {
		result.Errors["account"] = "无法读取容器模型认证，请在 RunDesk 检查"
	} else {
		var account struct {
			Account  json.RawMessage `json:"account"`
			Required *bool           `json:"requiresOpenaiAuth"`
		}
		if json.Unmarshal(raw, &account) != nil {
			result.Errors["account"] = "认证响应格式无效"
		} else {
			result.AccountReady = len(account.Account) > 0 && string(account.Account) != "null" || account.Required != nil && !*account.Required
		}
	}
	raw, e = client.Call(ctx, "mcpServerStatus/list", map[string]any{"limit": 100})
	if e != nil {
		result.Errors["mcp"] = "无法发现容器 MCP 工具，请检查工具配置和回调地址"
	} else {
		var state struct {
			Data []struct {
				Name  string                     `json:"name"`
				Tools map[string]json.RawMessage `json:"tools"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &state) != nil {
			result.Errors["mcp"] = "MCP 响应格式无效"
		} else {
			for _, server := range state.Data {
				if !slug.MatchString(server.Name) || len(result.Servers) >= 100 {
					continue
				}
				names := []string{}
				for name := range server.Tools {
					if len(name) > 0 && len(name) <= 256 && !strings.ContainsAny(name, "\r\n\x00") && len(names) < 4096 {
						names = append(names, name)
					}
				}
				sort.Strings(names)
				result.Servers[server.Name] = names
			}
		}
	}
	return result
}

// This reads only an existing configuration connection. Diagnostics never create
// environments/configuration clients, resume a session or make a model call.
func (m *Manager) diagnosticMCPConfig(ctx context.Context, w Workspace, i Instance, name string) (map[string]any, string) {
	m.mu.Lock()
	h := m.handles["config-"+i.ID+"-"+w.ID]
	m.mu.Unlock()
	if h == nil || !h.op.TryLock() {
		return nil, "mcp_config_unavailable"
	}
	defer h.op.Unlock()
	h.mu.Lock()
	c := h.client
	h.mu.Unlock()
	if c == nil {
		return nil, "mcp_config_unavailable"
	}
	raw, e := c.Call(ctx, "config/read", map[string]any{"cwd": w.Path, "includeLayers": true})
	if e != nil {
		return nil, "mcp_config_unavailable"
	}
	var config nativeConfig
	if json.Unmarshal(raw, &config) != nil {
		return nil, "mcp_config_invalid"
	}
	servers, _ := config.Config["mcp_servers"].(map[string]any)
	cfg, _ := servers[name].(map[string]any)
	if cfg == nil {
		return nil, "mcp_server_missing"
	}
	if cfg["enabled"] == false {
		return nil, "mcp_server_disabled"
	}
	const tool = "rundesk_diagnostic_image"
	for _, key := range []string{"enabled_tools", "disabled_tools"} {
		if values, ok := cfg[key].([]any); ok {
			found := false
			for _, value := range values {
				found = found || value == tool
			}
			if key == "enabled_tools" && !found || key == "disabled_tools" && found {
				return nil, "mcp_tool_disabled"
			}
		}
	}
	if endpoint, _ := cfg["url"].(string); endpoint == "" {
		return nil, "mcp_transport_unsupported"
	}
	return cfg, ""
}

func diagnosticConfigHash(cfg map[string]any) string {
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) diagnosticContainer(ctx context.Context, env Environment) error {
	raw, e := m.docker.Run(ctx, "container", "inspect", env.ContainerName)
	if e != nil || len(raw) > 256<<10 {
		return failure(503, "diagnostic_container_unavailable", "无法检查已选运行容器；未在宿主机执行替代探测")
	}
	var entries []containerInspection
	if json.Unmarshal(raw, &entries) != nil || len(entries) != 1 {
		return failure(502, "diagnostic_container_invalid", "运行容器检查结果无效")
	}
	entry := entries[0]
	if entry.Config.Labels["io.rundesk.owner"] != m.environmentOwner() || entry.Config.Labels["io.rundesk.environment"] != env.ID {
		return failure(409, "container_owner_mismatch", "运行容器归属不匹配，未执行探测")
	}
	if entry.ID != env.ContainerID || entry.Image != env.ImageID {
		return failure(409, "application_binding_changed", "运行容器或镜像已变化，请重试")
	}
	if entry.State.Status != "running" {
		return failure(409, "environment_not_running", "请先启动已选应用环境；诊断不会启动或重建容器")
	}
	return nil
}

func (m *Manager) diagnosticExecute(ctx context.Context, env Environment, w Workspace, payload any) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, failure(504, "diagnostic_timeout", "诊断已取消或超时，请检查运行环境")
	}
	input, e := json.Marshal(payload)
	if e != nil || len(input) > 65536 {
		return nil, failure(400, "diagnostic_input_invalid", "诊断配置过大或无效")
	}
	// Secrets are carried only over stdin to this fixed program, never argv or logs.
	cmd := m.docker.Command("exec", "-i", "--workdir", w.Path, env.ContainerName, "python3", "-I", "-c", applicationRuntimeProbe)
	cmd.Stdin = bytes.NewReader(input)
	output := &dockerOutput{}
	cmd.Stdout, cmd.Stderr = output, io.Discard
	cmd.WaitDelay = time.Second
	if e = cmd.Start(); e != nil {
		return nil, failure(503, "diagnostic_execution_failed", "无法在已选容器执行固定诊断；未在宿主机执行替代探测")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case e = <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return nil, failure(504, "diagnostic_timeout", "诊断已取消或超时，请检查运行环境")
	}
	if e != nil || output.Len() > 32<<10 {
		return nil, failure(503, "diagnostic_execution_failed", "固定诊断未完成，请检查容器 Python 与运行环境")
	}
	return output.Bytes(), nil
}

func (m *Manager) DiagnoseApplicationRuntime(parent context.Context, id string, in ApplicationRuntimeDiagnosticsInput) (map[string]any, error) {
	if !diagnosticNonce.MatchString(in.Nonce) || in.MCPImageServer != "" && !slug.MatchString(in.MCPImageServer) {
		return nil, failure(400, "invalid_diagnostic_request", "诊断需要 24 位小写十六进制 nonce 与有效服务名称")
	}
	a, i, e := m.applicationRuntimeBinding(id)
	if e != nil {
		return nil, e
	}
	if in.InstanceID != a.InstanceID || in.WorkspaceID != a.WorkspaceID || in.InstanceID == "" || in.WorkspaceID == "" {
		return nil, failure(409, "application_binding_changed", "应用绑定已变化，请重新连接")
	}
	if i.Execution.normalized().Mode != "docker" || runtimeKind(i.AgentRuntime.Kind) != "codex" {
		return nil, failure(409, "docker_configuration_required", "此诊断需要已选 Codex Docker 环境，不会回退到宿主机")
	}
	w, e := m.Workspace(a.WorkspaceID)
	if e != nil {
		return nil, e
	}
	if in.Challenge != nil {
		match := diagnosticChallengePath.FindStringSubmatch(in.Challenge.Path)
		if len(match) != 2 || in.Challenge.Nonce != in.Nonce {
			return nil, failure(400, "invalid_diagnostic_challenge", "挑战路径或 nonce 无效")
		}
		session, err := m.Session(match[1])
		if err != nil || session.InstanceID != i.ID || session.WorkspaceID != w.ID || session.Source.Kind != "application" || session.Source.AppID != a.AppID {
			return nil, forbidden()
		}
	}
	m.environmentMu.Lock()
	current := m.environments[environmentID(i.ID, w.ID)]
	var env Environment
	if current != nil {
		env = *current
	}
	m.environmentMu.Unlock()
	if current == nil || env.ContainerID == "" || env.State != "running" {
		return nil, failure(409, "environment_not_running", "请先启动已选应用环境；诊断不会启动或重建容器")
	}
	if env.Spec != i.Execution.normalized() {
		return nil, failure(409, "environment_update_required", "所选配置尚未应用，请由管理员更新执行环境")
	}
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	if e = m.diagnosticContainer(ctx, env); e != nil {
		return nil, e
	}
	metadata := m.diagnosticMetadata(ctx, w, i)
	var cfg map[string]any
	var imageCode string
	if in.MCPImageServer != "" {
		cfg, imageCode = m.diagnosticMCPConfig(ctx, w, i, in.MCPImageServer)
	}
	payload := map[string]any{"workspace": w.Path, "nonce": in.Nonce, "challenge": in.Challenge, "mcpConfig": cfg}
	raw, e := m.diagnosticExecute(ctx, env, w, payload)
	if e != nil {
		return nil, e
	}
	var result struct {
		SchemaVersion  int                        `json:"schemaVersion"`
		Runtime        diagnosticRuntime          `json:"runtime"`
		Challenge      *diagnosticChallengeResult `json:"challenge"`
		ImageTransport *diagnosticImageResult     `json:"imageTransport"`
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != 1 || (result.Runtime.Status != "ready" && result.Runtime.Status != "incomplete") {
		return nil, failure(502, "diagnostic_invalid_response", "固定诊断返回格式无效")
	}
	// Never bless a changed application, workspace, container or effective MCP config.
	after, next, e := m.applicationRuntimeBinding(id)
	if e != nil {
		return nil, e
	}
	afterWorkspace, e := m.Workspace(w.ID)
	if e != nil || after.Revision != a.Revision || after.InstanceID != a.InstanceID || after.WorkspaceID != a.WorkspaceID || next.Revision != i.Revision || afterWorkspace.Revision != w.Revision || afterWorkspace.Path != w.Path {
		return nil, failure(409, "application_binding_changed", "诊断期间应用配置已变化，请重试")
	}
	m.environmentMu.Lock()
	latest := m.environments[env.ID]
	changed := latest == nil || latest.Revision != env.Revision || latest.ContainerID != env.ContainerID || latest.ImageID != env.ImageID || latest.Spec != env.Spec
	m.environmentMu.Unlock()
	if changed {
		return nil, failure(409, "application_binding_changed", "诊断期间运行环境已变化，请重试")
	}
	if e = m.diagnosticContainer(ctx, env); e != nil {
		return nil, e
	}
	if cfg != nil {
		again, code := m.diagnosticMCPConfig(ctx, w, i, in.MCPImageServer)
		if code != "" || diagnosticConfigHash(cfg) != diagnosticConfigHash(again) {
			return nil, failure(409, "application_binding_changed", "诊断期间 MCP 配置已变化，请重试")
		}
	}
	view := map[string]any{"schemaVersion": 1, "appId": a.AppID, "instanceId": i.ID, "workspaceId": w.ID,
		"instanceRevision": i.Revision, "executionMode": "docker", "checkedAt": store.Now(),
		"modelTested": false, "modelVisionVerified": false, "runtime": result.Runtime,
		"probed": metadata.Probed, "accountReady": metadata.AccountReady, "servers": metadata.Servers, "errors": metadata.Errors}
	if in.Challenge != nil {
		if result.Challenge == nil || result.Challenge.Path != in.Challenge.Path || result.Challenge.Nonce != in.Nonce || result.Challenge.ResponsePath != strings.TrimSuffix(in.Challenge.Path, "request.json")+"response.json" {
			return nil, failure(502, "diagnostic_invalid_response", "挑战诊断返回无效")
		}
		view["challenge"] = result.Challenge
	}
	if in.MCPImageServer != "" {
		if imageCode != "" {
			state := "failed"
			if imageCode == "mcp_transport_unsupported" {
				state = "unsupported"
			}
			result.ImageTransport = &diagnosticImageResult{State: state, Nonce: in.Nonce, Code: imageCode}
		}
		if result.ImageTransport == nil || result.ImageTransport.Nonce != in.Nonce {
			return nil, failure(502, "diagnostic_invalid_response", "图片传输诊断返回无效")
		}
		view["imageTransport"] = result.ImageTransport
	}
	return view, nil
}

func (s *Server) applicationRuntimeDiagnosticRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/applications/{appId}/runtime/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		var in ApplicationRuntimeDiagnosticsInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.DiagnoseApplicationRuntime(r.Context(), r.PathValue("appId"), in)
		respond(w, v, e)
	})
}
