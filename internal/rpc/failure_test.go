package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFailureProcess(t *testing.T) {
	mode := os.Getenv("RUNDESK_RPC_FAILURE_FIXTURE")
	if mode == "" {
		return
	}
	if mode == "blocked-input" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var m Message
		_ = json.Unmarshal(s.Bytes(), &m)
		switch mode {
		case "invalid-json":
			fmt.Fprintln(os.Stdout, "this is not JSON")
		case "long-stderr":
			fmt.Fprintln(os.Stderr, strings.Repeat("x", 2<<20))
			fmt.Fprintf(os.Stdout, "{\"id\":%s,\"result\":{\"ok\":true}}\n", m.ID)
		case "exit":
			os.Exit(7)
		}
	}
	os.Exit(0)
}

func failureClient(t *testing.T, mode string, trace func(string, Message)) *Client {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFailureProcess$")
	cmd.Env = append(os.Environ(), "RUNDESK_RPC_FAILURE_FIXTURE="+mode)
	c, err := Start(cmd, nil, trace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); <-c.Done() })
	return c
}

func TestCallDeadlineIncludesBlockedPipeWrite(t *testing.T) {
	c := failureClient(t, "blocked-input", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "large", strings.Repeat("x", 2<<20)); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		c.Close()
		<-done
		t.Fatal("Call deadline did not interrupt blocked stdin write")
	}
}

func TestCanceledCallDoesNotSend(t *testing.T) {
	var sent atomic.Int32
	c := failureClient(t, "invalid-json", func(dir string, m Message) {
		if dir == "out" {
			sent.Add(1)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Call(ctx, "must-not-send", nil)
	if !errors.Is(err, context.Canceled) || sent.Load() != 0 {
		t.Fatalf("err=%v sent=%d", err, sent.Load())
	}
}

func TestMalformedProtocolFailsPromptly(t *testing.T) {
	c := failureClient(t, "invalid-json", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Call(ctx, "read", nil)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("missing immediate protocol cause: %v", err)
	}
}

func TestLongStderrDoesNotBlockReplies(t *testing.T) {
	reported := make(chan struct{}, 1)
	c := failureClient(t, "long-stderr", func(dir string, m Message) {
		if dir == "stderr" && strings.Contains(string(m.Params), "truncated") {
			select {
			case reported <- struct{}{}:
			default:
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := c.Call(ctx, "read", nil)
	if err != nil || string(result) != `{"ok":true}` {
		t.Fatalf("stderr blocked response: %s %v", result, err)
	}
	// stdout replies and stderr traces are drained by independent goroutines;
	// the reply does not establish ordering with the final stderr callback.
	select {
	case <-reported:
	case <-ctx.Done():
		t.Fatal("long stderr must be bounded and explicitly marked")
	}
}

func TestUnexpectedExitKeepsExitCause(t *testing.T) {
	c := failureClient(t, "exit", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.Call(ctx, "read", nil)
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("exit cause lost: %v", err)
	}
}
