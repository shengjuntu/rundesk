package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func waitTerminal(t *testing.T, e *Engine) p.State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := e.State()
		if !p.Active(s.Status) && s.Status != "completing" {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not terminate", e.State())
	return p.State{}
}
func modelTools(w http.ResponseWriter, calls []p.ToolCall, usage any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", ToolCalls: calls}, "finish_reason": "tool_calls"}}, "usage": usage})
}
func writeCall(id, path string) p.ToolCall {
	return p.ToolCall{ID: id, Type: "function", Function: p.Function{Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q,"content":"verified"}`, path)}}
}

func TestK1ToolAndTokenBudgetsPreventDispatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tools  int
		tokens int64
		usage  any
		want   string
		writes int
	}{
		{"tool-budget", 1, 0, nil, "tool_calls", 1},
		{"token-threshold", 10, 10, map[string]int{"total_tokens": 11}, "reported_token_threshold", 0},
		{"unknown-usage", 10, 10, nil, "token_usage_unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				modelTools(w, []p.ToolCall{writeCall("one", "one.txt"), writeCall("two", "two.txt")}, tc.usage)
			}))
			defer server.Close()
			e := newTestEngine(t)
			in := startRequest(t, server.URL)
			in.Config.Budget = p.BudgetLimits{MaxToolCalls: tc.tools, MaxTotalTokens: tc.tokens}
			if _, err := e.Start(in); err != nil {
				t.Fatal(err)
			}
			s := waitTerminal(t, e)
			if s.Status != "failed" || s.Budget.StopReason != tc.want || s.Budget.ToolCalls != tc.writes || calls.Load() != 1 {
				t.Fatalf("unexpected budget result: %+v", s)
			}
			if _, err := os.Stat(filepath.Join(in.Workspace, "two.txt")); !os.IsNotExist(err) {
				t.Fatal("second tool dispatched")
			}
			if tc.writes == 0 {
				if _, err := os.Stat(filepath.Join(in.Workspace, "one.txt")); !os.IsNotExist(err) {
					t.Fatal("tool dispatched despite token budget")
				}
			}
			if len(s.Pending) != 0 || s.Actions["two"] != "cancelled" {
				t.Fatal("pending protocol not completed", s.Actions)
			}
		})
	}
}
func TestK1InvalidArgumentsBeforeApprovalAndMCP(t *testing.T) {
	var external, model atomic.Int32
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(200)
			return
		}
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil {
			w.WriteHeader(202)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "lookup", "inputSchema": map[string]any{"type": "object", "required": []string{"q"}, "properties": map[string]any{"q": map[string]any{"type": "string"}}, "additionalProperties": false}}}}
		case "tools/call":
			external.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "should not execute"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer mcpServer.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.Add(1) == 1 {
			modelTools(w, []p.ToolCall{{ID: "invalid", Type: "function", Function: p.Function{Name: mcpAlias("weather", "lookup"), Arguments: `{"q":42}`}}}, nil)
		} else {
			final(w, "Invalid arguments were rejected.")
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.MCP = []p.MCPServer{mcpSpec(mcpServer.URL)}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitTerminal(t, e)
	if s.Status != "completed" || external.Load() != 0 || s.Actions["invalid"] != "rejected" || s.Budget.ToolCalls != 0 {
		t.Fatal(s.Status, s.Error, s.Actions, external.Load())
	}
	for _, ev := range allKunEvents(t, e) {
		if ev.Type == "kun/approval.requested" || ev.Type == "kun/tool.started" {
			t.Fatal("invalid arguments reached approval/dispatch", ev.Type)
		}
	}
}
func TestK1FailuresAndModelBudget(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := count.Add(1)
				call := writeCall(fmt.Sprint(n), fmt.Sprintf("%d.txt", n))
				if invalid {
					call.Function.Arguments = `{"path":42}`
				}
				modelTools(w, []p.ToolCall{call}, nil)
			}))
			defer server.Close()
			e := newTestEngine(t)
			in := startRequest(t, server.URL)
			in.Config.MaxSteps = 1
			if invalid {
				in.Config.MaxSteps = 10
				in.Config.Budget.MaxConsecutiveFailures = 2
			}
			if _, err := e.Start(in); err != nil {
				t.Fatal(err)
			}
			s := waitTerminal(t, e)
			want := "model_calls"
			if invalid {
				want = "consecutive_failures"
			}
			if s.Budget.StopReason != want {
				t.Fatal(s.Budget, s.Error)
			}
			if invalid && (count.Load() != 2 || s.Budget.ToolCalls != 0) {
				t.Fatal("invalid calls bypassed failure budget", count.Load(), s.Budget)
			}
		})
	}
}
func TestK1ActiveDeadlineAndWaitAccounting(t *testing.T) {
	t.Run("active deadline", func(t *testing.T) {
		cancelled := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			close(cancelled)
		}))
		defer server.Close()
		e := newTestEngine(t)
		in := startRequest(t, server.URL)
		in.Config.Budget.MaxActiveSeconds = 1
		if _, err := e.Start(in); err != nil {
			t.Fatal(err)
		}
		s := waitTerminal(t, e)
		if s.Status != "failed" || s.Budget.StopReason != "active_time" {
			t.Fatal(s.Status, s.Budget, s.Error)
		}
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("provider request remained active")
		}
	})
	t.Run("debug wait", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { final(w, "done") }))
		defer server.Close()
		e := newTestEngine(t)
		in := startRequest(t, server.URL)
		in.Config.Budget.MaxActiveSeconds = 1
		in.Config.PauseBeforeModel = true
		if _, err := e.Start(in); err != nil {
			t.Fatal(err)
		}
		s := waitKun(t, e, "paused")
		// Advance only the recorded wait clock without spending real test time.
		e.mu.Lock()
		e.clock = e.clock.Add(-2 * time.Second)
		e.mu.Unlock()
		_, err := e.Control(p.Control{RequestID: "resume-wait", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"})
		if err != nil {
			t.Fatal(err)
		}
		s = waitTerminal(t, e)
		if s.Status != "completed" || s.Budget.WaitMillis < 2000 || s.Budget.ActiveMillis >= 1000 {
			t.Fatal(s.Status, s.Budget, s.Error)
		}
		if len(s.Modules) != 4 || s.Harness.ID != "tool-loop-v1" {
			t.Fatal("module state missing")
		}
		for _, ev := range allKunEvents(t, e) {
			if ev.Type == "kun/model.started" {
				snap, err := e.Snapshot(ev.Sequence)
				if err != nil || len(snap.State.Modules) != 4 {
					t.Fatal("modules missing in model snapshot", err)
				}
			}
		}
	})
}
