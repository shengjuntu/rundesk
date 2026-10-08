package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestQueuedApplicationTaskRechecksSubmittingCredential(t *testing.T) {
	for _, mode := range []string{"revoked", "expired", "missing", "scope"} {
		t.Run(mode, func(t *testing.T) {
			m := testManager(t)
			q := m.Queue()
			q.Paused = true
			m.SaveQueue(q)
			wid := m.Workspaces()[0].ID
			a, k, token := setupKey(t, m, "queued-auth", wid, "read", "run")
			h := NewHandler(m, userTestAdmin, true)
			r := appRequest(h, "POST", "/api/v1/tasks", `{"workspaceId":"`+wid+`","input":{"text":"hello"}}`, token, "auth-task-0001")
			if r.Code != 202 {
				t.Fatal(r.Code, r.Body.String())
			}
			var task Task
			if err := json.Unmarshal(r.Body.Bytes(), &task); err != nil {
				t.Fatal(err)
			}
			if task.SubmittingKeyID != k.ID {
				t.Fatal("missing submitting key", task)
			}
			// Verify provenance survives a storage round-trip, not only in-memory state.
			var stored Task
			if err := m.Store.Get("task", task.ID, &stored); err != nil {
				t.Fatal(err)
			}
			if stored.SubmittingKeyID != k.ID {
				t.Fatal("provenance not persisted")
			}
			switch mode {
			case "revoked":
				if _, err := m.RevokeApplicationKey(a.AppID, k.ID); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := m.Store.Delete("application-key", k.ID); err != nil {
					t.Fatal(err)
				}
			default:
				var record keyRecord
				if err := m.Store.Get("application-key", k.ID, &record); err != nil {
					t.Fatal(err)
				}
				if mode == "expired" {
					record.Key.ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339)
				} else {
					record.Key.Scopes = []string{"read"}
				}
				if err := m.Store.Put("application-key", k.ID, record); err != nil {
					t.Fatal(err)
				}
			}
			q = m.Queue()
			q.Paused = false
			m.SaveQueue(q)
			m.queueTick()
			done := taskWait(t, m, task.ID, "failed")
			if done.Reason == "" || done.RunID != "" {
				t.Fatal("unauthorized task admitted", done)
			}
			session, err := m.Session(done.SessionID)
			if err != nil || session.RunID != "" {
				t.Fatal("agent started", session, err)
			}
		})
	}
}

func TestScheduledCredentialPropagationAndReauthorization(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	wid := m.Workspaces()[0].ID
	a, k, token := setupKey(t, m, "schedule-auth", wid, "read", "run", "schedules")
	h := NewHandler(m, userTestAdmin, true)
	payload := `{"name":"test","cron":"* * * * *","timezone":"UTC","enabled":true,"misfire":"once","overlap":"queue","task":{"workspaceId":"` + wid + `","input":{"text":"hello"}}}`
	r := appRequest(h, "POST", "/api/v1/schedules", payload, token, "auth-schedule-0001")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var schedule Schedule
	json.Unmarshal(r.Body.Bytes(), &schedule)
	if schedule.SubmittingKeyID != k.ID {
		t.Fatal("missing schedule key")
	}
	due, _ := time.Parse(time.RFC3339Nano, schedule.NextAt)
	m.scheduleTick(due)
	if len(m.Tasks()) != 1 || m.Tasks()[0].SubmittingKeyID != k.ID {
		t.Fatal("cron lost provenance", m.Tasks())
	}
	m.RevokeApplicationKey(a.AppID, k.ID)
	m.scheduleTick(due.Add(time.Minute))
	disabled, _ := m.Schedule(schedule.ID)
	if disabled.Spec.Enabled || disabled.NextAt != "" || disabled.LastReason == "" || len(m.Tasks()) != 1 {
		t.Fatal("revoked cron ran", disabled)
	}
	replacement, newToken, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "replacement", WorkspaceIDs: []string{wid}, Scopes: []string{"read", "run", "schedules"}})
	if err != nil {
		t.Fatal(err)
	}
	disabled.Spec.Enabled = true
	data, _ := json.Marshal(map[string]any{"spec": disabled.Spec, "revision": disabled.Revision})
	r = appRequest(h, http.MethodPut, "/api/v1/schedules/"+schedule.ID, string(data), newToken, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	fresh, _ := m.Schedule(schedule.ID)
	if fresh.SubmittingKeyID != replacement.ID || !fresh.Spec.Enabled {
		t.Fatal("reauthorization failed", fresh)
	}
	// The old occurrence retains its original authority; saving a cron cannot launder it.
	q = m.Queue()
	q.Paused = false
	m.SaveQueue(q)
	m.queueTick()
	taskWait(t, m, m.Tasks()[0].ID, "failed")
}

func TestTaskCredentialCannotBeSpoofedAndRunningTaskIsNotStopped(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	a, k, token := setupKey(t, m, "active-auth", wid, "read", "run")
	h := NewHandler(m, userTestAdmin, true)
	r := appRequest(h, "POST", "/api/v1/tasks", `{"workspaceId":"`+wid+`","submittingKeyId":"fake","input":{"text":"hello"}}`, token, "spoof-authority-0001")
	if r.Code == 202 {
		t.Fatal("caller controlled submitting key")
	}
	r = appRequest(h, "POST", "/api/v1/tasks", `{"workspaceId":"`+wid+`","input":{"text":"审批"}}`, token, "active-authority-0001")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	var task Task
	json.Unmarshal(r.Body.Bytes(), &task)
	taskWait(t, m, task.ID, "waiting")
	m.RevokeApplicationKey(a.AppID, k.ID)
	m.queueTick()
	current, _ := m.Task(task.ID)
	if current.Status != "waiting" || strings.Contains(current.Reason, "凭据") {
		t.Fatal("running task was implicitly stopped", current)
	}
	m.CancelTask(task.ID)
}

func TestQueuedCredentialSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	wid := m.Workspaces()[0].ID
	a, k, _ := setupKey(t, m, "restart-auth", wid, "read", "run")
	task, err := m.Enqueue(TaskSpec{SubmittingKeyID: k.ID, WorkspaceID: wid, InstanceID: a.InstanceID, Source: SessionSource{Kind: "application", AppID: a.AppID}, Input: Input{Text: "hello"}})
	if err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()
	m, err = New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	loaded, err := m.Task(task.ID)
	if err != nil || loaded.SubmittingKeyID != k.ID {
		t.Fatal("restart lost authority", loaded, err)
	}
	if _, err = m.RevokeApplicationKey(a.AppID, k.ID); err != nil {
		t.Fatal(err)
	}
	q = m.Queue()
	q.Paused = false
	m.SaveQueue(q)
	m.queueTick()
	taskWait(t, m, task.ID, "failed")
}
