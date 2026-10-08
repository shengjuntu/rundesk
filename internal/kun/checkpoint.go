package kun

import (
	"database/sql"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"path/filepath"
	"sort"
)

func checkpointUnsafe(s p.State) bool {
	switch s.Phase {
	case "model", "tool", "mcp_discovery", "restoring":
		return true
	}
	for _, status := range s.Actions {
		if status == "dispatched" || status == "outcome_unknown" {
			return true
		}
	}
	for _, c := range s.Queued {
		if c.Operation != "cancel" {
			return true
		}
	}
	return false
}
func checkpointSafe(s p.State) bool {
	if s.Manifest == nil || s.Modules["capability"].Phase != "ready" || checkpointUnsafe(s) {
		return false
	}
	switch s.Phase {
	case "before_model", "after_model", "before_tool", "after_tool", "approval":
		return true
	}
	return false
}
func (e *Engine) manifest(in p.Start) *p.RunManifest {
	workspace := filepath.Clean(in.Workspace)
	if real, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = real
	}
	// Persist digests only, never the resolved MCP environment/headers or API key.
	return &p.RunManifest{EngineVersion: p.EngineVersion, Workspace: workspace, ConfigHash: fingerprint(in.Config.Normalized()), MCPHash: fingerprint(in.MCP), SkillsHash: fingerprint(in.Skills), HarnessHash: fingerprint(e.modules.harness()), ContextRevision: in.ContextRevision}
}
func catalogFingerprint(s p.State) string {
	// JSON object key order and server tool listing order are not semantic changes.
	definitions := make([]string, 0, len(s.ToolDefinitions))
	for _, raw := range s.ToolDefinitions {
		var v any
		_ = json.Unmarshal(raw, &v)
		definitions = append(definitions, fingerprint(v))
	}
	sort.Strings(definitions)
	tools := make([]string, 0, len(s.MCPTools))
	for _, tool := range s.MCPTools {
		var v any
		_ = json.Unmarshal(p.JSON(tool), &v)
		tools = append(tools, fingerprint(v))
	}
	sort.Strings(tools)
	return fingerprint([][]string{definitions, tools})
}
func (e *Engine) checkpointLocked() (p.CheckpointCheck, p.State, error) {
	check := p.CheckpointCheck{Selection: p.CheckpointSelection{SourceRunID: e.state.RunID, ExpectedRevision: e.state.Revision, WorkerEpoch: e.epoch}, Budget: e.state.Budget, Step: e.state.Step}
	block := func(reason string) (p.CheckpointCheck, p.State, error) {
		check.Reason = reason
		return check, p.State{}, nil
	}
	if e.closed {
		return block("worker_closed")
	}
	if e.state.Status != "interrupted" && e.state.Status != "failed" {
		return block("run_not_stopped")
	}
	if e.done != nil {
		select {
		case <-e.done:
		default:
			return block("run_still_closing")
		}
	}
	if checkpointUnsafe(e.state) {
		return block("inflight_action_or_unapplied_control")
	}
	var raw []byte
	var consumed string
	err := e.j.db.QueryRow("SELECT seq,snapshot,consumed_by FROM checkpoints WHERE run_id=?", e.state.RunID).Scan(&check.Selection.Sequence, &raw, &consumed)
	if err == sql.ErrNoRows {
		return block("no_safe_checkpoint")
	}
	if err != nil {
		return check, p.State{}, err
	}
	if consumed != "" {
		return block("checkpoint_consumed")
	}
	var saved p.State
	if err = json.Unmarshal(raw, &saved); err != nil {
		return check, saved, err
	}
	if !checkpointSafe(saved) || saved.Manifest.EngineVersion != p.EngineVersion || saved.Manifest.HarnessHash != fingerprint(e.modules.harness()) {
		return block("checkpoint_incompatible")
	}
	// The terminal record retains consumed budgets; pausing/restarting never refunds them.
	saved.Budget = e.state.Budget
	saved.Step = e.state.Step
	check.Phase, check.Pending = saved.Phase, len(saved.Pending)
	next := "model"
	if len(saved.Pending) > 0 {
		next = "tool"
	}
	if e.modules.policy.Next(saved) == "complete" {
		next = "complete"
	}
	if reason := budgetReason(saved, next); reason != "" {
		return block("budget_" + reason)
	}
	check.Eligible, check.Reason = true, "safe_checkpoint"
	return check, saved, nil
}
func (e *Engine) Checkpoint() (p.CheckpointCheck, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	check, _, err := e.checkpointLocked()
	return check, err
}
func (e *Engine) CheckpointFor(in p.Start) (p.CheckpointCheck, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	check, saved, err := e.checkpointLocked()
	if err == nil && check.Eligible && (in.SessionID != saved.SessionID || fingerprint(e.manifest(in)) != fingerprint(saved.Manifest) || in.ApprovalPolicy != saved.ApprovalPolicy) {
		check.Eligible = false
		check.Reason = "runtime_manifest_changed"
	}
	return check, err
}
func (e *Engine) resumeLocked(in p.Start, hash string) (p.State, error) {
	check, saved, err := e.checkpointLocked()
	if err != nil {
		return p.State{}, err
	}
	if !check.Eligible {
		return p.State{}, fmt.Errorf("checkpoint resume blocked: %s", check.Reason)
	}
	if *in.Resume != check.Selection {
		return p.State{}, fmt.Errorf("checkpoint selection is stale; inspect again")
	}
	if fingerprint(e.manifest(in)) != fingerprint(saved.Manifest) || in.ApprovalPolicy != saved.ApprovalPolicy {
		return p.State{}, fmt.Errorf("checkpoint runtime manifest changed")
	}
	if in.RunID == saved.RunID {
		return p.State{}, fmt.Errorf("resume needs a new run ID")
	}
	previous := e.state
	e.expectedCatalog = catalogFingerprint(saved)
	saved.RunID, saved.Revision, saved.Status, saved.Phase, saved.Error = in.RunID, e.state.Revision+1, "running", "restoring", ""
	saved.ResumedFrom = in.Resume
	saved.Approval = nil // A prior one-time decision never authorizes a recovered call.
	saved.Queued = nil
	saved.Debug.Pause = nil
	saved.ToolDefinitions = nil
	for _, definition := range toolDefinitions(in.Config.AllowWrite) {
		saved.ToolDefinitions = append(saved.ToolDefinitions, p.JSON(definition))
	}
	saved.MCPTools = nil
	for n := range saved.MCP {
		saved.MCP[n].Status = "pending"
		saved.MCP[n].Error = ""
		saved.MCP[n].ToolCount = 0
	}
	if saved.Actions == nil {
		saved.Actions = map[string]string{}
	}
	e.state = saved
	if _, err = e.j.commit(e.state, "kun/run.started", map[string]any{"resumedFrom": in.Resume, "budget": saved.Budget}, in.RunID, hash, nil); err != nil {
		e.state = previous
		e.expectedCatalog = ""
		return p.State{}, err
	}
	e.launchLocked(in)
	return clone(e.state), nil
}
