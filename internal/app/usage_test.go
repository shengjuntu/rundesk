package app

import (
	"context"
	"encoding/json"

	"testing"
	"time"
)

func TestUsageCumulativeMissingAndReset(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, e := m.CreateSession(w.ID, "usage", "", DefaultInstance)
	if e != nil {
		t.Fatal(e)
	}
	add := func(dir, method string, v any) {
		t.Helper()
		if _, e := m.Store.Add(s.ID, dir, method, v); e != nil {
			t.Fatal(e)
		}
	}
	run := func(id, turn string) {
		add("internal", "run/input", map[string]any{"runId": id})
		add("in", "turn/started", map[string]any{"params": map[string]any{"turn": map[string]any{"id": turn}}})
	}
	token := func(turn string, total any) {
		add("in", "thread/tokenUsage/updated", map[string]any{"params": map[string]any{"threadId": "thread-1", "turnId": turn, "tokenUsage": map[string]any{"total": total, "last": map[string]any{"totalTokens": 99999}}}})
	}
	run("r1", "t1")
	add("out", "thread/start", map[string]any{})
	token("t1", map[string]any{"inputTokens": 70, "outputTokens": 30, "totalTokens": 100})
	token("t1", map[string]any{"inputTokens": 70, "outputTokens": 30, "totalTokens": 100})
	add("internal", "run/state", map[string]any{"runId": "r1", "status": "completed"})
	run("r2", "t2")
	token("t2", map[string]any{"inputTokens": 120, "outputTokens": 80, "totalTokens": 200})
	token("t2", map[string]any{"totalTokens": 20})
	add("internal", "run/state", map[string]any{"runId": "r2", "status": "failed"})
	run("r3", "t3")
	report, e := m.Usage(context.Background(), usageWindow{from: time.Now().Add(-time.Hour), to: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Rows) != 1 {
		t.Fatal(report)
	}
	r := report.Rows[0]
	if *r.Tokens["totalTokens"] != 200 || r.Runs != 3 || r.ReportedRuns != 2 || r.UnknownRuns != 1 || r.Tokens["cachedInputTokens"] != nil || r.Issues["counter_regression"] != 1 || r.Statuses["failed"] != 1 {
		t.Fatalf("%+v", r)
	}
	// Read-only report: no sessions or events created; repeated reports are stable.
	again, e := m.Usage(context.Background(), usageWindow{from: time.Now().Add(-time.Hour), to: time.Now().Add(time.Hour)})
	if e != nil || *again.Rows[0].Tokens["totalTokens"] != 200 {
		t.Fatal(again, e)
	}
}
func TestUsageUnknownBaselineWindowAndDedup(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "resumed", "", DefaultInstance)
	add := func(method string, v any) {
		if _, e := m.Store.Add(s.ID, "in", method, v); e != nil {
			t.Fatal(e)
		}
	}
	token := func(n int) {
		add("thread/tokenUsage/updated", map[string]any{"params": map[string]any{"threadId": "shared", "tokenUsage": map[string]any{"total": map[string]any{"totalTokens": n}}}})
	}
	token(1000)
	from := time.Now()
	token(1200)
	r, e := m.Usage(context.Background(), usageWindow{from: from, to: time.Now().Add(time.Hour)})
	if e != nil || len(r.Rows) != 1 || *r.Rows[0].Tokens["totalTokens"] != 200 {
		t.Fatal(r, e)
	}
	// Another retained session observes the same native thread: the snapshot is not added again.
	other, _ := m.CreateSession(w.ID, "alias", "", DefaultInstance)
	s = other
	token(1200)
	r, e = m.Usage(context.Background(), usageWindow{from: from.Add(-time.Hour), to: time.Now().Add(time.Hour)})
	if e != nil || *r.Rows[0].Tokens["totalTokens"] != 200 || r.Rows[0].Issues["unknown_baseline"] != 1 {
		t.Fatal(r, e)
	}
	// No source rows means no invented zero usage.
	empty, e := m.Usage(context.Background(), usageWindow{from: time.Now().Add(time.Hour), to: time.Now().Add(2 * time.Hour)})
	if e != nil || len(empty.Rows) != 0 {
		t.Fatal(empty, e)
	}
}
func TestUsageUnknownIsNullAndAuth(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "unknown", "", DefaultInstance)
	m.Store.Add(s.ID, "internal", "run/input", map[string]any{"runId": "unknown"})
	h := NewHandler(m, "", true)
	r := v1Request(h, "GET", "/usage", "", "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var result UsageReport
	json.Unmarshal(r.Body.Bytes(), &result)
	if result.Rows[0].Tokens["totalTokens"] != nil || result.Rows[0].UnknownRuns != 1 {
		t.Fatal(result)
	}
	if v1Request(h, "GET", "/usage?from=bad", "", "").Code != 400 {
		t.Fatal("bad range accepted")
	}
	_, _, secret := setupKey(t, m, "usage-worker", w.ID, "read", "run")
	h = NewHandler(m, "admin-secret-01234567890123456789", true)
	if appRequest(h, "GET", "/api/v1/usage", "", secret, "").Code != 403 {
		t.Fatal("app usage leak")
	}
	if v1Request(h, "GET", "/usage", "", "").Code != 401 {
		t.Fatal("anonymous usage leak")
	}
}

func TestUsageFieldBaselineFiltersAndDeletion(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "fields", "", DefaultInstance)
	m.Store.Add(s.ID, "out", "thread/start", map[string]any{})
	token := func(total map[string]any) {
		m.Store.Add(s.ID, "in", "thread/tokenUsage/updated", map[string]any{"params": map[string]any{"threadId": "fields", "tokenUsage": map[string]any{"total": total}}})
	}
	token(map[string]any{"totalTokens": 100})
	from := time.Now()
	token(map[string]any{"totalTokens": 150, "cachedInputTokens": 90})
	token(map[string]any{"totalTokens": 200, "cachedInputTokens": 100})
	f := usageWindow{from: from, to: time.Now().Add(time.Hour)}
	r, e := m.Usage(context.Background(), f)
	if e != nil {
		t.Fatal(e)
	}
	if *r.Rows[0].Tokens["totalTokens"] != 100 || *r.Rows[0].Tokens["cachedInputTokens"] != 10 || r.Rows[0].Issues["missing_field_baseline"] != 1 {
		t.Fatal(r.Rows[0])
	}
	f.work = "not-this-project"
	r, e = m.Usage(context.Background(), f)
	if e != nil || len(r.Rows) != 0 {
		t.Fatal(r, e)
	}
	if e = m.Store.DeleteSession(s.ID); e != nil {
		t.Fatal(e)
	}
	f.work = ""
	r, e = m.Usage(context.Background(), f)
	if e != nil || len(r.Rows) != 0 {
		t.Fatal("deleted usage still present", r, e)
	}
}
