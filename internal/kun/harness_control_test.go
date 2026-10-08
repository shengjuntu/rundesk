package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func harnessCommand(s p.State, policy string) p.Control {
	return p.Control{RequestID: fmt.Sprintf("harness-%s-%d", s.RunID, s.Revision), RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "set_harness", Harness: &p.HarnessConfig{LoopPolicy: policy}, Reason: "Compare explicit planning with the same task and remaining budget"}
}

func TestRuntimeHarnessSwitchRecoveryForksAndDefault(t *testing.T) {
	var plans, acts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		_ = json.NewDecoder(r.Body).Decode(&q)
		if q["tool_choice"] == "none" {
			plans.Add(1)
			if q["tools"] != nil {
				t.Error("planning advertised tools")
			}
			planReply(w, "new explicit plan")
		} else {
			acts.Add(1)
			planReply(w, "done")
		}
	}))
	defer provider.Close()
	path := filepath.Join(t.TempDir(), "runtime.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	in := startRequest(t, provider.URL)
	in.Config.PauseBeforeModel = true
	in.ApprovalPolicy = "on-request"
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	before := waitKun(t, e, "paused")
	events, _ := e.Events(0)
	snapshot, _ := e.Snapshot(events[len(events)-1].Sequence)
	cmd := harnessCommand(before, "plan-act-v1")
	receipt, err := e.Control(cmd)
	if err != nil {
		t.Fatal(err)
	}
	after := e.State()
	if after.Status != "paused" || after.Harness.Revision != 2 || after.Harness.ID != "plan-act-v1" || !compatibleHarness(after) || fingerprint(after.Config) != fingerprint(before.Config) || fingerprint(after.Budget) != fingerprint(before.Budget) || fingerprint(after.Messages) != fingerprint(before.Messages) || plans.Load() != 0 || acts.Load() != 0 {
		t.Fatal("switch changed admission, budget or execution", after)
	}
	if retry, err := e.Control(cmd); err != nil || retry != receipt || e.State().Revision != after.Revision {
		t.Fatal("retry changed state", retry, err)
	}
	conflict := cmd
	conflict.Reason = "different reason"
	if _, err := e.Control(conflict); err == nil {
		t.Fatal("request ID conflict accepted")
	}
	stale := harnessCommand(before, "tool-loop-v1")
	stale.RequestID += "-stale"
	if _, err := e.Control(stale); err == nil {
		t.Fatal("stale revision accepted")
	}
	checkpointControl(t, e, "step")
	planned := waitKun(t, e, "paused")
	if planned.Step != 1 || plans.Load() != 1 || planned.Modules["planning"].Phase != "ready" {
		t.Fatal(planned)
	}
	if _, err := e.Control(harnessCommand(planned, "tool-loop-v1")); err != nil {
		t.Fatal(err)
	}
	without := e.State()
	if strings.Contains(string(p.JSON(requestBody(without))), "Advisory execution plan") || without.Step != 1 {
		t.Fatal("old plan leaked after removal")
	}
	if _, err := e.Control(harnessCommand(without, "plan-act-v1")); err != nil {
		t.Fatal(err)
	}
	checkpointControl(t, e, "step")
	planned = waitKun(t, e, "paused")
	if planned.Step != 2 || plans.Load() != 2 || planned.Budget.ReportedTokens != 14 || planned.Harness.Revision != 4 {
		t.Fatal("re-entry did not replan with inherited budget", planned)
	}
	saved, _ := e.Snapshot(snapshot.Sequence)
	if fingerprint(saved) != fingerprint(snapshot) {
		t.Fatal("old snapshot changed")
	}
	e.Close()
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check, err := e.CheckpointFor(in)
	if err != nil || !check.Eligible {
		t.Fatal("runtime override prevented recovery with original config", check, err)
	}
	in.RunID, in.Resume = "recovered", &check.Selection
	bad := in
	bad.Config.Harness = p.HarnessConfig{LoopPolicy: "plan-act-v1"}
	if _, err := e.Start(bad); err == nil {
		t.Fatal("recovery accepted changed admission config")
	}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	recovered := waitKun(t, e, "paused")
	if recovered.Harness.Revision != 4 || recovered.RuntimeHarness == nil || recovered.Modules["planning"].Phase != "ready" {
		t.Fatal(recovered)
	}
	checkpointControl(t, e, "resume")
	waitKun(t, e, "completed")
	<-e.done
	if plans.Load() != 2 || acts.Load() != 1 {
		t.Fatal("recovery replanned", plans.Load(), acts.Load())
	}
	b := liveBundle(t, e, func(s p.State) bool { return s.Step == 2 && s.Harness.Revision == 4 && s.Phase == "before_model" })
	for _, mode := range []string{"live", "hybrid"} {
		t.Run(mode, func(t *testing.T) {
			bundle, err := e.ExportFork(p.ForkExport{Selection: b.Selection, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			fork := newTestEngine(t)
			request := liveRequest(in, bundle, "fork-"+mode)
			request.Fork.Origin.Mode = mode
			if mode == "hybrid" {
				request.Workspace = t.TempDir()
			}
			if _, err := fork.Start(request); err != nil {
				t.Fatal(err)
			}
			paused := waitKun(t, fork, "paused")
			if paused.Harness.ID != "plan-act-v1" || paused.Harness.Revision != 4 {
				t.Fatal("fork lost runtime override")
			}
			if _, err := fork.Control(harnessCommand(paused, "tool-loop-v1")); err == nil {
				t.Fatal("fork changed fixed composition")
			}
			checkpointControl(t, fork, "resume")
			waitKun(t, fork, "completed")
		})
	}
	if plans.Load() != 2 || acts.Load() != 3 {
		t.Fatal("fork replanned", plans.Load(), acts.Load())
	}
	in.Resume, in.RunID = nil, "next-ordinary"
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	next := waitKun(t, e, "paused")
	if next.RuntimeHarness != nil || next.Harness.ID != "tool-loop-v1" || next.Harness.Revision != 5 {
		t.Fatal("ordinary run inherited runtime override", next)
	}
	checkpointControl(t, e, "resume")
	waitKun(t, e, "completed")
}

func TestRuntimeHarnessRejectsUnsafeAndExhaustedStates(t *testing.T) {
	e := newTestEngine(t)
	in := startRequest(t, "http://127.0.0.1:9")
	in.Config.PauseBeforeModel = true
	in.ApprovalPolicy = "on-request"
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	base := waitKun(t, e, "paused")
	cases := map[string]func(*p.State){
		"model":          func(s *p.State) { s.Phase = "model" },
		"after_model":    func(s *p.State) { s.Phase = "after_model" },
		"tool_batch":     func(s *p.State) { s.Pending = []p.ToolCall{writeCall("pending", "pending.txt")} },
		"approval":       func(s *p.State) { s.Approval = &p.ToolApproval{CallID: "old", Decision: "approve"} },
		"queued":         func(s *p.State) { s.Queued = []p.Control{{Operation: "steer", Text: "pending"}} },
		"unknown":        func(s *p.State) { s.Actions = map[string]string{"write": "outcome_unknown"} },
		"prepared":       func(s *p.State) { s.Actions = map[string]string{"write": "prepared"} },
		"diagnostic":     func(s *p.State) { s.Diagnostic = &p.DiagnosticScope{} },
		"module_version": func(s *p.State) { s.Harness.Version = "unknown" },
		"model_budget":   func(s *p.State) { s.Step = s.Config.MaxSteps },
		"token_budget":   func(s *p.State) { s.Config.Budget.MaxTotalTokens = 10; s.Budget.ReportedTokens = 10 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e.mu.Lock()
			e.state = clone(base)
			mutate(&e.state)
			before := clone(e.state)
			e.mu.Unlock()
			if _, err := e.Control(harnessCommand(before, "plan-act-v1")); err == nil {
				t.Fatal("unsafe switch accepted")
			}
			if fingerprint(before) != fingerprint(e.State()) {
				t.Fatal("rejection changed state")
			}
		})
	}
	e.mu.Lock()
	e.state = base
	e.mu.Unlock()
	for _, edit := range []func(*p.Control){
		func(c *p.Control) { c.Reason = "" }, func(c *p.Control) { c.Reason = strings.Repeat("x", 2049) },
		func(c *p.Control) { c.Harness.Memory = "unknown" }, func(c *p.Control) { c.Harness.Planning = "no-explicit-plan-v1" },
		func(c *p.Control) { c.Operation = "resume" }, func(c *p.Control) { c.Text = "unreviewed steer" },
	} {
		c := harnessCommand(base, "plan-act-v1")
		edit(&c)
		if _, err := e.Control(c); err == nil {
			t.Fatal("invalid command accepted", c)
		}
		if fingerprint(base) != fingerprint(e.State()) {
			t.Fatal("invalid command changed state")
		}
	}
}

func TestRuntimeHarnessCommitFailureRollsBack(t *testing.T) {
	e := newTestEngine(t)
	in := startRequest(t, "http://127.0.0.1:9")
	in.Config.PauseBeforeModel = true
	in.ApprovalPolicy = "on-request"
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	before := waitKun(t, e, "paused")
	_, err := e.j.db.Exec(`CREATE TRIGGER fail_switch BEFORE INSERT ON commands BEGIN SELECT RAISE(ABORT,'fixture commit failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(harnessCommand(before, "plan-act-v1")); err == nil {
		t.Fatal("failed commit acknowledged")
	}
	<-e.done
	if e.State().Harness.ID != "tool-loop-v1" || e.State().RuntimeHarness != nil {
		t.Fatal("failed commit retained migration")
	}
	var count int
	_ = e.j.db.QueryRow("SELECT count(*) FROM commands").Scan(&count)
	if count != 0 {
		t.Fatal("uncommitted receipt persisted")
	}
}
