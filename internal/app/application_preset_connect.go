package app

import (
	"database/sql"
	"errors"
)

// Caller holds registrationMu. The legacy application-connection record is
// never reused as a preset initialization checkpoint or overwritten here.
func (m *Manager) connectApplicationPreset(id string, in ApplicationConnect, readOnly bool) (ApplicationConnectionResult, error) {
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	out := ApplicationConnectionResult{PresetID: in.PresetID, ManagementPath: "/#application/" + id}
	if !applicationID.MatchString(in.InstallationID) || len(in.Name) > 160 || len(in.Description) > 4096 || !validEntryURL(in.EntryURL) || len(in.Defaults.Model) > 160 || len(in.Defaults.Skills) > 32 || len(in.Defaults.MCP) > 32 {
		return out, failure(400, "invalid_registration", "应用连接信息无效")
	}
	a, _, e := m.applicationPresetBinding(id, in.PresetID)
	if e != nil {
		return out, e
	}
	out.Application = a
	if e = m.checkApplicationPresetExecution(SessionSource{Kind: "application", AppID: id, PresetID: in.PresetID}, a.InstanceID, a.WorkspaceID); e != nil {
		return out, e
	}
	if (in.InstanceID != "" && in.InstanceID != a.InstanceID) || (in.WorkspaceID != "" && in.WorkspaceID != a.WorkspaceID) {
		return out, failure(409, "application_binding_changed", "连接请求与所选预设不一致")
	}
	// An existing installation keeps ownership of all its explicitly approved presets.
	var legacy applicationConnection
	if e = m.Store.Get("application-connection", id, &legacy); e == nil && legacy.InstallationID != "" && legacy.InstallationID != in.InstallationID {
		return out, failure(409, "installation_conflict", "应用安装标识与既有连接不一致")
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	key := applicationPresetKey(id, in.PresetID)
	var record applicationConnection
	e = m.Store.Get("application-preset-connection", key, &record)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e == nil && (record.InstallationID != in.InstallationID || record.InstanceID != a.InstanceID || record.WorkspaceID != a.WorkspaceID) {
		return out, failure(409, "installation_conflict", "预设连接检查点与当前安装或运行归属不一致")
	}
	if record.Initialized {
		out.Status = "connected"
		out.Message = "已连接所选预设；模型、技能与工具由 RunDesk 管理"
		if readOnly {
			out.Message = "已只读确认所选预设；应用凭据不更改默认技能或工具配置"
		}
		return out, nil
	}
	if readOnly {
		return out, failure(409, "preset_configuration_required", "请先由管理员连接此预设并完成初始技能和工具配置；应用凭据不会修改配置")
	}
	for name, files := range in.Defaults.Skills {
		if !slug.MatchString(name) {
			return out, bundleError("无效技能名称")
		}
		assets := []skillAsset{}
		for p, v := range files {
			assets = append(assets, skillAsset{Path: p, Data: []byte(v), Mode: 0600})
		}
		if _, e = normalizeSkillAssets(assets); e != nil {
			return out, e
		}
	}
	for name, value := range in.Defaults.MCP {
		if !slug.MatchString(name) {
			return out, failure(400, "invalid_registration", "无效 MCP 名称")
		}
		if _, e = prepareMCP(value, nil); e != nil {
			return out, e
		}
	}
	record = applicationConnection{InstallationID: in.InstallationID, InstanceID: a.InstanceID, WorkspaceID: a.WorkspaceID}
	if e = m.Store.Put("application-preset-connection", key, record); e != nil {
		return out, e
	}
	for _, s := range m.Sessions() {
		if s.InstanceID == a.InstanceID && active(s.Status) {
			out.Status = "needs_attention"
			out.Message = "所选预设正在执行，请等待其任务结束后重试初始配置"
			return out, nil
		}
	}
	// Explicitly seed only absent entries in the selected instance. No current
	// model, enabled status, administrator Skill or MCP configuration is replaced.
	for name, files := range in.Defaults.Skills {
		if _, e = m.SkillBundle(a.WorkspaceID, name, a.InstanceID, "instance"); e == nil {
			continue
		} else if !missingSkill(e) {
			return out, e
		}
		assets := []skillAsset{}
		for p, v := range files {
			assets = append(assets, skillAsset{Path: p, Data: []byte(v), Mode: 0600})
		}
		if _, e = m.ImportSkillBundle(a.WorkspaceID, name, assets, false, a.InstanceID, "instance"); e != nil {
			return out, e
		}
	}
	if len(in.Defaults.MCP) > 0 {
		configMu.Lock()
		c, err := m.readConfig(a.WorkspaceID, a.InstanceID)
		if err == nil {
			servers, revision := userMCP(c)
			effective, _ := c.Config["mcp_servers"].(map[string]any)
			changed := false
			for name, value := range in.Defaults.MCP {
				if _, ok := servers[name]; ok {
					continue
				}
				if _, ok := effective[name]; ok {
					continue
				}
				servers[name] = value
				changed = true
			}
			if changed {
				_, err = m.writeMCP(a.WorkspaceID, revision, servers, a.InstanceID)
			}
		}
		configMu.Unlock()
		if err != nil {
			return out, err
		}
	}
	// A disabled or changed binding during configuration is not marked initialized.
	next, _, err := m.applicationPresetBinding(id, in.PresetID)
	if err != nil {
		return out, err
	}
	if next.PresetRevision != a.PresetRevision {
		return out, failure(409, "application_binding_changed", "初配期间预设已变化，请重新连接")
	}
	record.Initialized = true
	if e = m.Store.Put("application-preset-connection", key, record); e != nil {
		return out, e
	}
	out.Status = "connected"
	out.Message = "已连接所选预设；仅补充缺失技能与工具，管理员配置已保留"
	return out, nil
}
