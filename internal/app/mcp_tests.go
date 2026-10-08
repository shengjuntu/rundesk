package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/shengjuntu/rundesk/internal/mcptest"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type MCPTestRequest struct {
	Server    string         `json:"server"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Cursor    string         `json:"cursor,omitempty"`
	Confirm   bool           `json:"confirm"`
}
type MCPTestRecord struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`
	Server      string `json:"server"`
	Tool        string `json:"tool,omitempty"`
	Arguments   any    `json:"arguments,omitempty"`
	Created     string `json:"created"`
	DurationMS  int64  `json:"durationMs"`
	Status      string `json:"status"`
	Result      any    `json:"result,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (m *Manager) runMCPTest(parent context.Context, w Workspace, i Instance, p MCPTestRequest) (MCPTestRecord, error) {
	rec := MCPTestRecord{}
	if !p.Confirm {
		return rec, failure(400, "confirmation_required", "Explicit confirmation is required; direct MCP calls bypass Agent approvals.")
	}
	if i.Execution.Mode == "docker" {
		return rec, failure(422, "mcp_test_runtime_unsupported", "Container MCP tests are not yet supported; no host fallback is performed.")
	}
	if !slug.MatchString(p.Server) || len(p.Tool) > 256 || len(p.Cursor) > 4096 {
		return rec, failure(400, "invalid_mcp_test", "Invalid server, tool or cursor")
	}
	c, e := m.readConfig(w.ID, i.ID)
	if e != nil {
		return rec, e
	}
	servers, _ := c.Config["mcp_servers"].(map[string]any)
	cfg, ok := servers[p.Server].(map[string]any)
	if !ok {
		return rec, failure(404, "mcp_server_not_found", "Server is absent from effective configuration")
	}
	if p.Tool != "" {
		for _, field := range []string{"enabled_tools", "disabled_tools"} {
			if list, ok := cfg[field].([]any); ok {
				found := false
				for _, v := range list {
					if v == p.Tool {
						found = true
					}
				}
				if field == "enabled_tools" && !found || field == "disabled_tools" && found {
					return rec, failure(403, "mcp_tool_disabled", "Tool is excluded by configuration")
				}
			}
		}
	}
	secrets := []string{}
	for _, key := range []string{"env", "http_headers"} {
		if fields, ok := cfg[key].(map[string]any); ok {
			for _, v := range fields {
				if s, ok := v.(string); ok && s != "" {
					secrets = append(secrets, s)
				}
			}
		}
	}
	for _, key := range []string{"bearer_token_env_var"} {
		if name, ok := cfg[key].(string); ok && os.Getenv(name) != "" {
			secrets = append(secrets, os.Getenv(name))
		}
	}
	if fields, ok := cfg["env_http_headers"].(map[string]any); ok {
		for _, v := range fields {
			if name, ok := v.(string); ok && os.Getenv(name) != "" {
				secrets = append(secrets, os.Getenv(name))
			}
		}
	}
	rec = MCPTestRecord{ID: store.ID(), WorkspaceID: w.ID, InstanceID: i.ID, Server: p.Server, Tool: p.Tool, Arguments: scrubMCPValue(redact(p.Arguments), secrets), Created: store.Now(), Status: "unconfirmed"}
	// Persist before contacting the server. A crash never invites automatic retry.
	if e = m.Store.Put("mcp-test", rec.ID, rec); e != nil {
		return rec, e
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	client, e := m.mcpTestClient(ctx, w, i, cfg)
	if e == nil {
		defer client.Close()
		e = client.Initialize()
		if e == nil {
			method := "tools/list"
			params := map[string]any{}
			if p.Tool != "" {
				method = "tools/call"
				params["name"] = p.Tool
				if p.Arguments == nil {
					p.Arguments = map[string]any{}
				}
				params["arguments"] = p.Arguments
			} else if p.Cursor != "" {
				params["cursor"] = p.Cursor
			}
			var raw json.RawMessage
			raw, e = client.Call(method, params)
			if e == nil {
				_ = json.Unmarshal(raw, &rec.Result)
				rec.Result = redact(rec.Result)
			}
		}
	}
	rec.DurationMS = time.Since(start).Milliseconds()
	rec.Status = "completed"
	if e != nil {
		rec.Status = "failed"
		rec.Error = e.Error()
	} else if result, ok := rec.Result.(map[string]any); ok && result["isError"] == true {
		rec.Status = "tool_error"
	}
	// Scrub configured secrets even when servers echo them inside text content.
	rec.Error = scrubMCPText(rec.Error, secrets)
	rec.Result = scrubMCPValue(rec.Result, secrets)
	rec.Arguments = scrubMCPValue(rec.Arguments, secrets)
	if e = m.Store.Put("mcp-test", rec.ID, rec); e != nil {
		return rec, failure(503, "mcp_test_unconfirmed", "Call may have executed; result could not be saved. Do not automatically retry.")
	}
	return rec, nil
}
func scrubMCPText(v string, secrets []string) string {
	for _, s := range secrets {
		v = strings.ReplaceAll(v, s, "[redacted]")
	}
	return v
}
func scrubMCPValue(v any, secrets []string) any {
	switch x := v.(type) {
	case string:
		return scrubMCPText(x, secrets)
	case map[string]any:
		for k, v := range x {
			x[k] = scrubMCPValue(v, secrets)
		}
	case []any:
		for k, v := range x {
			x[k] = scrubMCPValue(v, secrets)
		}
	}
	return v
}
func (m *Manager) mcpTestClient(ctx context.Context, w Workspace, i Instance, cfg map[string]any) (*mcptest.Client, error) {
	if endpoint, _ := cfg["url"].(string); endpoint != "" {
		u, e := url.Parse(endpoint)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("MCP URL must be HTTP(S), without credentials, query or fragment")
		}
		headers := http.Header{}
		if values, ok := cfg["http_headers"].(map[string]any); ok {
			for k, v := range values {
				if s, ok := v.(string); ok {
					headers.Set(k, s)
				}
			}
		}
		if values, ok := cfg["env_http_headers"].(map[string]any); ok {
			for k, v := range values {
				if name, ok := v.(string); ok {
					value, exists := os.LookupEnv(name)
					if !exists {
						return nil, fmt.Errorf("missing header environment variable: %s", name)
					}
					headers.Set(k, value)
				}
			}
		}
		if name, _ := cfg["bearer_token_env_var"].(string); name != "" {
			value, ok := os.LookupEnv(name)
			if !ok {
				return nil, fmt.Errorf("missing token environment variable: %s", name)
			}
			headers.Set("Authorization", "Bearer "+value)
		}
		return mcptest.HTTP(ctx, endpoint, headers), nil
	}
	command, _ := cfg["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("MCP command or URL is required")
	}
	args := []string{}
	if values, ok := cfg["args"].([]any); ok {
		for _, v := range values {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("MCP arguments must be strings")
			}
			args = append(args, s)
		}
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = w.Path
	cmd.Env = m.command(w, i).Env
	if dir, _ := cfg["cwd"].(string); dir != "" {
		cmd.Dir = dir
	}
	if values, ok := cfg["env"].(map[string]any); ok {
		for k, v := range values {
			if s, ok := v.(string); ok {
				cmd.Env = append(cmd.Env, k+"="+s)
			}
		}
	}
	cmd.WaitDelay = time.Second
	configureProbeProcess(cmd)
	return mcptest.Stdio(ctx, cmd)
}
func (s *Server) mcpTestRoutes(mux *http.ServeMux) {
	recordHandler := func(w http.ResponseWriter, r *http.Request) {
		i, e := s.Manager.Instance(r.URL.Query().Get("instanceId"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		var rec MCPTestRecord
		if s.Manager.Store.Get("mcp-test", r.PathValue("testid"), &rec) != nil || rec.InstanceID != i.ID || rec.WorkspaceID != r.PathValue("wid") {
			writeErr(w, 404, failure(404, "mcp_test_not_found", "Test record not found"))
			return
		}
		if r.Method == "DELETE" {
			if rec.Status == "unconfirmed" {
				writeErr(w, 409, failure(409, "mcp_test_unconfirmed", "Cannot delete an unfinished test record"))
				return
			}
			respond(w, map[string]bool{"deleted": true}, s.Manager.Store.DeleteObject("mcp-test", rec.ID))
			return
		}
		respond(w, rec, nil)
	}
	mux.HandleFunc("GET /api/workspaces/{wid}/mcp-tests/{testid}", recordHandler)
	mux.HandleFunc("DELETE /api/workspaces/{wid}/mcp-tests/{testid}", recordHandler)
	mux.HandleFunc("POST /api/workspaces/{wid}/mcp-tests", func(w http.ResponseWriter, r *http.Request) {
		var p MCPTestRequest
		if !decode(w, r, &p) {
			return
		}
		work, e := s.Manager.Workspace(r.PathValue("wid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		i, e := s.Manager.Instance(r.URL.Query().Get("instanceId"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		rec, e := s.Manager.runMCPTest(r.Context(), work, i, p)
		respond(w, rec, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/mcp-tests", func(w http.ResponseWriter, r *http.Request) {
		i, e := s.Manager.Instance(r.URL.Query().Get("instanceId"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		rows, e := s.Manager.Store.List("mcp-test")
		if e != nil {
			respond(w, nil, e)
			return
		}
		out := []MCPTestRecord{}
		for _, b := range rows {
			var rec MCPTestRecord
			if json.Unmarshal(b, &rec) == nil && rec.WorkspaceID == r.PathValue("wid") && rec.InstanceID == i.ID {
				rec.Arguments = nil
				rec.Result = nil
				rec.Error = ""
				out = append(out, rec)
			}
		}
		sort.Slice(out, func(a, b int) bool { return out[a].Created > out[b].Created })
		if len(out) > 50 {
			out = out[:50]
		}
		respond(w, out, nil)
	})
}
