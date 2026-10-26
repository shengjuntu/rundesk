package kunproto

import "fmt"

// HarnessConfig selects an installed, versioned composition. Empty fields use
// the preset defaults; unknown implementations never silently fall back.
type HarnessConfig struct {
	LoopPolicy string `json:"loopPolicy"`
	Memory     string `json:"memory"`
	Planning   string `json:"planning"`
	Action     string `json:"action"`
	Capability string `json:"capability"`
}

func (h HarnessConfig) Normalized() HarnessConfig {
	if h.LoopPolicy == "" {
		h.LoopPolicy = "tool-loop-v1"
	}
	if h.Memory == "" {
		h.Memory = "full-history-v1"
	}
	if h.Action == "" {
		h.Action = "schema-action-v1"
	}
	if h.Capability == "" {
		h.Capability = "fixed-catalog-v1"
	}
	if h.Planning == "" {
		h.Planning = "no-explicit-plan-v1"
		if h.LoopPolicy == "plan-act-v1" {
			h.Planning = "explicit-plan-v1"
		}
	}
	return h
}

func (h HarnessConfig) Validate() error {
	h = h.Normalized()
	if h.Memory != "full-history-v1" || h.Action != "schema-action-v1" || h.Capability != "fixed-catalog-v1" {
		return fmt.Errorf("unsupported Kun module composition")
	}
	if (h.LoopPolicy == "tool-loop-v1" && h.Planning == "no-explicit-plan-v1") ||
		(h.LoopPolicy == "plan-act-v1" && h.Planning == "explicit-plan-v1") {
		return nil
	}
	return fmt.Errorf("unsupported or incompatible Kun LoopPolicy / Planning composition")
}
