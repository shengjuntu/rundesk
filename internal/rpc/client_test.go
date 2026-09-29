package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRPCProcess(t *testing.T) {
	if os.Getenv("CBASE_RPC_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	requests := []Message{}
	for scanner.Scan() {
		var m Message
		_ = json.Unmarshal(scanner.Bytes(), &m)
		if m.Method == "hold" {
			continue
		}
		requests = append(requests, m)
		if len(requests) == 2 {
			fmt.Fprint(os.Stdout, `{"method":"test/event","params":{"text":"`)
			fmt.Fprint(os.Stdout, strings.Repeat("x", 128*1024))
			fmt.Fprintln(os.Stdout, `"}}`)
			for i := 1; i >= 0; i-- {
				b, _ := json.Marshal(Message{ID: requests[i].ID, Result: requests[i].Params})
				for len(b) > 0 {
					n := 17
					if len(b) < n {
						n = len(b)
					}
					_, _ = os.Stdout.Write(b[:n])
					b = b[n:]
				}
				fmt.Fprintln(os.Stdout)
			}
			requests = nil
		}
	}
	os.Exit(0)
}
func helper(t *testing.T, event func(Message)) *Client {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRPCProcess$")
	cmd.Env = append(os.Environ(), "CBASE_RPC_FIXTURE=1")
	c, e := Start(cmd, event, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close(); <-c.Done() })
	return c
}
func TestFragmentedFramesOutOfOrderAndLargeNotification(t *testing.T) {
	events := make(chan Message, 1)
	c := helper(t, func(m Message) { events <- m })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, value := range []string{"first", "second"} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			raw, e := c.Call(ctx, "echo", v)
			if e != nil {
				t.Error(e)
				return
			}
			var got string
			_ = json.Unmarshal(raw, &got)
			if got != v {
				t.Errorf("response mismatch: %s != %s", got, v)
			}
		}(value)
	}
	wg.Wait()
	select {
	case ev := <-events:
		if len(ev.Params) < 128*1024 {
			t.Fatal("large event truncated")
		}
	case <-ctx.Done():
		t.Fatal("event missing")
	}
}
func TestPendingCallUnblocksOnProcessExit(t *testing.T) {
	c := helper(t, nil)
	done := make(chan error, 1)
	go func() { _, e := c.Call(context.Background(), "hold", nil); done <- e }()
	c.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("expected process-exit error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending call stranded")
	}
}
