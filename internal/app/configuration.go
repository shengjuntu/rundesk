package app

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/store"
)

func (s *Server) instanceConfiguration(w http.ResponseWriter, r *http.Request) {
	value, err := s.Manager.Configuration(r.PathValue("iid"), r.URL.Query().Get("workspaceId"), r.URL.Query().Get("probe") == "1")
	respond(w, value, err)
}
func (s *Server) sessionConfiguration(w http.ResponseWriter, r *http.Request) {
	session, err := s.Manager.Session(r.PathValue("sid"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	value, err := s.Manager.Configuration(session.InstanceID, session.WorkspaceID, r.URL.Query().Get("probe") == "1")
	if err != nil {
		respond(w, nil, err)
		return
	}
	value["session"] = session
	runtime, err := s.Manager.Runtime(session.ID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	value["runtime"] = runtime
	if event, err := s.Manager.Store.LatestEvent(session.ID, "run/input"); err == nil {
		var input struct {
			RunID            string `json:"runId"`
			Input            Input  `json:"input"`
			Model            string `json:"model"`
			InstanceRevision int    `json:"instanceRevision"`
			NotesRevision    int    `json:"notesRevision"`
		}
		if json.Unmarshal(event.Data, &input) == nil {
			value["lastSubmission"] = map[string]any{"runId": input.RunID, "skills": input.Input.Skills, "model": input.Model, "instanceRevision": input.InstanceRevision, "notesRevision": input.NotesRevision, "time": event.Time}
		}
	}
	respond(w, value, nil)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// The view separates current configuration from a session's recorded execution.
// Reading metadata never launches Codex. Explicit probe discovers tools, without a model turn.
func (m *Manager) Configuration(iid, wid string, probe bool) (map[string]any, error) {
	i, err := m.Instance(iid)
	if err != nil {
		return nil, err
	}
	w, err := m.Workspace(wid)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, session := range m.Sessions() {
		if session.InstanceID == i.ID {
			count++
		}
	}
	result := map[string]any{"instance": i, "workspace": map[string]string{"id": w.ID, "name": w.Name, "path": w.Path}, "sessionCount": count, "observedAt": store.Now(), "probed": probe, "errors": map[string]string{}}
	if !probe {
		return result, nil
	}
	problems := result["errors"].(map[string]string)
	raw, err := m.Skills(w.ID, i.ID)
	if err != nil {
		problems["skills"] = err.Error()
	} else {
		var data struct {
			Data []struct {
				Skills []map[string]any `json:"skills"`
				Errors []any            `json:"errors"`
			} `json:"data"`
		}
		if err = json.Unmarshal(raw, &data); err != nil {
			problems["skills"] = err.Error()
		} else {
			items := []map[string]any{}
			warnings := []any{}
			for _, group := range data.Data {
				for _, sk := range group.Skills {
					path, _ := sk["path"].(string)
					scope := "other"
					if within(filepath.Join(i.CodexHome, "skills"), path) {
						scope = "instance"
					} else if within(w.Path, path) {
						scope = "project"
					}
					sk["sourceScope"] = scope
					items = append(items, sk)
				}
				warnings = append(warnings, group.Errors...)
			}
			result["skills"] = items
			result["skillWarnings"] = warnings
		}
	}
	info, err := m.MCP(w.ID, i.ID)
	if err != nil {
		problems["mcp"] = err.Error()
	} else {
		data := info.(map[string]any)
		configured, _ := data["effectiveServers"].(map[string]any)
		user, _ := data["userServers"].(map[string]any)
		servers := []map[string]any{}
		for name, value := range configured {
			config, _ := value.(map[string]any)
			enabled := config["enabled"] != false
			origin := "effective"
			if _, ok := user[name]; ok {
				origin = "instance_or_override"
			}
			// Overview exposes names and enablement only, never URLs, argv or secrets.
			servers = append(servers, map[string]any{"name": name, "enabled": enabled, "origin": origin})
		}
		sort.Slice(servers, func(a, b int) bool { return servers[a]["name"].(string) < servers[b]["name"].(string) })
		result["mcp"] = map[string]any{"servers": servers, "version": data["version"], "status": redact(data["status"])}
	}
	result["observedAt"] = store.Now()
	return result, nil
}
