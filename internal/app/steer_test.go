package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSteerActiveTurnAndIdempotency(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "", "")
	if _, err := m.Start(s.ID, Input{Text: "审批"}); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, m, s.ID, "waiting")
	in := SteerInput{Input: Input{Text: "先解释，不修改文件。"}, ExpectedTurnID: s.TurnID, RequestID: "repeat-safe-123"}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := m.Steer(s.ID, in)
			if err != nil || r.Status != "accepted" || r.TurnID != s.TurnID {
				t.Errorf("receipt %+v: %v", r, err)
			}
		}()
	}
	wg.Wait()
	current, _ := m.Session(s.ID)
	if current.RunID != s.RunID || current.TurnID != s.TurnID || current.Status != "waiting" {
		t.Fatal("steer changed active run", current)
	}
	events, _ := m.Store.Events(s.ID, 0, 1000)
	calls, accepted, starts := 0, 0, 0
	for _, e := range events {
		if e.Method == "turn/steer" && e.Direction == "out" {
			calls++
		}
		if e.Method == "run/steer" {
			accepted++
		}
		if e.Method == "run/input" {
			starts++
		}
	}
	if calls != 1 || accepted != 1 || starts != 1 {
		t.Fatal(calls, accepted, starts)
	}
	text, err := m.Markdown(s.ID)
	if err != nil || !strings.Contains(text, in.Text) {
		t.Fatal(text, err)
	}
	trace, _, err := m.Store.TraceEvents(s.ID, 0, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range trace {
		if e.Method == "run/steer" {
			found = true
		}
	}
	if !found {
		t.Fatal("steer absent from trace")
	}
	in.Text = "different"
	if _, err = m.Steer(s.ID, in); err == nil {
		t.Fatal("reused request ID with different content accepted")
	}
	in.RequestID = "stale-turn-123"
	in.ExpectedTurnID = "stale"
	if _, err = m.Steer(s.ID, in); err == nil {
		t.Fatal("stale turn accepted")
	}
	if err = m.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "interrupted")
	in.ExpectedTurnID = s.TurnID
	in.RequestID = "after-end-123"
	if _, err = m.Steer(s.ID, in); err == nil {
		t.Fatal("ended turn accepted")
	}
	if err = m.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := m.Store.List("steer")
	if len(rows) != 0 {
		t.Fatal("receipts survived deletion")
	}
}
func TestSteerHTTPPreconditionsAndUnknownReceipt(t *testing.T) {
	m := testManager(t)
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	handler := NewHandler(m, "", true)
	in := SteerInput{Input: Input{Text: "hello"}, ExpectedTurnID: "stale", RequestID: "valid-request-1"}
	body, _ := json.Marshal(in)
	resp := request(handler, "POST", "/api/sessions/"+s.ID+"/steer", string(body), "", "")
	if resp.Code != 409 {
		t.Fatal(resp.Code, resp.Body.String())
	}
	if _, err := m.Start(s.ID, Input{Text: "审批"}); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, m, s.ID, "waiting")
	in.ExpectedTurnID = s.TurnID
	// A previously sent request whose outcome was lost must never be resent.
	encoded, _ := json.Marshal(in)
	saved := storedSteer{SessionID: s.ID, Receipt: SteerReceipt{RequestID: in.RequestID, TurnID: s.TurnID, Status: "submitting"}, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(encoded))}
	if err := m.Store.Put("steer", s.ID+":"+in.RequestID, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Steer(s.ID, in); err == nil {
		t.Fatal("uncertain request resent")
	}
	events, _ := m.Store.Events(s.ID, 0, 1000)
	for _, e := range events {
		if e.Method == "turn/steer" {
			t.Fatal("uncertain request transmitted")
		}
	}
	_ = m.Stop(s.ID)
	waitState(t, m, s.ID, "interrupted")
}
