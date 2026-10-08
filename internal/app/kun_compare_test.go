package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

func comparisonDraft(t *testing.T, m *Manager, id, mode string, state p.State) kunForkDraft {
	t.Helper()
	selection := p.ForkSelection{SourceRunID: state.RunID, Sequence: 10, Through: 14, ExpectedRevision: 20, WorkerEpoch: "fixture"}
	b := p.ForkBundle{Schema: p.ForkSchema, Mode: mode, Selection: selection, State: state, EnvironmentHash: "environment", CatalogHash: "catalog"}
	b.ContentHash = p.ForkHash(b)
	o := p.ForkOrigin{PreviewID: id, SessionID: state.SessionID, RunID: state.RunID, Sequence: 10, Through: 14, BundleHash: b.ContentHash, Mode: mode}
	d := kunForkDraft{Preview: KunForkPreview{ID: id, TargetSessionID: id + "-session", Origin: o}, Target: Session{ID: id + "-session", KunFork: &o}, Bundle: b}
	d.Preview.Hash = forkDraftHash(d)
	if err := m.Store.PutMany(store.Record{Kind: "kun_fork_bundle", ID: id, Value: d}, store.Record{Kind: "kun_fork_preview", ID: id, Value: d.Preview}); err != nil {
		t.Fatal(err)
	}
	return d
}
func comparisonEvent(t *testing.T, m *Manager, sid, run string, seq int64, kind string, data any) store.Event {
	t.Helper()
	e, err := m.Store.Add(sid, "in", "kun/"+kind, p.Event{SessionID: sid, RunID: run, Sequence: seq, Revision: seq, Type: "kun/" + kind, Data: p.JSON(data)})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestKunComparisonOfflinePinsAndSameBaseline(t *testing.T) {
	m := testManager(t)
	m.Kun = "nonexistent-worker"
	m.Codex = "nonexistent-codex"
	base := p.State{SessionID: "source", RunID: "source-run", Step: 2, Harness: p.Harness{ID: "tool-loop-v1", Revision: 1}, Config: p.Config{Model: "fixture", SystemPrompt: "PRIVATE SYSTEM PROMPT"}, Budget: p.BudgetUsage{ReportedTokens: 99, ActiveMillis: 500, WaitMillis: 300}, Messages: []p.Message{{Role: "user", Content: "PRIVATE HISTORY"}}}
	d := comparisonDraft(t, m, "hybrid", "hybrid", base)
	comparisonEvent(t, m, "source", "source-run", 11, "model.started", map[string]any{"step": 3, "request": map[string]any{"secret": "PRIVATE REQUEST"}})
	comparisonEvent(t, m, "source", "source-run", 12, "model.completed", map[string]any{"step": 3, "usage": map[string]int{"total_tokens": 7}, "message": p.Message{Content: "source answer"}})
	comparisonEvent(t, m, "source", "source-run", 13, "control.applied", map[string]any{})
	comparisonEvent(t, m, "source", "source-run", 14, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{ReportedTokens: 106, ActiveMillis: 650, WaitMillis: 310}})
	h := NewHandler(m, userTestAdmin, true)
	get := func(query string) KunForkComparison {
		t.Helper()
		r := appRequest(h, "GET", "/api/v1/kun-forks/comparisons/hybrid"+query, "", userTestAdmin, "")
		var out KunForkComparison
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &out) != nil {
			t.Fatal(r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), "PRIVATE") || strings.Contains(r.Body.String(), "messages") {
			t.Fatal("private baseline leaked")
		}
		return out
	}
	empty := get("")
	if !empty.Left.Complete || empty.Right.Complete || empty.Right.RunID != "" || empty.Right.Through != 0 || empty.Delta.ModelCalls != nil || empty.Left.ReportedTokens != 7 || *empty.Left.ActiveMillis != 150 {
		t.Fatal(empty)
	}
	comparisonEvent(t, m, d.Target.ID, "branch-run", 1, "run.started", map[string]any{"fork": p.ForkState{Origin: d.Preview.Origin, InheritedStep: base.Step, InheritedBudget: base.Budget}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 2, "model.started", map[string]any{"step": 3})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 3, "model.completed", map[string]any{"step": 3, "usage": map[string]int{"total_tokens": 12}, "message": p.Message{Content: strings.Repeat("🙂", 2050)}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 4, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{ReportedTokens: 111, ActiveMillis: 700, WaitMillis: 320}})
	pinned := get(fmt.Sprintf("?leftThrough=%d&rightThrough=0", empty.Left.Through))
	if forkDigest(empty) != forkDigest(pinned) {
		t.Fatal("frozen empty range changed", empty, pinned)
	}
	full := get("")
	if !full.Right.Complete || full.Delta.ReportedTokens == nil || *full.Delta.ReportedTokens != 5 || *full.Delta.ActiveMillis != 50 || !full.Right.LastReply.Truncated || len([]rune(full.Right.LastReply.Text)) != 2048 {
		t.Fatal(full)
	}
	comparisonEvent(t, m, "source", "new-run", 15, "model.completed", map[string]any{"message": p.Message{Content: "later source run"}})
	repeat := get(fmt.Sprintf("?leftThrough=%d&rightThrough=%d", full.Left.Through, full.Right.Through))
	if forkDigest(full) != forkDigest(repeat) {
		t.Fatal("later source run changed fixed comparison")
	}
	live := comparisonDraft(t, m, "live", "live", base)
	paired, err := m.CompareKunFork(context.Background(), d.Preview.ID, live.Preview.ID, nil, nil)
	if err != nil || paired.Left.Mode != "live" || paired.Left.PreviewID != live.Preview.ID || paired.Benchmark {
		t.Fatal(paired, err)
	}
	changed := base
	changed.Config.Model = "different"
	bad := comparisonDraft(t, m, "other", "hybrid", changed)
	if _, err = m.CompareKunFork(context.Background(), d.Preview.ID, bad.Preview.ID, nil, nil); err == nil {
		t.Fatal("different baseline accepted")
	}
	for _, query := range []string{"?rightThrough=-1", "?leftThrough=", "?leftThrough=1&leftThrough=2", "?unknown=1", "?against=hybrid"} {
		r := appRequest(h, "GET", "/api/kun-forks/comparisons/hybrid"+query, "", userTestAdmin, "")
		if r.Code != 400 {
			t.Fatal(query, r.Code, r.Body.String())
		}
	}
	if len(m.handles) != 0 {
		t.Fatal("comparison started a handle")
	}
	if err = m.Store.DeleteSession("source"); err != nil {
		t.Fatal(err)
	}
	missing := get("")
	if missing.Left.Complete || missing.Delta.ModelCalls != nil || !missing.Right.Complete {
		t.Fatal("deleted source falsely treated as zero", missing)
	}
}

func TestKunComparisonMissingUsageGapsAndPlanning(t *testing.T) {
	base := p.State{Step: 4, Budget: p.BudgetUsage{ActiveMillis: 100, WaitMillis: 50}}
	side := KunCompareSide{SessionID: "s", RunID: "r", Mode: "source", AfterSequence: 10}
	events := []store.Event{}
	add := func(seq int64, kind string, data any) {
		events = append(events, store.Event{ID: seq + 100, Method: "kun/" + kind, Data: p.JSON(p.Event{SessionID: "s", RunID: "r", Sequence: seq, Type: "kun/" + kind, Data: p.JSON(data)})})
	}
	add(11, "model.started", map[string]any{"step": 5, "purpose": "plan"})
	add(12, "model.completed", map[string]any{"step": 5, "purpose": "plan", "message": p.Message{Content: "not a reply"}})
	add(13, "run.finished", map[string]any{"status": "failed", "budget": map[string]any{}})
	out, err := reduceKunComparison(side, events, base, 13, nil)
	if err != nil || !out.Complete || out.TokenUsageComplete || out.UsageMissing != 1 || out.PlanningCalls != 1 || out.LastReply != nil || out.ActiveMillis != nil || out.WaitMillis != nil {
		t.Fatal(out, err)
	}
	out, err = reduceKunComparison(side, events[1:], base, 13, nil)
	if err != nil || out.Complete || out.TokenUsageComplete {
		t.Fatal("missing request treated as complete", out, err)
	}
	bad := append([]store.Event{}, events...)
	bad[1].Data = p.JSON(p.Event{SessionID: "other", RunID: "r", Sequence: 12, Type: bad[1].Method})
	if _, err = reduceKunComparison(side, bad, base, 13, nil); err == nil {
		t.Fatal("wrong session accepted")
	}
	events = nil
	add(11, "run.paused", map[string]any{})
	add(12, "control.applied", map[string]any{"command": p.Control{Operation: "resume"}})
	out, err = reduceKunComparison(side, events, base, 13, nil)
	if err != nil || out.Status != "in_progress" || out.Complete {
		t.Fatal("resume still displayed as paused", out, err)
	}
}

func TestKunComparisonToolExecutionAndReplay(t *testing.T) {
	base := p.State{}
	side := KunCompareSide{SessionID: "s", RunID: "r", Mode: "source"}
	call := func(id string) p.ToolCall { return p.ToolCall{ID: id, Function: p.Function{Name: "write_file"}} }
	data := []struct {
		kind    string
		payload any
	}{
		{"tool.started", map[string]any{"call": call("real")}},
		{"tool.completed", map[string]any{"call": call("real"), "status": "failed"}},
		{"tool.completed", map[string]any{"call": call("replayed"), "status": "replayed", "replay": p.ReplayEvidence{Mode: "recorded", RecordedStatus: "succeeded"}}},
		{"tool.completed", map[string]any{"call": call("denied"), "status": "declined"}},
		{"tool.started", map[string]any{"call": call("unknown")}},
		{"run.finished", map[string]any{"status": "interrupted"}},
	}
	events := []store.Event{}
	for i, v := range data {
		events = append(events, store.Event{ID: int64(i + 1), Method: "kun/" + v.kind, Data: p.JSON(p.Event{SessionID: "s", RunID: "r", Sequence: int64(i + 1), Type: "kun/" + v.kind, Data: p.JSON(v.payload)})})
	}
	out, err := reduceKunComparison(side, events, base, 6, nil)
	if err != nil || len(out.Tools) != 1 || out.Tools[0].Dispatched != 2 || out.Tools[0].Replayed != 1 || out.Tools[0].Failed != 1 || out.Tools[0].Declined != 1 || out.Tools[0].Unsettled != 1 {
		t.Fatal(out, err)
	}
}
