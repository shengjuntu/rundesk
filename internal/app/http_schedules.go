package app

import (
	"net/http"
	"time"
)

func (s *Server) scheduleRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/schedules", func(w http.ResponseWriter, r *http.Request) {
		items := []Schedule{}
		for _, v := range m.Schedules() {
			if scheduleVisible(r, v) {
				items = append(items, v)
			}
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/schedules", func(w http.ResponseWriter, r *http.Request) {
		var spec ScheduleSpec
		if !decode(w, r, &spec) {
			return
		}
		spec.Task.SubmittingKeyID = submittingKey(r)
		v, e := m.SaveSchedule("", spec, 0)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/schedules/{id}", func(w http.ResponseWriter, r *http.Request) { v, e := m.Schedule(r.PathValue("id")); respond(w, v, e) })
	mux.HandleFunc("PUT /api/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Spec     ScheduleSpec `json:"spec"`
			Revision int          `json:"revision"`
		}
		if !decode(w, r, &v) {
			return
		}
		v.Spec.Task.SubmittingKeyID = submittingKey(r)
		result, e := m.SaveSchedule(r.PathValue("id"), v.Spec, v.Revision)
		respond(w, result, e)
	})
	mux.HandleFunc("DELETE /api/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Revision int `json:"revision"`
		}
		if !decode(w, r, &v) {
			return
		}
		respond(w, map[string]bool{"deleted": true}, m.DeleteSchedule(r.PathValue("id"), v.Revision))
	})
	mux.HandleFunc("POST /api/schedules/preview", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Cron     string `json:"cron"`
			Timezone string `json:"timezone"`
		}
		if !decode(w, r, &v) {
			return
		}
		c, e := parseSchedule(v.Cron, v.Timezone)
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		times := []string{}
		loc, _ := time.LoadLocation(v.Timezone)
		at := time.Now().In(loc)
		for n := 0; n < 5; n++ {
			at = c.Next(at)
			if at.IsZero() {
				break
			}
			times = append(times, at.Format(time.RFC3339))
		}
		writeJSON(w, 200, map[string]any{"times": times, "timezone": v.Timezone})
	})
}
