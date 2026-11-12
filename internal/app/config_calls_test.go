package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

// The real subprocess protocol deliberately handles requests concurrently.
// Each wait is bounded even if a test aborts before releasing its fixture.
func configConcurrencyFixture() {
	var out sync.Mutex
	var data sync.Mutex
	version := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg rpc.Message
		if json.Unmarshal(scanner.Bytes(), &msg) != nil || len(msg.ID) == 0 {
			continue
		}
		go func(msg rpc.Message) {
			var p struct {
				Hold, Started, Tag string
				DelayMS            int
				Broken             bool
			}
			_ = json.Unmarshal(msg.Params, &p)
			if name := os.Getenv("RUNDESK_CONFIG_FIXTURE_LOG"); name != "" {
				f, _ := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if f != nil {
					fmt.Fprintln(f, msg.Method)
					f.Close()
				}
			}
			if p.Started != "" {
				_ = os.WriteFile(p.Started, []byte("started"), 0600)
			}
			data.Lock()
			observed := version
			data.Unlock()
			if msg.Method == "initialize" {
				n, _ := strconv.Atoi(os.Getenv("RUNDESK_CONFIG_INIT_DELAY_MS"))
				time.Sleep(time.Duration(n) * time.Millisecond)
			}
			until := time.Now().Add(3 * time.Second)
			for p.Hold != "" && time.Now().Before(until) {
				if _, err := os.Stat(p.Hold); err == nil {
					break
				}
				time.Sleep(2 * time.Millisecond)
			}
			if msg.Method == "config/read" && p.DelayMS == 0 {
				p.DelayMS, _ = strconv.Atoi(os.Getenv("RUNDESK_CONFIG_READ_DELAY_MS"))
			}
			time.Sleep(time.Duration(p.DelayMS) * time.Millisecond)
			if msg.Method == "config/value/write" {
				data.Lock()
				version++
				observed = version
				data.Unlock()
			}
			if p.Broken {
				out.Lock()
				fmt.Fprintln(os.Stdout, "broken native JSON")
				out.Unlock()
				return
			}
			result, _ := json.Marshal(map[string]any{"config": map[string]any{"model": p.Tag, "version": observed}, "layers": []any{}, "origins": map[string]any{}, "version": observed, "pid": os.Getpid()})
			out.Lock()
			_ = json.NewEncoder(os.Stdout).Encode(rpc.Message{ID: msg.ID, Result: result})
			out.Unlock()
		}(msg)
	}
}

func configFixtureManager(t *testing.T) (*Manager, Workspace, string) {
	t.Helper()
	t.Setenv("RUNDESK_CONFIG_CONCURRENCY_FIXTURE", "1")
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("RUNDESK_CONFIG_FIXTURE_LOG", log)
	m := testManager(t)
	w := m.Workspaces()[0]
	return m, w, log
}
func waitConfigFile(t *testing.T, path string) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("fixture request not received", path)
}
func configTestRead(m *Manager, wid, method string, p any, timeout time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() { _, err := m.configCallWithTimeout(context.Background(), timeout, wid, method, p); done <- err }()
	return done
}
func configCallCount(path, method string) int {
	b, _ := os.ReadFile(path)
	return strings.Count(string(b), method+"\n")
}

func TestConfigCallsReadAlongsideSlowDiscoveryAndKeepLifecycleBusy(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	hold, started := filepath.Join(t.TempDir(), "release"), filepath.Join(t.TempDir(), "started")
	slow := configTestRead(m, w.ID, "mcpServerStatus/list", map[string]any{"hold": hold, "started": started}, 2*time.Second)
	waitConfigFile(t, started)
	read := configTestRead(m, w.ID, "config/read", map[string]any{"tag": "fast-native"}, 250*time.Millisecond)
	if err := <-read; err != nil {
		t.Fatal("config read queued behind discovery", err)
	}
	r, err := m.ReloadInstance(DefaultInstance)
	if err != nil || r.(map[string]any)["busyConnections"] != 1 {
		t.Fatal("active reader not protected from reload", r, err)
	}
	_ = os.WriteFile(hold, nil, 0600)
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
}

func TestConfigCallsCoalesceOnlyInFlightAndCopyRawResponses(t *testing.T) {
	m, w, log := configFixtureManager(t)
	hold, started := filepath.Join(t.TempDir(), "release"), filepath.Join(t.TempDir(), "started")
	p := map[string]any{"hold": hold, "started": started, "tag": "same"}
	result := make(chan json.RawMessage, 2)
	errs := make(chan error, 2)
	call := func() { raw, err := m.ConfigCall(w.ID, "config/read", p); result <- raw; errs <- err }
	go call()
	waitConfigFile(t, started)
	go call()
	time.Sleep(30 * time.Millisecond)
	_ = os.WriteFile(hold, nil, 0600)
	first, second := <-result, <-result
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if configCallCount(log, "config/read") != 1 {
		t.Fatal("identical in-flight reads were repeated", configCallCount(log, "config/read"))
	}
	first[0] = 'x'
	if second[0] == 'x' {
		t.Fatal("callers share mutable raw response")
	}
	if _, err := m.ConfigCall(w.ID, "config/read", p); err != nil {
		t.Fatal(err)
	}
	if configCallCount(log, "config/read") != 2 {
		t.Fatal("completed native config was cached")
	}
}

func TestConfigCallsWriteBarrierAndFreshRead(t *testing.T) {
	m, w, log := configFixtureManager(t)
	hold, started := filepath.Join(t.TempDir(), "release"), filepath.Join(t.TempDir(), "started")
	slow := configTestRead(m, w.ID, "config/read", map[string]any{"hold": hold, "started": started}, 2*time.Second)
	waitConfigFile(t, started)
	write := configTestRead(m, w.ID, "config/value/write", map[string]any{}, 2*time.Second)
	h, _ := m.getHandle("config-" + DefaultInstance + "-" + w.ID)
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		h.mu.Lock()
		waiting := h.configWriters > 0
		h.mu.Unlock()
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	later := make(chan json.RawMessage, 1)
	laterErr := make(chan error, 1)
	go func() {
		raw, err := m.ConfigCall(w.ID, "config/read", map[string]any{"tag": "after-write"})
		later <- raw
		laterErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if configCallCount(log, "config/value/write") != 0 {
		t.Fatal("write raced an active read")
	}
	_ = os.WriteFile(hold, nil, 0600)
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
	if err := <-write; err != nil {
		t.Fatal(err)
	}
	raw := <-later
	if err := <-laterErr; err != nil {
		t.Fatal(err)
	}
	var value struct{ Version int }
	_ = json.Unmarshal(raw, &value)
	if value.Version != 1 {
		t.Fatal("post-write read used an old in-flight response", string(raw))
	}
}

func TestConfigCallsQueueAndExecutionWaitUseRequestBudget(t *testing.T) {
	for _, where := range []string{"operation", "execution"} {
		t.Run(where, func(t *testing.T) {
			m, w, log := configFixtureManager(t)
			h, _ := m.getHandle("config-" + DefaultInstance + "-" + w.ID)
			if where == "operation" {
				h.op.Lock()
			} else {
				m.executionMu.Lock()
			}
			start := time.Now()
			_, err := m.configCallWithTimeout(context.Background(), 30*time.Millisecond, w.ID, "config/read", nil)
			if where == "operation" {
				h.op.Unlock()
			} else {
				m.executionMu.Unlock()
			}
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 300*time.Millisecond {
				t.Fatal("queue ignored total budget", time.Since(start), err)
			}
			if configCallCount(log, "initialize") != 0 || m.loaded.Load() != 0 {
				t.Fatal("expired request launched a process")
			}
			if _, err := m.ConfigCall(w.ID, "config/read", nil); err != nil {
				t.Fatal("queue remained blocked", err)
			}
		})
	}
}

func TestConfigCallsInitializationAndRPCShareBudget(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	t.Setenv("RUNDESK_CONFIG_INIT_DELAY_MS", "60")
	start := time.Now()
	_, err := m.configCallWithTimeout(context.Background(), 120*time.Millisecond, w.ID, "config/read", map[string]any{"delayMS": 100})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 250*time.Millisecond {
		t.Fatal("initialization received a separate RPC budget", time.Since(start), err)
	}
}

func TestConfigCallsOneReadTimeoutDoesNotKillOtherReader(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	hold, started := filepath.Join(t.TempDir(), "release"), filepath.Join(t.TempDir(), "started")
	slow := configTestRead(m, w.ID, "model/list", map[string]any{"hold": hold, "started": started}, 2*time.Second)
	waitConfigFile(t, started)
	_, err := m.configCallWithTimeout(context.Background(), 35*time.Millisecond, w.ID, "skills/list", map[string]any{"delayMS": 100})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	rows := m.Processes()
	if len(rows) != 1 || rows[0].Closing || rows[0].Exited {
		t.Fatal("one timeout killed another active reader", rows)
	}
	_ = os.WriteFile(hold, nil, 0600)
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigCall(w.ID, "config/read", nil); err != nil {
		t.Fatal("retired connection did not recover", err)
	}
}

func TestConfigCallsInstanceRevisionAndWorkspaceRemainIsolated(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	if _, err := m.ConfigCall(w.ID, "config/read", nil); err != nil {
		t.Fatal(err)
	}
	first := m.Processes()[0].PID
	i, _ := m.Instance()
	if _, err := m.PatchInstance(i.ID, InstancePatch{Name: i.Name, DefaultModel: "changed", Revision: i.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigCall(w.ID, "config/read", nil); err != nil {
		t.Fatal(err)
	}
	if m.Processes()[0].PID == first {
		t.Fatal("old instance revision kept its native environment")
	}
	other, err := m.CreateWorkspace("Another config scope", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigCall(other.ID, "config/read", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.Processes()) != 2 {
		t.Fatal("different workspace shared a config handle")
	}
}

func TestConfigCallsSelectedExecutableInvalidatesOnlyLocalConnection(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	if _, err := m.ConfigCall(w.ID, "config/read", nil); err != nil {
		t.Fatal(err)
	}
	before := m.Processes()[0].PID
	i, _ := m.Instance()
	docker := i
	docker.Execution = ExecutionSpec{Mode: "docker", Image: "fixture:1"}
	dockerIdentity := configConnectionIdentity(m, docker, w)
	if err := m.Store.Put("system", "setup", SetupState{Codex: "/selected/codex-after-setup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigCall(w.ID, "config/read", nil); err != nil {
		t.Fatal(err)
	}
	if m.Processes()[0].PID == before {
		t.Fatal("newly selected Codex executable kept the old configuration process")
	}
	if configConnectionIdentity(m, docker, w) != dockerIdentity {
		t.Fatal("host Codex selection changed a container's Codex identity")
	}
}

func TestConfigReadCancellationKeepsHealthyConnection(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	t.Setenv("RUNDESK_CONFIG_READ_DELAY_MS", "100")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := m.readConfigContext(ctx, w.ID); result <- err }()
	// Wait for the outgoing trace: this tests response-wait cancellation,
	// not cancellation of a pipe write which correctly retires transport.
	sent := false
	until := time.Now().Add(time.Second)
	for !sent && time.Now().Before(until) {
		events, _ := m.Store.Events("config-"+DefaultInstance+"-"+w.ID, 0, 1000)
		for _, event := range events {
			if event.Direction == "out" && event.Method == "config/read" {
				sent = true
			}
		}
		if !sent {
			time.Sleep(time.Millisecond)
		}
	}
	if !sent {
		cancel()
		t.Fatal("native config request was not sent")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	rows := m.Processes()
	if len(rows) != 1 || rows[0].Closing {
		t.Fatal("HTTP read cancellation closed a healthy native process", rows)
	}
	pid := rows[0].PID
	if _, err := m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	if m.Processes()[0].PID != pid {
		t.Fatal("canceled browser read forced another startup")
	}
}

func TestConfigBrokenTransportDetachesBeforeLifecycleLockWins(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	hold, started := filepath.Join(t.TempDir(), "release"), filepath.Join(t.TempDir(), "started")
	read := configTestRead(m, w.ID, "config/read", map[string]any{"hold": hold, "started": started, "broken": true}, 2*time.Second)
	waitConfigFile(t, started)
	h, _ := m.getHandle("config-" + DefaultInstance + "-" + w.ID)
	entered, releaseWriter, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer func() { once.Do(func() { close(releaseWriter) }); <-done }()
	go func() { close(entered); h.op.Lock(); <-releaseWriter; h.op.Unlock(); close(done) }()
	<-entered
	// The exclusive lifecycle waiter is intentionally held past the call's
	// return. A fatal client must not rely on winning TryLock after RUnlock.
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(hold, nil, 0600)
	err := <-read
	var transport *rpc.TransportError
	if !errors.As(err, &transport) {
		t.Fatal(err)
	}
	h.mu.Lock()
	retained := h.client != nil
	h.mu.Unlock()
	if retained {
		t.Fatal("fatal native transport returned while still retained")
	}
}
