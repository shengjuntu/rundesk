package tracequery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func timingAt(ms int64, row map[string]any) map[string]any {
	row["at"] = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC).Add(time.Duration(ms) * time.Millisecond).Format(time.RFC3339Nano)
	return row
}

func timingReader(p *projection) *Reader {
	return &Reader{SessionID: "s", Through: 999, loaded: true, runs: p.runs, steps: p.steps}
}

func timingStats(t *testing.T, r *Reader, args Args) TimingSummary {
	t.Helper()
	v, err := r.Call("trace_statistics", args)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)["timing"].(TimingSummary)
}

func TestTimingOverlapWorkAndWallAreSeparate(t *testing.T) {
	p := project(t,
		timingAt(0, input("r")),
		timingAt(1000, item("started", "t", "command", nil)),
		timingAt(2000, item("started", "t", "mcp", map[string]any{"type": "mcpToolCall", "server": "fixture", "tool": "read"})),
		timingAt(9000, item("completed", "t", "command", map[string]any{"exitCode": 0})),
		timingAt(10000, item("completed", "t", "mcp", map[string]any{"type": "mcpToolCall", "server": "fixture", "tool": "read"})),
		timingAt(10000, item("completed", "t", "missing-start", nil)),
		timingAt(10000, item("started", "t", "missing-end", nil)),
		timingAt(10000, record("warning", map[string]any{"runId": "r", "message": "point"})),
		timingAt(10000, record("run/state", map[string]any{"runId": "r", "status": "completed"})))
	r := timingReader(p)
	summary := timingStats(t, r, Args{RunID: "r"})
	if len(summary.Runs) != 1 || !summary.Runs[0].Complete || summary.Runs[0].DurationMS == nil || *summary.Runs[0].DurationMS != 10000 || summary.Runs[0].Clock != "host_journal" || summary.Runs[0].StartMethod != "run/input" {
		t.Fatalf("unexpected wall interval: %+v", summary.Runs)
	}
	if summary.KnownWorkDurationMS != 16000 || summary.CompleteDurationSteps != 2 || summary.WithoutCompleteDuration != 2 || summary.PointSteps != 2 {
		t.Fatalf("parallel work was treated as wall time or missing clocks as zero: %+v", summary)
	}
	if len(summary.SlowestSteps) != 2 || summary.SlowestSteps[0].DurationMS != 8000 || summary.SlowestSteps[0].ID != p.steps[1].ID || summary.SlowestSteps[1].ID != p.steps[2].ID || summary.SlowestSteps[0].Clock != "host journal receive time" {
		t.Fatalf("slow step identity/tie order lost: %+v", summary.SlowestSteps)
	}
	byType := map[string]TypeTiming{}
	for _, row := range summary.ByType {
		byType[row.Type] = row
	}
	if row := byType["commandExecution"]; row.StepCount != 3 || row.CompleteDurationSteps != 1 || row.WithoutCompleteDuration != 2 || row.WorkDurationMS != 8000 {
		t.Fatal(row)
	}
	// Step filters affect work, but never truncate the selected run's boundaries.
	filtered := timingStats(t, r, Args{RunID: "r", Type: "mcpToolCall"})
	if filtered.KnownWorkDurationMS != 8000 || filtered.CompleteDurationSteps != 1 || len(filtered.ByType) != 1 || filtered.Runs[0].DurationMS == nil || *filtered.Runs[0].DurationMS != 10000 {
		t.Fatal(filtered)
	}
	for _, slow := range summary.SlowestSteps {
		v, err := r.Call("trace_get_step", Args{RunID: "r", StepID: slow.ID})
		if err != nil || v.(Step).DurationMS == nil || *v.(Step).DurationMS != slow.DurationMS {
			t.Fatal("slowest reference does not resolve exactly", slow, v, err)
		}
	}
}

func TestRunTimingClockEvidenceAndOpenSnapshot(t *testing.T) {
	cases := []struct {
		name, issue string
		rows        []map[string]any
		clock       string
		duration    *int64
		elapsed     *int64
	}{
		{name: "open", issue: "missing_end", rows: []map[string]any{timingAt(0, input("r")), timingAt(4000, record("warning", map[string]any{"runId": "r", "message": "last retained"}))}, clock: "host_journal", elapsed: timingMS(4000)},
		{name: "missing_start", issue: "missing_start", rows: []map[string]any{timingAt(5000, record("run/state", map[string]any{"runId": "r", "status": "completed"}))}, clock: "host_journal"},
		{name: "reverse_clock", issue: "invalid_clock", rows: []map[string]any{timingAt(5000, input("r")), timingAt(1000, record("run/state", map[string]any{"runId": "r", "status": "completed"}))}, clock: "host_journal"},
		{name: "invalid_clock", issue: "invalid_clock", rows: []map[string]any{{"method": "run/input", "at": "not-a-time", "data": map[string]any{"runId": "r"}}, timingAt(5000, record("run/state", map[string]any{"runId": "r", "status": "completed"}))}, clock: "host_journal"},
		{name: "duplicate_start", issue: "ambiguous_run_start", rows: []map[string]any{timingAt(0, input("r")), timingAt(1000, input("r")), timingAt(5000, record("run/state", map[string]any{"runId": "r", "status": "completed"}))}, clock: "host_journal"},
		{name: "worker_clock", rows: []map[string]any{timingAt(0, input("r")), timingAt(100000, worker("run.started", "r", 1, nil)), timingAt(110000, worker("run.finished", "r", 2, map[string]any{"status": "completed"}))}, clock: "kun_worker", duration: timingMS(10000)},
		{name: "host_interval_preferred", rows: []map[string]any{timingAt(0, input("r")), timingAt(100000, worker("run.started", "r", 1, nil)), timingAt(110000, worker("run.finished", "r", 2, map[string]any{"status": "completed"})), timingAt(30000, record("run/state", map[string]any{"runId": "r", "status": "completed"}))}, clock: "host_journal", duration: timingMS(30000)},
		{name: "never_mix_worker_end_with_host_start", issue: "missing_start", rows: []map[string]any{timingAt(0, input("r")), timingAt(110000, worker("run.finished", "r", 2, map[string]any{"status": "completed"}))}, clock: "kun_worker"},
		{name: "native_interval_basis", rows: []map[string]any{timingAt(2000, record("turn/started", map[string]any{"runId": "r", "params": map[string]any{"turn": map[string]any{"id": "t"}}})), timingAt(8000, record("turn/completed", map[string]any{"runId": "r", "params": map[string]any{"turn": map[string]any{"id": "t", "status": "completed"}}}))}, clock: "host_journal", duration: timingMS(6000)},
		{name: "first_native_terminal_is_consistent", rows: []map[string]any{timingAt(0, input("r")), timingAt(2000, record("turn/started", map[string]any{"params": map[string]any{"turn": map[string]any{"id": "t"}}})), timingAt(8000, record("turn/completed", map[string]any{"params": map[string]any{"turn": map[string]any{"id": "t", "status": "completed"}}})), timingAt(10000, record("run/state", map[string]any{"runId": "r", "status": "completed"}))}, clock: "host_journal", duration: timingMS(8000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := project(t, tc.rows...)
			got := timingStats(t, timingReader(p), Args{RunID: "r"}).Runs[0]
			if got.Clock != tc.clock || !sameTimingMS(got.DurationMS, tc.duration) || !sameTimingMS(got.ObservedElapsedMS, tc.elapsed) || got.Complete != (tc.duration != nil) {
				t.Fatalf("unexpected timing: %+v", got)
			}
			if tc.issue != "" && !strings.Contains(strings.Join(got.Issues, ","), tc.issue) {
				t.Fatal("uncertainty hidden", got)
			}
			if tc.name == "native_interval_basis" && (got.StartMethod != "turn/started" || got.EndMethod != "turn/completed") {
				t.Fatal("native interval mislabeled as full admission", got)
			}
		})
	}
}

func timingMS(v int64) *int64 { return &v }
func sameTimingMS(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func TestTimingSortAllMatchesBeforePagination(t *testing.T) {
	r := &Reader{SessionID: "s", Through: 99, loaded: true, runs: []Run{{ID: "r"}, {ID: "other"}}, steps: []Step{}}
	for n := 0; n < 65; n++ {
		r.steps = append(r.steps, Step{ID: fmt.Sprint("step-", n), RunID: "r", Type: "commandExecution", Status: "failed", DurationMS: timingMS(int64(n)), Issues: []string{"failed"}})
	}
	r.steps = append(r.steps, Step{ID: "unknown-1", RunID: "r", Type: "commandExecution", Issues: []string{"missing_start"}}, Step{ID: "unknown-2", RunID: "r", Type: "commandExecution", Issues: []string{"missing_start"}}, Step{ID: "equal-64", RunID: "r", Type: "commandExecution", Status: "failed", DurationMS: timingMS(64), Issues: []string{"failed"}}, Step{ID: "foreign-slower", RunID: "other", Type: "commandExecution", DurationMS: timingMS(9999)})
	for _, name := range []string{"trace_find_steps", "trace_find_issues"} {
		v, err := r.Call(name, Args{RunID: "r", Sort: "duration_desc", Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		page := v.(map[string]any)
		steps := page["steps"].([]Step)
		if page["total"] != 68 || page["nextOffset"] != 2 || page["hasMore"] != true || steps[0].ID != "step-64" || steps[1].ID != "equal-64" {
			t.Fatal("sort happened after paging or escaped run scope", page)
		}
		for _, order := range []string{"duration_asc", "duration_desc"} {
			v, err = r.Call(name, Args{RunID: "r", Sort: order, Offset: 66, Limit: 2})
			if err != nil {
				t.Fatal(err)
			}
			steps = v.(map[string]any)["steps"].([]Step)
			if steps[0].ID != "unknown-1" || steps[1].ID != "unknown-2" {
				t.Fatal("unknown duration must sort last in stable order", steps)
			}
		}
	}
	v, err := r.Call("trace_find_steps", Args{RunID: "r", Sort: "recorded", Limit: 1})
	if err != nil || v.(map[string]any)["steps"].([]Step)[0].ID != "step-0" || r.steps[0].ID != "step-0" {
		t.Fatal("sorting mutated frozen projection", v, err)
	}
	summary := timingStats(t, r, Args{RunID: "r"})
	if len(summary.SlowestSteps) != 10 || summary.SlowestSteps[0].ID != "step-64" || summary.SlowestSteps[1].ID != "equal-64" || summary.SlowestSteps[9].ID != "step-56" {
		t.Fatal("slowest list was paginated before ranking", summary.SlowestSteps)
	}
	for _, invalid := range []struct{ name, sort string }{{"trace_find_steps", "duration"}, {"trace_statistics", "duration_desc"}, {"trace_list_activity", "recorded"}} {
		if _, err := r.Call(invalid.name, Args{Sort: invalid.sort}); err == nil {
			t.Fatal("accepted invalid sort", invalid)
		}
	}
}

func TestTimingSnapshotAliasesAreLateScopedAndAuthoritative(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r"})
	// No start exists: completion cannot use current-run inference. Only the
	// later validated native acknowledgement can establish the exact mapping.
	done := unifiedAdd(t, j, "item/completed", "in", map[string]any{"params": map[string]any{"turnId": "t", "item": map[string]any{"id": "a", "type": "commandExecution", "exitCode": 0}}})
	before := unifiedRead(t, path, done.ID)
	alias := unifiedAdd(t, j, "run/turn", "internal", map[string]any{"runId": "r", "turnId": "t", "threadId": "thread"})
	unifiedAdd(t, j, "llm/exchange", "internal", map[string]any{"labels": map[string]any{"runId": "r"}, "durationMs": 999999})
	for _, tc := range []struct {
		reader *Reader
		run    string
		assoc  string
	}{{before, "observed-turn-t", "missing"}, {unifiedRead(t, path, alias.ID+1), "r", "reconstructed"}} {
		v, err := tc.reader.Call("trace_find_steps", Args{Type: "commandExecution"})
		if err != nil {
			t.Fatal(err)
		}
		steps := v.(map[string]any)["steps"].([]Step)
		if len(steps) != 1 || steps[0].RunID != tc.run || steps[0].Association != tc.assoc || steps[0].DurationMS != nil {
			t.Fatal("late alias guessed boundaries or snapshot escaped", steps)
		}
		summary := timingStats(t, tc.reader, Args{})
		if summary.KnownWorkDurationMS != 0 || summary.CompleteDurationSteps != 0 || summary.WithoutCompleteDuration != 1 || len(summary.SlowestSteps) != 0 {
			t.Fatal("mapping or transport double-counted as runtime work", summary)
		}
	}
	// Unified views use the same authoritative alias preindex.
	page := unifiedEventsPage(t, unifiedRead(t, path, alias.ID), UnifiedArgs{RunID: "r"})
	found := false
	for _, e := range page.Observations {
		if e.EventID == done.ID {
			found = e.RunID == "r" && e.Association == "reconstructed"
		}
	}
	if !found {
		t.Fatal("unified and timing projections disagree on exact late evidence", page)
	}
	// An incoming native notification is never an authoritative host proof.
	spoof := unifiedAdd(t, j, "run/turn", "in", map[string]any{"runId": "attacker", "turnId": "t"})
	reader := unifiedRead(t, path, spoof.ID)
	steps, err := reader.Call("trace_find_steps", Args{RunID: "r", Type: "commandExecution"})
	if err != nil || len(steps.(map[string]any)["steps"].([]Step)) != 1 {
		t.Fatal("untrusted incoming alias altered host mapping", steps, err)
	}
	// A conflicting authoritative proof invalidates both associations.
	conflict := unifiedAdd(t, j, "run/turn", "internal", map[string]any{"runId": "other", "turnId": "t"})
	v, err := unifiedRead(t, path, conflict.ID).Call("trace_find_steps", Args{Type: "commandExecution"})
	if err != nil || v.(map[string]any)["steps"].([]Step)[0].Association != "missing" {
		t.Fatal("conflicting proof silently overwritten", v, err)
	}
}

func TestTimingAliasesRejectMultipleTurnsAndForeignSessions(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r"})
	unifiedAdd(t, j, "item/completed", "in", map[string]any{"params": map[string]any{"turnId": "t", "item": map[string]any{"id": "a", "type": "commandExecution"}}})
	foreign, err := j.Add("foreign", "internal", "run/turn", map[string]any{"runId": "r", "turnId": "t"})
	if err != nil {
		t.Fatal(err)
	}
	reader := unifiedRead(t, path, foreign.ID)
	v, err := reader.Call("trace_find_steps", Args{Type: "commandExecution"})
	if err != nil || v.(map[string]any)["steps"].([]Step)[0].Association != "missing" {
		t.Fatal("foreign-session proof used", v, err)
	}
	unifiedAdd(t, j, "run/turn", "internal", map[string]any{"runId": "r", "turnId": "t"})
	last := unifiedAdd(t, j, "run/turn", "internal", map[string]any{"runId": "r", "turnId": "second"})
	v, err = unifiedRead(t, path, last.ID).Call("trace_find_steps", Args{Type: "commandExecution"})
	if err != nil || v.(map[string]any)["steps"].([]Step)[0].Association != "missing" {
		t.Fatal("multiple native turns were arbitrarily assigned to one host run", v, err)
	}
}

func TestTimingStrictToolsAndEmptyArrays(t *testing.T) {
	for _, raw := range []string{`{"sort":"duration_desc","runId":"r"}`, `{"sort":"duration_asc","offset":50}`, `{"sort":"recorded"}`} {
		if _, err := ParseArgs("trace_find_steps", json.RawMessage(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{{"trace_find_steps", `{"sort":"other"}`}, {"trace_statistics", `{"sort":"duration_desc"}`}, {"trace_find_steps", `{"sort":null}`}, {"trace_find_steps", `{"sort":true}`}} {
		if _, err := ParseArgs(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Fatal("accepted invalid sort", tc)
		}
	}
	r := timingReader(newProjection())
	got := timingStats(t, r, Args{})
	b, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(b), `"runs":[]`) || !strings.Contains(string(b), `"byType":[]`) || !strings.Contains(string(b), `"slowestSteps":[]`) {
		t.Fatal("empty summary arrays became null", string(b), err)
	}
	// Cancellation remains observable even when aliases are the first DB query.
	j, path := unifiedFixture(t)
	last := unifiedAdd(t, j, "run/turn", "internal", map[string]any{"runId": "r", "turnId": "t"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := unifiedRead(t, path, last.ID).CallContext(ctx, "trace_statistics", Args{}); err == nil {
		t.Fatal("cancelled projection query succeeded")
	}
}

func TestTimingIntervalRejectsDurationOverflowAndPreservesZero(t *testing.T) {
	if ms, ok := intervalMillis("0001-01-01T00:00:00Z", "9999-01-01T00:00:00Z"); ok || ms != nil {
		t.Fatal("overflow was reported as a valid saturated duration", ms)
	}
	if ms, ok := intervalMillis("2026-10-05T00:00:00Z", "2026-10-05T08:00:00+08:00"); !ok || ms == nil || *ms != 0 {
		t.Fatal("valid same-instant clocks became unknown", ms)
	}
}
