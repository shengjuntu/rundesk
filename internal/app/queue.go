package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/shengjuntu/rundesk/internal/store"
	"sort"
	"strings"
	"time"
)

type TaskSpec struct {
	SubmittingKeyID string        `json:"-"`
	Title           string        `json:"title"`
	WorkspaceID     string        `json:"workspaceId"`
	InstanceID      string        `json:"instanceId"`
	Model           string        `json:"model"`
	Source          SessionSource `json:"source"`
	Input           Input         `json:"input"`
	NotBefore       string        `json:"notBefore,omitempty"`
}
type Task struct {
	SubmittingKeyID string   `json:"submittingKeyId,omitempty"`
	FileOwner       string   `json:"fileOwner,omitempty"`
	ID              string   `json:"id"`
	SessionID       string   `json:"sessionId"`
	RunID           string   `json:"runId"`
	Spec            TaskSpec `json:"spec"`
	Status          string   `json:"status"`
	Reason          string   `json:"reason,omitempty"`
	CancelRequested bool     `json:"cancelRequested"`
	Created         string   `json:"created"`
	Updated         string   `json:"updated"`
	StartedAt       string   `json:"startedAt,omitempty"`
	FinishedAt      string   `json:"finishedAt,omitempty"`
	ScheduleID      string   `json:"scheduleId,omitempty"`
	ScheduledFor    string   `json:"scheduledFor,omitempty"`
}
type QueueSettings struct {
	Revision      int            `json:"revision"`
	Paused        bool           `json:"paused"`
	MaxConcurrent int            `json:"maxConcurrent"`
	PerInstance   map[string]int `json:"perInstance"`
}

func taskPending(s string) bool { return s == "queued" || s == "dispatching" || active(s) }
func (m *Manager) loadQueue() error {
	m.tasks = map[string]Task{}
	m.processReservations = map[string]bool{}
	m.queue = QueueSettings{MaxConcurrent: 4, PerInstance: map[string]int{}}
	if e := m.Store.Get("queue", "settings", &m.queue); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	rows, e := m.Store.List("task")
	if e != nil {
		return e
	}
	for _, b := range rows {
		var t Task
		if e = json.Unmarshal(b, &t); e != nil {
			return e
		}
		m.tasks[t.ID] = t
	}
	for _, t := range m.tasks {
		if t.Status != "queued" && taskPending(t.Status) {
			if e = m.reconcileTaskLocked(t); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m *Manager) prepareTask(spec TaskSpec, validate bool) (Task, Session, error) {
	if strings.TrimSpace(spec.Input.Text) == "" || len(spec.Input.Text) > 256*1024 {
		return Task{}, Session{}, failure(400, "invalid_input", "任务消息需为 1–256 KiB")
	}
	if len([]rune(spec.Title)) > 120 {
		return Task{}, Session{}, failure(400, "invalid_task", "标题最多 120 字符")
	}
	if spec.NotBefore != "" {
		at, e := time.Parse(time.RFC3339Nano, spec.NotBefore)
		if e != nil {
			return Task{}, Session{}, failure(400, "invalid_not_before", "notBefore 需要 RFC3339 时间")
		}
		spec.NotBefore = at.UTC().Format(time.RFC3339Nano)
	}
	s, e := m.prepareSession(spec.WorkspaceID, spec.Title, spec.Model, spec.InstanceID, spec.Source, nil)
	if e != nil {
		return Task{}, s, e
	}
	if validate {
		w, _ := m.Workspace(s.WorkspaceID)
		if e = m.validateInput(w, spec.Input, s.InstanceID); e != nil {
			return Task{}, s, e
		}
	}
	spec.InstanceID = s.InstanceID
	spec.Model = s.Model
	spec.Source = s.Source
	spec.Title = s.Title
	t := Task{SubmittingKeyID: spec.SubmittingKeyID, FileOwner: spec.Input.LibraryOwner, ID: store.ID(), SessionID: s.ID, Spec: spec, Status: "queued", Created: store.Now(), Updated: store.Now()}
	s.TaskID = t.ID
	return t, s, nil
}
func (m *Manager) pendingCountLocked() int {
	n := 0
	for _, t := range m.tasks {
		if taskPending(t.Status) {
			n++
		}
	}
	return n
}
func (m *Manager) Enqueue(spec TaskSpec) (Task, error) {
	t, s, e := m.prepareTask(spec, true)
	if e != nil {
		return t, e
	}
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if m.ctx.Err() != nil {
		return t, failure(503, "runtime_unavailable", "后台正在停止")
	}
	if m.pendingCountLocked() >= 1000 {
		return t, failure(429, "queue_full", "待处理任务最多 1000 项")
	}
	if e = m.Store.PutMany(store.Record{Kind: "task", ID: t.ID, Value: t}, store.Record{Kind: "session", ID: s.ID, Value: s}); e != nil {
		return t, e
	}
	m.tasks[t.ID] = t
	m.mu.Lock()
	m.sessions[s.ID] = &s
	m.mu.Unlock()
	return t, nil
}
func (m *Manager) saveTaskLocked(t Task) error {
	t.Updated = store.Now()
	if e := m.Store.Put("task", t.ID, t); e != nil {
		return e
	}
	m.tasks[t.ID] = t
	return nil
}
func (m *Manager) Task(id string) (Task, error) {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return t, failure(404, "task_not_found", "任务不存在")
	}
	return t, nil
}
func (m *Manager) Tasks() []Task {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	out := []Task{}
	for _, t := range m.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, out[i].Created)
		b, _ := time.Parse(time.RFC3339Nano, out[j].Created)
		if a.Equal(b) {
			return out[i].ID > out[j].ID
		}
		return a.After(b)
	})
	return out
}
func (m *Manager) Queue() QueueSettings {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	v := m.queue
	v.PerInstance = map[string]int{}
	for k, n := range m.queue.PerInstance {
		v.PerInstance[k] = n
	}
	return v
}
func (m *Manager) SaveQueue(q QueueSettings) (QueueSettings, error) {
	if q.MaxConcurrent < 1 || q.MaxConcurrent > 16 {
		return q, failure(400, "invalid_concurrency", "全局并发需为 1–16")
	}
	for id, n := range q.PerInstance {
		if _, e := m.Instance(id); e != nil {
			return q, e
		}
		if n < 1 || n > 16 {
			return q, failure(400, "invalid_concurrency", "应用并发需为 1–16")
		}
	}
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if q.Revision != m.queue.Revision {
		return q, failure(409, "revision_conflict", "队列设置已更新，请刷新")
	}
	q.Revision++
	if e := m.Store.Put("queue", "settings", q); e != nil {
		return q, e
	}
	m.queue = q
	return q, nil
}
func (m *Manager) capacityLocked(iid string) bool {
	total, per := 0, 0
	for _, s := range m.Sessions() {
		if active(s.Status) {
			total++
			if s.InstanceID == iid {
				per++
			}
		}
	}
	return total < m.queue.MaxConcurrent && (m.queue.PerInstance[iid] == 0 || per < m.queue.PerInstance[iid])
}
func (m *Manager) queueReserved(id string) bool {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	for _, t := range m.tasks {
		if t.SessionID == id && (t.Status == "queued" || t.Status == "dispatching") {
			return true
		}
	}
	return false
}

// queueMu held. Capture the original run before a manual continuation changes Session.RunID.
func (m *Manager) reconcileTaskLocked(t Task) error {
	if !taskPending(t.Status) || t.Status == "queued" {
		return nil
	}
	s, e := m.Session(t.SessionID)
	if e != nil || t.RunID == "" || s.RunID != t.RunID {
		t.Status = "unconfirmed"
		t.Reason = "执行接收状态不确定；请查看轨迹，不会自动重跑"
	} else {
		t.Status = s.Status
		t.Reason = s.Error
	}
	if !taskPending(t.Status) {
		t.FinishedAt = store.Now()
	}
	if old := m.tasks[t.ID]; old.Status == t.Status && old.Reason == t.Reason {
		return nil
	}
	return m.saveTaskLocked(t)
}
func (m *Manager) CancelTask(id string) (Task, error) {
	m.queueMu.Lock()
	t, ok := m.tasks[id]
	if !ok {
		m.queueMu.Unlock()
		return t, failure(404, "task_not_found", "任务不存在")
	}
	if t.Status == "queued" {
		t.Status = "canceled"
		t.FinishedAt = store.Now()
		e := m.saveTaskLocked(t)
		m.queueMu.Unlock()
		return t, e
	}
	if !taskPending(t.Status) {
		m.queueMu.Unlock()
		return t, nil
	}
	t.CancelRequested = true
	e := m.saveTaskLocked(t)
	m.queueMu.Unlock()
	if e != nil {
		return t, e
	}
	// A dispatch claim not yet accepted is canceled by start under queueMu.
	s, e := m.Session(t.SessionID)
	if e == nil && s.RunID == t.RunID && active(s.Status) {
		e = m.StopRun(t.SessionID, t.RunID)
	}
	return t, e
}
func (m *Manager) queueTick() {
	if !m.queueCycle.TryLock() {
		return
	}
	defer m.queueCycle.Unlock()
	if m.ctx.Err() != nil {
		return
	}
	m.queueMu.Lock()
	for _, t := range m.tasks {
		if e := m.reconcileTaskLocked(t); e != nil {
			m.cancel()
			m.queueMu.Unlock()
			return
		}
	}
	m.queueMu.Unlock()
	rows := m.Tasks()
	for _, t := range rows {
		if !taskPending(t.Status) {
			m.releaseTaskConnection(t)
		}
	}
	blocked := map[string]bool{}
	for n := len(rows) - 1; n >= 0; n-- {
		t := rows[n]
		if t.Status != "queued" || blocked[t.Spec.InstanceID] {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, t.Spec.NotBefore)
		if at.After(time.Now()) {
			continue
		}
		m.queueMu.Lock()
		t = m.tasks[t.ID]
		if m.ctx.Err() != nil || m.queue.Paused {
			m.queueMu.Unlock()
			return
		}
		if t.Status != "queued" {
			m.queueMu.Unlock()
			continue
		}
		if authErr := m.checkTaskAuthorization(t.SubmittingKeyID, t.Spec, t.ScheduleID != ""); authErr != nil {
			t.Status = "failed"
			t.Reason = authErr.Error()
			t.FinishedAt = store.Now()
			saveErr := m.saveTaskLocked(t)
			m.queueMu.Unlock()
			if saveErr != nil {
				m.cancel()
				return
			}
			continue
		}
		if !m.capacityLocked(t.Spec.InstanceID) {
			blocked[t.Spec.InstanceID] = true
			m.queueMu.Unlock()
			continue
		}
		t.Status = "dispatching"
		t.RunID = store.ID()
		t.StartedAt = store.Now()
		e := m.saveTaskLocked(t)
		m.queueMu.Unlock()
		if e != nil {
			m.cancel()
			return
		}
		t.Spec.Input.LibraryOwner = t.FileOwner
		_, e = m.start(t.SessionID, t.Spec.Input, nil, t.ID)
		m.queueMu.Lock()
		current := m.tasks[t.ID]
		if e != nil {
			var ae *apiError
			current.Reason = e.Error()
			if errors.As(e, &ae) && (ae.Code == "run_capacity" || ae.Code == "connection_limit") {
				current.Status = "queued"
				current.RunID = ""
				current.StartedAt = ""
				blocked[t.Spec.InstanceID] = true
			} else {
				current.Status = "failed"
				if current.CancelRequested {
					current.Status = "canceled"
				}
				current.FinishedAt = store.Now()
			}
			e = m.saveTaskLocked(current)
		} else {
			e = m.reconcileTaskLocked(current)
		}
		m.queueMu.Unlock()
		if e != nil {
			m.cancel()
			return
		}
	}
}
func (m *Manager) releaseTaskConnection(t Task) {
	s, e := m.Session(t.SessionID)
	if e != nil || s.RunID != t.RunID || active(s.Status) {
		return
	}
	h, _ := m.getHandle(s.ID)
	if !h.op.TryLock() {
		return
	}
	defer h.op.Unlock()
	s, e = m.Session(s.ID)
	if e != nil || active(s.Status) || s.RunID != t.RunID {
		return
	}
	h.mu.Lock()
	c := h.client
	k := h.kun
	h.kun = nil
	h.client = nil
	h.thread = ""
	h.mu.Unlock()
	if k != nil {
		k.Close()
	}
	if c != nil {
		c.Close()
		<-c.Done()
	}
}
func (m *Manager) reserveProcess(id string, h *handle) error {
	m.processMu.Lock()
	defer m.processMu.Unlock()
	h.mu.Lock()
	c := h.client
	k := h.kun
	h.mu.Unlock()
	if k != nil {
		select {
		case <-k.Done():
		default:
			return nil
		}
	}
	if c != nil && !c.Closing() {
		select {
		case <-c.Done():
		default:
			return nil
		}
	}
	if int(m.loaded.Load())+len(m.processReservations) >= 16 {
		return failure(503, "connection_limit", "会话连接已满，队列稍后继续")
	}
	m.processReservations[id] = true
	return nil
}
func (m *Manager) unreserveProcess(id string) {
	m.processMu.Lock()
	delete(m.processReservations, id)
	m.processMu.Unlock()
}
