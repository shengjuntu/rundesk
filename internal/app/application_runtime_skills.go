package app

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ApplicationRuntimeSkill struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}
type ApplicationRuntimeSkills struct {
	AppID       string                    `json:"appId"`
	PresetID    string                    `json:"presetId,omitempty"`
	InstanceID  string                    `json:"instanceId"`
	WorkspaceID string                    `json:"workspaceId"`
	Skills      []ApplicationRuntimeSkill `json:"skills"`
}

// This is a read of an already prepared connection, not an alternative runtime
// start endpoint. Only resource references needed for Skill inputs are exposed.
func (m *Manager) ApplicationRuntimeSkills(appID, presetID string) (ApplicationRuntimeSkills, error) {
	out := ApplicationRuntimeSkills{AppID: appID, PresetID: presetID, Skills: []ApplicationRuntimeSkill{}}
	a, i, err := m.applicationPresetBinding(appID, presetID)
	if err != nil {
		return out, err
	}
	out.InstanceID, out.WorkspaceID = a.InstanceID, a.WorkspaceID
	if presetID != "" {
		p, e := m.applicationPreset(appID, presetID)
		if e != nil {
			return out, e
		}
		if e = m.checkApplicationPresetResources(p); e != nil {
			return out, e
		}
	}
	if runtimeKind(i.AgentRuntime.Kind) != "codex" || i.Execution.normalized().Mode != "docker" {
		return out, failure(409, "docker_configuration_required", "此接口需要已准备的 Codex Docker 环境")
	}
	w, err := m.Workspace(a.WorkspaceID)
	if err != nil {
		return out, err
	}
	ready := false
	for _, env := range m.Environments(a.InstanceID) {
		if env.WorkspaceID == a.WorkspaceID && env.State == "running" && env.Spec == i.Execution.normalized() {
			ready = true
		}
	}
	if !ready {
		return out, failure(409, "runtime_not_prepared", "请先准备所选运行环境，再解析技能")
	}
	m.mu.Lock()
	h := m.handles["config-"+a.InstanceID+"-"+a.WorkspaceID]
	m.mu.Unlock()
	if h == nil {
		return out, failure(409, "runtime_not_prepared", "请先探测所选运行环境，再解析技能")
	}
	if !h.op.TryLock() {
		return out, failure(409, "runtime_configuration_busy", "运行环境配置连接正忙，请稍后重试")
	}
	defer h.op.Unlock()
	h.mu.Lock()
	c := h.client
	h.mu.Unlock()
	if c == nil || c.Closing() {
		return out, failure(409, "runtime_not_prepared", "运行环境配置连接已关闭，请重新准备")
	}
	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	defer cancel()
	raw, err := c.Call(ctx, "skills/list", map[string]any{"cwds": []string{w.Path}, "forceReload": true})
	if err != nil {
		return out, failure(502, "runtime_skills_unavailable", "无法读取所选环境的技能，请重新探测环境后重试")
	}
	var response struct {
		Data []struct {
			Cwd    string                    `json:"cwd"`
			Skills []ApplicationRuntimeSkill `json:"skills"`
			Errors []json.RawMessage         `json:"errors"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Data) != 1 || response.Data[0].Cwd != w.Path || len(response.Data[0].Errors) != 0 || len(response.Data[0].Skills) > 4096 {
		return out, failure(502, "runtime_skills_invalid", "运行环境未返回当前工作区的完整技能清单")
	}
	for _, sk := range response.Data[0].Skills {
		if strings.TrimSpace(sk.Name) == "" || len(sk.Name) > 1024 || len(sk.Path) > 8192 || !filepath.IsAbs(sk.Path) || strings.ContainsRune(sk.Path, 0) {
			return out, failure(502, "runtime_skills_invalid", "运行环境返回的技能引用不可供执行使用")
		}
		out.Skills = append(out.Skills, sk)
	}
	after, current, err := m.applicationPresetBinding(appID, presetID)
	if err != nil {
		return out, err
	}
	if after.InstanceID != a.InstanceID || after.WorkspaceID != a.WorkspaceID || after.PresetRevision != a.PresetRevision || current.Revision != i.Revision {
		return out, failure(409, "application_binding_changed", "技能解析期间运行配置已变化，请重试")
	}
	sort.Slice(out.Skills, func(i, j int) bool {
		if out.Skills[i].Name == out.Skills[j].Name {
			return out.Skills[i].Path < out.Skills[j].Path
		}
		return out.Skills[i].Name < out.Skills[j].Name
	})
	return out, nil
}
func (s *Server) applicationRuntimeSkillRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/applications/{appId}/runtime/skills", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.ApplicationRuntimeSkills(r.PathValue("appId"), r.URL.Query().Get("presetId"))
		respond(w, v, e)
	})
}
