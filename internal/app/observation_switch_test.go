package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestObservationSwitchDefaultPersistenceAndUpgrade(t *testing.T) {
	data := t.TempDir()
	m, err := New(data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			m.Close()
		}
	}()
	w := m.Workspaces()[0]
	first, err := m.CreateSession(w.ID, "default off", "")
	if err != nil || first.ObserveHTTP || first.Observation == nil || first.Observation.AppliedEnabled || first.Observation.Pending {
		t.Fatalf("new session did not default off: %+v %v", first, err)
	}
	if err = m.ConfigureHTTPObservation(ObservationOptions{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if m.observation != nil {
		t.Fatal("configuring defaults opened a listener")
	}
	second, err := m.CreateSession(w.ID, "new default on", "")
	if err != nil || !second.ObserveHTTP || second.Observation == nil || !second.Observation.Pending || second.Observation.AppliedEnabled {
		t.Fatalf("new default was not saved pending: %+v %v", second, err)
	}
	unchanged, _ := m.Session(first.ID)
	if unchanged.ObserveHTTP {
		t.Fatal("changing the default rewrote an existing session")
	}
	on, off := true, false
	if _, err = m.PatchSession(first.ID, SessionPatch{ObserveHTTP: &on}); err != nil {
		t.Fatal(err)
	}
	if err = m.update(first.ID, func(s *Session) {
		s.Observation = &ObservationStatus{State: "configured", AppliedEnabled: true, Provider: "fixture", Upstream: "http://127.0.0.1:1/v1"}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PatchSession(second.ID, SessionPatch{ObserveHTTP: &off}); err != nil {
		t.Fatal(err)
	}
	legacy, err := m.CreateSession(w.ID, "old serialized record", "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(legacy)
	var old map[string]any
	_ = json.Unmarshal(encoded, &old)
	delete(old, "observeHTTP")
	delete(old, "observation")
	if err = m.Store.Put("session", legacy.ID, old); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m = nil
	m, err = New(data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ConfigureHTTPObservation(ObservationOptions{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{first.ID: true, second.ID: false, legacy.ID: false} {
		got, err := m.Session(id)
		if err != nil || got.ObserveHTTP != want || got.Observation == nil || got.Observation.AppliedEnabled || got.Observation.Pending != want {
			t.Fatalf("saved preference/legacy default wrong after reopen: %+v %v", got, err)
		}
	}
	if m.observation != nil {
		t.Fatal("loading saved preferences started a listener")
	}
}

func TestObservationSwitchOffSkipsDiscoveryAndListener(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "off", "")
	if err != nil {
		t.Fatal(err)
	}
	m.Demo = false
	if err = m.ConfigureHTTPObservation(ObservationOptions{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"model": "keep", "config": map[string]any{"sandbox_workspace_write.network_access": false}}
	before, _ := json.Marshal(params)
	// A nil client makes any accidental config/read fail immediately.
	m.configureHTTPObservation(context.Background(), nil, s, w, params)
	after, _ := json.Marshal(params)
	current, err := m.Session(s.ID)
	if err != nil || !bytes.Equal(before, after) || m.observation != nil || current.Observation == nil || current.Observation.State != "disabled" || current.Observation.AppliedEnabled || current.Observation.Pending {
		t.Fatalf("disabled observation altered native execution: params=%s status=%+v error=%v", after, current.Observation, err)
	}
}

func TestObservationSwitchAuthorizationAndKun(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	application, _, reader := setupKey(t, m, "observation-switch-reader", w.ID, "read")
	s, err := m.CreateSessionWithSource(w.ID, "owned", "", application.InstanceID, SessionSource{Kind: "application", AppID: application.AppID})
	if err != nil {
		t.Fatal(err)
	}
	_, _, foreign := setupKey(t, m, "observation-switch-foreign", w.ID, "read", "run")
	handler := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{reader, foreign} {
		res := appRequest(handler, "PATCH", "/api/v1/sessions/"+s.ID, `{"observeHTTP":true}`, token, "")
		if res.Code != http.StatusForbidden {
			t.Fatalf("unauthorized capture patch: %d %s", res.Code, res.Body.String())
		}
	}
	current, _ := m.Session(s.ID)
	if current.ObserveHTTP {
		t.Fatal("unauthorized patch changed session preference")
	}
	i, err := m.CreateInstance(InstancePatch{Name: "Kun observation switch"})
	if err != nil {
		t.Fatal(err)
	}
	m.Demo = false
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	if err = m.ConfigureHTTPObservation(ObservationOptions{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	kun, err := m.CreateSession(w.ID, "Kun", "", i.ID)
	if err != nil || kun.ObserveHTTP {
		t.Fatalf("Codex default applied to Kun: %+v %v", kun, err)
	}
	on := true
	if _, err = m.PatchSession(kun.ID, SessionPatch{ObserveHTTP: &on}); err == nil {
		t.Fatal("Kun enabled the Codex observation switch")
	}
	kun, _ = m.Session(kun.ID)
	if kun.ObserveHTTP {
		t.Fatal("rejected Kun patch mutated preference")
	}
}

func TestObservationSwitchLateCaptureCannotMarkNewConnectionObserved(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Fixture-Request") == "old" {
			close(entered)
			<-release
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"status":"completed"}`)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); upstream.Close() }()
	m := testManager(t)
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "late capture", "native-model")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	s, err = m.PatchSession(s.ID, SessionPatch{ObserveHTTP: &on})
	if err != nil {
		t.Fatal(err)
	}
	m.Demo = false
	if err = m.ConfigureHTTPObservation(ObservationOptions{}); err != nil {
		t.Fatal(err)
	}
	native := map[string]any{"model_provider": "fixture", "model_providers": map[string]any{"fixture": map[string]any{"name": "Native", "base_url": upstream.URL + "/v1"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	configure := func() (string, string) {
		t.Helper()
		client := observationRPCFixture(t, native, false)
		params := map[string]any{}
		m.configureHTTPObservation(ctx, client, s, w, params)
		if _, err := client.Call(ctx, "thread/resume", params); err != nil {
			t.Fatal(err)
		}
		m.observationApplied(s.ID, true)
		current, _ := m.Session(s.ID)
		return params["config"].(map[string]any)["model_providers.fixture.base_url"].(string), current.Observation.CaptureID
	}
	send := func(route, label string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, route+"/responses", strings.NewReader(`{"input":[]}`))
		if err != nil {
			return err
		}
		req.Header.Set("X-Fixture-Request", label)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		_, err = io.Copy(io.Discard, res.Body)
		return err
	}
	oldRoute, oldCapture := configure()
	finished := make(chan error, 1)
	go func() { finished <- send(oldRoute, "old") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("old request did not enter upstream")
	}
	newRoute, newCapture := configure()
	if oldCapture == "" || newCapture == "" || oldCapture == newCapture {
		t.Fatal("capture connection identity was not renewed")
	}
	releaseOnce.Do(func() { close(release) })
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	waitObservationRecords(t, m, s.ID, 1)
	current, _ := m.Session(s.ID)
	if current.Observation == nil || current.Observation.CaptureID != newCapture || current.Observation.State != "configured" || !current.Observation.AppliedEnabled {
		t.Fatalf("late old record updated new capture status: %+v", current.Observation)
	}
	if err = send(newRoute, "new"); err != nil {
		t.Fatal(err)
	}
	waitObservationRecords(t, m, s.ID, 2)
	current, _ = m.Session(s.ID)
	if current.Observation == nil || current.Observation.CaptureID != newCapture || current.Observation.State != "observed" {
		t.Fatalf("new capture did not update its own status: %+v", current.Observation)
	}
}

type observationSwitchCall struct {
	PID    int            `json:"pid"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// The child makes actual HTTP requests, but models only App Server process and
// turn control. Endpoint persistence semantics are also checked by the real CLI
// smoke; this fixture must never read a user's native Codex configuration.
func TestObservationSwitchRPCFixture(t *testing.T) {
	if os.Getenv("RUNDESK_OBSERVATION_SWITCH_FIXTURE") != "1" {
		return
	}
	log, err := os.OpenFile(os.Getenv("RUNDESK_OBSERVATION_SWITCH_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	defer log.Close()
	logEncoder := json.NewEncoder(log)
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	native := os.Getenv("RUNDESK_OBSERVATION_SWITCH_UPSTREAM") + "/native/v1"
	endpoint := native
	thread := "thread-observation-switch"
	turn := 0
	for scanner.Scan() {
		var request rpc.Message
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(3)
		}
		var params map[string]any
		_ = json.Unmarshal(request.Params, &params)
		_ = logEncoder.Encode(observationSwitchCall{os.Getpid(), request.Method, params})
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result["userAgent"] = "observation-switch-fixture"
		case "config/read":
			result["config"] = map[string]any{"model_provider": "fixture", "model_providers": map[string]any{"fixture": map[string]any{"name": "Native fixture", "base_url": native, "wire_api": "responses", "env_key": "SYNTHETIC_ONLY"}}}
		case "thread/start", "thread/resume":
			if id, _ := params["threadId"].(string); id != "" {
				thread = id
			}
			if config, _ := params["config"].(map[string]any); config != nil {
				if base, _ := config["model_providers.fixture.base_url"].(string); base != "" {
					endpoint = base
				}
			}
			result["thread"] = map[string]any{"id": thread}
		case "turn/start":
			turn++
			result["turn"] = map[string]any{"id": fmt.Sprintf("turn-%d-%d", os.Getpid(), turn), "status": "inProgress"}
		}
		raw, _ := json.Marshal(result)
		if encoder.Encode(rpc.Message{ID: request.ID, Result: raw}) != nil {
			os.Exit(4)
		}
		if request.Method == "turn/start" {
			turnID := result["turn"].(map[string]any)["id"].(string)
			started, _ := json.Marshal(map[string]any{"threadId": thread, "turn": map[string]any{"id": turnID, "status": "inProgress"}})
			_ = encoder.Encode(rpc.Message{Method: "turn/started", Params: started})
			body := fmt.Sprintf(`{"model":"native-model","input":[],"tools":[],"turn":"%s"}`, turnID)
			req, _ := http.NewRequest(http.MethodPost, endpoint+"/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer synthetic-switch-key")
			req.Header.Set("X-Fixture-PID", fmt.Sprint(os.Getpid()))
			res, callErr := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if callErr != nil {
				os.Exit(5)
			}
			_, copyErr := io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if copyErr != nil || res.StatusCode != http.StatusOK {
				os.Exit(6)
			}
			completed, _ := json.Marshal(map[string]any{"threadId": thread, "turn": map[string]any{"id": turnID, "status": "completed"}})
			_ = encoder.Encode(rpc.Message{Method: "turn/completed", Params: completed})
		}
	}
	os.Exit(0)
}

func TestObservationSwitchAppliesNextTurnWithoutInterruptingRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executable uses a POSIX launcher")
	}
	requests := make(chan string, 8)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var requestMu sync.Mutex
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path != "/native/v1/responses" || r.Header.Get("Authorization") != "Bearer synthetic-switch-key" {
			t.Error("native request changed")
		}
		requestMu.Lock()
		requestCount++
		index := requestCount
		requestMu.Unlock()
		requests <- r.Header.Get("X-Fixture-PID")
		if index == 3 {
			close(entered)
			<-release
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		upstream.Close()
	}()
	fixtureDir := t.TempDir()
	logPath := filepath.Join(fixtureDir, "rpc.jsonl")
	executable := filepath.Join(fixtureDir, "codex-fixture")
	quotedTestBinary := "'" + strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") + "'"
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec "+quotedTestBinary+" -test.run='^TestObservationSwitchRPCFixture$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNDESK_OBSERVATION_SWITCH_FIXTURE", "1")
	t.Setenv("RUNDESK_OBSERVATION_SWITCH_LOG", logPath)
	t.Setenv("RUNDESK_OBSERVATION_SWITCH_UPSTREAM", upstream.URL)
	m := testManager(t)
	m.Codex, m.Demo = executable, false
	if err := m.ConfigureHTTPObservation(ObservationOptions{}); err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession(m.Workspaces()[0].ID, "switch lifecycle", "native-model")
	if err != nil {
		t.Fatal(err)
	}
	start := func() {
		t.Helper()
		if _, err := m.Start(s.ID, Input{Text: "synthetic task"}); err != nil {
			t.Fatal(err)
		}
	}
	requestPID := func() string {
		t.Helper()
		select {
		case pid := <-requests:
			return pid
		case <-time.After(5 * time.Second):
			t.Fatal("native request missing")
			return ""
		}
	}
	patch := func(enabled bool) Session {
		t.Helper()
		got, err := m.PatchSession(s.ID, SessionPatch{ObserveHTTP: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	start()
	firstPID := requestPID()
	first := waitState(t, m, s.ID, "completed")
	if first.ThreadID == "" || m.observation != nil {
		t.Fatal("off turn lost thread or started listener")
	}
	patch(false)
	start()
	if pid := requestPID(); pid != firstPID {
		t.Fatal("same off setting reconnected native process")
	}
	waitState(t, m, s.ID, "completed")
	h, _ := m.getHandle(s.ID)
	h.mu.Lock()
	initialClient := h.client
	h.mu.Unlock()
	pending := patch(true)
	if pending.Observation == nil || !pending.Observation.Pending || pending.Observation.AppliedEnabled {
		t.Fatalf("enable did not remain pending: %+v", pending.Observation)
	}
	h.mu.Lock()
	same := h.client == initialClient
	h.mu.Unlock()
	if !same || initialClient.Closing() {
		t.Fatal("idle preference patch closed current client")
	}
	start()
	onPID := requestPID()
	if onPID == firstPID {
		t.Fatal("new capture preference reused old native process")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("held request missing")
	}
	on := waitState(t, m, s.ID, "running")
	if on.ThreadID != first.ThreadID || on.Observation == nil || !on.Observation.AppliedEnabled || on.Observation.Pending {
		t.Fatalf("enabled connection status wrong: %+v", on)
	}
	h.mu.Lock()
	activeClient := h.client
	h.mu.Unlock()
	patch(true)
	// Toggling twice before another turn preserves the applied connection.
	if got := patch(false); got.Observation == nil || !got.Observation.Pending || !got.Observation.AppliedEnabled {
		t.Fatalf("disable lost active status: %+v", got.Observation)
	}
	if got := patch(true); got.Observation.Pending {
		t.Fatal("restoring applied preference left a pending reconnect")
	}
	pending = patch(false)
	h.mu.Lock()
	same = h.client == activeClient
	h.mu.Unlock()
	if !same || activeClient.Closing() || pending.Status != "running" || pending.ThreadID != first.ThreadID {
		t.Fatal("active preference patch disturbed execution")
	}
	releaseOnce.Do(func() { close(release) })
	waitState(t, m, s.ID, "completed")
	waitObservationRecords(t, m, s.ID, 1)
	completed, _ := m.Session(s.ID)
	if completed.Observation == nil || !completed.Observation.Pending || !completed.Observation.AppliedEnabled || completed.ObserveHTTP {
		t.Fatalf("late observation overwrote pending disable: %+v", completed.Observation)
	}
	start()
	offPID := requestPID()
	if offPID == onPID {
		t.Fatal("disable did not reconnect at next turn")
	}
	last := waitState(t, m, s.ID, "completed")
	if last.ThreadID != first.ThreadID || last.ObserveHTTP || last.Observation == nil || last.Observation.Pending || last.Observation.AppliedEnabled || last.Observation.State != "disabled" {
		t.Fatalf("disabled resumed connection wrong: %+v", last)
	}
	events, err := m.Store.QueryEvents(s.ID, 0, 100, store.EventFilter{Method: "llm/exchange"})
	if err != nil || len(events) != 1 {
		t.Fatalf("off turn was captured: %d %v", len(events), err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var discoveries, starts, resumes int
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var call observationSwitchCall
		if err := json.Unmarshal(line, &call); err != nil {
			t.Fatal(err)
		}
		switch call.Method {
		case "config/read":
			discoveries++
			if fmt.Sprint(call.PID) != onPID {
				t.Fatal("disabled turn discovered model configuration")
			}
		case "thread/start":
			starts++
		case "thread/resume":
			resumes++
			if call.Params["threadId"] != first.ThreadID {
				t.Fatal("preference change started a new native thread")
			}
			config, _ := call.Params["config"].(map[string]any)
			route, _ := config["model_providers.fixture.base_url"].(string)
			if fmt.Sprint(call.PID) == offPID && route != "" {
				t.Fatalf("disabled resume retained endpoint override: %s", route)
			}
			if fmt.Sprint(call.PID) == onPID && (route == "" || !strings.HasSuffix(route, "/native/v1")) {
				t.Fatal("enabled resume did not supply proxy route")
			}
			if call.Params["sandbox"] != "workspace-write" || call.Params["approvalPolicy"] != "on-request" {
				t.Fatal("switch changed native permissions")
			}
		}
	}
	if !reflect.DeepEqual([]int{discoveries, starts, resumes}, []int{1, 1, 2}) {
		t.Fatalf("unexpected lifecycle calls: discoveries=%d starts=%d resumes=%d", discoveries, starts, resumes)
	}
}
