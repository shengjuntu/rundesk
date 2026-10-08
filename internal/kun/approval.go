package kun

import (
	"context"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

// No external action is dispatched before the approval decision is durable.
func (e *Engine) approveMCP(ctx context.Context, tool p.MCPTool, call p.ToolCall) (bool, error) {
	e.mu.Lock()
	if tool.ApprovalMode == "approve" {
		e.mu.Unlock()
		return true, nil
	}
	if e.state.ApprovalPolicy == "never" {
		e.mu.Unlock()
		return false, nil
	}
	e.setWaiting(true)
	e.state.Approval = &p.ToolApproval{CallID: call.ID, Server: tool.Server, Tool: tool.Name, Arguments: call.Function.Arguments}
	e.state.Status = "paused"
	e.state.Phase = "approval"
	err := e.record("kun/approval.requested", e.state.Approval)
	e.mu.Unlock()
	if err != nil {
		return false, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		e.mu.Lock()
		a := e.state.Approval
		if a == nil {
			e.mu.Unlock()
			return false, fmt.Errorf("MCP approval state lost")
		}
		if a.Decision != "" {
			e.setWaiting(false)
			allowed := a.Decision == "approve"
			e.state.Approval = nil
			e.state.Status = "running"
			e.state.Phase = "before_tool"
			e.mu.Unlock()
			return allowed, nil
		}
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-e.wake:
		}
	}
}
