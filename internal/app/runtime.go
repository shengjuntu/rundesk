package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

type RuntimeNotice struct {
	Kind      string `json:"kind"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Count     int    `json:"count"`
	FirstSeen string `json:"firstSeen"`
	LastSeen  string `json:"lastSeen"`
}
type RuntimeStatus struct {
	ExecutionMode string          `json:"executionMode,omitempty"`
	EnvironmentID string          `json:"environmentId,omitempty"`
	ID            string          `json:"id"`
	InstanceID    string          `json:"instanceId"`
	WorkspaceID   string          `json:"workspaceId"`
	ConnectionID  string          `json:"connectionId"`
	ConnectedAt   string          `json:"connectedAt"`
	ObservedAt    string          `json:"observedAt,omitempty"`
	CodexHome     string          `json:"codexHome"`
	UserAgent     string          `json:"userAgent,omitempty"`
	Live          bool            `json:"live"`
	Requested     *Permissions    `json:"requested,omitempty"`
	Effective     map[string]any  `json:"effective,omitempty"`
	Notices       []RuntimeNotice `json:"notices"`
}

func (m *Manager) beginRuntime(id string, w Workspace, i Instance) string {
	key := store.ID()
	v := RuntimeStatus{ID: id, InstanceID: i.ID, WorkspaceID: w.ID, ConnectionID: key, ConnectedAt: store.Now(), CodexHome: i.CodexHome, Notices: []RuntimeNotice{}}
	v.ExecutionMode = i.Execution.normalized().Mode
	if v.ExecutionMode == "docker" {
		v.EnvironmentID = environmentID(i.ID, w.ID)
	}
	m.runtimeMu.Lock()
	err := m.Store.Put("runtime", id, v)
	m.runtimeMu.Unlock()
	if err != nil {
		log.Print(err)
	}
	return key
}

func (m *Manager) changeRuntime(id, key string, change func(*RuntimeStatus)) {
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	var v RuntimeStatus
	if m.Store.Get("runtime", id, &v) != nil || (key != "" && v.ConnectionID != key) {
		return
	}
	change(&v)
	if err := m.Store.Put("runtime", id, v); err != nil {
		log.Print(err)
	}
}

func (m *Manager) observeRuntime(id, key, direction string, msg rpc.Message) {
	if direction == "out" {
		return
	}
	var p map[string]any
	_ = json.Unmarshal(msg.Params, &p)
	if msg.Method == "" && len(msg.Result) > 0 {
		var result struct {
			UserAgent string `json:"userAgent"`
		}
		if json.Unmarshal(msg.Result, &result) == nil && result.UserAgent != "" {
			m.changeRuntime(id, key, func(v *RuntimeStatus) { v.UserAgent = result.UserAgent; v.Live = true })
		}
		return
	}
	message, kind, level := "", "配置", "warning"
	str := func(k string) string { s, _ := p[k].(string); return s }
	switch msg.Method {
	case "configWarning":
		message = str("summary")
		if details := str("details"); details != "" {
			message += "\n" + details
		}
	case "warning", "deprecationNotice":
		message = str("message")
		if message == "" {
			message = str("summary")
		}
	case "error":
		kind, level = "运行", "error"
		b, _ := json.Marshal(redact(p["error"]))
		message = string(b)
	case "process/stderr":
		message = str("text")
		// Structured startup warnings are captured above; avoid duplicate copies.
		if strings.Contains(message, "Codex could not find bubblewrap") {
			return
		}
		if !strings.Contains(message, "bwrap:") && !strings.Contains(message, "SandboxError") && !strings.Contains(message, "Failed RTM_NEWADDR") {
			return
		}
		kind, level = "沙箱", "error"
	case "item/completed":
		item, _ := p["item"].(map[string]any)
		if item["type"] != "commandExecution" {
			return
		}
		if item["status"] == "declined" {
			kind, level, message = "审批", "info", "命令被拒绝；未执行。"
		} else if item["status"] == "failed" {
			kind, level = "命令", "error"
			message, _ = item["aggregatedOutput"].(string)
			if message == "" {
				message = "命令执行失败；请查看对应工具事件。"
			}
		} else {
			return
		}
	default:
		return
	}
	if strings.Contains(message, "Model metadata") {
		kind = "模型配置"
	}
	if strings.Contains(message, "bubblewrap") || strings.Contains(message, "bwrap:") {
		kind = "沙箱"
	}
	if message == "" {
		return
	}
	if len(message) > 8192 {
		message = message[:8192] + "\n…完整内容见事件日志"
	}
	m.changeRuntime(id, key, func(v *RuntimeStatus) {
		now := store.Now()
		for n := range v.Notices {
			if v.Notices[n].Source == msg.Method && v.Notices[n].Message == message {
				v.Notices[n].Count++
				v.Notices[n].LastSeen = now
				return
			}
		}
		v.Notices = append(v.Notices, RuntimeNotice{Kind: kind, Level: level, Source: msg.Method, Message: message, Count: 1, FirstSeen: now, LastSeen: now})
		if len(v.Notices) > 32 {
			v.Notices = v.Notices[len(v.Notices)-32:]
		}
	})
}

func (m *Manager) recordEffective(id string, raw json.RawMessage, requested Permissions) {
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	effective := map[string]any{}
	for _, k := range []string{"model", "modelProvider", "cwd", "sandbox", "approvalPolicy", "approvalsReviewer", "activePermissionProfile", "runtimeWorkspaceRoots", "reasoningEffort"} {
		if value, ok := p[k]; ok {
			effective[k] = value
		}
	}
	m.changeRuntime(id, "", func(v *RuntimeStatus) { v.Effective = effective; v.Requested = &requested; v.ObservedAt = store.Now() })
	m.event(id, "internal", "runtime/effective", map[string]any{"effective": effective, "requested": requested})
}

func (m *Manager) Runtime(id string) (RuntimeStatus, error) {
	var v RuntimeStatus
	if err := m.Store.Get("runtime", id, &v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RuntimeStatus{ID: id, Notices: []RuntimeNotice{}}, nil
		}
		return RuntimeStatus{}, err
	}
	// Persisted snapshots remain visible after restart, but aren't live connections.
	m.mu.Lock()
	h := m.handles[id]
	m.mu.Unlock()
	if h == nil {
		v.Live = false
	} else {
		h.mu.Lock()
		c := h.client
		k := h.kun
		h.mu.Unlock()
		if k != nil {
			select {
			case <-k.Done():
				v.Live = false
			default:
				v.Live = true
			}
			return v, nil
		}
		if c == nil {
			v.Live = false
		} else {
			select {
			case <-c.Done():
				v.Live = false
			default:
			}
		}
	}
	return v, nil
}

func (m *Manager) InstanceRuntime(iid string) ([]RuntimeStatus, error) {
	if _, err := m.Instance(iid); err != nil {
		return nil, err
	}
	rows, err := m.Store.List("runtime")
	if err != nil {
		return nil, err
	}
	out := []RuntimeStatus{}
	for _, row := range rows {
		var v RuntimeStatus
		if json.Unmarshal(row, &v) == nil && v.InstanceID == iid {
			v, _ = m.Runtime(v.ID)
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConnectedAt > out[j].ConnectedAt })
	if len(out) > 50 {
		out = out[:50]
	}
	return out, nil
}

func (s *Server) runtimeRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/sessions/{sid}/runtime", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("sid")
		if _, err := m.Session(id); err != nil {
			respond(w, nil, err)
			return
		}
		v, err := m.Runtime(id)
		respond(w, v, err)
	})
	mux.HandleFunc("GET /api/instances/{iid}/runtime", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.InstanceRuntime(r.PathValue("iid"))
		respond(w, v, err)
	})
}
