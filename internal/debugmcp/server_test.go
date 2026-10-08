package debugmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

const initLine = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`
const readyLine = `{"jsonrpc":"2.0","method":"notifications/initialized"}`

func transcript(t *testing.T, c *Client, lines ...string) []map[string]json.RawMessage {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), c, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, "test"); err != nil {
		t.Fatal(err)
	}
	result := []map[string]json.RawMessage{}
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		var row map[string]json.RawMessage
		if json.Unmarshal(line, &row) != nil {
			t.Fatal("stdout was not JSON-RPC", out.String())
		}
		result = append(result, row)
	}
	return result
}
func errorCode(t *testing.T, r map[string]json.RawMessage, want int) {
	t.Helper()
	var err struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(r["error"], &err) != nil || err.Code != want {
		t.Fatal("wrong protocol error", string(r["error"]), want)
	}
}
func TestLifecycleValidationAndNoControlForwarding(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/v1/sessions/fixed-session/debug/query" || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("kind") != "events" || r.URL.Query().Get("through") != "0" {
			t.Error("escaped read boundary", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"sessionId":"fixed-session","backend":"codex","data":{"through":0,"events":[]}}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixed-session", "secret")
	if err != nil {
		t.Fatal(err)
	}
	rows := transcript(t, c,
		`{`, `[]`, `null`, `{"jsonrpc":"2.0","id":null,"method":"ping"}`, `{"jsonrpc":"2.0","id":1.5,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":0,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		initLine, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, readyLine,
		`{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"debug_events","arguments":{"through":0,"limit":1}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"debug_resume","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"debug_run","arguments":{"sessionId":"foreign"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"debug_run","arguments":{"url":"http://foreign"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"debug_event","arguments":{"eventId":null}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"debug_diff","arguments":{"sequence":1}}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"debug_snapshot","arguments":{"sequence":0}}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"debug_events","arguments":{"limit":0}}}`,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"debug_events","arguments":{"limit":201}}}`,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"debug_capabilities","arguments":null}}`,
		`{"jsonrpc":"2.0","id":13,"method":"resources/read","params":{"uri":"file:///etc/passwd"}}`,
		initLine,
	)
	if hits.Load() != 1 {
		t.Fatal("unexpected network side effect", hits.Load())
	}
	for n, want := range map[int]int{0: -32700, 1: -32600, 2: -32600, 3: -32600, 4: -32600, 6: -32000, 7: -32602, 9: -32000, 12: -32602, 13: -32602, 14: -32602, 15: -32602, 16: -32602, 17: -32602, 18: -32602, 19: -32602, 20: -32602, 21: -32601, 22: -32600} {
		errorCode(t, rows[n], want)
	}
	var catalog struct {
		Tools []struct {
			Name        string          `json:"name"`
			Annotations map[string]bool `json:"annotations"`
		} `json:"tools"`
	}
	if json.Unmarshal(rows[10]["result"], &catalog) != nil || len(catalog.Tools) != 19 {
		t.Fatal("missing tools", string(rows[10]["result"]))
	}
	for _, tool := range catalog.Tools {
		if !tool.Annotations["readOnlyHint"] || tool.Annotations["destructiveHint"] {
			t.Fatal(tool)
		}
	}
	if !bytes.Contains(rows[11]["result"], []byte(`"isError":false`)) {
		t.Fatal(string(rows[11]["result"]))
	}
}

func TestProtocolNegotiationAndBounds(t *testing.T) {
	for _, version := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "unknown"} {
		rows := transcript(t, nil, strings.Replace(initLine, "2025-11-25", version, 1))
		want := version
		if want == "unknown" {
			want = "2025-11-25"
		}
		if !bytes.Contains(rows[0]["result"], []byte(want)) {
			t.Fatal(string(rows[0]["result"]))
		}
	}
	var out bytes.Buffer
	if err := Serve(context.Background(), nil, strings.NewReader(strings.Repeat("x", 1<<20)), &out, "test"); err == nil || out.Len() != 0 {
		t.Fatal("input limit failed", err)
	}
	if validID(json.RawMessage(`null`)) || validID(json.RawMessage(`{}`)) || validID(json.RawMessage(`true`)) {
		t.Fatal("invalid ID accepted")
	}
}

func TestClientOriginCredentialAndResponseBoundaries(t *testing.T) {
	for _, origin := range []string{"http://example.com", "ftp://127.0.0.1", "http://secret@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?token=secret", "http://127.0.0.1?", "http://127.0.0.1#fragment", "https://example.com/%2f", "https:///"} {
		if _, err := New(origin, "session", "secret"); err == nil {
			t.Fatal("invalid origin accepted", origin)
		}
	}
	for _, session := range []string{"", "../escape", "foreign/debug/query?", "a/b"} {
		if _, err := New("http://127.0.0.1", session, "secret"); err == nil {
			t.Fatal(session)
		}
	}
	for _, token := range []string{"", " secret", "secret\r\nHeader: x"} {
		if _, err := New("http://127.0.0.1", "session", token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	for _, mode := range []string{"redirect", "denied", "large", "invalid", "ok"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				case "denied":
					w.WriteHeader(403)
					fmt.Fprint(w, `{"code":"application_forbidden","error":"secret"}`)
				case "large":
					fmt.Fprint(w, strings.Repeat("x", (4<<20)+1))
				case "invalid":
					fmt.Fprint(w, "secret")
				case "ok":
					fmt.Fprint(w, `{"readOnly":true}`)
				}
			}))
			defer server.Close()
			c, _ := New(server.URL, "session", "secret")
			value, err := c.Get(context.Background(), "capabilities", url.Values{})
			if mode == "ok" {
				if err != nil || !json.Valid(value) {
					t.Fatal(err, string(value))
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe error", err)
			}
			rows := transcript(t, c, initLine, readyLine, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"debug_capabilities"}}`)
			if !bytes.Contains(rows[1]["result"], []byte(`"isError":true`)) {
				t.Fatal("HTTP failure not tool error", string(rows[1]["result"]))
			}
		})
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect forwarded credentials")
	}
}

func TestProjectionArgumentsAreTypedAndScoped(t *testing.T) {
	for _, test := range []struct{ kind, args string }{{"steps", `{"runId":"r","query":"literal' OR 1=1 --","through":0,"limit":50}`}, {"step", `{"stepId":"step-1","runId":"r"}`}, {"statistics", `{"type":"mcpToolCall","status":"failed"}`}} {
		if _, e := arguments(test.kind, json.RawMessage(test.args)); e != nil {
			t.Fatal(test, e)
		}
	}
	for _, test := range []struct{ kind, args string }{{"steps", `{"runId":null}`}, {"steps", `{"query":1}`}, {"steps", `{"limit":51}`}, {"steps", `{"sessionId":"other"}`}, {"step", `{}`}, {"statistics", `{"offset":0}`}} {
		if _, e := arguments(test.kind, json.RawMessage(test.args)); e == nil {
			t.Fatal("invalid projection args", test)
		}
	}
}
