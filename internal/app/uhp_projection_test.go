package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func uhpProjectionEvent(id int64, direction, method string, data any) store.Event {
	b, _ := json.Marshal(data)
	return store.Event{ID: id, SessionID: "host-session", Direction: direction, Method: method, Data: b}
}

func uhpProjectionFixture() uhpProjection {
	return newUHPProjection(uhpResponse{ID: "resp_test", CreatedAt: 123, Model: "fixture", Store: true, Metadata: map[string]any{"session_id": "hsesstest"}})
}

func uhpProjectionCodex(id int64, method, turn string, data map[string]any) store.Event {
	data["turnId"] = turn
	return uhpProjectionEvent(id, "in", method, map[string]any{"params": data})
}

func TestUHPProjectionCodexProgressReplayAndArtifacts(t *testing.T) {
	p := uhpProjectionFixture()
	events := []store.Event{
		uhpProjectionCodex(1, "item/completed", "old", map[string]any{"item": map[string]any{"id": "old", "type": "agentMessage", "text": "SECRET OLD TURN"}}),
		uhpProjectionEvent(2, "internal", "run/input", map[string]any{"runId": "run"}),
		uhpProjectionEvent(3, "out", "turn/start", map[string]any{"id": 42, "params": map[string]any{}}),
		// A stale notification before the matching RPC result must not bind the
		// projection to the wrong turn. A notification for the real turn may
		// also precede that response and must be replayed after binding.
		uhpProjectionCodex(4, "item/completed", "old", map[string]any{"item": map[string]any{"id": "old", "type": "agentMessage", "text": "SECRET OLD TURN"}}),
		uhpProjectionCodex(5, "item/started", "turn", map[string]any{"item": map[string]any{"id": "answer", "type": "agentMessage"}}),
		uhpProjectionEvent(6, "in", "", map[string]any{"id": 42, "result": map[string]any{"turn": map[string]any{"id": "turn"}}}),
		uhpProjectionCodex(7, "item/agentMessage/delta", "turn", map[string]any{"itemId": "answer", "delta": "你"}),
		uhpProjectionCodex(8, "item/agentMessage/delta", "turn", map[string]any{"itemId": "answer", "delta": "好"}),
		uhpProjectionCodex(9, "item/completed", "turn", map[string]any{"item": map[string]any{"id": "answer", "type": "agentMessage", "text": "你好，完成。"}}),
		uhpProjectionCodex(10, "item/completed", "old", map[string]any{"item": map[string]any{"id": "old", "type": "agentMessage", "text": "SECRET OLD TURN"}}),
	}
	for _, event := range events[:7] {
		p.Apply(event, "run")
	}
	if p.Response.Status != "in_progress" || len(p.Response.Output) != 1 || !strings.Contains(uhpJSON(p.Events), "response.output_text.delta") {
		t.Fatalf("progress not available before completion: %s", uhpJSON(p))
	}
	// Restart from precisely the durable projection that HTTP polling reads.
	b, _ := json.Marshal(p)
	var restored uhpProjection
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	for _, projection := range []*uhpProjection{&p, &restored} {
		for _, event := range events[6:] { // replay one already-consumed event
			projection.Apply(event, "run")
		}
		projection.AddAnnotations([]map[string]any{{"type": "container_file_citation", "container_id": "cntr_a", "file_id": "file_a", "filename": "a.txt", "download_url": "/v1/containers/cntr_a/files/file_a/content"}})
		projection.Finish("completed", nil)
	}
	if uhpJSON(p) != uhpJSON(restored) {
		t.Fatal("replayed projection differs from uninterrupted projection")
	}
	if strings.Contains(uhpJSON(p), "SECRET OLD TURN") {
		t.Fatal("foreign native turn leaked into UHP")
	}
	var text strings.Builder
	annotationAt, doneAt := -1, -1
	for index, event := range p.Events {
		if uhpString(event["type"]) == "response.output_text.delta" {
			text.WriteString(uhpString(event["delta"]))
		}
		if event["type"] == "response.output_text.annotation.added" {
			annotationAt = index
		}
		if event["type"] == "response.output_item.done" {
			doneAt = index
		}
	}
	if text.String() != "你好，完成。" || annotationAt < 0 || doneAt < annotationAt {
		t.Fatalf("text/annotation ordering: text=%q annotation=%d done=%d", text.String(), annotationAt, doneAt)
	}
	initial := uhpMap(p.Events[0]["response"])
	if initial["status"] != "in_progress" || len(uhpArray(initial["output"])) != 0 {
		t.Fatal("initial response snapshot changed with final output")
	}
	uhpAssertProjectionSequence(t, p)
}

func TestUHPProjectionCancellationRetainsPartialText(t *testing.T) {
	p := uhpProjectionFixture()
	p.NativeTurnID = "turn"
	p.Apply(uhpProjectionEvent(1, "internal", "run/input", map[string]any{"runId": "run"}), "run")
	p.Apply(uhpProjectionCodex(2, "item/agentMessage/delta", "turn", map[string]any{"itemId": "answer", "delta": "partial"}), "run")
	p.Finish("cancelled", &uhpError{Type: "harness_error", Code: "cancelled", Message: "cancelled"})
	if p.Response.Status != "cancelled" || p.Response.Error != nil || p.Response.Usage != nil {
		t.Fatalf("bad cancellation shape: %s", uhpJSON(p.Response))
	}
	if p.Response.Output[0]["status"] != "incomplete" || uhpMap(uhpArray(p.Response.Output[0]["content"])[0])["text"] != "partial" {
		t.Fatalf("partial output lost: %s", uhpJSON(p.Response.Output))
	}
	before := uhpJSON(p)
	p.Finish("completed", nil)
	p.Apply(uhpProjectionCodex(3, "item/agentMessage/delta", "turn", map[string]any{"itemId": "answer", "delta": "too late"}), "run")
	p.AddAnnotations([]map[string]any{{"file_id": "file_late"}})
	if uhpJSON(p) != before {
		t.Fatal("terminal response changed")
	}
	uhpAssertProjectionSequence(t, p)
}

func TestUHPProjectionCodexToolsAndReasoningSummary(t *testing.T) {
	p := uhpProjectionFixture()
	p.NativeTurnID = "turn"
	p.Apply(uhpProjectionEvent(1, "internal", "run/input", map[string]any{"runId": "run"}), "run")
	p.Apply(uhpProjectionCodex(2, "item/started", "turn", map[string]any{"item": map[string]any{"id": "mcp", "type": "mcpToolCall", "server": "inventory", "tool": "list"}}), "run")
	p.Apply(uhpProjectionCodex(3, "item/completed", "turn", map[string]any{"item": map[string]any{"id": "mcp", "type": "mcpToolCall", "server": "inventory", "tool": "list", "arguments": map[string]any{"scope": "project"}, "result": map[string]any{"found": true}}}), "run")
	p.Apply(uhpProjectionCodex(4, "item/reasoning/summaryTextDelta", "turn", map[string]any{"itemId": "reason", "summaryIndex": 0, "delta": "Checking"}), "run")
	p.Apply(uhpProjectionCodex(5, "item/completed", "turn", map[string]any{"item": map[string]any{"id": "reason", "type": "reasoning", "summary": []string{"Checking the files."}, "content": []string{"RAW REASONING MUST STAY PRIVATE"}}}), "run")
	p.Finish("completed", nil)
	if len(p.Response.Output) != 3 || p.ToolCalls != 1 {
		t.Fatalf("unexpected output: %s", uhpJSON(p.Response.Output))
	}
	call, result := p.Response.Output[0], p.Response.Output[1]
	if call["type"] != "function_call" || result["type"] != "function_call_output" || call["call_id"] != result["call_id"] || call["arguments"] != `{"scope":"project"}` {
		t.Fatalf("tool pairing/arguments: %s", uhpJSON(p.Response.Output))
	}
	if strings.Contains(uhpJSON(p), "RAW REASONING") || !strings.Contains(uhpJSON(p.Response.Output), "Checking the files.") {
		t.Fatal("reasoning summary boundary broken")
	}
	uhpAssertProjectionSequence(t, p)
}

func TestUHPProjectionKunNativeRoundUsageAndScope(t *testing.T) {
	p := uhpProjectionFixture()
	apply := func(id int64, run, method string, data any) {
		p.Apply(uhpProjectionEvent(id, "in", method, map[string]any{"runId": run, "sequence": id, "data": data}), "run")
	}
	apply(1, "foreign", "kun/model.completed", map[string]any{"message": map[string]any{"content": "FOREIGN"}})
	apply(2, "run", "kun/model.completed", map[string]any{"step": 1, "purpose": "plan", "usage": map[string]any{"total_tokens": 4, "input_tokens": 3}, "message": map[string]any{"content": "PRIVATE PLAN"}})
	call := map[string]any{"id": "tool", "function": map[string]any{"name": "read_file", "arguments": `{"path":"a.txt"}`}}
	apply(3, "run", "kun/tool.started", map[string]any{"call": call})
	apply(4, "run", "kun/tool.completed", map[string]any{"call": call, "output": "file contents"})
	apply(5, "run", "kun/model.completed", map[string]any{"step": 2, "usage": map[string]any{"total_tokens": 6}, "message": map[string]any{"content": "answer"}})
	p.Finish("completed", nil)
	if p.Steps != 2 || len(p.Response.Output) != 3 || strings.Contains(uhpJSON(p), "FOREIGN") || strings.Contains(uhpJSON(p), "PRIVATE PLAN") {
		t.Fatalf("Kun filtering/steps: %s", uhpJSON(p))
	}
	if uhpJSON(p.Response.Usage) != `{"total_tokens":10}` {
		t.Fatalf("usage must omit counters missing from any call: %s", uhpJSON(p.Response.Usage))
	}
	uhpAssertProjectionSequence(t, p)
}

func TestUHPProjectionMissingUsageIsNotZero(t *testing.T) {
	p := uhpProjectionFixture()
	p.Apply(uhpProjectionEvent(1, "in", "kun/model.completed", map[string]any{"runId": "run", "sequence": 1, "data": map[string]any{"message": map[string]any{"content": "one"}, "usage": map[string]any{"total_tokens": 7}}}), "run")
	p.Apply(uhpProjectionEvent(2, "in", "kun/model.completed", map[string]any{"runId": "run", "sequence": 2, "data": map[string]any{"message": map[string]any{"content": "two"}}}), "run")
	p.Finish("incomplete", nil)
	if p.Response.Usage != nil || p.Response.Error != nil {
		t.Fatal("missing native usage was converted into a partial total or fake zero")
	}
	uhpAssertProjectionSequence(t, p)
}

func uhpAssertProjectionSequence(t *testing.T, p uhpProjection) {
	t.Helper()
	terminal := 0
	added, done := map[string]bool{}, map[string]bool{}
	for index, event := range p.Events {
		if uhpJSON(event["sequence_number"]) != uhpJSON(index) {
			t.Fatalf("sequence gap at %d: %v", index, event["sequence_number"])
		}
		kind := uhpString(event["type"])
		if index == 0 && kind != "response.created" {
			t.Fatal("first event is not response.created")
		}
		id := uhpString(event["item_id"])
		if kind == "response.output_item.added" {
			id = uhpString(uhpMap(event["item"])["id"])
			if added[id] {
				t.Fatalf("duplicate added item %s", id)
			}
			added[id] = true
		} else if kind == "response.output_item.done" {
			id = uhpString(uhpMap(event["item"])["id"])
			if !added[id] || done[id] {
				t.Fatalf("item done without one preceding added: %s", id)
			}
			done[id] = true
		} else if id != "" && (!added[id] || done[id]) {
			t.Fatalf("event outside item lifetime: %s %s", kind, id)
		}
		if kind == "response.completed" || kind == "response.failed" || kind == "response.incomplete" {
			terminal++
			var expected map[string]any
			encoded, _ := json.Marshal(p.Response)
			_ = json.Unmarshal(encoded, &expected)
			if index != len(p.Events)-1 || uhpJSON(event["response"]) != uhpJSON(expected) {
				t.Fatal("terminal snapshot differs from response readback")
			}
		}
	}
	if terminal != 1 || len(added) != len(done) {
		t.Fatalf("terminal/item counts: terminal=%d added=%d done=%d", terminal, len(added), len(done))
	}
}
