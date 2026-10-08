package kun

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
)

func TestHybridForkRecordingAndMissIsolation(t *testing.T) {
	var sourceCalls, branchCalls, mode atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Messages []p.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		hybrid := strings.Contains(q.Messages[0].Content, "RunDesk Hybrid")
		if hybrid {
			branchCalls.Add(1)
		} else {
			sourceCalls.Add(1)
		}
		msg := p.Message{Role: "assistant", Content: "done"}
		hasResults := false
		for _, message := range q.Messages {
			if message.Role == "tool" {
				hasResults = true
			}
		}
		if !hasResults {
			path := "input.txt"
			if hybrid && mode.Load() == 1 {
				path = "different.txt"
			}
			msg.Content = ""
			msg.ToolCalls = []p.ToolCall{{ID: "read-1", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"` + path + `"}`}}, {ID: "write-1", Type: "function", Function: p.Function{Name: "write_file", Arguments: `{"path":"proof.txt","content":"original write"}`}}}
			if hybrid && mode.Load() == 2 {
				msg.ToolCalls[0], msg.ToolCalls[1] = msg.ToolCalls[1], msg.ToolCalls[0]
			}
		} else if hybrid && mode.Load() == 3 {
			msg.ToolCalls = []p.ToolCall{{ID: "repeat", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"input.txt"}`}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 4}})
	}))
	defer model.Close()
	source, err := Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	in := startRequest(t, model.URL)
	in.Config.MaxSteps = 8
	os.WriteFile(filepath.Join(in.Workspace, "input.txt"), []byte("recorded input"), 0600)
	if _, err = source.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, source, "completed")
	<-source.done
	before := source.State()
	points, err := source.ForkPoints(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	var pendingSelection p.ForkSelection
	for _, v := range points.Items {
		if v.Phase == "before_model" && v.Step == 0 {
			selection.Sequence = v.Sequence
		}
		if v.Phase == "after_model" && v.Pending == 2 {
			pendingSelection = points.Selection
			pendingSelection.Sequence = v.Sequence
		}
	}
	if selection.Sequence == 0 || pendingSelection.Sequence == 0 {
		t.Fatal(points)
	}
	bundle, err := source.ExportFork(p.ForkExport{Selection: selection})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Records) != 2 || sourceCalls.Load() != 2 {
		t.Fatal(bundle.Records, sourceCalls.Load())
	}
	for _, change := range []func(*p.ForkSelection){func(v *p.ForkSelection) { v.WorkerEpoch = "stale" }, func(v *p.ForkSelection) { v.ExpectedRevision++ }, func(v *p.ForkSelection) { v.Through++ }, func(v *p.ForkSelection) { v.SourceRunID = "other" }} {
		bad := selection
		change(&bad)
		if _, err = source.ExportFork(p.ForkExport{Selection: bad}); err == nil {
			t.Fatal("stale selection accepted", bad)
		}
	}
	os.WriteFile(filepath.Join(in.Workspace, "proof.txt"), []byte("do not overwrite"), 0600)
	os.WriteFile(filepath.Join(in.Workspace, "input.txt"), []byte("new world"), 0600)
	makeRequest := func(b p.ForkBundle, sid string) p.Start {
		v := in
		v.SessionID = sid
		v.RunID = sid + "-run"
		v.Workspace = t.TempDir()
		v.Fork = &p.ForkStart{Origin: p.ForkOrigin{PreviewID: "preview", SessionID: b.State.SessionID, RunID: b.State.RunID, Sequence: b.Selection.Sequence, Through: b.Selection.Through, BundleHash: b.ContentHash, Mode: "hybrid"}, Bundle: b}
		return v
	}
	for _, test := range []struct {
		name string
		mode int32
		fail bool
	}{{"matched", 0, false}, {"arguments", 1, true}, {"order", 2, true}, {"exhausted", 3, true}, {"hypothesis", 0, false}, {"hypothesis-empty", 0, false}, {"hypothesis-miss", 1, true}} {
		t.Run(test.name, func(t *testing.T) {
			mode.Store(test.mode)
			engine, err := Open(filepath.Join(t.TempDir(), "branch.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			request := makeRequest(bundle, test.name)
			hypothetical := strings.HasPrefix(test.name, "hypothesis")
			if hypothetical {
				b := request.Fork.Bundle
				r := b.Records[0]
				output := "assumed result <script>"
				if test.name == "hypothesis-empty" {
					output = ""
				}
				b.Hypothesis = &p.ReplayHypothesis{ParentPreviewID: "parent", ParentHash: strings.Repeat("a", 64), ParentBundleHash: b.ContentHash, Position: 0, SourceSequence: r.Sequence, RecordHash: p.ReplayRecordHash(r), OriginalOutputHash: p.TextHash(r.Output), OutputHash: p.TextHash(output), Output: output, Reason: "controlled counterfactual"}
				b.ContentHash = p.ForkHash(b)
				request.Fork.Bundle = b
				request.Fork.Origin.BundleHash = b.ContentHash
			}
			if _, err = engine.Start(request); err != nil {
				t.Fatal(err)
			}
			<-engine.done
			state := engine.State()
			if test.fail {
				if state.Status != "failed" || state.Budget.StopReason != "replay_miss" || !strings.Contains(state.Error, "replay_miss") {
					t.Fatal(state)
				}
			} else {
				if state.Status != "completed" || state.Fork.ReplayCursor != 2 || state.Budget.ToolCalls != 2 {
					t.Fatal(state)
				}
				seen := false
				for _, message := range state.Messages {
					if message.Role == "tool" && ((!hypothetical && strings.Contains(message.Content, "recorded input")) || (hypothetical && message.Content == request.Fork.Bundle.Hypothesis.Output)) {
						seen = true
					}
				}
				if !seen {
					t.Fatal("did not use recorded input")
				}
			}
			if raw, _ := os.ReadFile(filepath.Join(in.Workspace, "proof.txt")); string(raw) != "do not overwrite" {
				t.Fatal("source write repeated")
			}
			if _, err = os.Stat(filepath.Join(request.Workspace, "proof.txt")); !os.IsNotExist(err) {
				t.Fatal("branch executed write")
			}
			events, err := engine.Events(0)
			if err != nil {
				t.Fatal(err)
			}
			seenHypothesis := 0
			for _, ev := range events {
				if ev.Type == "kun/tool.started" || ev.Type == "kun/mcp.connecting" {
					t.Fatal("live dispatch", ev.Type)
				}
				if ev.Type == "kun/tool.completed" && !strings.Contains(string(ev.Data), `"executed":false`) {
					t.Fatal("unmarked replay", string(ev.Data))
				}
				if ev.Type == "kun/tool.completed" {
					var data struct {
						Replay *p.ReplayEvidence `json:"replay"`
						Output string            `json:"output"`
					}
					json.Unmarshal(ev.Data, &data)
					if data.Replay != nil && data.Replay.Mode == "hypothetical" {
						seenHypothesis++
						h := request.Fork.Bundle.Hypothesis
						if h == nil || data.Replay.HypothesisHash != p.HypothesisHash(*h) || data.Output != h.Output || data.Replay.OutputHash != h.OutputHash || data.Replay.OriginalOutputHash != h.OriginalOutputHash {
							t.Fatal("bad hypothetical evidence", data)
						}
					}
				}
			}
			if hypothetical && !test.fail && (seenHypothesis != 1 || state.Fork.HypothesisHash == "" || !strings.Contains(state.Messages[0].Content, "USER-AUTHORED HYPOTHETICAL")) {
				t.Fatal("missing hypothetical evidence", seenHypothesis)
			}
			if test.name == "hypothesis-miss" && seenHypothesis != 0 {
				t.Fatal("miss used hypothesis")
			}
			calls := branchCalls.Load()
			if _, err = engine.Start(request); err != nil || branchCalls.Load() != calls {
				t.Fatal("duplicate fork executed", err)
			}
			ordinary := request
			ordinary.Fork = nil
			ordinary.RunID += "-ordinary"
			if _, err = engine.Start(ordinary); err == nil {
				t.Fatal("Hybrid changed into ordinary run")
			}
			if check, err := engine.Checkpoint(); err != nil || check.Eligible || check.Reason != "hybrid_use_new_fork" {
				t.Fatal(check, err)
			}
			if _, err = engine.executeAction(context.Background(), toolIntent{Name: "write_file"}, p.ToolCall{}); err == nil {
				t.Fatal("gateway allowed Hybrid")
			}
		})
	}
	mode.Store(0)
	pending, err := source.ExportFork(p.ForkExport{Selection: pendingSelection})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Open(filepath.Join(t.TempDir(), "pending.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	request := makeRequest(pending, "pending")
	request.Fork.Instruction = "new hypothesis"
	calls := branchCalls.Load()
	if _, err = engine.Start(request); err != nil {
		t.Fatal(err)
	}
	<-engine.done
	state := engine.State()
	if state.Status != "completed" || state.Fork.InheritedStep != 1 || state.Fork.InheritedBudget.ReportedTokens != 4 || branchCalls.Load() != calls+1 {
		t.Fatal(state, branchCalls.Load(), calls)
	}
	for n, message := range state.Messages {
		if message.Content == "new hypothesis" && (n == 0 || state.Messages[n-1].Role != "tool") {
			t.Fatal("instruction broke pending tool-message order")
		}
	}
	if !strings.Contains(string(p.JSON(state.Messages)), "new hypothesis") {
		t.Fatal("instruction was lost")
	}
	if fingerprint(before) != fingerprint(source.State()) {
		t.Fatal("source state changed")
	}
	pointsAfter, _ := source.ForkPoints(0, 50)
	if pointsAfter.Selection.Through != points.Selection.Through {
		t.Fatal("source journal changed")
	}
	for _, test := range []struct {
		name   string
		change func(*p.Start)
	}{
		{"content", func(r *p.Start) { r.Fork.Bundle.Records[0].Output = "tampered" }},
		{"catalog", func(r *p.Start) { r.Fork.Bundle.CatalogHash = "other" }},
		{"environment", func(r *p.Start) { r.Fork.Bundle.EnvironmentHash = "other" }},
		{"schema", func(r *p.Start) { r.Fork.Bundle.Schema++ }},
		{"live_mcp", func(r *p.Start) { r.MCP = []p.MCPServer{mcpSpec("http://localhost:9/mcp")} }},
		{"config", func(r *p.Start) { r.Config.MaxSteps++ }},
		{"overlay", func(r *p.Start) {
			r.Fork.Bundle.Hypothesis = &p.ReplayHypothesis{Position: 0, Output: "tampered"}
			r.Fork.Bundle.ContentHash = p.ForkHash(r.Fork.Bundle)
			r.Fork.Origin.BundleHash = r.Fork.Bundle.ContentHash
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newTestEngine(t)
			r := makeRequest(bundle, "invalid-"+test.name)
			var copied p.ForkStart
			json.Unmarshal(p.JSON(r.Fork), &copied)
			r.Fork = &copied
			test.change(&r)
			if _, err := e.Start(r); err == nil {
				t.Fatal("invalid fork admitted")
			}
			if e.State().SessionID != "" {
				t.Fatal("failed admission mutated state")
			}
		})
	}
}

func TestHybridMCPReplayWithoutConnection(t *testing.T) {
	var calls, models atomic.Int32
	server := mcpFixture(t, &calls, "normal", nil)
	defer server.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	source := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.DisabledTools = []string{"delete_item"}
	spec.ApprovalModes = map[string]string{"lookup weather": "approve"}
	in.MCP = []p.MCPServer{spec}
	if _, err := source.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, source, "completed")
	<-source.done
	points, err := source.ForkPoints(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	q := points.Selection
	for _, pt := range points.Items {
		if pt.Phase == "before_model" && pt.Step == 0 {
			q.Sequence = pt.Sequence
		}
	}
	b, err := source.ExportFork(p.ForkExport{Selection: q})
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	models.Store(0)
	e := newTestEngine(t)
	request := in
	request.MCP = nil
	request.SessionID = "mcp-branch"
	request.RunID = "mcp-branch-run"
	request.Workspace = t.TempDir()
	request.Fork = &p.ForkStart{Bundle: b, Origin: p.ForkOrigin{Mode: "hybrid", PreviewID: "preview", SessionID: b.State.SessionID, RunID: b.State.RunID, Sequence: q.Sequence, Through: q.Through, BundleHash: b.ContentHash}}
	if _, err = e.Start(request); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	<-e.done
	if calls.Load() != 1 || models.Load() != 2 || e.State().Fork.ReplayCursor != 1 {
		t.Fatal(calls.Load(), models.Load(), e.State())
	}
	events, _ := e.Events(0)
	for _, ev := range events {
		if strings.HasPrefix(ev.Type, "kun/mcp.") || ev.Type == "kun/approval.requested" || ev.Type == "kun/tool.started" {
			t.Fatal("external path used", ev.Type)
		}
	}
	// Persisted fork sessions stay single-run even without an in-memory tape.
	path := filepath.Join(t.TempDir(), "reopen.db")
	saved, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	request.SessionID = "reopen"
	request.RunID = "reopen-run"
	// A token threshold inherited at the selected boundary is not reset.
	request.Config.Budget = request.Config.Normalized().Budget
	request.Fork.Bundle.State.Budget.ReportedTokens = 10
	request.Config.Budget.MaxTotalTokens = 10
	request.Fork.Bundle.State.Config = request.Config.Normalized()
	request.Fork.Bundle.State.Manifest.ConfigHash = fingerprint(request.Fork.Bundle.State.Config)
	request.Fork.Bundle.EnvironmentHash = fingerprint(request.Fork.Bundle.State.Manifest)
	request.Fork.Bundle.ContentHash = p.ForkHash(request.Fork.Bundle)
	request.Fork.Origin.BundleHash = request.Fork.Bundle.ContentHash
	if _, err = saved.Start(request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-saved.done:
	case <-time.After(5 * time.Second):
		t.Fatal("budget did not stop")
	}
	if saved.State().Budget.StopReason != "reported_token_threshold" || models.Load() != 2 {
		t.Fatal(saved.State().Budget, models.Load())
	}
	saved.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	request.Fork = nil
	request.RunID = "ordinary-after-reopen"
	if _, err = reopened.Start(request); err == nil {
		t.Fatal("reopened branch escaped Hybrid")
	}
}

func TestHybridArgumentIdentity(t *testing.T) {
	a, err := replayArguments(`{"b":[1,2], "a":{"n":9007199254740993}}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := replayArguments(` { "a": {"n":9007199254740993},"b":[1,2] } `)
	if err != nil || a != b {
		t.Fatal(a, b, err)
	}
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":{"x":1,"x":2}}`, `[]`, `{} {}`, `{"n":NaN}`} {
		if _, err := replayArguments(raw); err == nil {
			t.Fatal("invalid args", raw)
		}
	}
	c, _ := replayArguments(`{"a":{"n":9007199254740992},"b":[1,2]}`)
	if a == c {
		t.Fatal("integer precision lost")
	}
}
