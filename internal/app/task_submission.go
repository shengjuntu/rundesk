package app

import (
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

// SubmitTask shares admission, authorization, queueing and persistence across
// protocol facades. In particular a retry receipt cannot be committed without
// its task, nor can the task be dispatched before the receipt is durable.
func (m *Manager) SubmitTask(sub TaskSubmission) (Task, error) {
	var t Task
	var s Session
	var err error
	if sub.SessionID == "" {
		if sub.ExpectedRunID != "" {
			return t, failure(400, "invalid_task", "新会话不能指定先前运行")
		}
		t, s, err = m.prepareTask(sub.Spec, true)
	} else {
		// Use the same session operation lock as manual starts, edits and deletion.
		// Wait for terminal-process cleanup, but reject a running or reserved turn
		// promptly instead of waiting behind native execution.
		if _, err = m.Session(sub.SessionID); err != nil {
			return t, err
		}
		h, e := m.getHandle(sub.SessionID)
		if e != nil {
			return t, e
		}
		if err = m.lockTaskContinuation(sub.SessionID, h); err != nil {
			return t, err
		}
		defer h.op.Unlock()
		h.admission.Lock()
		defer h.admission.Unlock()
		t, s, err = m.prepareContinuedTask(sub)
	}
	if err != nil {
		return t, err
	}
	// Match start's ordering: session operation -> application -> queue -> key.
	// Preparation (which may inspect runtime configuration) happens before these
	// global locks, while final authority is rechecked immediately before commit.
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	if m.ctx.Err() != nil {
		return t, failure(503, "runtime_unavailable", "后台正在停止")
	}
	if err = m.checkTaskAuthorization(t.SubmittingKeyID, t.Spec, false); err != nil {
		return t, err
	}
	if sub.SessionID != "" {
		for _, previous := range m.tasks {
			if previous.SessionID != s.ID {
				continue
			}
			// Freeze the original run's result before a new turn can change it.
			if err = m.reconcileTaskLocked(previous); err != nil {
				return t, err
			}
			if taskPending(m.tasks[previous.ID].Status) {
				return t, failure(409, "session_busy", "该会话已有运行或排队任务")
			}
		}
	}
	if m.pendingCountLocked() >= 1000 {
		return t, failure(429, "queue_full", "待处理任务最多 1000 项")
	}
	records := []store.Record{{Kind: "task", ID: t.ID, Value: t}, {Kind: "session", ID: s.ID, Value: s}}
	if sub.Records != nil {
		extra, e := sub.Records(t, s)
		if e != nil {
			return t, e
		}
		seen := map[string]bool{}
		for _, record := range extra {
			key := record.Kind + "\x00" + record.ID
			if record.Kind == "task" || record.Kind == "session" || record.Kind == "" || record.ID == "" || seen[key] {
				return t, failure(400, "invalid_task_records", "任务附加记录无效或重复")
			}
			seen[key] = true
		}
		records = append(records, extra...)
	}
	if err = m.Store.PutMany(records...); err != nil {
		return t, err
	}
	m.tasks[t.ID] = t
	m.mu.Lock()
	m.sessions[s.ID] = &s
	m.mu.Unlock()
	return t, nil
}

func (m *Manager) lockTaskContinuation(id string, h *handle) error {
	if h.op.TryLock() {
		return nil
	}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		s, err := m.Session(id)
		if err != nil {
			return err
		}
		if active(s.Status) || m.queueReserved(id) {
			return failure(409, "session_busy", "该会话已有运行或排队任务")
		}
		select {
		case <-m.ctx.Done():
			return failure(503, "runtime_unavailable", "后台正在停止")
		case <-tick.C:
			if h.op.TryLock() {
				return nil
			}
		}
	}
}

// Caller holds the session operation/admission locks, keeping native identity
// and the working directory unchanged through final admission.
func (m *Manager) prepareContinuedTask(sub TaskSubmission) (Task, Session, error) {
	s, err := m.Session(sub.SessionID)
	if err != nil {
		return Task{}, s, err
	}
	if active(s.Status) {
		return Task{}, s, failure(409, "session_busy", "该会话已有任务正在运行")
	}
	if s.Archived {
		return Task{}, s, failure(409, "session_archived", "请先恢复已归档会话，再提交任务")
	}
	if sub.ExpectedRunID != "" && s.RunID != sub.ExpectedRunID {
		return Task{}, s, failure(409, "run_conflict", "会话已切换到其他任务")
	}
	spec := sub.Spec
	if (spec.WorkspaceID != "" && spec.WorkspaceID != s.WorkspaceID) || (spec.InstanceID != "" && spec.InstanceID != s.InstanceID) || (spec.Source != (SessionSource{}) && spec.Source != s.Source) {
		return Task{}, s, failure(409, "session_mismatch", "续接必须沿用会话的项目、实例和来源")
	}
	if strings.TrimSpace(spec.Input.Text) == "" || len(spec.Input.Text) > 256*1024 {
		return Task{}, s, failure(400, "invalid_input", "任务消息需为 1–256 KiB")
	}
	if len([]rune(spec.Title)) > 120 {
		return Task{}, s, failure(400, "invalid_task", "标题最多 120 字符")
	}
	if spec.NotBefore != "" {
		at, e := time.Parse(time.RFC3339Nano, spec.NotBefore)
		if e != nil {
			return Task{}, s, failure(400, "invalid_not_before", "notBefore 需要 RFC3339 时间")
		}
		spec.NotBefore = at.UTC().Format(time.RFC3339Nano)
	}
	if err = m.checkSessionExecution(s); err != nil {
		return Task{}, s, err
	}
	w, err := m.Workspace(s.WorkspaceID)
	if err != nil {
		return Task{}, s, err
	}
	if err = m.validateSessionInput(s, w, spec.Input); err != nil {
		return Task{}, s, err
	}
	// A model override applies to this new turn; it does not create a new native
	// thread. The facade validates model availability before submission.
	if spec.Model != "" {
		s.Model = spec.Model
	}
	spec.WorkspaceID, spec.InstanceID = s.WorkspaceID, s.InstanceID
	spec.Model, spec.Source = s.Model, s.Source
	if spec.Title == "" {
		spec.Title = s.Title
	}
	t := Task{SubmittingKeyID: spec.SubmittingKeyID, FileOwner: spec.Input.LibraryOwner, ID: store.ID(), SessionID: s.ID, Spec: spec, Status: "queued", Created: store.Now(), Updated: store.Now()}
	s.TaskID = t.ID
	s.Updated = t.Updated
	return t, s, nil
}
