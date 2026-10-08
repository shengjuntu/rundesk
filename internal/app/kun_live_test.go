package app

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveHostConfirmationExecutionAndCredentialDrift(t *testing.T) {
	m := kunMCPManager(t)
	m.Kun = buildKunTestBinary(t)
	t.Setenv("KUN_LIVE_FIXTURE_TOKEN", "original-credential")
	var connections, models atomic.Int32
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if q.Method == "initialize" {
			connections.Add(1)
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}}
		} else if q.Method == "tools/list" {
			result = map[string]any{"tools": []any{}}
		} else {
			t.Error("unexpected real MCP call", q.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	defer mcp.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Messages []p.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		models.Add(1)
		msg := p.Message{Role: "assistant", Content: "done"}
		if q.Messages[len(q.Messages)-1].Role != "tool" {
			msg.Content = ""
			msg.ToolCalls = []p.ToolCall{{ID: "read", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"input.txt"}`}}, {ID: "write", Type: "function", Function: p.Function{Name: "write_file", Arguments: `{"path":"proof.txt","content":"live proof"}`}}}
		} else if strings.Contains(q.Messages[0].Content, "RunDesk Live") && !strings.Contains(string(p.JSON(q.Messages)), "current input") {
			t.Error("Live model did not receive current observation")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 5}})
	}))
	defer model.Close()
	i, _ := m.Instance()
	cfg := i.AgentRuntime
	cfg.Endpoint = model.URL + "/v1"
	cfg.AllowWrite = true
	if _, err := m.SetAgentRuntime(i.ID, i.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	ws := m.Workspaces()[0]
	if _, err := m.SaveMCP(ws.ID, "live-fixture", "0", map[string]any{"url": mcp.URL, "bearer_token_env_var": "KUN_LIVE_FIXTURE_TOKEN"}, false, i.ID); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(ws.Path, "input.txt"), []byte("past observation"), 0600)
	source, err := m.CreateSession(ws.ID, "source", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "read and write"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, source.ID, "completed")
	var points p.ForkPoints
	for n := 0; n < 30; n++ {
		points, err = m.KunForkPoints(source.ID, 0, 50)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	for _, pt := range points.Items {
		if pt.Phase == "after_model" && pt.Pending == 2 {
			selection.Sequence = pt.Sequence
			break
		}
	}
	if selection.Sequence == 0 {
		t.Fatal(points)
	}
	h := NewHandler(m, userTestAdmin, true)
	call := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		raw := ""
		if body != nil {
			raw = string(p.JSON(body))
		}
		return appRequest(h, method, "/api/v1"+path, raw, userTestAdmin, key)
	}
	input := KunForkInput{SessionID: source.ID, Selection: selection, Title: "Live test", Mode: "live"}
	r := call("POST", "/kun-forks", input, "live-preview")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var preview KunForkPreview
	_ = json.Unmarshal(r.Body.Bytes(), &preview)
	if preview.Origin.Mode != "live" || preview.Live == nil || !preview.Live.AllowWrite || len(preview.Live.PendingTools) != 2 || preview.RecordCount != 0 || models.Load() != 2 || connections.Load() != 1 {
		t.Fatal(preview, models.Load(), connections.Load())
	}
	if strings.Contains(r.Body.String(), "original-credential") || strings.Contains(r.Body.String(), "past observation") {
		t.Fatal("private recording leaked")
	}
	drift, err := m.CreateKunFork(input)
	if err != nil {
		t.Fatal(err)
	}
	url := "/kun-forks/" + preview.ID + "/start"
	r = call("POST", url, map[string]any{"expectedHash": preview.Hash}, "live-unconfirmed")
	if r.Code != 400 || models.Load() != 2 || connections.Load() != 1 {
		t.Fatal("unconfirmed Live executed", r.Code, r.Body.String())
	}
	if _, err = m.Session(preview.TargetSessionID); err == nil {
		t.Fatal("unconfirmed start created target")
	}
	_, _, key := setupKey(t, m, "live-denied", source.WorkspaceID, "read", "run", "approvals")
	r = appRequest(h, "POST", "/api/v1"+url, string(p.JSON(map[string]any{"expectedHash": preview.Hash, "confirmLive": true})), key, "live-scope-denied")
	if r.Code != 403 {
		t.Fatal("application scope authorized Live", r.Code, r.Body.String())
	}
	_ = os.WriteFile(filepath.Join(ws.Path, "input.txt"), []byte("current input"), 0600)
	_ = os.WriteFile(filepath.Join(ws.Path, "proof.txt"), []byte("before live"), 0600)
	r = call("POST", url, map[string]any{"expectedHash": preview.Hash, "confirmLive": true}, "live-start")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var branch Session
	_ = json.Unmarshal(r.Body.Bytes(), &branch)
	waitState(t, m, branch.ID, "completed")
	if models.Load() != 3 || connections.Load() != 2 || branch.KunFork.Mode != "live" {
		t.Fatal(branch, models.Load(), connections.Load())
	}
	if raw, _ := os.ReadFile(filepath.Join(ws.Path, "proof.txt")); string(raw) != "live proof" {
		t.Fatal("Live tool did not execute")
	}
	for _, id := range []string{"live-start", "live-new-key"} {
		r = call("POST", url, map[string]any{"expectedHash": preview.Hash, "confirmLive": true}, id)
		var retry Session
		_ = json.Unmarshal(r.Body.Bytes(), &retry)
		if r.Code != 200 || retry.RunID != branch.RunID || models.Load() != 3 || connections.Load() != 2 {
			t.Fatal("duplicate execution", r.Code, retry)
		}
	}
	after, err := m.KunForkPoints(source.ID, 0, 50)
	if err != nil || after.Selection != points.Selection {
		t.Fatal("source journal changed", after.Selection, err)
	}
	t.Setenv("KUN_LIVE_FIXTURE_TOKEN", "changed-credential")
	r = call("POST", "/kun-forks/"+drift.ID+"/start", map[string]any{"expectedHash": drift.Hash, "confirmLive": true}, "live-drift")
	if r.Code != 409 || !strings.Contains(r.Body.String(), "fork_live_environment_changed") || models.Load() != 3 || connections.Load() != 2 {
		t.Fatal("credential drift admitted", r.Code, r.Body.String())
	}
	if _, err = m.Session(drift.TargetSessionID); err == nil {
		t.Fatal("drift created target")
	}
	if err = m.DeleteSession(branch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.StartKunFork(preview.ID, preview.Hash, true); err == nil {
		t.Fatal("deleted Live target resurrected")
	}
}
