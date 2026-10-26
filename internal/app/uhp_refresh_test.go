package app

import (
	"strings"
	"testing"
)

// A native interrupt can finish a final item while CancelTask waits for its
// reply. Model that deterministic ordering without relying on scheduler timing.
type uhpClosingTaskControl struct {
	*Manager
	task        Task
	closingText string
	lastEventID int64
}

func (c *uhpClosingTaskControl) Task(id string) (Task, error) {
	if id != c.task.ID {
		return Task{}, failure(404, "task_not_found", "")
	}
	return c.task, nil
}

func (c *uhpClosingTaskControl) CancelTask(id string) (Task, error) {
	if id != c.task.ID {
		return Task{}, failure(404, "task_not_found", "")
	}
	params := map[string]any{"turnId": "native-turn", "itemId": "native-message", "delta": c.closingText}
	event, err := c.Store.Add(c.task.SessionID, "in", "item/agentMessage/delta", map[string]any{"params": params})
	if err != nil {
		return Task{}, err
	}
	c.lastEventID = event.ID
	c.task.Status = "interrupted"
	err = c.update(c.task.SessionID, func(s *Session) { s.Status = "interrupted" })
	return c.task, err
}

func TestUHPRefreshDrainsNativeOutputAfterSynchronousCancellation(t *testing.T) {
	m := testManager(t)
	session, err := m.CreateSession(m.Workspaces()[0].ID, "closing partial output", "demo-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.update(session.ID, func(s *Session) {
		s.RunID, s.TurnID, s.Status = "native-run", "native-turn", "running"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Store.Add(session.ID, "internal", "run/input", map[string]any{"runId": "native-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Store.Add(session.ID, "in", "item/agentMessage/delta", map[string]any{"params": map[string]any{"turnId": "native-turn", "itemId": "native-message", "delta": "partial "}}); err != nil {
		t.Fatal(err)
	}
	control := &uhpClosingTaskControl{Manager: m, task: Task{ID: "native-task", SessionID: session.ID, RunID: "native-run", Status: "running"}, closingText: "closing output"}
	u := uhpService{server: &Server{Manager: m, Tasks: control}}
	record := uhpRecord{Owner: "administrator", TaskID: control.task.ID, HostSessionID: session.ID, SessionID: "hsessclosing", ContainerID: "cntr_closing", CancelReason: "cancelled", Projection: newUHPProjection(uhpResponse{ID: "resp_closing", Model: "demo-fixture", Store: true, Metadata: map[string]any{"session_id": "hsessclosing"}})}
	m.uhpMu.Lock()
	err = u.refresh(&record)
	m.uhpMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if record.Projection.Response.Status != "cancelled" || record.Cursor != control.lastEventID {
		t.Fatalf("finalization missed the post-interrupt event: status=%s cursor=%d last=%d", record.Projection.Response.Status, record.Cursor, control.lastEventID)
	}
	var text strings.Builder
	for _, event := range record.Projection.Events {
		if event["type"] == "response.output_text.delta" {
			text.WriteString(uhpString(event["delta"]))
		}
	}
	if text.String() != "partial closing output" {
		t.Fatalf("cancel discarded native output: %q", text.String())
	}
	uhpAssertProjectionSequence(t, record.Projection)
	retained := uhpTestRecord(t, m, record.Projection.Response.ID)
	if uhpJSON(retained.Projection.Response) != uhpJSON(record.Projection.Response) {
		t.Fatal("persisted terminal response differs from final stream snapshot")
	}
}
