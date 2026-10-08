package kun

import (
	"bufio"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mcpSpec(url string) p.MCPServer {
	return p.MCPServer{Name: "weather", URL: url, StartupTimeout: 3, ToolTimeout: 3}
}
func mcpModel(t *testing.T, seen *atomic.Int32, check func([]p.Message)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []p.Message `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if seen.Add(1) == 1 {
			names := []string{}
			for _, tool := range req.Tools {
				if strings.HasPrefix(tool.Function.Name, "mcp_") {
					names = append(names, tool.Function.Name)
				}
			}
			if len(names) != 1 {
				t.Errorf("expected one MCP tool: %v", names)
				http.Error(w, "missing tool", 500)
				return
			}
			call := p.ToolCall{ID: "mcp-call", Type: "function", Function: p.Function{Name: names[0], Arguments: `{"city":"Shanghai"}`}}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", ToolCalls: []p.ToolCall{call}}, "finish_reason": "tool_calls"}}})
		} else {
			if check != nil {
				check(req.Messages)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: "MCP result verified"}, "finish_reason": "stop"}}})
		}
	}))
}
func mcpFixture(t *testing.T, calls *atomic.Int32, mode string, cancelled *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var q struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
			t.Error(e)
		}
		if q.Method == "notifications/cancelled" {
			if cancelled != nil {
				cancelled.Add(1)
			}
			w.WriteHeader(202)
			return
		}
		if q.Method != "initialize" && (r.Header.Get("MCP-Protocol-Version") != "2025-11-25" || r.Header.Get("Mcp-Session-Id") != "fixture-session") {
			t.Error("negotiated session headers missing")
		}
		if q.Method == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		var value any
		tool := map[string]any{"name": "lookup weather", "description": "Look up weather", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]string{"type": "string"}}, "required": []string{"city"}}, "annotations": map[string]bool{"readOnlyHint": true}}
		switch q.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "fixture-session")
			value = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fixture", "version": "1"}}
		case "tools/list":
			if q.Params["cursor"] == nil {
				value = map[string]any{"tools": []any{tool}, "nextCursor": "page2"}
			} else {
				next := ""
				if mode == "cursor" {
					next = "page2"
				}
				value = map[string]any{"tools": []any{map[string]any{"name": "delete_item", "inputSchema": map[string]string{"type": "object"}}}, "nextCursor": next}
			}
		case "tools/call":
			calls.Add(1)
			if q.Params["name"] != "lookup weather" {
				t.Error("wrong original tool name", q.Params["name"])
			}
			if mode == "timeout" {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			value = map[string]any{"content": []any{map[string]string{"type": "text", "text": "sunny / mcp-credential-for-test"}}, "structuredContent": map[string]any{"temperature": 23}, "isError": mode == "tool-error"}
		default:
			t.Errorf("unexpected method %s", q.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "id: priming\ndata: \n\ndata: %s\n\n", p.JSON(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": value}))
	}))
}
func allKunEvents(t *testing.T, e *Engine) []p.Event {
	out := []p.Event{}
	var after int64
	for {
		page, err := e.Events(after)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			return out
		}
		out = append(out, page...)
		after = page[len(page)-1].Sequence
	}
}
func TestMCPApprovalSnapshotAndPagination(t *testing.T) {
	var calls, models atomic.Int32
	server := mcpFixture(t, &calls, "", nil)
	defer server.Close()
	model := mcpModel(t, &models, func(messages []p.Message) {
		text := messages[len(messages)-1].Content
		if !strings.Contains(text, "sunny") || !strings.Contains(text, "[redacted]") || strings.Contains(text, "mcp-credential-for-test") {
			t.Error("bad model-visible MCP result", text)
		}
	})
	defer model.Close()
	e := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.Headers = map[string]string{"Authorization": "Bearer mcp-credential-for-test"}
	spec.Allowlist = true
	spec.EnabledTools = []string{"lookup weather"}
	in.MCP = []p.MCPServer{spec}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	if s.Phase != "approval" || s.Approval == nil || s.Actions["mcp-call"] != "prepared" || calls.Load() != 0 {
		t.Fatal("tool ran without approval", s)
	}
	if _, err := e.Control(p.Control{RequestID: "cannot-resume", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"}); err == nil {
		t.Fatal("resume bypassed approval")
	}
	c := p.Control{RequestID: "allow-this-call", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "approve", CallID: "wrong"}
	if _, err := e.Control(c); err == nil {
		t.Fatal("wrong call approved")
	}
	c.CallID = "mcp-call"
	if _, err := e.Control(c); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	if receipt, err := e.Control(c); err != nil || receipt.Status != "applied" {
		t.Fatal("approval retry not idempotent", receipt, err)
	}
	if calls.Load() != 1 || models.Load() != 2 {
		t.Fatal(calls.Load(), models.Load())
	}
	found := false
	for _, event := range allKunEvents(t, e) {
		if strings.Contains(string(p.JSON(event)), "mcp-credential-for-test") {
			t.Fatal("MCP credential persisted")
		}
		if event.Type == "kun/model.started" {
			snapshot, err := e.Snapshot(event.Sequence)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.State.MCPTools) != 1 || snapshot.State.MCPTools[0].ApprovalMode != "prompt" {
				t.Fatal("wrong tool snapshot")
			}
			found = true
		}
	}
	if !found || e.State().MCP[0].Status != "closed" {
		t.Fatal("missing MCP lifecycle snapshot")
	}
}
func TestMCPApprovalPolicies(t *testing.T) {
	for _, mode := range []string{"approve", "reject", "never", "tool-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls, models atomic.Int32
			server := mcpFixture(t, &calls, mode, nil)
			defer server.Close()
			model := mcpModel(t, &models, nil)
			defer model.Close()
			e := newTestEngine(t)
			in := startRequest(t, model.URL)
			spec := mcpSpec(server.URL)
			spec.DisabledTools = []string{"delete_item"}
			if mode == "approve" || mode == "tool-error" {
				spec.ApprovalModes = map[string]string{"lookup weather": "approve"}
			}
			if mode == "never" || mode == "approve" {
				in.ApprovalPolicy = "never"
			}
			in.MCP = []p.MCPServer{spec}
			if _, err := e.Start(in); err != nil {
				t.Fatal(err)
			}
			if mode == "reject" {
				s := waitKun(t, e, "paused")
				if _, err := e.Control(p.Control{RequestID: "reject-this-call", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "reject", CallID: s.Approval.CallID}); err != nil {
					t.Fatal(err)
				}
			}
			s := waitKun(t, e, "completed")
			want := int32(0)
			if mode == "approve" || mode == "tool-error" {
				want = 1
			}
			if calls.Load() != want {
				t.Fatal("approval policy ignored", calls.Load(), want)
			}
			action := s.Actions["mcp-call"]
			if want == 0 && action != "declined" || mode == "tool-error" && action != "failed" {
				t.Fatal("wrong action outcome", action)
			}
		})
	}
}
func TestMCPTimeoutIsUnknownAndNeverRetried(t *testing.T) {
	var calls, models, cancelled atomic.Int32
	server := mcpFixture(t, &calls, "timeout", &cancelled)
	defer server.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	e := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.ToolTimeout = .1
	spec.DisabledTools = []string{"delete_item"}
	spec.ApprovalModes = map[string]string{"lookup weather": "approve"}
	in.MCP = []p.MCPServer{spec}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && p.Active(e.State().Status) {
		time.Sleep(5 * time.Millisecond)
	}
	s := e.State()
	if s.Status != "failed" || s.Actions["mcp-call"] != "outcome_unknown" || calls.Load() != 1 || models.Load() != 1 || cancelled.Load() != 1 {
		t.Fatalf("unsafe retry/outcome: %+v / calls %d models %d cancellations %d", s, calls.Load(), models.Load(), cancelled.Load())
	}
}
func TestMCPRepeatedCursorFailsBeforeModel(t *testing.T) {
	var calls, models atomic.Int32
	server := mcpFixture(t, &calls, "cursor", nil)
	defer server.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	e := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.DisabledTools = []string{"delete_item"}
	in.MCP = []p.MCPServer{spec}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && p.Active(e.State().Status) {
		time.Sleep(5 * time.Millisecond)
	}
	if e.State().Status != "failed" || models.Load() != 0 {
		t.Fatal("partial catalog used by model")
	}
}
func TestMCPStdioFixture(t *testing.T) {
	if os.Getenv("KUN_MCP_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var q map[string]json.RawMessage
		_ = json.Unmarshal(scanner.Bytes(), &q)
		if q["id"] == nil {
			continue
		}
		var method string
		_ = json.Unmarshal(q["method"], &method)
		var result any
		switch method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "stdio_tool", "inputSchema": map[string]string{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "stdio result; parent secret=" + os.Getenv("KUN_UNRELATED_KEY")}}}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
	}
	os.Exit(0)
}
func TestMCPStdioRunAndEnvironmentIsolation(t *testing.T) {
	t.Setenv("KUN_UNRELATED_KEY", "must-not-reach-mcp")
	var models atomic.Int32
	model := mcpModel(t, &models, func(messages []p.Message) {
		last := messages[len(messages)-1].Content
		if strings.Contains(last, "must-not-reach-mcp") || !strings.Contains(last, "stdio result") {
			t.Error("stdio inherited unrelated credentials", last)
		}
	})
	defer model.Close()
	e := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec("")
	spec.Command = os.Args[0]
	spec.Args = []string{"-test.run=^TestMCPStdioFixture$"}
	spec.Environment = map[string]string{"KUN_MCP_FIXTURE": "1"}
	spec.ApprovalModes = map[string]string{"stdio_tool": "approve"}
	in.MCP = []p.MCPServer{spec}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
}
