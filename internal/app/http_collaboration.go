package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/shengjuntu/rundesk/internal/store"
	"io"
	"net/http"
	"strings"
)

func (s *Server) collaborationRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/collaboration/config", func(w http.ResponseWriter, r *http.Request) { v, e := m.collaborationConfig(); respond(w, v, e) })
	mux.HandleFunc("PUT /api/collaboration/config", func(w http.ResponseWriter, r *http.Request) {
		var v CollaborationConfig
		if !decode(w, r, &v) {
			return
		}
		x, e := m.saveCollaborationConfig(v)
		respond(w, x, e)
	})
	mux.HandleFunc("POST /api/collaboration/discover", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			URL      string `json:"url"`
			TokenEnv string `json:"tokenEnv"`
		}
		if !decode(w, r, &v) {
			return
		}
		if e := collaborationURL(v.URL); e != nil {
			respond(w, nil, e)
			return
		}
		if v.TokenEnv != "" && !envName.MatchString(v.TokenEnv) {
			respond(w, nil, fmt.Errorf("无效环境变量名"))
			return
		}
		var card map[string]any
		e := collaborationHTTP(r.Context(), "GET", v.URL, v.TokenEnv, nil, &card)
		if e == nil && card["protocolVersion"] != "0.3.0" {
			e = fmt.Errorf("目前只接入明确声明 A2A 0.3.0 的服务")
		}
		respond(w, card, e)
	})
	mux.HandleFunc("GET /api/collaborations", func(w http.ResponseWriter, r *http.Request) {
		m.collabMu.Lock()
		defer m.collabMu.Unlock()
		v, e := m.collaborations()
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/collaborations", func(w http.ResponseWriter, r *http.Request) {
		var in CollaborationInput
		if !decode(w, r, &in) {
			return
		}
		v, e := m.createCollaboration(in)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/collaborations/{cid}", func(w http.ResponseWriter, r *http.Request) {
		m.collabMu.Lock()
		defer m.collabMu.Unlock()
		v, e := m.collaboration(r.PathValue("cid"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/collaborations/{cid}/actions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Action string `json:"action"`
			Text   string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := m.updateCollaboration(r.PathValue("cid"), in.Action, in.Text)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/collaborations/{cid}/board", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Issue int64 `json:"issue"`
		}
		if !decode(w, r, &in) {
			return
		}
		m.collabMu.Lock()
		defer m.collabMu.Unlock()
		c, e := m.collaboration(r.PathValue("cid"))
		if e == nil {
			if in.Issue <= 0 || c.Board.URL == "" {
				e = fmt.Errorf("需要有效 Issue 编号和黑板配置")
			} else {
				var issue struct {
					Body    string `json:"body"`
					HTMLURL string `json:"html_url"`
				}
				e = collaborationHTTP(r.Context(), "GET", fmt.Sprintf("%s/issues/%d", boardBase(c.Board), in.Issue), c.Board.TokenEnv, nil, &issue)
				if e == nil {
					if !strings.Contains(issue.Body, "<!-- rundesk-collaboration:"+c.ID+" -->") {
						e = fmt.Errorf("Issue 不属于此协作任务")
					} else {
						c.Issue = in.Issue
						c.BoardURL = issue.HTMLURL
						c.NextSync = ""
						e = m.saveCollaboration(&c)
					}
				}
			}
		}
		respond(w, c, e)
	})

	mux.HandleFunc("POST /api/collaborations/{cid}/work/{work}/reconcile", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RemoteID string `json:"remoteId"`
		}
		if !decode(w, r, &in) {
			return
		}
		m.collabMu.Lock()
		defer m.collabMu.Unlock()
		c, e := m.collaboration(r.PathValue("cid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		found := false
		for n := range c.Work {
			item := &c.Work[n]
			if item.ID != r.PathValue("work") {
				continue
			}
			found = true
			if item.Status != "unconfirmed" || strings.TrimSpace(in.RemoteID) == "" {
				e = fmt.Errorf("仅允许核对状态不确定的远程任务")
				break
			}
			a, _ := c.agent(item.Agent)
			var t a2aTask
			t, e = a2aCall(r.Context(), a, "tasks/get", map[string]any{"id": in.RemoteID})
			if e != nil {
				break
			}
			if t.ID != in.RemoteID {
				e = fmt.Errorf("远程返回的任务 ID 不匹配")
				break
			}
			applyA2ATask(item, t)
			c.entry("reconcile", "user", "已人工核对远程任务："+in.RemoteID)
			c.Error = ""
			e = m.saveCollaboration(&c)
			break
		}
		if !found {
			e = fmt.Errorf("子任务不存在")
		}
		respond(w, c, e)
	})

	mux.HandleFunc("GET /api/a2a/{agent}/agent-card.json", s.collaborationCard)
	mux.HandleFunc("POST /api/a2a/{agent}", s.collaborationA2A)
}
func (s *Server) registeredAgent(id string) (CollaborationAgent, error) {
	cfg, e := s.Manager.collaborationConfig()
	if e != nil {
		return CollaborationAgent{}, e
	}
	for _, a := range cfg.Agents {
		if a.ID == id && a.Endpoint == "" {
			return a, nil
		}
	}
	return CollaborationAgent{}, fmt.Errorf("未登记本地 Agent")
}
func (s *Server) collaborationCard(w http.ResponseWriter, r *http.Request) {
	a, e := s.registeredAgent(r.PathValue("agent"))
	if e != nil {
		writeErr(w, 404, e)
		return
	}
	base := s.PublicOrigin
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	writeJSON(w, 200, map[string]any{"protocolVersion": "0.3.0", "name": a.Name, "description": a.Description, "url": base + "/api/v1/a2a/" + a.ID, "preferredTransport": "JSONRPC", "version": Version, "capabilities": map[string]bool{"streaming": false, "pushNotifications": false}, "defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"}, "skills": []any{map[string]any{"id": a.ID, "name": a.Name, "description": a.Description, "tags": []string{"rundesk"}}}, "securitySchemes": map[string]any{"bearer": map[string]string{"type": "http", "scheme": "bearer"}}, "security": []any{map[string]any{"bearer": []string{}}}})
}

type inboundA2A struct {
	Agent     string `json:"agent"`
	Scope     string `json:"scope"`
	Hash      string `json:"hash"`
	TaskID    string `json:"taskId"`
	ContextID string `json:"contextId"`
}

func a2aScope(r *http.Request) string {
	if k := principal(r); k != nil {
		return "app:" + k.ID
	}
	return "admin"
}
func (s *Server) collaborationA2A(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	reply := func(v any, code int, msg string) {
		out := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if code != 0 {
			out["error"] = map[string]any{"code": code, "message": msg}
		} else {
			out["result"] = v
		}
		writeJSON(w, 200, out)
	}
	if e := dec.Decode(&req); e != nil {
		req.ID = json.RawMessage("null")
		reply(nil, -32700, "Parse error")
		return
	}
	var trailing any
	var requestID any
	idErr := json.Unmarshal(req.ID, &requestID)
	validID := false
	switch requestID.(type) {
	case string, float64:
		validID = true
	}
	if req.JSONRPC != "2.0" || len(req.ID) == 0 || idErr != nil || !validID || dec.Decode(&trailing) != io.EOF {
		req.ID = json.RawMessage("null")
		reply(nil, -32600, "Invalid Request")
		return
	}
	a, e := s.registeredAgent(r.PathValue("agent"))
	if e != nil {
		reply(nil, -32602, "Unknown agent")
		return
	}
	m := s.Manager
	m.a2aMu.Lock()
	defer m.a2aMu.Unlock()
	scope := a2aScope(r)
	var record inboundA2A
	var task Task
	switch req.Method {
	case "message/send":
		var in struct {
			Message       a2aMessage `json:"message"`
			Configuration struct {
				Blocking bool            `json:"blocking"`
				Push     json.RawMessage `json:"pushNotificationConfig"`
			} `json:"configuration"`
		}
		if json.Unmarshal(req.Params, &in) != nil || in.Message.Kind != "message" || in.Message.Role != "user" || in.Message.MessageID == "" || len(in.Message.MessageID) > 200 {
			reply(nil, -32602, "Invalid message")
			return
		}
		if in.Configuration.Blocking {
			reply(nil, -32602, "Use configuration.blocking=false and tasks/get")
			return
		}
		if len(in.Configuration.Push) > 0 && string(in.Configuration.Push) != "null" {
			reply(nil, -32003, "Push notifications not supported")
			return
		}
		if in.Message.TaskID != "" {
			reply(nil, -32004, "Task continuation unavailable; submit a new task with contextId")
			return
		}
		text := []string{}
		for _, p := range in.Message.Parts {
			if p.Kind != "text" {
				reply(nil, -32005, "Only text/plain input is supported")
				return
			}
			text = append(text, p.Text)
		}
		input := strings.Join(text, "\n")
		if strings.TrimSpace(input) == "" || len(input) > 64000 {
			reply(nil, -32602, "Empty or oversized input")
			return
		}
		sum := sha256.Sum256([]byte(scope + "\x00" + a.ID + "\x00" + in.Message.MessageID))
		key := hex.EncodeToString(sum[:])
		var canonical any
		_ = json.Unmarshal(req.Params, &canonical)
		canonicalBytes, _ := json.Marshal(canonical)
		hash := sha256.Sum256(canonicalBytes)
		fingerprint := hex.EncodeToString(hash[:])
		e = m.Store.Get("a2a-message", key, &record)
		if e == nil {
			if record.Hash != fingerprint {
				reply(nil, -32602, "messageId already used with different parameters")
				return
			}
			task, e = m.Task(record.TaskID)
		} else if e == sql.ErrNoRows {
			contextID := in.Message.ContextID
			if contextID != "" {
				var binding inboundA2A
				if m.Store.Get("a2a-context", contextID, &binding) != nil || binding.Scope != scope || binding.Agent != a.ID {
					reply(nil, -32602, "Unknown contextId")
					return
				}
			} else {
				contextID = store.ID()
			}
			source := SessionSource{Kind: "human"}
			if k := principal(r); k != nil {
				source = SessionSource{Kind: "application", AppID: k.AppID}
			}
			var session Session
			task, session, e = m.prepareTask(TaskSpec{Title: "A2A · " + a.Name, WorkspaceID: a.WorkspaceID, InstanceID: a.InstanceID, Source: source, Input: Input{Text: input}}, true)
			if e == nil {
				record = inboundA2A{Agent: a.ID, Scope: scope, Hash: fingerprint, TaskID: task.ID, ContextID: contextID}
				m.queueMu.Lock()
				if m.pendingCountLocked() >= 1000 {
					e = fmt.Errorf("queue full")
				} else {
					e = m.Store.PutMany(store.Record{Kind: "task", ID: task.ID, Value: task}, store.Record{Kind: "session", ID: session.ID, Value: session}, store.Record{Kind: "a2a-message", ID: key, Value: record}, store.Record{Kind: "a2a-task", ID: task.ID, Value: record}, store.Record{Kind: "a2a-context", ID: contextID, Value: record})
					if e == nil {
						m.tasks[task.ID] = task
						m.mu.Lock()
						m.sessions[session.ID] = &session
						m.mu.Unlock()
					}
				}
				m.queueMu.Unlock()
			}
		}
	case "tasks/get", "tasks/cancel":
		var in struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(req.Params, &in) != nil || in.ID == "" {
			reply(nil, -32602, "Missing task id")
			return
		}
		if m.Store.Get("a2a-task", in.ID, &record) != nil || record.Scope != scope || record.Agent != a.ID {
			reply(nil, -32001, "Task not found")
			return
		}
		task, e = m.Task(in.ID)
		if e == nil && req.Method == "tasks/cancel" {
			if !taskPending(task.Status) {
				reply(nil, -32002, "Task is not cancelable")
				return
			}
			task, e = m.CancelTask(in.ID)
		}
	default:
		reply(nil, -32601, "Method not found")
		return
	}
	if e != nil {
		reply(nil, -32603, "Task could not be persisted or accessed")
		return
	}
	out := a2aTask{Kind: "task", ID: task.ID, ContextID: record.ContextID}
	out.Status.Timestamp = task.Updated
	switch task.Status {
	case "queued", "dispatching":
		out.Status.State = "submitted"
	case "running", "starting", "waiting", "stopping":
		out.Status.State = "working"
	case "completed":
		out.Status.State = "completed"
	case "canceled", "interrupted":
		out.Status.State = "canceled"
	default:
		out.Status.State = "failed"
	}
	if task.Reason != "" {
		out.Status.Message = &a2aMessage{Kind: "message", Role: "agent", MessageID: task.ID + "-status", Parts: []a2aPart{{Kind: "text", Text: task.Reason}}}
	}
	if task.Status == "completed" {
		result := m.collaborationReply(task.SessionID)
		b, _ := json.Marshal(map[string]any{"artifactId": task.ID + "-result", "parts": []a2aPart{{Kind: "text", Text: result}}})
		out.Artifacts = []json.RawMessage{b}
	}
	reply(out, 0, "")
}
