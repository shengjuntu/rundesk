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

	dockertemplate "github.com/shengjuntu/rundesk/examples/docker"
)

type SetupState struct {
	Completed   bool   `json:"completed"`
	Codex       string `json:"codex"`
	WorkspaceID string `json:"workspaceId"`
	Revision    int    `json:"revision"`
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
		writeJSON(w, 200, map[string]any{"settings": v, "codex": m.codexExecutable(), "codexHome": i.CodexHome, "data": m.Data, "user": username, "model": i.DefaultModel, "firstRun": !v.Completed && len(m.Sessions()) == 0 && !m.Demo, "protected": s.Token != "", "localOnly": s.Local, "publicOrigin": s.PublicOrigin})
	})
	mux.HandleFunc("PUT /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Codex       string `json:"codex"`
			WorkspaceID string `json:"workspaceId"`
			Model       string `json:"model"`
			Completed   bool   `json:"completed"`
			Revision    int    `json:"revision"`
		}
		if !decode(w, r, &p) {
			return
		}
		m.registrationMu.Lock()
		defer m.registrationMu.Unlock()
		v, e := m.setupState()
		if e != nil {
			respond(w, nil, e)
			return
		}
		if p.Revision != v.Revision {
			respond(w, nil, failure(409, "revision_conflict", "Settings changed. Reload and retry."))
			return
		}
		if len(p.Model) > 160 {
			respond(w, nil, failure(400, "invalid_model", "Model name is too long."))
			return
		}
		path, e := exec.LookPath(strings.TrimSpace(p.Codex))
		if e != nil && !m.Demo {
			respond(w, nil, failure(400, "codex_not_found", "Codex executable not found: "+p.Codex))
			return
		}
		if m.Demo {
			path = m.Codex
		}
		path, _ = filepath.Abs(path)
		project, e := m.Workspace(p.WorkspaceID)
		if e != nil {
			respond(w, nil, e)
			return
		}
		if e = writableDirectory(project.Path); e != nil {
			respond(w, nil, failure(400, "workspace_not_writable", e.Error()))
			return
		}
		i, _ := m.Instance()
		_, e = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Description: i.Description, DefaultModel: p.Model, Revision: i.Revision})
		if e != nil {
			respond(w, nil, e)
			return
		}
		v = SetupState{Completed: p.Completed || v.Completed, Codex: path, WorkspaceID: p.WorkspaceID, Revision: v.Revision + 1}
		e = m.Store.Put("system", "setup", v)
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
