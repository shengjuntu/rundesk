package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitKun(t *testing.T, e *Engine, want string) p.State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := e.State()
		if s.Status == want {
			return s
		}
		if s.Status == "failed" {
			t.Fatalf("engine failed: %s", s.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wanted %s: %+v", want, e.State())
	return p.State{}
}
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}
func startRequest(t *testing.T, url string) p.Start {
	return p.Start{SessionID: "session", RunID: "run-one", Workspace: t.TempDir(), Input: "create result", APIKey: "secret-fixture-key", Config: p.Config{Kind: "kun", Endpoint: url + "/v1", Model: "fixture", MaxSteps: 5, TimeoutSeconds: 5, AllowWrite: true}}
}
func emit(w http.ResponseWriter, v any) { fmt.Fprintf(w, "data: %s\n\n", p.JSON(v)) }
func final(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	emit(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}, "finish_reason": "stop"}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func TestLoopStepSnapshotsAndIdempotency(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret-fixture-key" {
			t.Errorf("invalid request")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			toolChunk(w, "write-one", "write_file", "{\"path\":\"out.txt\",", "")
			toolChunk(w, "", "", "\"content\":\"hello\"}", "tool_calls")
			fmt.Fprint(w, "data: [DONE]\n\n")
		} else {
			final(w, "done")
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.PauseBeforeModel = true
	in.Skills = []p.Skill{{Name: "test", Content: "be precise", Hash: "fixture"}}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	if calls.Load() != 0 {
		t.Fatal("model ran before breakpoint")
	}
	if _, err := e.Control(p.Control{RequestID: "stale-control", RunID: s.RunID, ExpectedRevision: s.Revision - 1, Operation: "step"}); err == nil {
		t.Fatal("stale control accepted")
	}
	c := p.Control{RequestID: "first-step", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "step"}
	if _, err := e.Control(c); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, e, "paused")
	if s.Phase != "before_tool" {
		t.Fatalf("bad phase %s", s.Phase)
	}
	if _, err := os.Stat(filepath.Join(in.Workspace, "out.txt")); !os.IsNotExist(err) {
		t.Fatal("tool ran while paused")
	}
	if _, err := e.Control(c); err != nil {
		t.Fatal("identical command not idempotent", err)
	}
	c.Operation = "resume"
	if _, err := e.Control(c); err == nil {
		t.Fatal("different duplicate accepted")
	}
	if _, err := e.Control(p.Control{RequestID: "second-step", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "step"}); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, e, "paused")
	if s.Phase != "before_model" {
		t.Fatalf("bad phase %s", s.Phase)
	}
	b, err := os.ReadFile(filepath.Join(in.Workspace, "out.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatal(string(b), err)
	}
	if _, err = e.Control(p.Control{RequestID: "final-resume", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"}); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, e, "completed")
	if calls.Load() != 2 {
		t.Fatal("unexpected model calls", calls.Load())
	}
	events, err := e.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if strings.Contains(string(p.JSON(ev)), in.APIKey) {
			t.Fatal("credential leaked")
		}
		if ev.Type == "kun/model.started" {
			snap, err := e.Snapshot(ev.Sequence)
			if err != nil || snap.State.Phase != "model" || !strings.Contains(snap.State.Messages[0].Content, "be precise") {
				t.Fatal("bad request snapshot", err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no request snapshot")
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal("duplicate run failed", err)
	}
	if calls.Load() != 2 {
		t.Fatal("duplicate re-executed")
	}
	in.Input = "different"
	if _, err = e.Start(in); err == nil {
		t.Fatal("run ID conflict accepted")
	}
}
func TestSteerQueuedDuringFinalResponseIsApplied(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []p.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if count.Add(1) == 1 {
			close(entered)
			<-release
			final(w, "original")
		} else {
			if req.Messages[len(req.Messages)-1].Content != "include more detail" {
				t.Error("steer not applied")
			}
			final(w, "revised")
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("model not started")
	}
	c := p.Control{RequestID: "steer-during-model", RunID: in.RunID, ExpectedRevision: -1, Operation: "steer", Text: "include more detail"}
	receipt, err := e.Control(c)
	if err != nil || receipt.Status != "queued" {
		t.Fatal(receipt, err)
	}
	close(release)
	waitKun(t, e, "completed")
	receipt, err = e.Control(c)
	if err != nil || receipt.Status != "applied" || count.Load() != 2 {
		t.Fatal("missing durable applied receipt", receipt, err, count.Load())
	}
}
func TestCancelPausedAndTruncatedStreamNeverExecutesTool(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		e := newTestEngine(t)
		in := startRequest(t, "http://127.0.0.1:1")
		in.Config.PauseBeforeModel = true
		if _, err := e.Start(in); err != nil {
			t.Fatal(err)
		}
		s := waitKun(t, e, "paused")
		if _, err := e.Control(p.Control{RequestID: "cancel-paused", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "cancel"}); err != nil {
			t.Fatal(err)
		}
		waitKun(t, e, "interrupted")
	})
	t.Run("truncated", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			toolChunk(w, "bad", "write_file", `{"path":"bad.txt","content":"bad"}`, "")
		}))
		defer server.Close()
		e := newTestEngine(t)
		in := startRequest(t, server.URL)
		if _, err := e.Start(in); err != nil {
			t.Fatal(err)
		}
		<-e.done
		s := e.State()
		if s.Status != "failed" || !strings.Contains(s.Error, "finish_reason") {
			t.Fatal(s.Status, s.Error)
		}
		if _, err := os.Stat(filepath.Join(in.Workspace, "bad.txt")); !os.IsNotExist(err) {
			t.Fatal("truncated stream executed a write")
		}
	})
}
func TestRecoverDispatchedActionAsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	j, err := openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	call := p.ToolCall{ID: "uncertain", Function: p.Function{Name: "write_file", Arguments: `{"path":"x","content":"x"}`}}
	state := p.State{Schema: 1, SessionID: "s", RunID: "r", Status: "running", Actions: map[string]string{"uncertain": "dispatched"}, Pending: []p.ToolCall{call}}
	if _, err = j.commit(state, "kun/tool.started", nil, "", "", nil); err != nil {
		t.Fatal(err)
	}
	j.db.Close()
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := e.State()
	if s.Status != "interrupted" || s.Actions["uncertain"] != "outcome_unknown" || len(s.Pending) != 0 {
		t.Fatal(s)
	}
}
func TestToolsStayWithinWorkspaceAndPermission(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlink unavailable")
	}
	for _, path := range []string{"../secret", filepath.Join(outside, "secret"), "escape/secret"} {
		call := p.ToolCall{Function: p.Function{Name: "read_file", Arguments: string(p.JSON(map[string]string{"path": path}))}}
		if _, err := executeTool(root, false, call); err == nil {
			t.Fatalf("escaped path %s", path)
		}
	}
	call := p.ToolCall{Function: p.Function{Name: "write_file", Arguments: `{"path":"denied","content":"x"}`}}
	if _, err := executeTool(root, false, call); err == nil {
		t.Fatal("unauthorized write")
	}
}
func TestModelCancellation(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := e.Control(p.Control{RequestID: "cancel-network", RunID: in.RunID, ExpectedRevision: -1, Operation: "cancel"}); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "interrupted")
}
func toolChunk(w http.ResponseWriter, id, name, args, finish string) {
	fragment := map[string]any{"index": 0, "id": id, "function": map[string]string{"name": name, "arguments": args}}
	delta := map[string]any{"tool_calls": []any{fragment}}
	emit(w, map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": finish}}})
}

func TestSteerWhilePausedReachesNextModel(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []p.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		calls.Add(1)
		if request.Messages[len(request.Messages)-1].Content != "do this first" {
			t.Error("paused steer did not reach the next request")
		}
		final(w, "done")
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.PauseBeforeModel = true
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	steer := p.Control{RequestID: "paused-steer", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "steer", Text: "do this first"}
	if _, err := e.Control(steer); err != nil {
		t.Fatal(err)
	}
	s = e.State()
	if _, err := e.Control(p.Control{RequestID: "resume-paused", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"}); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	if calls.Load() != 1 {
		t.Fatal("unexpected extra model call")
	}
	receipt, err := e.Control(steer)
	if err != nil || receipt.Status != "applied" {
		t.Fatal(receipt, err)
	}
}
func TestJournalFailureSignalsWorkerShutdown(t *testing.T) {
	e := newTestEngine(t)
	e.j.db.Close()
	if err := e.record("test", nil); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	select {
	case <-e.Fatal():
	case <-time.After(time.Second):
		t.Fatal("journal failure did not stop worker")
	}
}
