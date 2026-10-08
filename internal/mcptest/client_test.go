package mcptest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestHTTPJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprint(sse), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					w.WriteHeader(204)
					return
				}
				var q map[string]any
				json.NewDecoder(r.Body).Decode(&q)
				method := q["method"]
				if method != "initialize" {
					if r.Header.Get("Mcp-Session-Id") != "test-session" || r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
						t.Error("missing negotiated headers")
					}
				}
				if method == "notifications/initialized" {
					w.WriteHeader(202)
					return
				}
				result := map[string]any{"tools": []any{}, "nextCursor": "page-2"}
				if method == "initialize" {
					result = map[string]any{"protocolVersion": "2025-06-18"}
					w.Header().Set("Mcp-Session-Id", "test-session")
				} else {
					calls++
				}
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
				if sse {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.Write(b)
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c := HTTP(ctx, srv.URL, http.Header{})
			defer c.Close()
			if e := c.Initialize(); e != nil {
				t.Fatal(e)
			}
			if _, e := c.Call("tools/list", map[string]any{}); e != nil {
				t.Fatal(e)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
		})
	}
}
func TestHTTPBoundedAndNoRedirect(t *testing.T) {
	for _, status := range []int{302, 200} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if status == 302 {
				w.Header().Set("Location", "http://127.0.0.1:1")
				w.WriteHeader(302)
			} else {
				w.Write(make([]byte, Limit+2))
			}
		}))
		c := HTTP(context.Background(), srv.URL, http.Header{})
		if _, e := c.Call("tools/list", nil); e == nil {
			t.Fatal("accepted invalid response")
		}
		c.Close()
		srv.Close()
	}
}
func TestStdioFixture(t *testing.T) {
	if os.Getenv("RUNDESK_MCP_FIXTURE") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var q map[string]any
		json.Unmarshal(scan.Bytes(), &q)
		if q["id"] == nil {
			continue
		}
		result := map[string]any{"tools": []any{}}
		if q["method"] == "initialize" {
			result = map[string]any{"protocolVersion": "2025-11-25"}
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
	}
	os.Exit(0)
}
func TestStdio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStdioFixture$")
	cmd.Env = append(os.Environ(), "RUNDESK_MCP_FIXTURE=1")
	c, e := Stdio(ctx, cmd)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if e = c.Initialize(); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Call("tools/list", map[string]any{}); e != nil {
		t.Fatal(e)
	}
}
func TestServerRequestRejected(t *testing.T) {
	_, _, e := result([]byte(`{"jsonrpc":"2.0","id":9,"method":"sampling/createMessage"}`), 1)
	if e == nil {
		t.Fatal("server request accepted")
	}
}
