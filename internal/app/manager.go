package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

type Workspace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Notes    string `json:"notes"`
	Revision int    `json:"revision"`
}
type Session struct {
	ID          string        `json:"id"`
	WorkspaceID string        `json:"workspaceId"`
	InstanceID  string        `json:"instanceId"`
	Title       string        `json:"title"`
	ThreadID    string        `json:"threadId"`
	Model       string        `json:"model"`
	Status      string        `json:"status"`
	RunID       string        `json:"runId"`
	TurnID      string        `json:"turnId"`
	Error       string        `json:"error,omitempty"`
	Created     string        `json:"created"`
	Updated     string        `json:"updated"`
	Pinned      bool          `json:"pinned"`
	Archived    bool          `json:"archived"`
	Source      SessionSource `json:"source"`
}
type Approval struct {
	ID         string      `json:"id"`
	SessionID  string      `json:"sessionId"`
	RunID      string      `json:"runId"`
	Request    rpc.Message `json:"request"`
	Status     string      `json:"status"`
	Decisions  []any       `json:"decisions,omitempty"`
	Decision   any         `json:"decision,omitempty"`
	Scope      string      `json:"scope,omitempty"`
	ResolvedAt string      `json:"resolvedAt,omitempty"`
}
type Input struct {
	Text   string   `json:"text"`
	Files  []string `json:"files"`
	Skills []Skill  `json:"skills"`
}
type Skill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type handle struct {
	instanceID     string
	permissionsKey string
	op             sync.Mutex
	admission      sync.Mutex
	mu             sync.Mutex
	client         *rpc.Client
	thread         string
	canceled       bool
	last           time.Time
	requests       map[string]Approval
}
type Manager struct {
	Store           *store.Store
	Data            string
	Codex           string
	Demo            bool
	mu              sync.Mutex
	sessions        map[string]*Session
	handles         map[string]*handle
	instances       map[string]*Instance
	workspaces      map[string]*Workspace
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	loaded          atomic.Int32
	runtimeMu       sync.Mutex
	requestMu       sync.Mutex
	requestOwner    string
	pendingRequests map[string]bool
}

func New(data, codex string, demo bool) (*Manager, error) {
	data, e := filepath.Abs(data)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(data, 0700); e != nil {
		return nil, e
	}
	s, e := store.Open(filepath.Join(data, "state.db"))
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{requestOwner: store.ID(), pendingRequests: map[string]bool{}, Store: s, Data: data, Codex: codex, Demo: demo, sessions: map[string]*Session{}, handles: map[string]*handle{}, workspaces: map[string]*Workspace{}, instances: map[string]*Instance{}, ctx: ctx, cancel: cancel}
	fail := func(e error) (*Manager, error) { cancel(); s.Close(); return nil, e }
	if e := m.loadInstances(); e != nil {
		return fail(e)
	}
	rows, e := s.List("workspace")
	if e != nil {
		return fail(e)
	}
	for _, b := range rows {
		var w Workspace
		if e = json.Unmarshal(b, &w); e != nil {
			return fail(e)
		}
		m.workspaces[w.ID] = &w
	}
	rows, e = s.List("session")
	if e != nil {
		return fail(e)
	}
	for _, b := range rows {
		var v Session
		if e = json.Unmarshal(b, &v); e != nil {
			return fail(e)
		}
		if v.InstanceID == "" {
			v.InstanceID = DefaultInstance
			if e = s.Put("session", v.ID, v); e != nil {
				return fail(e)
			}
		}
		if _, e := m.Instance(v.InstanceID); e != nil {
			return fail(e)
		}
		if active(v.Status) {
			v.Status = "interrupted"
			v.Error = "后台已重启；任务未自动重跑。"
			if e = s.Put("session", v.ID, v); e != nil {
				return fail(e)
			}
		}
		m.sessions[v.ID] = &v
	}
	rows, e = s.List("approval")
	if e != nil {
		return fail(e)
	}
	for _, b := range rows {
		var a Approval
		if e = json.Unmarshal(b, &a); e != nil {
			return fail(e)
		}
		if a.Status == "pending" {
			a.Status = "expired"
			if e = s.Put("approval", a.ID, a); e != nil {
				return fail(e)
			}
		}
	}
	if len(m.workspaces) == 0 {
		if _, e = m.CreateWorkspace("默认工作区", ""); e != nil {
			return fail(e)
		}
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.reap()
			}
		}
	}()
	return m, nil
}
func active(s string) bool {
	return s == "starting" || s == "running" || s == "waiting" || s == "stopping"
}
func (m *Manager) Workspaces() []Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Workspace{}
	for _, w := range m.workspaces {
		out = append(out, *w)
	}
	return out
}
func (m *Manager) Workspace(id string) (Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workspaces[id]
	if w == nil {
		return Workspace{}, failure(404, "workspace_not_found", "工作区不存在")
	}
	return *w, nil
}
func (m *Manager) CreateWorkspace(name, path string) (Workspace, error) {
	id := store.ID()
	if name == "" {
		name = "工作区"
	}
	if path == "" {
		path = filepath.Join(m.Data, "workspaces", id)
	}
	path, e := filepath.Abs(path)
	if e != nil {
		return Workspace{}, e
	}
	if e = os.MkdirAll(path, 0700); e != nil {
		return Workspace{}, e
	}
	path, e = filepath.EvalSymlinks(path)
	if e != nil {
		return Workspace{}, e
	}
	w := Workspace{ID: id, Name: name, Path: path}
	if e = m.Store.Put("workspace", id, w); e != nil {
		return w, e
	}
	m.mu.Lock()
	m.workspaces[id] = &w
	m.mu.Unlock()
	return w, nil
}
func (m *Manager) Notes(id, text string, rev int) (Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workspaces[id]
	if w == nil {
		return Workspace{}, failure(404, "workspace_not_found", "工作区不存在")
	}
	if rev != w.Revision {
		return *w, errors.New("笔记已被更新，请重新加载")
	}
	if len(text) > 16384 {
		return *w, errors.New("笔记最多 16 KiB")
	}
	v := *w
	v.Notes = text
	v.Revision++
	if e := m.Store.Put("workspace", id, v); e != nil {
		return *w, e
	}
	*w = v
	return v, nil
}
func (m *Manager) Sessions() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Session{}
	for _, s := range m.sessions {
		out = append(out, *s)
	}
	return out
}
func (m *Manager) Session(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return Session{}, failure(404, "session_not_found", "会话不存在")
	}
	return *s, nil
}
func (m *Manager) CreateSession(wid, title, model string, ids ...string) (Session, error) {
	return m.CreateSessionWithSource(wid, title, model, instanceID(ids), SessionSource{})
}
func (m *Manager) CreateSessionWithSource(wid, title, model, iid string, source SessionSource) (Session, error) {
	if err := source.validate(); err != nil {
		return Session{}, err
	}
	if _, e := m.Workspace(wid); e != nil {
		return Session{}, e
	}
	if title == "" {
		title = "新对话"
	}
	i, err := m.Instance(iid)
	if err != nil {
		return Session{}, err
	}
	if model == "" {
		model = i.DefaultModel
	}
	s := Session{Source: source, ID: store.ID(), InstanceID: i.ID, WorkspaceID: wid, Title: title, Model: model, Status: "idle", Created: store.Now(), Updated: store.Now()}
	if e := m.Store.Put("session", s.ID, s); e != nil {
		return s, e
	}
	m.mu.Lock()
	m.sessions[s.ID] = &s
	m.mu.Unlock()
	return s, nil
}
func (m *Manager) update(id string, f func(*Session)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return failure(404, "session_not_found", "会话不存在")
	}
	v := *s
	f(&v)
	v.Updated = store.Now()
	if e := m.Store.Put("session", id, v); e != nil {
		return e
	}
	*s = v
	return nil
}
func (m *Manager) event(id, dir, method string, data any) {
	if _, e := m.Store.Add(id, dir, method, redact(data)); e != nil {
		log.Printf("journal failure: %v", e)
		m.cancel()
	}
}

// Structured secrets are removed. Arbitrary prose/tool output may still contain sensitive data.
func redact(v any) any {
	b, _ := json.Marshal(v)
	var x any
	_ = json.Unmarshal(b, &x)
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, v := range t {
				if sensitiveField(k) {
					t[k] = "[redacted]"
				} else {
					t[k] = walk(v)
				}
			}
		case []any:
			for i, v := range t {
				t[i] = walk(v)
			}
		}
		return x
	}
	return walk(x)
}
func sensitiveField(key string) bool {
	key = strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	switch key {
	case "env", "httpheaders", "authorization", "accesstoken", "refreshtoken", "apikey", "password", "bearertoken", "idtoken":
		return true
	}
	return false
}
func (m *Manager) getHandle(id string) (*handle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h := m.handles[id]; h != nil {
		return h, nil
	}
	h := &handle{last: time.Now(), requests: map[string]Approval{}}
	m.handles[id] = h
	return h, nil
}
func (m *Manager) command(w Workspace, i Instance) *exec.Cmd {
	var c *exec.Cmd
	if m.Demo {
		exe, _ := os.Executable()
		c = exec.Command(exe, "__demo_agent")
	} else {
		c = exec.Command(m.Codex, "app-server")
	}
	c.Dir = w.Path
	// Preserve the service environment; only managed Codex state is redirected.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.EqualFold(key, "CODEX_HOME") || ((i.Managed || m.Demo) && strings.EqualFold(key, "CODEX_SQLITE_HOME")) {
			continue
		}
		c.Env = append(c.Env, item)
	}
	c.Env = append(c.Env, "CODEX_HOME="+i.CodexHome)
	return c
}

// Caller holds h.op. A connection is reused across turns.
func (m *Manager) connect(id string, w Workspace, h *handle, ids ...string) (*rpc.Client, error) {
	i, err := m.Instance(ids...)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	c := h.client
	if c != nil {
		select {
		case <-c.Done():
			c = nil
		default:
		}
	}
	h.last = time.Now()
	h.mu.Unlock()
	if c != nil {
		return c, nil
	}
	if m.loaded.Add(1) > 16 {
		m.loaded.Add(-1)
		return nil, errors.New("最多加载 16 个会话/配置连接；空闲连接十分钟后释放")
	}
	runtimeKey := m.beginRuntime(id, w, i)
	c, e := rpc.Start(m.command(w, i), func(msg rpc.Message) { m.onMessage(id, h, msg) }, func(dir string, msg rpc.Message) {
		m.event(id, dir, msg.Method, msg)
		m.observeRuntime(id, runtimeKey, dir, msg)
	})
	if e != nil {
		m.loaded.Add(-1)
		return nil, e
	}
	m.wg.Add(1)
	go func() { defer m.wg.Done(); <-c.Done(); m.loaded.Add(-1) }()
	h.mu.Lock()
	h.instanceID = i.ID
	h.client = c
	h.thread = ""
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()
	if e = c.Initialize(ctx); e != nil {
		c.Close()
		return nil, e
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		<-c.Done()
		h.op.Lock()
		defer h.op.Unlock()
		h.mu.Lock()
		same := h.client == c
		if same {
			h.client = nil
			h.thread = ""
		}
		h.mu.Unlock()
		if same {
			m.expire(id, h)
			s, e := m.Session(id)
			if e == nil && active(s.Status) && s.Status != "starting" {
				h.mu.Lock()
				canceled := h.canceled || m.ctx.Err() != nil
				h.mu.Unlock()
				if canceled {
					m.finish(id, "interrupted", "")
				} else {
					m.finish(id, "failed", "Codex App Server 连接已断开")
				}
			}
		}
	}()
	return c, nil
}
func (m *Manager) onMessage(id string, h *handle, msg rpc.Message) {
	var p map[string]json.RawMessage
	_ = json.Unmarshal(msg.Params, &p)
	if len(msg.ID) > 0 && msg.Method != "" {
		switch msg.Method {
		case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "item/tool/requestUserInput", "mcpServer/elicitation/request":
			s, _ := m.Session(id)
			a := Approval{ID: store.ID(), SessionID: id, RunID: s.RunID, Request: msg, Status: "pending"}
			h.mu.Lock()
			h.requests[a.ID] = a
			h.mu.Unlock()
			saved := a
			saved.Request.Params, _ = json.Marshal(redact(json.RawMessage(msg.Params)))
			if e := m.Store.Put("approval", a.ID, saved); e != nil {
				m.cancel()
				return
			}
			_ = m.update(id, func(s *Session) {
				if active(s.Status) {
					s.Status = "waiting"
				}
			})
			m.event(id, "internal", "approval/pending", saved)
		default:
			go func() {
				h.mu.Lock()
				c := h.client
				h.mu.Unlock()
				if c != nil {
					_ = c.Send(rpc.Message{ID: msg.ID, Error: &rpc.Error{Code: -32601, Message: "RunDesk does not implement " + msg.Method}})
				}
			}()
		}
		return
	}
	switch msg.Method {
	case "turn/started":
		var t struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(p["turn"], &t)
		_ = m.update(id, func(s *Session) {
			s.TurnID = t.ID
			if s.Status == "starting" {
				s.Status = "running"
			}
		})
	case "turn/completed":
		var t struct {
			Status string `json:"status"`
			Error  any    `json:"error"`
		}
		_ = json.Unmarshal(p["turn"], &t)
		status := t.Status
		if status == "" {
			status = "completed"
		}
		errText := ""
		if t.Error != nil {
			b, _ := json.Marshal(t.Error)
			errText = string(b)
		}
		m.finish(id, status, errText)
		m.expire(id, h)
	case "serverRequest/resolved":
		h.mu.Lock()
		for aid, a := range h.requests {
			if string(a.Request.ID) == string(p["requestId"]) {
				a.Status = "resolved"
				a.Request.Params, _ = json.Marshal(redact(a.Request.Params))
				_ = m.Store.Put("approval", aid, a)
				delete(h.requests, aid)
			}
		}
		empty := len(h.requests) == 0
		h.mu.Unlock()
		if empty {
			_ = m.update(id, func(s *Session) {
				if s.Status == "waiting" {
					s.Status = "running"
				}
			})
		}
	}
}
func (m *Manager) finish(id, status, errText string) {
	if e := m.update(id, func(s *Session) { s.Status = status; s.Error = errText }); e != nil {
		log.Print(e)
	}
	m.event(id, "internal", "run/state", map[string]string{"status": status, "error": errText})
}
func (m *Manager) expire(id string, h *handle) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for aid, a := range h.requests {
		a.Status = "expired"
		a.Request.Params, _ = json.Marshal(redact(a.Request.Params))
		_ = m.Store.Put("approval", aid, a)
		delete(h.requests, aid)
	}
	m.event(id, "internal", "approval/expired", map[string]string{})
}
func (m *Manager) Start(id string, in Input) (Session, error) {
	if strings.TrimSpace(in.Text) == "" {
		return Session{}, errors.New("请输入消息")
	}
	if len(in.Text) > 256*1024 {
		return Session{}, errors.New("消息过长")
	}
	s, e := m.Session(id)
	if e != nil {
		return s, e
	}
	w, e := m.Workspace(s.WorkspaceID)
	if e != nil {
		return s, e
	}
	if e = m.validateInput(w, in, s.InstanceID); e != nil {
		return s, e
	}
	h, e := m.getHandle(id)
	if e != nil {
		return s, e
	}
	h.op.Lock()
	defer h.op.Unlock()
	h.admission.Lock()
	defer h.admission.Unlock()
	s, _ = m.Session(id)
	if s.Archived {
		return s, failure(409, "session_archived", "请先恢复已归档会话，再提交任务")
	}
	if active(s.Status) {
		return s, failure(409, "session_busy", "该会话已有任务正在运行")
	}
	h.mu.Lock()
	h.canceled = false
	h.last = time.Now()
	h.mu.Unlock()
	if e = m.update(id, func(s *Session) {
		s.RunID = store.ID()
		s.TurnID = ""
		s.Status = "starting"
		s.Error = ""
		if s.Title == "新对话" {
			r := []rune(in.Text)
			if len(r) > 30 {
				r = r[:30]
			}
			s.Title = string(r)
		}
	}); e != nil {
		return s, e
	}
	s, _ = m.Session(id)
	instanceAtSubmit, _ := m.Instance(s.InstanceID)
	m.event(id, "internal", "run/input", map[string]any{"instanceRevision": instanceAtSubmit.Revision, "runId": s.RunID, "input": in, "notes": w.Notes, "notesRevision": w.Revision, "cwd": w.Path, "instanceId": s.InstanceID, "model": s.Model})
	m.wg.Add(1)
	go func() { defer m.wg.Done(); h.op.Lock(); defer h.op.Unlock(); m.run(s, w, h, in) }()
	return s, nil
}
func (m *Manager) run(s Session, w Workspace, h *handle, in Input) {
	fail := func(e error) {
		h.mu.Lock()
		c := h.client
		h.client = nil
		h.thread = ""
		canceled := h.canceled || m.ctx.Err() != nil
		h.mu.Unlock()
		if c != nil {
			c.Close()
			<-c.Done()
		}
		m.expire(s.ID, h)
		if canceled {
			m.finish(s.ID, "interrupted", "")
		} else {
			m.finish(s.ID, "failed", e.Error())
		}
	}
	i, e := m.Instance(s.InstanceID)
	if e != nil {
		fail(e)
		return
	}
	permissions := i.Permissions.normalized()
	h.mu.Lock()
	old := h.client
	changed := old != nil && h.permissionsKey != "" && h.permissionsKey != permissions.key()
	if changed {
		h.client = nil
		h.thread = ""
	}
	h.mu.Unlock()
	if changed {
		old.Close()
		<-old.Done()
	}
	c, e := m.connect(s.ID, w, h, s.InstanceID)
	if e != nil {
		fail(e)
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, 90*time.Second)
	defer cancel()
	h.mu.Lock()
	canceled := h.canceled
	h.mu.Unlock()
	if canceled {
		m.finish(s.ID, "interrupted", "")
		return
	}
	// A fresh process must resume the native thread before it can accept a turn.
	h.mu.Lock()
	ready := h.thread != "" && h.thread == s.ThreadID
	h.mu.Unlock()
	var raw json.RawMessage
	if !ready {
		params := permissions.threadParams(w.Path)
		if s.Model != "" {
			params["model"] = s.Model
		}
		method := "thread/start"
		if s.ThreadID != "" {
			method = "thread/resume"
			params["threadId"] = s.ThreadID
			params["excludeTurns"] = true
		}
		raw, e = c.Call(ctx, method, params)
		if e != nil {
			fail(e)
			return
		}
		var res struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if e = json.Unmarshal(raw, &res); e != nil || res.Thread.ID == "" {
			fail(errors.New("thread/start/resume 未返回 thread.id"))
			return
		}
		s.ThreadID = res.Thread.ID
		if e = m.update(s.ID, func(v *Session) { v.ThreadID = s.ThreadID }); e != nil {
			fail(e)
			return
		}
		h.mu.Lock()
		h.thread = s.ThreadID
		h.permissionsKey = permissions.key()
		h.mu.Unlock()
		m.recordEffective(s.ID, raw, permissions)
	}
	// Reload queues updated MCP configuration for the next turn on this thread.
	if _, e = c.Call(ctx, "config/mcpServer/reload", map[string]any{}); e != nil {
		fail(fmt.Errorf("MCP reload: %w", e))
		return
	}
	root, e := os.OpenRoot(w.Path)
	if e != nil {
		fail(e)
		return
	}
	e = root.MkdirAll("outputs/"+s.ID, 0700)
	root.Close()
	if e != nil {
		fail(e)
		return
	}
	text := in.Text
	if w.Notes != "" {
		text += "\n\n<project-notes revision=\"" + fmt.Sprint(w.Revision) + "\">\n" + w.Notes + "\n</project-notes>"
	}
	text += "\n\n[Application context] Save deliverable files in " + filepath.Join(w.Path, "outputs", s.ID) + ". Uploaded files, if any, are listed below."
	input := []any{}
	for _, f := range in.Files {
		path := filepath.Join(w.Path, filepath.FromSlash(f))
		text += "\nUploaded file: " + path
		ext := strings.ToLower(filepath.Ext(f))
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			input = append(input, map[string]string{"type": "localImage", "path": path})
		}
	}
	input = append(input, map[string]string{"type": "text", "text": text})
	for _, sk := range in.Skills {
		input = append(input, map[string]string{"type": "skill", "name": sk.Name, "path": sk.Path})
	}
	h.mu.Lock()
	canceled = h.canceled
	h.mu.Unlock()
	if canceled {
		m.finish(s.ID, "interrupted", "")
		return
	}
	raw, e = c.Call(ctx, "turn/start", map[string]any{"threadId": s.ThreadID, "input": input})
	if e != nil {
		fail(e)
		return
	}
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(raw, &result)
	_ = m.update(s.ID, func(v *Session) {
		if v.TurnID == "" {
			v.TurnID = result.Turn.ID
		}
		if v.Status == "starting" {
			v.Status = "running"
		}
	})
	h.mu.Lock()
	canceled = h.canceled
	h.mu.Unlock()
	if canceled {
		go m.interrupt(s.ID, h, s.RunID)
	}
}
func (m *Manager) Stop(id string) error { return m.StopRun(id, "") }
func (m *Manager) StopRun(id, expectedRunID string) error {
	if _, err := m.Session(id); err != nil {
		return err
	}
	h, e := m.getHandle(id)
	if e != nil {
		return e
	}
	h.admission.Lock()
	defer h.admission.Unlock()
	s, e := m.Session(id)
	if e != nil {
		return e
	}
	if expectedRunID != "" && s.RunID != expectedRunID {
		return failure(409, "run_conflict", "会话已切换到其他任务；停止请求未执行")
	}
	if !active(s.Status) {
		if expectedRunID != "" {
			return nil
		}
		return errors.New("没有正在运行的任务")
	}
	if s.Status == "stopping" {
		return nil
	}
	if e = m.update(id, func(s *Session) { s.Status = "stopping" }); e != nil {
		return e
	}
	h.mu.Lock()
	h.canceled = true
	if s.Status == "starting" && s.TurnID == "" && h.client != nil {
		h.client.Close()
	}
	h.mu.Unlock()
	go m.interrupt(id, h, s.RunID)
	return nil
}
func (m *Manager) interrupt(id string, h *handle, expectedRunIDs ...string) {
	h.op.Lock()
	defer h.op.Unlock()
	s, e := m.Session(id)
	if e != nil || !active(s.Status) || (len(expectedRunIDs) > 0 && s.RunID != expectedRunIDs[0]) {
		return
	}
	h.mu.Lock()
	c := h.client
	h.mu.Unlock()
	if c == nil || s.TurnID == "" {
		m.finish(id, "interrupted", "")
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	_, e = c.Call(ctx, "turn/interrupt", map[string]string{"threadId": s.ThreadID, "turnId": s.TurnID})
	if e != nil {
		c.Close()
		m.finish(id, "interrupted", e.Error())
	}
	m.expire(id, h)
}
func (m *Manager) Approvals(id string) []Approval {
	rows, e := m.Store.List("approval")
	if e != nil {
		return nil
	}
	out := []Approval{}
	for _, b := range rows {
		var a Approval
		_ = json.Unmarshal(b, &a)
		if a.SessionID == id && a.Status == "pending" {
			a.Decisions = approvalDecisions(a.Request)
			out = append(out, a)
		}
	}
	return out
}
func (m *Manager) Approve(id, aid string, decision any, answers map[string]any, content any, scopes ...string) error {
	h, e := m.getHandle(id)
	if e != nil {
		return e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	a, ok := h.requests[aid]
	if !ok {
		return errors.New("审批已处理或过期")
	}
	scope := "turn"
	if len(scopes) > 0 && scopes[0] != "" {
		scope = scopes[0]
	}
	result, e := approvalResult(a.Request, decision, answers, content, scope)
	if e != nil {
		return e
	}
	scope = ""
	if response, ok := result.(map[string]any); ok {
		scope, _ = response["scope"].(string)
	}
	if h.client == nil {
		return errors.New("连接已关闭")
	}
	if e = h.client.Reply(a.Request.ID, result); e != nil {
		return e
	}
	delete(h.requests, aid)
	a.Decision, a.Scope, a.ResolvedAt = decision, scope, store.Now()
	a.Status = "resolved"
	a.Request.Params, _ = json.Marshal(redact(a.Request.Params))
	if e = m.Store.Put("approval", aid, a); e != nil {
		return e
	}
	_ = m.update(id, func(s *Session) {
		if s.Status == "waiting" && len(h.requests) == 0 {
			s.Status = "running"
		}
	})
	m.event(id, "internal", "approval/resolved", map[string]any{"id": aid, "decision": decision, "scope": scope, "resolvedAt": a.ResolvedAt})
	return nil
}
func (m *Manager) ConfigCall(wid, method string, params any, ids ...string) (json.RawMessage, error) {
	w, e := m.Workspace(wid)
	if e != nil {
		return nil, e
	}
	i, e := m.Instance(ids...)
	if e != nil {
		return nil, e
	}
	id := "config-" + i.ID + "-" + wid
	h, e := m.getHandle(id)
	if e != nil {
		return nil, e
	}
	h.op.Lock()
	defer h.op.Unlock()
	c, e := m.connect(id, w, h, i.ID)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	defer cancel()
	return c.Call(ctx, method, params)
}
func (m *Manager) reap() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, h := range m.handles {
		if !h.op.TryLock() {
			continue
		}
		if !h.mu.TryLock() {
			h.op.Unlock()
			continue
		}
		s := m.sessions[id]
		if (s == nil || !active(s.Status)) && time.Since(h.last) > 10*time.Minute {
			if h.client != nil {
				h.client.Close()
			}
			delete(m.handles, id)
		}
		h.mu.Unlock()
		h.op.Unlock()
	}
}
func (m *Manager) Done() <-chan struct{} { return m.ctx.Done() }
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	hs := []*handle{}
	for _, h := range m.handles {
		hs = append(hs, h)
	}
	m.mu.Unlock()
	for _, h := range hs {
		h.mu.Lock()
		c := h.client
		h.mu.Unlock()
		if c != nil {
			c.Close()
			<-c.Done()
		}
	}
	m.wg.Wait()
	_ = m.Store.Close()
}
