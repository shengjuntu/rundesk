package kun

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"time"
)

func breakpointMatches(b p.Breakpoint, s p.State, phase string, call p.ToolCall) bool {
	if b.Phase != phase || (b.Tool != "" && b.Tool != call.Function.Name) || (b.Model != "" && b.Model != s.Config.Model) {
		return false
	}
	step := s.Step
	if phase == "before_model" {
		step++
	}
	return step >= b.MinStep && s.Budget.ToolCalls >= b.MinToolCalls && s.Budget.ConsecutiveFailures >= b.MinFailures && s.Budget.ReportedTokens >= b.MinReportedTokens
}

// Evaluation happens once per boundary visit. Continuing a pause cannot re-hit
// the same visit, and rules never dispatch tools or grant approval.
func (e *Engine) boundary(ctx context.Context, phase string, completed ...p.ToolCall) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	e.state.Phase = phase
	if err := e.applyQueued(phase, false); err != nil {
		e.mu.Unlock()
		return err
	}
	call := p.ToolCall{}
	if phase == "before_tool" && len(e.state.Pending) > 0 {
		call = e.state.Pending[0]
	}
	if phase == "after_tool" && len(completed) > 0 {
		call = completed[0]
	}
	hits := []string{}
	for _, rule := range e.state.Debug.Policy.Breakpoints {
		if rule.Once && e.state.Debug.Hits[rule.ID] > 0 {
			continue
		}
		if breakpointMatches(rule, e.state, phase, call) {
			hits = append(hits, rule.ID)
		}
	}
	before := phase == "before_model" || phase == "before_tool"
	legacy := phase == "before_model" && e.state.Config.PauseBeforeModel
	shouldPause := (e.pause && before) || legacy || len(hits) > 0
	if !shouldPause {
		e.mu.Unlock()
		return ctx.Err()
	}
	reason := "requested"
	if e.single {
		reason = "step"
	}
	if legacy {
		reason = "model_breakpoint"
	}
	if len(hits) > 0 {
		reason = "breakpoint"
	}
	pause := &p.DebugPause{Reason: reason, Phase: phase, RuleIDs: hits, CallID: call.ID, PolicyRevision: e.state.Debug.Revision}
	var deadline time.Time
	if seconds := e.state.Debug.Policy.PauseTimeoutSeconds; seconds > 0 {
		deadline = time.Now().Add(time.Duration(seconds) * time.Second)
		pause.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	}
	if e.state.Debug.Hits == nil {
		e.state.Debug.Hits = map[string]int{}
	}
	for _, id := range hits {
		e.state.Debug.Hits[id]++
	}
	e.state.Debug.Pause = pause
	e.setWaiting(true)
	e.pause = true
	e.state.Status = "paused"
	if err := e.record("kun/run.paused", map[string]any{"phase": phase, "pause": pause}); err != nil {
		e.mu.Unlock()
		return err
	}
	e.mu.Unlock()
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		timeout = timer.C
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.mu.Lock()
		if !e.pause {
			e.state.Debug.Pause = nil
			e.setWaiting(false)
			err := e.applyQueued(phase, false)
			e.mu.Unlock()
			return err
		}
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.wake:
		case <-timeout:
			e.mu.Lock()
			// A concurrent continue that already committed wins over the timer.
			if !e.pause {
				e.mu.Unlock()
				continue
			}
			e.cancel()
			e.mu.Unlock()
			return fmt.Errorf("debug pause timeout at %s; execution stopped without dispatching the next action", phase)
		}
	}
}

// Query projects an immutable state copy and never writes an event, changes the
// revision, uses a model, or invokes a tool. The host applies its normal redactor.
func (e *Engine) Query(q p.DebugQuery) (p.DebugResult, error) {
	if err := q.Validate(); err != nil {
		return p.DebugResult{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s := clone(e.state)
	if q.Sequence > 0 {
		snapshot, err := e.j.snapshot(q.Sequence)
		if err != nil {
			return p.DebugResult{}, err
		}
		s = snapshot.State
	}
	var data any
	switch q.Kind {
	case "run":
		data = map[string]any{"status": s.Status, "phase": s.Phase, "step": s.Step, "error": s.Error, "resumedFrom": s.ResumedFrom, "approval": s.Approval, "pause": s.Debug.Pause, "pendingCount": len(s.Pending)}
	case "context":
		data = map[string]any{"capture": "state_context", "messages": s.Messages, "skills": s.Skills, "toolDefinitions": s.ToolDefinitions}
		if q.Sequence > 0 {
			event, err := e.j.event(q.Sequence)
			if err != nil {
				return p.DebugResult{}, err
			}
			if event.Type == "kun/model.started" {
				var payload struct {
					Request json.RawMessage `json:"request"`
				}
				if err := json.Unmarshal(event.Data, &payload); err != nil {
					return p.DebugResult{}, err
				}
				data = map[string]any{"capture": "model_request", "request": payload.Request, "skills": s.Skills, "evidence": event}
			}
		}
	case "evidence":
		event, err := e.j.event(q.Sequence)
		if err != nil {
			return p.DebugResult{}, err
		}
		data = event
	case "diff":
		from, err := e.j.snapshot(q.FromSequence)
		if err != nil {
			return p.DebugResult{}, err
		}
		diff, err := diffSnapshots(from, p.Snapshot{Sequence: q.Sequence, State: s})
		if err != nil {
			return p.DebugResult{}, err
		}
		data = diff
	case "tools":
		data = map[string]any{"definitions": s.ToolDefinitions, "mcpTools": s.MCPTools, "servers": s.MCP, "approvalPolicy": s.ApprovalPolicy}
	case "budget":
		data = map[string]any{"usage": s.Budget, "limits": s.Config.Budget, "modelCalls": s.Step, "maxModelCalls": s.Config.MaxSteps, "usageAsOfRevision": s.Revision}
	case "modules":
		data = map[string]any{"harness": s.Harness, "modules": s.Modules}
	case "breakpoints":
		data = map[string]any{"debug": s.Debug, "pauseBeforeModel": s.Config.PauseBeforeModel, "queuedControls": s.Queued}
	case "actions":
		data = map[string]any{"actions": s.Actions, "pending": s.Pending}
	}
	return p.DebugResult{SessionID: s.SessionID, RunID: s.RunID, Revision: s.Revision, Sequence: q.Sequence, Kind: q.Kind, Data: p.JSON(data)}, nil
}
