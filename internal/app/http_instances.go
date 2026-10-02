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
		if w.Header().Get("RunDesk-API-Version") == "v1" {
			var patch struct {
				Name         *string      `json:"name"`
				Description  *string      `json:"description"`
				DefaultModel *string      `json:"defaultModel"`
				Permissions  *Permissions `json:"permissions"`
				Revision     *int         `json:"revision"`
			}
			if !decode(w, r, &patch) {
				return
			}
			if patch.Revision == nil {
				writeErr(w, 400, failure(400, "revision_required", "修改实例需要当前 revision"))
				return
			}
			current, err := m.Instance(r.PathValue("iid"))
			if err != nil {
				respond(w, nil, err)
				return
			}
			value := InstancePatch{Name: current.Name, Description: current.Description, DefaultModel: current.DefaultModel, Permissions: patch.Permissions, Revision: *patch.Revision}
			if patch.Name != nil {
				value.Name = *patch.Name
			}
			if patch.Description != nil {
				value.Description = *patch.Description
			}
			if patch.DefaultModel != nil {
				value.DefaultModel = *patch.DefaultModel
			}
			result, err := m.PatchInstance(current.ID, value)
			respond(w, result, err)
			return
		}
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
