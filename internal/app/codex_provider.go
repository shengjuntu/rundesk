package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

type CodexProviderConfig struct {
	Configured bool   `json:"configured"`
	Revision   int    `json:"revision"`
	BaseURL    string `json:"baseUrl"`
	Model      string `json:"model"`
	APIKeyEnv  string `json:"apiKeyEnv"`
	WireAPI    string `json:"wireAPI"`
}
type CodexProviderInput struct {
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"apiKeyEnv"`
	Revision  *int   `json:"revision"`
}
type CodexProviderView struct {
	CodexProviderConfig
	InstanceID    string   `json:"instanceId"`
	WorkspaceID   string   `json:"workspaceId"`
	EnvironmentID string   `json:"environmentId,omitempty"`
	Scope         string   `json:"scope"`
	State         string   `json:"state"`
	Warnings      []string `json:"warnings"`
}
type codexProviderBinding struct {
	CodexProviderView
	DefaultRevision        int    `json:"defaultRevision,omitempty"`
	CredentialHash         string `json:"credentialHash,omitempty"`
	DeployedCredentialHash string `json:"deployedCredentialHash,omitempty"`
}

var codexProviderMu sync.Mutex

func normalizeCodexProvider(in CodexProviderInput, allowEmpty bool) (CodexProviderConfig, error) {
	p := CodexProviderConfig{Model: strings.TrimSpace(in.Model), APIKeyEnv: strings.TrimSpace(in.APIKeyEnv), WireAPI: "responses"}
	if allowEmpty && strings.TrimSpace(in.BaseURL) == "" && p.Model == "" && p.APIKeyEnv == "" {
		return p, nil
	}
	endpoint := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"), "/responses")
	var err error
	p.BaseURL, err = setupModelEndpoint(endpoint)
	if err != nil {
		return p, failure(400, "invalid_codex_provider", err.Error())
	}
	if p.Model == "" || len(p.Model) > 160 || strings.ContainsAny(p.Model, "\r\n\x00") {
		return p, failure(400, "invalid_codex_provider", "填写准确的 Codex 模型 ID")
	}
	if p.APIKeyEnv != "" {
		switch p.APIKeyEnv {
		case "PATH", "HOME", "CODEX_HOME", "CODEX_SQLITE_HOME", "LD_PRELOAD", "LD_LIBRARY_PATH", "NODE_OPTIONS", "PYTHONPATH", "DOCKER_HOST":
			return p, failure(400, "invalid_codex_provider", "密钥变量不能覆盖运行环境控制变量")
		}
		secret, e := setupModelCredential(p.APIKeyEnv)
		if e != nil {
			return p, failure(400, "codex_provider_credential", e.Error())
		}
		if len(secret) > 16384 || strings.ContainsAny(secret, "\r\n\x00") {
			return p, failure(400, "codex_provider_credential", "密钥变量内容不适合容器环境传递")
		}
	}
	p.Configured = true
	return p, nil
}
func (m *Manager) DefaultCodexProvider() (CodexProviderConfig, error) {
	p := CodexProviderConfig{WireAPI: "responses"}
	err := m.Store.Get("system", "codex-provider", &p)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}
func (m *Manager) SaveDefaultCodexProvider(in CodexProviderInput) (CodexProviderConfig, error) {
	codexProviderMu.Lock()
	defer codexProviderMu.Unlock()
	current, err := m.DefaultCodexProvider()
	if err != nil {
		return current, err
	}
	if in.Revision == nil || *in.Revision != current.Revision {
		return current, failure(409, "revision_conflict", "Codex 默认模型服务已变化，请刷新后保存")
	}
	p, err := normalizeCodexProvider(in, true)
	if err != nil {
		return current, err
	}
	p.Revision = current.Revision
	if reflect.DeepEqual(p, current) {
		return current, nil
	}
	p.Revision++
	return p, m.Store.Put("system", "codex-provider", p)
}
func (m *Manager) ValidateDefaultCodexProvider(revision int) error {
	p, err := m.DefaultCodexProvider()
	if err != nil {
		return err
	}
	if !p.Configured || p.Revision != revision {
		return failure(409, "codex_provider_changed", "默认 Codex 模型服务已变化或未配置，请刷新后选择")
	}
	_, err = normalizeCodexProvider(CodexProviderInput{BaseURL: p.BaseURL, Model: p.Model, APIKeyEnv: p.APIKeyEnv}, false)
	return err
}
func (m *Manager) codexProviderKey(iid, wid string) string {
	if i, err := m.Instance(iid); err == nil && i.Execution.normalized().Mode != "docker" {
		return iid + ":local"
	}
	return iid + ":" + wid
}
func (m *Manager) codexProviderBinding(iid, wid string) (codexProviderBinding, error) {
	var b codexProviderBinding
	err := m.Store.Get("codex-provider-binding", m.codexProviderKey(iid, wid), &b)
	return b, err
}
func (m *Manager) CodexProvider(iid, wid string) (CodexProviderView, error) {
	i, err := m.Instance(iid)
	if err != nil {
		return CodexProviderView{}, err
	}
	if err = m.requireDailyInstance(i.ID); err != nil {
		return CodexProviderView{}, err
	}
	if _, err = m.Workspace(wid); err != nil {
		return CodexProviderView{}, err
	}
	b, err := m.codexProviderBinding(i.ID, wid)
	if errors.Is(err, sql.ErrNoRows) {
		return CodexProviderView{CodexProviderConfig: CodexProviderConfig{WireAPI: "responses"}, InstanceID: i.ID, WorkspaceID: wid, Scope: map[bool]string{true: "environment", false: "instance"}[i.Execution.normalized().Mode == "docker"], State: "unmanaged", Warnings: []string{}}, nil
	}
	b.WorkspaceID = wid
	return b.CodexProviderView, err
}
func (m *Manager) codexProviderModel(iid, wid string) string {
	b, err := m.codexProviderBinding(iid, wid)
	if err == nil && b.State != "recreate_required" {
		return b.Model
	}
	return ""
}
func (m *Manager) ApplyDefaultCodexProvider(iid, wid string, revision int) error {
	codexProviderMu.Lock()
	defer codexProviderMu.Unlock()
	b, err := m.codexProviderBinding(iid, wid)
	if err == nil {
		if b.DefaultRevision != revision {
			return failure(409, "codex_provider_conflict", "此环境已有不同模型配置，不能用默认值覆盖")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = m.ValidateDefaultCodexProvider(revision); err != nil {
		return err
	}
	p, err := m.DefaultCodexProvider()
	if err != nil {
		return err
	}
	p.Revision = 1
	_, err = m.applyCodexProvider(iid, wid, p, revision)
	return err
}
func (m *Manager) SaveCodexProvider(iid, wid string, in CodexProviderInput) (CodexProviderView, error) {
	codexProviderMu.Lock()
	defer codexProviderMu.Unlock()
	old, err := m.CodexProvider(iid, wid)
	if err != nil {
		return old, err
	}
	if in.Revision == nil || *in.Revision != old.Revision {
		return old, failure(409, "revision_conflict", "模型服务配置已变化，请刷新后保存")
	}
	p, err := normalizeCodexProvider(in, false)
	if err != nil {
		return old, err
	}
	p.Revision = old.Revision + 1
	return m.applyCodexProvider(iid, wid, p, 0)
}
func (m *Manager) applyCodexProvider(iid, wid string, p CodexProviderConfig, defaultRevision int) (CodexProviderView, error) {
	i, err := m.Instance(iid)
	if err != nil {
		return CodexProviderView{}, err
	}
	if err = m.requireDailyInstance(i.ID); err != nil {
		return CodexProviderView{}, err
	}
	w, err := m.Workspace(wid)
	if err != nil {
		return CodexProviderView{}, err
	}
	for _, s := range m.Sessions() {
		if s.InstanceID == i.ID && (s.WorkspaceID == wid || i.Execution.normalized().Mode != "docker") && active(s.Status) {
			return CodexProviderView{}, failure(409, "codex_provider_busy", "请等待此环境的任务结束后修改模型服务")
		}
	}
	b := codexProviderBinding{CodexProviderView: CodexProviderView{CodexProviderConfig: p, InstanceID: i.ID, WorkspaceID: wid, Scope: "instance", State: "configured", Warnings: []string{}}, DefaultRevision: defaultRevision}
	if p.APIKeyEnv != "" {
		b.CredentialHash = keyHash(os.Getenv(p.APIKeyEnv))
	}
	home := i.CodexHome
	configMu.Lock()
	defer configMu.Unlock()
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	if i.Execution.normalized().Mode == "docker" {
		v, e := m.ensureEnvironment(i, w)
		if e != nil {
			return b.CodexProviderView, e
		}
		b.Scope, b.EnvironmentID, home = "environment", v.ID, v.CodexHome
		if err = m.closeEnvironmentConnections(v); err != nil {
			return b.CodexProviderView, err
		}
		old, _ := m.codexProviderBinding(i.ID, wid)
		b.DeployedCredentialHash = old.DeployedCredentialHash
		if v.ContainerID != "" && b.CredentialHash != b.DeployedCredentialHash {
			b.State = "recreate_required"
			b.Warnings = append(b.Warnings, "密钥环境变量已变化；请在运行环境中明确重建容器后测试")
		}
		if _, e = os.Stat(filepath.Join(home, ".rundesk-seeded")); os.IsNotExist(e) && v.ContainerID == "" {
			b.State = "pending"
		}
		if u, _ := url.Parse(p.BaseURL); u != nil {
			host := u.Hostname()
			ip := net.ParseIP(host)
			if strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback() {
				b.Warnings = append(b.Warnings, "容器内 localhost 指向容器自身；宿主服务请填写容器可访问的地址，例如 host.docker.internal")
			}
		}
	} else {
		// Local instances share one native home across workspaces. Close idle native
		// connections before replacing that home; active work was rejected above.
		reloaded, e := m.ReloadInstance(i.ID)
		if e != nil {
			return b.CodexProviderView, e
		}
		if stats, ok := reloaded.(map[string]any); ok && stats["busyConnections"].(int) > 0 {
			return b.CodexProviderView, failure(409, "codex_provider_busy", "配置连接正在使用，请稍后重试")
		}
	}
	if b.State == "configured" {
		if err = writeCodexProviderConfig(home, p); err != nil {
			return b.CodexProviderView, err
		}
	}
	err = m.Store.Put("codex-provider-binding", m.codexProviderKey(i.ID, wid), b)
	return b.CodexProviderView, err
}

// The parser preserves unknown configuration values (MCP, Skills and permissions).
// Re-serialization normalizes TOML formatting and comments; no string surgery.
func writeCodexProviderConfig(home string, p CodexProviderConfig) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer root.Close()
	config := map[string]any{}
	if info, e := root.Lstat("config.toml"); e == nil && !info.Mode().IsRegular() {
		return failure(409, "codex_config_invalid", "配置文件必须是普通文件，未修改")
	}
	data, err := root.ReadFile("config.toml")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 8<<20 {
		return failure(400, "codex_config_too_large", "配置超过 8 MiB，未修改")
	}
	if len(data) > 0 {
		if err = toml.Unmarshal(data, &config); err != nil {
			return failure(409, "codex_config_invalid", "原 Codex TOML 配置无效，未修改")
		}
	}
	providers, _ := config["model_providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	provider := map[string]any{"name": "RunDesk model service", "base_url": p.BaseURL, "wire_api": "responses", "requires_openai_auth": false}
	if p.APIKeyEnv != "" {
		provider["env_key"] = p.APIKeyEnv
	}
	providers["rundesk"] = provider
	config["model_providers"] = providers
	config["model_provider"] = "rundesk"
	config["model"] = p.Model
	data, err = toml.Marshal(config)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(home, ".rundesk-provider-*.tmp")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(filepath.Base(path), "config.toml")
}

func (m *Manager) applySeededCodexProvider(v Environment) error {
	b, err := m.codexProviderBinding(v.InstanceID, v.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.State == "configured" {
		return nil
	}
	if b.CredentialHash != b.DeployedCredentialHash {
		return failure(409, "codex_provider_recreate_required", "模型密钥已变化，请明确重建容器后继续")
	}
	if err = writeCodexProviderConfig(v.CodexHome, b.CodexProviderConfig); err != nil {
		return err
	}
	b.State = "configured"
	b.Warnings = []string{}
	return m.Store.Put("codex-provider-binding", m.codexProviderKey(v.InstanceID, v.WorkspaceID), b)
}
func (m *Manager) codexProviderEnvFile(v Environment) (string, func(), error) {
	cleanup := func() {}
	b, err := m.codexProviderBinding(v.InstanceID, v.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && b.APIKeyEnv == "" {
		return "", cleanup, nil
	}
	if err != nil {
		return "", cleanup, err
	}
	secret, err := setupModelCredential(b.APIKeyEnv)
	if err != nil {
		return "", cleanup, err
	}
	if keyHash(secret) != b.CredentialHash {
		return "", cleanup, failure(409, "codex_provider_credential_changed", "密钥环境变量已变化，请重新保存模型服务配置")
	}
	if len(secret) > 16384 || strings.ContainsAny(secret, "\r\n\x00") {
		return "", cleanup, failure(400, "codex_provider_credential", "密钥内容无法传递")
	}
	f, err := os.CreateTemp(m.Data, ".codex-provider-env-*")
	if err != nil {
		return "", cleanup, err
	}
	path := f.Name()
	cleanup = func() { os.Remove(path) }
	if err = f.Chmod(0600); err == nil {
		_, err = fmt.Fprintf(f, "%s=%s\n", b.APIKeyEnv, secret)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}
func (m *Manager) recordCodexProviderDeployment(v Environment) error {
	b, err := m.codexProviderBinding(v.InstanceID, v.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	b.DeployedCredentialHash = b.CredentialHash
	return m.Store.Put("codex-provider-binding", m.codexProviderKey(v.InstanceID, v.WorkspaceID), b)
}
func (s *Server) codexProviderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/codex-provider", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.DefaultCodexProvider()
		respond(w, v, e)
	})
	mux.HandleFunc("PUT /api/setup/codex-provider", func(w http.ResponseWriter, r *http.Request) {
		var in CodexProviderInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.SaveDefaultCodexProvider(in)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/instances/{iid}/codex-provider", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.CodexProvider(r.PathValue("iid"), r.URL.Query().Get("workspaceId"))
		respond(w, v, e)
	})
	mux.HandleFunc("PUT /api/instances/{iid}/codex-provider", func(w http.ResponseWriter, r *http.Request) {
		var in CodexProviderInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.SaveCodexProvider(r.PathValue("iid"), r.URL.Query().Get("workspaceId"), in)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/instances/{iid}/codex-provider/test", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.TestCodexProvider(r.Context(), r.PathValue("iid"), r.URL.Query().Get("workspaceId"))
		respond(w, v, e)
	})
}

// Project homes already snapshot their source Agent config. Keep provider
// metadata and its selected credential name aligned with that same snapshot.
func (m *Manager) inheritProjectCodexProvider(iid, sourceWid, targetWid string) error {
	codexProviderMu.Lock()
	defer codexProviderMu.Unlock()
	if _, err := m.codexProviderBinding(iid, targetWid); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	source, err := m.codexProviderBinding(iid, sourceWid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	source.WorkspaceID = targetWid
	source.EnvironmentID = environmentID(iid, targetWid)
	source.Revision = 1
	source.DefaultRevision = 0
	source.State = "pending"
	source.DeployedCredentialHash = ""
	source.Warnings = []string{}
	return m.Store.Put("codex-provider-binding", m.codexProviderKey(iid, targetWid), source)
}
