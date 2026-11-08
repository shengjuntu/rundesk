package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

// A separate process speaks the real JSONL protocol. Faults are injected at the
// child boundary; no production flag or demo behaviour is changed.
func appFailureFixture(mode string) {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var m rpc.Message
		if json.Unmarshal(s.Bytes(), &m) != nil {
			return
		}
		if len(m.ID) == 0 {
			continue
		}
		response := rpc.Message{ID: m.ID, Result: json.RawMessage(`{}`)}
		switch {
		case mode == "init-error" && m.Method == "initialize":
			response.Result = nil
			response.Error = &rpc.Error{Code: -32603, Message: "initialization rejected"}
		case m.Method == "thread/start":
			response.Result = json.RawMessage(`{"thread":{"id":"fixture-thread"}}`)
		case mode == "upstream" && m.Method == "model/list":
			response.Result = nil
			response.Error = &rpc.Error{Code: -32603, Message: "HTTP 500 provider fixture", Data: json.RawMessage(`{"stage":"model/list"}`)}
		case mode == "broken-config" && m.Method == "model/list":
			fmt.Fprintln(os.Stdout, "broken JSON")
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(response)
	}
}

func TestFailedInitializeDetachesConnection(t *testing.T) {
	t.Setenv("RUNDESK_APP_FAILURE_FIXTURE", "init-error")
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	for n := 0; n < 2; n++ {
		_, err := m.ConfigCall(wid, "model/list", map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "initialize") {
			t.Fatal(err)
		}
		h, _ := m.getHandle("config-" + DefaultInstance + "-" + wid)
		h.mu.Lock()
		retained := h.client != nil
		h.mu.Unlock()
		if retained {
			t.Fatal("failed initialization left a cached connection")
		}
	}
}

func TestBrokenConfigConnectionIsReplacedOnNextRequest(t *testing.T) {
	t.Setenv("RUNDESK_APP_FAILURE_FIXTURE", "broken-config")
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	_, err := m.ConfigCall(wid, "model/list", map[string]any{})
	var transport *rpc.TransportError
	if !errors.As(err, &transport) {
		t.Fatal(err)
	}
	h, _ := m.getHandle("config-" + DefaultInstance + "-" + wid)
	h.mu.Lock()
	retained := h.client != nil
	h.mu.Unlock()
	if retained {
		t.Fatal("broken connection retained")
	}
	// A new explicitly requested operation gets a fresh process. No failed
	// mutation is retried automatically.
	t.Setenv("RUNDESK_APP_FAILURE_FIXTURE", "")
	if _, err = m.ConfigCall(wid, "model/list", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestMissingTurnAcknowledgementDoesNotStayRunning(t *testing.T) {
	t.Setenv("RUNDESK_APP_FAILURE_FIXTURE", "bad-turn")
	m := testManager(t)
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	if _, err := m.Start(s.ID, Input{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		current, _ := m.Session(s.ID)
		if current.Status == "failed" {
			if !strings.Contains(current.Error, "turn.id") {
				t.Fatal(current.Error)
			}
			events, err := m.Store.Events(s.ID, 0, 1000)
			if err != nil {
				t.Fatal(err)
			}
			starts := 0
			for _, ev := range events {
				if ev.Direction == "out" && ev.Method == "turn/start" {
					starts++
				}
			}
			if starts != 1 {
				t.Fatalf("turn/start sent %d times", starts)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("invalid acknowledgement stranded the session")
}

func TestRuntimeStoppedRejectsNewWork(t *testing.T) {
	m := testManager(t)
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	m.cancel()
	result := v1Request(NewHandler(m, "", true), "POST", "/sessions/"+s.ID+"/turns", `{"text":"do not start"}`, "stopped-runtime-123")
	if result.Code != 503 || object(t, result)["code"] != "runtime_unavailable" {
		t.Fatal(result.Code, result.Body.String())
	}
	current, _ := m.Session(s.ID)
	if current.Status != "idle" || current.RunID != "" || m.loaded.Load() != 0 {
		t.Fatal(current)
	}
}

func TestInputJournalFailureDoesNotSubmitToCodex(t *testing.T) {
	m := testManager(t)
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	db, err := sql.Open("sqlite", filepath.Join(m.Data, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER reject_input BEFORE INSERT ON events WHEN NEW.method='run/input' BEGIN SELECT RAISE(FAIL,'fixture journal failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	r := v1Request(NewHandler(m, "", true), "POST", "/sessions/"+s.ID+"/turns", `{"text":"do not execute"}`, "journal-failure-123")
	if r.Code != 503 || object(t, r)["code"] != "journal_unavailable" {
		t.Fatal(r.Code, r.Body.String())
	}
	current, _ := m.Session(s.ID)
	if current.Status != "failed" || m.loaded.Load() != 0 {
		t.Fatal(current)
	}
	events, err := m.Store.Events(s.ID, 0, 100)
	if err != nil || len(events) != 0 {
		t.Fatal(events, err)
	}
}

func TestModelListUpstreamErrorIsNotLocal500(t *testing.T) {
	t.Setenv("RUNDESK_APP_FAILURE_FIXTURE", "upstream")
	m := testManager(t)
	r := v1Request(NewHandler(m, "", true), "GET", "/workspaces/"+m.Workspaces()[0].ID+"/models", "", "")
	d := object(t, r)
	if r.Code != 502 || d["code"] != "codex_rpc_error" || !strings.Contains(d["error"].(string), "HTTP 500 provider fixture") || d["details"].(map[string]any)["origin"] != "codex_rpc" {
		t.Fatal(r.Code, d)
	}
}

func TestRequestReceiptStorageFailureIsDiagnosable(t *testing.T) {
	m := testManager(t)
	_ = m.Store.Close()
	r := v1Request(NewHandler(m, "", true), "GET", "/requests/storage-failure-123", "", "")
	d := object(t, r)
	if r.Code != 503 || d["code"] != "storage_unavailable" || d["requestId"] == "" || !strings.Contains(r.Body.String(), "database is closed") {
		t.Fatal(r.Code, d)
	}
}

func TestIncompleteReceiptDoesNotPanicOrRepeatMutation(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	key := "invalid-receipt-123"
	first := v1Request(h, "POST", "/workspaces", `{"name":"once"}`, key)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	count := len(m.Workspaces())
	var receipt requestRecord
	if err := m.Store.Get("api-request", requestStoreKey(key), &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.HTTPStatus = 0
	if err := m.Store.Put("api-request", requestStoreKey(key), receipt); err != nil {
		t.Fatal(err)
	}
	replay := v1Request(h, "POST", "/workspaces", `{"name":"once"}`, key)
	if replay.Code != 503 || object(t, replay)["code"] != "receipt_invalid" || len(m.Workspaces()) != count {
		t.Fatal(replay.Code, replay.Body.String())
	}
}

func TestErrorClassificationAndLegacyCompatibility(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"timeout", fmt.Errorf("Codex model/list: %w", context.DeadlineExceeded), 504, "codex_timeout"},
		{"exit", &rpc.TransportError{Op: "exited", Cause: io.EOF}, 503, "codex_unavailable"},
		{"invalid-json", &rpc.TransportError{Op: "invalid JSON", Cause: errors.New("bad JSON")}, 502, "codex_protocol_error"},
		{"parameters", &rpc.Error{Code: -32602, Message: "bad argument"}, 400, "codex_invalid_params"},
		{"unsupported", &rpc.Error{Code: -32601, Message: "missing method"}, 502, "codex_method_unsupported"},
		{"rollback", &apiError{Status: 500, Code: "skill_restore_required", Message: "backup retained", Cause: io.ErrClosedPipe}, 500, "skill_restore_required"},
		{"bug", errors.New("internal fixture"), 500, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, prefix := range []string{"/api/v1/", "/api/"} {
				h := (&Server{}).apiBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAPIError(w, 500, tt.err) }))
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost"+prefix+"test", nil))
				want := tt.status
				if prefix == "/api/" {
					want = 500
				}
				if w.Code != want || object(t, w)["code"] != tt.code || object(t, w)["retryable"] != false {
					t.Fatal(w.Code, w.Body.String())
				}
			}
		})
	}
}
