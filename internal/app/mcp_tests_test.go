package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMCPIndependentCallsAndReceipts(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		if q["id"] == nil {
			w.WriteHeader(202)
			return
		}
		result := map[string]any{"tools": []any{map[string]any{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}}
		if q["method"] == "initialize" {
			result = map[string]any{"protocolVersion": "2025-11-25"}
		}
		if q["method"] == "tools/call" {
			count.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "secret-fixture-token"}}, "isError": true}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
	}))
	defer srv.Close()
	c, e := m.readConfig(wid)
	if e != nil {
		t.Fatal(e)
	}
	_, version := userMCP(c)
	_, e = m.SaveMCP(wid, "fixture", version, map[string]any{"url": srv.URL, "http_headers": map[string]any{"X-Secret": "secret-fixture-token"}, "enabled": false}, false)
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, "", true)
	path := "/workspaces/" + wid + "/mcp-tests?instanceId=default"
	body := `{"server":"fixture","tool":"echo","arguments":{},"confirm":true}`
	first := v1Request(h, "POST", path, body, "mcp-call-test-0001")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	rec := object(t, first)
	if rec["status"] != "tool_error" || strings.Contains(first.Body.String(), "secret-fixture-token") {
		t.Fatal(first.Body.String())
	}
	second := v1Request(h, "POST", path, body, "mcp-call-test-0001")
	if second.Header().Get("Idempotency-Replayed") != "true" || count.Load() != 1 {
		t.Fatal("reexecuted", second.Body.String())
	}
	if v1Request(h, "POST", path, `{"server":"fixture","confirm":true}`, "mcp-call-test-0001").Code != 409 {
		t.Fatal("no conflict")
	}
	get := v1Request(h, "GET", "/workspaces/"+wid+"/mcp-tests/"+rec["id"].(string), "", "")
	if get.Code != 200 || count.Load() != 1 {
		t.Fatal("replay called tool")
	}
	if len(m.Sessions()) != 0 {
		t.Fatal("created an Agent conversation")
	}
	if v1Request(h, "POST", path, `{"server":"fixture"}`, "mcp-call-test-0002").Code != 400 {
		t.Fatal("missing confirmation accepted")
	}
	if v1Request(h, "POST", path, body, "").Code != 400 {
		t.Fatal("missing key accepted")
	}
	work, _ := m.Workspace(wid)
	i, _ := m.Instance()
	i.Execution.Mode = "docker"
	if _, e = m.runMCPTest(context.Background(), work, i, MCPTestRequest{Server: "fixture", Confirm: true}); e == nil {
		t.Fatal("docker fell back to host")
	}
	del := v1Request(h, "DELETE", "/workspaces/"+wid+"/mcp-tests/"+rec["id"].(string), "", "")
	if del.Code != 200 {
		t.Fatal(del.Body.String())
	}
}

func TestMCPTestAdminBoundary(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	_, _, secret := setupKey(t, m, "test-worker", wid, "read", "run")
	h := NewHandler(m, "admin-secret-01234567890123456789", true)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		path := "/api/v1/workspaces/" + wid + "/mcp-tests"
		if method == "DELETE" {
			path += "/fake-id"
		}
		r := appRequest(h, method, path, `{"server":"test","confirm":true}`, secret, "admin-boundary-001")
		if r.Code != 403 {
			t.Fatal(method, r.Code, r.Body.String())
		}
	}
}
