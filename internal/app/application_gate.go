package app

import (
	"bytes"
	"encoding/json"
	"github.com/your-org/kun/sdk/protocol"
	"io"
	"net/http"
	"strings"
)

// Default-deny app access. Run before idempotency so a cached reply cannot bypass ownership.
func (s *Server) applicationGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := principal(r)
		if k == nil {
			next.ServeHTTP(w, r)
			return
		}
		if e := s.checkApplication(r, k); e != nil {
			writeErr(w, 403, e)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func forbidden() error {
	return failure(403, "application_forbidden", "此应用凭据不允许访问该资源或执行此操作")
}
func normalizeTask(k *ApplicationKey, t *TaskSpec) error {
	if t.Source.Kind != "" && t.Source.Kind != "application" || t.Source.AppID != "" && t.Source.AppID != k.AppID {
		return forbidden()
	}
	presetID := t.Source.PresetID
	if presetID == "" && !k.legacyWorkspace(t.WorkspaceID) {
		for _, p := range k.PresetBindings {
			if p.WorkspaceID == t.WorkspaceID && (t.InstanceID == "" || t.InstanceID == p.InstanceID) {
				if presetID != "" {
					return forbidden()
				}
				presetID = p.PresetID
			}
		}
	}
	if presetID != "" {
		p, ok := k.preset(presetID)
		if !ok || (t.InstanceID != "" && t.InstanceID != p.InstanceID) || (t.WorkspaceID != "" && t.WorkspaceID != p.WorkspaceID) {
			return forbidden()
		}
		t.InstanceID = p.InstanceID
		t.WorkspaceID = p.WorkspaceID
		t.Source.PresetID = p.PresetID
	} else {
		if t.InstanceID != "" && t.InstanceID != k.InstanceID || !k.legacyWorkspace(t.WorkspaceID) {
			return forbidden()
		}
		t.InstanceID = k.InstanceID
	}
	t.Source.Kind = "application"
	t.Source.AppID = k.AppID
	return nil
}
func rewriteBody(r *http.Request, v any, validate func() error) error {
	b, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		return failure(400, "invalid_request", "请求过大或无法读取")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return failure(400, "invalid_request", e.Error())
	}
	var rest any
	if d.Decode(&rest) != io.EOF {
		return failure(400, "invalid_request", "只接受一个 JSON 对象")
	}
	if e = validate(); e != nil {
		return e
	}
	b, e = json.Marshal(v)
	if e != nil {
		return e
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	r.ContentLength = int64(len(b))
	return nil
}
func (s *Server) checkApplication(r *http.Request, k *ApplicationKey) error {
	p := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	method := r.Method
	m := s.Manager
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return forbidden()
	}
	if len(p) == 1 {
		if method == "GET" {
			switch p[0] {
			case "meta", "openapi.json", "whoami", "workspaces", "sessions", "tasks":
				return nil
			case "schedules":
				if k.scope("schedules") {
					return nil
				}
			}
		}
		if method == "POST" && k.scope("run") {
			switch p[0] {
			case "tasks":
				var v TaskSpec
				return rewriteBody(r, &v, func() error { return s.normalizeApplicationTask(r, k, &v) })
			case "sessions":
				var v struct {
					WorkspaceID   string          `json:"workspaceId"`
					InstanceID    string          `json:"instanceId"`
					Title         string          `json:"title"`
					Model         string          `json:"model"`
					Source        SessionSource   `json:"source"`
					TraceAnalysis *TraceSelection `json:"traceAnalysis,omitempty"`
				}
				return rewriteBody(r, &v, func() error {
					if v.TraceAnalysis != nil {
						return forbidden()
					}
					t := TaskSpec{WorkspaceID: v.WorkspaceID, InstanceID: v.InstanceID, Source: v.Source}
					if e := normalizeTask(k, &t); e != nil {
						return e
					}
					if e := m.checkApplicationPresetExecution(t.Source, t.InstanceID, t.WorkspaceID); e != nil {
						return e
					}
					v.InstanceID = t.InstanceID
					v.WorkspaceID = t.WorkspaceID
					v.Source = t.Source
					return nil
				})
			case "schedules":
				if k.scope("schedules") {
					var v ScheduleSpec
					return rewriteBody(r, &v, func() error { return s.normalizeApplicationTask(r, k, &v.Task) })
				}
			}
		}
		return forbidden()
	}
	switch p[0] {
	case "applications":
		if len(p) < 3 || p[1] != k.AppID {
			return forbidden()
		}
		if len(p) == 3 && p[2] == "runtime-presets" && method == "GET" && k.scope("read") {
			return nil
		}
		if len(p) == 3 && p[2] == "connect" && method == "POST" && k.scope("read") {
			// Preserve default-connect denial before decoding privileged defaults.
			b, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
			var selector struct {
				PresetID string `json:"presetId"`
			}
			if err != nil || len(b) > 1<<20 || json.Unmarshal(b, &selector) != nil || selector.PresetID == "" {
				return forbidden()
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			var in ApplicationConnect
			return rewriteBody(r, &in, func() error {
				if in.PresetID == "" {
					return forbidden()
				}
				a, _, e := m.applicationPresetBinding(k.AppID, in.PresetID)
				if e != nil {
					return forbidden()
				}
				if !k.owns(a.InstanceID, a.WorkspaceID, SessionSource{Kind: "application", AppID: k.AppID, PresetID: in.PresetID}) {
					return forbidden()
				}
				return nil
			})
		}
		if (len(p) == 3 && p[2] == "runtime" || len(p) == 4 && p[2] == "runtime" && p[3] == "skills") && method == "GET" && k.scope("read") {
			a, _, e := m.applicationPresetBinding(k.AppID, r.URL.Query().Get("presetId"))
			if e == nil && k.owns(a.InstanceID, a.WorkspaceID, SessionSource{Kind: "application", AppID: k.AppID, PresetID: a.PresetID}) {
				return nil
			}
			return forbidden()
		}
		if len(p) == 4 && p[2] == "runtime" && method == "POST" && k.scope("run") {
			if p[3] == "prepare" {
				var in ApplicationRuntimeInput
				return rewriteBody(r, &in, func() error {
					a, _, e := m.applicationPresetBinding(k.AppID, in.PresetID)
					if e != nil || !k.owns(a.InstanceID, a.WorkspaceID, SessionSource{Kind: "application", AppID: k.AppID, PresetID: in.PresetID}) {
						return forbidden()
					}
					if in.PresetID != "" && ((in.InstanceID != "" && in.InstanceID != a.InstanceID) || (in.WorkspaceID != "" && in.WorkspaceID != a.WorkspaceID)) {
						return forbidden()
					}
					return nil
				})
			}
			if p[3] == "diagnostics" {
				var in ApplicationRuntimeDiagnosticsInput
				return rewriteBody(r, &in, func() error {
					a, _, e := m.applicationPresetBinding(k.AppID, in.PresetID)
					if e != nil || !k.owns(a.InstanceID, a.WorkspaceID, SessionSource{Kind: "application", AppID: k.AppID, PresetID: in.PresetID}) {
						return forbidden()
					}
					if in.PresetID != "" && (in.InstanceID != a.InstanceID || in.WorkspaceID != a.WorkspaceID) {
						return forbidden()
					}
					return nil
				})
			}
		}
	case "a2a":
		if len(p) == 2 && method == "POST" && k.scope("run") || len(p) == 3 && p[2] == "agent-card.json" && method == "GET" {
			a, e := s.registeredAgent(p[1])
			if e != nil {
				return forbidden()
			}
			if a.InstanceID == k.InstanceID && k.legacyWorkspace(a.WorkspaceID) {
				return nil
			}
		}
	case "requests":
		if len(p) == 2 && method == "GET" {
			return nil
		}
	case "workspaces":
		if len(p) == 3 && k.workspace(p[1]) && k.scope("files") && ((p[2] == "file" && method == "GET") || (p[2] == "uploads" && method == "POST")) {
			return nil
		}
	case "tasks":
		if len(p) > 3 {
			return forbidden()
		}
		t, e := s.taskControl().Task(p[1])
		if e != nil {
			return e
		}
		if !taskVisible(r, t) {
			return forbidden()
		}
		if len(p) == 2 && method == "GET" || len(p) == 3 && p[2] == "resources" && method == "GET" && k.scope("read") || len(p) == 3 && p[2] == "cancel" && method == "POST" && k.scope("run") {
			return nil
		}
	case "schedules":
		if !k.scope("schedules") {
			return forbidden()
		}
		if len(p) == 2 && p[1] == "preview" && method == "POST" {
			return nil
		}
		if len(p) != 2 {
			return forbidden()
		}
		item, e := m.Schedule(p[1])
		if e != nil {
			return e
		}
		if !scheduleVisible(r, item) {
			return forbidden()
		}
		switch method {
		case "GET", "DELETE":
			return nil
		case "PUT":
			var v struct {
				Spec     ScheduleSpec `json:"spec"`
				Revision int          `json:"revision"`
			}
			return rewriteBody(r, &v, func() error { return s.normalizeApplicationTask(r, k, &v.Spec.Task) })
		}
	case "sessions":
		item, e := m.Session(p[1])
		if e != nil {
			return e
		}
		if !sessionVisible(r, item) {
			return forbidden()
		}
		if len(p) == 2 {
			if method == "GET" || (method == "PATCH" || method == "DELETE") && k.scope("run") {
				return nil
			}
			return forbidden()
		}
		if len(p) >= 3 && p[2] == "debug" {
			if len(p) == 4 && method == "GET" && k.scope("read") && (p[3] == "query" || p[3] == "capabilities") {
				return nil
			}
			return forbidden()
		}
		if len(p) >= 4 && p[2] == "kun" {
			if method == "GET" && k.scope("read") && ((p[3] == "state" || p[3] == "checkpoint" || p[3] == "query") && len(p) == 4 || p[3] == "snapshots" && len(p) == 5) {
				return nil
			}
			if method == "POST" && p[3] == "resume" && len(p) == 4 && k.scope("run") {
				return nil
			}
			if method == "POST" && p[3] == "control" && len(p) == 4 && k.scope("run") {
				var command kunproto.Control
				return rewriteBody(r, &command, func() error {
					if command.Operation == "set_harness" {
						return forbidden()
					}
					if (command.Operation == "approve" || command.Operation == "reject") && !k.scope("approvals") {
						return forbidden()
					}
					return nil
				})
			}
			return forbidden()
		}
		if method == "GET" {
			if len(p) == 3 {
				switch p[2] {
				case "events", "trace", "export", "feedback", "approvals":
					return nil
				case "files":
					if k.scope("files") {
						return nil
					}
				}
			}
			if len(p) == 4 && (p[2] == "events" || p[2] == "messages") {
				return nil
			}
		}
		if method == "POST" && k.scope("run") {
			if (len(p) == 3 && (p[2] == "turns" || p[2] == "steer" || p[2] == "recover")) || (len(p) == 4 && p[2] == "recovery" && p[3] == "check") {
				if e := m.checkApplicationPresetExecution(item.Source, item.InstanceID, item.WorkspaceID); e != nil {
					return e
				}
			}
			if len(p) == 3 {
				switch p[2] {
				case "turns":
					var in Input
					return rewriteBody(r, &in, func() error { return s.checkInputFiles(r, item.WorkspaceID, in) })
				case "steer":
					var in SteerInput
					return rewriteBody(r, &in, func() error { return s.checkInputFiles(r, item.WorkspaceID, in.Input) })
				case "stop", "recover":
					return nil
				}
			}
			if len(p) == 4 && p[2] == "recovery" && p[3] == "check" {
				return nil
			}
			if len(p) == 4 && p[2] == "approvals" && k.scope("approvals") {
				return nil
			}
		}
	}
	return forbidden()
}
