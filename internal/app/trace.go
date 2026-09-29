package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"
)

// A bounded preview travels with each lifecycle record; source events are fetched
// separately by their stable journal IDs. Never mutate the underlying journal.
func tracePreview(v any, budget *int, depth int) any {
	if depth > 12 {
		return "… [预览已截断]"
	}
	switch x := v.(type) {
	case string:
		n := len(x)
		if n > 4096 {
			n = 4096
		}
		if n > *budget {
			n = *budget
		}
		for n > 0 && !utf8.ValidString(x[:n]) {
			n--
		}
		*budget -= n
		if n < len(x) {
			return x[:n] + "… [预览已截断]"
		}
		return x
	case []any:
		out := make([]any, 0)
		for i, item := range x {
			if i >= 64 || *budget <= 0 {
				out = append(out, "… [预览已截断]")
				break
			}
			out = append(out, tracePreview(item, budget, depth+1))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		priority := map[string]int{"id": 1, "type": 2, "method": 3, "runId": 4, "threadId": 5, "turnId": 6, "itemId": 7, "requestId": 8, "status": 9, "startedAtMs": 10, "completedAtMs": 11, "exitCode": 12, "server": 13, "tool": 14, "scope": 15, "decision": 16}
		sort.Slice(keys, func(i, j int) bool {
			a, b := priority[keys[i]], priority[keys[j]]
			if a == 0 {
				a = 100
			}
			if b == 0 {
				b = 100
			}
			if a == b {
				return keys[i] < keys[j]
			}
			return a < b
		})
		for i, k := range keys {
			if i >= 96 {
				out["_previewTruncated"] = true
				break
			}
			out[k] = tracePreview(x[k], budget, depth+1)
		}
		return out
	default:
		return v
	}
}

func (s *Server) traceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/trace", func(w http.ResponseWriter, r *http.Request) {
		sid := r.PathValue("sid")
		session, err := s.Manager.Session(sid)
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		through, _ := strconv.ParseInt(r.URL.Query().Get("through"), 10, 64)
		if after < 0 || through < 0 {
			writeErr(w, 400, errors.New("invalid cursor"))
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 500 {
			limit = 250
		}
		events, snapshot, err := s.Manager.Store.TraceEvents(sid, after, through, limit+1)
		if err != nil {
			respond(w, nil, err)
			return
		}
		more := len(events) > limit
		if more {
			events = events[:limit]
		}
		cursor := snapshot
		if more {
			cursor = events[len(events)-1].ID
		}
		if cursor < after {
			cursor = after
		}
		for i := range events {
			var v any
			if json.Unmarshal(events[i].Data, &v) == nil {
				budget := 16384
				events[i].Data, _ = json.Marshal(tracePreview(v, &budget, 0))
			}
		}
		writeJSON(w, 200, map[string]any{"events": events, "nextCursor": cursor, "hasMore": more, "snapshot": snapshot, "session": session, "observedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	})
	mux.HandleFunc("GET /api/sessions/{sid}/events/{eid}", func(w http.ResponseWriter, r *http.Request) {
		sid := r.PathValue("sid")
		if _, err := s.Manager.Session(sid); err != nil {
			writeErr(w, 404, err)
			return
		}
		id, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
		if err != nil || id <= 0 {
			writeErr(w, 400, errors.New("invalid event ID"))
			return
		}
		v, err := s.Manager.Store.Event(sid, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, errors.New("事件不存在"))
			return
		}
		respond(w, v, err)
	})
}
