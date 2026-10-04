package app

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
)

type capabilitySelection struct {
	Skills []string `json:"skills"`
	MCP    []string `json:"mcp"`
}

// The default configuration is the shared inventory. Disabled entries remain
// selectable; enabling a copy never changes the assistant's enabled state.
func (m *Manager) availableCapabilities(a Application) (map[string]any, error) {
	raw, e := m.Skills(a.WorkspaceID, DefaultInstance)
	if e != nil {
		return nil, e
	}
	var list struct {
		Data []struct {
			Skills []Skill `json:"skills"`
		} `json:"data"`
	}
	if e = json.Unmarshal(raw, &list); e != nil {
		return nil, e
	}
	rows := []map[string]string{}
	base, e := m.Instance()
	if e != nil {
		return nil, e
	}
	for _, g := range list.Data {
		for _, sk := range g.Skills {
			name := filepath.Base(filepath.Dir(sk.Path))
			if sk.Path == filepath.Join(base.CodexHome, "skills", name, "SKILL.md") {
				rows = append(rows, map[string]string{"name": name})
			}
		}
	}
	c, e := m.readConfig(a.WorkspaceID, DefaultInstance)
	if e != nil {
		return nil, e
	}
	servers, _ := userMCP(c)
	names := []string{}
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return map[string]any{"skills": rows, "mcp": names}, nil
}
func (m *Manager) selectCapabilities(a Application, p capabilitySelection) error {
	if len(p.Skills)+len(p.MCP) > 64 {
		return failure(400, "invalid_selection", "最多选择 64 项")
	}
	for _, s := range m.Sessions() {
		if s.InstanceID == a.InstanceID && active(s.Status) {
			return failure(409, "configuration_busy", "请等待应用当前任务结束后修改能力")
		}
	}
	// Pre-read all material before mutating any target configuration.
	bundles := map[string][]skillAsset{}
	m.skillMu.Lock()
	for _, name := range p.Skills {
		files, e := m.skillAssets(a.WorkspaceID, name, DefaultInstance, "instance")
		if e != nil {
			m.skillMu.Unlock()
			return e
		}
		bundles[name] = files
	}
	m.skillMu.Unlock()
	c, e := m.readConfig(a.WorkspaceID, DefaultInstance)
	if e != nil {
		return e
	}
	source, _ := userMCP(c)
	for _, name := range p.MCP {
		if _, ok := source[name]; !ok {
			return failure(400, "missing_capability", "所选工具已不存在")
		}
	}
	for name, files := range bundles {
		if _, e = m.SkillBundle(a.WorkspaceID, name, a.InstanceID, "instance"); missingSkill(e) {
			_, e = m.ImportSkillBundle(a.WorkspaceID, name, files, false, a.InstanceID, "instance")
		}
		if e != nil {
			return e
		}
		root, dir, e := m.skillRoot(a.WorkspaceID, []string{a.InstanceID, "instance"})
		if e != nil {
			return e
		}
		if e = m.ToggleSkill(a.WorkspaceID, filepath.Join(root, dir, name, "SKILL.md"), true, a.InstanceID); e != nil {
			return e
		}
	}
	if len(p.MCP) == 0 {
		return nil
	}
	configMu.Lock()
	defer configMu.Unlock()
	target, e := m.readConfig(a.WorkspaceID, a.InstanceID)
	if e != nil {
		return e
	}
	servers, rev := userMCP(target)
	for _, name := range p.MCP {
		value, _ := servers[name].(map[string]any)
		if value == nil {
			b, _ := json.Marshal(source[name])
			if e = json.Unmarshal(b, &value); e != nil {
				return e
			}
		}
		value["enabled"] = true
		servers[name] = value
	}
	_, e = m.writeMCP(a.WorkspaceID, rev, servers, a.InstanceID)
	return e
}
func (s *Server) applicationCapabilityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/applications/{appId}/available-capabilities", func(w http.ResponseWriter, r *http.Request) {
		var a Application
		e := s.Manager.Store.Get("application", r.PathValue("appId"), &a)
		if e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Manager.availableCapabilities(a)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/applications/{appId}/capabilities", func(w http.ResponseWriter, r *http.Request) {
		var a Application
		e := s.Manager.Store.Get("application", r.PathValue("appId"), &a)
		if e != nil {
			respond(w, nil, e)
			return
		}
		var p capabilitySelection
		if !decode(w, r, &p) {
			return
		}
		e = s.Manager.selectCapabilities(a, p)
		respond(w, map[string]bool{"saved": e == nil}, e)
	})
}
