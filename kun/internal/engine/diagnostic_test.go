package kun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var diagnosticToolNames = []string{"trace_list_runs", "trace_find_steps", "trace_get_step", "trace_read_event", "trace_find_issues", "trace_propose", "trace_list_llm_requests", "trace_read_llm_request", "trace_statistics"}

func diagnosticTools() []any {
	out := []any{}
	for _, name := range diagnosticToolNames {
		props := map[string]any{"runId": map[string]any{"type": "string"}}
		required := []string{}
		if name == "trace_read_event" {
			props["eventId"] = map[string]any{"type": "integer"}
			required = []string{"eventId"}
		}
		if name == "trace_propose" {
			props["proposal"] = map[string]any{"type": "object"}
			required = []string{"runId", "proposal"}
		}
		out = append(out, map[string]any{"name": name, "description": "Read frozen evidence", "inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false}})
	}
	return out
}

func diagnosticFixture(t *testing.T) (p.MCPServer, int64) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch req.Method {
		case "initialize":
			value = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			value = map[string]any{"tools": diagnosticTools()}
		case "tools/call":
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				t.Error(err)
				return
			}
			evidence := map[string]any{"sessionId": "source", "through": 2, "runId": args["runId"], "recorded": true}
			if req.Params.Name == "trace_read_event" {
				evidence["message"] = "upstream failed"
			}
			if req.Params.Name == "trace_propose" {
				evidence["status"] = "suggestion_only"
			}
			value = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(p.JSON(evidence))}}}
		default:
			t.Errorf("unexpected provider method %s", req.Method)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value})
	}))
	t.Cleanup(server.Close)
	return p.MCPServer{Name: "frozen_trace", URL: server.URL, StartupTimeout: 3, ToolTimeout: 3, Allowlist: true, EnabledTools: append([]string(nil), diagnosticToolNames...)}, 2
}
func TestDiagnosticExclusiveToolsAndRestart(t *testing.T) {
	source, through := diagnosticFixture(t)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			return
		}
		if len(req.Tools) != 9 {
			t.Errorf("expected 9 exclusive diagnostic tools: %v", req.Tools)
		}
		for _, tool := range req.Tools {
			if !strings.HasPrefix(tool.Function.Name, "trace_") {
				t.Errorf("unsafe tool: %s", tool.Function.Name)
			}
		}
		encoded := string(p.JSON(req.Messages))
		if strings.Contains(encoded, "DO-NOT-EXPOSE") || strings.Contains(encoded, "AFTER-FROZEN-CURSOR") || strings.Contains(encoded, "BUSINESS-SYSTEM") {
			t.Error("source scope or prompt leak")
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		respond := func(name, args string) {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", ToolCalls: []p.ToolCall{{ID: fmt.Sprint("call-", n), Type: "function", Function: p.Function{Name: name, Arguments: args}}}}, "finish_reason": "tool_calls"}}})
		}
		switch n {
		case 1:
			respond("write_file", `{"path":"should-not-exist","content":"bad"}`)
		case 2:
			if !strings.Contains(encoded, "authorized catalog") {
				t.Error("write rejection not returned")
			}
			respond("trace_read_event", fmt.Sprintf(`{"eventId":%d}`, through))
		case 3:
			respond("trace_propose", fmt.Sprintf(`{"runId":"source-run","proposal":{"kind":"steer","reason":"记录中出现上游失败","text":"先核对服务地址","evidenceIds":[%d]}}`, through))
		default:
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: "事实与推断分开；建议尚未执行。"}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 31}})
		}
	}))
	defer model.Close()
	journal := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(journal)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { e.Close() }()
	in := startRequest(t, model.URL)
	in.Config.AllowWrite = false
	in.Config.SystemPrompt = "BUSINESS-SYSTEM"
	in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: through}, Provider: &source}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	if calls.Load() != 4 || s.Budget.ReportedTokens != 31 || s.Actions["call-1"] != "rejected" {
		t.Fatal(s, calls.Load())
	}
	if _, err = os.Stat(filepath.Join(in.Workspace, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("write executed")
	}
	events, _ := e.Events(0)
	found := false
	for _, ev := range events {
		if ev.Type == "kun/tool.completed" && strings.Contains(string(ev.Data), "suggestion_only") {
			found = true
		}
	}
	if !found {
		t.Fatal("suggestion not retained in diagnostic events")
	}
	check, _ := e.CheckpointFor(in)
	if check.Eligible || check.Reason != "diagnostic_session_use_followup" {
		t.Fatal(check)
	}
	e.Close()
	e, err = Open(journal)
	if err != nil {
		t.Fatal(err)
	}
	in.RunID = "diagnostic-followup"
	in.Input = "继续解释这些证据"
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	changed := in
	changed.RunID = "bad-scope"
	scope := *in.Diagnostic
	scope.Through++
	changed.Diagnostic = &scope
	if _, err = e.Start(changed); err == nil {
		t.Fatal("scope changed")
	}
	changed = in
	changed.RunID = "bad-mode"
	changed.Diagnostic = nil
	if _, err = e.Start(changed); err == nil {
		t.Fatal("diagnostic became executable task")
	}
}
func TestDiagnosticRejectsInheritedCapabilities(t *testing.T) {
	path, through := diagnosticFixture(t)
	for _, kind := range []string{"write", "bash", "mcp", "skill", "resume", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			e := newTestEngine(t)
			in := startRequest(t, "http://127.0.0.1:1")
			in.Config.AllowWrite = false
			in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: through}, Provider: &path}
			switch kind {
			case "write":
				in.Config.AllowWrite = true
			case "bash":
				in.Config.AllowBash = true
			case "legacy":
				in.Diagnostic.Provider = nil
				in.Diagnostic.SnapshotPath = filepath.Join(t.TempDir(), "legacy.db")
			case "mcp":
				in.MCP = []p.MCPServer{{Name: "business"}}
			case "skill":
				in.Skills = []p.Skill{{Name: "business"}}
			case "resume":
				in.Resume = &p.CheckpointSelection{}
			}
			if _, err := e.Start(in); err == nil {
				t.Fatal("inherited capability accepted")
			}
		})
	}
}

func TestDiagnosticProviderRejectsUnsafeCatalogAndBounds(t *testing.T) {
	for _, kind := range []string{"business", "not-readonly", "destructive", "missing-annotation", "oversized", "duplicate", "invalid-json"} {
		t.Run(kind, func(t *testing.T) {
			tools := diagnosticTools()
			first := tools[0].(map[string]any)
			switch kind {
			case "business":
				first["name"] = "write_file"
			case "not-readonly":
				first["annotations"] = map[string]any{"readOnlyHint": false, "destructiveHint": false}
			case "destructive":
				first["annotations"] = map[string]any{"readOnlyHint": true, "destructiveHint": true}
			case "missing-annotation":
				delete(first, "annotations")
			case "duplicate":
				tools = append(tools, tools[0])
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     any    `json:"id"`
					Method string `json:"method"`
				}
				json.NewDecoder(r.Body).Decode(&req)
				var value any
				switch req.Method {
				case "initialize":
					value = map[string]any{"protocolVersion": "2025-11-25"}
				case "notifications/initialized":
					w.WriteHeader(202)
					return
				case "tools/list":
					value = map[string]any{"tools": tools}
				default:
					text := "{}"
					if kind == "oversized" {
						text = strings.Repeat("x", 513<<10)
					}
					if kind == "invalid-json" {
						text = "duplicate evidence is not JSON"
					}
					value = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}}
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value})
			}))
			defer server.Close()
			in := startRequest(t, "http://127.0.0.1:1")
			in.Config.AllowWrite = false
			in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: 2}, Provider: &p.MCPServer{Name: "trace", URL: server.URL, StartupTimeout: 3, ToolTimeout: 3}}
			e := newTestEngine(t)
			if _, err := e.Start(in); err == nil {
				t.Fatal("unsafe provider accepted")
			}
		})
	}
}

func TestDiagnosticProviderCancellationAndSelectedRun(t *testing.T) {
	spec, through := diagnosticFixture(t)
	in := startRequest(t, "http://127.0.0.1:1")
	in.Config.AllowWrite = false
	in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: through}, Provider: &spec}
	provider, err := openDiagnosticProvider(in)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	e := newTestEngine(t)
	e.trace = provider
	scope := in.Diagnostic.DiagnosticScope
	e.state.Diagnostic = &scope
	result, err := e.executeDiagnostic(context.Background(), p.ToolCall{Function: p.Function{Name: "trace_propose", Arguments: `{"runId":"another-run","proposal":{}}`}})
	if err != nil || !result.IsError || !strings.Contains(result.Output, "selected diagnostic run") {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := time.Now()
	if _, err = provider.call(ctx, "trace_statistics", json.RawMessage(`{}`)); err == nil || time.Since(before) > time.Second {
		t.Fatal("cancelled provider call was not bounded", err)
	}
}

func TestDiagnosticProviderCancelsActiveQueryAndReopens(t *testing.T) {
	started := make(chan struct{}, 1)
	var queryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var value any
		switch req.Method {
		case "initialize":
			value = map[string]any{"protocolVersion": "2025-11-25"}
		case "notifications/initialized", "notifications/cancelled":
			w.WriteHeader(202)
			return
		case "tools/list":
			value = map[string]any{"tools": diagnosticTools()}
		default:
			if req.Params.Name == "trace_read_event" && queryCount.Add(1) == 1 {
				started <- struct{}{}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(3 * time.Second):
				}
			}
			value = map[string]any{"content": []any{map[string]string{"type": "text", "text": `{"frozen":true}`}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value})
	}))
	defer server.Close()
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if modelCalls.Add(1) > 1 {
			final(w, "followup")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		msg := p.Message{Role: "assistant", ToolCalls: []p.ToolCall{{ID: "slow-query", Type: "function", Function: p.Function{Name: "trace_read_event", Arguments: `{"eventId":1}`}}}}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "tool_calls"}}})
	}))
	defer model.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, model.URL)
	in.Config.AllowWrite = false
	in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: 2}, Provider: &p.MCPServer{Name: "trace", URL: server.URL, StartupTimeout: 3, ToolTimeout: 5}}
	if _, err = e.Start(in); err != nil {
		e.Close()
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		e.Close()
		t.Fatal("query did not begin")
	}
	before := time.Now()
	_, err = e.Control(p.Control{RequestID: "cancel-active-diagnostic", RunID: in.RunID, ExpectedRevision: -1, Operation: "cancel"})
	if err != nil {
		e.Close()
		t.Fatal(err)
	}
	waitKun(t, e, "interrupted")
	e.Close()
	if time.Since(before) > 2*time.Second {
		t.Fatal("cancel waited for provider timeout")
	}
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	in.RunID = "diagnostic-followup-after-cancel"
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	if result := waitKun(t, e, "completed"); result.Diagnostic == nil || *result.Diagnostic != in.Diagnostic.DiagnosticScope {
		t.Fatal(result)
	}
}

func TestDiagnosticProviderStdioFixtureHelper(t *testing.T) {
	if os.Getenv("KUN_DIAGNOSTIC_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		if req.ID == nil {
			continue
		}
		var value any
		switch req.Method {
		case "initialize":
			value = map[string]any{"protocolVersion": "2025-11-25"}
		case "tools/list":
			value = map[string]any{"tools": diagnosticTools()}
		default:
			value = map[string]any{"content": []any{map[string]string{"type": "text", "text": `{"frozen":true}`}}}
		}
		if encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value}) != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func TestDiagnosticStdioProviderLifetimeOutlivesInitialization(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, "http://127.0.0.1:1")
	in.Config.AllowWrite = false
	in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: 2}, Provider: &p.MCPServer{Name: "trace", Command: exe, Args: []string{"-test.run=^TestDiagnosticProviderStdioFixtureHelper$"}, Environment: map[string]string{"KUN_DIAGNOSTIC_FIXTURE": "1"}, StartupTimeout: 0.5, ToolTimeout: 2}}
	provider, err := openDiagnosticProvider(in)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	timer := time.NewTimer(600 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	result, err := provider.call(context.Background(), "trace_statistics", json.RawMessage(`{"runId":"source-run"}`))
	if err != nil || result.IsError || result.Output != `{"frozen":true}` {
		t.Fatal("provider inherited startup deadline", result, err)
	}
}
