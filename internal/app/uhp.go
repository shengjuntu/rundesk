package app

// UHP is a wire adapter over the task kernel. Native runtimes remain behind
// TaskControl; these records are protocol identities/projections, not a queue.
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

const UHPVersion = "2026-10-19"

type uhpError struct {
	Type    string  `json:"type"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Param   *string `json:"param"`
	Detail  any     `json:"detail"`
}

type uhpRecord struct {
	BeforeFiles   []string
	Owner         string
	Harness       uhpHarness
	HostSessionID string
	SessionID     string
	ContainerID   string
	TaskID        string
	Projection    uhpProjection
	Input         json.RawMessage
	Cursor        int64
	Deleted       bool
	CancelReason  string
	Deadline      int64
	MaxStep       int // Legacy records only; never treat model steps as UHP tool rounds.
	CompletedAt   int64
}
type uhpSessionRecord struct {
	ID            string
	Owner         string
	Harness       uhpHarness
	HostSessionID string
	ContainerID   string
	ResponseIDs   []string
	CreatedAt     int64
	Deleted       bool
}
type uhpReceipt struct{ ResponseID string }
type uhpService struct{ server *Server }

func uhpOwner(r *http.Request) string {
	if k := principal(r); k != nil {
		return "application-key:" + k.ID
	}
	return "administrator"
}
func uhpFail(w http.ResponseWriter, status int, code, message string) {
	kind := "invalid_request_error"
	switch {
	case status == 401:
		kind = "authentication_error"
	case status == 403:
		kind = "permission_error"
	case status == 429:
		kind = "rate_limit_error"
	case status >= 500:
		kind = "server_error"
	}
	var detail any
	if code == "file_too_large" {
		detail = map[string]any{"max_bytes": UHPFileLimit}
	}
	writeUHPJSON(w, status, map[string]any{"error": uhpError{Type: kind, Code: code, Message: message, Detail: detail}})
}
func uhpWriteError(w http.ResponseWriter, err error) {
	var a *apiError
	if errors.As(err, &a) {
		// Native errors can include host paths/credentials. Only stable boundary
		// descriptions are exposed; detailed causes stay in the RDCA diagnostic log.
		code := a.Code
		messages := map[string]string{"invalid_input": "The request input is invalid.", "harness_not_found": "Harness not found.", "response_not_found": "Response not found.", "session_not_found": "Session not found.", "file_not_found": "File not found.", "session_expired": "The session has expired.", "harness_mismatch": "The configured harness differs from the continued session.", "session_busy": "The session already has a task in progress.", "model_unavailable": "The model is unavailable for this harness.", "file_too_large": "The file exceeds the size limit.", "insufficient_scope": "The credential does not permit this operation.", "preview_unavailable": "This server does not render file previews.", "missing_credential": "A bearer credential is required.", "rundesk_capability_unsupported": "A required execution capability is unavailable for this harness.", "task_deadline_exceeded": "The task deadline expired before admission.", "invalid_credential": "The bearer credential is invalid."}
		message, ok := messages[code]
		if !ok {
			if !strings.HasPrefix(code, "rundesk_") {
				code = "rundesk_" + code
			}
			message = "RunDesk could not perform this operation."
		}
		if a.Status == 429 {
			code = "rate_limited"
			message = "Task admission is temporarily limited."
		}
		if code == "file_too_large" {
			writeUHPJSON(w, a.Status, map[string]any{"error": uhpError{Type: "invalid_request_error", Code: code, Message: message, Detail: map[string]any{"max_bytes": UHPFileLimit}}})
			return
		}
		uhpFail(w, a.Status, code, message)
		return
	}
	uhpFail(w, 500, "rundesk_storage_error", "The server could not complete this operation.")
}
func uhpScope(r *http.Request, scope string) bool {
	k := principal(r)
	return k == nil || k.scope(scope)
}

func (s *Server) uhpBoundary(next http.Handler) http.Handler {
	u := &uhpService{server: s}
	mux := http.NewServeMux()
	u.routes(mux)
	// Recovery and budgets live with Manager, never with a request/connection.
	s.Manager.wg.Add(1)
	go func() {
		defer s.Manager.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.Manager.ctx.Done():
				return
			case <-ticker.C:
				u.tick()
			}
		}
	}()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("UHP-Version", UHPVersion)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if !s.uhpTransportAllowed(r) {
			uhpFail(w, 403, "insufficient_scope", "The request origin is not permitted.")
			return
		}
		if version := r.Header.Get("UHP-Version"); version != "" && version != UHPVersion {
			writeUHPJSON(w, 400, map[string]any{"error": uhpError{Type: "invalid_request_error", Code: "unsupported_protocol_version", Message: "The requested protocol version is unsupported.", Detail: map[string]any{"supported": []string{UHPVersion}}}})
			return
		}
		// Prevent ServeMux's path-clean redirect from turning traversal into an
		// authenticated request at a different resource.
		if strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "//") {
			uhpFail(w, 404, "file_not_found", "Resource not found.")
			return
		}
		for _, seg := range strings.Split(r.URL.Path, "/") {
			if seg == ".." || seg == "." {
				uhpFail(w, 404, "file_not_found", "Resource not found.")
				return
			}
		}
		if r.Method == "GET" && r.URL.Path == "/v1/uhp" {
			u.discovery(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			uhpFail(w, 401, "missing_credential", "A bearer credential is required.")
			return
		}
		secret, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || secret == "" || strings.ContainsAny(secret, " \r\n\t") {
			uhpFail(w, 401, "invalid_credential", "The bearer credential is invalid.")
			return
		}
		var err error
		r, err = s.authorize(r)
		if err != nil || human(r) != nil {
			uhpFail(w, 401, "invalid_credential", "The bearer credential is invalid.")
			return
		}
		// UHP intentionally accepts service/admin tokens only; human accounts keep
		// their existing project gate on RDCA rather than bypassing it here.
		if !uhpScope(r, "read") && r.Method == "GET" {
			uhpFail(w, 403, "insufficient_scope", "Read scope is required.")
			return
		}
		if r.Method != "GET" && !uhpScope(r, "run") {
			uhpFail(w, 403, "insufficient_scope", "Run scope is required.")
			return
		}
		if (strings.Contains(r.URL.Path, "/files") || strings.Contains(r.URL.Path, "/containers/")) && !uhpScope(r, "files") {
			uhpFail(w, 403, "insufficient_scope", "Files scope is required.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (u *uhpService) discovery(w http.ResponseWriter, r *http.Request) {
	writeUHPJSON(w, 200, map[string]any{"object": "uhp.discovery", "protocol": "uhp", "versions": []string{UHPVersion}, "default_version": UHPVersion, "conformance_class": "core", "capabilities": map[string]bool{"streaming": true, "sessions": true, "cancellation": true, "files_input": true, "files_output": true, "session_listing": true, "harness_management": false, "harness_retirement": false, "input_history": false, "session_sharing": false, "idempotency": true, "plugins": false, "environments": false, "memories": false}, "rundesk_execution": uhpExecutionSemantics(), "rundesk_extensions": map[string]any{"required_capabilities": "metadata.rundesk_require", "progress": "response.metadata.rundesk_progress"}, "implementation": map[string]string{"name": "RunDesk", "version": Version, "limitations": "Tool-round and output-token request budgets are unsupported; use required capabilities for strict admission. Images require a supporting Codex model. Instructions are retained task text."}})
}
func (u *uhpService) loadResponse(r *http.Request, id string, allowDeleted bool) (uhpRecord, error) {
	var v uhpRecord
	err := u.server.Manager.Store.Get("uhp-response", id, &v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if err != nil || v.Owner != uhpOwner(r) || (!allowDeleted && v.Deleted) {
		return v, failure(404, "response_not_found", "")
	}
	var ss uhpSessionRecord
	if err = u.server.Manager.Store.Get("uhp-session", v.SessionID, &ss); err != nil || ss.Deleted {
		return v, failure(404, "response_not_found", "")
	}
	if k := principal(r); k != nil && !k.owns(v.Harness.InstanceID, v.Harness.WorkspaceID, v.Harness.Source) {
		return v, failure(404, "response_not_found", "")
	}
	return v, nil
}
func (u *uhpService) loadSession(r *http.Request, id string) (uhpSessionRecord, error) {
	var v uhpSessionRecord
	err := u.server.Manager.Store.Get("uhp-session", id, &v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if err != nil || v.Deleted || v.Owner != uhpOwner(r) {
		return v, failure(404, "session_not_found", "")
	}
	if k := principal(r); k != nil && !k.owns(v.Harness.InstanceID, v.Harness.WorkspaceID, v.Harness.Source) {
		return v, failure(404, "session_not_found", "")
	}
	return v, nil
}
func (u *uhpService) tick() {
	m := u.server.Manager
	m.uhpMu.Lock()
	defer m.uhpMu.Unlock()
	if m.ctx.Err() != nil {
		return
	}
	rows, err := m.Store.List("uhp-response")
	if err != nil {
		return
	}
	for _, raw := range rows {
		var v uhpRecord
		if json.Unmarshal(raw, &v) != nil || v.Projection.Response.Status != "in_progress" {
			continue
		}
		_ = u.refresh(&v)
	}
}

// Caller holds Manager.uhpMu, which serializes every facade projection/admission.
func (u *uhpService) refresh(v *uhpRecord) error {
	if v.Projection.Response.Status != "in_progress" {
		return nil
	}
	m := u.server.Manager
	t, err := u.server.taskControl().Task(v.TaskID)
	if err != nil {
		return err
	}
	if ss, e := m.Session(t.SessionID); e == nil && ss.RunID == t.RunID && ss.TurnID != "" {
		v.Projection.NativeTurnID = ss.TurnID
	}
	// The session receives its terminal native result before the queue's periodic
	// reconciliation. Use it to avoid cancelling already-completed work.
	if ss, e := m.Session(t.SessionID); e == nil && t.RunID != "" && ss.RunID == t.RunID && !active(ss.Status) && t.Status != "queued" {
		t.Status = ss.Status
	}
	if _, err = u.updateProgress(v, t); err != nil {
		return err
	}
	if err = u.drainNative(v, t); err != nil {
		return err
	}
	pending := taskPending(t.Status)
	if t.StopReason == "timeout" {
		v.CancelReason = "timeout"
	}
	if pending && v.CancelReason == "" {
		if v.Deadline > 0 && time.Now().UnixNano() >= v.Deadline {
			v.CancelReason = "timeout"
		}
		if v.MaxStep > 0 {
			// A pre-upgrade record accepted a model-step approximation. Stop it
			// explicitly rather than silently removing its promised constraint.
			v.CancelReason = "legacy_step_budget_unsupported"
		}
	}
	if pending && v.CancelReason != "" {
		// Persist intent before issuing a native stop. A crash cannot turn a budget
		// stop into completed, nor lose the explicit cancellation on restart.
		if err = m.Store.Put("uhp-response", v.Projection.Response.ID, v); err != nil {
			return err
		}
		if _, err = u.server.taskControl().CancelTask(t.ID); err != nil {
			return err
		}
		t, err = u.server.taskControl().Task(t.ID)
		if err != nil {
			return err
		}
		pending = taskPending(t.Status)
	}
	if !pending {
		// Stop may emit final fragments while winding down. Observe terminal
		// state first, then drain through that boundary before freezing output.
		if err = u.drainNative(v, t); err != nil {
			return err
		}
		status := "failed"
		var terminalError *uhpError
		switch t.Status {
		case "completed":
			status = "completed"
		case "interrupted", "canceled", "cancelled":
			status = "cancelled"
		}
		if v.CancelReason == "cancelled" {
			status = "cancelled"
		}
		if v.CancelReason == "timeout" || v.CancelReason == "max_step" || v.CancelReason == "legacy_step_budget_unsupported" {
			status = "incomplete"
			v.Projection.Response.IncompleteDetails = map[string]any{"reason": v.CancelReason}
		}
		if status == "failed" {
			terminalError = &uhpError{Type: "harness_error", Code: "harness_error", Message: "The harness could not complete the task. Inspect the RunDesk task diagnostics."}
		}
		files, e := (&uhpFiles{Manager: m}).DiscoverArtifacts(v.Owner, v.SessionID, v.HostSessionID, v.ContainerID)
		if e != nil {
			v.Projection.Response.Metadata["rundesk_artifacts_unavailable"] = true
			files = nil
		}
		before := map[string]bool{}
		for _, id := range v.BeforeFiles {
			before[id] = true
		}
		annotations := []map[string]any{}
		for _, f := range files {
			if before[f.ID] || f.Availability != "available" {
				continue
			}
			annotations = append(annotations, map[string]any{"type": "container_file_citation", "container_id": f.ContainerID, "file_id": f.ID, "filename": f.Filename, "download_url": "/v1/containers/" + f.ContainerID + "/files/" + f.ID + "/content", "start_index": 0, "end_index": 0})
		}
		if len(annotations) > 0 {
			v.Projection.AddAnnotations(annotations)
		}
		previousStatus := v.Projection.Response.Status
		v.Projection.Response.Status = status
		_, err = u.updateProgress(v, t)
		v.Projection.Response.Status = previousStatus
		if err != nil {
			return err
		}
		v.Projection.Finish(status, terminalError)
		v.CompletedAt = time.Now().Unix()
	} else if _, err = u.updateProgress(v, t); err != nil {
		return err
	}
	return m.Store.Put("uhp-response", v.Projection.Response.ID, v)
}
func (u *uhpService) read(r *http.Request, id string) (uhpRecord, error) {
	return u.readRetained(r, id, false)
}
func (u *uhpService) readRetained(r *http.Request, id string, deleted bool) (uhpRecord, error) {
	m := u.server.Manager
	m.uhpMu.Lock()
	defer m.uhpMu.Unlock()
	v, err := u.loadResponse(r, id, deleted)
	if err != nil {
		return v, err
	}
	err = u.refresh(&v)
	return v, err
}
func (u *uhpService) wait(r *http.Request, id string) (uhpRecord, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !u.server.credentialStillValid(r) {
			return uhpRecord{}, failure(401, "invalid_credential", "")
		}
		v, err := u.readRetained(r, id, true)
		if err != nil || v.Projection.Response.Status != "in_progress" {
			return v, err
		}
		select {
		case <-r.Context().Done():
			return v, r.Context().Err()
		case <-u.server.Manager.ctx.Done():
			return v, context.Canceled
		case <-ticker.C:
		}
	}
}
func uhpSortedMetadata(in map[string]any, ignored []string, harness, session string) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if strings.HasPrefix(k, "rundesk_") {
			continue // Server-owned metadata cannot be forged by a caller.
		}
		out[k] = v
	}
	delete(out, "ignored_fields")
	delete(out, "model_fallback")
	delete(out, "requested_model")
	delete(out, "model_fallback_reason")
	out["harness_id"] = harness
	out["session_id"] = session
	if len(ignored) > 0 {
		sort.Strings(ignored)
		out["ignored_fields"] = ignored
	}
	return out
}

func (u *uhpService) drainNative(v *uhpRecord, t Task) error {
	m := u.server.Manager
	if t.RunID != "" {
		for {
			events, e := m.Store.Events(t.SessionID, v.Cursor, 1000)
			if e != nil {
				return e
			}
			for _, ev := range events {
				// List views may redact/trim payloads; translation uses the original event.
				original, e := m.Store.Event(t.SessionID, ev.ID)
				if e != nil {
					return e
				}
				v.Projection.Apply(original, t.RunID)
				v.Cursor = ev.ID
			}
			if len(events) < 1000 {
				break
			}
		}
	}
	return nil
}

func writeUHPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
