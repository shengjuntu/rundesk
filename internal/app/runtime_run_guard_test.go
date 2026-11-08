package app

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func assertNoGuardedNativeDispatch(t *testing.T, m *Manager, s Session, h *handle) {
	t.Helper()
	for _, method := range []string{"initialize", "thread/start", "turn/start", "kun/run.started"} {
		if _, err := m.Store.LatestEvent(s.ID, method); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rejected asynchronous run dispatched %s: %v", method, err)
		}
	}
	var runtime RuntimeStatus
	if err := m.Store.Get("runtime", s.ID, &runtime); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rejected asynchronous run opened a runtime: %v", err)
	}
	h.mu.Lock()
	connected := h.client != nil || h.kun != nil
	h.mu.Unlock()
	if connected || m.loaded.Load() != 0 {
		t.Fatal("rejected asynchronous run launched a native process")
	}
}

// Deterministically exercise the state after stop wins h.op and a newer
// admission resets h.canceled, before the old start goroutine gets h.op.
// No scheduler timing or sleep is needed to reproduce the stale dispatch.
func TestRuntimeRunGuardPreservesReplacementAndTerminalState(t *testing.T) {
	for _, backend := range []string{"codex", "kun"} {
		for _, state := range []string{"replacement", "completed", "interrupted"} {
			t.Run(backend+"/"+state, func(t *testing.T) {
				m := testManager(t)
				m.queueCycle.Lock()
				defer m.queueCycle.Unlock()
				w := m.Workspaces()[0]
				s, err := m.CreateSession(w.ID, "guarded old run", "")
				if err != nil {
					t.Fatal(err)
				}
				captured := s
				captured.RunID, captured.TaskID, captured.RuntimeKind, captured.Status = "old-run", "old-task", backend, "starting"
				if err = m.update(s.ID, func(current *Session) {
					current.RunID, current.TaskID, current.RuntimeKind, current.Status = captured.RunID, captured.TaskID, backend, state
					if state == "replacement" {
						current.RunID, current.TaskID, current.Status = "new-run", "new-task", "starting"
					}
					current.Error = "retained state"
				}); err != nil {
					t.Fatal(err)
				}
				before, _ := m.Session(s.ID)
				h, _ := m.getHandle(s.ID)
				// A later start resets this flag; it cannot establish old ownership.
				h.canceled = false
				m.processMu.Lock()
				m.processReservations[s.ID] = true
				m.processMu.Unlock()
				h.op.Lock()
				m.run(captured, w, h, Input{Text: "must never reach the model"}, nil)
				h.op.Unlock()
				after, _ := m.Session(s.ID)
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("late run changed retained session: before=%+v after=%+v", before, after)
				}
				m.processMu.Lock()
				reserved := m.processReservations[s.ID]
				m.processMu.Unlock()
				if reserved != (state == "replacement") {
					t.Fatalf("late run changed reservation ownership: reserved=%v", reserved)
				}
				assertNoGuardedNativeDispatch(t, m, s, h)
			})
		}
	}
}

func TestRuntimeRunGuardStopsCancelledOrExpiredAdmission(t *testing.T) {
	for _, backend := range []string{"codex", "kun"} {
		for _, cause := range []string{"stop_flag", "stopping", "task_cancelled", "deadline"} {
			t.Run(backend+"/"+cause, func(t *testing.T) {
				m := testManager(t)
				m.queueCycle.Lock()
				defer m.queueCycle.Unlock()
				w := m.Workspaces()[0]
				s, err := m.CreateSession(w.ID, "cancel before native dispatch", "")
				if err != nil {
					t.Fatal(err)
				}
				if err = m.update(s.ID, func(current *Session) {
					current.RunID, current.TaskID, current.RuntimeKind, current.Status = "accepted-run", "accepted-task", backend, "starting"
					if cause == "stopping" {
						current.Status = "stopping"
					}
				}); err != nil {
					t.Fatal(err)
				}
				s, _ = m.Session(s.ID)
				h, _ := m.getHandle(s.ID)
				h.canceled = cause == "stop_flag"
				if cause == "task_cancelled" || cause == "deadline" {
					task := Task{ID: s.TaskID, SessionID: s.ID, RunID: s.RunID, Status: "dispatching", Created: store.Now()}
					task.CancelRequested = cause == "task_cancelled"
					if cause == "deadline" {
						// Model expiration after host admission but before its timer
						// has delivered StopRun. Dispatch must check the clock itself.
						task.Deadline = time.Now().Add(-time.Second).UnixNano()
					}
					m.queueMu.Lock()
					err = m.saveTaskLocked(task)
					m.queueMu.Unlock()
					if err != nil {
						t.Fatal(err)
					}
				}
				h.op.Lock()
				m.run(s, w, h, Input{Text: "must never reach the model"}, nil)
				h.op.Unlock()
				current, _ := m.Session(s.ID)
				if current.RunID != s.RunID || current.TaskID != s.TaskID || current.Status != "interrupted" {
					t.Fatalf("cancelled asynchronous admission was not settled: %+v", current)
				}
				if cause == "deadline" {
					task, err := m.Task(s.TaskID)
					if err != nil || !task.CancelRequested || task.StopReason != "timeout" {
						t.Fatalf("late dispatch lost durable timeout: %+v (%v)", task, err)
					}
				}
				assertNoGuardedNativeDispatch(t, m, s, h)
			})
		}
	}
}
