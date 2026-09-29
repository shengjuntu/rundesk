package app

import (
	"net/http"

	"github.com/shengjuntu/rundesk/internal/store"
)

func filterFromRequest(r *http.Request) store.EventFilter {
	q := r.URL.Query()
	return store.EventFilter{Direction: q.Get("direction"), Method: q.Get("method"), Query: q.Get("q"), From: q.Get("from"), To: q.Get("to"), Category: q.Get("category")}
}
func (s *Server) extraRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("PATCH /api/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		var p SessionPatch
		if !decode(w, r, &p) {
			return
		}
		result, e := m.PatchSession(r.PathValue("sid"), p)
		if e != nil {
			writeErr(w, 409, e)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("DELETE /api/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		e := m.DeleteSession(r.PathValue("sid"))
		if e != nil {
			writeErr(w, 409, e)
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": true, "filesRetained": true, "nativeThreadRetained": true})
	})
	mux.HandleFunc("DELETE /api/workspaces/{wid}/skills/{name}", func(w http.ResponseWriter, r *http.Request) {
		backup, e := m.DeleteSkill(r.PathValue("wid"), r.PathValue("name"), r.URL.Query().Get("instanceId"), r.URL.Query().Get("scope"))
		respond(w, map[string]any{"removed": e == nil, "backupPath": backup, "supportingFilesRetained": true}, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/mcp/export", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ExportMCP(r.PathValue("wid"), r.URL.Query().Get("instanceId"))
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="rundesk-mcp.json"`)
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/workspaces/{wid}/mcp/import", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Version   string    `json:"version"`
			Bundle    MCPBundle `json:"bundle"`
			Overwrite bool      `json:"overwrite"`
		}
		if !decode(w, r, &v) {
			return
		}
		result, e := m.ImportMCP(r.PathValue("wid"), v.Version, v.Bundle, v.Overwrite, r.URL.Query().Get("instanceId"))
		respond(w, result, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.Diagnostics(r.PathValue("wid"), r.URL.Query().Get("instanceId"))
		respond(w, v, e)
	})
}
