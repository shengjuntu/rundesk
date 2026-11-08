package app

import (
	"errors"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func validateTaskDeadline(deadline int64) error {
	if deadline < 0 {
		return failure(400, "invalid_task_deadline", "任务截止时间必须为有效的 Unix 纳秒时间")
	}
	if deadline > 0 && time.Now().UnixNano() >= deadline {
		return failure(408, "task_deadline_exceeded", "任务截止时间已到，任务未提交")
	}
	return nil
}

func taskDeadlineDue(t Task) bool {
	return t.Deadline > 0 && time.Now().UnixNano() >= t.Deadline
}

// queueMu held. This is a dispatch guard, not a native cancellation operation.
// The stop intent is durable before returning, so queue error handling and a
// restart cannot turn an elapsed deadline into another execution attempt.
func (m *Manager) checkTaskDeadlineLocked(t Task) error {
	if !taskDeadlineDue(t) {
		return nil
	}
	if t.CancelRequested && t.StopReason != "timeout" {
		return failure(409, "task_canceled", "任务已经取消")
	}
	if t.StopReason != "timeout" || !t.CancelRequested {
		t.StopReason = "timeout"
		t.CancelRequested = true
		t.Reason = "任务截止时间已到"
		if t.Status == "queued" {
			t.Status = "canceled"
			t.FinishedAt = store.Now()
		}
		if err := m.saveTaskLocked(t); err != nil {
			return err
		}
	}
	return failure(408, "task_deadline_exceeded", "任务截止时间已到，未启动执行")
}

// A deadline timer is independent of queue pause/capacity and slow dispatch.
// It never belongs to an HTTP/SSE request. loadQueue rebuilds these timers from
// persisted tasks, and Manager shutdown cancels them before closing storage.
func (m *Manager) watchTaskDeadline(t Task) {
	if t.Deadline == 0 || !taskPending(t.Status) || m.ctx.Err() != nil {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		timer := time.NewTimer(time.Until(time.Unix(0, t.Deadline)))
		defer timer.Stop()
		// Completed/cancelled tasks should not retain a goroutine until a distant
		// deadline. This read does not inspect or modify the native runtime.
		settled := time.NewTicker(time.Second)
		defer settled.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-settled.C:
				current, err := m.Task(t.ID)
				if err != nil || !taskPending(current.Status) {
					return
				}
			case <-timer.C:
				if err := m.expireTaskDeadline(t.ID); err != nil {
					m.cancel()
				}
				return
			}
		}
	}()
}

// The saved Task and its original run identity own the cancellation. A later
// turn in the same session must never inherit an earlier task's deadline.
func (m *Manager) expireTaskDeadline(id string) error {
	m.queueMu.Lock()
	t, ok := m.tasks[id]
	if !ok || !taskPending(t.Status) || !taskDeadlineDue(t) || m.ctx.Err() != nil {
		m.queueMu.Unlock()
		return nil
	}
	// Before final admission, a dispatch claim's Session.RunID still belongs to
	// the previous turn. Once the identities match, native execution may already
	// have completed before the queue reconciles "dispatching"; preserve that
	// terminal result instead of relabelling it as a timeout.
	reconcile := t.Status != "queued" && t.Status != "dispatching"
	if t.Status == "dispatching" && t.RunID != "" {
		if s, err := m.Session(t.SessionID); err == nil && s.RunID == t.RunID {
			reconcile = true
		}
	}
	if reconcile {
		if err := m.reconcileTaskLocked(t); err != nil {
			m.queueMu.Unlock()
			return err
		}
		t = m.tasks[id]
		if !taskPending(t.Status) {
			m.queueMu.Unlock()
			return nil
		}
	}
	// A user's earlier cancellation retains its cause.
	if t.CancelRequested && t.StopReason != "timeout" {
		m.queueMu.Unlock()
		return nil
	}
	t.StopReason = "timeout"
	t.CancelRequested = true
	t.Reason = "任务截止时间已到"
	if t.Status == "queued" {
		t.Status = "canceled"
		t.FinishedAt = store.Now()
	}
	if err := m.saveTaskLocked(t); err != nil {
		m.queueMu.Unlock()
		return err
	}
	m.queueMu.Unlock()
	if t.RunID == "" {
		return nil
	}
	s, err := m.Session(t.SessionID)
	if err != nil || s.RunID != t.RunID || !active(s.Status) {
		return nil
	}
	if err = m.StopRun(t.SessionID, t.RunID); err != nil {
		var api *apiError
		if errors.As(err, &api) && (api.Code == "run_conflict" || api.Code == "session_not_found") {
			return nil
		}
		return err
	}
	return nil
}
