package kun

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func planReply(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: text}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 7}})
}

func TestPlanActExecutionAndHarnessRevision(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		_ = json.NewDecoder(r.Body).Decode(&q)
		n := calls.Add(1)
		if n == 1 {
			if _, ok := q["tools"]; ok || q["tool_choice"] != "none" {
				t.Error("planning advertised tools", q)
			}
			planReply(w, "Create proof.txt, then check the result.")
			return
		}
		if n == 2 {
			if q["tools"] == nil || !strings.Contains(string(p.JSON(q["messages"])), "Advisory execution plan") {
				t.Error("act lost catalog or plan")
			}
			modelTools(w, []p.ToolCall{writeCall("proof", "proof.txt")}, map[string]int{"total_tokens": 11})
			return
		}
		planReply(w, "done")
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.Harness.LoopPolicy = "plan-act-v1"
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	<-e.done
	if s.Step != 3 || s.Budget.ReportedTokens != 25 || s.Budget.ToolCalls != 1 || s.Harness.ID != "plan-act-v1" || s.Modules["planning"].Phase != "ready" || !compatibleHarness(s) {
		t.Fatal(s)
	}
	if raw, _ := os.ReadFile(filepath.Join(in.Workspace, "proof.txt")); string(raw) != "verified" {
		t.Fatal("action did not run")
	}
	for _, m := range s.Messages {
		if strings.Contains(m.Content, "Create proof.txt") {
			t.Fatal("plan leaked into canonical conversation")
		}
	}
	events, _ := e.Events(0)
	plans := 0
	for _, ev := range events {
		if ev.Type == "kun/model.completed" && strings.Contains(string(ev.Data), `"purpose":"plan"`) {
			plans++
		}
	}
	if plans != 1 {
		t.Fatal("plan evidence missing", plans)
	}
	// Invalid module pairs are rejected without altering the completed run.
	bad := in
	bad.RunID = "bad"
	bad.Config.Harness.Planning = "no-explicit-plan-v1"
	if _, err := e.Start(bad); err == nil {
		t.Fatal("incompatible modules accepted")
	}
	if e.State().RunID != in.RunID || calls.Load() != 3 {
		t.Fatal("rejection mutated state")
	}
	in.RunID = "tool-loop-next"
	in.Config.Harness = p.HarnessConfig{}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, e, "completed")
	<-e.done
	if s.Harness.ID != "tool-loop-v1" || s.Harness.Revision != 2 || s.Step != 1 || !compatibleHarness(s) {
		t.Fatal(s)
	}
	for _, change := range []func(*p.State){func(s *p.State) { s.Harness.Version = "unknown" }, func(s *p.State) {
		m := s.Modules["memory"]
		m.Implementation.StateSchemaVersion = 999
		s.Modules["memory"] = m
	}, func(s *p.State) { s.Config.Harness.Memory = "unknown" }} {
		badState := clone(s)
		change(&badState)
		if compatibleHarness(badState) {
			t.Fatal("unregistered snapshot accepted")
		}
	}
}

func TestPlanActRejectsPlanningToolsAndHonorsBudgets(t *testing.T) {
	for _, name := range []string{"tools", "empty", "oversized", "model-budget", "token-budget"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch name {
				case "tools":
					modelTools(w, []p.ToolCall{writeCall("illegal", "illegal.txt")}, map[string]int{"total_tokens": 7})
				case "empty":
					planReply(w, "  ")
				case "oversized":
					planReply(w, strings.Repeat("x", 32769))
				default:
					planReply(w, "proposed plan")
				}
			}))
			defer server.Close()
			e := newTestEngine(t)
			in := startRequest(t, server.URL)
			in.Config.Harness.LoopPolicy = "plan-act-v1"
			if name == "model-budget" {
				in.Config.MaxSteps = 1
			}
			if name == "token-budget" {
				in.Config.Budget.MaxTotalTokens = 7
			}
			if _, err := e.Start(in); err != nil {
				t.Fatal(err)
			}
			s := waitTerminal(t, e)
			if s.Status != "failed" || s.Step != 1 || s.Budget.ReportedTokens != 7 || s.Budget.ToolCalls != 0 || calls.Load() != 1 || len(s.Actions) != 0 {
				t.Fatal(s)
			}
			if _, err := os.Stat(filepath.Join(in.Workspace, "illegal.txt")); !os.IsNotExist(err) {
				t.Fatal("planning dispatched tools")
			}
			if name == "model-budget" && s.Budget.StopReason != "model_calls" {
				t.Fatal(s.Budget)
			}
			if name == "token-budget" && s.Budget.StopReason != "reported_token_threshold" {
				t.Fatal(s.Budget)
			}
		})
	}
}

func TestPlanActCheckpointAndHybridRetainPlan(t *testing.T) {
	var plans, acts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		_ = json.NewDecoder(r.Body).Decode(&q)
		if q["tool_choice"] == "none" {
			plans.Add(1)
			planReply(w, "retained plan")
			return
		}
		acts.Add(1)
		messages := q["messages"].([]any)
		planAt, userAt := -1, -1
		for n, raw := range messages {
			m := raw.(map[string]any)
			if m["role"] == "user" {
				userAt = n
			}
			if strings.Contains(m["content"].(string), "Advisory execution plan") {
				planAt = n
			}
		}
		if planAt <= userAt {
			t.Error("plan inserted before original goal", planAt, userAt)
		}
		planReply(w, "done")
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, server.URL)
	in.Config.Harness.LoopPolicy = "plan-act-v1"
	in.Config.PauseBeforeModel = true
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	checkpointControl(t, e, "step")
	s := waitKun(t, e, "paused")
	if s.Step != 1 || s.Modules["planning"].Phase != "ready" || acts.Load() != 0 {
		t.Fatal(s)
	}
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
	in.RunID = "resumed"
	in.Resume = &check.Selection
	bad := in
	bad.Config.Harness = p.HarnessConfig{}
	if _, err = e.Start(bad); err == nil {
		t.Fatal("resume changed policy")
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	checkpointControl(t, e, "resume")
	waitKun(t, e, "completed")
	<-e.done
	if plans.Load() != 1 || acts.Load() != 1 {
		t.Fatal("resume replanned", plans.Load(), acts.Load())
	}
	points, err := e.ForkPoints(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	for _, point := range points.Items {
		if point.Phase == "before_model" && point.Step == 1 {
			selection.Sequence = point.Sequence
			break
		}
	}
	if selection.Sequence == 0 {
		t.Fatal(points)
	}
	bundle, err := e.ExportFork(p.ForkExport{Selection: selection})
	if err != nil {
		t.Fatal(err)
	}
	branch := newTestEngine(t)
	fork := in
	fork.Resume = nil
	fork.SessionID = "branch"
	fork.RunID = "branch-run"
	fork.Workspace = t.TempDir()
	fork.Fork = &p.ForkStart{Origin: p.ForkOrigin{PreviewID: "preview", SessionID: bundle.State.SessionID, RunID: bundle.State.RunID, Sequence: selection.Sequence, Through: selection.Through, BundleHash: bundle.ContentHash, Mode: "hybrid"}, Bundle: bundle}
	if _, err = branch.Start(fork); err != nil {
		t.Fatal(err)
	}
	waitKun(t, branch, "paused")
	checkpointControl(t, branch, "resume")
	s = waitKun(t, branch, "completed")
	if plans.Load() != 1 || acts.Load() != 2 || s.Fork.InheritedStep != 1 || s.Budget.ReportedTokens != 14 {
		t.Fatal(s, plans.Load(), acts.Load())
	}
	liveRecording, err := e.ExportFork(p.ForkExport{Selection: selection, Mode: "live"})
	if err != nil {
		t.Fatal(err)
	}
	live := newTestEngine(t)
	request := liveRequest(in, liveRecording, "live-plan")
	if _, err = live.Start(request); err != nil {
		t.Fatal(err)
	}
	waitKun(t, live, "paused")
	checkpointControl(t, live, "resume")
	s = waitKun(t, live, "completed")
	if plans.Load() != 1 || acts.Load() != 3 || s.Fork.InheritedStep != 1 || s.Fork.Origin.Mode != "live" {
		t.Fatal(s, plans.Load(), acts.Load())
	}

}

func TestPlanActSteerFollowsAdvisoryPlan(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Messages   []p.Message `json:"messages"`
			ToolChoice string      `json:"tool_choice"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if calls.Add(1) == 1 {
			planReply(w, "initial proposal")
			return
		}
		planIndex, steerIndex := -1, -1
		for n, m := range q.Messages {
			if strings.Contains(m.Content, "Advisory execution plan") {
				planIndex = n
			}
			if m.Content == "new constraint" {
				steerIndex = n
			}
		}
		if planIndex < 0 || steerIndex <= planIndex || q.ToolChoice == "none" {
			t.Error("steer missing, reordered or triggered unrequested replanning", q)
		}
		planReply(w, "done")
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.Harness.LoopPolicy = "plan-act-v1"
	in.Config.PauseBeforeModel = true
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	checkpointControl(t, e, "step")
	s := waitKun(t, e, "paused")
	if _, err := e.Control(p.Control{RequestID: "plan-act-steer", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "steer", Text: "new constraint"}); err != nil {
		t.Fatal(err)
	}
	checkpointControl(t, e, "resume")
	waitKun(t, e, "completed")
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
}
