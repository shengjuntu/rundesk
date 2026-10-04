package app

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Defaults are seed material, not desired state. Reconnecting never reapplies them.
type ApplicationDefaults struct {
	Skills map[string]map[string]string `json:"skills,omitempty"`
	MCP    map[string]map[string]any    `json:"mcp,omitempty"`
	Model  string                       `json:"model,omitempty"`
}
type ApplicationConnect struct {
	Name           string              `json:"name"`
	Description    string              `json:"description"`
	EntryURL       string              `json:"entryUrl"`
	InstallationID string              `json:"installationId"`
	InstanceID     string              `json:"instanceId,omitempty"`
	WorkspaceID    string              `json:"workspaceId,omitempty"`
	Defaults       ApplicationDefaults `json:"defaults"`
}
type applicationConnection struct {
	InstallationID string `json:"installationId"`
	InstanceID     string `json:"instanceId"`
	WorkspaceID    string `json:"workspaceId"`
	Initialized    bool   `json:"initialized"`
}
type ApplicationConnectionResult struct {
	Application    Application `json:"application"`
	Status         string      `json:"status"`
	Message        string      `json:"message"`
	ManagementPath string      `json:"managementPath"`
}

func (m *Manager) ConnectApplication(id string, in ApplicationConnect) (ApplicationConnectionResult, error) {
	m.registrationMu.Lock()
	defer m.registrationMu.Unlock()
	out := ApplicationConnectionResult{ManagementPath: "/#application/" + id}
	if in.InstanceID == DefaultInstance {
		return out, failure(409, "assistant_reserved", "应用不能接管通用助手")
	}
	if !applicationID.MatchString(id) || !applicationID.MatchString(in.InstallationID) || strings.TrimSpace(in.Name) == "" || len(in.Name) > 160 || len(in.Description) > 4096 || !validEntryURL(in.EntryURL) || len(in.Defaults.Model) > 160 || len(in.Defaults.Skills) > 32 || len(in.Defaults.MCP) > 32 {
		return out, failure(400, "invalid_registration", "应用注册信息无效")
	}
	for name, files := range in.Defaults.Skills {
		if !slug.MatchString(name) {
			return out, bundleError("无效技能名称")
		}
		assets := []skillAsset{}
		for p, v := range files {
			assets = append(assets, skillAsset{Path: p, Data: []byte(v), Mode: 0600})
		}
		if _, e := normalizeSkillAssets(assets); e != nil {
			return out, e
		}
	}
	for name, v := range in.Defaults.MCP {
		if !slug.MatchString(name) {
			return out, fmt.Errorf("无效 MCP 名称")
		}
		if _, e := prepareMCP(v, nil); e != nil {
			return out, e
		}
	}
	var record applicationConnection
	e := m.Store.Get("application-connection", id, &record)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e == nil && record.InstallationID != in.InstallationID {
		return out, failure(409, "installation_conflict", "此应用编号已由另一份安装注册，请保留原应用数据目录或使用不同编号")
	}
	var a Application
	ae := m.Store.Get("application", id, &a)
	if ae != nil && !errors.Is(ae, sql.ErrNoRows) {
		return out, ae
	}
	if record.InstanceID == "" {
		if in.InstanceID != "" {
			if _, err := m.Instance(in.InstanceID); err != nil {
				return out, err
			}
		}
		if in.WorkspaceID != "" {
			if _, err := m.Workspace(in.WorkspaceID); err != nil {
				return out, err
			}
		}
	}
	if record.InstanceID == "" {
		record.InstallationID = in.InstallationID
		record.InstanceID = in.InstanceID
		record.WorkspaceID = in.WorkspaceID
		if ae == nil {
			if in.InstanceID != "" && in.InstanceID != a.InstanceID {
				return out, failure(409, "application_binding_conflict", "已有应用与旧实例不一致")
			}
			record.InstanceID = a.InstanceID
			if record.WorkspaceID == "" {
				record.WorkspaceID = a.WorkspaceID
			}
		}
		// Checkpoint identity before creation. A lost response or restart reuses it.
		if record.InstanceID == "" {
			record.InstanceID = "app-" + store.ID()
		}
		if record.WorkspaceID == "" {
			record.WorkspaceID = "app-" + store.ID()
		}
		if e = m.Store.Put("application-connection", id, record); e != nil {
			return out, e
		}
	}
	if record.InstanceID == DefaultInstance {
		return out, failure(409, "assistant_reserved", "应用不能接管通用助手")
	}
	if _, e = m.Instance(record.InstanceID); e != nil {
		if record.Initialized || !strings.HasPrefix(record.InstanceID, "app-") {
			return out, e
		}
		i := Instance{ID: record.InstanceID, Name: in.Name, Description: in.Description, DefaultModel: in.Defaults.Model, Managed: true, Created: store.Now(), CodexHome: filepath.Join(m.Data, "instances", record.InstanceID, "codex"), Execution: ExecutionSpec{Mode: "local"}, Permissions: Permissions{}.normalized()}
		if i.DefaultModel == "" {
			base, _ := m.Instance()
			i.DefaultModel = base.DefaultModel
		}
		if e = os.MkdirAll(i.CodexHome, 0700); e != nil {
			return out, e
		}
		if e = m.Store.Put("instance", i.ID, i); e != nil {
			return out, e
		}
		m.mu.Lock()
		m.instances[i.ID] = &i
		m.mu.Unlock()
	}
	if _, e = m.Workspace(record.WorkspaceID); e != nil {
		if record.Initialized || !strings.HasPrefix(record.WorkspaceID, "app-") {
			return out, e
		}
		w := Workspace{ID: record.WorkspaceID, Name: in.Name, Path: filepath.Join(m.Data, "workspaces", record.WorkspaceID)}
		if e = os.MkdirAll(w.Path, 0700); e != nil {
			return out, e
		}
		if e = m.Store.Put("workspace", w.ID, w); e != nil {
			return out, e
		}
		m.mu.Lock()
		m.workspaces[w.ID] = &w
		m.mu.Unlock()
	}
	if ae != nil {
		a, e = m.RegisterApplication(id, ApplicationInput{Name: in.Name, Description: in.Description, EntryURL: in.EntryURL, InstanceID: record.InstanceID, WorkspaceID: record.WorkspaceID})
		if e != nil {
			return out, e
		}
	}
	out.Application = a
	out.Status = "connected"
	out.Message = "已连接；模型、技能与工具由 RunDesk 管理"
	if record.Initialized {
		if in.EntryURL != "" && a.EntryURL != "" && in.EntryURL != a.EntryURL {
			out.Status = "needs_attention"
			out.Message = "应用访问地址发生变化；请在 RunDesk 检查应用入口和 MCP 地址，已有配置未覆盖"
		}
		return out, nil
	}
	for _, session := range m.Sessions() {
		if session.InstanceID == record.InstanceID && active(session.Status) {
			out.Status = "needs_attention"
			out.Message = "应用已登记，请等待当前任务结束后重新连接以完成首次配置"
			return out, nil
		}
	}
	// Missing items only: migration and retries must preserve administrator edits.
	for name, files := range in.Defaults.Skills {
		if _, e = m.SkillBundle(record.WorkspaceID, name, record.InstanceID, "instance"); e == nil {
			continue
		} else if !missingSkill(e) {
			out.Status = "needs_attention"
			out.Message = "技能检查失败，请在 RunDesk 管理页处理"
			return out, nil
		}
		assets := []skillAsset{}
		for p, v := range files {
			assets = append(assets, skillAsset{Path: p, Data: []byte(v), Mode: 0600})
		}
		if _, e = m.ImportSkillBundle(record.WorkspaceID, name, assets, false, record.InstanceID, "instance"); e != nil {
			out.Status = "needs_attention"
			out.Message = "初始技能安装未完成，请在 RunDesk 管理页处理"
			return out, nil
		}
	}
	if len(in.Defaults.MCP) > 0 {
		configMu.Lock()
		c, err := m.readConfig(record.WorkspaceID, record.InstanceID)
		if err == nil {
			servers, revision := userMCP(c)
			changed := false
			effective, _ := c.Config["mcp_servers"].(map[string]any)
			for name, v := range in.Defaults.MCP {
				if _, exists := servers[name]; exists {
					continue
				}
				if _, exists := effective[name]; exists {
					continue
				}
				servers[name] = v
				changed = true
			}
			if changed {
				_, err = m.writeMCP(record.WorkspaceID, revision, servers, record.InstanceID)
			}
		}
		configMu.Unlock()
		if err != nil {
			out.Status = "needs_attention"
			out.Message = "应用已登记，初始工具配置未完成；请在 RunDesk 检查 Codex 环境后重新连接"
			return out, nil
		}
	}
	record.Initialized = true
	e = m.Store.Put("application-connection", id, record)
	return out, e
}
func (s *Server) applicationConnectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/applications/{appId}/connect", func(w http.ResponseWriter, r *http.Request) {
		var in ApplicationConnect
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.ConnectApplication(r.PathValue("appId"), in)
		respond(w, v, e)
	})
}

func missingSkill(e error) bool {
	var a *apiError
	return errors.Is(e, os.ErrNotExist) || (errors.As(e, &a) && a.Code == "skill_not_found")
}
