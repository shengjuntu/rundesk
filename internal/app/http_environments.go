package app

import (
	"net/http"
	"time"
)

func (s *Server) environmentRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/docker/status", func(w http.ResponseWriter, r *http.Request) { v, e := m.DockerStatus(); respond(w, v, e) })
	mux.HandleFunc("PUT /api/instances/{iid}/execution", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Revision *int           `json:"revision"`
			Spec     *ExecutionSpec `json:"spec"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Revision == nil {
			respond(w, nil, failure(400, "revision_required", "需要应用当前 revision"))
			return
		}
		if p.Spec == nil || p.Spec.Mode == "" {
			respond(w, nil, failure(400, "invalid_execution", "需要明确填写 spec.mode"))
			return
		}
		v, e := m.SetExecution(r.PathValue("iid"), *p.Revision, *p.Spec)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/environments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, m.Environments(r.URL.Query().Get("instanceId")))
	})
	mux.HandleFunc("POST /api/environments", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			InstanceID  string `json:"instanceId"`
			WorkspaceID string `json:"workspaceId"`
		}
		if !decode(w, r, &p) {
			return
		}
		v, e := m.EnsureApplicationEnvironment(p.InstanceID, p.WorkspaceID)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/environments/{eid}", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.EnvironmentAction(r.PathValue("eid"), "inspect", 0)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/environments/{eid}/actions", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			ApplicationRevision *int   `json:"applicationRevision"`
			Action              string `json:"action"`
			Revision            *int   `json:"revision"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Revision == nil {
			respond(w, nil, failure(400, "revision_required", "需要环境当前 revision"))
			return
		}
		var target []int
		if p.ApplicationRevision != nil {
			target = append(target, *p.ApplicationRevision)
		}
		v, e := m.EnvironmentAction(r.PathValue("eid"), p.Action, *p.Revision, target...)
		respond(w, v, e)
	})
}
func (m *Manager) dockerDiagnostics(w Workspace, i Instance) (any, error) {
	current, v, e := m.effectiveInstance(i, w)
	if e != nil {
		return nil, e
	}
	checks := []Check{}
	add := func(name string, fn func() error) {
		start := time.Now()
		e := fn()
		v := Check{Name: name, Status: "ok", Detail: "检查通过", DurationMS: time.Since(start).Milliseconds()}
		if e != nil {
			v.Status = "error"
			v.Detail = e.Error()
		}
		checks = append(checks, v)
	}
	add("Docker Engine", func() error { _, e := m.DockerStatus(); return e })
	add("容器 App Server 配置接口", func() error { _, e := m.readConfig(w.ID, i.ID); return e })
	add("容器 Skills 扫描", func() error { _, e := m.Skills(w.ID, i.ID); return e })
	add("容器 MCP 状态接口", func() error {
		_, e := m.ConfigCall(w.ID, "mcpServerStatus/list", map[string]any{"limit": 100}, i.ID)
		return e
	})
	checks = append(checks, Check{Name: "容器内 Codex 沙箱", Status: "skipped", Detail: "此检查不执行模型任务。容器启用只读根目录和资源限制，Codex 保留应用配置的权限；需要在目标主机验收具体工具的执行。"})
	return map[string]any{"version": Version, "instanceId": i.ID, "executionMode": "docker", "codexHome": current.CodexHome, "environment": v, "checks": checks, "permissions": i.Permissions, "note": "配置探测不调用模型；认证与 MCP 保存在当前项目环境。"}, nil
}
