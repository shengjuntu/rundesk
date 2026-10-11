package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestKunEvaluationPinnedFullReplyAndPersistence(t *testing.T) {
	m := testManager(t)
	m.Kun = "not-a-worker"
	m.Codex = "not-codex"
	d := comparisonDraft(t, m, "branch", "hybrid", p.State{SessionID: "source", RunID: "original", Config: p.Config{Model: "fixture"}})
	comparisonEvent(t, m, "source", "original", 11, "model.started", map[string]any{"step": 1})
	text := strings.Repeat("界", 2050) + " match-tail PRIVATE-END"
	comparisonEvent(t, m, "source", "original", 12, "model.completed", map[string]any{"step": 1, "message": p.Message{Role: "assistant", Content: text}})
	comparisonEvent(t, m, "source", "original", 13, "control.applied", map[string]any{})
	comparisonEvent(t, m, "source", "original", 14, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 1, "run.started", map[string]any{"fork": p.ForkState{Origin: d.Preview.Origin}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 2, "model.started", map[string]any{"step": 1})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 3, "model.completed", map[string]any{"step": 1, "message": p.Message{Role: "assistant", Content: "different answer"}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 4, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{}})
	h := NewHandler(m, userTestAdmin, true)
	request := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		return appRequest(h, method, "/api/v1"+path, string(p.JSON(body)), userTestAdmin, key)
	}
	rin := KunRubricInput{PreviewID: d.Preview.ID, ExpectedPreviewHash: d.Preview.Hash, Title: "fixed rules", Rules: []KunReplyRule{{Kind: "reply_contains", Value: "match-tail"}, {Kind: "reply_not_contains", Value: "forbidden"}}}
	r := request("POST", "/kun-rubrics", rin, "rubric-once")
	var rubric KunRubric
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rubric) != nil {
		t.Fatal(r.Code, r.Body.String())
	}
	if rubric.RuleCount != 3 || rubric.Hash == "" || rubric.BaselineHash != kunBaselineHash(d) {
		t.Fatal(rubric)
	}
	var repeated KunRubric
	r = request("POST", "/kun-rubrics", rin, "rubric-once")
	json.Unmarshal(r.Body.Bytes(), &repeated)
	if r.Code != 200 || forkDigest(repeated) != forkDigest(rubric) {
		t.Fatal("rubric idempotency", r.Code, repeated)
	}
	comparison, err := m.CompareKunFork(context.Background(), d.Preview.ID, "", nil, nil)
	if err != nil || !comparison.Left.LastReply.Truncated {
		t.Fatal(comparison, err)
	}
	in := KunEvaluationInput{RubricID: rubric.ID, ExpectedRubricHash: rubric.Hash, PreviewID: d.Preview.ID, LeftThrough: &comparison.Left.Through, RightThrough: &comparison.Right.Through, ExpectedComparisonHash: comparison.Hash}
	r = request("POST", "/kun-evaluations", in, "grade-once")
	var report KunEvaluation
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &report) != nil {
		t.Fatal(r.Code, r.Body.String())
	}
	if report.IndependentValidation || report.Comparison.Benchmark || report.Left.Status != "pass" || report.Right.Status != "fail" || *report.Left.Score != 100 || report.Right.Passed != 2 || report.Right.Failed != 1 || report.ScoreDelta == nil || *report.ScoreDelta >= 0 || report.Left.Reply.Hash != forkDigest(text) {
		t.Fatal(report)
	}
	if strings.Contains(r.Body.String(), "PRIVATE-END") {
		t.Fatal("full reply leaked")
	}
	var again KunEvaluation
	r = request("POST", "/kun-evaluations", in, "grade-once")
	json.Unmarshal(r.Body.Bytes(), &again)
	if r.Code != 200 || forkDigest(again) != forkDigest(report) {
		t.Fatal("evaluation retry", r.Code)
	}
	// Later records cannot change a pinned range or saved report.
	comparisonEvent(t, m, "source", "next-run", 15, "model.completed", map[string]any{"message": p.Message{Content: "changed"}})
	other, err := m.CreateKunEvaluation(context.Background(), in)
	if err != nil || forkDigest(other.Left) != forkDigest(report.Left) || other.Comparison.Hash != report.Comparison.Hash {
		t.Fatal(other, err)
	}
	for _, path := range []string{"/kun-rubrics?previewId=branch", "/kun-evaluations?previewId=branch"} {
		rr := request("GET", path, nil, "")
		if rr.Code != 200 || strings.Contains(rr.Body.String(), "match-tail") || strings.Contains(rr.Body.String(), "different answer") || strings.Contains(rr.Body.String(), `"comparison"`) {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
	for _, change := range []func(*KunEvaluationInput){func(v *KunEvaluationInput) { v.LeftThrough = nil }, func(v *KunEvaluationInput) { v.RightThrough = nil }, func(v *KunEvaluationInput) { v.ExpectedComparisonHash = "wrong" }, func(v *KunEvaluationInput) { v.ExpectedRubricHash = "wrong" }} {
		bad := in
		change(&bad)
		if _, err = m.CreateKunEvaluation(context.Background(), bad); err == nil {
			t.Fatal("stale or unpinned evaluation accepted")
		}
	}
	foreign := comparisonDraft(t, m, "foreign", "hybrid", p.State{SessionID: "different", RunID: "original"})
	foreignComparison, _ := m.CompareKunFork(context.Background(), foreign.Preview.ID, "", nil, nil)
	bad := in
	bad.PreviewID = foreign.Preview.ID
	bad.LeftThrough = &foreignComparison.Left.Through
	bad.RightThrough = &foreignComparison.Right.Through
	bad.ExpectedComparisonHash = foreignComparison.Hash
	if _, err = m.CreateKunEvaluation(context.Background(), bad); err == nil {
		t.Fatal("different baseline accepted")
	}
	if err = m.Store.DeleteSession("source"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CreateKunEvaluation(context.Background(), in); err == nil {
		t.Fatal("deleted evidence silently changed fixed report")
	}
	read, err := m.KunEvaluation(report.ID)
	if err != nil || forkDigest(read) != forkDigest(report) {
		t.Fatal("saved report changed after source deletion", err)
	}
	if len(m.handles) != 0 {
		t.Fatal("scoring started runtime")
	}
	data := m.Data
	m.Close()
	m2, err := New(data, "missing-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	read, err = m2.KunEvaluation(report.ID)
	if err != nil || forkDigest(read) != forkDigest(report) {
		t.Fatal("saved report changed after restart", err)
	}
	read.Right.Status = "pass"
	m2.Store.Put("kun_evaluation", read.ID, read)
	if _, err = m2.KunEvaluation(read.ID); err == nil {
		t.Fatal("tampered report accepted")
	}
}

func scoreEvents(items ...struct {
	kind string
	data any
}) []store.Event {
	out := []store.Event{}
	for i, item := range items {
		seq := int64(i + 1)
		out = append(out, store.Event{ID: seq, Method: "kun/" + item.kind, Data: p.JSON(p.Event{Sequence: seq, Type: "kun/" + item.kind, Data: p.JSON(item.data)})})
	}
	return out
}
func TestKunEvaluationMissingSupersededAndLargeReply(t *testing.T) {
	rules := []KunReplyRule{{Kind: "reply_contains", Value: "answer"}, {Kind: "reply_not_contains", Value: "bad"}, {Kind: "reply_equals", Value: "answer"}}
	complete := KunCompareSide{Complete: true, Status: "completed", LastEventID: 9}
	message := func(text string) struct {
		kind string
		data any
	} {
		return struct {
			kind string
			data any
		}{"model.completed", map[string]any{"message": p.Message{Role: "assistant", Content: text}}}
	}
	events := scoreEvents(message("answer"))
	good, err := scoreKunSide(complete, events, rules)
	if err != nil || good.Status != "pass" || *good.Score != 100 {
		t.Fatal(good, err)
	}
	incomplete := complete
	incomplete.Complete = false
	unknown, _ := scoreKunSide(incomplete, events, rules)
	if unknown.Status != "unknown" || unknown.Score != nil || unknown.Unknown != 4 {
		t.Fatal(unknown)
	}
	for name, item := range map[string]struct {
		kind string
		data any
	}{
		"empty":     message(""),
		"pending":   {kind: "model.started", data: map[string]any{}},
		"tool-call": {kind: "model.completed", data: map[string]any{"message": p.Message{Content: "answer", ToolCalls: []p.ToolCall{{ID: "tool"}}}}},
		"rejected":  {kind: "model.completed", data: map[string]any{"status": "rejected", "message": p.Message{Content: "answer"}}},
	} {
		t.Run(name, func(t *testing.T) {
			v, _ := scoreKunSide(complete, scoreEvents(message("answer"), item), rules)
			if v.Reply.Status != "missing" || v.Status != "fail" || v.Failed != 3 {
				t.Fatal("used stale reply", v)
			}
		})
	}
	plan := scoreEvents(struct {
		kind string
		data any
	}{"model.completed", map[string]any{"purpose": "plan", "message": p.Message{Content: "answer"}}})
	missing, _ := scoreKunSide(complete, plan, rules)
	if missing.Reply.Status != "missing" || missing.Results[2].Status != "fail" {
		t.Fatal("missing reply passes negative rule", missing)
	}
	large, _ := scoreKunSide(complete, scoreEvents(message(strings.Repeat("界", maxKunEvaluationReply/3+1))), rules)
	if large.Score != nil || large.Unknown != 3 || large.Status != "unknown" || large.Reply.Status != "too_large" {
		t.Fatal(large)
	}
	failed := complete
	failed.Status = "failed"
	v, _ := scoreKunSide(failed, events, rules)
	if v.Status != "fail" || v.Results[0].Status != "fail" || v.Passed != 3 {
		t.Fatal("failed run passed gate", v)
	}
	missingStatus := complete
	missingStatus.Status = ""
	v, _ = scoreKunSide(missingStatus, events, rules)
	if v.Status != "unknown" || v.Score != nil || v.Results[0].Reason != "terminal_status_missing" {
		t.Fatal("missing status treated as failure or pass", v)
	}
	literal, _ := scoreKunSide(complete, events, []KunReplyRule{{Kind: "reply_contains", Value: ".*"}, {Kind: "reply_equals", Value: "Answer"}, {Kind: "reply_equals", Value: "answer\n"}})
	if literal.Failed != 3 {
		t.Fatal("non-literal matching", literal)
	}
}

func TestKunEvaluationZeroCursorStaysUnknown(t *testing.T) {
	m := testManager(t)
	d := comparisonDraft(t, m, "new-branch", "hybrid", p.State{SessionID: "source", RunID: "source-run"})
	rubric, err := m.CreateKunRubric(KunRubricInput{PreviewID: d.Preview.ID, ExpectedPreviewHash: d.Preview.Hash, Title: "before run", Rules: []KunReplyRule{{Kind: "reply_equals", Value: "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := m.CompareKunFork(context.Background(), d.Preview.ID, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := KunEvaluationInput{RubricID: rubric.ID, ExpectedRubricHash: rubric.Hash, PreviewID: d.Preview.ID, ExpectedComparisonHash: comparison.Hash, LeftThrough: &comparison.Left.Through, RightThrough: &comparison.Right.Through}
	before, err := m.CreateKunEvaluation(context.Background(), input)
	if err != nil || before.Left.Status != "unknown" || before.Right.Status != "unknown" || before.Left.Score != nil || before.ScoreDelta != nil {
		t.Fatal(before, err)
	}
	comparisonEvent(t, m, d.Target.ID, "branch-run", 1, "run.started", map[string]any{"fork": p.ForkState{Origin: d.Preview.Origin}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 2, "model.started", map[string]any{"step": 1})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 3, "model.completed", map[string]any{"step": 1, "message": p.Message{Role: "assistant", Content: "done"}})
	comparisonEvent(t, m, d.Target.ID, "branch-run", 4, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{}})
	after, err := m.CreateKunEvaluation(context.Background(), input)
	if err != nil || after.Comparison.Hash != before.Comparison.Hash || forkDigest(after.Right) != forkDigest(before.Right) {
		t.Fatal("fixed zero range changed", after, err)
	}
	latest, err := m.CompareKunFork(context.Background(), d.Preview.ID, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedComparisonHash = latest.Hash
	input.LeftThrough = &latest.Left.Through
	input.RightThrough = &latest.Right.Through
	graded, err := m.CreateKunEvaluation(context.Background(), input)
	if err != nil || graded.Right.Status != "pass" || graded.Comparison.Right.TokenUsageComplete || graded.Right.Score == nil || *graded.Right.Score != 100 || graded.ScoreDelta != nil {
		t.Fatal("unknown usage incorrectly prevents reply grading", graded, err)
	}
}

func TestKunEvaluationInputAndAuthorization(t *testing.T) {
	for _, rules := range [][]KunReplyRule{nil, {{Kind: "script", Value: "x"}}, {{Kind: "reply_contains", Value: " "}}, {{Kind: "reply_contains", Value: strings.Repeat("x", 2049)}}, {{Kind: "reply_contains", Value: "a"}, {Kind: "reply_contains", Value: "a"}}} {
		if validateReplyRules(rules) == nil {
			t.Fatal("invalid rules accepted", rules)
		}
	}
	rules := []KunReplyRule{}
	for i := 0; i < 13; i++ {
		rules = append(rules, KunReplyRule{Kind: "reply_contains", Value: string(rune('a' + i))})
	}
	if validateReplyRules(rules) == nil {
		t.Fatal("unbounded rules")
	}
	m, user, code, _, viewer, source, _ := userFixture(t)
	_, _, key := setupKey(t, m, "evaluation-key", source.WorkspaceID, "read", "run", "approvals")
	h := NewHandler(m, userTestAdmin, true)
	routes := []struct{ method, path string }{{"GET", "/kun-rubrics?previewId=id"}, {"POST", "/kun-rubrics"}, {"GET", "/kun-rubrics/id"}, {"GET", "/kun-evaluations?previewId=id"}, {"POST", "/kun-evaluations"}, {"GET", "/kun-evaluations/id"}}
	for _, token := range []string{code, viewer, key} {
		for _, prefix := range []string{"/api", "/api/v1", "/api/v1/member/" + user.Grants[0].ID} {
			for _, route := range routes {
				r := appRequest(h, route.method, prefix+route.path, `{}`, token, "denied-eval")
				if r.Code != 403 {
					t.Fatal(token[:3], route, prefix, r.Code)
				}
			}
		}
	}
	for _, path := range []string{"/kun-rubrics", "/kun-evaluations", "/kun-evaluations?previewId=id&limit=51", "/kun-rubrics?previewId=id&offset=-1", "/kun-evaluations?previewId=id&previewId=other", "/kun-evaluations/id?unknown=1"} {
		r := appRequest(h, "GET", "/api/v1"+path, "", userTestAdmin, "")
		if r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	for _, path := range []string{"/kun-rubrics", "/kun-evaluations"} {
		r := appRequest(h, "POST", "/api/v1"+path, `{}`, userTestAdmin, "")
		if r.Code != 400 {
			t.Fatal("missing idempotency key accepted", path, r.Code)
		}
	}
}
