package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/store"
)

// Each application project gets an independent workspace and Docker HOME for
// each approved Agent. The preset's original binding remains a configuration
// and legacy execution identity, not the project's writable directory.
type ApplicationProjectEnvironment struct {
	AppID       string `json:"appId"`
	ProjectID   string `json:"projectId"`
	PresetID    string `json:"presetId,omitempty"`
	Name        string `json:"name"`
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path,omitempty"`
	Created     string `json:"created"`
}
type ApplicationProjectEnvironmentInput struct {
	Name string `json:"name"`
}

// VideoAPP project IDs are 24-digit hex IDs. Keeping this strict also makes
// the injected MCP identity unambiguous and prevents path or header injection.
var applicationProjectID = regexp.MustCompile(`^[a-f0-9]{24}$`)

func projectEnvironmentKey(appID, projectID, presetID string) string {
	if presetID == "" {
		presetID = "_default"
	}
	return appID + "/" + projectID + "/" + presetID
}

func (m *Manager) applicationProjectEnvironment(appID, projectID, presetID string) (ApplicationProjectEnvironment, error) {
	var b ApplicationProjectEnvironment
	if !applicationID.MatchString(appID) || !applicationProjectID.MatchString(projectID) || presetID != "" && !applicationPresetID.MatchString(presetID) {
		return b, failure(400, "invalid_application_project", "应用项目或环境编号无效")
	}
	e := m.Store.Get("application-project-environment", projectEnvironmentKey(appID, projectID, presetID), &b)
	if errors.Is(e, sql.ErrNoRows) {
		return b, failure(404, "application_project_not_found", "项目尚未绑定此 Agent 环境")
	}
	return b, e
}

func (m *Manager) applicationProjectEnvironments(appID, projectID string) ([]ApplicationProjectEnvironment, error) {
	if !applicationID.MatchString(appID) || !applicationProjectID.MatchString(projectID) {
		return nil, failure(400, "invalid_application_project", "应用项目编号无效")
	}
	rows, e := m.Store.List("application-project-environment")
	if e != nil {
		return nil, e
	}
	out := []ApplicationProjectEnvironment{}
	for _, raw := range rows {
		var b ApplicationProjectEnvironment
		if e = json.Unmarshal(raw, &b); e != nil {
			return nil, e
		}
		if b.AppID == appID && b.ProjectID == projectID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PresetID < out[j].PresetID })
	return out, nil
}

func (m *Manager) projectEnvironmentForWorkspace(wid string) (ApplicationProjectEnvironment, bool, error) {
	rows, e := m.Store.List("application-project-environment")
	if e != nil {
		return ApplicationProjectEnvironment{}, false, e
	}
	for _, raw := range rows {
		var b ApplicationProjectEnvironment
		if e = json.Unmarshal(raw, &b); e != nil {
			return b, false, e
		}
		if b.WorkspaceID == wid {
			return b, true, nil
		}
	}
	return ApplicationProjectEnvironment{}, false, nil
}

func (m *Manager) checkApplicationProjectSource(source SessionSource, iid, wid string, enforceEnabled bool) error {
	b, e := m.applicationProjectEnvironment(source.AppID, source.ProjectID, source.PresetID)
	if e != nil {
		return e
	}
	if b.InstanceID != iid || b.WorkspaceID != wid {
		return failure(409, "application_project_binding_conflict", "任务与项目的 Agent 环境不一致")
	}
	w, e := m.Workspace(wid)
	if e != nil {
		return e
	}
	path, e := filepath.EvalSymlinks(w.Path)
	if e != nil || path != b.Path {
		return failure(409, "application_project_workspace_changed", "项目工作目录已变化，任务未启动")
	}
	a, i, e := m.applicationPresetBinding(source.AppID, source.PresetID)
	if e != nil {
		return e
	}
	if a.InstanceID != iid || i.Execution.normalized().Mode != "docker" || runtimeKind(i.AgentRuntime.Kind) != "codex" || source.PresetID != "" && enforceEnabled && (a.PresetEnabled == nil || !*a.PresetEnabled) {
		return failure(409, "application_project_environment_changed", "项目 Agent 环境已禁用或改变")
	}
	if source.PresetID != "" {
		return m.checkApplicationPresetResources(ApplicationRuntimePreset{AppID: a.AppID, ID: source.PresetID, InstanceID: a.InstanceID, WorkspaceID: a.WorkspaceID})
	}
	return nil
}

func (m *Manager) PutApplicationProjectEnvironment(appID, projectID, presetID string, in ApplicationProjectEnvironmentInput) (ApplicationProjectEnvironment, error) {
	m.applicationProjectMu.Lock()
	defer m.applicationProjectMu.Unlock()
	if !applicationID.MatchString(appID) || !applicationProjectID.MatchString(projectID) || presetID != "" && !applicationPresetID.MatchString(presetID) || len([]rune(in.Name)) > 160 {
		return ApplicationProjectEnvironment{}, failure(400, "invalid_application_project", "项目编号、环境或名称无效")
	}
	if previous, e := m.applicationProjectEnvironment(appID, projectID, presetID); e == nil {
		return previous, nil // PUT is idempotent; project identity and resources are immutable.
	} else {
		var typed *apiError
		if !errors.As(e, &typed) || typed.Code != "application_project_not_found" {
			return ApplicationProjectEnvironment{}, e
		}
	}
	a, i, e := m.applicationPresetBinding(appID, presetID)
	if e != nil {
		return ApplicationProjectEnvironment{}, e
	}
	if i.Execution.normalized().Mode != "docker" || runtimeKind(i.AgentRuntime.Kind) != "codex" || presetID != "" && (a.PresetEnabled == nil || !*a.PresetEnabled) {
		return ApplicationProjectEnvironment{}, failure(409, "application_project_requires_docker", "请先在 RunDesk 启用 Codex Docker 环境")
	}
	if presetID != "" {
		if e := m.checkApplicationPresetResources(ApplicationRuntimePreset{AppID: appID, ID: presetID, InstanceID: i.ID, WorkspaceID: a.WorkspaceID}); e != nil {
			return ApplicationProjectEnvironment{}, e
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "视频项目 " + projectID[:8]
	}
	w, e := m.CreateWorkspace(name, "")
	if e != nil {
		return ApplicationProjectEnvironment{}, e
	}
	base, e := m.EnsureApplicationEnvironment(i.ID, a.WorkspaceID)
	if e != nil {
		return ApplicationProjectEnvironment{}, e
	}
	if e = m.seedApplicationProjectHome(base.CodexHome, filepath.Join(m.environmentRoot(environmentID(i.ID, w.ID)), "codex")); e != nil {
		return ApplicationProjectEnvironment{}, e
	}
	if _, e = m.EnsureApplicationEnvironment(i.ID, w.ID); e != nil {
		return ApplicationProjectEnvironment{}, e
	}
	b := ApplicationProjectEnvironment{AppID: appID, ProjectID: projectID, PresetID: presetID, Name: name, InstanceID: i.ID, WorkspaceID: w.ID, Path: w.Path, Created: store.Now()}
	if e := m.Store.Put("application-project-environment", projectEnvironmentKey(appID, projectID, presetID), b); e != nil {
		return ApplicationProjectEnvironment{}, e
	}
	return b, nil
}

func (s *Server) applicationProjectRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/applications/{appId}/project-environments", func(w http.ResponseWriter, r *http.Request) {
		appID := r.PathValue("appId")
		if _, _, e := m.applicationPresetBinding(appID, ""); e != nil {
			respond(w, nil, e)
			return
		}
		rows, e := m.Store.List("application-project-environment")
		items := []ApplicationProjectEnvironment{}
		if e == nil {
			for _, raw := range rows {
				var item ApplicationProjectEnvironment
				if e = json.Unmarshal(raw, &item); e != nil {
					break
				}
				if item.AppID == appID {
					item.Path = ""
					items = append(items, item)
				}
			}
		}
		respond(w, items, e)
	})
	mux.HandleFunc("GET /api/applications/{appId}/projects/{projectId}/environments", func(w http.ResponseWriter, r *http.Request) {
		items, e := m.applicationProjectEnvironments(r.PathValue("appId"), r.PathValue("projectId"))
		if e == nil && principal(r) != nil {
			filtered := []ApplicationProjectEnvironment{}
			for _, item := range items {
				if principal(r).owns(item.InstanceID, item.WorkspaceID, SessionSource{Kind: "application", AppID: item.AppID, ProjectID: item.ProjectID, PresetID: item.PresetID}) {
					item.Path = ""
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		for n := range items {
			items[n].Path = ""
		}
		respond(w, items, e)
	})
	mux.HandleFunc("PUT /api/applications/{appId}/projects/{projectId}/environments/{presetId}", func(w http.ResponseWriter, r *http.Request) {
		var in ApplicationProjectEnvironmentInput
		if !decode(w, r, &in) {
			return
		}
		presetID := r.PathValue("presetId")
		if presetID == "__default__" {
			presetID = ""
		}
		b, e := m.PutApplicationProjectEnvironment(r.PathValue("appId"), r.PathValue("projectId"), presetID, in)
		b.Path = ""
		respond(w, b, e)
	})
}
