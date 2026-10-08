package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

const Version = "0.17.0"

type apiError struct {
	Status        int
	Code, Message string
	Retryable     bool
	Cause         error
}

func (e *apiError) Error() string { return e.Message }
func (e *apiError) Unwrap() error { return e.Cause }
func failure(status int, code, message string) error {
	return &apiError{Status: status, Code: code, Message: message}
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	code := map[int]string{400: "invalid_request", 401: "unauthorized", 403: "forbidden", 404: "not_found", 405: "method_not_allowed", 409: "conflict", 413: "body_too_large", 429: "rate_limited", 500: "internal_error", 502: "upstream_error", 503: "unavailable", 504: "upstream_timeout"}[status]
	if code == "" {
		code = "request_failed"
	}
	retryable := false
	var typed *apiError
	var native *rpc.Error
	var transport *rpc.TransportError
	if !errors.As(err, &typed) {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			typed = &apiError{Status: 504, Code: "codex_timeout"}
		case errors.Is(err, context.Canceled):
			typed = &apiError{Status: 503, Code: "operation_canceled"}
		case errors.As(err, &native):
			typed = &apiError{Status: 502, Code: "codex_rpc_error"}
			if native.Code == -32602 {
				typed.Status, typed.Code = 400, "codex_invalid_params"
			}
			if native.Code == -32601 {
				typed.Code = "codex_method_unsupported"
			}
		case errors.As(err, &transport):
			typed = &apiError{Status: 503, Code: "codex_unavailable"}
			if transport.Op == "invalid JSON" || transport.Op == "read" {
				typed.Status, typed.Code = 502, "codex_protocol_error"
			}
		}
	}
	if typed != nil {
		code, retryable = typed.Code, typed.Retryable
		if w.Header().Get("RunDesk-API-Version") == "v1" {
			status = typed.Status
		}
	}
	var large *http.MaxBytesError
	if errors.As(err, &large) {
		code = "body_too_large"
		if w.Header().Get("RunDesk-API-Version") == "v1" {
			status = 413
		}
	}
	writeJSON(w, status, map[string]any{"error": diagnosticText(err.Error()), "code": code, "requestId": w.Header().Get("X-Request-ID"), "retryable": retryable, "details": errorDiagnostic(w, status, code, err)})
}

var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{7,127}$`)

// Rewrite only the version prefix, then use precisely the same guard and handlers.
// No response buffering here: SSE and file downloads remain streaming.
func (s *Server) apiBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			id := r.Header.Get("X-Request-ID")
			if !safeRequestID.MatchString(id) {
				id = store.ID()
			}
			w.Header().Set("X-Request-ID", id)
			if strings.HasPrefix(r.URL.Path, "/api/v1/") {
				w.Header().Set("RunDesk-API-Version", "v1")
				r = r.Clone(r.Context())
				u := *r.URL
				r.URL = &u
				r.URL.Path = "/api/" + strings.TrimPrefix(r.URL.Path, "/api/v1/")
				if r.URL.RawPath != "" {
					r.URL.RawPath = strings.Replace(r.URL.RawPath, "/api/v1/", "/api/", 1)
				}
			} else {
				w.Header().Set("RunDesk-API-Version", "legacy")
			}
		}
		diagnosticBoundary(w, r, next)
	})
}

type SessionSource struct {
	Kind   string `json:"kind,omitempty"`
	AppID  string `json:"appId,omitempty"`
	TaskID string `json:"taskId,omitempty"`
}

func (v *SessionSource) validate() error {
	if v.Kind == "" {
		v.Kind = "human"
		if v.AppID != "" {
			v.Kind = "application"
		}
	}
	if (v.Kind != "human" && v.Kind != "application") || len(v.AppID) > 80 || len(v.TaskID) > 200 || strings.ContainsAny(v.AppID+v.TaskID, "\r\n\x00") || (v.Kind == "application" && strings.TrimSpace(v.AppID) == "") || (v.Kind == "human" && (v.AppID != "" || v.TaskID != "")) {
		return failure(400, "invalid_source", "source 必须是 human，或带 appId 的 application；taskId 为可选业务编号")
	}
	return nil
}

type requestRecord struct {
	Key         string          `json:"key"`
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	State       string          `json:"state"`
	HTTPStatus  int             `json:"httpStatus,omitempty"`
	Response    json.RawMessage `json:"response,omitempty"`
	Created     string          `json:"created"`
	Fingerprint string          `json:"fingerprint"`
	Owner       string          `json:"owner"`
}

func requestStoreKey(key string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(key))) }
func supportsIdempotency(r *http.Request) bool {
	if r.Method != "POST" {
		return false
	}
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return len(p) == 4 && p[1] == "workspaces" && p[3] == "mcp-tests" || len(p) == 2 && (p[1] == "instances" || p[1] == "workspaces" || p[1] == "sessions" || p[1] == "tasks" || p[1] == "schedules" || p[1] == "collaborations") || len(p) == 4 && p[1] == "sessions" && (p[3] == "turns" || p[3] == "recover")
}

type recordedResponse struct {
	header     http.Header
	code       int
	body       bytes.Buffer
	diagnostic *requestDiagnostic
}

func (w *recordedResponse) requestDiagnostic() *requestDiagnostic { return w.diagnostic }

func (w *recordedResponse) Header() http.Header { return w.header }
func (w *recordedResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *recordedResponse) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	return w.body.Write(p)
}

func (s *Server) idempotent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !supportsIdempotency(r) {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" && w.Header().Get("RunDesk-API-Version") != "v1" {
			next.ServeHTTP(w, r)
			return
		}
		if !safeRequestID.MatchString(key) {
			writeErr(w, 400, failure(400, "idempotency_key_required", "此接口需要 8–128 字符的 Idempotency-Key；同一操作重试时保持不变"))
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		var canonical any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err = decoder.Decode(&canonical); err != nil {
			writeErr(w, 400, err)
			return
		}
		if _, ok := canonical.(map[string]any); !ok {
			writeErr(w, 400, failure(400, "invalid_request", "请求必须为 JSON 对象"))
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			writeErr(w, 400, failure(400, "invalid_request", "只接受单个 JSON 对象"))
			return
		}
		encoded, _ := json.Marshal(canonical)
		fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(r.Method+"\n"+r.URL.Path+"\n"+r.URL.Query().Encode()+"\n"+string(encoded))))
		m := s.Manager
		m.requestMu.Lock()
		var record requestRecord
		err = m.Store.Get("api-request", requestStoreKey(requestNamespace(r, key)), &record)
		if err == nil {
			pending := m.pendingRequests[requestNamespace(r, key)]
			m.requestMu.Unlock()
			if record.Fingerprint != fingerprint {
				writeErr(w, 409, failure(409, "idempotency_conflict", "同一 Idempotency-Key 已用于不同请求"))
				return
			}
			if record.State != "completed" {
				if record.Owner == m.requestOwner && pending {
					w.Header().Set("Retry-After", "1")
					writeErr(w, 409, &apiError{Status: 409, Code: "request_in_progress", Message: "原请求仍在处理；请用相同 Key 查询或重试", Retryable: true})
				} else {
					writeErr(w, 409, failure(409, "request_unconfirmed", "原请求在服务重启前未完成记录；不会重新执行，请检查会话及事件"))
				}
				return
			}
			if record.HTTPStatus < 200 || record.HTTPStatus > 599 || !json.Valid(record.Response) {
				writeErr(w, 503, failure(503, "receipt_invalid", "原请求的接收记录不完整，无法安全回放；操作未重新执行，请核对会话与事件，不要更换 Key 重发"))
				return
			}
			w.Header().Set("Idempotency-Replayed", "true")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			// A receipt is the original acknowledgement, not the current session status.
			w.WriteHeader(record.HTTPStatus)
			_, _ = w.Write(record.Response)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			m.requestMu.Unlock()
			writeErr(w, 503, failure(503, "storage_unavailable", "无法读取提交记录，操作未执行"))
			return
		}
		record = requestRecord{Key: key, Method: r.Method, Path: strings.TrimPrefix(r.URL.Path, "/api"), State: "processing", Created: store.Now(), Fingerprint: fingerprint, Owner: m.requestOwner}
		err = m.Store.Put("api-request", requestStoreKey(requestNamespace(r, key)), record)
		if err == nil {
			m.pendingRequests[requestNamespace(r, key)] = true
		}
		m.requestMu.Unlock()
		if err != nil {
			writeErr(w, 503, failure(503, "storage_unavailable", "无法保存提交记录，操作未执行"))
			return
		}
		defer func() { m.requestMu.Lock(); delete(m.pendingRequests, requestNamespace(r, key)); m.requestMu.Unlock() }()
		r.Body = io.NopCloser(bytes.NewReader(body))
		reply := &recordedResponse{header: w.Header().Clone(), diagnostic: requestDiagnosticOf(w)}
		next.ServeHTTP(reply, r)
		if reply.code == 0 {
			reply.code = 200
		}
		record.State = "completed"
		record.HTTPStatus = reply.code
		record.Response = append(json.RawMessage(nil), reply.body.Bytes()...)
		if !json.Valid(record.Response) {
			record.Response = json.RawMessage(`{"error":"handler did not return JSON"}`)
		}
		m.requestMu.Lock()
		err = m.Store.Put("api-request", requestStoreKey(requestNamespace(r, key)), record)
		m.requestMu.Unlock()
		if err != nil {
			writeErr(w, 503, failure(503, "request_unconfirmed", "操作可能已经执行，但接收记录保存失败；请检查会话，不要更换 Key 重发"))
			return
		}
		for k, v := range reply.header {
			w.Header()[k] = v
		}
		w.Header().Set("Idempotency-Replayed", "false")
		w.WriteHeader(reply.code)
		_, _ = w.Write(reply.body.Bytes())
	})
}

func (s *Server) integrationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/requests/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !safeRequestID.MatchString(key) {
			writeErr(w, 400, failure(400, "invalid_request", "无效请求 Key"))
			return
		}
		var record requestRecord
		s.Manager.requestMu.Lock()
		err := s.Manager.Store.Get("api-request", requestStoreKey(requestNamespace(r, key)), &record)
		pending := s.Manager.pendingRequests[requestNamespace(r, key)]
		s.Manager.requestMu.Unlock()
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, failure(404, "request_not_found", "没有找到该请求"))
			return
		}
		if err != nil {
			writeErr(w, 503, &apiError{Status: 503, Code: "storage_unavailable", Message: "无法读取请求记录；请检查数据目录和数据库状态", Cause: err})
			return
		}
		if record.State != "completed" && (record.Owner != s.Manager.requestOwner || !pending) {
			record.State = "unconfirmed"
		}
		writeJSON(w, 200, map[string]any{"key": record.Key, "method": record.Method, "path": record.Path, "state": record.State, "httpStatus": record.HTTPStatus, "response": record.Response, "created": record.Created})
	})
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAPISpec)
	})
	mux.HandleFunc("GET /api/instances/{iid}/configuration", s.instanceConfiguration)
	mux.HandleFunc("GET /api/sessions/{sid}/configuration", s.sessionConfiguration)
}
