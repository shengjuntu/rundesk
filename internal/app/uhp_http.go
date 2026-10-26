package app

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func (s *Server) uhpTransportAllowed(r *http.Request) bool {
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	configured := ""
	if v, e := url.Parse(s.PublicOrigin); e == nil {
		configured = v.Host
	}
	if s.Local && r.Host != configured && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		v, e := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		valid := e == nil && v.Host == r.Host && v.Scheme == scheme
		if s.PublicOrigin != "" {
			valid = origin == s.PublicOrigin && r.Host == configured
		}
		return valid
	}
	return true
}
func (u *uhpService) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/harnesses", func(w http.ResponseWriter, r *http.Request) {
		hs, e := u.server.uhpHarnesses(r)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		out := []any{}
		for _, h := range hs {
			out = append(out, h.Wire())
		}
		writeUHPJSON(w, 200, map[string]any{"harnesses": out})
	})
	mux.HandleFunc("GET /v1/harnesses/{hid}", func(w http.ResponseWriter, r *http.Request) {
		h, e := u.harness(r, r.PathValue("hid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		writeUHPJSON(w, 200, h.Wire())
	})
	mux.HandleFunc("GET /v1/harnesses/{hid}/models", func(w http.ResponseWriter, r *http.Request) {
		h, e := u.harness(r, r.PathValue("hid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		out, e := u.server.uhpModels(h)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		writeUHPJSON(w, 200, out)
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		hs, e := u.server.uhpHarnesses(r)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		backends := map[string]any{}
		for _, h := range hs {
			out, e := u.server.uhpModels(h)
			if e != nil {
				uhpWriteError(w, e)
				return
			}
			backends[h.ID] = out
		}
		writeUHPJSON(w, 200, map[string]any{"backends": backends})
	})
	mux.HandleFunc("POST /v1/responses", u.create)
	mux.HandleFunc("GET /v1/responses/{rid}", func(w http.ResponseWriter, r *http.Request) {
		v, e := u.read(r, r.PathValue("rid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		if !v.Projection.Response.Store && v.Projection.Response.Status != "in_progress" {
			uhpFail(w, 404, "response_not_found", "Response not found.")
			return
		}
		writeUHPJSON(w, 200, v.Projection.Response)
	})
	mux.HandleFunc("GET /v1/responses/{rid}/events", func(w http.ResponseWriter, r *http.Request) { u.stream(w, r, r.PathValue("rid")) })
	mux.HandleFunc("GET /v1/responses/{rid}/input_items", func(w http.ResponseWriter, r *http.Request) {
		v, e := u.read(r, r.PathValue("rid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		if !v.Projection.Response.Store {
			uhpFail(w, 404, "response_not_found", "Response not found.")
			return
		}
		var input any
		_ = json.Unmarshal(v.Input, &input)
		if txt, ok := input.(string); ok {
			input = []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_text", "text": txt}}}}
		}
		writeUHPJSON(w, 200, map[string]any{"object": "list", "data": input, "has_more": false})
	})
	mux.HandleFunc("POST /v1/responses/{rid}/cancel", func(w http.ResponseWriter, r *http.Request) {
		v, e := u.cancel(r, r.PathValue("rid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		writeUHPJSON(w, 200, v.Projection.Response)
	})
	mux.HandleFunc("DELETE /v1/responses/{rid}", func(w http.ResponseWriter, r *http.Request) {
		m := u.server.Manager
		m.uhpMu.Lock()
		defer m.uhpMu.Unlock()
		v, e := u.loadResponse(r, r.PathValue("rid"), false)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		v.Deleted = true
		if e = m.Store.Put("uhp-response", v.Projection.Response.ID, v); e != nil {
			uhpWriteError(w, e)
			return
		}
		writeUHPJSON(w, 200, map[string]any{"id": v.Projection.Response.ID, "object": "response.deleted", "deleted": true})
	})
	mux.HandleFunc("GET /v1/sessions", u.sessions)
	mux.HandleFunc("GET /v1/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		v, e := u.loadSession(r, r.PathValue("sid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		writeUHPJSON(w, 200, u.sessionWire(v))
	})
	mux.HandleFunc("GET /v1/sessions/{sid}/turns", u.turns)
	mux.HandleFunc("POST /v1/sessions/{sid}/cancel", func(w http.ResponseWriter, r *http.Request) {
		v, e := u.loadSession(r, r.PathValue("sid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		for i := len(v.ResponseIDs) - 1; i >= 0; i-- {
			var rec uhpRecord
			if u.server.Manager.Store.Get("uhp-response", v.ResponseIDs[i], &rec) == nil && rec.Projection.Response.Status == "in_progress" {
				_, e = u.cancel(r, v.ResponseIDs[i])
				if e != nil {
					uhpWriteError(w, e)
					return
				}
				break
			}
		}
		writeUHPJSON(w, 200, u.sessionWire(v))
	})
	mux.HandleFunc("POST /v1/files", u.upload)
	mux.HandleFunc("GET /v1/sessions/{sid}/files", func(w http.ResponseWriter, r *http.Request) {
		v, e := u.loadSession(r, r.PathValue("sid"))
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		files, e := (&uhpFiles{Manager: u.server.Manager}).DiscoverArtifacts(v.Owner, v.ID, v.HostSessionID, v.ContainerID)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		out := []any{}
		for _, f := range files {
			out = append(out, f.Wire())
		}
		writeUHPJSON(w, 200, map[string]any{"files": out})
	})
	mux.HandleFunc("GET /v1/sessions/{sid}/files/archive", u.archive)
	mux.HandleFunc("GET /v1/containers/{cid}/files/{fid}/content", u.download)
	mux.HandleFunc("GET /v1/containers/{cid}/files/{fid}/pdf", func(w http.ResponseWriter, r *http.Request) {
		if _, e := u.file(r); e != nil {
			uhpWriteError(w, e)
			return
		}
		uhpFail(w, 501, "preview_unavailable", "This server does not render file previews.")
	})
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		uhpFail(w, 404, "rundesk_endpoint_not_found", "The UHP endpoint is unavailable.")
	})
}
func (u *uhpService) harness(r *http.Request, id string) (uhpHarness, error) {
	hs, e := u.server.uhpHarnesses(r)
	if e != nil {
		return uhpHarness{}, e
	}
	for _, h := range hs {
		if id == "" || h.ID == id {
			return h, nil
		}
	}
	return uhpHarness{}, failure(404, "harness_not_found", "")
}
func uhpReceiptID(owner, key string) string {
	hash := sha256.Sum256([]byte(owner + "\x00" + key))
	return hex.EncodeToString(hash[:])
}
func (u *uhpService) create(w http.ResponseWriter, r *http.Request) {
	m := u.server.Manager
	key := r.Header.Get("Idempotency-Key")
	if len(key) > 255 {
		uhpFail(w, 400, "invalid_input", "The idempotency key is too long.")
		return
	}
	// A retry uses the original receipt, even if its request body differs.
	if key != "" {
		var receipt uhpReceipt
		m.uhpMu.Lock()
		e := m.Store.Get("uhp-receipt", uhpReceiptID(uhpOwner(r), key), &receipt)
		m.uhpMu.Unlock()
		if e == nil {
			u.reply(w, r, receipt.ResponseID, false, false, true)
			return
		}
		if !errors.Is(e, sql.ErrNoRows) {
			uhpWriteError(w, e)
			return
		}
	}
	req, e := parseUHPRequest(r.Body)
	if e != nil {
		var limitErr *apiError
		if errors.As(e, &limitErr) && limitErr.Status == 413 {
			writeUHPJSON(w, 413, map[string]any{"error": uhpError{Type: "invalid_request_error", Code: "file_too_large", Message: "The JSON request exceeds the inline size limit; upload the file separately.", Detail: map[string]any{"max_bytes": uhpRequestLimit}}})
			return
		}
		uhpWriteError(w, e)
		return
	}
	var prev uhpRecord
	var ss uhpSessionRecord
	hid, _ := req.Metadata["harness_id"].(string)
	if req.PreviousResponseID != "" {
		prev, e = u.read(r, req.PreviousResponseID)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		if !prev.Projection.Response.Store {
			uhpFail(w, 404, "response_not_found", "Response not found.")
			return
		}
		if prev.Projection.Response.Status == "in_progress" {
			uhpFail(w, 409, "session_busy", "The session has a task in progress.")
			return
		}
		if hid != "" && hid != prev.Harness.ID {
			uhpFail(w, 409, "harness_mismatch", "The configured harness differs from the continued session.")
			return
		}
		hid = prev.Harness.ID
		ss, e = u.loadSession(r, prev.SessionID)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
	}
	h, e := u.harness(r, hid)
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	if req.MaxStep != nil && h.Base != "kun" {
		uhpFail(w, 422, "rundesk_budget_unsupported", "This harness cannot enforce a native tool-round budget.")
		return
	}
	model, e := u.server.uhpSelectModel(h, req.Model)
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	if uhpRequestHasFiles(req.Input) && !uhpScope(r, "files") {
		uhpFail(w, 403, "insufficient_scope", "Files scope is required.")
		return
	}
	in, e := u.server.uhpPrepareInput(req, uhpOwner(r), h.WorkspaceID)
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	for _, path := range in.Files {
		if err := u.server.recordUploadAccess(r, h.WorkspaceID, path); err != nil {
			uhpWriteError(w, err)
			return
		}
	}
	spec := TaskSpec{SubmittingKeyID: submittingKey(r), Title: "UHP task", WorkspaceID: h.WorkspaceID, InstanceID: h.InstanceID, Model: model, Source: h.Source, Input: in}
	if k := principal(r); k != nil {
		if e = normalizeTask(k, &spec); e != nil {
			uhpFail(w, 404, "harness_not_found", "Harness not found.")
			return
		}
	}
	m.uhpMu.Lock()
	// Recheck receipt after validation: two simultaneous submissions still commit
	// only one task. Task+session+projection+receipt are a single SQLite transaction.
	if key != "" {
		var receipt uhpReceipt
		receiptErr := m.Store.Get("uhp-receipt", uhpReceiptID(uhpOwner(r), key), &receipt)
		if receiptErr == nil {
			m.uhpMu.Unlock()
			u.reply(w, r, receipt.ResponseID, false, false, true)
			return
		}
		if !errors.Is(receiptErr, sql.ErrNoRows) {
			m.uhpMu.Unlock()
			uhpWriteError(w, receiptErr)
			return
		}
	}
	if prev.TaskID != "" {
		prev, e = u.loadResponse(r, req.PreviousResponseID, false)
		if e != nil {
			m.uhpMu.Unlock()
			uhpWriteError(w, e)
			return
		}
		ss, e = u.loadSession(r, prev.SessionID)
		if e != nil {
			m.uhpMu.Unlock()
			uhpWriteError(w, e)
			return
		}
	}
	rid := "resp_" + store.ID()
	now := time.Now().Unix()
	if ss.ID == "" {
		ss = uhpSessionRecord{ID: "hsess" + store.ID(), Owner: uhpOwner(r), Harness: h, ContainerID: "cntr_" + store.ID(), CreatedAt: now, ResponseIDs: []string{}}
	}
	kept := true
	if req.Store != nil && !*req.Store {
		// RunDesk keeps an audit journal and durable retry receipts. Never
		// promise per-request erasure while retaining the actual response.
		req.IgnoredFields = append(req.IgnoredFields, "store")
	}
	var previous *string
	if req.PreviousResponseID != "" {
		previous = &req.PreviousResponseID
	}
	response := uhpResponse{ID: rid, Object: "response", CreatedAt: now, Status: "in_progress", Model: model, Output: []map[string]any{}, Metadata: uhpSortedMetadata(req.Metadata, req.IgnoredFields, h.ID, ss.ID), PreviousResponseID: previous, Store: kept}
	delete(response.Metadata, "rundesk_instruction_mode")
	if req.InstructionMode != "" {
		response.Metadata["rundesk_instruction_mode"] = req.InstructionMode
	}
	record := uhpRecord{Owner: uhpOwner(r), Harness: h, SessionID: ss.ID, ContainerID: ss.ContainerID, Projection: newUHPProjection(response), Input: req.Input}
	if req.TimeoutSeconds != nil {
		record.Deadline = time.Now().Add(time.Duration(*req.TimeoutSeconds) * time.Second).UnixNano()
	}
	if req.MaxStep != nil {
		record.MaxStep = *req.MaxStep
	}
	if ss.HostSessionID != "" {
		artifacts, err := (&uhpFiles{Manager: m}).DiscoverArtifacts(ss.Owner, ss.ID, ss.HostSessionID, ss.ContainerID)
		if err != nil {
			m.uhpMu.Unlock()
			uhpWriteError(w, err)
			return
		}
		for _, f := range artifacts {
			record.BeforeFiles = append(record.BeforeFiles, f.ID)
		}
	}
	submission := TaskSubmission{Spec: spec}
	if prev.TaskID != "" {
		submission.SessionID = ss.HostSessionID
	}
	submission.Records = func(t Task, host Session) ([]store.Record, error) {
		record.TaskID = t.ID
		record.HostSessionID = host.ID
		ss.HostSessionID = host.ID
		ss.ResponseIDs = append(ss.ResponseIDs, rid)
		records := []store.Record{{Kind: "uhp-response", ID: rid, Value: record}, {Kind: "uhp-session", ID: ss.ID, Value: ss}}
		if key != "" {
			records = append(records, store.Record{Kind: "uhp-receipt", ID: uhpReceiptID(uhpOwner(r), key), Value: uhpReceipt{ResponseID: rid}})
		}
		return records, nil
	}
	submitter, ok := u.server.taskControl().(TaskSubmissionControl)
	if !ok {
		m.uhpMu.Unlock()
		uhpFail(w, 503, "harness_unavailable", "Task submission is unavailable.")
		return
	}
	_, e = submitter.SubmitTask(submission)
	m.uhpMu.Unlock()
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	u.reply(w, r, rid, req.Stream, req.Background, false)
}
func (u *uhpService) reply(w http.ResponseWriter, r *http.Request, id string, stream, background, replay bool) {
	if stream && !replay {
		u.stream(w, r, id)
		return
	}
	var v uhpRecord
	var e error
	if background && !replay {
		v, e = u.readRetained(r, id, true)
	} else {
		v, e = u.wait(r, id)
	}
	if e != nil {
		if r.Context().Err() == nil {
			uhpWriteError(w, e)
		}
		return
	}
	writeUHPJSON(w, 200, v.Projection.Response)
}
func (u *uhpService) stream(w http.ResponseWriter, r *http.Request, id string) {
	after := -1
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		var e error
		after, e = strconv.Atoi(raw)
		if e != nil || after < 0 {
			uhpFail(w, 400, "invalid_input", "Last-Event-ID must be a sequence number.")
			return
		}
	}
	v, e := u.read(r, id)
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	if r.Method == "GET" && !v.Projection.Response.Store && v.Projection.Response.Status != "in_progress" {
		uhpFail(w, 404, "response_not_found", "Response not found.")
		return
	}
	if after >= len(v.Projection.Events) {
		uhpFail(w, 400, "invalid_input", "Last-Event-ID is outside the event stream.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		uhpFail(w, 500, "rundesk_stream_unavailable", "Streaming is unavailable.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	alive := time.NewTicker(15 * time.Second)
	defer alive.Stop()
	for {
		if !u.server.credentialStillValid(r) {
			return
		}
		for after+1 < len(v.Projection.Events) {
			after++
			event := v.Projection.Events[after]
			b, _ := json.Marshal(event)
			if _, e = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", after, b); e != nil {
				return
			}
			flusher.Flush()
		}
		if v.Projection.Response.Status != "in_progress" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-u.server.Manager.ctx.Done():
			return
		case <-alive.C:
			if _, e = io.WriteString(w, ": keep-alive\n\n"); e != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
		}
		v, e = u.readRetained(r, id, true)
		if e != nil {
			return
		}
	}
}
func (u *uhpService) cancel(r *http.Request, id string) (uhpRecord, error) {
	m := u.server.Manager
	m.uhpMu.Lock()
	defer m.uhpMu.Unlock()
	v, e := u.loadResponse(r, id, true)
	if e != nil {
		return v, e
	}
	if e = u.refresh(&v); e != nil {
		return v, e
	}
	if v.Projection.Response.Status == "in_progress" {
		v.CancelReason = "cancelled"
		if e = m.Store.Put("uhp-response", id, v); e != nil {
			return v, e
		}
		_, e = u.server.taskControl().CancelTask(v.TaskID)
		if e == nil {
			e = u.refresh(&v)
		}
	}
	return v, e
}
func (u *uhpService) sessionWire(v uhpSessionRecord) map[string]any {
	status := "completed"
	updated := v.CreatedAt
	title := "UHP session"
	if host, e := u.server.Manager.Session(v.HostSessionID); e == nil {
		title = host.Title
	}
	if len(v.ResponseIDs) > 0 {
		var last uhpRecord
		if u.server.Manager.Store.Get("uhp-response", v.ResponseIDs[len(v.ResponseIDs)-1], &last) == nil {
			status = last.Projection.Response.Status
			updated = last.Projection.Response.CreatedAt
			if last.CompletedAt > 0 {
				updated = last.CompletedAt
			}
		}
	}
	return map[string]any{"id": v.ID, "object": "session", "harness_id": v.Harness.ID, "title": title, "status": status, "created_at": v.CreatedAt, "updated_at": updated}
}
func (u *uhpService) sessions(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var e error
		limit, e = strconv.Atoi(raw)
		if e != nil || limit < 1 || limit > 100 {
			uhpFail(w, 400, "invalid_input", "Limit must be between 1 and 100.")
			return
		}
	}
	rows, e := u.server.Manager.Store.List("uhp-session")
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	sessions := []uhpSessionRecord{}
	for _, raw := range rows {
		var v uhpSessionRecord
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		if _, e := u.loadSession(r, v.ID); e != nil {
			continue
		}
		if h := r.URL.Query().Get("harness"); h != "" && h != v.Harness.ID {
			continue
		}
		sessions = append(sessions, v)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	cursor := r.URL.Query().Get("cursor")
	out := []any{}
	var next any
	for _, v := range sessions {
		if v.ID <= cursor {
			continue
		}
		if len(out) == limit {
			next = out[len(out)-1].(map[string]any)["id"]
			break
		}
		out = append(out, u.sessionWire(v))
	}
	writeUHPJSON(w, 200, map[string]any{"sessions": out, "next_cursor": next})
}
func (u *uhpService) turns(w http.ResponseWriter, r *http.Request) {
	v, e := u.loadSession(r, r.PathValue("sid"))
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	out := []any{}
	for _, id := range v.ResponseIDs {
		rec, e := u.read(r, id)
		if e != nil {
			continue
		}
		if !rec.Projection.Response.Store {
			continue
		}
		var completed any
		if rec.CompletedAt > 0 {
			completed = rec.CompletedAt
		}
		out = append(out, map[string]any{"id": id, "status": rec.Projection.Response.Status, "created_at": rec.Projection.Response.CreatedAt, "completed_at": completed, "imported": false, "output": rec.Projection.Response.Output})
	}
	writeUHPJSON(w, 200, map[string]any{"turns": out})
}
func (u *uhpService) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, UHPFileLimit+(1<<20))
	reader, e := r.MultipartReader()
	if e != nil {
		uhpFail(w, 400, "invalid_input", "A multipart file is required.")
		return
	}
	for {
		part, e := reader.NextPart()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			uhpFail(w, 413, "file_too_large", "The upload exceeds the size limit.")
			return
		}
		if part.FormName() != "file" {
			_, e = io.Copy(io.Discard, part)
			part.Close()
			if e != nil {
				uhpFail(w, 413, "file_too_large", "The upload exceeds the size limit.")
				return
			}
			continue
		}
		f, e := (&uhpFiles{Manager: u.server.Manager}).Upload(uhpOwner(r), part.FileName(), part)
		part.Close()
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		writeUHPJSON(w, 200, f.Wire())
		return
	}
	uhpFail(w, 400, "invalid_input", "A file part is required.")
}
func (u *uhpService) file(r *http.Request) (uhpFileRecord, error) {
	f, e := (&uhpFiles{Manager: u.server.Manager}).Record(uhpOwner(r), r.PathValue("cid"), r.PathValue("fid"))
	if e != nil {
		return f, e
	}
	if _, e = u.loadSession(r, f.SessionID); e != nil {
		return f, failure(404, "file_not_found", "")
	}
	return f, nil
}
func (u *uhpService) download(w http.ResponseWriter, r *http.Request) {
	if _, e := u.file(r); e != nil {
		uhpWriteError(w, e)
		return
	}
	f, body, cleanup, e := (&uhpFiles{Manager: u.server.Manager}).Content(uhpOwner(r), r.PathValue("cid"), r.PathValue("fid"))
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	defer cleanup()
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.DownloadName()}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, f.Filename, time.Unix(f.CreatedAt, 0), body)
}
func (u *uhpService) archive(w http.ResponseWriter, r *http.Request) {
	v, e := u.loadSession(r, r.PathValue("sid"))
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	files, e := (&uhpFiles{Manager: u.server.Manager}).DiscoverArtifacts(v.Owner, v.ID, v.HostSessionID, v.ContainerID)
	if e != nil {
		uhpWriteError(w, e)
		return
	}
	// Validate and pin every input before emitting bytes; no partial success for
	// a missing/changed original. Downloads use temporary verified spools.
	type opened struct {
		rec   uhpFileRecord
		body  io.Reader
		close func()
	}
	all := []opened{}
	defer func() {
		for _, f := range all {
			f.close()
		}
	}()
	for _, f := range files {
		rec, body, done, e := (&uhpFiles{Manager: u.server.Manager}).Content(v.Owner, v.ContainerID, f.ID)
		if e != nil {
			uhpWriteError(w, e)
			return
		}
		all = append(all, opened{rec, body, done})
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=artifacts.zip")
	z := zip.NewWriter(w)
	defer z.Close()
	for _, f := range all {
		dst, e := z.Create(f.rec.ID + "/" + f.rec.Filename)
		if e != nil {
			return
		}
		if _, e = io.Copy(dst, f.body); e != nil {
			return
		}
	}
}

func uhpRequestHasFiles(raw json.RawMessage) bool {
	var items []struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, item := range items {
		var parts []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item.Content, &parts) != nil {
			continue
		}
		for _, part := range parts {
			if part.Type == "input_file" || part.Type == "input_image" {
				return true
			}
		}
	}
	return false
}
