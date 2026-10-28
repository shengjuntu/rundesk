package app

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

// Completing onboarding records the user's choices without changing a running
// instance's backend. Kun setup has its own instance and persistent configuration.
type SetupCompleteInput struct {
	WorkspaceID string `json:"workspaceId,omitempty"`
	Codex       string `json:"codex,omitempty"`
	Revision    int    `json:"revision"`
	Completed   *bool  `json:"completed,omitempty"`
}

func (m *Manager) completeSetup(in SetupCompleteInput) (SetupState, error) {
	m.registrationMu.Lock()
	defer m.registrationMu.Unlock()
	saved, err := m.setupState()
	if err != nil {
		return saved, err
	}
	if saved.Revision != in.Revision {
		return saved, failure(409, "revision_conflict", "初始化设置已变化，请刷新后重试")
	}
	wid := in.WorkspaceID
	if wid == "" {
		wid = saved.WorkspaceID
	}
	if wid == "" {
		if all := m.Workspaces(); len(all) > 0 {
			wid = all[0].ID
		}
	}
	workspace, err := m.Workspace(wid)
	if err != nil {
		return saved, err
	}
	if err = writableDirectory(workspace.Path); err != nil {
		return saved, failure(400, "workspace_not_writable", err.Error())
	}
	if path := strings.TrimSpace(in.Codex); path != "" {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return saved, failure(400, "codex_not_found", "请选择已安装的 Codex 可执行文件")
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return saved, err
		}
		if !m.Demo {
			if _, err = probeVersion(m.ctx, resolved); err != nil {
				return saved, failure(400, "codex_check_failed", "Codex 版本检查失败："+err.Error())
			}
		}
		saved.Codex = resolved
	}
	saved.WorkspaceID = wid
	saved.Revision++
	if in.Completed == nil || *in.Completed {
		saved.Completed = true
	}
	err = m.Store.Put("system", "setup", saved)
	return saved, err
}

func (s *Server) setupCompleteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/setup/complete", func(w http.ResponseWriter, r *http.Request) {
		var in SetupCompleteInput
		if !decode(w, r, &in) {
			return
		}
		old, _ := s.Manager.setupState()
		result, err := s.Manager.completeSetup(in)
		if err != nil {
			respond(w, nil, err)
			return
		}
		warnings := []string{}
		if result.Codex != "" && result.Codex != old.Codex {
			for _, instance := range s.Manager.Instances() {
				if instance.Execution.normalized().Mode == "local" && runtimeKind(instance.AgentRuntime.Kind) == "codex" {
					if _, e := s.Manager.ReloadInstance(instance.ID); e != nil {
						warnings = append(warnings, e.Error())
					}
				}
			}
		}
		respond(w, map[string]any{"settings": result, "reloadWarning": strings.Join(warnings, "; ")}, nil)
	})
}
