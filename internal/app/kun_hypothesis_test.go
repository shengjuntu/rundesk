package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestKunHypothesisOfflineReviewValidationAndComparison(t *testing.T) {
	m := testManager(t)
	m.Kun = "nonexistent-worker"
	base := p.State{SessionID: "source", RunID: "run", Manifest: &p.RunManifest{EngineVersion: p.EngineVersion}}
	d := comparisonDraft(t, m, "parent", "hybrid", base)
	d.Bundle.Records = []p.ReplayRecord{{Sequence: 12, Tool: "read_file", Status: "failed", IsError: true, Output: `{"apiKey":"SECRET","n":9007199254740993,"text":"` + strings.Repeat("界", 2100) + `"}`}}
	d.Bundle.ContentHash = p.ForkHash(d.Bundle)
	d.Preview.RecordCount = 1
	d.Preview.Origin.BundleHash = d.Bundle.ContentHash
	o := d.Preview.Origin
	d.Target.KunFork = &o
	d.Preview.Hash = forkDraftHash(d)
	if err := m.Store.PutMany(store.Record{Kind: "kun_fork_bundle", ID: "parent", Value: d}, store.Record{Kind: "kun_fork_preview", ID: "parent", Value: d.Preview}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	request := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		return appRequest(h, method, "/api/v1"+path, string(p.JSON(body)), userTestAdmin, key)
	}
	r := request("GET", "/kun-forks/recordings/parent", nil, "")
	var reviews KunReplayReviews
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &reviews) != nil || len(reviews.Items) != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	review := reviews.Items[0]
	if !review.Redacted || !review.Truncated || strings.Contains(review.Preview, "SECRET") || !strings.Contains(review.Preview, "9007199254740993") || len([]rune(review.Preview)) != 2048 {
		t.Fatal(review)
	}
	position, output := 0, "假设替换 <script>literal</script>"
	in := KunHypothesisInput{ExpectedHash: d.Preview.Hash, Position: &position, RecordHash: review.RecordHash, Output: &output, Reason: "unique-reason", Title: "assumption"}
	r = request("POST", "/kun-forks/parent/hypotheses", in, "one-hypothesis")
	var child KunForkPreview
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &child) != nil || child.Hypothesis == nil || child.TargetSessionID == d.Target.ID {
		t.Fatal(r.Code, r.Body.String())
	}
	if _, err := m.Session(child.TargetSessionID); err == nil {
		t.Fatal("preview started session")
	}
	retry := request("POST", "/kun-forks/parent/hypotheses", in, "one-hypothesis")
	var retryChild KunForkPreview
	json.Unmarshal(retry.Body.Bytes(), &retryChild)
	if retry.Code != 200 || forkDigest(retryChild) != forkDigest(child) {
		t.Fatal("idempotency", retry.Code, retry.Body.String())
	}
	for n, change := range []func(*KunHypothesisInput){
		func(v *KunHypothesisInput) { v.ExpectedHash = "wrong" }, func(v *KunHypothesisInput) { v.RecordHash = "wrong" }, func(v *KunHypothesisInput) { v.Position = nil }, func(v *KunHypothesisInput) { v.Output = nil }, func(v *KunHypothesisInput) { v.Reason = "" },
	} {
		bad := in
		change(&bad)
		rr := request("POST", "/kun-forks/parent/hypotheses", bad, string(rune('a'+n)))
		if rr.Code != 400 && rr.Code != 409 {
			t.Fatal("accepted invalid input", n, rr.Code)
		}
	}
	if _, err := m.CreateKunHypothesis(child.ID, in); err == nil {
		t.Fatal("nested overlay")
	}
	old := comparisonDraft(t, m, "old", "hybrid", p.State{Manifest: &p.RunManifest{EngineVersion: "0.11.0"}})
	if _, err := m.KunReplayReviews(old.Preview.ID); err == nil {
		t.Fatal("old engine editable")
	}
	live := comparisonDraft(t, m, "live", "live", base)
	if _, err := m.KunReplayReviews(live.Preview.ID); err == nil {
		t.Fatal("Live editable")
	}
	if rr := request("GET", "/kun-forks/recordings/parent?unknown=1", nil, ""); rr.Code != 400 {
		t.Fatal(rr.Code)
	}
	if rr := appRequest(h, "POST", "/api/v1/kun-forks/parent/hypotheses", `{"status":"succeeded"}`, userTestAdmin, "unknown-field"); rr.Code != 400 {
		t.Fatal(rr.Code)
	}
	list := request("GET", "/kun-forks", nil, "").Body.String()
	if strings.Contains(list, "unique-reason") || strings.Contains(list, "假设替换") || strings.Contains(list, "SECRET") {
		t.Fatal("list leaked bodies", list)
	}
	parent, _ := m.kunForkDraft("parent")
	c, _ := m.kunForkDraft(child.ID)
	if forkDigest(parent) != forkDigest(d) || forkDigest(c.Bundle.Records) != forkDigest(d.Bundle.Records) || c.Bundle.Records[0].Status != "failed" {
		t.Fatal("original changed")
	}
	read := func() *KunCompareHypothesis {
		t.Helper()
		out, err := m.CompareKunFork(context.Background(), child.ID, "parent", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out.Right.Hypothesis
	}
	if read().Status != "unknown" {
		t.Fatal("unused empty log claimed complete")
	}
	comparisonEvent(t, m, child.TargetSessionID, "child-run", 1, "run.started", map[string]any{"fork": p.ForkState{Origin: child.Origin, HypothesisHash: p.HypothesisHash(*child.Hypothesis)}})
	comparisonEvent(t, m, child.TargetSessionID, "child-run", 2, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{}})
	if read().Status != "not_reached" {
		t.Fatal("unused assumption reported applied")
	}
	empty := ""
	in.Output = &empty
	second, err := m.CreateKunHypothesis("parent", in)
	if err != nil {
		t.Fatal(err)
	}
	summary := second.Hypothesis
	comparisonEvent(t, m, second.TargetSessionID, "second-run", 1, "run.started", map[string]any{"fork": p.ForkState{Origin: second.Origin, HypothesisHash: p.HypothesisHash(*summary)}})
	replayed := comparisonEvent(t, m, second.TargetSessionID, "second-run", 2, "tool.completed", map[string]any{"call": p.ToolCall{ID: "tool", Function: p.Function{Name: "read_file"}}, "status": "replayed", "replay": p.ReplayEvidence{Mode: "hypothetical", RecordedStatus: "failed", SourceSequence: summary.SourceSequence, Position: 0, BundleHash: second.Origin.BundleHash, HypothesisHash: p.HypothesisHash(*summary), OriginalOutputHash: summary.OriginalOutputHash, OutputHash: summary.OutputHash}})
	comparisonEvent(t, m, second.TargetSessionID, "second-run", 3, "run.finished", map[string]any{"status": "completed", "budget": p.BudgetUsage{}})
	comp, err := m.CompareKunFork(context.Background(), second.ID, "parent", nil, nil)
	if err != nil || comp.Right.Hypothesis.Status != "applied" || comp.Right.Hypothesis.EventID != replayed.ID || comp.Right.Tools[0].Hypothetical != 1 || comp.Right.Tools[0].Replayed != 0 || comp.Right.Tools[0].Dispatched != 0 || comp.Right.Tools[0].Failed != 1 {
		t.Fatal(comp, err)
	}
	if len(m.handles) != 0 {
		t.Fatal("offline operation started worker")
	}
}

func TestKunHypothesisHostWorkerLifecycle(t *testing.T) {
	m := kunMCPManager(t)
	binary := buildKunTestBinary(t)
	m.Kun = binary
	var calls, sawAssumption atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Messages []p.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		calls.Add(1)
		msg := p.Message{Role: "assistant", Content: "done"}
		last := q.Messages[len(q.Messages)-1]
		if last.Role != "tool" {
			msg.Content = ""
			msg.ToolCalls = []p.ToolCall{{ID: "read", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"input.txt"}`}}}
		} else if last.Content == "假设输出" {
			sawAssumption.Add(1)
			if !strings.Contains(q.Messages[0].Content, "USER-AUTHORED HYPOTHETICAL") {
				t.Error("model lacks hypothesis marker")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 5}})
	}))
	defer model.Close()
	i, _ := m.Instance()
	cfg := i.AgentRuntime
	cfg.Endpoint = model.URL + "/v1"
	if _, err := m.SetAgentRuntime(i.ID, i.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	ws := m.Workspaces()[0]
	os.WriteFile(filepath.Join(ws.Path, "input.txt"), []byte("source output"), 0600)
	source, err := m.CreateSession(ws.ID, "source", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "read"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, source.ID, "completed")
	var points p.ForkPoints
	for n := 0; n < 30; n++ {
		points, err = m.KunForkPoints(source.ID, 0, 50)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	for _, pt := range points.Items {
		if pt.Phase == "after_model" && pt.Pending == 1 {
			selection.Sequence = pt.Sequence
		}
	}
	parent, err := m.CreateKunFork(KunForkInput{SessionID: source.ID, Selection: selection, Title: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := m.KunReplayReviews(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	position, output := 0, "假设输出"
	child, err := m.CreateKunHypothesis(parent.ID, KunHypothesisInput{ExpectedHash: parent.Hash, Position: &position, RecordHash: reviews.Items[0].RecordHash, Output: &output, Reason: "counterfactual", Title: "assumption"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("preview called model")
	}
	os.Remove(filepath.Join(ws.Path, "input.txt"))
	if err = m.DeleteSession(source.ID); err != nil {
		t.Fatal(err)
	}
	data := m.Data
	m.Close()
	m2, err := New(data, "missing-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	m2.Kun = binary
	branch, err := m2.StartKunFork(child.ID, child.Hash, false)
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m2, branch.ID, "completed")
	if calls.Load() != 3 || sawAssumption.Load() != 1 {
		t.Fatal("worker did not consume assumed output", calls.Load(), sawAssumption.Load())
	}
	comparison, err := m2.CompareKunFork(context.Background(), child.ID, parent.ID, nil, nil)
	if err != nil || !comparison.Right.Complete || comparison.Right.Hypothesis.Status != "applied" || comparison.Right.Tools[0].Hypothetical != 1 || comparison.Right.Tools[0].Dispatched != 0 {
		t.Fatal(comparison, err)
	}
	if retry, err := m2.StartKunFork(child.ID, child.Hash, false); err != nil || retry.RunID != branch.RunID || calls.Load() != 3 {
		t.Fatal("duplicate execution", retry, err)
	}
	if err = m2.DeleteSession(branch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m2.StartKunFork(child.ID, child.Hash, false); err == nil {
		t.Fatal("deleted branch resurrected")
	}
}
