package kun

import (
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"strings"
	"unicode/utf8"
)

// Config remains the immutable admission configuration. A recorded override
// changes only the installed composition; no model, tools or permissions change.
func configuredStateModules(s p.State) (modules, error) {
	if err := s.Config.Harness.Validate(); err != nil {
		return modules{}, err
	}
	c := s.Config
	if s.RuntimeHarness != nil {
		c.Harness = *s.RuntimeHarness
	}
	return configuredModules(c)
}

func stateNext(s p.State) string {
	m, err := configuredStateModules(s)
	if err != nil {
		return "incompatible"
	}
	return m.policy.Next(s)
}

func (e *Engine) restoreStateModules(s p.State) error {
	m, err := configuredStateModules(s)
	if err != nil {
		return err
	}
	m.provider = e.modules.provider
	e.modules = m
	return nil
}

// Input still has to match every admitted configuration/environment field.
// The effective Harness is separately checked against the installed registry.
func (e *Engine) manifestForState(in p.Start, s p.State) *p.RunManifest {
	m := e.manifest(in)
	m.HarnessHash = fingerprint(s.Harness)
	return m
}

// Only called under e.mu while the loop is waiting at before_model. The run
// remains paused; the control receipt and new safe checkpoint commit
// in the same journal transaction. Previous snapshots remain immutable.
func (e *Engine) setHarnessLocked(c p.Control) (map[string]any, error) {
	s := e.state
	if s.Fork != nil || s.Diagnostic != nil {
		return nil, fmt.Errorf("runtime Harness changes require an ordinary Kun run")
	}
	if s.Status != "paused" || s.Phase != "before_model" || s.Debug.Pause == nil || s.Debug.Pause.Phase != "before_model" || !e.pause || len(s.Pending) != 0 || s.Approval != nil || len(s.Queued) != 0 || !checkpointSafe(s) || !compatibleHarness(s) {
		return nil, fmt.Errorf("pause before a model request with no pending tools, approvals or controls before changing Harness")
	}
	for _, status := range s.Actions {
		if status == "prepared" || status == "dispatched" || status == "outcome_unknown" {
			return nil, fmt.Errorf("unsettled action prevents Harness change")
		}
	}
	if c.Harness == nil || strings.TrimSpace(c.Reason) == "" || utf8.RuneCountInString(c.Reason) > 2048 || c.Text != "" || c.CallID != "" {
		return nil, fmt.Errorf("set_harness requires a composition and a reason of 1–2048 characters, without tool IDs or steer text")
	}
	h := c.Harness.Normalized()
	cfg := s.Config
	cfg.Harness = h
	m, err := configuredModules(cfg)
	if err != nil {
		return nil, err
	}
	next := m.harness()
	next.Revision = s.Harness.Revision
	if fingerprint(next) == fingerprint(s.Harness) {
		return nil, fmt.Errorf("Harness is already selected")
	}
	next.Revision++
	updated := clone(s)
	updated.RuntimeHarness, updated.Harness = &h, next
	updated.Manifest.HarnessHash = fingerprint(next)
	for _, name := range []string{"memory", "planning"} {
		updated.Modules[name] = p.ModuleState{Implementation: next.Modules[name], Phase: "pending", Data: p.JSON(map[string]any{})}
	}
	if !compatibleHarness(updated) {
		return nil, fmt.Errorf("incompatible Harness migration")
	}
	// Switching to Plan-Act schedules a new planning call only after continue.
	// Limits, spent budgets, context, action ledger and catalog are untouched.
	if reason := budgetReason(updated, "model"); reason != "" {
		return nil, budgetError(reason)
	}
	m.provider = e.modules.provider
	e.state, e.modules = updated, m
	return map[string]any{"previous": s.Harness, "current": next, "phase": s.Phase, "step": s.Step, "reason": c.Reason, "resetModules": []string{"memory", "planning"}, "preservedModules": []string{"action", "capability"}, "planning": "reset; Plan-Act plans after explicit continue", "defaultChanged": false}, nil
}
