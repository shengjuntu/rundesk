package kunproto

import (
	"encoding/json"
	"fmt"
)

// These are inspectable contracts, not a promise of checkpoint restoration.
type ModuleVersion struct {
	ID                 string `json:"id"`
	Version            string `json:"version"`
	StateSchemaVersion int    `json:"stateSchemaVersion"`
}
type ModuleState struct {
	Implementation ModuleVersion   `json:"implementation"`
	Phase          string          `json:"phase"`
	Data           json.RawMessage `json:"data"`
}
type Harness struct {
	ID       string                   `json:"id"`
	Version  string                   `json:"version"`
	Revision int                      `json:"revision"`
	Modules  map[string]ModuleVersion `json:"modules"`
}
type BudgetLimits struct {
	MaxToolCalls           int   `json:"maxToolCalls"`
	MaxTotalTokens         int64 `json:"maxTotalTokens"`
	MaxActiveSeconds       int   `json:"maxActiveSeconds"`
	MaxConsecutiveFailures int   `json:"maxConsecutiveFailures"`
}

func (b BudgetLimits) Normalized() BudgetLimits {
	if b.MaxToolCalls == 0 {
		b.MaxToolCalls = 64
	}
	if b.MaxActiveSeconds == 0 {
		b.MaxActiveSeconds = 900
	}
	if b.MaxConsecutiveFailures == 0 {
		b.MaxConsecutiveFailures = 3
	}
	return b
}
func (b BudgetLimits) Validate() error {
	b = b.Normalized()
	if b.MaxToolCalls < 1 || b.MaxToolCalls > 3200 || b.MaxTotalTokens < 0 || b.MaxTotalTokens > 1_000_000_000 || b.MaxActiveSeconds < 1 || b.MaxActiveSeconds > 86400 || b.MaxConsecutiveFailures < 1 || b.MaxConsecutiveFailures > 100 {
		return fmt.Errorf("invalid Kun budget limits")
	}
	return nil
}

type BudgetUsage struct {
	ToolCalls            int    `json:"toolCalls"`
	ReportedTokens       int64  `json:"reportedTokens"`
	UnreportedModelCalls int    `json:"unreportedModelCalls"`
	ConsecutiveFailures  int    `json:"consecutiveFailures"`
	ActiveMillis         int64  `json:"activeMillis"`
	WaitMillis           int64  `json:"waitMillis"`
	StopReason           string `json:"stopReason,omitempty"`
}
