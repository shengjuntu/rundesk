package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func pauseSubmissionQueue(t *testing.T, m *Manager) {
	t.Helper()
	q := m.Queue()
	q.Paused = true
	if _, err := m.SaveQueue(q); err != nil {
		t.Fatal(err)
	}
}

func TestTaskSubmissionBindsRecordsAtomically(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	spec := TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "bound task"}}
	var taskID, sessionID string
	_, err := m.SubmitTask(TaskSubmission{Spec: spec, Records: func(task Task, session Session) ([]store.Record, error) {
		taskID, sessionID = task.ID, session.ID
		return []store.Record{{Kind: "test-receipt", ID: "failed", Value: make(chan int)}}, nil
	}})
	if err == nil {
		t.Fatal("unserializable metadata accepted")
	}
	if _, err = m.Task(taskID); err == nil {
		t.Fatal("failed transaction admitted task")
	}
	if _, err = m.Session(sessionID); err == nil {
		t.Fatal("failed transaction cached session")
	}
	for _, object := range []struct{ kind, id string }{{"task", taskID}, {"session", sessionID}, {"test-receipt", "failed"}} {
		var value any
		if err = m.Store.Get(object.kind, object.id, &value); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("partial transaction %s: %v", object.kind, err)
		}
	}
	task, err := m.SubmitTask(TaskSubmission{Spec: spec, Records: func(task Task, session Session) ([]store.Record, error) {
		return []store.Record{{Kind: "test-receipt", ID: "accepted", Value: map[string]string{"taskId": task.ID, "sessionId": session.ID}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]string
	if err = m.Store.Get("test-receipt", "accepted", &receipt); err != nil || receipt["taskId"] != task.ID || receipt["sessionId"] != task.SessionID {
		t.Fatalf("receipt did not bind to admitted task: %#v %v", receipt, err)
	}
}

func TestTaskSubmissionCannotReplaceCoreRecords(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	for _, kind := range []string{"task", "session"} {
		_, err := m.SubmitTask(TaskSubmission{Spec: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "invalid binding"}}, Records: func(task Task, session Session) ([]store.Record, error) {
			return []store.Record{{Kind: kind, ID: task.ID, Value: "corrupt"}}, nil
		}})
		if err == nil {
			t.Fatalf("accepted replacement for %s", kind)
		}
	}
	if len(m.Tasks()) != 0 {
		t.Fatal("invalid binding created tasks")
	}
}

func TestTaskContinuationPreservesNativeSessionAndOldTask(t *testing.T) {
	m := testManager(t)
	first, err := m.Enqueue(TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "first turn"}})
	if err != nil {
		t.Fatal(err)
	}
	first = taskWait(t, m, first.ID, "completed")
	before, err := m.Session(first.SessionID)
	if err != nil || before.ThreadID == "" {
		t.Fatal(before, err)
	}
	second, err := m.SubmitTask(TaskSubmission{SessionID: before.ID, ExpectedRunID: first.RunID, Spec: TaskSpec{Model: "continued-model", Input: Input{Text: "continue turn"}}})
	if err != nil {
		t.Fatal(err)
	}
	second = taskWait(t, m, second.ID, "completed")
	after, _ := m.Session(before.ID)
	if second.ID == first.ID || second.SessionID != first.SessionID || second.RunID == first.RunID || after.ThreadID != before.ThreadID || after.WorkspaceID != before.WorkspaceID || after.TaskID != second.ID {
		t.Fatalf("continuation lost identity: first=%+v second=%+v before=%+v after=%+v", first, second, before, after)
	}
	retained, _ := m.Task(first.ID)
	if retained.Status != "completed" || retained.RunID != first.RunID || retained.FinishedAt != first.FinishedAt {
		t.Fatal("continuation changed original task", retained)
	}
	if after.Model != "continued-model" || second.Spec.Model != "continued-model" || retained.Spec.Model != first.Spec.Model {
		t.Fatal("model override was not scoped to new turn")
	}
	event, err := m.Store.LatestEvent(after.ID, "turn/start")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Params struct {
			Model string `json:"model"`
		} `json:"params"`
	}
	if err = json.Unmarshal(event.Data, &envelope); err != nil || envelope.Params.Model != "continued-model" {
		t.Fatalf("native turn did not select continued model: %s %v", event.Data, err)
	}
	if _, err = m.SubmitTask(TaskSubmission{SessionID: after.ID, ExpectedRunID: first.RunID, Spec: TaskSpec{Input: Input{Text: "stale parent"}}}); err == nil {
		t.Fatal("stale parent accepted")
	}
}

func TestConcurrentTaskContinuationsReserveOneSession(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	s, err := m.CreateSession(m.Workspaces()[0].ID, "shared session", "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for n := 0; n < cap(results); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.SubmitTask(TaskSubmission{SessionID: s.ID, Spec: TaskSpec{Input: Input{Text: "one continuation"}}})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
			continue
		}
		var api *apiError
		if !errors.As(err, &api) || api.Code != "session_busy" {
			t.Fatal(err)
		}
	}
	if accepted != 1 || len(m.Tasks()) != 1 {
		t.Fatalf("accepted %d continuations; tasks %d", accepted, len(m.Tasks()))
	}
	if _, err = m.Start(s.ID, Input{Text: "manual bypass"}); err == nil {
		t.Fatal("manual start bypassed continuation reservation")
	}
}

func TestTaskContinuationRejectsActiveSessionPromptly(t *testing.T) {
	m := testManager(t)
	task, err := m.Enqueue(TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "审批"}})
	if err != nil {
		t.Fatal(err)
	}
	taskWait(t, m, task.ID, "waiting")
	defer m.CancelTask(task.ID)
	done := make(chan error, 1)
	go func() {
		_, err := m.SubmitTask(TaskSubmission{SessionID: task.SessionID, Spec: TaskSpec{Input: Input{Text: "must not wait for approval"}}})
		done <- err
	}()
	select {
	case err := <-done:
		var api *apiError
		if !errors.As(err, &api) || api.Code != "session_busy" {
			t.Fatalf("expected busy, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("continuation blocked behind active native execution")
	}
}

func TestTaskContinuationRechecksCredentialBeforeCommitAndDispatch(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	wid := m.Workspaces()[0].ID
	app, key, _ := setupKey(t, m, "continuation-auth", wid, "read", "run")
	source := SessionSource{Kind: "application", AppID: app.AppID}
	s, err := m.CreateSessionWithSource(wid, "protected", "", app.InstanceID, source)
	if err != nil {
		t.Fatal(err)
	}
	sub := TaskSubmission{SessionID: s.ID, Spec: TaskSpec{SubmittingKeyID: key.ID, Input: Input{Text: "protected continuation"}}}
	task, err := m.SubmitTask(sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.RevokeApplicationKey(app.AppID, key.ID); err != nil {
		t.Fatal(err)
	}
	q := m.Queue()
	q.Paused = false
	if _, err = m.SaveQueue(q); err != nil {
		t.Fatal(err)
	}
	m.queueTick()
	failed := taskWait(t, m, task.ID, "failed")
	if failed.RunID != "" {
		t.Fatal("revoked queued continuation executed")
	}
	if _, err = m.SubmitTask(sub); err == nil {
		t.Fatal("revoked credential admitted a new continuation")
	}
	if len(m.Tasks()) != 1 {
		t.Fatal("unauthorized admission persisted task")
	}
}
