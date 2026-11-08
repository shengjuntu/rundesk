package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/debugmcp"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestUnifiedObservationHTTPMCPReadBoundaryAndSnapshot(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	a, _, token := setupKey(t, m, "unified-owner", w.ID, "read")
	_, _, foreign := setupKey(t, m, "unified-foreign", w.ID, "read")
	s, err := m.CreateSessionWithSource(w.ID, "unified", "", a.InstanceID, SessionSource{Kind: "application", AppID: a.AppID})
	if err != nil {
		t.Fatal(err)
	}
	add := func(dir, method string, data any) store.Event {
		e, err := m.Store.Add(s.ID, dir, method, data)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	first := add("internal", "run/input", map[string]any{"runId": "r1", "taskId": "task-history", "input": map[string]string{"text": "PRIVATE PROMPT"}})
	add("in", "turn/started", map[string]any{"params": map[string]any{"turn": map[string]any{"id": "turn1"}}})
	add("in", "item/completed", map[string]any{"params": map[string]any{"turnId": "turn1", "item": map[string]any{"id": "tool1", "type": "commandExecution", "command": "PRIVATE COMMAND", "status": "completed"}}})
	add("in", "kun/model.completed", map[string]any{"runId": "r1", "sequence": 11, "data": map[string]any{"step": 1, "response": "PRIVATE MODEL OUTPUT"}})
	body := bytes.Repeat([]byte("raw-model-secret"), 700000) // larger than legacy full-event parsing limit
	capture := add("internal", "llm/exchange", map[string]any{
		"id": "capture1", "labels": map[string]string{"sessionId": s.ID, "runId": "r1"}, "association": "request-start", "started": "2026-10-20T10:00:00Z", "ended": "2026-10-20T10:00:01Z",
		"request":  map[string]any{"method": "POST", "url": "https://model.invalid/v1/responses", "headers": map[string][]string{"Authorization": {"[redacted]"}}, "body": map[string]any{"bytes": body, "size": len(body), "complete": true}},
		"response": map[string]any{"status": 200, "body": map[string]any{"bytes": []byte("PRIVATE RESPONSE"), "size": 16, "complete": true}},
	})
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/sessions/" + s.ID + "/debug/"
	get := func(q string) *httptest.ResponseRecorder { return appRequest(h, "GET", root+"query?"+q, "", token, "") }
	var result struct {
		Source  string `json:"source"`
		Through int64  `json:"through"`
		Data    struct {
			Observations []struct {
				Source  string `json:"source"`
				EventID int64  `json:"eventId"`
			} `json:"observations"`
			Activity        []any `json:"activity"`
			SnapshotThrough int64 `json:"snapshotThrough"`
			NextAfter       int64 `json:"nextAfter"`
			HasMore         bool  `json:"hasMore"`
		} `json:"data"`
	}
	for _, kind := range []string{"observations", "activity"} {
		response := get("kind=" + kind)
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Source != "unified_observation" || result.Through != capture.ID {
			t.Fatal(kind, response.Code, response.Body.String())
		}
		if response.Body.Len() > 200000 || strings.Contains(response.Body.String(), base64.StdEncoding.EncodeToString(body[:48])) || strings.Contains(response.Body.String(), "PRIVATE RESPONSE") {
			t.Fatal("raw transport body escaped metadata query", kind, response.Body.Len())
		}
		denied := appRequest(h, "GET", root+"query?kind="+kind, "", foreign, "")
		if denied.Code != 403 {
			t.Fatal("cross application read", kind, denied.Code)
		}
		write := appRequest(h, "POST", root+"query?kind="+kind, `{}`, token, "")
		if write.Code != 403 {
			t.Fatal("write escaped read scope", kind, write.Code)
		}
		empty := get("kind=" + kind + "&through=0")
		var frozen struct {
			Through int64 `json:"through"`
			Data    struct {
				Observations []any `json:"observations"`
				Activity     []any `json:"activity"`
			} `json:"data"`
		}
		if empty.Code != 200 || json.Unmarshal(empty.Body.Bytes(), &frozen) != nil || frozen.Through != 0 || len(frozen.Data.Observations) != 0 || len(frozen.Data.Activity) != 0 {
			t.Fatal("zero snapshot", kind, empty.Code, empty.Body.String())
		}
	}
	response := get("kind=observations&limit=1")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Data.NextAfter != first.ID || !result.Data.HasMore {
		t.Fatal(response.Code, response.Body.String())
	}
	through := result.Through
	add("in", "future/unknown", map[string]string{"value": "FUTURE"})
	response = get(fmt.Sprintf("kind=observations&through=%d&after=%d&source=observeproxy&recordKind=transport", through, first.ID))
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Through != through || len(result.Data.Observations) != 1 || result.Data.Observations[0].Source != "observeproxy" || result.Data.HasMore {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, q := range []string{"kind=observations&source=invalid", "kind=activity&recordKind=invalid", "kind=observations&limit=201", "kind=activity&after=0", "kind=observations&offset=0"} {
		response = get(q)
		if response.Code != 400 {
			t.Fatal("invalid selector accepted", q, response.Code, response.Body.String())
		}
	}
	// MCP uses exactly the authenticated host endpoint and freezes the same snapshot.
	server := httptest.NewServer(h)
	defer server.Close()
	client, err := debugmcp.New(server.URL, s.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"debug_observations","arguments":{"through":%d,"source":"observeproxy","recordKind":"transport","runId":"r1","limit":1}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"debug_activity","arguments":{"through":0}}}
`, through)
	var out bytes.Buffer
	if err = debugmcp.Serve(context.Background(), client, strings.NewReader(input), &out, "test"); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatal(out.String())
	}
	for _, line := range lines[1:] {
		if !bytes.Contains(line, []byte(`"isError":false`)) {
			t.Fatal(string(line))
		}
	}
	if len(m.handles) != 0 {
		t.Fatal("read started runtime")
	}
}
