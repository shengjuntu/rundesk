package app

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"

	dockertemplate "github.com/shengjuntu/rundesk/examples/docker"
)

type SetupState struct {
	ApplicationsOnly bool   `json:"applicationsOnly"`
	Completed        bool   `json:"completed"`
	Codex            string `json:"codex"`
	WorkspaceID      string `json:"workspaceId"`
	Revision         int    `json:"revision"`
	RuntimeKind      string `json:"runtimeKind"`
}

func (m *Manager) setupState() (SetupState, error) {
	var v SetupState
	e := m.Store.Get("system", "setup", &v)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	return v, e
}
func (m *Manager) codexExecutable() string {
	v, e := m.setupState()
	if e == nil && v.Codex != "" {
		return v.Codex
	}
	return m.Codex
}
func writableDirectory(path string) error {
	f, e := os.CreateTemp(path, ".rundesk-check-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.WriteString("check"); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
func (s *Server) setupRoutes(mux *http.ServeMux) {
	m := s.Manager
	s.setupEnvironmentRoutes(mux)
	s.codexInstallRoutes(mux)
	s.setupImageRoutes(mux)
	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.setupState()
		if e != nil {
			respond(w, nil, e)
			return
		}
		i, _ := m.Instance()
		u, _ := user.Current()
		username := ""
		if u != nil {
			username = u.Username
		}
		writeJSON(w, 200, map[string]any{"settings": v, "codex": m.codexExecutable(), "codexHome": i.CodexHome, "instance": i, "backends": backendCatalog(), "data": m.Data, "user": username, "model": i.DefaultModel, "firstRun": !v.Completed && len(m.Sessions()) == 0 && !m.Demo, "protected": s.Token != "", "localOnly": s.Local, "publicOrigin": s.PublicOrigin})
	})
	mux.HandleFunc("PUT /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var p SetupInput
		if !decode(w, r, &p) {
			return
		}
		v, e := m.saveSetup(p)
		if e != nil {
			respond(w, nil, e)
			return
		}
		// Existing busy sessions keep their process. Idle assistant connections reopen.
		for _, instance := range m.Instances() {
			if instance.Execution.normalized().Mode == "local" {
				_, err := m.ReloadInstance(instance.ID)
				if err != nil {
					e = err
				}
			}
		}
		writeJSON(w, 200, map[string]any{"settings": v, "reloadWarning": errorText(e)})
	})
	mux.HandleFunc("POST /api/setup/check", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Kind        string `json:"kind"`
			WorkspaceID string `json:"workspaceId"`
			InstanceID  string `json:"instanceId"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.InstanceID == "" {
			p.InstanceID = DefaultInstance
		}
		var result any
		var e error
		switch p.Kind {
		case "kun", "bash":
			i, err := m.Instance(p.InstanceID)
			if err != nil {
				e = err
				break
			}
			executable := m.kunExecutable()
			if p.Kind == "bash" {
				executable = setupBashPath(i.AgentRuntime.BashPath)
			}
			result, e = probeVersion(r.Context(), executable)
		case "codex":
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			if m.Demo {
				result = "Demo runtime (no real Codex check)"
			} else {
				var out limitedOutput
				cmd := exec.CommandContext(ctx, m.codexExecutable(), "--version")
				cmd.WaitDelay = 2 * time.Second
				cmd.Stdout, cmd.Stderr = &out, &out
				configureProbeProcess(cmd)
				e = cmd.Run()
				result = out.String()
			}
		case "workspace":
			var project Workspace
			project, e = m.Workspace(p.WorkspaceID)
			if e == nil {
				e = writableDirectory(project.Path)
				result = project.Path
			}
		case "docker":
			result, e = m.DockerStatus()
		case "models":
			result, e = m.ConfigCall(p.WorkspaceID, "model/list", map[string]any{}, p.InstanceID)
		case "account":
			result, e = m.ConfigCall(p.WorkspaceID, "account/read", map[string]any{"refreshToken": false}, p.InstanceID)
		case "mcp":
			result, e = m.MCP(p.WorkspaceID, p.InstanceID)
		default:
			respond(w, nil, failure(400, "invalid_check", "Unknown check"))
			return
		}
		// A successful RPC is only a successful probe, not inference readiness.
		writeJSON(w, 200, map[string]any{"kind": p.Kind, "ok": e == nil, "result": result, "error": errorText(e), "checkedAt": time.Now().UTC().Format(time.RFC3339), "demo": m.Demo})
	})
	mux.HandleFunc("POST /api/setup/provider", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			WorkspaceID string `json:"workspaceId"`
			BaseURL     string `json:"baseUrl"`
			EnvKey      string `json:"envKey"`
			Model       string `json:"model"`
		}
		if !decode(w, r, &p) {
			return
		}
		u, e := url.Parse(p.BaseURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(p.Model) > 160 || strings.TrimSpace(p.Model) == "" {
			respond(w, nil, failure(400, "invalid_provider", "Enter a valid HTTP(S) endpoint and model."))
			return
		}
		if p.EnvKey != "" && !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(p.EnvKey) {
			respond(w, nil, failure(400, "invalid_env_key", "Enter an environment variable name, not the secret value."))
			return
		}
		provider := map[string]any{"name": "RunDesk custom provider", "base_url": p.BaseURL, "wire_api": "responses"}
		if p.EnvKey != "" {
			provider["env_key"] = p.EnvKey
		}
		configMu.Lock()
		defer configMu.Unlock()
		for _, edit := range []struct {
			key   string
			value any
		}{{"model_providers.rundesk", provider}, {"model_provider", "rundesk"}, {"model", p.Model}} {
			_, e = m.ConfigCall(p.WorkspaceID, "config/value/write", map[string]any{"keyPath": edit.key, "value": edit.value, "mergeStrategy": "replace"}, DefaultInstance)
			if e != nil {
				respond(w, nil, e)
				return
			}
		}
		i, e := m.Instance()
		if e != nil {
			respond(w, nil, e)
			return
		}
		_, e = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Description: i.Description, DefaultModel: p.Model, Revision: i.Revision})
		if e != nil {
			respond(w, nil, e)
			return
		}
		_, e = m.ReloadInstance(DefaultInstance)
		respond(w, map[string]any{"saved": true}, e)
	})
	mux.HandleFunc("GET /api/setup/docker-template", func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Query().Get("version")
		if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`).MatchString(version) {
			respond(w, nil, failure(400, "invalid_version", "Enter an exact Codex release, for example X.Y.Z."))
			return
		}
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		e := fs.WalkDir(dockertemplate.Files, ".", func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			data, e := dockertemplate.Files.ReadFile(path)
			if e != nil {
				return e
			}
			if path == "Dockerfile" {
				data = []byte(strings.Replace(string(data), "ARG CODEX_VERSION\n", "ARG CODEX_VERSION="+version+"\n", 1))
			}
			f, e := z.Create(path)
			if e != nil {
				return e
			}
			_, e = f.Write(data)
			return e
		})
		if e == nil {
			e = z.Close()
		}
		if e != nil {
			respond(w, nil, e)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="rundesk-codex-`+version+`.zip"`)
		w.Write(b.Bytes())
	})
}
func errorText(e error) string {
	if e != nil {
		return e.Error()
	}
	return ""
}

// SetupInput keeps old Codex-only clients compatible. The wizard supplies both
// revisions so a stale form cannot overwrite a separately edited instance.
type SetupInput struct {
	ApplicationsOnly bool      `json:"applicationsOnly"`
	Codex            string    `json:"codex"`
	WorkspaceID      string    `json:"workspaceId"`
	Model            string    `json:"model"`
	Completed        bool      `json:"completed"`
	Revision         int       `json:"revision"`
	InstanceRevision *int      `json:"instanceRevision,omitempty"`
	AgentRuntime     *p.Config `json:"agentRuntime,omitempty"`
}

func (m *Manager) saveSetup(in SetupInput) (SetupState, error) {
	m.registrationMu.Lock()
	defer m.registrationMu.Unlock()
	old, err := m.setupState()
	if err != nil {
		return old, err
	}
	if old.Revision != in.Revision {
		return old, failure(409, "revision_conflict", "Settings changed. Reload and retry.")
	}
	if len(in.Model) > 160 {
		return old, failure(400, "invalid_model", "Model name is too long.")
	}
	project, err := m.Workspace(in.WorkspaceID)
	if err != nil {
		return old, err
	}
	if err = writableDirectory(project.Path); err != nil {
		return old, failure(400, "workspace_not_writable", err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.instances[DefaultInstance]
	if current == nil {
		return old, failure(404, "instance_not_found", "Default instance not found")
	}
	if in.AgentRuntime != nil && in.InstanceRevision == nil {
		return old, failure(400, "instance_revision_required", "Engine setup requires the instance revision")
	}
	if in.InstanceRevision != nil && *in.InstanceRevision != current.Revision {
		return old, failure(409, "revision_conflict", "Engine settings changed. Reload and retry.")
	}
	next := *current
	if in.AgentRuntime != nil {
		cfg := in.AgentRuntime.Normalized()
		if err = cfg.Validate(); err != nil {
			return old, failure(400, "invalid_kun_config", err.Error())
		}
		if m.Demo && cfg.Kind == "kun" {
			return old, failure(400, "demo_no_model_calls", "Start without --demo to configure Kun")
		}
		if cfg.Kind == "kun" && next.Execution.normalized().Mode != "local" {
			return old, failure(400, "kun_local_only", "Kun supports local execution only")
		}
		if cfg.AllowWrite && next.Permissions.normalized().Sandbox == "read-only" {
			return old, failure(400, "kun_read_only", "Read-only instance cannot enable file writes")
		}
		if cfg.AllowBash && next.Permissions.normalized().Sandbox != "danger-full-access" {
			return old, failure(400, "kun_bash_host_permission", "Bash requires explicit full host access")
		}
		for _, session := range m.sessions {
			if session.InstanceID == next.ID && active(session.Status) {
				return old, failure(409, "instance_busy", "Finish active tasks before changing engine setup")
			}
		}
		next.AgentRuntime = cfg
	}
	kind := runtimeKind(next.AgentRuntime.Kind)
	if _, err = agentBackend(kind); err != nil {
		return old, err
	}
	codex := old.Codex
	if codex == "" {
		codex = m.Codex
	}
	if strings.TrimSpace(in.Codex) != "" {
		codex = strings.TrimSpace(in.Codex)
	}
	if kind == "codex" && !m.Demo && !in.ApplicationsOnly {
		codex, err = exec.LookPath(codex)
		if err != nil {
			return old, failure(400, "codex_not_found", "Codex executable not found; install Codex or select Kun")
		}
		codex, err = filepath.Abs(codex)
		if err != nil {
			return old, err
		}
	}
	if kind == "kun" && in.Completed && !in.ApplicationsOnly {
		if _, err = exec.LookPath(m.kunExecutable()); err != nil {
			return old, failure(400, "kun_not_found", "Build/install the Kun worker next to RunDesk, or set --kun")
		}
		if key := next.AgentRuntime.APIKeyEnv; key != "" && os.Getenv(key) == "" {
			return old, failure(400, "kun_credential_missing", "Set the configured credential environment variable on the service process")
		}
	}
	if !in.ApplicationsOnly {
		next.DefaultModel = strings.TrimSpace(in.Model)
		if kind == "kun" {
			next.DefaultModel = next.AgentRuntime.Model
		}
	}
	next.Revision++
	saved := SetupState{ApplicationsOnly: in.ApplicationsOnly, Completed: in.Completed || old.Completed, Codex: codex, WorkspaceID: in.WorkspaceID, Revision: old.Revision + 1, RuntimeKind: kind}
	if err = m.Store.PutMany(store.Record{Kind: "instance", ID: next.ID, Value: next}, store.Record{Kind: "system", ID: "setup", Value: saved}); err != nil {
		return old, err
	}
	*current = next
	return saved, nil
}
