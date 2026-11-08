package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shengjuntu/rundesk/internal/store"
)

// The projection is a durable view of the native journal, not another execution
// engine. HTTP polling and SSE both read this same response and event sequence.
type uhpResponse struct {
	ID                 string           `json:"id"`
	Object             string           `json:"object"`
	CreatedAt          int64            `json:"created_at"`
	Status             string           `json:"status"`
	Error              *uhpError        `json:"error"`
	IncompleteDetails  map[string]any   `json:"incomplete_details"`
	PreviousResponseID *string          `json:"previous_response_id"`
	Model              string           `json:"model"`
	Output             []map[string]any `json:"output"`
	Store              bool             `json:"store"`
	Usage              map[string]any   `json:"usage"`
	Metadata           map[string]any   `json:"metadata"`
}

type uhpProjectedItem struct {
	Index      int      `json:"index"`
	Kind       string   `json:"kind"`
	Text       string   `json:"text,omitempty"`
	Arguments  string   `json:"arguments,omitempty"`
	Summary    []string `json:"summary,omitempty"`
	NativeDone bool     `json:"native_done,omitempty"`
	Done       bool     `json:"done,omitempty"`
}

type uhpProjection struct {
	Response       uhpResponse                  `json:"response"`
	Events         []map[string]any             `json:"events"`
	Items          map[string]*uhpProjectedItem `json:"items"`
	LastSourceID   int64                        `json:"last_source_id"`
	Admitted       bool                         `json:"admitted"`
	Started        bool                         `json:"started"`
	NativeTurnID   string                       `json:"native_turn_id,omitempty"`
	TurnRequestID  string                       `json:"turn_request_id,omitempty"`
	PendingNative  []store.Event                `json:"pending_native,omitempty"`
	Steps          int                          `json:"steps,omitempty"`
	ToolCalls      int                          `json:"tool_calls,omitempty"`
	KunUsage       map[string]int64             `json:"kun_usage,omitempty"`
	KunUsageKnown  bool                         `json:"kun_usage_known,omitempty"`
	KunUsageAbsent bool                         `json:"kun_usage_absent,omitempty"`
}

func newUHPProjection(r uhpResponse) uhpProjection {
	r.Object, r.Status = "response", "in_progress"
	if r.Output == nil {
		r.Output = []map[string]any{}
	}
	if r.Metadata == nil {
		r.Metadata = map[string]any{}
	}
	p := uhpProjection{Response: r, Events: []map[string]any{}, Items: map[string]*uhpProjectedItem{}}
	p.emit("response.created", map[string]any{"response": r})
	return p
}

func (p *uhpProjection) emit(kind string, fields map[string]any) {
	// Snapshots must not share mutable maps with current output. In particular,
	// response.created must stay in_progress after the response completes.
	b, _ := json.Marshal(fields)
	var event map[string]any
	_ = json.Unmarshal(b, &event)
	if event == nil {
		event = map[string]any{}
	}
	event["type"], event["sequence_number"] = kind, len(p.Events)
	p.Events = append(p.Events, event)
}

func (p *uhpProjection) begin() {
	if !p.Started {
		p.Started = true
		p.emit("response.in_progress", map[string]any{"response": p.Response})
	}
}

// Apply consumes a persisted host event. The caller supplies a task's RunID and
// an admission cursor. Duplicate journal reads and foreign runs are harmless.
// Codex notifications are bound to the turn/start RPC response, rather than to
// whichever turn/started notification happened to arrive first.
func (p *uhpProjection) Apply(event store.Event, runID string) {
	if p.Response.Status != "in_progress" || runID == "" || event.ID <= p.LastSourceID {
		return
	}
	p.LastSourceID = event.ID
	var raw map[string]any
	if json.Unmarshal(event.Data, &raw) != nil {
		return
	}
	if event.Method == "run/input" {
		p.Admitted = uhpString(raw["runId"]) == runID
		if p.Admitted {
			p.begin()
		}
		return
	}
	if strings.HasPrefix(event.Method, "kun/") {
		if uhpString(raw["runId"]) == runID {
			p.begin()
			p.kunEvent(event, raw)
		}
		return
	}
	if !p.Admitted {
		return
	}
	if event.Direction == "out" && event.Method == "turn/start" {
		p.TurnRequestID = uhpJSON(raw["id"])
		return
	}
	if event.Direction != "in" {
		return
	}
	if event.Method == "" && p.TurnRequestID != "" && uhpJSON(raw["id"]) == p.TurnRequestID {
		turn := uhpMap(uhpMap(raw["result"])["turn"])
		if id := uhpString(turn["id"]); id != "" {
			p.NativeTurnID = id
		}
	}
	if p.NativeTurnID != "" && len(p.PendingNative) > 0 {
		pending := p.PendingNative
		p.PendingNative = nil
		for _, saved := range pending {
			var v map[string]any
			if json.Unmarshal(saved.Data, &v) == nil {
				p.codexEvent(saved, v)
			}
		}
	}
	if !strings.HasPrefix(event.Method, "item/") && event.Method != "turn/started" {
		return
	}
	if p.NativeTurnID == "" {
		p.PendingNative = append(p.PendingNative, event)
		return
	}
	p.codexEvent(event, raw)
}

func (p *uhpProjection) codexEvent(event store.Event, raw map[string]any) {
	params := uhpMap(raw["params"])
	turnID := uhpString(params["turnId"])
	if turnID == "" && event.Method == "turn/started" {
		turnID = uhpString(uhpMap(params["turn"])["id"])
	}
	if turnID == "" || turnID != p.NativeTurnID {
		return
	}
	item := uhpMap(params["item"])
	id := uhpString(params["itemId"])
	if id == "" {
		id = uhpString(item["id"])
	}
	if id == "" {
		return
	}
	key := "codex:" + id
	switch event.Method {
	case "item/agentMessage/delta":
		state := p.message(key)
		p.textDelta(state, uhpString(params["delta"]))
	case "item/reasoning/summaryTextDelta":
		state := p.reasoning(key)
		index := uhpInt(params["summaryIndex"])
		p.summaryDelta(state, index, uhpString(params["delta"]))
	case "item/reasoning/summaryPartAdded":
		state := p.reasoning(key)
		p.summaryPart(state, uhpInt(params["summaryIndex"]))
	case "item/started", "item/completed":
		done := event.Method == "item/completed"
		switch uhpString(item["type"]) {
		case "agentMessage":
			state := p.message(key)
			if done {
				p.completeText(state, uhpString(item["text"]))
				state.NativeDone = true
			}
		case "reasoning":
			state := p.reasoning(key)
			if done {
				for n, part := range uhpArray(item["summary"]) {
					text := uhpString(part)
					if text == "" {
						text = uhpString(uhpMap(part)["text"])
					}
					p.summaryPart(state, n)
					if n < len(state.Summary) && strings.HasPrefix(text, state.Summary[n]) {
						p.summaryDelta(state, n, strings.TrimPrefix(text, state.Summary[n]))
					}
				}
				p.closeItem(state, "completed")
			}
		default:
			name, args, output, supported := uhpCodexTool(item)
			if !supported {
				return
			}
			state := p.tool(key, name, args)
			if done {
				p.completeArguments(state, args)
				p.closeItem(state, "completed")
				p.toolResult(key, output)
			}
		}
	}
}

func (p *uhpProjection) kunEvent(event store.Event, raw map[string]any) {
	data := uhpMap(raw["data"])
	step := uhpInt(data["step"])
	key := fmt.Sprintf("kun:model:%v", raw["sequence"])
	switch event.Method {
	case "kun/model.completed", "kun/fast.completed":
		if step > p.Steps {
			p.Steps = step
		}
		p.kunUsage(uhpMap(data["usage"]))
		// Kun's planning content is not a public reasoning summary. It stays in
		// the native trace and is never exposed as the assistant's final answer.
		if uhpString(data["purpose"]) == "plan" || uhpString(data["status"]) == "rejected" {
			return
		}
		message := uhpMap(data["message"])
		if text := uhpString(message["content"]); text != "" {
			state := p.message(key)
			p.completeText(state, text)
			state.NativeDone = true
		}
		for _, value := range uhpArray(message["tool_calls"]) {
			call := uhpMap(value)
			function := uhpMap(call["function"])
			if id := uhpString(call["id"]); id != "" {
				state := p.tool("kun:tool:"+id, uhpString(function["name"]), uhpString(function["arguments"]))
				p.completeArguments(state, uhpString(function["arguments"]))
			}
		}
	case "kun/tool.started", "kun/tool.completed":
		call := uhpMap(data["call"])
		id := uhpString(call["id"])
		if id == "" {
			return
		}
		function := uhpMap(call["function"])
		key = "kun:tool:" + id
		state := p.tool(key, uhpString(function["name"]), uhpString(function["arguments"]))
		if event.Method == "kun/tool.completed" {
			p.completeArguments(state, uhpString(function["arguments"]))
			p.closeItem(state, "completed")
			output := uhpString(data["output"])
			if _, ok := data["output"].(string); !ok {
				output = uhpJSON(data["output"])
			}
			p.toolResult(key, output)
		}
	}
}

func (p *uhpProjection) kunUsage(usage map[string]any) {
	if len(usage) == 0 {
		p.KunUsageAbsent = true
		p.Response.Usage = nil
		return
	}
	values := map[string]int64{}
	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens", "cache_read_tokens", "cache_write_tokens"} {
		if value, ok := usage[field].(float64); ok && value >= 0 && value <= 9007199254740991 && value == float64(int64(value)) {
			values[field] = int64(value)
		}
	}
	if !p.KunUsageKnown {
		p.KunUsage = values
	} else {
		// A task counter is known only when every model call reported it.
		for field, old := range p.KunUsage {
			value, exists := values[field]
			if !exists || old > 9007199254740991-value {
				delete(p.KunUsage, field)
			} else {
				p.KunUsage[field] += value
			}
		}
	}
	p.KunUsageKnown = true
	if len(p.KunUsage) == 0 {
		p.KunUsageAbsent = true
	}
	if p.KunUsageKnown && !p.KunUsageAbsent {
		p.Response.Usage = map[string]any{}
		for field, value := range p.KunUsage {
			p.Response.Usage[field] = value
		}
	} else {
		p.Response.Usage = nil
	}
}

func (p *uhpProjection) itemID(prefix, key string) string {
	h := sha256.Sum256([]byte(p.Response.ID + "\x00" + key))
	return prefix + hex.EncodeToString(h[:12])
}

func (p *uhpProjection) add(key, kind, prefix string, fields map[string]any) *uhpProjectedItem {
	if p.Items == nil {
		p.Items = map[string]*uhpProjectedItem{}
	}
	if state := p.Items[key]; state != nil {
		return state
	}
	fields["id"], fields["type"], fields["status"] = p.itemID(prefix, key), kind, "in_progress"
	state := &uhpProjectedItem{Index: len(p.Response.Output), Kind: kind}
	p.Response.Output = append(p.Response.Output, fields)
	p.Items[key] = state
	p.emit("response.output_item.added", map[string]any{"output_index": state.Index, "item": fields})
	return state
}

func (p *uhpProjection) fields(state *uhpProjectedItem) map[string]any {
	return map[string]any{"output_index": state.Index, "item_id": p.Response.Output[state.Index]["id"]}
}

func (p *uhpProjection) message(key string) *uhpProjectedItem {
	if state := p.Items[key]; state != nil {
		return state
	}
	part := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
	state := p.add(key, "message", "msg_", map[string]any{"role": "assistant", "content": []any{part}})
	fields := p.fields(state)
	fields["content_index"], fields["part"] = 0, part
	p.emit("response.content_part.added", fields)
	return state
}

func (p *uhpProjection) textDelta(state *uhpProjectedItem, delta string) {
	if state.Done || delta == "" {
		return
	}
	state.Text += delta
	part := uhpMap(uhpArray(p.Response.Output[state.Index]["content"])[0])
	part["text"] = state.Text
	fields := p.fields(state)
	fields["content_index"], fields["delta"] = 0, delta
	p.emit("response.output_text.delta", fields)
}

func (p *uhpProjection) completeText(state *uhpProjectedItem, text string) {
	if strings.HasPrefix(text, state.Text) {
		p.textDelta(state, strings.TrimPrefix(text, state.Text))
	}
	// A conflicting full-text replay cannot retract bytes already streamed.
	// Retain the observed text consistently instead of giving GET a different
	// result. The native journal remains available for diagnosis through RDCA.
}

func (p *uhpProjection) reasoning(key string) *uhpProjectedItem {
	return p.add(key, "reasoning", "rs_", map[string]any{"summary": []any{}})
}

func (p *uhpProjection) summaryPart(state *uhpProjectedItem, index int) {
	if state.Done || index < 0 || index > 1024 {
		return
	}
	for len(state.Summary) <= index {
		at := len(state.Summary)
		state.Summary = append(state.Summary, "")
		part := map[string]any{"type": "summary_text", "text": ""}
		item := p.Response.Output[state.Index]
		item["summary"] = append(uhpArray(item["summary"]), part)
		fields := p.fields(state)
		fields["summary_index"], fields["part"] = at, part
		p.emit("response.reasoning_summary_part.added", fields)
	}
}

func (p *uhpProjection) summaryDelta(state *uhpProjectedItem, index int, delta string) {
	p.summaryPart(state, index)
	if state.Done || index < 0 || index >= len(state.Summary) || delta == "" {
		return
	}
	state.Summary[index] += delta
	uhpMap(uhpArray(p.Response.Output[state.Index]["summary"])[index])["text"] = state.Summary[index]
	fields := p.fields(state)
	fields["summary_index"], fields["delta"] = index, delta
	p.emit("response.reasoning_summary_text.delta", fields)
}

func (p *uhpProjection) tool(key, name, arguments string) *uhpProjectedItem {
	if state := p.Items[key]; state != nil {
		if name != "" {
			p.Response.Output[state.Index]["name"] = name
		}
		return state
	}
	state := p.add(key, "function_call", "fc_", map[string]any{"name": name, "call_id": p.itemID("call_", key), "arguments": ""})
	p.ToolCalls++
	p.completeArguments(state, arguments)
	return state
}

func (p *uhpProjection) completeArguments(state *uhpProjectedItem, arguments string) {
	if state.Done || arguments == "" || !strings.HasPrefix(arguments, state.Arguments) {
		return
	}
	delta := strings.TrimPrefix(arguments, state.Arguments)
	state.Arguments = arguments
	p.Response.Output[state.Index]["arguments"] = arguments
	if delta != "" {
		fields := p.fields(state)
		fields["delta"] = delta
		p.emit("response.function_call_arguments.delta", fields)
	}
}

func (p *uhpProjection) toolResult(key, output string) {
	resultKey := key + ":result"
	if p.Items[resultKey] != nil {
		return
	}
	state := p.add(resultKey, "function_call_output", "fco_", map[string]any{"call_id": p.itemID("call_", key), "output": output})
	p.closeItem(state, "completed")
}

func (p *uhpProjection) closeItem(state *uhpProjectedItem, status string) {
	if state.Done {
		return
	}
	item := p.Response.Output[state.Index]
	item["status"] = status
	switch state.Kind {
	case "message":
		fields := p.fields(state)
		fields["content_index"], fields["text"] = 0, state.Text
		p.emit("response.output_text.done", fields)
		fields = p.fields(state)
		fields["content_index"], fields["part"] = 0, uhpArray(item["content"])[0]
		p.emit("response.content_part.done", fields)
	case "reasoning":
		for index, part := range uhpArray(item["summary"]) {
			fields := p.fields(state)
			fields["summary_index"], fields["part"] = index, part
			p.emit("response.reasoning_summary_part.done", fields)
		}
	case "function_call":
		fields := p.fields(state)
		fields["arguments"] = state.Arguments
		p.emit("response.function_call_arguments.done", fields)
	}
	state.Done = true
	p.emit("response.output_item.done", map[string]any{"output_index": state.Index, "item": item})
}

// AddAnnotations is called before Finish after the resource service has found
// artifacts. Message completion is delayed until Finish, so annotations never
// arrive after output_item.done for the item they refer to.
func (p *uhpProjection) AddAnnotations(annotations []map[string]any) {
	if p.Response.Status != "in_progress" || len(annotations) == 0 {
		return
	}
	var state *uhpProjectedItem
	for _, candidate := range p.Items {
		if candidate.Kind == "message" && !candidate.Done && (state == nil || candidate.Index > state.Index) {
			state = candidate
		}
	}
	if state == nil {
		state = p.message("artifacts")
		state.NativeDone = true
	}
	part := uhpMap(uhpArray(p.Response.Output[state.Index]["content"])[0])
	existing := uhpArray(part["annotations"])
	for _, annotation := range annotations {
		encoded := uhpJSON(annotation)
		duplicate := false
		for _, old := range existing {
			if uhpJSON(old) == encoded {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		fields := p.fields(state)
		fields["content_index"], fields["annotation_index"], fields["annotation"] = 0, len(existing), annotation
		p.emit("response.output_text.annotation.added", fields)
		existing = append(existing, annotation)
	}
	part["annotations"] = existing
}

func (p *uhpProjection) Finish(status string, err *uhpError) {
	if p.Response.Status != "in_progress" {
		return
	}
	if status != "completed" && status != "failed" && status != "cancelled" && status != "incomplete" {
		status = "failed"
	}
	// Close in output order, not Go map order: replay must yield the same wire
	// sequence after any restart or polling interval.
	byIndex := make([]*uhpProjectedItem, len(p.Response.Output))
	for _, state := range p.Items {
		byIndex[state.Index] = state
	}
	for _, state := range byIndex {
		if state != nil {
			itemStatus := "incomplete"
			if state.NativeDone || state.Done || status == "completed" {
				itemStatus = "completed"
			}
			p.closeItem(state, itemStatus)
		}
	}
	p.Response.Status = status
	if status == "failed" {
		if err == nil {
			err = &uhpError{Type: "harness_error", Code: "harness_error", Message: "The harness could not complete the task."}
		}
		p.Response.Error = err
	} else {
		p.Response.Error = nil
	}
	kind := "response." + status
	if status == "cancelled" {
		kind = "response.failed"
	}
	p.emit(kind, map[string]any{"response": p.Response})
	p.PendingNative = nil
}

func uhpCodexTool(item map[string]any) (name, arguments, output string, supported bool) {
	supported = true
	switch uhpString(item["type"]) {
	case "commandExecution":
		name = "exec_command"
		if item["command"] != nil {
			arguments = uhpJSON(map[string]any{"command": item["command"]})
		}
		output = uhpString(item["aggregatedOutput"])
	case "mcpToolCall":
		name = uhpString(item["server"]) + "." + uhpString(item["tool"])
		arguments = uhpKnownJSON(item["arguments"])
		output = uhpJSON(item["result"])
		if item["error"] != nil {
			output = uhpJSON(map[string]any{"error": item["error"], "result": item["result"]})
		}
	case "dynamicToolCall":
		name = uhpString(item["tool"])
		arguments = uhpKnownJSON(item["arguments"])
		output = uhpJSON(item["contentItems"])
	case "fileChange":
		name, arguments, output = "apply_patch", uhpKnownJSON(item["changes"]), uhpString(item["status"])
	case "webSearch":
		name, arguments, output = "web_search", uhpJSON(map[string]any{"query": item["query"]}), uhpJSON(item["action"])
	case "collabAgentToolCall":
		name = uhpString(item["tool"])
		arguments = uhpJSON(map[string]any{"prompt": item["prompt"], "receiver_thread_ids": item["receiverThreadIds"]})
		output = uhpJSON(item["agentsStates"])
	default:
		supported = false
	}
	return
}

func uhpMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func uhpArray(v any) []any {
	if values, ok := v.([]any); ok {
		return values
	}
	if values, ok := v.([]map[string]any); ok {
		out := make([]any, len(values))
		for index, item := range values {
			out[index] = item
		}
		return out
	}
	return nil
}

func uhpString(v any) string { s, _ := v.(string); return s }
func uhpInt(v any) int       { n, _ := v.(float64); return int(n) }
func uhpJSON(v any) string   { b, _ := json.Marshal(v); return string(b) }

func uhpKnownJSON(v any) string {
	if v == nil {
		return ""
	}
	return uhpJSON(v)
}
