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
)

func TestKunPlanActProcessAndConfigHTTP(t *testing.T) {
	m, err := New(t.TempDir(), "missing-codex-for-kun-test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Kun = buildKunTestBinary(t)
	var plans, acts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		_ = json.NewDecoder(r.Body).Decode(&q)
		text := "final action response"
		if q["tool_choice"] == "none" {
			plans.Add(1)
			text = "internal proposed plan"
		} else {
			acts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: text}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 3}})
	}))
	defer provider.Close()
	i, _ := m.Instance()
	h := NewHandler(m, "", true)
	cfg := p.Config{Kind: "kun", Endpoint: provider.URL + "/v1", Model: "fixture", Harness: p.HarnessConfig{LoopPolicy: "plan-act-v1"}}
	save := func(c p.Config, revision int) *httptest.ResponseRecorder {
		return request(h, "PUT", "/api/instances/"+i.ID+"/agent-runtime", string(p.JSON(map[string]any{"revision": revision, "config": c})), "", "")
	}
	bad := cfg
	bad.Harness.Planning = "no-explicit-plan-v1"
	if res := save(bad, i.Revision); res.Code < 400 {
		t.Fatal("HTTP accepted incompatible composition", res.Body.String())
	}
	res := save(cfg, i.Revision)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "explicit-plan-v1") {
		t.Fatal("composition defaults missing", res.Body.String())
	}
	session, err := m.CreateSession(m.Workspaces()[0].ID, "Plan-Act", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(session.ID, Input{Text: "answer the task"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "completed")
	rows, err := m.Store.Events(session.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var planID, finalID, sequence int64
	for _, event := range rows {
		if event.Method != "kun/model.completed" {
			continue
		}
		var ev p.Event
		_ = json.Unmarshal(event.Data, &ev)
		if strings.Contains(string(ev.Data), `"purpose":"plan"`) {
			planID = event.ID
			sequence = ev.Sequence
		} else {
			finalID = event.ID
		}
	}
	if plans.Load() != 1 || acts.Load() != 1 || planID == 0 || finalID == 0 {
		t.Fatal("missing process evidence", plans.Load(), acts.Load())
	}
	if _, err = m.Reply(session.ID, planID); err == nil {
		t.Fatal("plan exposed as completed reply")
	}
	if reply, err := m.Reply(session.ID, finalID); err != nil || reply.Text != "final action response" {
		t.Fatal(reply, err)
	}
	markdown, err := m.Markdown(session.ID)
	if err != nil || strings.Contains(markdown, "internal proposed plan") || !strings.Contains(markdown, "final action response") {
		t.Fatal(markdown, err)
	}
	url := fmt.Sprintf("/api/sessions/%s/kun/snapshots/%d", session.ID, sequence)
	res = request(h, "GET", url, "", "", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), "internal proposed plan") {
		t.Fatal("plan missing from read-only snapshot", res.Code, res.Body.String())
	}
	var snap p.Snapshot
	_ = json.Unmarshal(res.Body.Bytes(), &snap)
	if snap.State.Harness.ID != "plan-act-v1" || snap.State.Step != 1 || snap.State.Budget.ReportedTokens != 3 {
		t.Fatal(snap)
	}
	// An existing Kun session uses the selected composition on its next ordinary
	// run. Prior snapshots remain fixed and can be compared through the same API.
	i, _ = m.Instance()
	cfg.Harness = p.HarnessConfig{}
	if res = save(cfg, i.Revision); res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	if _, err = m.Start(session.ID, Input{Text: "answer the task again"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "completed")
	res = request(h, "GET", "/api/sessions/"+session.ID+"/kun/state", "", "", "")
	var current p.State
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &current) != nil {
		t.Fatal(res.Code, res.Body.String())
	}
	if current.Harness.ID != "tool-loop-v1" || current.Harness.Revision != 2 || plans.Load() != 1 || acts.Load() != 2 {
		t.Fatal(current, plans.Load(), acts.Load())
	}
	res = request(h, "GET", url, "", "", "")
	var retained p.Snapshot
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &retained) != nil || string(p.JSON(retained)) != string(p.JSON(snap)) {
		t.Fatal("prior plan snapshot changed", res.Body.String())
	}
}
