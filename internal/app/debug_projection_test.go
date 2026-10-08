package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/debugmcp"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestDebugProjectionHTTPMCPAuthorizationAndFrozenEvidence(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	a, _, reader := setupKey(t, m, "projection-owner", w.ID, "read")
	_, _, foreign := setupKey(t, m, "projection-foreign", w.ID, "read")
	s, e := m.CreateSessionWithSource(w.ID, "inspect", "", a.InstanceID, SessionSource{Kind: "application", AppID: a.AppID})
	if e != nil {
		t.Fatal(e)
	}
	m.Store.Add(s.ID, "internal", "run/input", map[string]any{"runId": "r", "input": map[string]string{"text": "fixed run"}})
	var event store.Event
	for n := 0; n < 23; n++ {
		event, _ = m.Store.Add(s.ID, "in", "warning", map[string]any{"runId": "r", "message": fmt.Sprintf("literal-' OR 1=1 -- %d", n), "api_key": "DO-NOT-EXPOSE"})
	}
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/sessions/" + s.ID + "/debug/"
	for _, kind := range []string{"runs", "steps", "issues", "statistics", "step&stepId=step-2&runId=r"} {
		res := appRequest(h, "GET", root+"query?kind="+kind, "", reader, "")
		var reply struct {
			Source  string `json:"source"`
			Through int64  `json:"through"`
		}
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &reply) != nil || reply.Source != "host_projection" || reply.Through != event.ID || strings.Contains(res.Body.String(), "DO-NOT-EXPOSE") {
			t.Fatal(kind, res.Code, res.Body.String())
		}
		res = appRequest(h, "GET", root+"query?kind="+kind, "", foreign, "")
		if res.Code != 403 {
			t.Fatal("projection bypassed ownership", res.Code)
		}
		res = appRequest(h, "POST", root+"query?kind="+kind, `{}`, reader, "")
		if res.Code != 403 {
			t.Fatal("projection wrote", res.Code)
		}
	}
	last := event.ID
	newer, _ := m.Store.Add(s.ID, "in", "warning", map[string]any{"runId": "r", "message": "future"})
	res := appRequest(h, "GET", fmt.Sprintf("%squery?kind=issues&through=%d&offset=20&limit=20", root, last), "", reader, "")
	var reply struct {
		Data struct {
			Total, Next int
			Steps       []any
			HasMore     bool
		} `json:"data"`
	}
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &reply) != nil || reply.Data.Total != 23 || len(reply.Data.Steps) != 3 || reply.Data.HasMore {
		t.Fatal(res.Code, res.Body.String())
	}
	res = appRequest(h, "GET", fmt.Sprintf("%squery?kind=event&through=%d&eventId=%d", root, last, newer.ID), "", reader, "")
	if res.Code != 404 {
		t.Fatal("future evidence escaped", res.Code)
	}
	res = appRequest(h, "GET", root+"query?kind=step&stepId=step-2&runId=other", "", reader, "")
	if res.Code != 409 {
		t.Fatal("wrong run allowed", res.Code)
	}
	// String filters and frozen selectors travel through MCP into the same gate.
	server := httptest.NewServer(h)
	defer server.Close()
	client, e := debugmcp.New(server.URL, s.ID, reader)
	if e != nil {
		t.Fatal(e)
	}
	var in bytes.Buffer
	encoder := json.NewEncoder(&in)
	encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "fixture", "version": "1"}}})
	encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "debug_steps", "arguments": map[string]any{"through": last, "runId": "r", "query": "' OR 1=1 --", "limit": 20, "offset": 20}}})
	var out bytes.Buffer
	if e = debugmcp.Serve(context.Background(), client, &in, &out, "test"); e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatal(out.String())
	}
	var rpc struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(lines[1], &rpc) != nil || rpc.Result.IsError || len(rpc.Result.Content) != 1 {
		t.Fatal(out.String())
	}
	if json.Unmarshal([]byte(rpc.Result.Content[0].Text), &reply) != nil || reply.Data.Total != 23 || len(reply.Data.Steps) != 3 {
		t.Fatal(out.String())
	}
	after, _ := m.Session(s.ID)
	if after != s || len(m.handles) != 0 {
		t.Fatal("projection changed runtime")
	}
	huge, _ := m.Store.Add(s.ID, "in", "warning", map[string]string{"message": strings.Repeat("z", (8<<20)+1)})
	res = appRequest(h, "GET", root+"query?kind=steps", "", reader, "")
	if res.Code != 413 || !strings.Contains(res.Body.String(), "debug_projection_too_large") {
		t.Fatal(huge.ID, res.Code, res.Body.String())
	}
	res = appRequest(h, "GET", fmt.Sprintf("%squery?kind=steps&through=%d", root, last), "", reader, "")
	if res.Code != 200 {
		t.Fatal("large future event affected earlier cursor", res.Code)
	}
}
