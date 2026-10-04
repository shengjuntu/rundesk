package app

import (
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/store"
	"strings"
	"testing"
	"time"
)

func taskWait(t *testing.T, m *Manager, id, status string) Task {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		v, e := m.Task(id)
		if e != nil {
			t.Fatal(e)
		}
		if v.Status == status {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	v, _ := m.Task(id)
	t.Fatalf("want %s got %+v", status, v)
	return v
}
func TestQueueApprovalCapacityCancelAndReservation(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	q.MaxConcurrent = 1
	if _, e := m.SaveQueue(q); e != nil {
		t.Fatal(e)
	}
	spec := TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "审批"}}
	a, e := m.Enqueue(spec)
	if e != nil {
		t.Fatal(e)
	}
	b, e := m.Enqueue(spec)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(a.SessionID, Input{Text: "bypass"}); e == nil {
		t.Fatal("reserved task bypass")
	}
	yes := true
	if _, e = m.PatchSession(a.SessionID, SessionPatch{Archived: &yes}); e == nil {
		t.Fatal("archived reserved")
	}
	if e = m.DeleteSession(a.SessionID); e == nil {
		t.Fatal("deleted reserved")
	}
	q = m.Queue()
	q.Paused = false
	m.SaveQueue(q)
	taskWait(t, m, a.ID, "waiting")
	if v, _ := m.Task(b.ID); v.Status != "queued" {
		t.Fatal(v)
	}
	manual, _ := m.CreateSession(spec.WorkspaceID, "manual", "")
	if _, e = m.Start(manual.ID, Input{Text: "no bypass"}); e == nil {
		t.Fatal("interactive bypassed shared capacity")
	}
	if _, e = m.CancelTask(b.ID); e != nil {
		t.Fatal(e)
	}
	taskWait(t, m, b.ID, "canceled")
	if s, _ := m.Session(b.SessionID); s.RunID != "" {
		t.Fatal("canceled task executed")
	}
	if _, e = m.CancelTask(a.ID); e != nil {
		t.Fatal(e)
	}
	taskWait(t, m, a.ID, "interrupted")
	c, e := m.Enqueue(TaskSpec{WorkspaceID: spec.WorkspaceID, Input: Input{Text: "complete"}})
	if e != nil {
		t.Fatal(e)
	}
	taskWait(t, m, c.ID, "completed")
	end := time.Now().Add(3 * time.Second)
	for m.loaded.Load() != 0 && time.Now().Before(end) {
		time.Sleep(25 * time.Millisecond)
	}
	if m.loaded.Load() != 0 {
		t.Fatal("task connection leaked")
	}
}
func TestQueueRestartNeverRepeatsUnconfirmed(t *testing.T) {
	path := t.TempDir()
	m, e := New(path, "", true)
	if e != nil {
		t.Fatal(e)
	}
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	a, _ := m.Enqueue(TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "pending"}})
	b, _ := m.Enqueue(a.Spec)
	m.queueMu.Lock()
	b.Status = "dispatching"
	b.RunID = store.ID()
	m.saveTaskLocked(b)
	m.queueMu.Unlock()
	m.Close()
	m, e = New(path, "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	taskWait(t, m, a.ID, "queued")
	taskWait(t, m, b.ID, "unconfirmed")
}
func TestQueueAPIReplayAndCursor(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	h := NewHandler(m, "", true)
	spec := TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "test"}}
	body, _ := json.Marshal(spec)
	a := v1Request(h, "POST", "/tasks", string(body), "queue-key-0001")
	if a.Code != 202 {
		t.Fatal(a.Code, a.Body.String())
	}
	b := v1Request(h, "POST", "/tasks", string(body), "queue-key-0001")
	if b.Code != 202 || strings.TrimSpace(a.Body.String()) != strings.TrimSpace(b.Body.String()) {
		t.Fatal("bad replay", b.Body.String())
	}
	v1Request(h, "POST", "/tasks", string(body), "queue-key-0002")
	r := v1Request(h, "GET", "/tasks?limit=1", "", "")
	data := object(t, r)
	if data["nextCursor"] == "" {
		t.Fatal(data)
	}
	r = v1Request(h, "GET", "/tasks?limit=1&cursor="+data["nextCursor"].(string), "", "")
	if len(object(t, r)["items"].([]any)) != 1 {
		t.Fatal(r.Body.String())
	}
	spec.NotBefore = "tomorrow"
	if _, e := m.Enqueue(spec); e == nil {
		t.Fatal("invalid time accepted")
	}
}
