package app

import (
	"net/http"
	"strconv"
)

func (s *Server) queueRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/queue", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, m.Queue()) })
	mux.HandleFunc("PUT /api/queue", func(w http.ResponseWriter, r *http.Request) {
		var q QueueSettings
		if !decode(w, r, &q) {
			return
		}
		v, e := m.SaveQueue(q)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var spec TaskSpec
		if !decode(w, r, &spec) {
			return
		}
		v, e := m.Enqueue(spec)
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		writeJSON(w, 202, v)
	})
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 100
		if q.Get("limit") != "" {
			n, e := strconv.Atoi(q.Get("limit"))
			if e != nil || n < 1 || n > 500 {
				writeErr(w, 400, failure(400, "invalid_limit", "limit 需为 1–500"))
				return
			}
			limit = n
		}
		rows := []Task{}
		for _, t := range m.Tasks() {
			if !taskVisible(r, t) {
				continue
			}
			if (q.Get("status") == "" || t.Status == q.Get("status")) && (q.Get("appId") == "" || t.Spec.Source.AppID == q.Get("appId")) && (q.Get("instanceId") == "" || t.Spec.InstanceID == q.Get("instanceId")) {
				rows = append(rows, t)
			}
		}
		start := 0
		if cursor := q.Get("cursor"); cursor != "" {
			start = -1
			for n, t := range rows {
				if t.ID == cursor {
					start = n + 1
					break
				}
			}
			if start < 0 {
				writeErr(w, 400, failure(400, "invalid_cursor", "分页位置已失效，请刷新"))
				return
			}
		}
		rows = rows[start:]
		next := ""
		if len(rows) > limit {
			rows = rows[:limit]
			next = rows[len(rows)-1].ID
		}
		writeJSON(w, 200, map[string]any{"items": rows, "nextCursor": next})
	})
	mux.HandleFunc("GET /api/tasks/{tid}", func(w http.ResponseWriter, r *http.Request) { v, e := m.Task(r.PathValue("tid")); respond(w, v, e) })
	mux.HandleFunc("POST /api/tasks/{tid}/cancel", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.CancelTask(r.PathValue("tid"))
		respond(w, v, e)
	})
}
