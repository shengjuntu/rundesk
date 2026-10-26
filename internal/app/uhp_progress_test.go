package app

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/rpc"
)

func uhpProgressFixture(t *testing.T, backend, status string) (*Manager, *uhpService, *uhpRecord, Task, Session) {
	t.Helper()
	m := testManager(t)
	s, err := m.CreateSession(m.Workspaces()[0].ID, "progress", "")
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "progress-task", SessionID: s.ID, RunID: "progress-run", Status: status}
	if err = m.update(s.ID, func(current *Session) {
		current.TaskID, current.RunID, current.RuntimeKind, current.Status = task.ID, task.RunID, backend, status
	}); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Session(s.ID)
	v := &uhpRecord{TaskID: task.ID, HostSessionID: s.ID, Projection: newUHPProjection(uhpResponse{Metadata: map[string]any{"client_label": "keep"}})}
	return m, &uhpService{server: &Server{Manager: m}}, v, task, s
}

func uhpReadProgress(t *testing.T, v *uhpRecord) uhpProgress {
	t.Helper()
	b, err := json.Marshal(v.Projection.Response.Metadata["rundesk_progress"])
	if err != nil {
		t.Fatal(err)
	}
	var got uhpProgress
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func uhpAssertProgress(t *testing.T, u *uhpService, v *uhpRecord, task Task, want string) uhpProgress {
	t.Helper()
	if _, err := u.updateProgress(v, task); err != nil {
		t.Fatal(err)
	}
	got := uhpReadProgress(t, v)
	if got.State != want {
		t.Fatalf("progress = %+v; want %s", got, want)
	}
	return got
}

func TestUHPProgressTaskAndTerminalStates(t *testing.T) {
	m, u, v, task, session := uhpProgressFixture(t, "codex", "running")
	for _, tc := range []struct{ task, response, reason, want string }{
		{"queued", "in_progress", "", "queued"},
		{"dispatching", "in_progress", "", "dispatching"},
		{"running", "in_progress", "", "running"},
		{"running", "in_progress", "cancelled", "cancelling"},
		{"stopping", "in_progress", "", "cancelling"},
		{"completed", "completed", "", "completed"},
		{"interrupted", "cancelled", "", "interrupted"},
		{"interrupted", "cancelled", "cancelled", "cancelled"},
		{"canceled", "cancelled", "", "cancelled"},
		{"canceled", "incomplete", "timeout", "incomplete"},
		{"failed", "failed", "", "failed"},
		{"future-state", "in_progress", "", "unknown"},
	} {
		t.Run(tc.task+"/"+tc.response+"/"+tc.reason, func(t *testing.T) {
			task.Status, v.Projection.Response.Status, v.CancelReason = tc.task, tc.response, tc.reason
			got := uhpAssertProgress(t, u, v, task, tc.want)
			if got.RequiredAction != nil || v.Projection.Response.Status != tc.response {
				t.Fatal("progress changed standard status or invented required action")
			}
		})
	}
	// A response for a finished turn must not inspect the next turn's approval.
	if err := m.update(session.ID, func(s *Session) { s.TaskID, s.RunID, s.Status = "next-task", "next-run", "waiting" }); err != nil {
		t.Fatal(err)
	}
	task.Status, v.Projection.Response.Status, v.CancelReason = "completed", "completed", ""
	uhpAssertProgress(t, u, v, task, "completed")
	v.Projection.Response.Status = "in_progress"
	task.Status = "running"
	uhpAssertProgress(t, u, v, task, "unknown")
	// A malformed caller cannot borrow another task's live state.
	task.Status, task.ID = "queued", "other-task"
	uhpAssertProgress(t, u, v, task, "unknown")
	task.ID, task.Status, task.StopReason = v.TaskID, "canceled", "timeout"
	uhpAssertProgress(t, u, v, task, "incomplete")
	task.Status = "running"
	uhpAssertProgress(t, u, v, task, "cancelling")
}

func TestUHPProgressApprovalIdentityPrivacyAndReadOnlyPolling(t *testing.T) {
	m, u, v, task, s := uhpProgressFixture(t, "codex", "waiting")
	for _, a := range []Approval{
		{ID: "old-run", SessionID: s.ID, RunID: "old-run", Status: "pending"},
		{ID: "other-session", SessionID: "another", RunID: s.RunID, Status: "pending"},
		{ID: "already-resolved", SessionID: s.ID, RunID: s.RunID, Status: "approved"},
	} {
		if err := m.Store.Put("approval", a.ID, a); err != nil {
			t.Fatal(err)
		}
	}
	uhpAssertProgress(t, u, v, task, "unknown") // waiting alone is not approval.
	a := Approval{ID: "secret-approval-id", SessionID: s.ID, RunID: s.RunID, Status: "pending", Request: rpc.Message{Params: json.RawMessage(`{"arguments":"/private/path SECRET_TOKEN"}`)}}
	if err := m.Store.Put("approval", a.ID, a); err != nil {
		t.Fatal(err)
	}
	got := uhpAssertProgress(t, u, v, task, "waiting_approval")
	if got.RequiredAction == nil || got.RequiredAction.Type != "approval" || got.RequiredAction.WorkbenchURL != "/?session="+url.QueryEscape(s.ID) {
		t.Fatalf("missing safe workbench action: %+v", got)
	}
	encoded, _ := json.Marshal(v.Projection.Response.Metadata)
	for _, secret := range []string{"secret-approval-id", "/private/path", "SECRET_TOKEN", "arguments", "decision"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("progress leaked %q: %s", secret, encoded)
		}
	}
	if v.Projection.Response.Metadata["client_label"] != "keep" {
		t.Fatal("unrelated client metadata lost")
	}
	beforeEvents, _ := m.Store.Events(s.ID, 0, 100)
	beforeProjectionEvents := len(v.Projection.Events)
	for range 3 {
		if changed, err := u.updateProgress(v, task); changed || err != nil {
			t.Fatalf("unchanged poll was reported as a change: %t %v", changed, err)
		}
	}
	// Decoded persisted metadata must compare equal to the typed projection.
	raw, _ := json.Marshal(v)
	var restored uhpRecord
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if changed, err := u.updateProgress(&restored, task); changed || err != nil {
		t.Fatalf("round-trip metadata was not stable: %t %v", changed, err)
	}
	afterEvents, _ := m.Store.Events(s.ID, 0, 100)
	var retained Approval
	if err := m.Store.Get("approval", a.ID, &retained); err != nil || retained.Status != "pending" || len(beforeEvents) != len(afterEvents) || len(v.Projection.Events) != beforeProjectionEvents {
		t.Fatal("progress polling changed approval or event history", err)
	}
	task.CancelRequested = true
	got = uhpAssertProgress(t, u, v, task, "cancelling")
	if got.RequiredAction != nil {
		t.Fatal("cancellation retained an obsolete approval action")
	}
}

func TestUHPProgressKunPauseAndApprovalUseCommittedRunEvidence(t *testing.T) {
	m, u, v, task, s := uhpProgressFixture(t, "kun", "waiting")
	add := func(run, method, operation string) {
		t.Helper()
		data := map[string]any{"secret": "/private/SECRET", "command": map[string]any{"operation": operation}}
		if _, err := m.Store.Add(s.ID, "in", method, p.Event{SessionID: s.ID, RunID: run, Type: method, Data: p.JSON(data)}); err != nil {
			t.Fatal(err)
		}
	}
	add("old-run", "kun/approval.requested", "")
	uhpAssertProgress(t, u, v, task, "unknown")
	add(s.RunID, "kun/run.paused", "")
	got := uhpAssertProgress(t, u, v, task, "paused")
	if got.RequiredAction != nil {
		t.Fatal("debugger pause was presented as tool approval")
	}
	add(s.RunID, "kun/approval.requested", "")
	add(s.RunID, "kun/run.paused", "")
	uhpAssertProgress(t, u, v, task, "waiting_approval")
	add(s.RunID, "kun/control.applied", "approve")
	add(s.RunID, "kun/control.applied", "set_harness")
	add(s.RunID, "kun/run.paused", "")
	uhpAssertProgress(t, u, v, task, "paused") // Later non-resolving control cannot revive old approval.
	add(s.RunID, "kun/control.applied", "resume")
	uhpAssertProgress(t, u, v, task, "unknown") // Stale Session.waiting is not proof of a pause.
	if err := m.update(s.ID, func(s *Session) { s.Status = "running" }); err != nil {
		t.Fatal(err)
	}
	uhpAssertProgress(t, u, v, task, "running")
}

func TestUHPProgressDemoApprovalIsObservedWithoutResolvingIt(t *testing.T) {
	m := testManager(t)
	task, err := m.Enqueue(TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "审批"}})
	if err != nil {
		t.Fatal(err)
	}
	taskWait(t, m, task.ID, "waiting")
	task, _ = m.Task(task.ID)
	v := &uhpRecord{TaskID: task.ID, HostSessionID: task.SessionID, Projection: newUHPProjection(uhpResponse{})}
	u := &uhpService{server: &Server{Manager: m}}
	got := uhpAssertProgress(t, u, v, task, "waiting_approval")
	if got.RequiredAction == nil || len(m.Approvals(task.SessionID)) == 0 {
		t.Fatal("native demo pending approval was not retained")
	}
	if _, err = m.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUHPProgressKunMismatchedEnvelopeAndBoundedControls(t *testing.T) {
	m, u, v, task, s := uhpProgressFixture(t, "kun", "waiting")
	if _, err := m.Store.Add(s.ID, "in", "kun/approval.requested", p.Event{SessionID: "another-session", RunID: s.RunID, Type: "kun/approval.requested"}); err != nil {
		t.Fatal(err)
	}
	uhpAssertProgress(t, u, v, task, "unknown")
	if _, err := m.Store.Add(s.ID, "in", "kun/run.paused", p.Event{SessionID: s.ID, RunID: s.RunID, Type: "kun/run.paused"}); err != nil {
		t.Fatal(err)
	}
	// A truncated control tail cannot prove a still-pending pause/approval.
	for range 1000 {
		if _, err := m.Store.Add(s.ID, "in", "kun/control.applied", p.Event{SessionID: s.ID, RunID: s.RunID, Type: "kun/control.applied", Data: p.JSON(map[string]any{"command": p.Control{Operation: "set_harness"}})}); err != nil {
			t.Fatal(err)
		}
	}
	uhpAssertProgress(t, u, v, task, "unknown")
}
