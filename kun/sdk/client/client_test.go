package kun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
)

func TestMain(m *testing.M) {
	if os.Getenv("KUN_SDK_TEST_WORKER") == "1" {
		mode := os.Args[len(os.Args)-1]
		worker(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func worker(mode string) {
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 65536), p.MaxMessage+2)
	var held *p.Envelope
	for scan.Scan() {
		var request p.Envelope
		if json.Unmarshal(scan.Bytes(), &request) != nil {
			return
		}
		send := func(id string, result any) {
			_ = json.NewEncoder(os.Stdout).Encode(p.Envelope{Version: p.Version, ID: id, Result: p.JSON(result)})
		}
		if request.Method == "hello" {
			hello := p.Hello{Engine: "kun", EngineVersion: "0.16.0", ProtocolVersion: p.Version, PID: os.Getpid(), Capabilities: map[string]bool{"pause": true, "diagnosticSession": true}}
			switch mode {
			case "wrong-protocol":
				hello.ProtocolVersion++
			case "wrong-engine":
				hello.Engine = "other"
			case "missing-engine-version":
				hello.EngineVersion = ""
			case "new":
				hello.EngineVersion = p.EngineVersion
				hello.Capabilities["diagnosticProvider"] = true
			case "wrong-envelope":
				_ = json.NewEncoder(os.Stdout).Encode(p.Envelope{Version: p.Version - 1, ID: request.ID, Result: p.JSON(hello)})
				continue
			}
			send(request.ID, hello)
			continue
		}
		switch mode {
		case "malformed":
			fmt.Fprintln(os.Stdout, `{not valid`)
		case "duplicate":
			fmt.Fprintf(os.Stdout, "{\"version\":16,\"id\":\"%s\",\"id\":\"99\",\"result\":1}\n", request.ID)
		case "missing-id":
			fmt.Fprintln(os.Stdout, `{"version":16,"result":1}`)
		case "conflicting":
			_ = json.NewEncoder(os.Stdout).Encode(p.Envelope{Version: p.Version, ID: request.ID, Error: "bad", Result: p.JSON(1)})
		case "unknown-id":
			send("999999", 1)
		case "oversized":
			send(request.ID, strings.Repeat("x", p.MaxMessage))
		case "exact-limit":
			base := p.Envelope{Version: p.Version, ID: request.ID, Result: p.JSON("")}
			raw, _ := json.Marshal(base)
			send(request.ID, strings.Repeat("x", p.MaxMessage-len(raw)))
		case "reorder", "late-cancel":
			if held == nil {
				saved := request
				held = &saved
				continue
			}
			if mode == "reorder" {
				send(request.ID, request.Method)
				send(held.ID, held.Method)
			} else {
				send(held.ID, "late")
				send(request.ID, request.Method)
			}
			held = nil
		case "remote-error":
			if request.Method == "bad" {
				_ = json.NewEncoder(os.Stdout).Encode(p.Envelope{Version: p.Version, ID: request.ID, Error: "expected rejection"})
			} else {
				send(request.ID, request.Method)
			}
		default:
			send(request.ID, request.Method)
		}
	}
}
func launch(t *testing.T, mode string) *Client {
	t.Helper()
	t.Setenv("KUN_SDK_TEST_WORKER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := Launch(ctx, executable, mode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}
func TestHandshakeIdentityAndCapabilities(t *testing.T) {
	client := launch(t, "new")
	info := client.Info()
	if info.EngineVersion != p.EngineVersion || info.ProtocolVersion != p.Version || info.PID != client.PID() {
		t.Fatalf("wrong actual identity: %#v", info)
	}
	info.Capabilities["diagnosticProvider"] = false
	if err := client.RequireCapabilities("diagnosticProvider", "pause"); err != nil {
		t.Fatal(err)
	}
	if err := client.RequireCapabilities("notSupported"); err == nil {
		t.Fatal("missing extension accepted")
	}
}
func TestLegacyEngineCoreAndOptionalExtension(t *testing.T) {
	client := launch(t, "legacy")
	if client.Info().EngineVersion != "0.16.0" {
		t.Fatal("legacy version lost")
	}
	if err := client.RequireCapabilities("diagnosticProvider"); err == nil {
		t.Fatal("legacy diagnostic provider accepted")
	}
	var value string
	if err := client.Call(context.Background(), "state", nil, &value); err != nil || value != "state" {
		t.Fatalf("legacy core call: %q %v", value, err)
	}
}
func TestRejectHandshakeMismatch(t *testing.T) {
	for _, mode := range []string{"wrong-protocol", "wrong-envelope", "wrong-engine", "missing-engine-version"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("KUN_SDK_TEST_WORKER", "1")
			executable, _ := os.Executable()
			client, err := Launch(context.Background(), executable, mode)
			if client != nil {
				client.Close()
			}
			if err == nil {
				t.Fatal("invalid handshake accepted")
			}
		})
	}
}
func TestRejectDamagedResponse(t *testing.T) {
	for _, mode := range []string{"malformed", "duplicate", "missing-id", "conflicting", "unknown-id", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			client := launch(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := client.Call(ctx, "state", nil, nil); err == nil {
				t.Fatal("invalid response accepted")
			}
			select {
			case <-client.Done():
			case <-ctx.Done():
				t.Fatal("corrupted worker not stopped")
			}
		})
	}
}
func TestExactResponseLimit(t *testing.T) {
	client := launch(t, "exact-limit")
	var value string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, "state", nil, &value); err != nil {
		t.Fatal(err)
	}
	if len(value) < p.MaxMessage-100 {
		t.Fatalf("unexpected result size: %d", len(value))
	}
}
func TestOversizedRequestAndUnserializableParameterDoNotKillWorker(t *testing.T) {
	client := launch(t, "legacy")
	if err := client.Call(context.Background(), "state", strings.Repeat("x", p.MaxMessage), nil); err == nil {
		t.Fatal("oversized request accepted")
	}
	if err := client.Call(context.Background(), "state", make(chan int), nil); err == nil {
		t.Fatal("unserializable parameters accepted")
	}
	var result string
	if err := client.Call(context.Background(), "state", nil, &result); err != nil || result != "state" {
		t.Fatalf("worker poisoned: %q %v", result, err)
	}
}
func TestConcurrentOutOfOrderResponses(t *testing.T) {
	client := launch(t, "reorder")
	type reply struct {
		want, got string
		err       error
	}
	replies := make(chan reply, 2)
	for _, name := range []string{"first", "second"} {
		go func(name string) {
			var got string
			err := client.Call(context.Background(), name, nil, &got)
			replies <- reply{name, got, err}
		}(name)
	}
	for range 2 {
		result := <-replies
		if result.err != nil || result.got != result.want {
			t.Fatalf("response cross-talk: %#v", result)
		}
	}
}
func TestCanceledCallLateResponseDoesNotReachNextCall(t *testing.T) {
	client := launch(t, "late-cancel")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var slow string
	if err := client.Call(ctx, "slow", nil, &slow); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	var quick string
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := client.Call(ctx2, "quick", nil, &quick); err != nil || quick != "quick" {
		t.Fatalf("canceled response leaked: %q %v", quick, err)
	}
}
func TestRemoteErrorAndResultDecodeLeaveWorkerUsable(t *testing.T) {
	client := launch(t, "remote-error")
	var remote *RemoteError
	if err := client.Call(context.Background(), "bad", nil, nil); !errors.As(err, &remote) || remote.Message != "expected rejection" {
		t.Fatalf("remote error lost: %v", err)
	}
	var integer int
	if err := client.Call(context.Background(), "state", nil, &integer); err == nil {
		t.Fatal("invalid result shape accepted")
	}
	var value string
	if err := client.Call(context.Background(), "state", nil, &value); err != nil || value != "state" {
		t.Fatalf("worker poisoned: %v", err)
	}
	client.Close()
	if err := client.Call(context.Background(), "state", nil, nil); err == nil {
		t.Fatal("closed worker accepted call")
	}
}
