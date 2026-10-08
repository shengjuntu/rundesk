package app

import (
	"context"
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func kunMCPManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("CODEX_HOME", t.TempDir())
	m, err := New(t.TempDir(), "missing-codex-for-kun", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	i, _ := m.Instance()
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:9/v1", Model: "fixture"}); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestKunMCPConfigurationIsolationAndAtomicImport(t *testing.T) {
	m := kunMCPManager(t)
	w := m.Workspaces()[0]
	i, _ := m.Instance()
	value := map[string]any{"url": "http://localhost:9/mcp", "http_headers": map[string]any{"X-Key": "private-mcp-credential"}, "enabled": false}
	if _, err := m.SaveMCP(w.ID, "fixture", "0", value, false, i.ID); err != nil {
		t.Fatal(err)
	}
	view, err := m.MCP(w.ID, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(p.JSON(view)), "private-mcp-credential") || !strings.Contains(string(p.JSON(view)), `"runtime":"kun"`) {
		t.Fatal(view)
	}
	if _, err = m.SaveMCP(w.ID, "stale", "0", value, false, i.ID); err == nil {
		t.Fatal("stale config accepted")
	}
	exported, err := m.ExportMCP(w.ID, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	var bundle MCPBundle
	_ = json.Unmarshal(p.JSON(exported), &bundle)
	if _, err = m.ImportMCP(w.ID, "1", bundle, true, i.ID); err != nil {
		t.Fatal(err)
	}
	saved, _ := m.kunMCPConfig(i.ID)
	if saved.Servers["fixture"].(map[string]any)["http_headers"].(map[string]any)["X-Key"] != "private-mcp-credential" {
		t.Fatal("masked credential lost")
	}
	bundle.Servers["invalid"] = map[string]any{"url": "http://localhost:9/mcp", "tools": map[string]any{"x": map[string]any{"approval_mode": "auto"}}}
	if _, err = m.ImportMCP(w.ID, "2", bundle, true, i.ID); err == nil {
		t.Fatal("unsupported policy accepted")
	}
	saved, _ = m.kunMCPConfig(i.ID)
	if saved.Revision != 2 || len(saved.Servers) != 1 {
		t.Fatal("partial import", saved)
	}
	// Disabled entries can retain missing credential references; they are not resolved at run time.
	if _, err = m.SaveMCP(w.ID, "disabled", "2", map[string]any{"url": "http://localhost:9/mcp", "enabled": false, "bearer_token_env_var": "KUN_MISSING_TEST_SECRET"}, false, i.ID); err != nil {
		t.Fatal(err)
	}
	specs, err := m.kunRuntimeMCP(i.ID)
	if err != nil || len(specs) != 0 {
		t.Fatal(specs, err)
	}
	other, err := m.CreateInstance(InstancePatch{Name: "independent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SetAgentRuntime(other.ID, other.Revision, i.AgentRuntime); err != nil {
		t.Fatal(err)
	}
	isolated, _ := m.kunMCPConfig(other.ID)
	if len(isolated.Servers) != 0 {
		t.Fatal("config crossed instances")
	}
}
func TestKunMCPWorkerApprovalScopesAndFrozenConfig(t *testing.T) {
	m := kunMCPManager(t)
	m.Kun = buildKunTestBinary(t)
	w := m.Workspaces()[0]
	i, err := m.CreateInstance(InstancePatch{Name: "Kun application"})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "credential-not-for-journal"
	t.Setenv("KUN_HTTP_TEST_SECRET", secret)
	var external, models atomic.Int32
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("missing MCP credential")
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var q struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if q.ID == nil {
			w.WriteHeader(202)
			return
		}
		var result any
		switch q.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "lookup", "inputSchema": map[string]string{"type": "object"}}}}
		case "tools/call":
			external.Add(1)
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "result " + secret}}}
		default:
			t.Error(q.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	defer mcpServer.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		message := p.Message{Role: "assistant", Content: "finished"}
		finish := "stop"
		if models.Add(1)%2 == 1 {
			for _, tool := range req.Tools {
				if strings.HasPrefix(tool.Function.Name, "mcp_") {
					message.Content = ""
					message.ToolCalls = []p.ToolCall{{ID: "lookup-call", Type: "function", Function: p.Function{Name: tool.Function.Name, Arguments: `{}`}}}
					finish = "tool_calls"
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
	}))
	defer model.Close()
	i, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: model.URL + "/v1", Model: "fixture", MaxSteps: 4, TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"url": mcpServer.URL, "bearer_token_env_var": "KUN_HTTP_TEST_SECRET"}
	if _, err = m.SaveMCP(w.ID, "fixture", "0", config, false, i.ID); err != nil {
		t.Fatal(err)
	}
	_, err = m.RegisterApplication("kun-test", ApplicationInput{Name: "kun-test", InstanceID: i.ID, WorkspaceID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, runKey, err := m.CreateApplicationKey("kun-test", KeyInput{Name: "runner", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read", "run"}})
	if err != nil {
		t.Fatal(err)
	}
	_, approvalKey, err := m.CreateApplicationKey("kun-test", KeyInput{Name: "reviewer", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read", "run", "approvals"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, foreign := setupKey(t, m, "foreign", w.ID, "read", "run", "approvals")
	handler := NewHandler(m, "admin-secret-01234567890123456789", true)
	created := appRequest(handler, "POST", "/api/v1/sessions", string(p.JSON(map[string]string{"workspaceId": w.ID, "title": "MCP approval"})), runKey, "mcp-session-create-001")
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	var session Session
	_ = json.Unmarshal(created.Body.Bytes(), &session)
	if _, err = m.Start(session.ID, Input{Text: "lookup"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "waiting")
	client, err := m.kunClient(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	get := func() p.State {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var s p.State
		if err := client.Call(ctx, "state", nil, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	current := get()
	if current.Approval == nil || external.Load() != 0 || current.MCP[0].Revision != "1" {
		t.Fatal(current)
	}
	// Saving an always-allow rule must not grant permission retroactively to a paused run.
	config["tools"] = map[string]any{"lookup": map[string]any{"approval_mode": "approve"}}
	if _, err = m.SaveMCP(w.ID, "fixture", "1", config, false, i.ID); err != nil {
		t.Fatal(err)
	}
	frozen := get()
	if frozen.MCP[0].Revision != "1" || frozen.MCPTools[0].ApprovalMode != "prompt" || external.Load() != 0 {
		t.Fatal(frozen)
	}
	body := string(p.JSON(p.Control{RequestID: "approve-mcp-001", RunID: current.RunID, ExpectedRevision: current.Revision, Operation: "approve", CallID: current.Approval.CallID}))
	path := "/api/v1/sessions/" + session.ID + "/kun/control"
	for _, key := range []string{runKey, foreign} {
		res := appRequest(handler, "POST", path, body, key, "")
		if res.Code != 403 {
			t.Fatal("approval scope/ownership bypass", res.Code, res.Body.String())
		}
	}
	res := appRequest(handler, "POST", path, body, approvalKey, "")
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	waitState(t, m, session.ID, "completed")
	if external.Load() != 1 {
		t.Fatal(external.Load())
	}
	if _, err = m.Start(session.ID, Input{Text: "lookup again"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "completed")
	current = get()
	if external.Load() != 2 || current.MCP[0].Revision != "2" || current.MCPTools[0].ApprovalMode != "approve" {
		t.Fatal(current, external.Load())
	}
	rows, err := m.Store.Events(session.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, ev := range rows {
		if strings.Contains(string(ev.Data), secret) {
			t.Fatal("MCP credential leaked into host events")
		}
		if ev.Method == "kun/approval.requested" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("always-allow still prompted", count)
	}
}
