package tracequery

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// Run clocks are never interchangeable. Host admission and a worker's terminal
// timestamp cannot establish a wall interval, even if they parse as RFC3339.
type timingBoundary struct {
	at     string
	id     int64
	method string
}
type clockEvidence struct {
	start, end, last timingBoundary
	input            bool
	ambiguousStart   bool
}
type runTimingEvidence struct{ host, worker clockEvidence }

func (r *runTimingEvidence) observe(id int64, at, method string, d, params map[string]any) {
	c := &r.host
	if strings.HasPrefix(method, "kun/") {
		c = &r.worker
	}
	c.last = timingBoundary{at: at, id: id, method: method}
	start := method == "run/input" || method == "turn/started" || method == "kun/run.started"
	if start {
		if method == "run/input" {
			if c.input {
				c.ambiguousStart = true
			}
			c.start = timingBoundary{at: at, id: id, method: method}
			c.input = true
		} else if c.start.id == 0 {
			c.start = timingBoundary{at: at, id: id, method: method}
		} else if method == "kun/run.started" {
			c.ambiguousStart = true
		}
	}
	status := ""
	switch method {
	case "turn/completed":
		status = str(obj(params["turn"])["status"])
	case "run/state":
		status = str(d["status"])
	case "kun/run.finished":
		status = str(obj(d["data"])["status"])
	}
	if terminal(status) && c.end.id == 0 {
		c.end = timingBoundary{at: at, id: id, method: method}
	}
}

type RunTiming struct {
	RunID             string   `json:"runId"`
	Status            string   `json:"status"`
	Start             string   `json:"start,omitempty"`
	End               string   `json:"end,omitempty"`
	DurationMS        *int64   `json:"durationMs,omitempty"`
	ObservedElapsedMS *int64   `json:"observedElapsedMs,omitempty"`
	Clock             string   `json:"clock"`
	StartMethod       string   `json:"startMethod,omitempty"`
	EndMethod         string   `json:"endMethod,omitempty"`
	StartObserved     bool     `json:"startObserved"`
	EndObserved       bool     `json:"endObserved"`
	Complete          bool     `json:"complete"`
	Issues            []string `json:"issues"`
}
type TypeTiming struct {
	Type                    string `json:"type"`
	StepCount               int    `json:"stepCount"`
	CompleteDurationSteps   int    `json:"completeDurationSteps"`
	WithoutCompleteDuration int    `json:"withoutCompleteDuration"`
	WorkDurationMS          int64  `json:"workDurationMs"`
}
type SlowStep struct {
	ID          string `json:"id"`
	RunID       string `json:"runId"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	DurationMS  int64  `json:"durationMs"`
	Clock       string `json:"clock"`
	Association string `json:"association"`
}
type TimingSummary struct {
	Runs                    []RunTiming  `json:"runs"`
	ByType                  []TypeTiming `json:"byType"`
	SlowestSteps            []SlowStep   `json:"slowestSteps"`
	KnownWorkDurationMS     int64        `json:"knownWorkDurationMs"`
	CompleteDurationSteps   int          `json:"completeDurationSteps"`
	WithoutCompleteDuration int          `json:"withoutCompleteDuration"`
	PointSteps              int          `json:"pointSteps"`
}

func intervalMillis(start, end string) (*int64, bool) {
	a, ae := time.Parse(time.RFC3339Nano, start)
	b, be := time.Parse(time.RFC3339Nano, end)
	if ae != nil || be != nil || b.Before(a) {
		return nil, false
	}
	delta := b.Sub(a)
	if !a.Add(delta).Equal(b) {
		return nil, false // time.Duration overflow is not a measured interval.
	}
	ms := delta.Milliseconds()
	return &ms, true
}
func runTiming(run Run) RunTiming {
	clock, e := "host_journal", run.timing.host
	// Prefer the full host admission interval when it is observed and valid.
	// A worker-only fallback never borrows the host input timestamp.
	_, hostValid := intervalMillis(e.start.at, e.end.at)
	hostComplete := e.input && e.start.id > 0 && e.end.id >= e.start.id && !e.ambiguousStart && hostValid
	if !hostComplete && (run.timing.worker.start.id > 0 || run.timing.worker.end.id > 0) {
		clock, e = "kun_worker", run.timing.worker
	}
	out := RunTiming{RunID: run.ID, Status: run.Status, Start: e.start.at, End: e.end.at, Clock: clock, StartMethod: e.start.method, EndMethod: e.end.method, StartObserved: e.start.id > 0, EndObserved: e.end.id > 0, Issues: []string{}}
	if !out.StartObserved {
		out.Issues = append(out.Issues, "missing_start")
	}
	if !out.EndObserved {
		out.Issues = append(out.Issues, "missing_end")
	}
	if e.ambiguousStart {
		out.Issues = append(out.Issues, "ambiguous_run_start")
	}
	if run.Association == "missing" {
		out.Issues = append(out.Issues, "run_unassigned")
	}
	if out.StartObserved && out.EndObserved {
		if ms, ok := intervalMillis(out.Start, out.End); !ok || e.end.id < e.start.id {
			out.Issues = append(out.Issues, "invalid_clock")
		} else if len(out.Issues) == 0 && terminal(run.Status) {
			out.DurationMS = ms
			out.Complete = true
		}
	} else if out.StartObserved && !terminal(run.Status) && e.last.id >= e.start.id && !e.ambiguousStart && run.Association != "missing" {
		if ms, ok := intervalMillis(out.Start, e.last.at); ok {
			out.ObservedElapsedMS = ms
		} else {
			out.Issues = append(out.Issues, "invalid_clock")
		}
	}
	return out
}

func (r *Reader) timingSummary(found []Step, runID string) TimingSummary {
	out := TimingSummary{Runs: []RunTiming{}, ByType: []TypeTiming{}, SlowestSteps: []SlowStep{}}
	for _, run := range r.runs {
		if runID == "" || run.ID == runID {
			out.Runs = append(out.Runs, runTiming(run))
		}
	}
	byType := map[string]*TypeTiming{}
	for _, step := range found {
		row := byType[step.Type]
		if row == nil {
			row = &TypeTiming{Type: step.Type}
			byType[step.Type] = row
		}
		row.StepCount++
		if step.Point {
			out.PointSteps++
			continue
		}
		if step.DurationMS == nil {
			out.WithoutCompleteDuration++
			row.WithoutCompleteDuration++
			continue
		}
		out.KnownWorkDurationMS += *step.DurationMS
		out.CompleteDurationSteps++
		row.WorkDurationMS += *step.DurationMS
		row.CompleteDurationSteps++
		out.SlowestSteps = append(out.SlowestSteps, SlowStep{ID: step.ID, RunID: step.RunID, Title: step.Title, Type: step.Type, Status: step.Status, DurationMS: *step.DurationMS, Clock: step.Clock, Association: step.Association})
	}
	for _, row := range byType {
		out.ByType = append(out.ByType, *row)
	}
	sort.Slice(out.ByType, func(i, j int) bool {
		if out.ByType[i].WorkDurationMS != out.ByType[j].WorkDurationMS {
			return out.ByType[i].WorkDurationMS > out.ByType[j].WorkDurationMS
		}
		return out.ByType[i].Type < out.ByType[j].Type
	})
	sort.SliceStable(out.SlowestSteps, func(i, j int) bool { return out.SlowestSteps[i].DurationMS > out.SlowestSteps[j].DurationMS })
	if len(out.SlowestSteps) > 10 {
		out.SlowestSteps = out.SlowestSteps[:10]
	}
	return out
}
func validateStepSort(name, value string) error {
	if value == "" {
		return nil
	}
	if name != "trace_find_steps" && name != "trace_find_issues" {
		return errors.New("sort is only supported for runtime steps or issues")
	}
	if value != "recorded" && value != "duration_desc" && value != "duration_asc" {
		return errors.New("unknown runtime step sort")
	}
	return nil
}
func sortSteps(steps []Step, order string) {
	if order == "" || order == "recorded" {
		return
	}
	sort.SliceStable(steps, func(i, j int) bool {
		a, b := steps[i].DurationMS, steps[j].DurationMS
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		if order == "duration_asc" {
			return *a < *b
		}
		return *a > *b
	})
}

// A late native response can establish exact earlier associations, but only
// inside the immutable snapshot and only if no conflicting mapping is retained.
func (r *Reader) preindexTurnAliases(ctx context.Context, p *projection) error {
	rows, err := r.db.QueryContext(ctx, `SELECT
 CASE WHEN json_type(data,'$.runId')='text' AND length(json_extract(data,'$.runId'))<=256 THEN json_extract(data,'$.runId') ELSE '' END,
 CASE WHEN json_type(data,'$.turnId')='text' AND length(json_extract(data,'$.turnId'))<=256 THEN json_extract(data,'$.turnId') ELSE '' END
 FROM events WHERE session=? AND id<=? AND method='run/turn' AND direction='internal' ORDER BY id`, r.SessionID, r.Through)
	if err != nil {
		return err
	}
	defer rows.Close()
	turnRuns, runTurns := map[string]map[string]bool{}, map[string]map[string]bool{}
	for rows.Next() {
		var run, turn string
		if err = rows.Scan(&run, &turn); err != nil {
			return err
		}
		if run == "" || turn == "" {
			continue
		}
		if turnRuns[turn] == nil {
			turnRuns[turn] = map[string]bool{}
		}
		if runTurns[run] == nil {
			runTurns[run] = map[string]bool{}
		}
		turnRuns[turn][run] = true
		runTurns[run][turn] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for turn, runs := range turnRuns {
		for run := range runs {
			if len(runs) != 1 || len(runTurns[run]) != 1 {
				p.aliasConflicts[turn] = true
				break
			}
		}
		if !p.aliasConflicts[turn] {
			for run := range runs {
				p.turns[turn], p.runTurns[run] = run, turn
			}
		}
	}
	return nil
}
