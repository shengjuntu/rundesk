package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Storage fixture only; actual distillation and execution use the HTTP fixture.
func benchmarkFixture(t *testing.T) (*Manager, KunSkill) {
	t.Helper()
	m := kunMCPManager(t)
	i, _ := m.Instance()
	w := m.Workspaces()[0]
	set, e := m.CreateKunTaskSet(KunTaskSetInput{Title: "heldout", Cases: []KunTaskCase{skillCase("a", "ALPHA"), skillCase("b", "BETA")}})
	if e != nil {
		t.Fatal(e)
	}
	c := KunSkill{Schema: 1, ID: store.ID(), Title: "storage fixture", CreatedAt: store.Now(), InstanceID: i.ID, WorkspaceID: w.ID, ValidationSet: set, Program: p.FastProgram{Schema: 1, Domain: "file-copy", InputSchema: json.RawMessage(`{"type":"object"}`), Guard: p.FastGuard{EngineVersion: p.EngineVersion, ConfigHash: "config", SkillsHash: "skills", MCPHash: "mcp", Workspace: w.Path, CatalogHash: "catalog", ApprovalPolicy: "never", HarnessHash: "harness"}, Steps: []p.FastInstruction{{Op: "tool", Tool: "read_file", Store: "result", Args: map[string]p.FastValue{"path": {Ref: "input#/path"}}}, {Op: "finish", Value: p.FastValue{Ref: "result"}}}}}
	c.Hash = skillHash(c)
	if e = m.Store.Put("kun_skill", c.ID, c); e != nil {
		t.Fatal(e)
	}
	return m, c
}
func TestKunBenchmarkFixedPlansUnknownAndRestart(t *testing.T) {
	m, c := benchmarkFixture(t)
	in := KunBenchmarkInput{Title: "paired", CandidateID: c.ID, ExpectedCandidateHash: c.Hash, Repetitions: 2}
	h := NewHandler(m, userTestAdmin, true)
	r := appRequest(h, "POST", "/api/v1/kun-benchmarks", string(p.JSON(in)), userTestAdmin, "benchmark-fixed-1")
	var v KunBenchmarkView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &v) != nil {
		t.Fatal(r.Code, r.Body.String())
	}
	r = appRequest(h, "POST", "/api/v1/kun-benchmarks", string(p.JSON(in)), userTestAdmin, "benchmark-fixed-1")
	var same KunBenchmarkView
	_ = json.Unmarshal(r.Body.Bytes(), &same)
	if r.Code != 200 || forkDigest(v) != forkDigest(same) {
		t.Fatal("idempotent preview changed")
	}
	b := v.Benchmark
	if len(b.Plans) != 8 || len(m.handles) != 0 || v.Execution.Status != "planned" {
		t.Fatal(v)
	}
	for n, plan := range b.Plans {
		view, e := m.benchmarkPlan(b, plan)
		if e != nil || view.Session != nil {
			t.Fatal(n, e)
		}
		if _, e = m.StartKunCaseRun(plan.PlanID, plan.PlanHash, true); e == nil {
			t.Fatal("individual benchmark plan admitted")
		}
	}
	if b.Plans[0].Arm != "dynamic" || b.Plans[2].Arm != "fast" || b.Plans[4].Arm != "fast" || b.Plans[6].Arm != "dynamic" {
		t.Fatal("order not counterbalanced")
	}
	if _, e := m.StartKunBenchmark(b.ID, b.Hash, false); e == nil {
		t.Fatal("unconfirmed batch admitted")
	}
	if _, e := m.StartKunBenchmark(b.ID, "stale", true); e == nil {
		t.Fatal("stale batch admitted")
	}
	report, e := m.EvaluateKunBenchmark(context.Background(), b.ID, b.Hash)
	if e != nil || report.Status != "partial" || report.Dynamic.Unknown != 4 || report.Fast.Unknown != 4 || report.Dynamic.PassRate != nil || report.Fast.Metrics.ReportedTokens.Total != nil || report.PairedDelta.ModelCalls.Missing != 4 {
		t.Fatal(report, e)
	}
	for _, pair := range report.Pairs {
		if pair.Outcome != "unknown" {
			t.Fatal(pair)
		}
	}
	if _, e = m.PromoteKunSkill(c.ID, c.Hash, report.ID, report.Hash, "app", 1); e == nil {
		t.Fatal("benchmark bypassed holdout gate")
	}
	for _, path := range []string{"/kun-benchmarks?candidateId=" + c.ID + "&limit=51", "/kun-benchmarks?unexpected=1", "/kun-benchmarks/" + b.ID + "?extra=1", "/kun-benchmark-reports"} {
		if r = appRequest(h, "GET", "/api/v1"+path, "", userTestAdmin, ""); r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	items, e := m.Store.KunBenchmarkList(context.Background(), "kun_benchmark", c.ID, 0, 1)
	if e != nil || len(items) != 1 || strings.Contains(string(items[0]), "ALPHA") {
		t.Fatal(items, e)
	}
	if e = m.Store.Put("kun_benchmark_execution", b.ID, KunBenchmarkExecution{Status: "running", Total: 8, Next: 0}); e != nil {
		t.Fatal(e)
	}
	data := m.Data
	m.Close()
	reopened, e := New(data, "missing-codex", false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	read, e := reopened.KunBenchmark(b.ID)
	if e != nil || read.Execution.Status != "interrupted" || forkDigest(read.Benchmark) != forkDigest(b) || len(reopened.handles) != 0 {
		t.Fatal(read, e)
	}
	saved, e := reopened.KunBenchmarkReport(report.ID)
	if e != nil || saved.Hash != report.Hash {
		t.Fatal(saved, e)
	}
}

func TestKunBenchmarkBoundsDriftAndFailedSample(t *testing.T) {
	m, c := benchmarkFixture(t)
	for _, n := range []int{0, 1, 6} {
		if _, e := m.CreateKunBenchmark(KunBenchmarkInput{Title: "bad", CandidateID: c.ID, ExpectedCandidateHash: c.Hash, Repetitions: n}); e == nil {
			t.Fatal("invalid repeats accepted", n)
		}
	}
	for _, mode := range []string{"drift", "failed", "stop"} {
		t.Run(mode, func(t *testing.T) {
			v, e := m.CreateKunBenchmark(KunBenchmarkInput{Title: mode, CandidateID: c.ID, ExpectedCandidateHash: c.Hash, Repetitions: 2})
			if e != nil {
				t.Fatal(e)
			}
			b := v.Benchmark
			if mode == "stop" {
				v, e = m.StopKunBenchmark(b.ID, b.Hash)
				if e != nil || v.Execution.Status != "stopped" {
					t.Fatal(v, e)
				}
				if len(m.handles) != 0 {
					t.Fatal("stop launched worker")
				}
				return
			}
			if mode == "failed" {
				d, e := m.kunCaseDraft(b.Plans[0].PlanID)
				if e != nil {
					t.Fatal(e)
				}
				s := d.Target
				s.Status = "interrupted"
				s.RunID = d.Preview.RunID
				if e = m.Store.Put("session", s.ID, s); e != nil {
					t.Fatal(e)
				}
				m.mu.Lock()
				m.sessions[s.ID] = &s
				m.mu.Unlock()
				if _, e = m.StartKunBenchmark(b.ID, b.Hash, true); e == nil {
					t.Fatal("uncertain sample replayed")
				}
				return
			}
			w, _ := m.Workspace(c.WorkspaceID)
			if _, e = m.Notes(w.ID, "changed environment", w.Revision); e != nil {
				t.Fatal(e)
			}
			if _, e = m.StartKunBenchmark(b.ID, b.Hash, true); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				v, e = m.KunBenchmark(b.ID)
				if e != nil {
					t.Fatal(e)
				}
				if v.Execution.Status == "blocked" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if v.Execution.Status != "blocked" || v.Execution.Next != 0 || len(m.handles) != 0 {
				t.Fatal("drift dispatched run", v)
			}
		})
	}
}

func TestKunBenchmarkMetricsMissingFallbackAndFailed(t *testing.T) {
	x := int64(25)
	y := int64(5)
	passed := KunCaseScore{Summary: KunCompareSide{Complete: true, ModelCalls: 2, ReportedTokens: 20, TokenUsageComplete: true, ActiveMillis: &x, WaitMillis: &y}, Evaluation: KunEvaluationSide{Status: "pass"}, FastFallback: true}
	var arm KunBenchmarkArm
	arm.add(passed)
	if arm.Passed != 1 || arm.Fallbacks != 1 || *arm.PassRate != 100 || *arm.Metrics.RunMillis.Mean != 30 {
		t.Fatal(arm)
	}
	missing := passed
	missing.FastFallback = false
	missing.Summary.TokenUsageComplete = false
	missing.Summary.ReportedTokens = 999
	missing.Summary.ActiveMillis = nil
	missing.Evaluation = KunEvaluationSide{Status: "fail"}
	arm.add(missing)
	if arm.Passed != 1 || arm.Failed != 1 || *arm.PassRate != 50 || arm.Metrics.ReportedTokens.Count != 1 || arm.Metrics.ReportedTokens.Missing != 1 || arm.Metrics.ReportedTokens.Total != nil || *arm.Metrics.ReportedTokens.Mean != 20 {
		t.Fatal(arm)
	}
	unknown := passed
	unknown.Summary.Complete = false
	unknown.Summary.TokenUsageComplete = false
	unknown.Evaluation = KunEvaluationSide{Status: "fail", Unknown: 1}
	arm.add(unknown)
	if arm.Unknown != 1 || arm.PassRate != nil || arm.Metrics.ModelCalls.Missing != 1 || arm.Metrics.ModelCalls.Total != nil {
		t.Fatal(arm)
	}
	var negative KunBenchmarkMetric
	negative.add(benchmarkNumber(-2, true))
	negative.add(benchmarkNumber(0, true))
	negative.add(nil)
	if *negative.Mean != -1 || *negative.Min != -2 || *negative.Max != 0 || negative.Total != nil {
		t.Fatal(negative)
	}
}

func TestKunBenchmarkWaitsForSharedCapacityAndStop(t *testing.T) {
	m, c := benchmarkFixture(t)
	v, e := m.CreateKunBenchmark(KunBenchmarkInput{Title: "capacity", CandidateID: c.ID, ExpectedCandidateHash: c.Hash, Repetitions: 2})
	if e != nil {
		t.Fatal(e)
	}
	b := v.Benchmark
	// A real occupied slot prevents admission even though this batch has consent.
	busy, e := m.CreateSession(c.WorkspaceID, "other run", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.update(busy.ID, func(s *Session) { s.Status = "running"; s.RunID = "other-run" }); e != nil {
		t.Fatal(e)
	}
	m.queueMu.Lock()
	m.queue.MaxConcurrent = 1
	m.queueMu.Unlock()
	if _, e = m.StartKunBenchmark(b.ID, b.Hash, true); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, e = m.KunBenchmark(b.ID)
		if e != nil {
			t.Fatal(e)
		}
		if v.Execution.Status == "waiting" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v.Execution.Status != "waiting" || v.Execution.Next != 0 {
		t.Fatal(v)
	}
	other, e := m.CreateKunBenchmark(KunBenchmarkInput{Title: "second", CandidateID: c.ID, ExpectedCandidateHash: c.Hash, Repetitions: 2})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.StartKunBenchmark(other.Benchmark.ID, other.Benchmark.Hash, true); e == nil {
		t.Fatal("concurrent benchmark admitted")
	}
	if _, e = m.StopKunBenchmark(b.ID, b.Hash); e != nil {
		t.Fatal(e)
	}
	if e = m.update(busy.ID, func(s *Session) { s.Status = "completed" }); e != nil {
		t.Fatal(e)
	}
	time.Sleep(300 * time.Millisecond)
	for _, plan := range b.Plans {
		x, e := m.KunCaseRun(plan.PlanID)
		if e != nil || (x.Session != nil && x.Session.RunID != "") {
			t.Fatal("stop admitted later work", x, e)
		}
	}
}

func TestKunBenchmarkPreservesLegacySkillReportHashes(t *testing.T) {
	// 0.40 report documents have no fastFallback property. The additive evidence
	// field must not invalidate their stored hashes or application promotion.
	legacy := KunSkillReport{ID: "legacy", Cases: []KunCaseScore{{PlanID: "source", FastCompleted: true}}}
	raw := bytes.ReplaceAll(p.JSON(legacy), []byte(`"fastFallback":false,`), nil)
	expected := forkDigest(json.RawMessage(raw))
	var decoded KunSkillReport
	if e := json.Unmarshal(raw, &decoded); e != nil {
		t.Fatal(e)
	}
	if skillReportHash(decoded) != expected {
		t.Fatal("legacy report hash changed")
	}
}
