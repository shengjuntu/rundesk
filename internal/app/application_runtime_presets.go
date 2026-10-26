package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/store"
)

// Runtime presets approve a dedicated execution identity, rather than allowing
// applications to supply container images or reconfigure a shared environment.
type ApplicationRuntimePreset struct {
	ID          string `json:"id"`
	AppID       string `json:"appId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
	Enabled     bool   `json:"enabled"`
	Revision    int    `json:"revision"`
	Created     string `json:"created"`
}
type ApplicationRuntimePresetInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Revision    *int   `json:"revision,omitempty"`
}
type ApplicationPresetBinding struct {
	PresetID    string `json:"presetId"`
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
}

var applicationPresetID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func applicationPresetKey(appID, presetID string) string { return appID + "/" + presetID }
func (m *Manager) applicationPreset(appID, presetID string) (ApplicationRuntimePreset, error) {
	var p ApplicationRuntimePreset
	if !applicationID.MatchString(appID) || !applicationPresetID.MatchString(presetID) {
		return p, failure(400, "invalid_runtime_preset", "运行环境预设编号无效")
	}
	e := m.Store.Get("application-runtime-preset", applicationPresetKey(appID, presetID), &p)
	if errors.Is(e, sql.ErrNoRows) {
		e = failure(404, "runtime_preset_not_found", "运行环境预设不存在")
	}
	return p, e
}
func (m *Manager) allApplicationPresets() ([]ApplicationRuntimePreset, error) {
	rows, e := m.Store.List("application-runtime-preset")
	if e != nil {
		return nil, e
	}
	out := []ApplicationRuntimePreset{}
	for _, raw := range rows {
		var p ApplicationRuntimePreset
		if e = json.Unmarshal(raw, &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created == out[j].Created {
			return out[i].ID < out[j].ID
		}
		return out[i].Created < out[j].Created
	})
	return out, nil
}
func (m *Manager) ApplicationRuntimePresets(appID string) ([]ApplicationRuntimePreset, error) {
	var a Application
	if e := m.Store.Get("application", appID, &a); e != nil {
		return nil, e
	}
	rows, e := m.allApplicationPresets()
	if e != nil {
		return nil, e
	}
	out := []ApplicationRuntimePreset{}
	for _, p := range rows {
		if p.AppID == appID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (m *Manager) PutApplicationRuntimePreset(appID, presetID string, in ApplicationRuntimePresetInput) (ApplicationRuntimePreset, error) {
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	var empty ApplicationRuntimePreset
	if !applicationPresetID.MatchString(presetID) || strings.TrimSpace(in.Name) == "" || len(in.Name) > 160 || len(in.Description) > 4096 || in.InstanceID == "" || in.InstanceID == DefaultInstance || in.WorkspaceID == "" {
		return empty, failure(400, "invalid_runtime_preset", "预设需要有效编号、名称、专用实例和工作区")
	}
	var app Application
	if e := m.Store.Get("application", appID, &app); e != nil {
		return empty, e
	}
	prev, e := m.applicationPreset(appID, presetID)
	exists := e == nil
	if e != nil {
		var typed *apiError
		if !errors.As(e, &typed) || typed.Code != "runtime_preset_not_found" {
			return empty, e
		}
	}
	if exists && (prev.InstanceID != in.InstanceID || prev.WorkspaceID != in.WorkspaceID) {
		return prev, failure(409, "runtime_preset_binding_immutable", "预设的实例和工作区归属不可修改；请登记新的预设")
	}
	if exists {
		next := prev
		next.Name = strings.TrimSpace(in.Name)
		next.Description = in.Description
		if in.Enabled != nil {
			next.Enabled = *in.Enabled
		}
		if next.Name == prev.Name && next.Description == prev.Description && next.Enabled == prev.Enabled {
			return prev, nil
		}
		if in.Revision == nil || *in.Revision != prev.Revision {
			return prev, failure(409, "revision_conflict", "预设已变化，请刷新后重试")
		}
		next.Revision++
		return next, m.Store.Put("application-runtime-preset", applicationPresetKey(appID, presetID), next)
	}
	instance, e := m.Instance(in.InstanceID)
	if e != nil {
		return empty, e
	}
	if runtimeKind(instance.AgentRuntime.Kind) != "codex" || instance.Execution.normalized().Mode != "docker" {
		return empty, failure(409, "runtime_preset_requires_docker", "预设需要已配置的专用 Codex Docker 实例")
	}
	if _, e = m.Workspace(in.WorkspaceID); e != nil {
		return empty, e
	}
	w, e := m.Workspace(in.WorkspaceID)
	if e != nil {
		return empty, e
	}
	canonical, e := canonicalPresetPath(w.Path)
	if e != nil {
		return empty, failure(409, "runtime_preset_workspace_invalid", "预设工作目录不可规范化")
	}
	for _, other := range m.Workspaces() {
		if other.ID == w.ID {
			continue
		}
		path, err := canonicalPresetPath(other.Path)
		if err != nil {
			return empty, err
		}
		if presetPathsOverlap(canonical, path) {
			return empty, failure(409, "runtime_preset_workspace_overlap", "预设工作目录与已有工作区相同或存在父子关系，请使用独立目录")
		}
	}
	entries, e := os.ReadDir(canonical)
	if e != nil || len(entries) > 0 {
		return empty, failure(409, "runtime_preset_workspace_not_empty", "预设首次绑定需要空的专用工作目录，不会接管已有文件")
	}
	for _, kind := range []string{"session", "task", "schedule", "application-key", "upload-access", "user"} {
		rows, err := m.Store.List(kind)
		if err != nil {
			return empty, err
		}
		for _, raw := range rows {
			used := false
			switch kind {
			case "session":
				var s Session
				if err = json.Unmarshal(raw, &s); err == nil {
					used = s.InstanceID == in.InstanceID || s.WorkspaceID == in.WorkspaceID
				}
			case "task":
				var s Task
				if err = json.Unmarshal(raw, &s); err == nil {
					used = s.Spec.InstanceID == in.InstanceID || s.Spec.WorkspaceID == in.WorkspaceID
				}
			case "schedule":
				var s Schedule
				if err = json.Unmarshal(raw, &s); err == nil {
					used = s.Spec.Task.InstanceID == in.InstanceID || s.Spec.Task.WorkspaceID == in.WorkspaceID
				}
			case "application-key":
				var r keyRecord
				if err = json.Unmarshal(raw, &r); err == nil {
					used = r.Key.InstanceID == in.InstanceID || r.Key.legacyWorkspace(in.WorkspaceID)
				}
			case "user":
				var r userRecord
				if err = json.Unmarshal(raw, &r); err == nil {
					for _, g := range r.User.Grants {
						if g.WorkspaceID == in.WorkspaceID {
							used = true
						}
					}
				}
			case "upload-access":
				var r uploadAccess
				if err = json.Unmarshal(raw, &r); err == nil {
					used = r.InstanceID == in.InstanceID || r.WorkspaceID == in.WorkspaceID
				}
			}
			if err != nil {
				return empty, err
			}
			if used {
				return empty, failure(409, "runtime_preset_history_conflict", "预设需要没有旧会话、任务或文件授权的专用实例和工作区")
			}
		}
	}
	apps, e := m.applicationRecords()
	if e != nil {
		return empty, e
	}
	for _, a := range apps {
		if a.InstanceID == in.InstanceID || a.WorkspaceID == in.WorkspaceID {
			return empty, failure(409, "runtime_preset_binding_conflict", "此实例或工作区已属于应用的默认环境；请使用独立配置")
		}
	}
	presets, e := m.allApplicationPresets()
	if e != nil {
		return empty, e
	}
	for _, p := range presets {
		if p.AppID == appID && p.ID == presetID {
			continue
		}
		if p.InstanceID == in.InstanceID || p.WorkspaceID == in.WorkspaceID {
			return empty, failure(409, "runtime_preset_binding_conflict", "此实例或工作区已属于其他运行环境预设")
		}
	}
	// Existing environments are never reassigned by registering a preset.
	for _, env := range m.Environments("") {
		if (env.InstanceID == in.InstanceID && env.WorkspaceID != in.WorkspaceID) || (env.WorkspaceID == in.WorkspaceID && env.InstanceID != in.InstanceID) {
			return empty, failure(409, "runtime_preset_binding_conflict", "此实例或工作区已有其他运行环境归属")
		}
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	next := ApplicationRuntimePreset{ID: presetID, AppID: appID, Name: strings.TrimSpace(in.Name), Description: in.Description, InstanceID: in.InstanceID, WorkspaceID: in.WorkspaceID, Enabled: enabled, Revision: 1, Created: store.Now()}
	if e = m.Store.Put("application-runtime-preset-path", applicationPresetKey(appID, presetID), map[string]string{"path": canonical}); e != nil {
		return empty, e
	}
	return next, m.Store.Put("application-runtime-preset", applicationPresetKey(appID, presetID), next)
}
func (m *Manager) applicationPresetBinding(appID, presetID string) (Application, Instance, error) {
	var a Application
	if e := m.Store.Get("application", appID, &a); e != nil {
		return a, Instance{}, e
	}
	if presetID != "" {
		p, e := m.applicationPreset(appID, presetID)
		if e != nil {
			return a, Instance{}, e
		}
		a.PresetID = p.ID
		a.PresetRevision = p.Revision
		a.PresetEnabled = &p.Enabled
		a.InstanceID = p.InstanceID
		a.WorkspaceID = p.WorkspaceID
	}
	i, e := m.Instance(a.InstanceID)
	return a, i, e
}
func (m *Manager) checkApplicationPresetExecution(source SessionSource, iid, wid string) error {
	return m.checkApplicationPresetSource(source, iid, wid, true)
}

// An accepted run keeps its frozen resources after disable; new admission also
// checks Enabled. Resource ownership is enforced in both cases.
func (m *Manager) checkApplicationPresetSource(source SessionSource, iid, wid string, enforceEnabled bool) error {
	if source.PresetID == "" {
		rows, e := m.allApplicationPresets()
		if e != nil {
			return e
		}
		for _, p := range rows {
			if p.InstanceID == iid || p.WorkspaceID == wid {
				return failure(409, "runtime_preset_binding_conflict", "专用预设资源需要准确的应用来源和presetId")
			}
		}
		return nil
	}
	if source.Kind != "application" {
		return forbidden()
	}
	p, e := m.applicationPreset(source.AppID, source.PresetID)
	if e != nil {
		return e
	}
	if p.InstanceID != iid || p.WorkspaceID != wid {
		return failure(409, "runtime_preset_binding_conflict", "任务与运行环境预设不一致")
	}
	if enforceEnabled && !p.Enabled {
		return failure(409, "runtime_preset_disabled", "运行环境预设已停用；历史可读，不能发起或继续任务")
	}
	return m.checkApplicationPresetResources(p)
}

func (m *Manager) checkApplicationPresetResources(p ApplicationRuntimePreset) error {
	w, e := m.Workspace(p.WorkspaceID)
	if e != nil {
		return e
	}
	current, e := canonicalPresetPath(w.Path)
	if e != nil {
		return e
	}
	var frozen map[string]string
	if e = m.Store.Get("application-runtime-preset-path", applicationPresetKey(p.AppID, p.ID), &frozen); e != nil || frozen["path"] != current {
		return failure(409, "runtime_preset_workspace_changed", "预设的实际工作目录已变化，未启动任务")
	}
	i, e := m.Instance(p.InstanceID)
	if e != nil {
		return e
	}
	if runtimeKind(i.AgentRuntime.Kind) != "codex" || i.Execution.normalized().Mode != "docker" {
		return failure(409, "runtime_preset_requires_docker", "预设必须保持专用 Codex Docker 运行方式")
	}
	return nil
}
func (s *Server) applicationRuntimePresetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/applications/{appId}/runtime-presets", func(w http.ResponseWriter, r *http.Request) {
		values, e := s.Manager.ApplicationRuntimePresets(r.PathValue("appId"))
		if e == nil && principal(r) != nil {
			filtered := []ApplicationRuntimePreset{}
			for _, p := range values {
				if principal(r).owns(p.InstanceID, p.WorkspaceID, SessionSource{Kind: "application", AppID: p.AppID, PresetID: p.ID}) {
					filtered = append(filtered, p)
				}
			}
			values = filtered
		}
		respond(w, values, e)
	})
	mux.HandleFunc("PUT /api/applications/{appId}/runtime-presets/{presetId}", func(w http.ResponseWriter, r *http.Request) {
		var in ApplicationRuntimePresetInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.PutApplicationRuntimePreset(r.PathValue("appId"), r.PathValue("presetId"), in)
		respond(w, v, e)
	})
}

func canonicalPresetPath(value string) (string, error) {
	p, e := filepath.Abs(value)
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(p)
}
func presetPathsOverlap(a, b string) bool {
	relative, e := filepath.Rel(a, b)
	if e == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return true
	}
	relative, e = filepath.Rel(b, a)
	return e == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
func (m *Manager) checkPresetWorkspacePath(path string) error {
	rows, e := m.allApplicationPresets()
	if e != nil {
		return e
	}
	for _, p := range rows {
		w, err := m.Workspace(p.WorkspaceID)
		if err != nil {
			return err
		}
		other, err := canonicalPresetPath(w.Path)
		if err != nil {
			return err
		}
		if presetPathsOverlap(path, other) {
			return failure(409, "runtime_preset_workspace_overlap", "工作目录与专用预设相同或存在父子关系")
		}
	}
	return nil
}
