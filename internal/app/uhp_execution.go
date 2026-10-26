package app

// UHP execution semantics are explicit and shared by discovery, admission, and
// response metadata. Native model-call/tool-call counters are not tool rounds.
const uhpDefaultTimeoutSeconds = 3600
const uhpMaximumTimeoutSeconds = 86400

func uhpExecutionCapabilities(base string) map[string]bool {
	return map[string]bool{
		"input_text": true, "input_file": true, "input_image": base == "codex",
		"input_history": false, "native_instructions": false,
		"max_step": false, "max_output_tokens": false,
		"timeout_seconds": true, "progress": true,
	}
}

func uhpExecutionSemantics() map[string]any {
	return map[string]any{
		"version":           1,
		"capability_basis":  "adapter_contract_not_model_readiness",
		"max_step":          "unsupported",
		"max_output_tokens": "unsupported",
		"instructions":      map[string]any{"mode": "task_text", "native_priority": false, "retention": "session_history"},
		"timeout_seconds": map[string]any{
			"enforcement": "task_deadline", "includes_queue": true,
			"default": uhpDefaultTimeoutSeconds, "maximum": uhpMaximumTimeoutSeconds,
			"cancellation": "cooperative_no_rollback",
		},
		"progress": "response_metadata_polling",
	}
}

func uhpCheckRequiredCapabilities(req uhpRequest, h uhpHarness) error {
	caps := uhpExecutionCapabilities(h.Base)
	for _, name := range req.RequiredCapabilities {
		if !caps[name] {
			return failure(422, "rundesk_capability_unsupported", "A required execution capability is unavailable for this harness.")
		}
	}
	return nil
}
