package kun

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDebugBoundariesAndOnceRules(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if models.Add(1) == 1 {
			modelTools(w, []p.ToolCall{writeCall("one", "one.txt"), writeCall("two", "two.txt")}, map[string]int{"total_tokens": 12})
		} else {
			final(w, "done")
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.Debug = p.DebugPolicy{Breakpoints: []p.Breakpoint{
		{ID: "before", Phase: "before_model", Model: "fixture", Once: true},
		{ID: "after", Phase: "after_model", MinReportedTokens: 10, MinStep: 2},
		{ID: "write", Phase: "before_tool", Tool: "write_file", Once: true},
		{ID: "written", Phase: "after_tool", Tool: "write_file", MinToolCalls: 2},
	}}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	if models.Load() != 0 || s.Phase != "before_model" || s.Debug.Pause.RuleIDs[0] != "before" {
		t.Fatal(s)
	}
	// Caller-owned configuration changes do not alter the run's frozen rules.
	in.Config.Debug.Breakpoints[0].Phase = "after_tool"
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	if s.Phase != "before_tool" || s.Debug.Hits["write"] != 1 || s.Debug.Pause.CallID != "one" {
		t.Fatal(s)
	}
	if _, err := os.Stat(filepath.Join(in.Workspace, "one.txt")); !os.IsNotExist(err) {
		t.Fatal("tool dispatched while paused")
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	if s.Phase != "after_tool" || s.Debug.Pause.CallID != "two" || s.Budget.ToolCalls != 2 || models.Load() != 1 {
		t.Fatal(s)
	}
	if _, err := os.Stat(filepath.Join(in.Workspace, "two.txt")); err != nil {
		t.Fatal(err)
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	if s.Phase != "after_model" || s.Step != 2 || len(s.Pending) != 0 || s.Debug.Hits["before"] != 1 || models.Load() != 2 {
		t.Fatal(s)
	}
	checkpointControl(t, e, "resume")
	waitKun(t, e, "completed")
	if models.Load() != 2 {
		t.Fatal("final-answer breakpoint invoked extra model call")
	}
}
func TestDebugFailurePauseAndRuntimePolicyReceipts(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if models.Add(1) == 1 {
			modelTools(w, []p.ToolCall{{ID: "bad", Type: "function", Function: p.Function{Name: "write_file", Arguments: `{"path":42}`}}, writeCall("good", "out.txt")}, nil)
		} else {
			final(w, "done")
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.PauseBeforeModel = true
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	policy := p.DebugPolicy{Breakpoints: []p.Breakpoint{{ID: "failure", Phase: "after_tool", MinFailures: 1}}}
	command := p.Control{RequestID: "set-runtime-rules", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "set_breakpoints", Debug: &policy}
	receipt, err := e.Control(command)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.Control(command)
	if err != nil || receipt != again || e.State().Debug.Revision != 2 {
		t.Fatal(receipt, again, err)
	}
	conflict := command
	conflict.Debug = &p.DebugPolicy{}
	if _, err = e.Control(conflict); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	stale := command
	stale.RequestID = "set-stale-rules"
	if _, err = e.Control(stale); err == nil {
		t.Fatal("stale policy edit accepted")
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	if s.Phase != "after_tool" || s.Actions["bad"] != "rejected" || s.Budget.ToolCalls != 0 || len(s.Pending) != 1 {
		t.Fatal(s)
	}
	if _, err = os.Stat(filepath.Join(in.Workspace, "out.txt")); !os.IsNotExist(err) {
		t.Fatal("next tool ran before inspection")
	}
	// Disabling future rules does not release the current pause.
	command = p.Control{RequestID: "clear-runtime-rules", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "set_breakpoints", Debug: &p.DebugPolicy{}}
	if _, err = e.Control(command); err != nil {
		t.Fatal(err)
	}
	if e.State().Status != "paused" || e.State().Debug.Pause == nil {
		t.Fatal("rule edit resumed execution")
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	if s.Phase != "before_model" || s.Budget.ToolCalls != 1 {
		t.Fatal(s)
	}
	checkpointControl(t, e, "resume")
	waitKun(t, e, "completed")
}
func TestDebugPauseTimeoutStopsWithoutConsumingActiveBudget(t *testing.T) {
	e := newTestEngine(t)
	in := startRequest(t, "http://127.0.0.1:1")
	in.Config.Debug = p.DebugPolicy{Breakpoints: []p.Breakpoint{{ID: "wait", Phase: "before_model"}}, PauseTimeoutSeconds: 1}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	if s.Debug.Pause.Deadline == "" {
		t.Fatal("missing durable timeout")
	}
	s = waitTerminal(t, e)
	if s.Status != "interrupted" || !strings.Contains(s.Error, "debug pause timeout") || s.Step != 0 || s.Budget.ToolCalls != 0 || s.Budget.WaitMillis < 900 || s.Budget.ActiveMillis >= 900 {
		t.Fatal(s)
	}
}
func TestDebugQueryIsReadOnlyAndSnapshotPinned(t *testing.T) {
	e := newTestEngine(t)
	in := startRequest(t, "http://127.0.0.1:1")
	in.Config.PauseBeforeModel = true
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	before := waitKun(t, e, "paused")
	events := allKunEvents(t, e)
	seq := events[len(events)-1].Sequence
	for _, kind := range []string{"run", "context", "tools", "budget", "modules", "breakpoints", "actions"} {
		result, err := e.Query(p.DebugQuery{Kind: kind, Sequence: seq})
		if err != nil || result.Sequence != seq || result.Revision != before.Revision || result.RunID != before.RunID {
			t.Fatal(result, err)
		}
	}
	if !reflect.DeepEqual(before, e.State()) || len(allKunEvents(t, e)) != len(events) {
		t.Fatal("read-only query changed state or journal")
	}
	if _, err := e.Query(p.DebugQuery{Kind: "shell:rm"}); err == nil {
		t.Fatal("unknown query accepted")
	}
	if _, err := e.Query(p.DebugQuery{Kind: "run", Sequence: seq + 999}); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	if _, err := e.Control(p.Control{RequestID: "query-policy-edit", RunID: before.RunID, ExpectedRevision: before.Revision, Operation: "set_breakpoints", Debug: &p.DebugPolicy{PauseTimeoutSeconds: 5}}); err != nil {
		t.Fatal(err)
	}
	historic, err := e.Query(p.DebugQuery{Kind: "breakpoints", Sequence: seq})
	if err != nil {
		t.Fatal(err)
	}
	live, err := e.Query(p.DebugQuery{Kind: "breakpoints"})
	if err != nil || live.Revision <= historic.Revision || string(live.Data) == string(historic.Data) {
		t.Fatal(live, historic, err)
	}
}
func TestDebugMCPBreakpointDoesNotGrantApproval(t *testing.T) {
	var calls, models atomic.Int32
	server := mcpFixture(t, &calls, "", nil)
	defer server.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	e := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.DisabledTools = []string{"delete_item"}
	in.MCP = []p.MCPServer{spec}
	in.Config.Debug = p.DebugPolicy{Breakpoints: []p.Breakpoint{{ID: "mcp", Phase: "before_tool", Tool: mcpAlias("weather", "lookup weather")}}}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	if s.Phase != "before_tool" || s.Approval != nil || calls.Load() != 0 {
		t.Fatal(s)
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	if s.Phase != "approval" || s.Approval == nil || s.Debug.Pause != nil || calls.Load() != 0 {
		t.Fatal(s)
	}
	if _, err := e.Control(p.Control{RequestID: "cannot-skip-approval", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"}); err == nil {
		t.Fatal("breakpoint resume bypassed approval")
	}
	checkpointControl(t, e, "approve")
	waitKun(t, e, "completed")
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
func TestDebugPolicyValidation(t *testing.T) {
	for _, raw := range []string{`{"breakpoints":[{"id":"a","phase":"before_model","script":"true"}]}`, `{"expression":"x > 1"}`} {
		var policy p.DebugPolicy
		if json.Unmarshal([]byte(raw), &policy) == nil {
			t.Fatal("unknown conditions ignored", raw)
		}
	}
	for _, policy := range []p.DebugPolicy{
		{Breakpoints: []p.Breakpoint{{ID: "a", Phase: "approval"}}},
		{Breakpoints: []p.Breakpoint{{ID: "a", Phase: "before_model", Tool: "write_file"}}},
		{Breakpoints: []p.Breakpoint{{ID: "a", Phase: "before_model", MinStep: -1}}},
		{Breakpoints: []p.Breakpoint{{ID: "a", Phase: "before_tool"}, {ID: "a", Phase: "after_tool"}}},
		{PauseTimeoutSeconds: 86401},
	} {
		if policy.Validate() == nil {
			t.Fatal("invalid policy accepted", policy)
		}
	}
}

func TestDebugOnceRuleSurvivesCheckpoint(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { models.Add(1); final(w, "recovered") }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, server.URL)
	in.Config.Debug = p.DebugPolicy{Breakpoints: []p.Breakpoint{{ID: "once", Phase: "before_model", Once: true}}}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	e.Close()
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	check, err := e.Checkpoint()
	if err != nil || !check.Eligible {
		t.Fatal(check, err)
	}
	in.RunID = "debug-resumed"
	in.Resume = &check.Selection
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	if models.Load() != 1 || s.Debug.Revision != 1 || s.Debug.Hits["once"] != 1 || s.Debug.Pause != nil {
		t.Fatal(s)
	}
}
