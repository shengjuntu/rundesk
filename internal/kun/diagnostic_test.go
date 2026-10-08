package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func diagnosticFixture(t *testing.T) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("session", "source", map[string]string{"id": "source"}); err != nil {
		t.Fatal(err)
	}
	s.Add("source", "internal", "run/input", map[string]any{"runId": "source-run", "input": map[string]string{"text": "original business task"}})
	ev, err := s.Add("source", "in", "warning", map[string]any{"runId": "source-run", "message": "upstream failed", "api_key": "DO-NOT-EXPOSE"})
	if err != nil {
		t.Fatal(err)
	}
	s.Add("source", "in", "warning", map[string]any{"runId": "source-run", "message": "AFTER-FROZEN-CURSOR"})
	return path, ev.ID
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
		if len(req.Tools) != 7 {
			t.Errorf("expected 7 exclusive diagnostic tools: %v", req.Tools)
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
	in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: through}, SnapshotPath: source}
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
	for _, kind := range []string{"write", "mcp", "skill", "resume"} {
		t.Run(kind, func(t *testing.T) {
			e := newTestEngine(t)
			in := startRequest(t, "http://127.0.0.1:1")
			in.Config.AllowWrite = false
			in.Diagnostic = &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: "source", RunID: "source-run", Through: through}, SnapshotPath: path}
			switch kind {
			case "write":
				in.Config.AllowWrite = true
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
