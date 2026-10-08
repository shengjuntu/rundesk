package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"strings"
)

func configuredModules(c p.Config) (modules, error) {
	if err := c.Harness.Validate(); err != nil {
		return modules{}, err
	}
	m := defaultModules()
	if c.Harness.Normalized().LoopPolicy == "plan-act-v1" {
		m.policy, m.planning = planAct{}, explicitPlan{}
	}
	return m, nil
}

// A new ordinary run takes the configured default. Runtime overrides belong
// to the current run and survive checkpoints and forks only.
func (e *Engine) nextHarness(c p.Config) p.Harness {
	m, _ := configuredModules(c) // An unsupported candidate produces a nonmatching manifest.
	if m.policy == nil {
		return p.Harness{}
	}
	h := m.harness()
	old := e.state.Harness
	if old.Revision > 0 {
		h.Revision = old.Revision
		if fingerprint(h) != fingerprint(old) {
			h.Revision++
		}
	}
	return h
}

func compatibleHarness(s p.State) bool {
	m, err := configuredStateModules(s)
	if err != nil || s.Harness.Revision < 1 || s.Manifest == nil {
		return false
	}
	h := m.harness()
	h.Revision = s.Harness.Revision
	if fingerprint(h) != fingerprint(s.Harness) || s.Manifest.HarnessHash != fingerprint(h) {
		return false
	}
	if len(s.Modules) != len(h.Modules) {
		return false
	}
	if h.ID == "plan-act-v1" && s.Modules["planning"].Phase == "ready" {
		var plan planData
		if json.Unmarshal(s.Modules["planning"].Data, &plan) != nil || strings.TrimSpace(plan.Plan) == "" || len(plan.Plan) > 32768 || plan.MessageCount < 1 || plan.MessageCount > len(s.Messages) || plan.Step < 1 || plan.Step > s.Step {
			return false
		}
	}
	for name, version := range h.Modules {
		if s.Modules[name].Implementation != version {
			return false
		}
	}
	return true
}

type planAct struct{}

func (planAct) Version() p.ModuleVersion {
	return p.ModuleVersion{ID: "plan-act-v1", Version: "1.0.0", StateSchemaVersion: 1}
}
func (planAct) Next(s p.State) string {
	if len(s.Pending) > 0 {
		return "before_tool"
	}
	if s.Modules["planning"].Phase != "ready" {
		return "before_model"
	}
	return (toolLoop{}).Next(s)
}

type explicitPlan struct{}

func (explicitPlan) Version() p.ModuleVersion {
	return p.ModuleVersion{ID: "explicit-plan-v1", Version: "1.0.0", StateSchemaVersion: 1}
}
func (m explicitPlan) Plan(s p.State) p.ModuleState {
	previous := s.Modules["planning"]
	if previous.Phase == "ready" {
		return previous
	}
	return p.ModuleState{Implementation: m.Version(), Phase: "planning", Data: p.JSON(map[string]any{"status": "planning", "reason": "one explicit plan per ordinary run; no tools in planning"})}
}

type planData struct {
	Status       string `json:"status"`
	Plan         string `json:"plan"`
	Step         int    `json:"step"`
	MessageCount int    `json:"messageCount"`
}

func modelPurpose(s p.State) string {
	if s.Harness.ID == "plan-act-v1" && s.Modules["planning"].Phase != "ready" {
		return "plan"
	}
	return "act"
}

// Called only by the engine while holding its lock. Even a provider ignoring
// tool_choice cannot turn a planning completion into dispatchable actions.
func (e *Engine) acceptPlan(result completion) error {
	if len(result.Message.ToolCalls) != 0 || len(result.Message.Content) > 32768 || len(strings.TrimSpace(result.Message.Content)) == 0 {
		e.moduleState("planning", "rejected", map[string]any{"status": "rejected", "reason": "planning requires nonempty text up to 32768 bytes and no tool calls"})
		return fmt.Errorf("invalid planning completion: tools, empty or oversized plan are not accepted")
	}
	e.moduleState("planning", "ready", planData{Status: "ready", Plan: result.Message.Content, Step: e.state.Step, MessageCount: len(e.state.Messages)})
	return nil
}
