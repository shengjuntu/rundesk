package tracequery

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrProjectionLimit = errors.New("trace projection exceeds 20000 lifecycle records, 64 MiB total or 8 MiB per event; choose an earlier through cursor or a smaller source session")

// Deltas are retained in the journal but never mistaken for complete calls.
const lifecycleSQL = `method IN ('run/input','run/steer','turn/started','turn/completed','run/state','item/started','item/completed','approval/pending','approval/resolved','approval/expired','error','warning','configWarning','thread/compacted','kun/run.started','kun/run.paused','kun/run.finished','kun/model.started','kun/model.completed','kun/tool.started','kun/tool.completed','kun/mcp.request','kun/mcp.response','kun/mcp.failed','kun/mcp.ready','kun/approval.requested','kun/control.applied','kun/context.built','kun/capability.ready')`

type projection struct {
	runs         []Run
	steps        []Step
	runIndex     map[string]int
	turns        map[string]string
	runTurns     map[string]string
	open         map[string][]int
	approvalRuns map[string]map[string]bool
	current      string
}

func newProjection() *projection {
	return &projection{runs: []Run{}, steps: []Step{}, runIndex: map[string]int{}, turns: map[string]string{}, runTurns: map[string]string{}, open: map[string][]int{}, approvalRuns: map[string]map[string]bool{}}
}
func shortText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + " [preview truncated]"
	}
	return s
}
func key(parts ...string) string { b, _ := json.Marshal(parts); return string(b) }
func integer(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, e := n.Int64()
	return i, e == nil
}
func numberKey(v any) string {
	i, ok := integer(v)
	if !ok || i < 1 {
		return ""
	}
	return fmt.Sprint(i)
}
func terminal(status string) bool {
	return status == "completed" || status == "succeeded" || status == "stopped" || isFailure(status)
}
func hasError(v any) bool {
	if v == nil || v == false {
		return false
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}
func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
func (p *projection) run(id, at, association string) *Run {
	i, ok := p.runIndex[id]
	if !ok {
		i = len(p.runs)
		p.runIndex[id] = i
		p.runs = append(p.runs, Run{ID: id, Status: "unknown", Start: at, Association: association, EventIDs: []int64{}})
	}
	return &p.runs[i]
}

// Missing turn/run IDs are disclosed as inference or an observed synthetic run.
// An unknown explicit turn is never attached to a different known current turn.
func (p *projection) resolve(id int64, method string, d, params map[string]any) (string, string, string) {
	tid := str(params["turnId"])
	if tid == "" {
		tid = str(obj(params["turn"])["id"])
	}
	rid := str(d["runId"])
	if rid != "" {
		return rid, "observed", tid
	}
	if strings.HasPrefix(method, "kun/") {
		return fmt.Sprint("observed-event-", id), "missing", tid
	}
	if tid != "" {
		if rid = p.turns[tid]; rid != "" {
			return rid, "reconstructed", tid
		}
		if p.current != "" && p.runTurns[p.current] == "" && !terminal(p.runs[p.runIndex[p.current]].Status) && (method == "turn/started" || method == "item/started") {
			return p.current, "inferred", tid
		}
		return "observed-turn-" + tid, "missing", tid
	}
	if method == "approval/resolved" {
		owners := p.approvalRuns[str(d["id"])]
		if len(owners) == 1 {
			for owner := range owners {
				return owner, "reconstructed", ""
			}
		}
		if len(owners) > 1 {
			return fmt.Sprint("observed-event-", id), "missing", ""
		}
	}
	if p.current != "" {
		return p.current, "inferred", ""
	}
	return fmt.Sprint("observed-event-", id), "missing", ""
}

// Preview budgets include object keys, scalar values, depth and element count.
// Inputs are already structurally redacted before this function is called.
func preview(v any, budget *int, depth int, truncated *bool) any {
	if depth > 10 || *budget <= 0 {
		*truncated = true
		return "[preview truncated]"
	}
	switch x := v.(type) {
	case string:
		r := []rune(x)
		n := len(r)
		if n > 2048 {
			n = 2048
		}
		if n > *budget {
			n = *budget
		}
		*budget -= n
		if n < len(r) {
			*truncated = true
			return string(r[:n]) + " [preview truncated]"
		}
		return x
	case map[string]any:
		out := map[string]any{}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			cost := len([]rune(k)) + 1
			if i >= 64 || cost > *budget {
				*truncated = true
				break
			}
			*budget -= cost
			out[k] = preview(x[k], budget, depth+1, truncated)
		}
		return out
	case []any:
		out := []any{}
		for i, v := range x {
			if i >= 64 || *budget <= 0 {
				*truncated = true
				break
			}
			*budget--
			out = append(out, preview(v, budget, depth+1, truncated))
		}
		return out
	default:
		b, _ := json.Marshal(v)
		*budget -= len(b)
		return v
	}
}

func (p *projection) ingest(id int64, at, method string, d map[string]any) {
	params := obj(d["params"])
	rid, association, tid := p.resolve(id, method, d, params)
	if method == "run/input" {
		if str(d["runId"]) != "" {
			rid = str(d["runId"])
			association = "observed"
		}
		p.current = rid
	}
	r := p.run(rid, at, association)
	if tid != "" && p.turns[tid] == "" {
		p.turns[tid] = rid
		p.runTurns[rid] = tid
	}
	r.EventIDs = append(r.EventIDs, id)
	if len(r.EventIDs) > 16 {
		r.EventIDs = append(r.EventIDs[:1], r.EventIDs[len(r.EventIDs)-15:]...)
	}
	if method == "run/input" {
		r.Question = shortText(str(obj(d["input"])["text"]), 2000)
		r.InputEventID = id
		r.Start = at
		r.Status = "running"
		r.Association = association
	}
	data := d
	kun := strings.HasPrefix(method, "kun/")
	if kun {
		data = obj(d["data"])
	}
	if method == "kun/run.started" {
		p.current = rid
		r.Status = "running"
	}
	if method == "turn/started" {
		r.Status = "running"
		return
	}
	if method == "turn/completed" || method == "run/state" || method == "kun/run.finished" {
		status := str(data["status"])
		errValue := data["error"]
		if method == "turn/completed" {
			status = str(obj(params["turn"])["status"])
			errValue = obj(params["turn"])["error"]
		}
		if status == "" {
			status = "unknown"
		}
		r.Status = status
		if terminal(status) {
			r.End = at
		} else {
			r.End = ""
		}
		if !hasError(errValue) {
			return
		}
	}
	if method == "kun/run.paused" {
		r.Status = "paused"
	}
	if method == "kun/approval.requested" {
		r.Status = "waiting"
	}
	if method == "kun/control.applied" {
		command := obj(data["command"])
		operation := str(command["operation"])
		if operation == "resume" || operation == "step" || operation == "approve" || operation == "reject" {
			r.Status = "running"
		}
		if operation == "approve" || operation == "reject" {
			base := key(rid, "kun-approval", str(command["callId"]))
			if matches := p.open[base]; len(matches) == 1 {
				s := &p.steps[matches[0]]
				s.End = at
				s.EndObserved = true
				s.Status = "accepted"
				if operation == "reject" {
					s.Status = "declined"
				}
				p.evidence(s, id, d)
				p.payload(s, data)
				delete(p.open, base)
			}
		}
	}
	kind, title, status := method, method, "recorded"
	base := ""
	phase := "point"
	payload := data
	switch method {
	case "item/started", "item/completed":
		item := obj(params["item"])
		payload = item
		kind = str(item["type"])
		if kind == "" {
			kind = "unknownItem"
		}
		title = kind
		switch kind {
		case "commandExecution":
			title = str(item["command"])
		case "mcpToolCall":
			title = str(item["server"]) + " / " + str(item["tool"])
		case "agentMessage":
			title = "Assistant reply"
		case "reasoning":
			title = "Public reasoning summary"
		}
		if str(item["id"]) != "" && tid != "" {
			base = key(rid, tid, kind, str(item["id"]))
		}
		phase = "start"
		status = "running"
		if method == "item/completed" {
			phase = "end"
			status = str(item["status"])
			if status == "" {
				status = "completed"
			}
			code, ok := integer(item["exitCode"])
			if ok && code != 0 || item["success"] == false || hasError(item["error"]) || obj(item["result"])["isError"] == true {
				status = "failed"
			}
		}
	case "approval/pending", "approval/resolved":
		kind = "approval"
		title = "Approval"
		aid := str(d["id"])
		if aid != "" {
			base = key(rid, "approval", aid)
			if p.approvalRuns[aid] == nil {
				p.approvalRuns[aid] = map[string]bool{}
			}
			p.approvalRuns[aid][rid] = true
		}
		phase = "start"
		status = "pending"
		if method == "approval/resolved" {
			phase = "end"
			status = "unknown"
			decision := str(d["decision"])
			if decision == "accept" || decision == "acceptForSession" || decision == "acceptForTurn" {
				status = "accepted"
			}
			if decision == "decline" || decision == "cancel" {
				status = "declined"
			}
		}
	case "approval/expired":
		kind = "approval/expired"
		title = "Approval expiration recorded; affected IDs unavailable"
		status = "expired"
	case "kun/model.started", "kun/model.completed":
		kind = "modelCall"
		title = "Kun model"
		if step := numberKey(data["step"]); step != "" {
			base = key(rid, "model", step)
		}
		phase = "start"
		status = "running"
		if method == "kun/model.completed" {
			phase = "end"
			status = "completed"
			if hasError(data["error"]) {
				status = "failed"
			}
		}
	case "kun/tool.started", "kun/tool.completed":
		kind = "toolCall"
		call := obj(data["call"])
		title = str(obj(call["function"])["name"])
		if step := numberKey(data["step"]); step != "" && str(call["id"]) != "" {
			base = key(rid, "tool", step, str(call["id"]))
		}
		phase = "start"
		status = "running"
		if method == "kun/tool.completed" {
			phase = "end"
			status = str(data["status"])
			if status == "" {
				status = "completed"
			}
			if data["isError"] == true || hasError(data["error"]) {
				status = "failed"
			}
			if replay := obj(data["replay"]); replay["mode"] == "recorded" && replay["executed"] == false {
				phase, status, kind, title = "point", "replayed", "toolReplay", "Recorded tool result: "+title
			}
		}
	case "kun/mcp.request", "kun/mcp.response":
		kind = "mcpExchange"
		title = str(data["server"]) + " / " + str(data["method"])
		if exchange := numberKey(data["exchangeId"]); exchange != "" && str(data["server"]) != "" {
			base = key(rid, "mcp", str(data["server"]), exchange)
		}
		phase = "start"
		status = "running"
		if method == "kun/mcp.response" {
			phase = "end"
			status = "completed"
			if hasError(data["error"]) || obj(data["result"])["isError"] == true {
				status = "failed"
			}
		}
	case "kun/approval.requested":
		kind = "approval"
		title = "Kun tool approval"
		phase = "start"
		status = "pending"
		if call := str(data["callId"]); call != "" {
			base = key(rid, "kun-approval", call)
		}
	case "error", "kun/mcp.failed":
		status = "failed"
	case "warning", "configWarning":
		status = "warning"
	case "run/state", "turn/completed", "kun/run.finished":
		status = "failed"
	}
	index := -1
	ambiguous := false
	if base != "" && phase == "end" {
		matches := p.open[base]
		if len(matches) == 1 {
			index = matches[0]
			delete(p.open, base)
		} else if len(matches) > 1 {
			ambiguous = true
			for _, i := range matches {
				p.steps[i].Issues = appendUnique(p.steps[i].Issues, "ambiguous_pair")
			}
		}
	}
	if index < 0 {
		index = len(p.steps)
		s := Step{ID: fmt.Sprint("step-", id), RunID: rid, Type: kind, Title: shortText(title, 512), Status: status, Point: phase == "point", PreviewOnly: true, Association: association, Payload: map[string]any{}, EventIDs: []int64{}, Missing: []string{}, Issues: []string{}, Clock: "host journal receive time"}
		if kun {
			s.Clock = "Kun worker event time retained by host"
		}
		if phase != "end" {
			s.Start = at
			s.StartObserved = true
		}
		if ambiguous {
			s.Issues = append(s.Issues, "ambiguous_pair")
		}
		p.steps = append(p.steps, s)
		if base != "" && phase == "start" {
			p.open[base] = append(p.open[base], index)
		}
	}
	s := &p.steps[index]
	s.Status = status
	if title != "" {
		s.Title = shortText(title, 512)
	}
	if phase != "start" {
		s.End = at
		s.EndObserved = true
	}
	if phase != "point" && base == "" {
		s.Issues = appendUnique(s.Issues, "missing_identity")
	}
	if association == "missing" {
		s.Issues = appendUnique(s.Issues, "run_unassigned")
	}
	if kun && phase == "end" {
		if duration, ok := integer(data["durationMs"]); ok && duration >= 0 {
			s.ReportedDurationMS = &duration
		}
	}
	p.evidence(s, id, d)
	p.payload(s, payload)
}
func (p *projection) evidence(s *Step, id int64, d map[string]any) {
	s.EventIDs = append(s.EventIDs, id)
	if len(s.EventIDs) > 16 {
		s.EventIDs = append(s.EventIDs[:1], s.EventIDs[len(s.EventIDs)-15:]...)
		s.EvidenceTruncated = true
	}
	if seq, ok := integer(d["sequence"]); ok && seq > 0 && strings.HasPrefix(s.Clock, "Kun ") {
		s.WorkerSequences = append(s.WorkerSequences, seq)
		if len(s.WorkerSequences) > 16 {
			s.WorkerSequences = append(s.WorkerSequences[:1], s.WorkerSequences[len(s.WorkerSequences)-15:]...)
			s.EvidenceTruncated = true
		}
	}
}
func (p *projection) payload(s *Step, payload map[string]any) {
	combined := map[string]any{}
	for k, v := range s.Payload {
		combined[k] = v
	}
	for k, v := range payload {
		combined[k] = v
	}
	budget := 8192
	s.Payload = obj(preview(combined, &budget, 0, &s.PreviewTruncated))
}
func (p *projection) finish() {
	for i := range p.steps {
		s := &p.steps[i]
		if !s.Point {
			if !s.StartObserved {
				s.Missing = append(s.Missing, "start")
				s.Issues = appendUnique(s.Issues, "missing_start")
			}
			if !s.EndObserved {
				s.Missing = append(s.Missing, "end")
				if r := p.runs[p.runIndex[s.RunID]]; terminal(r.Status) || r.Status == "unknown" {
					s.Status = "unknown"
					s.Issues = appendUnique(s.Issues, "missing_end")
				}
			}
			if s.StartObserved && s.EndObserved {
				a, x := time.Parse(time.RFC3339Nano, s.Start)
				b, y := time.Parse(time.RFC3339Nano, s.End)
				if x == nil && y == nil && !b.Before(a) {
					n := b.Sub(a).Milliseconds()
					s.DurationMS = &n
				} else {
					s.Issues = appendUnique(s.Issues, "invalid_clock")
				}
			}
		}
		if s.Status == "unknown" {
			s.Issues = appendUnique(s.Issues, "status_unknown")
		}
		if isFailure(s.Status) || s.Status == "warning" || s.Status == "pending" {
			s.Issues = appendUnique(s.Issues, s.Status)
		}
	}
}
