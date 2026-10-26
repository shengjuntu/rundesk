package app

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func assertTaskDeadlineCode(t *testing.T, err error, code string) {
	t.Helper()
	var api *apiError
	if !errors.As(err, &api) || api.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestTaskDeadlineRejectsBeforeAtomicAdmission(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	spec := TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "must not be admitted"}}
	for _, deadline := range []int64{-1, time.Now().Add(-time.Second).UnixNano()} {
		_, err := m.SubmitTask(TaskSubmission{Spec: spec, Deadline: deadline})
		code := "task_deadline_exceeded"
		if deadline < 0 {
			code = "invalid_task_deadline"
		}
		assertTaskDeadlineCode(t, err, code)
	}
	var taskID, sessionID string
	_, err := m.SubmitTask(TaskSubmission{
		Spec: spec, Deadline: time.Now().Add(150 * time.Millisecond).UnixNano(),
		Records: func(task Task, session Session) ([]store.Record, error) {
			taskID, sessionID = task.ID, session.ID
			// Exercise expiration after preparation, not merely initial validation.
			<-time.After(time.Until(time.Unix(0, task.Deadline)) + time.Millisecond)
			return []store.Record{{Kind: "deadline-test-receipt", ID: "not-accepted", Value: task.ID}}, nil
		},
	})
	assertTaskDeadlineCode(t, err, "task_deadline_exceeded")
	if len(m.Tasks()) != 0 {
		t.Fatal("expired admission persisted a task")
	}
	if _, err = m.Session(sessionID); err == nil {
		t.Fatal("expired admission cached a session")
	}
	for _, key := range []struct{ kind, id string }{{"task", taskID}, {"session", sessionID}, {"deadline-test-receipt", "not-accepted"}} {
		var v any
		if err = m.Store.Get(key.kind, key.id, &v); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expired admission persisted %s: %v", key.kind, err)
		}
	}
}

func TestTaskDeadlineExpiresPausedScheduledQueueWithoutExecution(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	task, err := m.SubmitTask(TaskSubmission{
		Spec:     TaskSpec{WorkspaceID: m.Workspaces()[0].ID, NotBefore: time.Now().Add(time.Hour).Format(time.RFC3339Nano), Input: Input{Text: "must never run"}},
		Deadline: time.Now().Add(200 * time.Millisecond).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	done := taskWait(t, m, task.ID, "canceled")
	if done.StopReason != "timeout" || !done.CancelRequested || done.Deadline != task.Deadline || done.RunID != "" || done.StartedAt != "" {
		t.Fatalf("deadline was not applied before dispatch: %+v", done)
	}
	s, _ := m.Session(task.SessionID)
	if s.RunID != "" || active(s.Status) {
		t.Fatalf("expired queue task executed: %+v", s)
	}
	if _, err = m.Store.LatestEvent(s.ID, "run/input"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired queue task recorded native admission: %v", err)
	}
}

func TestTaskDeadlineRejectsExpiredDispatchClaimAtFinalAdmission(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	m.queueCycle.Lock()
	defer m.queueCycle.Unlock()
	task, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "dispatch guard"}}, Deadline: time.Now().Add(time.Hour).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	// Model a dispatch claim whose deadline elapses while start prepares its
	// resources, before a delayed timer has delivered cancellation.
	m.queueMu.Lock()
	task.Status, task.RunID = "dispatching", store.ID()
	task.Deadline = time.Now().Add(-time.Second).UnixNano()
	err = m.saveTaskLocked(task)
	m.queueMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.start(task.SessionID, task.Spec.Input, nil, task.ID)
	assertTaskDeadlineCode(t, err, "task_deadline_exceeded")
	current, _ := m.Task(task.ID)
	if !current.CancelRequested || current.StopReason != "timeout" {
		t.Fatal("final admission failed to persist stop intent", current)
	}
	s, _ := m.Session(task.SessionID)
	if s.RunID != "" || s.ThreadID != "" {
		t.Fatal("expired dispatch was sent to a native runtime", s)
	}
}

func TestTaskDeadlineStopsRunningTaskWithQueuePaused(t *testing.T) {
	m := testManager(t)
	task, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "审批"}}, Deadline: time.Now().Add(3 * time.Second).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	taskWait(t, m, task.ID, "waiting")
	pauseSubmissionQueue(t, m)
	// The timer must work even while a slow dispatcher owns the queue cycle.
	m.queueCycle.Lock()
	defer m.queueCycle.Unlock()
	waitState(t, m, task.SessionID, "interrupted")
	current, _ := m.Task(task.ID)
	if current.StopReason != "timeout" || !current.CancelRequested {
		t.Fatalf("running deadline lost stop cause: %+v", current)
	}
	if len(m.Approvals(task.SessionID)) != 0 {
		t.Fatal("timed out task retained actionable approvals")
	}
}

func TestTaskDeadlineSurvivesRestartForQueuedTask(t *testing.T) {
	path := t.TempDir()
	m, err := New(path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	pauseSubmissionQueue(t, m)
	task, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "restart expiry"}}, Deadline: time.Now().Add(400 * time.Millisecond).UnixNano()})
	if err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()
	<-time.After(time.Until(time.Unix(0, task.Deadline)) + 20*time.Millisecond)
	m, err = New(path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	done := taskWait(t, m, task.ID, "canceled")
	if done.StopReason != "timeout" || done.Deadline != task.Deadline || done.RunID != "" {
		t.Fatal("restart changed deadline or dispatched expired task", done)
	}
}

func TestTaskDeadlineOldTurnCannotStopContinuation(t *testing.T) {
	m := testManager(t)
	first, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "first completes"}}, Deadline: time.Now().Add(5 * time.Second).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	first = taskWait(t, m, first.ID, "completed")
	second, err := m.SubmitTask(TaskSubmission{SessionID: first.SessionID, Spec: TaskSpec{Input: Input{Text: "审批"}}})
	if err != nil {
		t.Fatal(err)
	}
	taskWait(t, m, second.ID, "waiting")
	defer m.CancelTask(second.ID)
	// A delayed notification for an already completed task must remain inert,
	// even when its absolute deadline has passed and the session is active again.
	m.queueMu.Lock()
	old := m.tasks[first.ID]
	old.Deadline = time.Now().Add(-time.Second).UnixNano()
	err = m.saveTaskLocked(old)
	m.queueMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.expireTaskDeadline(first.ID); err != nil {
		t.Fatal(err)
	}
	s, _ := m.Session(second.SessionID)
	current, _ := m.Task(second.ID)
	retained, _ := m.Task(first.ID)
	if s.RunID != current.RunID || s.Status != "waiting" || current.CancelRequested || current.Deadline != 0 || retained.Status != "completed" || retained.StopReason != "" {
		t.Fatalf("old deadline affected continuation: session=%+v current=%+v old=%+v", s, current, retained)
	}
}

func TestTaskDeadlineDoesNotRelabelEarlierUserCancellation(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	task, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "cancel before expiry"}}, Deadline: time.Now().Add(time.Hour).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
	m.queueMu.Lock()
	old := m.tasks[task.ID]
	old.Deadline = time.Now().Add(-time.Second).UnixNano()
	err = m.saveTaskLocked(old)
	m.queueMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.expireTaskDeadline(task.ID); err != nil {
		t.Fatal(err)
	}
	done, _ := m.Task(task.ID)
	if done.Status != "canceled" || done.StopReason != "" {
		t.Fatal("user cancellation was relabeled as timeout", done)
	}
}

func TestTaskDeadlineDispatchingClaimPreservesCompletedNativeRun(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	m.queueCycle.Lock()
	defer m.queueCycle.Unlock()
	task, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "completed before queue reconciliation"}}, Deadline: time.Now().Add(time.Hour).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	// Native completion can win the race while the queue still stores its
	// dispatch claim. Delay the deadline notification deterministically.
	m.queueMu.Lock()
	task.Status, task.RunID = "dispatching", store.ID()
	task.Deadline = time.Now().Add(-time.Second).UnixNano()
	err = m.saveTaskLocked(task)
	m.queueMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.update(task.SessionID, func(s *Session) {
		s.RunID = task.RunID
		s.Status = "completed"
		s.Error = ""
	}); err != nil {
		t.Fatal(err)
	}
	if err = m.expireTaskDeadline(task.ID); err != nil {
		t.Fatal(err)
	}
	done, _ := m.Task(task.ID)
	if done.Status != "completed" || done.StopReason != "" || done.CancelRequested || done.RunID != task.RunID {
		t.Fatalf("completed native run was relabelled as timeout: %+v", done)
	}
	s, _ := m.Session(task.SessionID)
	if s.Status != "completed" || s.RunID != task.RunID {
		t.Fatal("deadline changed the completed native session", s)
	}
}
