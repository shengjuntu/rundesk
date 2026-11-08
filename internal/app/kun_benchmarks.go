package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

type KunBenchmarkInput struct {
	Title                 string `json:"title"`
	CandidateID           string `json:"candidateId"`
	ExpectedCandidateHash string `json:"expectedCandidateHash"`
	Repetitions           int    `json:"repetitions"`
}
type KunBenchmarkPlan struct {
	PlanID    string `json:"planId"`
	PlanHash  string `json:"planHash"`
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	CaseKey   string `json:"caseKey"`
	Sample    int    `json:"sample"`
	Arm       string `json:"arm"`
}
type KunBenchmark struct {
	Schema          int                `json:"schema"`
	ID              string             `json:"id"`
	Hash            string             `json:"hash"`
	Title           string             `json:"title"`
	CandidateID     string             `json:"candidateId"`
	CandidateHash   string             `json:"candidateHash"`
	CreatedAt       string             `json:"createdAt"`
	Repetitions     int                `json:"repetitions"`
	TaskSet         KunTaskSet         `json:"taskSet"`
	EnvironmentHash string             `json:"environmentHash"`
	Environment     KunCaseRunPreview  `json:"environment"`
	Plans           []KunBenchmarkPlan `json:"plans"`
}
type KunBenchmarkExecution struct {
	Status    string `json:"status"`
	Next      int    `json:"next"`
	Total     int    `json:"total"`
	PlanID    string `json:"planId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}
type KunBenchmarkView struct {
	Benchmark KunBenchmark          `json:"benchmark"`
	Execution KunBenchmarkExecution `json:"execution"`
}

func benchmarkHash(v KunBenchmark) string             { v.Hash = ""; return forkDigest(v) }
func benchmarkReportHash(v KunBenchmarkReport) string { v.Hash = ""; return forkDigest(v) }

func (m *Manager) CreateKunBenchmark(in KunBenchmarkInput) (KunBenchmarkView, error) {
	var out KunBenchmarkView
	if err := experimentTitle(in.Title); err != nil {
		return out, err
	}
	c, err := m.KunSkill(in.CandidateID)
	if err != nil {
		return out, err
	}
	if c.Hash != in.ExpectedCandidateHash {
		return out, failure(409, "benchmark_candidate_changed", "请重新核对候选指纹")
	}
	if in.Repetitions < 2 || in.Repetitions > 5 || len(c.ValidationSet.Cases)*in.Repetitions*2 > 100 {
		return out, failure(400, "benchmark_size", "每例重复 2–5 次，两个执行组总计不超过 100 次")
	}
	b := KunBenchmark{Schema: 1, ID: store.ID(), Title: in.Title, CandidateID: c.ID, CandidateHash: c.Hash, CreatedAt: store.Now(), Repetitions: in.Repetitions, TaskSet: c.ValidationSet, Plans: []KunBenchmarkPlan{}}
	drafts := map[string]json.RawMessage{}
	bytes := 0
	for sample := 1; sample <= in.Repetitions; sample++ {
		for index, task := range c.ValidationSet.Cases {
			arms := []string{"dynamic", "fast"}
			if (sample+index)%2 == 0 {
				arms[0], arms[1] = arms[1], arms[0]
			}
			for _, arm := range arms {
				input := KunCaseRunInput{TaskSetID: c.ValidationSet.ID, ExpectedTaskSetHash: c.ValidationSet.Hash, CaseKey: task.Key, InstanceID: c.InstanceID, WorkspaceID: c.WorkspaceID}
				if arm == "fast" {
					input.CandidateID = c.ID
				}
				d, err := m.prepareKunCaseRun(input)
				if err != nil {
					return out, err
				}
				d.Preview.BenchmarkID = b.ID
				d.Preview.Hash = caseDraftHash(d)
				if len(b.Plans) == 0 {
					b.EnvironmentHash, b.Environment = d.HostConfigHash, d.Preview
				}
				if d.HostConfigHash != b.EnvironmentHash {
					return out, failure(409, "benchmark_environment_changed", "生成预览时环境已变化，请重试")
				}
				pv := d.Preview
				b.Plans = append(b.Plans, KunBenchmarkPlan{PlanID: pv.ID, PlanHash: pv.Hash, SessionID: pv.SessionID, RunID: pv.RunID, CaseKey: task.Key, Sample: sample, Arm: arm})
				raw := p.JSON(d)
				bytes += len(raw)
				if bytes > 8<<20 {
					return out, failure(413, "benchmark_too_large", "对照预览合计超过 8 MiB，请减少案例或重复次数")
				}
				drafts[pv.ID] = raw
			}
		}
	}
	b.Hash = benchmarkHash(b)
	if len(p.JSON(b)) > 1<<20 {
		return out, failure(413, "benchmark_too_large", "对照定义超过 1 MiB")
	}
	if err = m.Store.CreateKunBenchmark(m.ctx, b.ID, b, drafts); err != nil {
		return out, err
	}
	return KunBenchmarkView{Benchmark: b, Execution: KunBenchmarkExecution{Status: "planned", Total: len(b.Plans)}}, nil
}

func (m *Manager) kunBenchmark(id string) (KunBenchmark, error) {
	var b KunBenchmark
	if err := m.Store.Get("kun_benchmark", id, &b); err != nil {
		return b, failure(404, "benchmark_missing", "对照批次不存在")
	}
	if b.ID != id || b.Schema != 1 || b.Hash != benchmarkHash(b) || b.TaskSet.Hash != taskSetHash(b.TaskSet) || b.Repetitions < 2 || b.Repetitions > 5 || len(b.Plans) != 2*b.Repetitions*len(b.TaskSet.Cases) || len(b.Plans) < 8 || len(b.Plans) > 100 {
		return b, failure(409, "benchmark_integrity", "对照定义指纹或结构不一致")
	}
	seen := map[string]bool{}
	for index, plan := range b.Plans {
		pair := index / 2
		expectCase := b.TaskSet.Cases[pair%len(b.TaskSet.Cases)].Key
		expectSample := pair/len(b.TaskSet.Cases) + 1
		expectArm := "dynamic"
		if ((expectSample+pair%len(b.TaskSet.Cases))%2 == 0) != (index%2 == 1) {
			expectArm = "fast"
		}
		if plan.CaseKey != expectCase || plan.Sample != expectSample || plan.Arm != expectArm || plan.PlanID == "" || seen[plan.PlanID] || seen[plan.SessionID] || seen[plan.RunID] {
			return b, failure(409, "benchmark_binding", "对照执行顺序或案例身份不一致")
		}
		seen[plan.PlanID], seen[plan.SessionID], seen[plan.RunID] = true, true, true
	}
	return b, nil
}

func (m *Manager) benchmarkPlan(b KunBenchmark, plan KunBenchmarkPlan) (KunCaseRunView, error) {
	d, err := m.kunCaseDraft(plan.PlanID)
	if err != nil {
		return KunCaseRunView{}, err
	}
	pv := d.Preview
	candidate, candidateHash := "", ""
	if plan.Arm == "fast" {
		candidate, candidateHash = b.CandidateID, b.CandidateHash
	}
	if pv.Hash != plan.PlanHash || pv.BenchmarkID != b.ID || pv.SessionID != plan.SessionID || pv.RunID != plan.RunID || pv.Case.Key != plan.CaseKey || pv.TaskSetHash != b.TaskSet.Hash || pv.TaskSetID != b.TaskSet.ID || pv.CandidateID != candidate || pv.CandidateHash != candidateHash || d.HostConfigHash != b.EnvironmentHash {
		return KunCaseRunView{}, failure(409, "benchmark_plan_changed", "固定对照案例绑定不一致")
	}
	return m.KunCaseRun(plan.PlanID)
}

// kunBenchmarkMu held. An absent in-memory scheduler means a prior host died;
// reporting this never resumes work as a side effect of GET.
func (m *Manager) benchmarkExecution(b KunBenchmark) (KunBenchmarkExecution, error) {
	x := KunBenchmarkExecution{Status: "planned", Total: len(b.Plans)}
	err := m.Store.Get("kun_benchmark_execution", b.ID, &x)
	if errors.Is(err, sql.ErrNoRows) {
		return x, nil
	}
	if err != nil {
		return x, err
	}
	if (x.Status == "running" || x.Status == "waiting") && m.kunBenchmarkActive != b.ID {
		x.Status = "interrupted"
	}
	return x, nil
}
func (m *Manager) KunBenchmark(id string) (KunBenchmarkView, error) {
	b, err := m.kunBenchmark(id)
	if err != nil {
		return KunBenchmarkView{}, err
	}
	m.kunBenchmarkMu.Lock()
	defer m.kunBenchmarkMu.Unlock()
	x, err := m.benchmarkExecution(b)
	return KunBenchmarkView{Benchmark: b, Execution: x}, err
}
func (m *Manager) StartKunBenchmark(id, expected string, confirmed bool) (KunBenchmarkView, error) {
	b, err := m.kunBenchmark(id)
	if err != nil {
		return KunBenchmarkView{}, err
	}
	if b.Hash != expected || !confirmed {
		return KunBenchmarkView{}, failure(409, "benchmark_confirmation", "需要核对固定批次并确认全部重复运行会执行真实工具")
	}
	m.kunBenchmarkMu.Lock()
	defer m.kunBenchmarkMu.Unlock()
	x, err := m.benchmarkExecution(b)
	if err != nil {
		return KunBenchmarkView{}, err
	}
	if m.kunBenchmarkActive == id || x.Status == "completed" {
		return KunBenchmarkView{Benchmark: b, Execution: x}, nil
	}
	if m.kunBenchmarkActive != "" {
		return KunBenchmarkView{}, failure(409, "benchmark_busy", "已有对照批次在执行，请等待或停止该批次")
	}
	if m.ctx.Err() != nil {
		return KunBenchmarkView{}, m.ctx.Err()
	}
	// Reconcile every fixed identity before scheduling anything. A failed or
	// uncertain earlier sample must never cause a fresh duplicate execution.
	for _, plan := range b.Plans {
		v, err := m.benchmarkPlan(b, plan)
		if err != nil {
			return KunBenchmarkView{}, err
		}
		if v.Status == "deleted" || (v.Session != nil && v.Session.RunID != "" && !active(v.Status) && v.Status != "completed") {
			return KunBenchmarkView{}, failure(409, "benchmark_run_unsettled", "批次包含失败、中断或已删除案例；请检查账本，不能重跑此批次")
		}
	}
	x = KunBenchmarkExecution{Status: "running", Total: len(b.Plans), UpdatedAt: store.Now()}
	if err = m.Store.Put("kun_benchmark_execution", id, x); err != nil {
		return KunBenchmarkView{}, err
	}
	m.kunBenchmarkActive = id
	m.wg.Add(1)
	go m.runKunBenchmark(b)
	return KunBenchmarkView{Benchmark: b, Execution: x}, nil
}
func (m *Manager) StopKunBenchmark(id, expected string) (KunBenchmarkView, error) {
	b, err := m.kunBenchmark(id)
	if err != nil {
		return KunBenchmarkView{}, err
	}
	if b.Hash != expected {
		return KunBenchmarkView{}, failure(409, "benchmark_changed", "对照指纹不一致")
	}
	m.kunBenchmarkMu.Lock()
	defer m.kunBenchmarkMu.Unlock()
	x, err := m.benchmarkExecution(b)
	if err != nil {
		return KunBenchmarkView{}, err
	}
	if x.Status != "completed" {
		x.Status, x.UpdatedAt = "stopped", store.Now()
		err = m.Store.Put("kun_benchmark_execution", id, x)
	}
	return KunBenchmarkView{Benchmark: b, Execution: x}, err
}
func (m *Manager) runKunBenchmark(b KunBenchmark) {
	defer m.wg.Done()
	defer func() {
		m.kunBenchmarkMu.Lock()
		if m.kunBenchmarkActive == b.ID {
			m.kunBenchmarkActive = ""
		}
		m.kunBenchmarkMu.Unlock()
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if m.benchmarkTick(b) {
			return
		}
		select {
		case <-m.ctx.Done():
			m.benchmarkTick(b)
			return
		case <-ticker.C:
		}
	}
}

// Serialize the final admission with stop. Only one new run can be admitted per
// tick; later samples wait for completion, approvals and shared capacity.
func (m *Manager) benchmarkTick(b KunBenchmark) bool {
	m.kunBenchmarkMu.Lock()
	defer m.kunBenchmarkMu.Unlock()
	x, err := m.benchmarkExecution(b)
	if err != nil || (x.Status != "running" && x.Status != "waiting") {
		return true
	}
	save := func(status, message string) bool {
		x.Status, x.Error, x.UpdatedAt = status, message, store.Now()
		return m.Store.Put("kun_benchmark_execution", b.ID, x) != nil || (status != "running" && status != "waiting")
	}
	if m.ctx.Err() != nil {
		return save("interrupted", "宿主已停止；不会自动重跑已接收的案例")
	}
	for index, plan := range b.Plans {
		x.Next, x.PlanID, x.SessionID = index, plan.PlanID, plan.SessionID
		v, err := m.benchmarkPlan(b, plan)
		if err != nil {
			return save("blocked", err.Error())
		}
		if v.Status == "completed" {
			// Completed samples have no future turns. Release only this batch's
			// worker, preserving disk evidence and the shared process ceiling.
			m.releaseTaskConnection(Task{SessionID: plan.SessionID, RunID: plan.RunID})
			continue
		}
		if v.Status == "deleted" {
			return save("blocked", "案例会话已删除")
		}
		if v.Session != nil && v.Session.RunID != "" {
			if !active(v.Status) {
				return save("blocked", "案例未成功结束；请检查账本，不自动继续或重跑")
			}
			return save("running", "")
		}
		_, err = m.startKunCaseRun(plan.PlanID, plan.PlanHash, true, b.ID)
		if err != nil {
			var ae *apiError
			if errors.As(err, &ae) && (ae.Code == "run_capacity" || ae.Code == "connection_limit") {
				return save("waiting", err.Error())
			}
			return save("blocked", err.Error())
		}
		return save("running", "")
	}
	x.Next, x.PlanID, x.SessionID = len(b.Plans), "", ""
	return save("completed", "")
}

type KunBenchmarkMetric struct {
	Count   int      `json:"count"`
	Missing int      `json:"missing"`
	Total   *float64 `json:"total"`
	Mean    *float64 `json:"mean"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
}
type KunBenchmarkMetrics struct {
	ModelCalls     KunBenchmarkMetric `json:"modelCalls"`
	ReportedTokens KunBenchmarkMetric `json:"reportedTokens"`
	RunMillis      KunBenchmarkMetric `json:"runMillis"`
}
type KunBenchmarkArm struct {
	Total         int                 `json:"total"`
	Passed        int                 `json:"passed"`
	Failed        int                 `json:"failed"`
	Unknown       int                 `json:"unknown"`
	PassRate      *float64            `json:"passRate"`
	FastCompleted int                 `json:"fastCompleted"`
	Fallbacks     int                 `json:"fallbacks"`
	Metrics       KunBenchmarkMetrics `json:"metrics"`
}
type KunBenchmarkResult struct {
	Plan  KunBenchmarkPlan `json:"plan"`
	Score KunCaseScore     `json:"score"`
}
type KunBenchmarkPair struct {
	CaseKey       string `json:"caseKey"`
	Sample        int    `json:"sample"`
	DynamicPlanID string `json:"dynamicPlanId"`
	FastPlanID    string `json:"fastPlanId"`
	Outcome       string `json:"outcome"`
}
type KunBenchmarkReport struct {
	Schema        int                  `json:"schema"`
	ID            string               `json:"id"`
	Hash          string               `json:"hash"`
	CreatedAt     string               `json:"createdAt"`
	BenchmarkID   string               `json:"benchmarkId"`
	BenchmarkHash string               `json:"benchmarkHash"`
	CandidateID   string               `json:"candidateId"`
	CandidateHash string               `json:"candidateHash"`
	Status        string               `json:"status"`
	Dynamic       KunBenchmarkArm      `json:"dynamic"`
	Fast          KunBenchmarkArm      `json:"fast"`
	PairedDelta   KunBenchmarkMetrics  `json:"pairedDelta"`
	Pairs         []KunBenchmarkPair   `json:"pairs"`
	Results       []KunBenchmarkResult `json:"results"`
	Warnings      []string             `json:"warnings"`
}

func benchmarkNumber(v int64, known bool) *float64 {
	if !known {
		return nil
	}
	n := float64(v)
	return &n
}
func (v *KunBenchmarkMetric) add(n *float64) {
	if n == nil {
		v.Missing++
		v.Total = nil
		return
	}
	if v.Count == 0 {
		low, high, mean := *n, *n, *n
		v.Min, v.Max, v.Mean = &low, &high, &mean
	} else {
		*v.Mean = (*v.Mean*float64(v.Count) + *n) / float64(v.Count+1)
		if *n < *v.Min {
			*v.Min = *n
		}
		if *n > *v.Max {
			*v.Max = *n
		}
	}
	v.Count++
	if v.Missing == 0 {
		total := *v.Mean * float64(v.Count)
		v.Total = &total
	}
}
func benchmarkValues(s KunCaseScore) [3]*float64 {
	v := [3]*float64{benchmarkNumber(int64(s.Summary.ModelCalls), s.Summary.Complete), benchmarkNumber(s.Summary.ReportedTokens, s.Summary.TokenUsageComplete), nil}
	if s.Summary.Complete && s.Summary.ActiveMillis != nil && s.Summary.WaitMillis != nil {
		v[2] = benchmarkNumber(*s.Summary.ActiveMillis+*s.Summary.WaitMillis, true)
	}
	return v
}
func (m *KunBenchmarkMetrics) add(v [3]*float64) {
	m.ModelCalls.add(v[0])
	m.ReportedTokens.add(v[1])
	m.RunMillis.add(v[2])
}
func benchmarkOutcome(s KunCaseScore) string {
	if !s.Summary.Complete || s.Evaluation.Unknown > 0 {
		return "unknown"
	}
	if s.Evaluation.Status == "pass" {
		return "pass"
	}
	return "fail"
}
func (a *KunBenchmarkArm) add(s KunCaseScore) {
	a.Total++
	switch benchmarkOutcome(s) {
	case "pass":
		a.Passed++
	case "fail":
		a.Failed++
	default:
		a.Unknown++
	}
	if s.FastCompleted {
		a.FastCompleted++
	}
	if s.FastFallback {
		a.Fallbacks++
	}
	a.Metrics.add(benchmarkValues(s))
	if a.Unknown == 0 {
		a.PassRate = benchmarkNumber(int64(a.Passed), true)
		*a.PassRate *= 100 / float64(a.Total)
	} else {
		a.PassRate = nil
	}
}
func (m *Manager) EvaluateKunBenchmark(ctx context.Context, id, expected string) (KunBenchmarkReport, error) {
	var out KunBenchmarkReport
	b, err := m.kunBenchmark(id)
	if err != nil {
		return out, err
	}
	if b.Hash != expected {
		return out, failure(409, "benchmark_changed", "对照指纹不一致")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out = KunBenchmarkReport{Schema: 1, ID: store.ID(), CreatedAt: store.Now(), BenchmarkID: b.ID, BenchmarkHash: b.Hash, CandidateID: b.CandidateID, CandidateHash: b.CandidateHash, Status: "complete", Pairs: []KunBenchmarkPair{}, Results: []KunBenchmarkResult{}, Warnings: []string{
		"描述性重复对照：同一固定保留集、交替先后顺序、独立会话；不构成随机试验、统计显著性或新的盲测。",
		"共享模式在原工作区执行；独立评测模式按每个案例预先固定的文件初始化新目录，仅支持内置文件工具；不隔离模型服务、操作系统或外部编辑器。",
		"质量按预先固定的回复与可选产物规则判定；快速路径回退后成功也计为通过，并单独统计回退；此报告不能用于启用候选。",
		"缺失计量不按零。均值和范围仅覆盖 count 个已知观测；total 仅在全部已知时给出。配对差值为 fast − dynamic，只纳入两侧都有记录的配对。",
		"运行毫秒为引擎活动加等待时间，不含宿主排队和进程启动；token 仅覆盖已报告的模型用量，不是货币费用，也不含外部工具账单。",
	}}
	for index, plan := range b.Plans {
		if _, err = m.benchmarkPlan(b, plan); err != nil {
			return out, err
		}
		score, err := m.KunCaseScore(ctx, plan.PlanID)
		if err != nil {
			return out, err
		}
		out.Results = append(out.Results, KunBenchmarkResult{Plan: plan, Score: score})
		if plan.Arm == "dynamic" {
			out.Dynamic.add(score)
		} else {
			out.Fast.add(score)
		}
		if index%2 == 0 {
			continue
		}
		left, right := out.Results[index-1], out.Results[index]
		if left.Plan.Arm != "dynamic" {
			left, right = right, left
		}
		outcome := "unknown"
		l, r := benchmarkOutcome(left.Score), benchmarkOutcome(right.Score)
		if l != "unknown" && r != "unknown" {
			outcome = "both_fail"
			if l == "pass" && r == "pass" {
				outcome = "both_pass"
			} else if r == "pass" {
				outcome = "fast_only_pass"
			} else if l == "pass" {
				outcome = "dynamic_only_pass"
			}
		}
		out.Pairs = append(out.Pairs, KunBenchmarkPair{CaseKey: plan.CaseKey, Sample: plan.Sample, DynamicPlanID: left.Plan.PlanID, FastPlanID: right.Plan.PlanID, Outcome: outcome})
		a, z := benchmarkValues(left.Score), benchmarkValues(right.Score)
		delta := [3]*float64{}
		for n := range delta {
			if a[n] != nil && z[n] != nil {
				value := *z[n] - *a[n]
				delta[n] = &value
			}
		}
		out.PairedDelta.add(delta)
	}
	if out.Dynamic.Unknown+out.Fast.Unknown > 0 {
		out.Status = "partial"
	}
	out.Hash = benchmarkReportHash(out)
	if len(p.JSON(out)) > 4<<20 {
		return out, failure(413, "benchmark_report_too_large", "对照报告超过 4 MiB")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return out, m.Store.Put("kun_benchmark_report", out.ID, out)
}
func (m *Manager) KunBenchmarkReport(id string) (KunBenchmarkReport, error) {
	var v KunBenchmarkReport
	if err := m.Store.Get("kun_benchmark_report", id, &v); err != nil {
		return v, failure(404, "benchmark_report_missing", "对照报告不存在")
	}
	if v.ID != id || v.Schema != 1 || v.Hash != benchmarkReportHash(v) {
		return v, failure(409, "benchmark_report_integrity", "对照报告指纹不一致")
	}
	return v, nil
}

func (s *Server) kunBenchmarkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kun-benchmarks", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in KunBenchmarkInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.CreateKunBenchmark(in)
		debugRespond(w, v, e)
	})
	for _, entry := range []struct{ Path, Kind, Parent string }{{"/api/kun-benchmarks", "kun_benchmark", "candidateId"}, {"/api/kun-benchmark-reports", "kun_benchmark_report", "benchmarkId"}} {
		entry := entry
		mux.HandleFunc("GET "+entry.Path, func(w http.ResponseWriter, r *http.Request) {
			q, e := experimentQuery(r, "offset", "limit", entry.Parent)
			if e != nil {
				respond(w, nil, e)
				return
			}
			if q.Get(entry.Parent) == "" {
				respond(w, nil, failure(400, "benchmark_parent_required", "需要批次或候选 ID"))
				return
			}
			offset, limit, e := experimentPage(q, 50)
			if e != nil {
				respond(w, nil, experimentPageError(e))
				return
			}
			items, e := s.Manager.Store.KunBenchmarkList(r.Context(), entry.Kind, q.Get(entry.Parent), offset, limit)
			if e != nil {
				respond(w, nil, e)
				return
			}
			more := len(items) > limit
			if more {
				items = items[:limit]
			}
			debugRespond(w, map[string]any{"items": items, "hasMore": more, "nextOffset": offset + len(items)}, nil)
		})
	}
	mux.HandleFunc("GET /api/kun-benchmarks/{bid}", func(w http.ResponseWriter, r *http.Request) {
		if _, e := experimentQuery(r); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Manager.KunBenchmark(r.PathValue("bid"))
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/kun-benchmark-reports/{rid}", func(w http.ResponseWriter, r *http.Request) {
		if _, e := experimentQuery(r); e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Manager.KunBenchmarkReport(r.PathValue("rid"))
		debugRespond(w, v, e)
	})
	for _, action := range []string{"start", "stop", "evaluate"} {
		action := action
		mux.HandleFunc("POST /api/kun-benchmarks/{bid}/"+action, func(w http.ResponseWriter, r *http.Request) {
			if _, e := experimentQuery(r); e != nil {
				respond(w, nil, e)
				return
			}
			// Keep stop/evaluate strict: they do not accept execution consent.
			var hash string
			var confirmed bool
			if action == "start" {
				var in struct {
					ExpectedHash   string `json:"expectedHash"`
					ConfirmExecute bool   `json:"confirmExecute"`
				}
				if !decode(w, r, &in) {
					return
				}
				hash, confirmed = in.ExpectedHash, in.ConfirmExecute
			} else {
				var in struct {
					ExpectedHash string `json:"expectedHash"`
				}
				if !decode(w, r, &in) {
					return
				}
				hash = in.ExpectedHash
			}
			var v any
			var e error
			switch action {
			case "start":
				v, e = s.Manager.StartKunBenchmark(r.PathValue("bid"), hash, confirmed)
			case "stop":
				v, e = s.Manager.StopKunBenchmark(r.PathValue("bid"), hash)
			default:
				v, e = s.Manager.EvaluateKunBenchmark(r.Context(), r.PathValue("bid"), hash)
			}
			debugRespond(w, v, e)
		})
	}
}
