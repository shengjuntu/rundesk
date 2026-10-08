package kun

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"time"
)

type budgetError string

func (b budgetError) Error() string { return "Kun budget stopped execution: " + string(b) }

// Caller holds Engine.mu. Durations retain sub-millisecond remainders in the
// clock, so repeated inspection cannot reset or discount an active-time limit.
func (e *Engine) tick(now time.Time) {
	if e.clock.IsZero() {
		e.clock = now
		return
	}
	delta := now.Sub(e.clock)
	if delta < 0 {
		return
	}
	ms := delta.Milliseconds()
	if e.waiting {
		e.state.Budget.WaitMillis += ms
	} else {
		e.state.Budget.ActiveMillis += ms
	}
	e.clock = e.clock.Add(time.Duration(ms) * time.Millisecond)
}
func (e *Engine) setWaiting(wait bool) {
	e.tick(time.Now())
	e.waiting = wait
}
func (e *Engine) checkBudget(next string) error {
	e.tick(time.Now())
	reason := budgetReason(e.state, next)
	if reason != "" {
		e.state.Budget.StopReason = reason
		return budgetError(reason)
	}
	return nil
}
func budgetReason(s p.State, next string) string {
	b, c := s.Budget, s.Config.Budget
	var reason string
	switch {
	case b.ActiveMillis >= int64(c.MaxActiveSeconds)*1000:
		reason = "active_time"
	case b.ConsecutiveFailures >= c.MaxConsecutiveFailures:
		reason = "consecutive_failures"
	case c.MaxTotalTokens > 0 && b.UnreportedModelCalls > 0:
		reason = "token_usage_unknown"
	case c.MaxTotalTokens > 0 && b.ReportedTokens >= c.MaxTotalTokens:
		reason = "reported_token_threshold"
	case next == "model" && s.Step >= s.Config.MaxSteps:
		reason = "model_calls"
	case next == "tool" && b.ToolCalls >= c.MaxToolCalls:
		reason = "tool_calls"
	}
	return reason
}

// Child deadline cancels active network work, but not human approval/debug wait.
func (e *Engine) activeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	e.mu.Lock()
	e.tick(time.Now())
	remaining := time.Duration(int64(e.state.Config.Budget.MaxActiveSeconds)*1000-e.state.Budget.ActiveMillis) * time.Millisecond
	e.mu.Unlock()
	return context.WithTimeout(ctx, remaining)
}
func absorbUsage(b *p.BudgetUsage, raw json.RawMessage) {
	var u struct {
		Total  *int64 `json:"total_tokens"`
		Input  *int64 `json:"prompt_tokens"`
		Output *int64 `json:"completion_tokens"`
	}
	if json.Unmarshal(raw, &u) != nil {
		b.UnreportedModelCalls++
		return
	}
	var total int64
	if u.Total != nil {
		total = *u.Total
	} else if u.Input != nil && u.Output != nil && *u.Input >= 0 && *u.Output >= 0 && *u.Input <= 1_000_000_000 && *u.Output <= 1_000_000_000 {
		total = *u.Input + *u.Output
	} else {
		b.UnreportedModelCalls++
		return
	}
	if total < 0 || total > 2_000_000_000 {
		b.UnreportedModelCalls++
		return
	}
	b.ReportedTokens += total
}
func (e *Engine) finishTool(call p.ToolCall, intent toolIntent, result toolResult, status string, elapsed time.Duration) error {
	e.state.Actions[call.ID] = status
	e.state.Messages = append(e.state.Messages, p.Message{Role: "tool", ToolCallID: call.ID, Content: result.Output})
	e.state.Pending = e.state.Pending[1:]
	e.state.Phase = "after_tool"
	if result.IsError {
		e.state.Budget.ConsecutiveFailures++
	} else {
		e.state.Budget.ConsecutiveFailures = 0
	}
	metadata := map[string]string{}
	if intent.MCP != nil {
		metadata = map[string]string{"server": intent.MCP.Server, "tool": intent.MCP.Name}
	}
	e.moduleState("action", "after_tool", map[string]any{"callId": call.ID, "tool": call.Function.Name, "status": status, "schemaHash": intent.SchemaHash})
	data := map[string]any{"call": call, "output": result.Output, "isError": result.IsError, "status": status, "durationMs": elapsed.Milliseconds(), "step": e.state.Step, "mcp": metadata, "result": result.Raw}
	if result.Replay != nil {
		data["replay"] = result.Replay
	}
	err := e.record("kun/tool.completed", data)
	if e.single {
		e.pause = true
	}
	return err
}
func (e *Engine) prepareCatalog() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	catalog, err := e.modules.capability.Build(clone(e.state))
	if err != nil {
		return err
	}
	if e.expectedCatalog != "" && e.expectedCatalog != catalogFingerprint(e.state) {
		return fmt.Errorf("checkpoint tool catalog changed; resume blocked before execution")
	}
	e.expectedCatalog = ""
	e.state.Phase = e.modules.policy.Next(clone(e.state))
	if e.state.Phase == "complete" {
		e.state.Phase = "after_model"
	}
	e.catalog = catalog
	hashes := map[string]string{}
	for name, t := range catalog.Tools {
		hashes[name] = t.SchemaHash
	}
	e.moduleState("capability", "ready", map[string]any{"toolCount": len(catalog.Tools), "schemaHashes": hashes, "schemaDialect": "bounded-2020-12-subset", "format": "annotation-only"})
	return e.record("kun/capability.ready", e.state.Modules["capability"])
}
func (e *Engine) prepareContext() error {
	copy := clone(e.state)
	ctx, err := e.modules.memory.Build(copy)
	if err != nil {
		return err
	}
	e.state.Messages = ctx.Messages
	e.state.ToolDefinitions = ctx.Tools
	skills := []string{}
	for _, sk := range e.state.Skills {
		skills = append(skills, sk.Hash)
	}
	e.moduleState("memory", "before_model", map[string]any{"messageCount": len(ctx.Messages), "requestBytes": ctx.Bytes, "skillHashes": skills, "compression": "none", "truncated": false})
	e.state.Modules["planning"] = e.modules.planning.Plan(clone(e.state))
	return e.record("kun/context.built", map[string]any{"memory": e.state.Modules["memory"], "planning": e.state.Modules["planning"]})
}
func (e *Engine) activeFailure(err error) error {
	if err == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tick(time.Now())
	if e.state.Budget.ActiveMillis >= int64(e.state.Config.Budget.MaxActiveSeconds)*1000 {
		e.state.Budget.StopReason = "active_time"
		return budgetError("active_time")
	}
	return fmt.Errorf("%w", err)
}
