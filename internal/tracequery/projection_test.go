package tracequery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/redaction"
	"github.com/shengjuntu/rundesk/internal/store"
)

func project(t *testing.T, rows ...map[string]any) *projection {
	t.Helper()
	p := newProjection()
	for i, row := range rows {
		b, _ := json.Marshal(row["data"])
		var value any
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		if e := decoder.Decode(&value); e != nil {
			t.Fatal(e)
		}
		at := fmt.Sprintf("2026-10-05T00:00:%02dZ", i)
		if row["at"] != nil {
			at = row["at"].(string)
		}
		p.ingest(int64(i+1), at, row["method"].(string), obj(redaction.Fields(value)))
	}
	p.finish()
	return p
}
func record(method string, data any) map[string]any {
	return map[string]any{"method": method, "data": data}
}
func input(run string) map[string]any {
	return record("run/input", map[string]any{"runId": run, "input": map[string]string{"text": run}})
}
func item(method, turn, id string, fields map[string]any) map[string]any {
	v := map[string]any{"id": id, "type": "commandExecution", "command": "echo fixture"}
	for k, x := range fields {
		v[k] = x
	}
	return record("item/"+method, map[string]any{"params": map[string]any{"turnId": turn, "item": v}})
}
func worker(method, run string, sequence int64, data any) map[string]any {
	return record("kun/"+method, map[string]any{"runId": run, "sequence": sequence, "data": data})
}
func stepsOf(p *projection, kind string) []Step {
	out := []Step{}
	for _, s := range p.steps {
		if s.Type == kind {
			out = append(out, s)
		}
	}
	return out
}

func TestProjectionCodexLateEvidenceAndMissingBoundaries(t *testing.T) {
	p := project(t, input("r1"), item("started", "t1", "same", nil), input("r2"), item("started", "t2", "same", nil),
		item("completed", "t1", "same", map[string]any{"exitCode": 1}), item("completed", "t2", "same", map[string]any{"exitCode": 0}),
		item("completed", "missing-turn", "same", nil), item("completed", "t2", "", nil), item("completed", "t2", "", nil),
		record("run/state", map[string]any{"runId": "r2", "status": "completed", "error": ""}))
	steps := stepsOf(p, "commandExecution")
	if len(steps) != 5 || steps[0].RunID != "r1" || steps[0].Status != "failed" || steps[1].RunID != "r2" || steps[1].Status != "completed" {
		t.Fatal(steps)
	}
	if steps[0].DurationMS == nil || *steps[0].DurationMS != 3000 || steps[1].DurationMS == nil || *steps[1].DurationMS != 2000 {
		t.Fatal("paired wrong boundaries", steps)
	}
	if steps[2].RunID != "observed-turn-missing-turn" || steps[2].Association != "missing" || steps[2].DurationMS != nil || steps[2].StartObserved {
		t.Fatal("unknown turn attached to current", steps[2])
	}
	if steps[3].ID == steps[4].ID || steps[3].DurationMS != nil {
		t.Fatal("missing item IDs paired")
	}
	if len(stepsOf(p, "run/state")) != 0 {
		t.Fatal("empty error created phantom failure")
	}
	// Duplicate starts are ambiguous; a completion cannot invent a unique pairing.
	p = project(t, input("r"), item("started", "t", "a", nil), item("started", "t", "a", nil), item("completed", "t", "a", nil), record("run/state", map[string]string{"runId": "r", "status": "completed"}))
	steps = stepsOf(p, "commandExecution")
	if len(steps) != 3 {
		t.Fatal(steps)
	}
	for _, s := range steps {
		if s.DurationMS != nil || !strings.Contains(strings.Join(s.Issues, ","), "ambiguous_pair") {
			t.Fatal("ambiguity hidden", s)
		}
	}
}

func TestProjectionKunScopesEvidenceAndApproval(t *testing.T) {
	call := map[string]any{"id": "reused", "function": map[string]string{"name": "read_file", "arguments": "{}"}}
	p := project(t, input("r"), worker("tool.started", "r", 10, map[string]any{"step": 1, "call": call}), worker("tool.completed", "r", 11, map[string]any{"step": 1, "call": call, "status": "succeeded", "durationMs": 37}),
		worker("tool.started", "r", 12, map[string]any{"step": 2, "call": call}), worker("tool.completed", "r", 13, map[string]any{"step": 2, "call": call, "isError": true}),
		worker("mcp.request", "r", 14, map[string]any{"server": "a", "exchangeId": 1, "method": "tools/call"}), worker("mcp.request", "r", 15, map[string]any{"server": "b", "exchangeId": 1, "method": "tools/call"}),
		worker("mcp.response", "r", 16, map[string]any{"server": "b", "exchangeId": 1, "result": map[string]any{"isError": true}}), worker("mcp.response", "r", 17, map[string]any{"server": "a", "exchangeId": 1, "result": map[string]any{}}),
		worker("approval.requested", "r", 18, map[string]any{"callId": "approval"}), worker("control.applied", "r", 19, map[string]any{"command": map[string]any{"operation": "approve", "callId": "approval"}}),
		worker("model.started", "r", 20, map[string]any{"step": 3}), worker("run.finished", "r", 21, map[string]any{"status": "interrupted"}))
	tools := stepsOf(p, "toolCall")
	if len(tools) != 2 || tools[0].Status != "succeeded" || tools[1].Status != "failed" || *tools[0].ReportedDurationMS != 37 {
		t.Fatal(tools)
	}
	if len(tools[0].WorkerSequences) != 2 || tools[0].WorkerSequences[0] != 10 || tools[0].EventIDs[0] != 2 {
		t.Fatal("worker sequence confused with host ID", tools)
	}
	mcp := stepsOf(p, "mcpExchange")
	if len(mcp) != 2 || mcp[0].Status != "completed" || mcp[1].Status != "failed" || len(mcp[0].EventIDs) != 2 {
		t.Fatal(mcp)
	}
	approval := stepsOf(p, "approval")
	if len(approval) != 1 || approval[0].Status != "accepted" || !approval[0].EndObserved {
		t.Fatal(approval)
	}
	model := stepsOf(p, "modelCall")
	if len(model) != 1 || model[0].Status != "unknown" || model[0].DurationMS != nil || strings.Join(model[0].Missing, ",") != "end" {
		t.Fatal(model)
	}
}

func TestProjectionRedactionPrecisionPreviewAndClock(t *testing.T) {
	start := item("started", "t", "a", map[string]any{"authorization": "secret-header", "arguments": map[string]any{"count": json.Number("9007199254740993"), "password": "secret-password", "long": strings.Repeat("字", 9000)}})
	end := item("completed", "t", "a", map[string]any{"exitCode": 0})
	end["at"] = "2026-10-04T00:00:00Z"
	p := project(t, input("r"), start, end)
	s := stepsOf(p, "commandExecution")[0]
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "secret-") || !strings.Contains(string(b), "9007199254740993") || !s.PreviewTruncated || len(b) > 20000 || s.DurationMS != nil || !strings.Contains(strings.Join(s.Issues, ","), "invalid_clock") {
		t.Fatal(string(b))
	}
	// Numeric-only objects must also exhaust the preview bound.
	large := map[string]any{}
	for n := 0; n < 10000; n++ {
		large[fmt.Sprint(n)] = n
	}
	budget := 8192
	truncated := false
	b, _ = json.Marshal(preview(large, &budget, 0, &truncated))
	if !truncated || len(b) > 2000 {
		t.Fatal("unbounded numeric object", len(b))
	}
}

func TestProjectionFrozenPagingRunScopeLimitsAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Put("session", "source", map[string]string{"id": "source"})
	s.Add("source", "internal", "run/input", map[string]any{"runId": "r", "input": map[string]string{"text": "fixture"}})
	var last store.Event
	for n := 0; n < 55; n++ {
		last, e = s.Add("source", "in", "warning", map[string]any{"runId": "r", "message": fmt.Sprint("warn-", n), "api_key": "NEVER-SEARCHABLE"})
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e := Open(path, "source", last.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	newer, _ := s.Add("source", "in", "warning", map[string]string{"runId": "r", "message": "FUTURE"})
	v, e := r.Call("trace_find_issues", Args{RunID: "r", Limit: 50})
	if e != nil {
		t.Fatal(e)
	}
	page := v.(map[string]any)
	if page["total"] != 55 || page["hasMore"] != true || page["nextOffset"] != 50 {
		t.Fatal(page)
	}
	v, e = r.Call("trace_find_issues", Args{RunID: "r", Offset: 50})
	if e != nil || len(v.(map[string]any)["steps"].([]Step)) != 5 {
		t.Fatal(v, e)
	}
	for _, q := range []string{"NEVER-SEARCHABLE", "FUTURE"} {
		v, e = r.Call("trace_find_steps", Args{Query: q})
		if e != nil || v.(map[string]any)["total"] != 0 {
			t.Fatal(q, v, e)
		}
	}
	if _, e = r.Call("trace_get_step", Args{RunID: "outside", StepID: "step-2"}); e == nil {
		t.Fatal("run filter bypass")
	}
	if _, e = r.Call("trace_read_event", Args{EventID: newer.ID}); e == nil {
		t.Fatal("snapshot escape")
	}
	empty, e := Open(path, "source", 0)
	if e != nil {
		t.Fatal(e)
	}
	defer empty.Close()
	v, e = empty.Call("trace_list_runs", Args{})
	if e != nil || v.(map[string]any)["total"] != 0 {
		t.Fatal(v, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unloaded, e := Open(path, "source", last.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer unloaded.Close()
	if _, e = unloaded.CallContext(ctx, "trace_find_steps", Args{}); e == nil {
		t.Fatal("canceled request still indexed")
	}
	huge, _ := s.Add("source", "in", "warning", map[string]string{"message": strings.Repeat("z", (8<<20)+1)})
	bounded, e := Open(path, "source", huge.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer bounded.Close()
	if _, e = bounded.Call("trace_find_steps", Args{}); !errors.Is(e, ErrProjectionLimit) {
		t.Fatal("parse bound", e)
	}
	if _, e = bounded.Call("trace_read_event", Args{EventID: huge.ID}); !errors.Is(e, ErrProjectionLimit) {
		t.Fatal("raw bound", e)
	}
}
