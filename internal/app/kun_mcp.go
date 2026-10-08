package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Kun owns an instance-scoped configuration; reading it never launches Codex.
type kunMCPConfig struct {
	Revision int            `json:"revision"`
	Servers  map[string]any `json:"servers"`
}

func (m *Manager) kunMCPConfig(id string) (kunMCPConfig, error) {
	var c kunMCPConfig
	err := m.Store.Get("kun-mcp-config", id, &c)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if c.Servers == nil {
		c.Servers = map[string]any{}
	}
	return c, err
}
func (m *Manager) kunReadConfig(id string) (nativeConfig, error) {
	saved, err := m.kunMCPConfig(id)
	if err != nil {
		return nativeConfig{}, err
	}
	var c nativeConfig
	raw := p.JSON(map[string]any{"config": map[string]any{"mcp_servers": saved.Servers}, "layers": []any{map[string]any{"name": map[string]string{"type": "user"}, "version": strconv.Itoa(saved.Revision), "config": map[string]any{"mcp_servers": saved.Servers}}}, "origins": map[string]string{"runtime": "kun", "scope": "instance"}})
	err = json.Unmarshal(raw, &c)
	return c, err
}
func (m *Manager) kunMCPView(id string) (any, error) {
	saved, err := m.kunMCPConfig(id)
	if err != nil {
		return nil, err
	}
	statuses := []any{}
	names := []string{}
	for name := range saved.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfg, _ := saved.Servers[name].(map[string]any)
		status := "configured"
		if cfg["enabled"] == false {
			status = "disabled"
		}
		statuses = append(statuses, map[string]any{"name": name, "status": status, "tools": []any{}})
	}
	return map[string]any{"runtime": "kun", "userServers": maskConfig(saved.Servers), "effectiveServers": maskConfig(saved.Servers), "version": strconv.Itoa(saved.Revision), "origins": map[string]string{"scope": "instance"}, "status": map[string]any{"data": statuses}, "applies": "Kun 在下一轮开始时连接服务并固定工具清单；当前运行使用原有快照。"}, nil
}

// Caller holds configMu. Validate the entire import before the single database write.
func (m *Manager) writeKunMCP(id, current string, servers map[string]any) (any, error) {
	if len(servers) > 100 {
		return nil, failure(400, "kun_mcp_limit", "最多保存 100 个 MCP 服务")
	}
	enabled := 0
	for name, raw := range servers {
		cfg, ok := raw.(map[string]any)
		if !ok {
			return nil, failure(400, "kun_mcp_invalid", "无效 MCP 配置")
		}
		if _, err := kunMCPSpec(name, current, cfg, false); err != nil {
			return nil, failure(400, "kun_mcp_invalid", err.Error())
		}
		if cfg["enabled"] != false {
			enabled++
		}
	}
	if enabled > 16 {
		return nil, failure(400, "kun_mcp_limit", "Kun 每轮最多连接 16 个已启用服务")
	}
	saved, err := m.kunMCPConfig(id)
	if err != nil {
		return nil, err
	}
	if strconv.Itoa(saved.Revision) != current {
		return nil, failure(409, "revision_conflict", "MCP 配置已变化，请刷新")
	}
	saved.Revision++
	saved.Servers = servers
	if err = m.Store.Put("kun-mcp-config", id, saved); err != nil {
		return nil, err
	}
	return map[string]any{"saved": true, "version": strconv.Itoa(saved.Revision), "applies": "下一轮 Kun 任务使用新 MCP 配置；本轮使用原有工具与权限快照。"}, nil
}
func kunMCPSpec(name, revision string, cfg map[string]any, resolve bool) (p.MCPServer, error) {
	spec := p.MCPServer{Name: name, Revision: revision, StartupTimeout: 30, ToolTimeout: 60, Environment: map[string]string{}, Headers: map[string]string{}, ApprovalModes: map[string]string{}}
	spec.Command, _ = cfg["command"].(string)
	spec.URL, _ = cfg["url"].(string)
	array := func(key string) []string {
		out := []string{}
		if values, ok := cfg[key].([]any); ok {
			for _, v := range values {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
			}
		}
		return out
	}
	spec.Args = array("args")
	spec.EnabledTools = array("enabled_tools")
	spec.DisabledTools = array("disabled_tools")
	_, spec.Allowlist = cfg["enabled_tools"]
	if n, ok := cfg["startup_timeout_sec"].(float64); ok {
		spec.StartupTimeout = n
	}
	if n, ok := cfg["tool_timeout_sec"].(float64); ok {
		spec.ToolTimeout = n
	}
	for field, target := range map[string]map[string]string{"env": spec.Environment, "http_headers": spec.Headers} {
		if values, ok := cfg[field].(map[string]any); ok {
			for k, v := range values {
				if s, ok := v.(string); ok {
					if field == "http_headers" {
						k = http.CanonicalHeaderKey(k)
					}
					target[k] = s
				}
			}
		}
	}
	envValue := func(key string) (string, error) {
		// Validate the reference before lookup; missing secrets are checked at execution time.
		if key == "" || len(key) > 128 || !(key[0] == '_' || key[0] >= 'A' && key[0] <= 'Z' || key[0] >= 'a' && key[0] <= 'z') {
			return "", fmt.Errorf("%s: 无效环境变量名", name)
		}
		for _, c := range key {
			if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				return "", fmt.Errorf("%s: 无效环境变量名", name)
			}
		}
		if !resolve {
			return "configured-reference", nil
		}
		value, ok := os.LookupEnv(key)
		if !ok || value == "" {
			return "", fmt.Errorf("%s: 环境变量未设置：%s", name, key)
		}
		return value, nil
	}
	for _, key := range array("env_vars") {
		value, err := envValue(key)
		if err != nil {
			return spec, err
		}
		if _, exists := spec.Environment[key]; !exists {
			spec.Environment[key] = value
		}
	}
	if values, ok := cfg["env_http_headers"].(map[string]any); ok {
		for k, v := range values {
			key, _ := v.(string)
			value, err := envValue(key)
			if err != nil {
				return spec, err
			}
			spec.Headers[http.CanonicalHeaderKey(k)] = value
		}
	}
	if v, exists := cfg["bearer_token_env_var"]; exists {
		key, ok := v.(string)
		if !ok {
			return spec, fmt.Errorf("%s: bearer_token_env_var 必须是环境变量名", name)
		}
		value, err := envValue(key)
		if err != nil {
			return spec, err
		}
		spec.Headers["Authorization"] = "Bearer " + value
	}
	if policies, ok := cfg["tools"].(map[string]any); ok {
		for tool, raw := range policies {
			policy, _ := raw.(map[string]any)
			if _, exists := policy["output_token_limit"]; exists {
				return spec, fmt.Errorf("%s/%s: Kun 暂不支持 output_token_limit；保留结果有统一大小上限", name, tool)
			}
			if mode, ok := policy["approval_mode"].(string); ok {
				spec.ApprovalModes[tool] = mode
			}
		}
	}
	if spec.URL != "" && len(spec.Environment) > 0 {
		return spec, fmt.Errorf("%s: HTTP 服务请使用请求头环境变量配置", name)
	}
	if spec.Command != "" && len(spec.Headers) > 0 {
		return spec, fmt.Errorf("%s: stdio 服务请使用 env / env_vars", name)
	}
	if strings.TrimSpace(spec.Command) == "" && spec.URL == "" {
		return spec, fmt.Errorf("%s: command 或 url 必填", name)
	}
	return spec, spec.Validate()
}
func (m *Manager) kunRuntimeMCP(id string) ([]p.MCPServer, error) {
	config, err := m.kunMCPConfig(id)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for name := range config.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []p.MCPServer{}
	for _, name := range names {
		cfg, _ := config.Servers[name].(map[string]any)
		if cfg["enabled"] == false {
			continue
		}
		spec, err := kunMCPSpec(name, strconv.Itoa(config.Revision), cfg, true)
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
	return out, nil
}
