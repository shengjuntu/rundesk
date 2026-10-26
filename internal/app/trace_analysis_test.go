package app

import (
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/tracequery"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTraceAnalysisNewThreadAndSnapshot(t *testing.T) {
	m := testManager(t)
	source, e := m.CreateSession(m.Workspaces()[0].ID, "original", "demo-fixture")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(source.ID, Input{Text: "original task"}); e != nil {
		t.Fatal(e)
	}
	source = waitState(t, m, source.ID, "completed")
	before, e := m.Store.Events(source.ID, 0, 1000)
	// The native completion callback publishes Session.Status before appending
	// run/state and expiring approvals. Capture the immutable source baseline
	// only after that callback's final journal event, not merely its state update.
	// A timed delay would still race under load and could hide a real mutation.
	journalDeadline := time.Now().Add(5 * time.Second)
	for {
		if e != nil {
			t.Fatal(e)
		}
		terminal, drained := false, false
		for _, event := range before {
			if event.Method == "run/state" {
				var state struct {
					RunID  string `json:"runId"`
					Status string `json:"status"`
				}
				if json.Unmarshal(event.Data, &state) == nil && state.RunID == source.RunID && state.Status == "completed" {
					terminal = true
				}
			}
			if terminal && event.Method == "approval/expired" {
				drained = true
				break
			}
		}
		if drained {
			break
		}
		if time.Now().After(journalDeadline) {
			t.Fatal("source terminal callback did not finish its journal")
		}
		time.Sleep(time.Millisecond)
		before, e = m.Store.Events(source.ID, 0, 1000)
	}
	selection := TraceSelection{SessionID: source.ID, RunID: source.RunID, EventIDs: []int64{before[0].ID}}
	h := NewHandler(m, "", true)
	body, _ := json.Marshal(map[string]any{"traceAnalysis": selection})
	create := func() Session {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "http://localhost/api/v1/sessions", strings.NewReader(string(body)))
		req.Header.Set("Idempotency-Key", "trace-analysis-idempotency-01")
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var s Session
		if e = json.Unmarshal(w.Body.Bytes(), &s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	analysis := create()
	replay := create()
	if analysis.ID == source.ID || analysis.ID != replay.ID || analysis.ThreadID != "" || analysis.TraceOrigin == nil || analysis.Source.Kind != "human" {
		t.Fatal(analysis, replay)
	}
	if analysis.InstanceID != source.InstanceID || analysis.WorkspaceID != source.WorkspaceID || analysis.Model != source.Model {
		t.Fatal("configuration changed")
	}
	if _, e = m.Start(analysis.ID, Input{Text: "Explain the trace"}); e != nil {
		t.Fatal(e)
	}
	analysis = waitState(t, m, analysis.ID, "completed")
	if analysis.ThreadID == source.ThreadID {
		t.Fatal("analysis reused original native thread")
	}
	after, e := m.Store.Events(source.ID, 0, 1000)
	if e != nil {
		t.Fatal(e)
	}
	old, e := m.Session(source.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(before) != len(after) || !reflect.DeepEqual(before, after) || old.RunID != source.RunID || old.ThreadID != source.ThreadID {
		t.Fatal("source run modified")
	}
	events, _ := m.Store.Events(analysis.ID, 0, 1000)
	found := false
	instructions := false
	for _, ev := range events {
		if ev.Method == "thread/start" && ev.Direction == "out" {
			var d map[string]any
			json.Unmarshal(ev.Data, &d)
			p := d["params"].(map[string]any)
			if p["sandbox"] != "read-only" {
				t.Fatal(p)
			}
			if p["config"].(map[string]any)["mcp_servers.rundesk_trace"] == nil {
				t.Fatal(p)
			}
			found = true
		}
		if ev.Method == "turn/start" && strings.Contains(string(ev.Data), "trace_find_steps") {
			instructions = true
		}
	}
	if !found || !instructions {
		t.Fatal("missing session-local tool configuration or instructions")
	}
	r, e := tracequery.Open(filepath.Join(m.Data, "state.db"), source.ID, analysis.TraceOrigin.Through)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	newer, _ := m.Store.Add(source.ID, "in", "warning", map[string]string{"message": "later"})
	if _, e = r.Call("trace_read_event", tracequery.Args{EventID: newer.ID}); e == nil {
		t.Fatal("snapshot changed")
	}
	other, _ := m.CreateSession(source.WorkspaceID, "other", "")
	foreign, _ := m.Store.Add(other.ID, "in", "error", map[string]string{"message": "foreign"})
	selection.EventIDs = []int64{foreign.ID}
	if _, e = m.CreateTraceAnalysis(selection); e == nil {
		t.Fatal("cross-session selection accepted")
	}
	selection.EventIDs = nil
	selection.RunID = "missing"
	if _, e = m.CreateTraceAnalysis(selection); e == nil {
		t.Fatal("unknown run accepted")
	}
	var saved Session
	if e = m.Store.Get("session", analysis.ID, &saved); e != nil || saved.TraceOrigin.SessionID != source.ID {
		t.Fatal("lost relation", e)
	}
}
