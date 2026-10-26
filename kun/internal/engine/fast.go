package kun

import (
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/your-org/kun/sdk/validation"
	"strconv"
	"strings"
)

// ValidateFastProgram and FastGuardFor use the same contract as independent hosts.
func ValidateFastProgram(v p.FastProgram) error { return validation.ValidateFastProgram(v) }
func FastGuardFor(s p.State) p.FastGuard        { return validation.FastGuardFor(s) }
func fastPointer(v any, path string) (any, error) {
	if path == "" {
		return v, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("invalid JSON pointer")
	}
	for _, part := range strings.Split(path[1:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 >= len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return nil, fmt.Errorf("invalid pointer escape")
				}
				i++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = x[part]
			if !ok {
				return nil, fmt.Errorf("field missing")
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) || strconv.Itoa(i) != part {
				return nil, fmt.Errorf("array index missing")
			}
			v = x[i]
		default:
			return nil, fmt.Errorf("field not structured")
		}
	}
	return v, nil
}
func fastValue(f *p.FastState, x p.FastValue) (any, error) {
	if x.Ref == "" {
		return strictJSON(x.Literal)
	}
	parts := strings.SplitN(x.Ref, "#", 2)
	raw, ok := f.Values[parts[0]]
	if !ok {
		return nil, fmt.Errorf("value unavailable on this branch")
	}
	v, err := strictJSON(raw)
	if err != nil {
		return nil, err
	}
	if len(parts) == 2 {
		return fastPointer(v, parts[1])
	}
	return v, nil
}
func fastSignature(call p.ToolCall) string {
	args, err := strictJSON([]byte(call.Function.Arguments))
	if err != nil {
		return ""
	}
	return fingerprint([]any{call.Function.Name, args})
}

func (e *Engine) fastFallback(reason string) error {
	f := e.state.Fast
	if f == nil || f.Status != "active" {
		return nil
	}
	f.Status = "fallback"
	f.Reason = reason
	// Tool messages and durable actions stay in place. Never reset the run/ledger.
	e.state.Messages = append(e.state.Messages, p.Message{Role: "system", Content: fmt.Sprintf("Fast path stopped at instruction %d: %s. Continue from the recorded tool results. Completed actions must not be repeated. No success has been inferred.", f.PC, reason)})
	return e.record("kun/fast.fallback", map[string]any{"candidateId": f.CandidateID, "pc": f.PC, "reason": reason, "completed": len(f.Completed)})
}

// Called with Engine.mu held at a normal safe boundary. Tools are queued into
// the ordinary loop; the interpreter cannot call executeAction itself.
func (e *Engine) fastAdvance() error {
	f := e.state.Fast
	if f == nil || f.Status != "active" {
		return nil
	}
	if fingerprint(f.Program.Guard) != fingerprint(FastGuardFor(e.state)) {
		return e.fastFallback("runtime_guard_changed")
	}
	if err := e.checkBudget(""); err != nil {
		return err
	}
	if f.PC == 0 {
		if len(e.state.Messages) != 2 {
			return e.fastFallback("existing_conversation_context")
		}
		schema, _ := compileArguments(f.Program.InputSchema)
		if err := schema.Validate(f.Input); err != nil {
			return e.fastFallback("input_schema_mismatch")
		}
		v, err := strictJSON(f.Input)
		if err != nil {
			return e.fastFallback("input_not_json")
		}
		obj, ok := v.(map[string]any)
		if !ok || obj["domain"] != f.Program.Domain {
			return e.fastFallback("task_domain_mismatch")
		}
	}
	if f.PC >= len(f.Program.Steps) {
		return e.fastFallback("program_exhausted")
	}
	s := f.Program.Steps[f.PC]
	next := f.PC + 1
	fail := func(err error) error { return e.fastFallback("instruction_guard: " + err.Error()) }
	switch s.Op {
	case "tool":
		args := map[string]any{}
		for k, x := range s.Args {
			v, err := fastValue(f, x)
			if err != nil {
				return fail(err)
			}
			args[k] = v
		}
		call := p.ToolCall{ID: fmt.Sprintf("fast-%s-%d", e.state.RunID, f.PC), Type: "function", Function: p.Function{Name: s.Tool, Arguments: string(p.JSON(args))}}
		if _, err := e.modules.action.Validate(call, e.catalog); err != nil {
			return fail(err)
		}
		if _, exists := e.state.Actions[call.ID]; exists {
			return e.fastFallback("action_already_recorded")
		}
		e.state.Pending = []p.ToolCall{call}
		e.state.Actions[call.ID] = "prepared"
		f.PendingStore = s.Store
		e.state.Messages = append(e.state.Messages, p.Message{Role: "assistant", ToolCalls: []p.ToolCall{call}})
	case "extract", "assert", "branch", "finish":
		v, err := fastValue(f, s.Value)
		if err != nil {
			return fail(err)
		}
		switch s.Op {
		case "extract":
			if str, ok := v.(string); ok {
				v, err = strictJSON([]byte(str))
				if err != nil {
					return fail(err)
				}
			}
			v, err = fastPointer(v, s.Pointer)
			if err != nil {
				return fail(err)
			}
			f.Values[s.Store] = p.JSON(v)
			f.Evidence[s.Store] = f.Evidence[strings.SplitN(s.Value.Ref, "#", 2)[0]]
		case "assert":
			schema, _ := compileArguments(s.Schema)
			if err := schema.ValidateValue(v); err != nil {
				return fail(err)
			}
		case "branch":
			var expected any
			expected, err = strictJSON(s.Equals)
			if err != nil {
				return fail(err)
			}
			next = s.Else
			if fingerprint(v) == fingerprint(expected) {
				next = s.Then
			}
		case "finish":
			if !f.Evidence[strings.SplitN(s.Value.Ref, "#", 2)[0]] {
				return e.fastFallback("terminal_without_tool_evidence")
			}
			text, ok := v.(string)
			if !ok {
				text = string(p.JSON(v))
			}
			if strings.TrimSpace(text) == "" || len(text) > 256<<10 {
				return e.fastFallback("terminal_output_invalid")
			}
			msg := p.Message{Role: "assistant", Content: text}
			e.state.Messages = append(e.state.Messages, msg)
			f.Status = "completed"
			e.state.Phase = "after_model"
			f.PC = next
			return e.record("kun/fast.completed", map[string]any{"candidateId": f.CandidateID, "candidateHash": f.CandidateHash, "message": msg, "pc": f.PC})
		}
	}
	f.PC = next
	return e.record("kun/fast.advanced", map[string]any{"candidateId": f.CandidateID, "pc": f.PC, "op": s.Op})
}
func (e *Engine) fastToolFinished(call p.ToolCall, result toolResult, status string) error {
	f := e.state.Fast
	if f == nil || f.Status != "active" || f.PendingStore == "" {
		return nil
	}
	dest := f.PendingStore
	f.PendingStore = ""
	if status != "succeeded" || result.IsError {
		return e.fastFallback("tool_" + status)
	}
	f.Completed[fastSignature(call)] = p.FastResult{CallID: call.ID, Output: "Completed action; inspect prior tool result. Do not repeat."}
	if len(result.Output) > 256<<10 || len(p.JSON(f.Values))+len(result.Output) > 512<<10 {
		return e.fastFallback("tool_output_too_large")
	}
	f.Values[dest] = p.JSON(result.Output)
	f.Evidence[dest] = true
	f.Completed[fastSignature(call)] = p.FastResult{CallID: call.ID, Output: result.Output}
	return nil
}
