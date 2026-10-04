package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

func TestHTTPDiagnosticErrorDetails(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	s := &Server{}
	h := s.apiBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, 500, fmt.Errorf("loading provider: %w", &rpc.Error{Code: -32001, Message: "HTTP 500 upstream failure; api_key=secret-value", Data: json.RawMessage(`{"stage":"turn/start","authorization":"Bearer secret-value","token":"secret-value","nested":{"message":"password=secret-value"}}`)}))
	}))
	req := httptest.NewRequest("GET", "http://localhost/api/v1/workspaces/demo/config?token=do-not-log", nil)
	req.Header.Set("X-Request-ID", "request-test-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	data := object(t, w)
	details := data["details"].(map[string]any)
	if w.Code != 500 || data["requestId"] != "request-test-123" || details["path"] != "/api/v1/workspaces/demo/config" || details["origin"] != "codex_rpc" || details["rpcCode"] != float64(-32001) {
		t.Fatal(w.Code, data)
	}
	if len(details["causes"].([]any)) != 2 || details["time"] == "" || details["durationMs"] == nil {
		t.Fatal(details)
	}
	if strings.Contains(w.Body.String(), "secret-value") || strings.Contains(logs.String(), "secret-value") || strings.Contains(logs.String(), "do-not-log") || !strings.Contains(logs.String(), "request-test-123") {
		t.Fatal("unsafe or uncorrelated diagnostics")
	}
}

func TestHTTPDiagnosticsPanicAndStreaming(t *testing.T) {
	s := &Server{}
	panicHandler := s.apiBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("private panic payload") }))
	w := httptest.NewRecorder()
	panicHandler.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/api/v1/test", nil))
	if w.Code != 500 || object(t, w)["code"] != "internal_panic" || strings.Contains(w.Body.String(), "private panic payload") {
		t.Fatal(w.Code, w.Body.String())
	}
	stream := s.apiBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
	}))
	w = httptest.NewRecorder()
	stream.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/api/v1/test", nil))
	if !w.Flushed || w.Body.String() != "data: ready\n\n" {
		t.Fatal("SSE changed", w.Body.String())
	}
	broken := s.apiBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Error("stream abort swallowed")
			}
		}()
		broken.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://localhost/api/v1/test", nil))
	}()
}

func TestHTTPDiagnosticReceiptRetainsContext(t *testing.T) {
	m := testManager(t)
	s := &Server{Manager: m}
	h := s.apiBoundary(s.idempotent(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAPIError(w, 500, errors.New("fixture failure")) })))
	first := v1Request(h, "POST", "/sessions", `{}`, "diagnostic-receipt-123")
	second := v1Request(h, "POST", "/sessions", `{}`, "diagnostic-receipt-123")
	if first.Code != 500 || second.Code != 500 || second.Header().Get("Idempotency-Replayed") != "true" || !reflect.DeepEqual(object(t, first), object(t, second)) {
		t.Fatal("receipt semantics changed")
	}
	if object(t, first)["details"].(map[string]any)["path"] != "/api/v1/sessions" {
		t.Fatal(first.Body.String())
	}
}
