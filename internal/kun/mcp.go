package kun

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/mcp"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type mcpConnection struct {
	spec   p.MCPServer
	client *mcp.Client
}

func mcpAlias(server, name string) string {
	clean := func(s string, n int) string {
		var b strings.Builder
		for _, r := range s {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
			if b.Len() >= n {
				break
			}
		}
		return b.String()
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(server+"\x00"+name)))
	return "mcp_" + clean(server, 20) + "_" + clean(name, 20) + "_" + hash[:12]
}
func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
func (e *Engine) scrubText(s string) string {
	for _, secret := range e.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}
func (e *Engine) scrubJSON(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return p.JSON(e.scrubText(string(raw)))
	}
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case string:
			return e.scrubText(x)
		case []any:
			for n, a := range x {
				x[n] = scrub(a)
			}
		case map[string]any:
			for k, a := range x {
				x[k] = scrub(a)
			}
		}
		return v
	}
	return p.JSON(scrub(value))
}
func (e *Engine) mcpRequest(ctx context.Context, server, method string, params any, invoke func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	e.mu.Lock()
	e.exchange++
	exchange := e.exchange
	err := e.record("kun/mcp.request", map[string]any{"exchangeId": exchange, "server": server, "method": method, "params": params})
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	raw, callErr := invoke(ctx)
	if len(raw) > 512<<10 {
		callErr = fmt.Errorf("MCP result exceeds 512 KiB; outcome may be unknown")
		raw = nil
	}
	if raw != nil {
		raw = e.scrubJSON(raw)
	}
	data := map[string]any{"exchangeId": exchange, "server": server, "method": method, "durationMs": time.Since(start).Milliseconds()}
	if callErr != nil {
		callErr = fmt.Errorf("%s", e.scrubText(callErr.Error()))
		data["error"] = callErr.Error()
	} else {
		data["result"] = raw
	}
	e.mu.Lock()
	err = e.record("kun/mcp.response", data)
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return raw, callErr
}
func mcpEnvironment(extra map[string]string) []string {
	values := map[string]string{}
	for _, name := range []string{"PATH", "HOME", "USER", "LOGNAME", "USERPROFILE", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "TMPDIR", "APPDATA", "LOCALAPPDATA", "LANG", "LC_ALL"} {
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	for name, value := range extra {
		values[name] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name+"="+values[name])
	}
	return out
}
func (e *Engine) discoverMCP(ctx context.Context) error {
	for index, spec := range e.mcpSpecs {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.mu.Lock()
		e.state.Phase = "mcp_discovery"
		e.state.MCP[index].Status = "connecting"
		err := e.record("kun/mcp.connecting", e.state.MCP[index])
		e.mu.Unlock()
		if err != nil {
			return err
		}
		var client *mcp.Client
		if spec.URL != "" {
			headers := http.Header{}
			for k, v := range spec.Headers {
				headers.Set(k, v)
			}
			client = mcp.HTTP(spec.URL, headers)
		} else {
			cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
			cmd.Dir = e.workspace
			cmd.Env = mcpEnvironment(spec.Environment)
			client, err = mcp.Stdio(ctx, cmd)
		}
		if err == nil {
			e.connections[spec.Name] = &mcpConnection{spec, client}
			startCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.StartupTimeout*float64(time.Second)))
			params := map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "kun", "version": p.EngineVersion}}
			var raw json.RawMessage
			raw, err = e.mcpRequest(startCtx, spec.Name, "initialize", params, func(c context.Context) (json.RawMessage, error) { return client.Initialize(c, "kun", p.EngineVersion) })
			var init struct {
				ProtocolVersion string                     `json:"protocolVersion"`
				Capabilities    map[string]json.RawMessage `json:"capabilities"`
			}
			if err == nil {
				err = json.Unmarshal(raw, &init)
			}
			if err == nil && (init.Capabilities["tools"] == nil || string(init.Capabilities["tools"]) == "null") {
				err = fmt.Errorf("server does not advertise MCP tools")
			}
			var tools []p.MCPTool
			if err == nil {
				tools, err = e.listMCPTools(startCtx, spec, client)
			}
			cancel()
			if err == nil {
				e.mu.Lock()
				if len(e.state.MCPTools)+len(tools) > 128 {
					err = fmt.Errorf("MCP catalog exceeds 128 tools")
				} else {
					e.state.MCPTools = append(e.state.MCPTools, tools...)
					for _, tool := range tools {
						e.state.ToolDefinitions = append(e.state.ToolDefinitions, p.JSON(map[string]any{"type": "function", "function": map[string]any{"name": tool.Alias, "description": "MCP " + tool.Server + "/" + tool.Name + "\n" + tool.Description, "parameters": tool.InputSchema}}))
					}
					if len(p.JSON(e.state.ToolDefinitions)) > 512<<10 {
						err = fmt.Errorf("MCP tool definitions exceed 512 KiB")
					}
				}
				if err == nil {
					e.state.MCP[index].Status = "ready"
					e.state.MCP[index].ToolCount = len(tools)
					e.state.MCP[index].ProtocolVersion = init.ProtocolVersion
					err = e.record("kun/mcp.ready", map[string]any{"server": e.state.MCP[index], "tools": tools})
				}
				e.mu.Unlock()
			}
		}
		if err != nil {
			e.mu.Lock()
			e.state.MCP[index].Status = "failed"
			e.state.MCP[index].Error = e.scrubText(err.Error())
			recordErr := e.record("kun/mcp.failed", e.state.MCP[index])
			e.mu.Unlock()
			if recordErr != nil {
				return recordErr
			}
			return fmt.Errorf("MCP %s: %s", spec.Name, e.scrubText(err.Error()))
		}
	}
	return nil
}
func (e *Engine) listMCPTools(ctx context.Context, spec p.MCPServer, client *mcp.Client) ([]p.MCPTool, error) {
	tools := []p.MCPTool{}
	seenNames := map[string]bool{}
	seenCursors := map[string]bool{}
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := e.mcpRequest(ctx, spec.Name, "tools/list", params, func(c context.Context) (json.RawMessage, error) { return client.Call(c, "tools/list", params) })
		if err != nil {
			return nil, err
		}
		var value struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
				Annotations json.RawMessage `json:"annotations"`
				Execution   struct {
					TaskSupport string `json:"taskSupport"`
				} `json:"execution"`
			} `json:"tools"`
			Next string `json:"nextCursor"`
		}
		if err = json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		if value.Tools == nil {
			return nil, fmt.Errorf("MCP tools/list requires a tools array")
		}
		for _, tool := range value.Tools {
			if tool.Name == "" || len(tool.Name) > 256 || seenNames[tool.Name] {
				return nil, fmt.Errorf("invalid or duplicate MCP tool name")
			}
			seenNames[tool.Name] = true
			if len(seenNames) > 256 {
				return nil, fmt.Errorf("MCP server lists too many tools")
			}
			if spec.Allowlist && !contains(spec.EnabledTools, tool.Name) || contains(spec.DisabledTools, tool.Name) {
				continue
			}
			if tool.Execution.TaskSupport == "required" {
				return nil, fmt.Errorf("MCP task-based tool %s is unsupported; disable it", tool.Name)
			}
			var schema map[string]any
			if json.Unmarshal(tool.InputSchema, &schema) != nil || schema["type"] != "object" || len(tool.InputSchema) > 65536 || len(tool.Description) > 16384 {
				return nil, fmt.Errorf("invalid or oversized schema for MCP tool %s", tool.Name)
			}
			mode := spec.ApprovalModes[tool.Name]
			if mode == "" {
				mode = "prompt"
			}
			tools = append(tools, p.MCPTool{Alias: mcpAlias(spec.Name, tool.Name), Server: spec.Name, Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, Annotations: tool.Annotations, ApprovalMode: mode})
		}
		if value.Next == "" {
			sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
			return tools, nil
		}
		if len(value.Next) > 4096 || seenCursors[value.Next] {
			return nil, fmt.Errorf("MCP tools/list repeated or invalid cursor")
		}
		cursor = value.Next
		seenCursors[cursor] = true
	}
	return nil, fmt.Errorf("MCP tools/list exceeds 32 pages")
}
func (e *Engine) mcpTool(name string) (p.MCPTool, bool) {
	for _, t := range e.state.MCPTools {
		if t.Alias == name {
			return t, true
		}
	}
	return p.MCPTool{}, false
}
func (e *Engine) callMCP(ctx context.Context, tool p.MCPTool, call p.ToolCall) (string, json.RawMessage, bool, error) {
	var args map[string]json.RawMessage
	if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
		return "MCP arguments must be an object", nil, true, nil
	}
	conn := e.connections[tool.Server]
	if conn == nil {
		return "", nil, false, fmt.Errorf("MCP connection unavailable; action outcome unknown")
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(conn.spec.ToolTimeout*float64(time.Second)))
	defer cancel()
	params := map[string]any{"name": tool.Name, "arguments": args}
	raw, err := e.mcpRequest(callCtx, tool.Server, "tools/call", params, func(c context.Context) (json.RawMessage, error) { return conn.client.Call(c, "tools/call", params) })
	if err != nil {
		return "", nil, false, err
	}
	var result struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Resource struct {
				Text string `json:"text"`
			} `json:"resource"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return "", nil, false, fmt.Errorf("invalid MCP tool result: %w", err)
	}
	if result.Content == nil {
		return "", nil, false, fmt.Errorf("MCP tool result requires a content array; action outcome unknown")
	}
	parts := []string{}
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			parts = append(parts, c.Text)
		case "resource":
			if c.Resource.Text != "" {
				parts = append(parts, c.Resource.Text)
			} else {
				parts = append(parts, "[MCP resource returned; binary content is not supplied to the text model]")
			}
		default:
			parts = append(parts, "[MCP "+c.Type+" content returned; not supplied to the text model]")
		}
	}
	if len(result.Structured) > 0 && string(result.Structured) != "null" {
		parts = append(parts, string(result.Structured))
	}
	if len(parts) == 0 {
		parts = append(parts, "[MCP tool returned no text content]")
	}
	return strings.Join(parts, "\n"), raw, result.IsError, nil
}
func (e *Engine) closeMCP() {
	for _, conn := range e.connections {
		conn.client.Close()
	}
	e.connections = nil
	e.mcpSpecs = nil
}
