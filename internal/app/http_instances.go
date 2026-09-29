package app

import "net/http"

func (s *Server) instanceRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("POST /api/instances/{iid}/reload", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ReloadInstance(r.PathValue("iid"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/instances", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, m.Instances()) })
	mux.HandleFunc("POST /api/instances", func(w http.ResponseWriter, r *http.Request) {
		var p InstancePatch
		if !decode(w, r, &p) {
			return
		}
		v, e := m.CreateInstance(p)
		respond(w, v, e)
	})
	mux.HandleFunc("PATCH /api/instances/{iid}", func(w http.ResponseWriter, r *http.Request) {
		var p InstancePatch
		if !decode(w, r, &p) {
			return
		}
		v, e := m.PatchInstance(r.PathValue("iid"), p)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/account", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ConfigCall(r.PathValue("wid"), "account/read", map[string]any{"refreshToken": false}, r.URL.Query().Get("instanceId"))
		respond(w, v, e)
	})
}
