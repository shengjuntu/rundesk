package app

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKunRuntimeHarnessHTTPAuthorizationAndRecovery(t *testing.T) {
	m := kunMCPManager(t)
	m.Kun = buildKunTestBinary(t)
	w := m.Workspaces()[0]
	a, _, runner := setupKey(t, m, "harness-owner", w.ID, "read", "run", "approvals")
	var plans, acts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		_ = json.NewDecoder(r.Body).Decode(&q)
		text := "final answer"
		if q["tool_choice"] == "none" {
			plans.Add(1)
			text = "private test plan"
		} else {
			acts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: text}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 3}})
	}))
	defer server.Close()
	i, _ := m.Instance(a.InstanceID)
	if _, err := m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: server.URL + "/v1", Model: "fixture", PauseBeforeModel: true}); err != nil {
		t.Fatal(err)
	}
	admin := "harness-admin-01234567890123456789"
	h := NewHandler(m, admin, true)
	res := appRequest(h, "POST", "/api/v1/sessions", string(p.JSON(map[string]string{"workspaceId": w.ID})), runner, "harness-session-01")
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	var session Session
	_ = json.Unmarshal(res.Body.Bytes(), &session)
	if _, err := m.Start(session.ID, Input{Text: "answer the task"}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/sessions/" + session.ID + "/kun/"
	state := func() p.State {
		t.Helper()
		res := appRequest(h, "GET", path+"state", "", admin, "")
		var s p.State
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &s) != nil {
			t.Fatal(res.Code, res.Body.String())
		}
		return s
	}
	paused := func(step int) p.State {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			s := state()
			if s.Status == "paused" && s.Step == step {
				return s
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("pause not reached", state())
		return p.State{}
	}
	waitState(t, m, session.ID, "waiting")
	before := paused(0)
	cmd := p.Control{RequestID: "harness-change-01", RunID: before.RunID, ExpectedRevision: before.Revision, Operation: "set_harness", Harness: &p.HarnessConfig{LoopPolicy: "plan-act-v1"}, Reason: "compare planning without changing defaults"}
	body := string(p.JSON(cmd))
	for _, prefix := range []string{"/api/v1/", "/api/"} {
		res = appRequest(h, "POST", prefix+"sessions/"+session.ID+"/kun/control", body, runner, "")
		if res.Code != 403 {
			t.Fatal("application may mutate Harness", res.Code, res.Body.String())
		}
	}
	if state().Revision != before.Revision || plans.Load() != 0 {
		t.Fatal("denied command mutated state")
	}
	res = appRequest(h, "POST", path+"control", body, admin, "")
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	receipt := res.Body.String()
	res = appRequest(h, "POST", path+"control", body, admin, "")
	if res.Code != 200 || res.Body.String() != receipt {
		t.Fatal("duplicate did not return receipt", res.Body.String())
	}
	after := state()
	if after.Harness.Revision != 2 || after.RuntimeHarness == nil || after.Status != "paused" || plans.Load() != 0 {
		t.Fatal(after)
	}
	currentInstance, _ := m.Instance(i.ID)
	if currentInstance.AgentRuntime.Harness.LoopPolicy != "tool-loop-v1" {
		t.Fatal("default changed")
	}
	control := func(op string) {
		t.Helper()
		s := state()
		res := appRequest(h, "POST", path+"control", string(p.JSON(p.Control{RequestID: fmt.Sprintf("http-%s-%d-%s", s.RunID, s.Revision, op), RunID: s.RunID, ExpectedRevision: s.Revision, Operation: op})), admin, "")
		if res.Code != 200 {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	control("step")
	planned := paused(1)
	if plans.Load() != 1 || acts.Load() != 0 || planned.Modules["planning"].Phase != "ready" {
		t.Fatal(planned)
	}
	control("cancel")
	waitState(t, m, session.ID, "interrupted")
	res = appRequest(h, "GET", path+"checkpoint", "", admin, "")
	var check p.CheckpointCheck
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &check) != nil || !check.Eligible {
		t.Fatal(res.Code, res.Body.String())
	}
	res = appRequest(h, "POST", path+"resume", string(p.JSON(check.Selection)), admin, "resume-runtime-harness-01")
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	resumed := paused(1)
	if resumed.RunID == before.RunID || resumed.Harness.Revision != 2 || resumed.RuntimeHarness == nil {
		t.Fatal(resumed)
	}
	control("resume")
	waitState(t, m, session.ID, "completed")
	if plans.Load() != 1 || acts.Load() != 1 {
		t.Fatal("resume replanned", plans.Load(), acts.Load())
	}
	rows, _ := m.Store.Events(session.ID, 0, 1000)
	seen := false
	for _, row := range rows {
		if row.Method == "kun/control.applied" && strings.Contains(string(row.Data), `"harnessChange"`) {
			seen = true
		}
	}
	if !seen {
		t.Fatal("Harness change not projected")
	}
	if _, err := m.Start(session.ID, Input{Text: "next ordinary turn"}); err != nil {
		t.Fatal(err)
	}
	next := paused(0)
	if next.RuntimeHarness != nil || next.Harness.ID != "tool-loop-v1" || next.Harness.Revision != 3 {
		t.Fatal("default not restored", next)
	}
	control("resume")
	waitState(t, m, session.ID, "completed")
}
