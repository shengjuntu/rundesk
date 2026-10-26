package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestHTTPServerRequestsAndFiniteResponse(t *testing.T) {
	for _, method := range []string{"ping", "sampling/createMessage"} {
		t.Run(method, func(t *testing.T) {
			response := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q map[string]any
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Error(err)
					return
				}
				if q["method"] == nil {
					response <- q
					w.WriteHeader(202)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":\"server-question\",\"method\":%q}\n\n", method)
				w.(http.Flusher).Flush()
				select {
				case reply := <-response:
					if reply["id"] != "server-question" {
						t.Error(reply)
					}
					if method == "ping" && reply["result"] == nil || method != "ping" && reply["error"].(map[string]any)["code"] != float64(-32601) {
						t.Error(reply)
					}
				case <-r.Context().Done():
					return
				}
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": map[string]any{"tools": []any{}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
				w.(http.Flusher).Flush()
				// The response stream stays open. A matching RPC result must end the call.
				<-r.Context().Done()
			}))
			defer server.Close()
			client := HTTP(server.URL, nil)
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			raw, err := client.Call(ctx, "tools/list", map[string]any{})
			if err != nil || string(raw) != `{"tools":[]}` {
				t.Fatal(string(raw), err)
			}
		})
	}
}
func TestResultCorrelation(t *testing.T) {
	if _, match, err := Result([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`), 1); err != nil || match {
		t.Fatal("wrong ID matched", err)
	}
	if _, _, err := Result([]byte(`{"id":1,"result":{}}`), 1); err == nil {
		t.Fatal("invalid version accepted")
	}
	if _, _, err := Result([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`), 1); err == nil {
		t.Fatal("null result accepted")
	}
}
func TestSSEChunkingBoundsAndStop(t *testing.T) {
	input := ": keepalive\r\nid: first\r\nretry: 23\r\nevent: message\r\ndata: a\r\ndata: b\r\n\r\ndata: tail"
	var events []SSEEvent
	var ids []string
	retry := 0
	err := ConsumeSSEStream(iotest.OneByteReader(strings.NewReader(input)), ConsumeSSEOptions{OnEvent: func(e SSEEvent) error { events = append(events, e); return nil }, OnID: func(s string) { ids = append(ids, s) }, OnRetry: func(n int) { retry = n }})
	if err != nil || len(events) != 2 || events[0].Data != "a\nb" || events[0].Event != "message" || events[1].Data != "tail" || len(ids) != 1 || retry != 23 {
		t.Fatal(events, ids, retry, err)
	}
	err = ConsumeSSEStream(strings.NewReader("data: abc\ndata: def\n"), ConsumeSSEOptions{MaxEventBytes: 5, OnEvent: func(SSEEvent) error { t.Error("oversize event dispatched"); return nil }})
	if err == nil {
		t.Fatal("unbounded event")
	}
	stop := errors.New("stop")
	err = ConsumeSSEStream(strings.NewReader("data: first\n\ndata: second\n\n"), ConsumeSSEOptions{OnEvent: func(e SSEEvent) error {
		if e.Data != "first" {
			t.Error("read after matching event")
		}
		return stop
	}})
	if err != stop {
		t.Fatal(err)
	}
}
func TestStdioCancellationFixture(t *testing.T) {
	if os.Getenv("MCP_CANCEL_FIXTURE") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
	fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`)
	// Keep stdout open without producing a result until the client kills us.
	for {
		time.Sleep(time.Hour)
	}
}
func TestStdioCancellationWaitsForProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Plain exec.Command is supported as well as CommandContext.
	cmd := exec.Command(os.Args[0], "-test.run=^TestStdioCancellationFixture$")
	cmd.Env = append(os.Environ(), "MCP_CANCEL_FIXTURE=1")
	client, err := Stdio(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	callCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if _, err = client.Call(callCtx, "tools/call", map[string]any{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatal("cancelled child was not reaped")
	}
}
