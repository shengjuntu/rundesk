package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func prepareMCP(value, old map[string]any) (map[string]any, error) {
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	var copy map[string]any
	if e = json.Unmarshal(raw, &copy); e != nil {
		return nil, e
	}
	value = copy
	allowed := map[string]bool{"command": true, "args": true, "env": true, "env_vars": true, "url": true, "bearer_token_env_var": true, "http_headers": true, "env_http_headers": true, "enabled": true, "startup_timeout_sec": true, "tool_timeout_sec": true, "enabled_tools": true, "disabled_tools": true}
	for key := range value {
		if !allowed[key] {
			return nil, fmt.Errorf("暂不支持 MCP 字段: %s", key)
		}
	}
	command, _ := value["command"].(string)
	rawURL, _ := value["url"].(string)
	if (strings.TrimSpace(command) == "") == (rawURL == "") {
		return nil, errors.New("command 和 url 必须填写其中一个")
	}
	if rawURL != "" {
		u, e := url.Parse(rawURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("MCP URL 需要有效的 http(s) 地址")
		}
		if u.User != nil {
			return nil, errors.New("请通过环境变量或请求头配置凭据，不要在 URL 中嵌入密码")
		}
	}
	for _, k := range []string{"args", "env_vars", "enabled_tools", "disabled_tools"} {
		if v, ok := value[k]; ok {
			a, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%s 必须为字符串数组", k)
			}
			for _, item := range a {
				if _, ok := item.(string); !ok {
					return nil, fmt.Errorf("%s 必须为字符串数组", k)
				}
			}
		}
	}
	if v, ok := value["enabled"]; ok {
		if _, ok = v.(bool); !ok {
			return nil, errors.New("enabled 必须为布尔值")
		}
	}
	for _, k := range []string{"startup_timeout_sec", "tool_timeout_sec"} {
		if v, ok := value[k]; ok {
			n, ok := v.(float64)
			if !ok || n <= 0 {
				return nil, fmt.Errorf("%s 必须是正数", k)
			}
		}
	}
	for _, k := range []string{"env", "http_headers", "env_http_headers"} {
		if v, ok := value[k]; ok {
			fields, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s 必须为对象", k)
			}
			prior, _ := old[k].(map[string]any)
			for field, v := range fields {
				str, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("%s.%s 必须为字符串", k, field)
				}
				if str == "[redacted]" && k != "env_http_headers" {
					pv, exists := prior[field]
					if !exists {
						return nil, fmt.Errorf("请补充 %s.%s 的密钥值，导出文件中未包含密钥", k, field)
					}
					fields[field] = pv
				}
			}
		}
	}
	return value, nil
}

type MCPBundle struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Kind          string                    `json:"kind"`
	Servers       map[string]map[string]any `json:"servers"`
	Secrets       string                    `json:"secrets,omitempty"`
}

func (m *Manager) ExportMCP(wid string, ids ...string) (any, error) {
	c, e := m.readConfig(wid, ids...)
	if e != nil {
		return nil, e
	}
	servers, _ := userMCP(c)
	return map[string]any{"schemaVersion": 1, "kind": "rundesk.mcp", "servers": maskConfig(servers), "secrets": "redacted"}, nil
}
func (m *Manager) ImportMCP(wid, version string, b MCPBundle, overwrite bool, ids ...string) (any, error) {
	if b.SchemaVersion != 1 || b.Kind != "rundesk.mcp" || len(b.Servers) == 0 || len(b.Servers) > 100 {
		return nil, errors.New("需要 schemaVersion=1、kind=rundesk.mcp 和 1–100 个服务")
	}
	configMu.Lock()
	defer configMu.Unlock()
	c, e := m.readConfig(wid, ids...)
	if e != nil {
		return nil, e
	}
	servers, current := userMCP(c)
	if current != version {
		return nil, errors.New("配置已变更，请刷新后重试")
	}
	for name, value := range b.Servers {
		if !slug.MatchString(name) {
			return nil, fmt.Errorf("无效 MCP 名称: %s", name)
		}
		old, exists := servers[name]
		if exists && !overwrite {
			return nil, fmt.Errorf("服务 %s 已存在；需要明确选择覆盖同名服务", name)
		}
		previous, _ := old.(map[string]any)
		prepared, e := prepareMCP(value, previous)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", name, e)
		}
		servers[name] = prepared
	}
	return m.writeMCP(wid, current, servers, ids...)
}

func (m *Manager) DeleteSkill(wid, name string, opts ...string) (string, error) {
	if !slug.MatchString(name) {
		return "", errors.New("无效 Skill 名称")
	}
	rootPath, skillDir, e := m.skillRoot(wid, opts)
	if e != nil {
		return "", e
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return "", e
	}
	defer root.Close()
	src := skillDir + "/" + name + "/SKILL.md"
	info, e := root.Lstat(src)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("只移除普通 SKILL.md 文件")
	}
	dest := ".rundesk/skill-backups/" + name + "-" + store.ID() + "/SKILL.md"
	if e = root.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return "", e
	}
	if e = root.Rename(src, dest); e != nil {
		return "", e
	}
	return dest, nil
}

type Check struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Detail     string   `json:"detail"`
	DurationMS int64    `json:"durationMs"`
	Command    []string `json:"command,omitempty"`
	Output     string   `json:"output,omitempty"`
	ExitCode   *int     `json:"exitCode,omitempty"`
	Hint       string   `json:"hint,omitempty"`
}

func (m *Manager) Diagnostics(wid string, ids ...string) (any, error) {
	i, e := m.Instance(ids...)
	if e != nil {
		return nil, e
	}
	w, e := m.Workspace(wid)
	if e != nil {
		return nil, e
	}
	checks := []Check{}
	add := func(name string, fn func() (string, error)) {
		start := time.Now()
		detail, e := fn()
		status := "ok"
		if e != nil {
			status = "error"
			detail = e.Error()
		}
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, DurationMS: time.Since(start).Milliseconds()})
	}
	add("工作区读写", func() (string, error) {
		root, e := os.OpenRoot(w.Path)
		if e != nil {
			return "", e
		}
		defer root.Close()
		name := ".rundesk-check-" + store.ID()
		f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		defer root.Remove(name)
		if _, e = f.WriteString("RunDesk diagnostic\n"); e != nil {
			f.Close()
			return "", e
		}
		if e = f.Close(); e != nil {
			return "", e
		}
		return w.Path, nil
	})
	add("Codex 可执行文件", func() (string, error) {
		if m.Demo {
			return "演示协议模拟器；未检查真实 Codex", nil
		}
		path, e := exec.LookPath(m.Codex)
		if e != nil {
			return "", e
		}
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, path, "--version").CombinedOutput()
		if e != nil {
			return "", e
		}
		return strings.TrimSpace(string(out)), nil
	})
	if runtime.GOOS == "linux" && !m.Demo {
		path, err := exec.LookPath("bwrap")
		if err != nil {
			checks = append(checks, Check{Name: "系统 bubblewrap", Status: "warning", Detail: "PATH 中未找到 bwrap；Codex 可能使用内置版本。以下实际执行结果才代表沙箱启动是否通过。"})
		} else {
			checks = append(checks, Check{Name: "系统 bubblewrap", Status: "ok", Detail: path})
		}
	}
	checks = append(checks, m.sandboxCheck(w, i))
	add("App Server 配置接口", func() (string, error) {
		_, e := m.readConfig(wid, ids...)
		return "initialize 与 config/read 成功", e
	})
	add("Skills 扫描", func() (string, error) {
		_, e := m.Skills(wid, ids...)
		return "skills/list 成功；详情见 Skills 页", e
	})
	add("MCP 状态接口", func() (string, error) {
		_, e := m.ConfigCall(wid, "mcpServerStatus/list", map[string]any{"limit": 100}, ids...)
		return "状态读取成功；各服务的连接和工具见 MCP 页", e
	})
	return map[string]any{"version": "0.6.1", "platform": runtime.GOOS + "/" + runtime.GOARCH, "demo": m.Demo, "loadedConnections": m.loaded.Load(), "instanceId": i.ID, "codexHome": i.CodexHome, "checks": checks, "permissions": i.Permissions, "note": "检查不会调用模型。沙箱检查固定使用 workspace-write 和禁网设置；通过仅说明最小命令可执行，不代表模型凭据、网络或全部工具已验证。"}, nil
}
