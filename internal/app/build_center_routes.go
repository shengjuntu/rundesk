package app

import (
	"net/http"
	"strconv"
)

// These platform routes use the ordinary administrator boundary. Application
// keys and members remain default-denied by applicationGate/userGate.
func (s *Server) buildCenterImageRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/build-center/images", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.BuildCenterImages()
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/build-center/images/{key}", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.BuildCenterImage(r.PathValue("key"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/build-center/images/{key}/check", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.CheckBuildCenterImage(r.PathValue("key"))
		respond(w, v, e)
	})
	mux.HandleFunc("DELETE /api/build-center/images/{key}", func(w http.ResponseWriter, r *http.Request) {
		var in BuildCenterImageDeleteInput
		if !decode(w, r, &in) {
			return
		}
		v, e := m.DeleteBuildCenterImage(r.PathValue("key"), in)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/build-center/images/{key}/launch", func(w http.ResponseWriter, r *http.Request) {
		var in BuildCenterImageLaunchInput
		if !decode(w, r, &in) {
			return
		}
		v, e := m.LaunchBuildCenterImage(r.PathValue("key"), in)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/build-center/images/{key}/tests", func(w http.ResponseWriter, r *http.Request) {
		var in BuildCenterImageTestInput
		if !decode(w, r, &in) {
			return
		}
		v, e := m.StartBuildCenterImageTest(r.PathValue("key"), in)
		if e != nil {
			respond(w, nil, e)
			return
		}
		writeJSON(w, http.StatusAccepted, v)
	})
	mux.HandleFunc("GET /api/build-center/tests/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.BuildCenterImageTest(r.PathValue("id"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/build-center/tests/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		after := int64(0)
		if raw := r.URL.Query().Get("after"); raw != "" {
			var e error
			after, e = strconv.ParseInt(raw, 10, 64)
			if e != nil || after < 0 {
				respond(w, nil, failure(400, "invalid_log_offset", "after 必须为非负整数"))
				return
			}
		}
		v, e := m.BuildCenterImageTestLog(r.PathValue("id"), after)
		respond(w, v, e)
	})
}
