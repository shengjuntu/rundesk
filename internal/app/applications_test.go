package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func appCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *apiError
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func TestApplicationRegistration(t *testing.T) {
	m := testManager(t)
	i, e := m.CreateInstance(InstancePatch{Name: "News"})
	if e != nil {
		t.Fatal(e)
	}
	p := ApplicationInput{Name: "News", InstanceID: i.ID, WorkspaceID: m.Workspaces()[0].ID, EntryURL: "http://localhost:18080"}
	a, e := m.RegisterApplication("news", p)
	if e != nil {
		t.Fatal(e)
	}
	rows, _ := m.Applications()
	if len(rows) != 1 || rows[0].SessionCount != 0 || len(m.Sessions()) != 0 {
		t.Fatal(rows)
	}
	again, e := m.RegisterApplication("news", p)
	if e != nil || !reflect.DeepEqual(a, again) {
		t.Fatal(again, e)
	}
	p.Name = "Changed"
	_, e = m.RegisterApplication("news", p)
	appCode(t, e, "revision_conflict")
	rev := 0
	p.Revision = &rev
	a, e = m.RegisterApplication("news", p)
	if e != nil || a.Revision != 1 {
		t.Fatal(a, e)
	}
	_, e = m.RegisterApplication("news", p)
	if e != nil {
		t.Fatal("lost acknowledgement retry", e)
	}
	p.Name = "Stale"
	_, e = m.RegisterApplication("news", p)
	appCode(t, e, "revision_conflict")
	p.InstanceID = "default"
	_, e = m.RegisterApplication("new", p)
	appCode(t, e, "assistant_reserved")
	p.InstanceID = i.ID
	_, e = m.RegisterApplication("other", p)
	appCode(t, e, "application_binding_conflict")
	second, _ := m.CreateInstance(InstancePatch{Name: "Second"})
	p.InstanceID = second.ID
	_, e = m.RegisterApplication("news", p)
	appCode(t, e, "application_binding_conflict")
	for _, u := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://user:pass@example.com", "https://example.com\n"} {
		p.EntryURL = u
		_, e = m.RegisterApplication("other", p)
		appCode(t, e, "invalid_application")
	}
	h := NewHandler(m, "private-long-token-1234567890", true)
	if r := v1Request(h, "GET", "/applications", "", ""); r.Code != 401 {
		t.Fatal("registry lacks auth", r.Code)
	}
	h = NewHandler(m, "", true)
	r := v1Request(h, "GET", "/applications/news", "", "")
	if r.Code != 200 || object(t, r)["name"] != "Changed" {
		t.Fatal(r.Code, r.Body.String())
	}
	p.EntryURL = ""
	p.WorkspaceID = ""
	body, _ := json.Marshal(p)
	r = v1Request(h, "PUT", "/applications/other", string(body), "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestApplicationSourceAndConcurrentBinding(t *testing.T) {
	m := testManager(t)
	i, _ := m.CreateInstance(InstancePatch{Name: "Video"})
	wid := m.Workspaces()[0].ID
	src := SessionSource{Kind: "application", AppID: "video", TaskID: "task-1"}
	_, e := m.CreateSessionWithSource(wid, "task", "", i.ID, src)
	if e != nil {
		t.Fatal(e)
	}
	rows, _ := m.Applications()
	if len(rows) != 1 || rows[0].SessionCount != 1 || rows[0].Origin != "session-source" {
		t.Fatal(rows)
	}
	_, e = m.CreateSessionWithSource(wid, "bad", "", "default", src)
	appCode(t, e, "application_binding_conflict")
	src.AppID = "other"
	_, e = m.CreateSessionWithSource(wid, "bad", "", i.ID, src)
	appCode(t, e, "application_binding_conflict")
	_, e = m.CreateSessionWithSource(wid, "legacy", "", "default", src)
	if e != nil {
		t.Fatal(e)
	}
	other, _ := m.CreateInstance(InstancePatch{Name: "Concurrent"})
	var wg sync.WaitGroup
	success := make(chan bool, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := m.RegisterApplication(fmt.Sprintf("app-%d", n), ApplicationInput{Name: "Test", InstanceID: other.ID})
			if e == nil {
				success <- true
			}
		}(n)
	}
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatal("multiple apps claimed same identity", len(success))
	}
}
func TestApplicationMigrationKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	m, e := New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	wid := m.Workspaces()[0].ID
	a, _ := m.CreateInstance(InstancePatch{Name: "News"})
	b, _ := m.CreateInstance(InstancePatch{Name: "Shared"})
	m.CreateInstance(InstancePatch{Name: "Unassigned"})
	before := map[string]Session{}
	for n, v := range [][2]string{{a.ID, "news"}, {b.ID, "ambiguous-a"}, {b.ID, "ambiguous-b"}, {"default", "old-default"}} {
		s, e := m.CreateSession(wid, fmt.Sprintf("old-%d", n), "old-model", v[0])
		if e != nil {
			t.Fatal(e)
		}
		s.Source = SessionSource{Kind: "application", AppID: v[1], TaskID: "old-business-task"}
		s.ThreadID = "native-thread-" + s.ID
		if e = m.Store.Put("session", s.ID, s); e != nil {
			t.Fatal(e)
		}
		before[s.ID] = s
	}
	m.Close()
	m, e = New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	rows, e := m.Applications()
	if e != nil || len(rows) != 1 || rows[0].AppID != "news" || rows[0].Origin != "session-migration" {
		t.Fatal(rows, e)
	}
	for id, s := range before {
		got, e := m.Session(id)
		if e != nil || !reflect.DeepEqual(s, got) {
			t.Fatal("history changed", s, got, e)
		}
	}
	if e = m.migrateApplications(); e != nil {
		t.Fatal(e)
	}
	again, _ := m.Applications()
	if !reflect.DeepEqual(rows, again) {
		t.Fatal("migration not idempotent")
	}
	_, e = m.CreateSessionWithSource(wid, "continuation", "", b.ID, SessionSource{Kind: "application", AppID: "ambiguous-a"})
	if e != nil {
		t.Fatal(e)
	}
	again, _ = m.Applications()
	if len(again) != 1 {
		t.Fatal("ambiguous owner guessed")
	}
}
