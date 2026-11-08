package tracequery

import (
	"bytes"
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/store"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyScopePaginationAndStatistics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Put("session", "source", map[string]string{"id": "source"})
	s.Put("session", "other", map[string]string{"id": "other"})
	add := func(method string, data any) store.Event {
		ev, e := s.Add("source", "in", method, data)
		if e != nil {
			t.Fatal(e)
		}
		return ev
	}
	add("run/input", map[string]any{"runId": "r", "input": map[string]string{"text": "query"}})
	for _, method := range []string{"item/started", "item/completed"} {
		item := map[string]any{"id": "search", "type": "mcpToolCall", "server": "social", "tool": "search", "arguments": map[string]string{"query": "literal' OR 1=1 --"}}
		if method == "item/completed" {
			delete(item, "arguments")
			item["result"] = map[string]any{"isError": true, "content": []string{"HTTP 403"}}
		}
		add(method, map[string]any{"params": map[string]any{"turnId": "t", "item": item}})
	}
	last := add("turn/completed", map[string]any{"params": map[string]any{"turn": map[string]string{"id": "t", "status": "completed"}}})
	foreign, _ := s.Add("other", "in", "error", map[string]string{"secret": "other data"})
	r, e := Open(path, "source", last.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if _, e = r.db.Exec("DELETE FROM events"); e == nil {
		t.Fatal("database accepted mutation")
	}
	if _, e = r.Call("trace_read_event", Args{EventID: foreign.ID}); e == nil {
		t.Fatal("cross-session read")
	}
	newer := add("warning", map[string]string{"message": "after snapshot"})
	if _, e = r.Call("trace_read_event", Args{EventID: newer.ID}); e == nil {
		t.Fatal("future event read")
	}
	v, e := r.Call("trace_find_steps", Args{RunID: "r", Type: "mcpToolCall"})
	if e != nil {
		t.Fatal(e)
	}
	steps := v.(map[string]any)["steps"].([]Step)
	if len(steps) != 1 || steps[0].Status != "failed" || steps[0].Payload["arguments"] == nil {
		t.Fatal(v)
	}
	chunk, e := r.Call("trace_read_event", Args{EventID: steps[0].EventIDs[1], Limit: 7})
	if e != nil || !chunk.(map[string]any)["hasMore"].(bool) {
		t.Fatal(chunk, e)
	}
	v, e = r.Call("trace_find_steps", Args{Query: "' OR 1=1 --"})
	if e != nil || v.(map[string]any)["total"].(int) != 1 {
		t.Fatal("literal search", v, e)
	}
	v, e = r.Call("trace_statistics", Args{Type: "mcpToolCall"})
	if e != nil || v.(map[string]any)["byStatus"].(map[string]int)["failed"] != 1 {
		t.Fatal(v, e)
	}
	if _, e = r.Call("execute_sql", Args{}); e == nil {
		t.Fatal("unknown tool accepted")
	}
	var output bytes.Buffer
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"trace_find_steps","arguments":{"sql":"DELETE FROM events"}}}
`)
	if e = Serve(r, input, &output); e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatal(output.String())
	}
	var reply map[string]any
	json.Unmarshal(lines[2], &reply)
	if reply["error"] == nil {
		t.Fatal("unknown SQL argument accepted")
	}
}
