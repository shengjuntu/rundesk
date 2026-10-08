package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	d "github.com/shengjuntu/rundesk/internal/debugapi"
	"github.com/shengjuntu/rundesk/internal/debugmcp"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestCodexDebugScopePagingChunksAndReadonlyMCP(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	app, _, token := setupKey(t, m, "codex-debug", w.ID, "read")
	_, _, foreign := setupKey(t, m, "foreign-debug", w.ID, "read")
	if _, _, e := m.CreateApplicationKey(app.AppID, KeyInput{Name: "run only", WorkspaceIDs: []string{w.ID}, Scopes: []string{"run"}}); e == nil {
		t.Fatal("keys must require read scope")
	}
	a, e := m.CreateSessionWithSource(w.ID, "recorded Codex", "", app.InstanceID, SessionSource{Kind: "application", AppID: app.AppID})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := m.CreateSessionWithSource(w.ID, "empty Codex", "", app.InstanceID, SessionSource{Kind: "application", AppID: app.AppID})
	x, _ := m.Store.Add(a.ID, "in", "item/completed", map[string]any{"count": json.Number("9007199254740993"), "authorization": strings.Repeat("SECRET", 200), "nested": map[string]string{"api_key": "KEEP-PRIVATE"}, "output": strings.Repeat("中文🙂", 100)})
	y, _ := m.Store.Add(a.ID, "in", "item/agentMessage/delta", map[string]string{"delta": "retained delta"})
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/sessions/" + a.ID + "/debug/"
	get := func(query string) *httptest.ResponseRecorder { return appRequest(h, "GET", root+query, "", token, "") }
	res := get("capabilities")
	var caps d.Capabilities
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &caps) != nil || caps.Backend != "codex" || !caps.ReadOnly || !caps.Queries["events"].Available || caps.Queries["context"].Supported || caps.Queries["diff"].Supported {
		t.Fatal(res.Code, res.Body.String())
	}
	res = get("query?kind=run")
	var result map[string]json.RawMessage
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &result) != nil || result["revision"] != nil || result["sequence"] != nil || string(result["source"]) != `"host_session"` {
		t.Fatal(res.Body.String())
	}
	for _, kind := range []string{"context", "tools", "budget", "modules", "breakpoints", "actions", "snapshot&sequence=1", "evidence&sequence=1", "diff&fromSequence=1&sequence=1", "run&sequence=1"} {
		if res = get("query?kind=" + kind); res.Code != 409 || !strings.Contains(res.Body.String(), "debug_unsupported") {
			t.Fatal(kind, res.Code, res.Body.String())
		}
	}
	res = get("query?kind=events&limit=1")
	var page struct {
		Data store.DebugEventPage `json:"data"`
	}
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &page) != nil || !page.Data.HasMore || page.Data.NextCursor != x.ID || page.Data.Through != y.ID || len(page.Data.Events) != 1 || strings.Contains(res.Body.String(), "SECRET") {
		t.Fatal(res.Body.String())
	}
	later, _ := m.Store.Add(a.ID, "in", "turn/completed", map[string]string{"status": "completed"})
	res = get(fmt.Sprintf("query?kind=events&after=%d&through=%d&limit=1", page.Data.NextCursor, page.Data.Through))
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &page) != nil || page.Data.HasMore || len(page.Data.Events) != 1 || page.Data.Events[0].ID != y.ID {
		t.Fatal(res.Body.String())
	}
	if page.Data.NextCursor == later.ID {
		t.Fatal("new event entered frozen page")
	}
	// An explicitly frozen empty journal remains empty after new events arrive.
	_, _ = m.Store.Add(b.ID, "in", "item/completed", map[string]bool{"later": true})
	res = appRequest(h, "GET", "/api/v1/sessions/"+b.ID+"/debug/query?kind=events&through=0", "", token, "")
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &page) != nil || len(page.Data.Events) != 0 || page.Data.Through != 0 {
		t.Fatal(res.Body.String())
	}
	var full strings.Builder
	for offset := 0; ; {
		res = get(fmt.Sprintf("query?kind=event&eventId=%d&offset=%d&limit=17", x.ID, offset))
		var chunk struct {
			Data struct {
				Text string `json:"text"`
				Next int    `json:"nextOffset"`
				More bool   `json:"hasMore"`
			} `json:"data"`
		}
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &chunk) != nil {
			t.Fatal(res.Body.String())
		}
		full.WriteString(chunk.Data.Text)
		if !chunk.Data.More {
			break
		}
		if chunk.Data.Next <= offset {
			t.Fatal("stalled chunk")
		}
		offset = chunk.Data.Next
	}
	if !json.Valid([]byte(full.String())) || strings.Contains(full.String(), "SECRET") || strings.Contains(full.String(), "KEEP-PRIVATE") || !strings.Contains(full.String(), "9007199254740993") || !strings.Contains(full.String(), "中文🙂") {
		t.Fatal("redaction, precision or Unicode chunking failed", full.String())
	}
	res = appRequest(h, "GET", fmt.Sprintf("/api/v1/sessions/%s/debug/query?kind=event&eventId=%d", b.ID, x.ID), "", token, "")
	if res.Code != 404 {
		t.Fatal("foreign session event", res.Code)
	}
	for _, path := range []string{"capabilities", "query?kind=overview", "query?kind=events", fmt.Sprintf("query?kind=event&eventId=%d", x.ID)} {
		for _, credential := range []string{foreign} {
			res = appRequest(h, "GET", root+path, "", credential, "")
			if res.Code != 403 {
				t.Fatal("read or ownership bypass", path, res.Code)
			}
		}
		res = appRequest(h, "POST", root+path, `{}`, token, "")
		if res.Code != 403 {
			t.Fatal("read-only write", res.Code)
		}
	}
	for _, query := range []string{"kind=events&through=999999", "kind=event&eventId=1&offset=2147483648", "kind=events&limit=201", "kind=events&limit=0", "kind=overview&sequence=0", "kind=run&fromSequence=0", "kind=run&kind=context", "kind=run&sessionId=other", "kind=events&through=0&after=1", "kind=event&eventId=0", "kind=run%zz", "kind=run;sequence=1"} {
		res = get("query?" + query)
		if res.Code != 400 {
			t.Fatal(query, res.Code, res.Body.String())
		}
	}
	// Full MCP -> HTTP -> application gate -> shared service, no direct DB path.
	server := httptest.NewServer(h)
	defer server.Close()
	client, e := debugmcp.New(server.URL, a.ID, token)
	if e != nil {
		t.Fatal(e)
	}
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"debug_run","arguments":{}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"debug_context","arguments":{}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"debug_events","arguments":{"through":0}}}
`
	var output bytes.Buffer
	if e = debugmcp.Serve(context.Background(), client, strings.NewReader(input), &output, "test"); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(output.String(), "host_session") || !strings.Contains(output.String(), "debug_unsupported") || strings.Contains(output.String(), token) {
		t.Fatal(output.String())
	}
	before := a
	after, _ := m.Session(a.ID)
	if after != before || len(m.handles) != 0 {
		t.Fatal("inspection mutated session or opened backend")
	}
	// The per-event parse bound is enforced before loading payloads.
	huge, _ := m.Store.Add(a.ID, "in", "item/completed", strings.Repeat("z", (8<<20)+1))
	res = get(fmt.Sprintf("query?kind=event&eventId=%d", huge.ID))
	if res.Code != 413 {
		t.Fatal(res.Code, res.Body.String())
	}
	res = get("query?kind=events")
	if res.Code != 200 || res.Body.Len() > 4000 {
		t.Fatal("metadata loaded payload", res.Body.Len())
	}
}

func TestDebugMemberBoundaryAndKunOffline(t *testing.T) {
	m, _, _, viewer, code, shared, private := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/member/" + viewer.Grants[0].ID + "/sessions/"
	for _, suffix := range []string{"/debug/capabilities", "/debug/query?kind=events", "/debug/query?kind=run", "/debug/query?kind=runs", "/debug/query?kind=steps", "/debug/query?kind=issues", "/debug/query?kind=statistics"} {
		for _, test := range []struct {
			sid    string
			status int
		}{{shared.ID, 200}, {private.ID, 403}} {
			res := appRequest(h, "GET", root+test.sid+suffix, "", code, "")
			if res.Code != test.status {
				t.Fatal(res.Code, res.Body.String())
			}
		}
		res := appRequest(h, "POST", root+shared.ID+suffix, `{}`, code, "")
		if res.Code != 403 {
			t.Fatal(res.Code)
		}
	}
	// An offline Kun session remains offline even for capabilities/snapshot reads.
	km := kunMCPManager(t)
	w := km.Workspaces()[0]
	i, _ := km.Instance("default")
	if _, e := km.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "offline"}); e != nil {
		t.Fatal(e)
	}
	ks, e := km.CreateSession(w.ID, "offline", "")
	if e != nil {
		t.Fatal(e)
	}
	service := DebugService{manager: km}
	caps, e := service.Capabilities(ks.ID)
	if e != nil || !caps.Queries["context"].Supported || caps.Queries["context"].Available || !caps.Queries["events"].Available {
		t.Fatal(caps, e)
	}
	for _, q := range []d.Query{{Kind: "run"}, {Kind: "snapshot", Sequence: 1}, {Kind: "diff", Sequence: 1, FromSequence: 1}} {
		if _, e = service.Query(context.Background(), ks.ID, q); e == nil {
			t.Fatal("offline query succeeded")
		}
	}
	for _, kind := range []string{"runs", "steps", "issues", "statistics"} {
		result, e := service.Query(context.Background(), ks.ID, d.Query{Kind: kind})
		if e != nil || result.Source != "host_projection" {
			t.Fatal(kind, result, e)
		}
	}
	if len(km.handles) != 0 {
		t.Fatal("read opened worker handle")
	}
}
