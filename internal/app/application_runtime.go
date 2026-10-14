package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
)

// An application may resume only its administrator-selected environment. This
// endpoint cannot choose images, change configuration, rebuild or remove it.
type ApplicationRuntimeInput struct {
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
	Probe       bool   `json:"probe"`
}

func (m *Manager) applicationRuntimeBinding(id string) (Application, Instance, error) {
	var a Application
	if e := m.Store.Get("application", id, &a); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			e = failure(404, "application_not_found", "应用未登记")
		}
		return a, Instance{}, e
	}
	i, e := m.Instance(a.InstanceID)
	return a, i, e
}

func applicationRuntimeView(a Application, i Instance, env *Environment) map[string]any {
	v := map[string]any{"appId": a.AppID, "instanceId": a.InstanceID, "workspaceId": a.WorkspaceID,
		"executionMode": i.Execution.normalized().Mode, "instanceRevision": i.Revision,
		"image": i.Execution.Image, "probed": false, "modelTested": false}
	if env != nil {
		// Do not expose host paths, credentials or raw configuration to app keys.
		v["environment"] = map[string]any{"id": env.ID, "state": env.State, "imageId": env.ImageID,
			"revision": env.Revision, "observedAt": env.ObservedAt, "pendingUpdate": env.Spec != i.Execution.normalized()}
	}
	return v
}

func (m *Manager) ApplicationRuntime(id string) (map[string]any, error) {
	a, i, e := m.applicationRuntimeBinding(id)
	if e != nil {
		return nil, e
	}
	for _, env := range m.Environments(i.ID) {
		if env.WorkspaceID == a.WorkspaceID {
			return applicationRuntimeView(a, i, &env), nil
		}
	}
	return applicationRuntimeView(a, i, nil), nil
}

func (m *Manager) PrepareApplicationRuntime(id string, in ApplicationRuntimeInput) (map[string]any, error) {
	// Serialize with reconfiguration, but unlike the administrative start action,
	// leave other active sessions and their leases untouched.
	m.executionMu.RLock()
	a, i, e := m.applicationRuntimeBinding(id)
	if e == nil && (in.InstanceID == "" || in.WorkspaceID == "" || in.InstanceID != a.InstanceID || in.WorkspaceID != a.WorkspaceID) {
		e = failure(409, "application_binding_changed", "应用绑定已变化，请重新连接后开始任务")
	}
	if e == nil && (i.Execution.normalized().Mode != "docker" || runtimeKind(i.AgentRuntime.Kind) != "codex") {
		e = failure(409, "docker_configuration_required", "请先由管理员为此应用选择 Codex Docker 镜像")
	}
	var env Environment
	if e == nil {
		var w Workspace
		w, e = m.Workspace(a.WorkspaceID)
		if e == nil {
			m.environmentMu.Lock()
			env, e = m.ensureEnvironmentLocked(i, w)
			if e == nil && env.Spec != i.Execution.normalized() {
				e = failure(409, "environment_update_required", "应用已选择新镜像或资源配置，请在 RunDesk 显式更新环境后重试")
			}
			if e == nil {
				e = m.startEnvironmentLocked(&env, w)
			}
			m.environmentMu.Unlock()
		}
	}
	m.executionMu.RUnlock()
	if e != nil {
		return nil, e
	}
	view := applicationRuntimeView(a, i, &env)
	if !in.Probe {
		return view, nil
	}
	problems := map[string]string{}
	accountReady := false
	raw, e := m.ConfigCall(a.WorkspaceID, "account/read", map[string]any{"refreshToken": false}, i.ID)
	if e != nil {
		problems["account"] = "无法读取容器模型认证，请在 RunDesk 检查"
	} else {
		var account struct {
			Account  json.RawMessage `json:"account"`
			Required *bool           `json:"requiresOpenaiAuth"`
		}
		if json.Unmarshal(raw, &account) != nil {
			problems["account"] = "认证响应格式无效"
		} else {
			accountReady = len(account.Account) > 0 && string(account.Account) != "null" || account.Required != nil && !*account.Required
		}
	}
	servers := map[string][]string{}
	raw, e = m.ConfigCall(a.WorkspaceID, "mcpServerStatus/list", map[string]any{"limit": 100}, i.ID)
	if e != nil {
		problems["mcp"] = "无法发现容器 MCP 工具，请检查工具配置和回调地址"
	} else {
		var state struct {
			Data []struct {
				Name  string                     `json:"name"`
				Tools map[string]json.RawMessage `json:"tools"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &state) != nil {
			problems["mcp"] = "MCP 响应格式无效"
		} else {
			for _, server := range state.Data {
				names := []string{}
				for name := range server.Tools {
					names = append(names, name)
				}
				sort.Strings(names)
				servers[server.Name] = names
			}
		}
	}
	// A successful probe must not bless a binding/configuration changed mid-call.
	after, current, e := m.applicationRuntimeBinding(id)
	if e != nil {
		return nil, e
	}
	if after.InstanceID != a.InstanceID || after.WorkspaceID != a.WorkspaceID || current.Revision != i.Revision {
		return nil, failure(409, "application_binding_changed", "探测期间应用配置已变化，请重试")
	}
	view["probed"], view["accountReady"], view["servers"], view["errors"] = true, accountReady, servers, problems
	return view, nil
}

func (s *Server) applicationRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/applications/{appId}/runtime", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.ApplicationRuntime(r.PathValue("appId"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/applications/{appId}/runtime/prepare", func(w http.ResponseWriter, r *http.Request) {
		var in ApplicationRuntimeInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.PrepareApplicationRuntime(r.PathValue("appId"), in)
		respond(w, v, e)
	})
}
