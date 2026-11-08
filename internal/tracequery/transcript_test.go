package tracequery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/observation"
)

func readTranscript(t *testing.T, r *Reader, a TranscriptArgs) TranscriptPage {
	t.Helper()
	v, e := r.Transcript(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func entryText(e TranscriptEntry) string {
	var b strings.Builder
	for _, v := range e.Blocks {
		b.WriteString(v.Text)
	}
	return b.String()
}
func TestTranscriptCodexReadingAndEvidence(t *testing.T) {
	j, path := unifiedFixture(t)
	input := unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "taskId": "task", "input": map[string]any{"text": "修复测试", "files": []string{"/work/readme.md"}}})
	unifiedAdd(t, j, "turn/started", "in", map[string]any{"params": map[string]any{"turn": map[string]any{"id": "t"}}})
	item := func(method, id, kind string, extra map[string]any) int64 {
		v := map[string]any{"id": id, "type": kind}
		for k, x := range extra {
			v[k] = x
		}
		return unifiedAdd(t, j, method, "in", map[string]any{"params": map[string]any{"turnId": "t", "item": v}}).ID
	}
	item("item/started", "u", "userMessage", map[string]any{"content": []any{map[string]string{"type": "text", "text": "修复测试"}}})
	item("item/completed", "u", "userMessage", map[string]any{"content": []any{map[string]string{"type": "text", "text": "修复测试"}}})
	start := item("item/started", "tool", "mcpToolCall", map[string]any{"server": "files", "tool": "read", "arguments": map[string]any{"path": "/work/a.go", "api_key": "secret-start", "n": json.Number("9007199254740993")}})
	end := item("item/completed", "tool", "mcpToolCall", map[string]any{"result": map[string]any{"content": "data", "authorization": "secret-end"}})
	item("item/completed", "diff", "fileChange", map[string]any{"changes": []any{map[string]string{"path": "/work/a.go", "diff": "-old\n+new"}}})
	item("item/completed", "thought", "reasoning", map[string]any{"summary": []any{map[string]string{"text": "检查依赖"}}})
	item("item/completed", "answer", "agentMessage", map[string]any{"text": "已修复 **测试**。"})
	capture := unifiedAdd(t, j, "llm/exchange", "internal", map[string]any{"labels": map[string]string{"runId": "r"}, "request": map[string]any{"body": map[string]any{"bytes": strings.Repeat("private HTTP payload", 500000)}}})
	r := unifiedRead(t, path, capture.ID)
	page := readTranscript(t, r, TranscriptArgs{Limit: 50})
	if page.Total != 6 || page.HasMore {
		t.Fatalf("duplicate or lost entry: %+v", page)
	}
	if page.Entries[0].Role != "user" || len(page.Entries[0].Blocks) != 2 || page.Entries[0].Activity.Evidence[0].EventID != input.ID {
		t.Fatal(page.Entries[0])
	}
	tool := page.Entries[1]
	if tool.Role != "tool" || len(tool.Activity.Evidence) != 2 || tool.Activity.Evidence[0].EventID != start || tool.Activity.Evidence[1].EventID != end {
		t.Fatal(tool)
	}
	if text := entryText(tool); strings.Contains(text, "secret-") || !strings.Contains(text, "9007199254740993") || !strings.Contains(text, "data") {
		t.Fatal(text)
	}
	if page.Entries[2].Blocks[0].Kind != "diff" || page.Entries[3].Role != "reasoning" || page.Entries[4].Role != "assistant" {
		t.Fatal(page.Entries)
	}
	transport := page.Entries[5]
	if transport.Activity.Kind != "transport" || len(transport.Blocks) != 0 || transport.Activity.Evidence[0].ReadWith != "llm_request" {
		t.Fatal(transport)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "private HTTP payload") {
		t.Fatal("transport body escaped reading projection")
	}
	first := readTranscript(t, r, TranscriptArgs{Limit: 2})
	second := readTranscript(t, r, TranscriptArgs{Offset: first.NextOffset, Limit: 2})
	if first.Total != second.Total || first.Entries[1].ID == second.Entries[0].ID || !second.HasMore {
		t.Fatal(first, second)
	}
	if p := readTranscript(t, r, TranscriptArgs{RunID: "foreign"}); p.Total != 0 {
		t.Fatal("foreign run escaped", p)
	}
	if p := readTranscript(t, unifiedRead(t, path, 0), TranscriptArgs{}); p.Total != 0 || len(p.Entries) != 0 {
		t.Fatal("zero cursor", p)
	}
}
func TestTranscriptCodexOpenDeltasAndCompletion(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r"})
	unifiedAdd(t, j, "turn/started", "in", map[string]any{"params": map[string]any{"turn": map[string]string{"id": "t"}}})
	unifiedAdd(t, j, "item/started", "in", map[string]any{"params": map[string]any{"turnId": "t", "item": map[string]any{"id": "a", "type": "agentMessage", "text": "开始 "}}})
	delta := func(turn, text string) int64 {
		return unifiedAdd(t, j, "item/agentMessage/delta", "in", map[string]any{"params": map[string]string{"turnId": turn, "itemId": "a", "delta": text}}).ID
	}
	delta("other", "WRONG TURN")
	through := delta("t", "你好")
	r := unifiedRead(t, path, through)
	delta("t", "FUTURE")
	page := readTranscript(t, r, TranscriptArgs{})
	e := page.Entries[1]
	if entryText(e) != "开始 你好" || !e.ContentPartial || e.Activity.EndObserved || len(e.Activity.Evidence) != 2 || e.Activity.Evidence[1].EventID != through {
		t.Fatal(e)
	}
	done := unifiedAdd(t, j, "item/completed", "in", map[string]any{"params": map[string]any{"turnId": "t", "item": map[string]any{"id": "a", "type": "agentMessage", "text": "最终回复"}}})
	final := readTranscript(t, unifiedRead(t, path, done.ID), TranscriptArgs{}).Entries[1]
	if entryText(final) != "最终回复" || final.ContentPartial || !final.Activity.EndObserved {
		t.Fatal(final)
	}
}
func TestTranscriptKunTextArgumentsTruncationAndControls(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "kun", "input": map[string]string{"text": "请求"}})
	add := func(method string, data map[string]any) int64 {
		return unifiedAdd(t, j, method, "in", map[string]any{"runId": "kun", "sequence": 4, "data": data}).ID
	}
	add("kun/control.queued", map[string]any{"command": map[string]string{"operation": "steer", "text": "补充说明"}})
	add("kun/control.applied", map[string]any{"command": map[string]string{"operation": "steer", "text": "补充说明"}})
	for _, purpose := range []string{"plan", "act"} {
		add("kun/model.started", map[string]any{"step": 1, "purpose": purpose})
		add("kun/model.completed", map[string]any{"step": 1, "purpose": purpose, "message": map[string]string{"content": "结果 " + purpose}})
	}
	add("kun/tool.started", map[string]any{"step": 1, "call": map[string]any{"id": "c", "function": map[string]string{"name": "read", "arguments": `{"apiKey":"secret","n":9007199254740993}`}}})
	add("kun/tool.completed", map[string]any{"step": 1, "call": map[string]any{"id": "c"}, "output": strings.Repeat("中", 13000)})
	last := add("kun/fast.completed", map[string]any{"message": map[string]string{"content": "快速回复"}})
	page := readTranscript(t, unifiedRead(t, path, last), TranscriptArgs{Limit: 50})
	if page.Total != 6 || page.Entries[1].Role != "user" || entryText(page.Entries[1]) != "补充说明" || page.Entries[2].Role != "reasoning" || page.Entries[3].Role != "assistant" || entryText(page.Entries[5]) != "快速回复" {
		t.Fatal(page)
	}
	tool := page.Entries[4]
	text := entryText(tool)
	if !tool.ContentTruncated || utf8.RuneCountInString(text) > transcriptEntryCharacters || strings.Contains(text, "secret") || !strings.Contains(text, "9007199254740993") || tool.Activity.Source != observation.KunSDK {
		t.Fatal(tool)
	}
}
func TestTranscriptSourceBudgetAndAmbiguousDeltas(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r"})
	var last int64
	for n := 0; n < 4; n++ {
		last = unifiedAdd(t, j, "item/completed", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": fmt.Sprint(n), "type": "agentMessage", "text": strings.Repeat("x", 5<<20)}}}).ID
	}
	page := readTranscript(t, unifiedRead(t, path, last), TranscriptArgs{Limit: 50})
	if !page.Entries[4].ContentUnavailable || !page.Entries[4].ContentTruncated {
		t.Fatal("source budget not disclosed", page.Entries[4])
	}
	for n := 0; n < 2; n++ {
		unifiedAdd(t, j, "item/started", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]string{"id": "duplicate", "type": "agentMessage"}}})
	}
	last = unifiedAdd(t, j, "item/agentMessage/delta", "in", map[string]any{"params": map[string]string{"turnId": "t", "itemId": "duplicate", "delta": "ambiguous text"}}).ID
	page = readTranscript(t, unifiedRead(t, path, last), TranscriptArgs{Offset: 5, Limit: 50})
	for _, entry := range page.Entries {
		if strings.Contains(entryText(entry), "ambiguous text") || !entry.ContentUnavailable {
			t.Fatal(entry)
		}
	}
}

func TestTranscriptEmptySuccessIsNotMissingContent(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "input": map[string]string{"text": strings.Repeat("长", 3000)}})
	unifiedAdd(t, j, "item/completed", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": "u", "type": "userMessage", "content": []any{map[string]string{"type": "text", "text": strings.Repeat("长", 3000)}}}}})
	command := strings.Repeat("command ", 1000)
	unifiedAdd(t, j, "item/started", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]string{"id": "c", "type": "commandExecution", "command": command}}})
	unifiedAdd(t, j, "item/completed", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": "c", "type": "commandExecution", "aggregatedOutput": "", "exitCode": 0}}})
	unifiedAdd(t, j, "kun/model.completed", "in", map[string]any{"runId": "r", "data": map[string]any{"step": 1, "message": map[string]any{"content": "", "tool_calls": []any{map[string]string{"id": "c"}}}}})
	unifiedAdd(t, j, "thread/compacted", "in", map[string]any{"runId": "r"})
	last := unifiedAdd(t, j, "run/state", "internal", map[string]any{"runId": "r", "status": "completed"})
	page := readTranscript(t, unifiedRead(t, path, last.ID), TranscriptArgs{Limit: 50})
	if page.Total != 4 {
		t.Fatalf("input echo or successful finish noise: %+v", page)
	}
	for _, e := range page.Entries {
		if e.ContentUnavailable || e.Title == "运行错误" {
			t.Fatalf("normal completion mislabeled: %s %s", e.ID, e.Title)
		}
	}
	if utf8.RuneCountInString(page.Entries[1].Title) > 280 || !strings.Contains(entryText(page.Entries[1]), command) {
		t.Fatal("command title/body bounds")
	}
}

func TestTranscriptNativeMixedContentNotSuppressedByEarlierTextEcho(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "input": map[string]string{"text": "hello"}})
	content := []any{map[string]string{"type": "text", "text": "hello"}}
	unifiedAdd(t, j, "item/started", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": "u", "type": "userMessage", "content": content}}})
	content = append(content, map[string]string{"type": "localImage", "path": "/work/screenshot.png"})
	last := unifiedAdd(t, j, "item/completed", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": "u", "type": "userMessage", "content": content}}})
	page := readTranscript(t, unifiedRead(t, path, last.ID), TranscriptArgs{})
	if page.Total != 2 || len(page.Entries[1].Blocks) != 2 || page.Entries[1].Blocks[1].Path != "/work/screenshot.png" {
		t.Fatalf("mixed content suppressed: %+v", page)
	}
}

func TestTranscriptEchoSuppressionDoesNotHideRepeatedNativeMessages(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "input": map[string]string{"text": "same"}})
	var last int64
	for _, id := range []string{"first", "second"} {
		last = unifiedAdd(t, j, "item/completed", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": id, "type": "userMessage", "content": []any{map[string]string{"type": "text", "text": "same"}}}}}).ID
	}
	page := readTranscript(t, unifiedRead(t, path, last), TranscriptArgs{})
	if page.Total != 2 || page.Entries[1].Activity.Native.ItemID != "second" {
		t.Fatal("repeated native input hidden", page)
	}
}

func TestTranscriptNativeCollaborationAndRejectedResponse(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "input": map[string]string{"text": "hello"}})
	collab := unifiedAdd(t, j, "item/completed", "in", map[string]any{"runId": "r", "params": map[string]any{"turnId": "t", "item": map[string]any{"id": "collab", "type": "collabAgentToolCall", "tool": "spawnAgent", "prompt": "review", "receiverThreadIds": []string{"child-thread"}, "agentsStates": map[string]any{"child-thread": map[string]string{"status": "completed"}}}}})
	unifiedAdd(t, j, "kun/model.started", "in", map[string]any{"runId": "r", "data": map[string]any{"step": 1, "purpose": "act"}})
	last := unifiedAdd(t, j, "kun/model.completed", "in", map[string]any{"runId": "r", "data": map[string]any{"step": 1, "purpose": "act", "status": "rejected", "message": map[string]string{"content": "discarded answer"}}})
	r := unifiedRead(t, path, last.ID)
	page := readTranscript(t, r, TranscriptArgs{})
	if page.Total != 3 || page.Entries[1].Role != "tool" || page.Entries[1].Activity.Kind != "tool" || page.Entries[1].Activity.Evidence[0].EventID != collab.ID || !strings.Contains(entryText(page.Entries[1]), "child-thread") {
		t.Fatal("collaboration lost", page)
	}
	rejected := page.Entries[2]
	if rejected.Role != "system" || rejected.Activity.Status != "rejected" || rejected.Title != "被拒绝的模型回复" || !strings.Contains(strings.Join(rejected.Activity.Issues, ","), "rejected") {
		t.Fatal("discarded reply was accepted", rejected)
	}
	events := unifiedEventsPage(t, r, UnifiedArgs{After: collab.ID})
	if events.Observations[len(events.Observations)-1].Status != "rejected" {
		t.Fatal("canonical status lost", events)
	}
}
