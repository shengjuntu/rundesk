package kunproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

// Conditions within a rule are ANDed. Rules are ORed. No expression evaluator.
type Breakpoint struct {
	ID                string `json:"id"`
	Phase             string `json:"phase"`
	Tool              string `json:"tool,omitempty"`
	Model             string `json:"model,omitempty"`
	MinStep           int    `json:"minStep,omitempty"`
	MinToolCalls      int    `json:"minToolCalls,omitempty"`
	MinFailures       int    `json:"minFailures,omitempty"`
	MinReportedTokens int64  `json:"minReportedTokens,omitempty"`
	Once              bool   `json:"once,omitempty"`
}
type DebugPolicy struct {
	Breakpoints         []Breakpoint `json:"breakpoints"`
	PauseTimeoutSeconds int          `json:"pauseTimeoutSeconds"`
}

// Reject unknown condition fields even through the worker protocol. A typo must
// not silently broaden a condition into an unconditional breakpoint.
func (p *DebugPolicy) UnmarshalJSON(raw []byte) error {
	type plain DebugPolicy
	var v plain
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	*p = DebugPolicy(v)
	return nil
}
func (p DebugPolicy) Validate() error {
	if len(p.Breakpoints) > 16 || p.PauseTimeoutSeconds < 0 || p.PauseTimeoutSeconds > 86400 {
		return fmt.Errorf("debug policy allows at most 16 breakpoints and a 0–86400 second pause timeout")
	}
	seen := map[string]bool{}
	for _, b := range p.Breakpoints {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(b.ID) || seen[b.ID] {
			return fmt.Errorf("invalid or duplicate breakpoint ID")
		}
		seen[b.ID] = true
		switch b.Phase {
		case "before_model", "after_model", "before_tool", "after_tool":
		default:
			return fmt.Errorf("unsupported breakpoint phase")
		}
		if b.Tool != "" && b.Phase != "before_tool" && b.Phase != "after_tool" {
			return fmt.Errorf("tool filter requires a tool boundary")
		}
		if len(b.Tool) > 256 || len(b.Model) > 160 || b.MinStep < 0 || b.MinStep > 100 || b.MinToolCalls < 0 || b.MinToolCalls > 3200 || b.MinFailures < 0 || b.MinFailures > 100 || b.MinReportedTokens < 0 || b.MinReportedTokens > 1_000_000_000 {
			return fmt.Errorf("invalid breakpoint condition")
		}
	}
	return nil
}

type DebugPause struct {
	Reason         string   `json:"reason"`
	Phase          string   `json:"phase"`
	RuleIDs        []string `json:"ruleIds,omitempty"`
	CallID         string   `json:"callId,omitempty"`
	PolicyRevision int64    `json:"policyRevision"`
	Deadline       string   `json:"deadline,omitempty"`
}
type DebugState struct {
	Revision int64          `json:"revision"`
	Policy   DebugPolicy    `json:"policy"`
	Hits     map[string]int `json:"hits,omitempty"`
	Pause    *DebugPause    `json:"pause,omitempty"`
}
type DebugQuery struct {
	Kind     string `json:"kind"`
	Sequence int64  `json:"sequence,omitempty"`
}

func (q DebugQuery) Validate() error {
	if q.Sequence < 0 {
		return fmt.Errorf("invalid snapshot sequence")
	}
	switch q.Kind {
	case "run", "context", "tools", "budget", "modules", "breakpoints", "actions":
		return nil
	}
	return fmt.Errorf("unsupported debug query")
}

type DebugResult struct {
	SessionID string          `json:"sessionId"`
	RunID     string          `json:"runId"`
	Revision  int64           `json:"revision"`
	Sequence  int64           `json:"sequence"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
}
