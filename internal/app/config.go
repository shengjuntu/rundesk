package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/shengjuntu/rundesk/internal/store"
)

var slug = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var configMu sync.Mutex

func (m *Manager) validateInput(w Workspace, in Input, ids ...string) error {
	root, e := os.OpenRoot(w.Path)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, p := range in.Files {
		if e := m.libraryPathDeleted(w.ID, p); e != nil {
			return e
		}
		if !strings.HasPrefix(p, "uploads/") || !safePath(p) {
			return errors.New("无效上传文件路径")
		}
		f, e := root.Open(p)
		if e != nil {
			return e
		}
		s, e := f.Stat()
		f.Close()
		if e != nil || !s.Mode().IsRegular() {
			return errors.New("附件必须是普通文件")
		}
	}
	if len(in.Skills) > 0 {
		raw, e := m.Skills(w.ID, ids...)
		if e != nil {
			return e
		}
		var r struct {
			Data []struct {
				Skills []struct {
					Name    string `json:"name"`
					Path    string `json:"path"`
					Enabled bool   `json:"enabled"`
				} `json:"skills"`
			} `json:"data"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return e
		}
		for _, sk := range in.Skills {
			found := false
			for _, d := range r.Data {
				for _, x := range d.Skills {
					if x.Path == sk.Path && x.Name == sk.Name && x.Enabled {
						found = true
					}
				}
			}
			if !found {
				return fmt.Errorf("Skill 不存在或已禁用: %s", sk.Name)
			}
		}
	}
	return nil
}
func safePath(p string) bool {
	return p != "" && !strings.Contains(p, "\\") && !strings.ContainsRune(p, 0) && !filepath.IsAbs(p) && filepath.ToSlash(filepath.Clean(p)) == p && p != ".." && !strings.HasPrefix(p, "../")
}
func (m *Manager) Skills(wid string, ids ...string) (json.RawMessage, error) {
	w, e := m.Workspace(wid)
	if e != nil {
		return nil, e
	}
	return m.ConfigCall(wid, "skills/list", map[string]any{"cwds": []string{w.Path}, "forceReload": true}, ids...)
}
func (m *Manager) SaveSkill(wid, name, content string, opts ...string) error {
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !slug.MatchString(name) {
		return errors.New("Skill 名称仅支持字母、数字、下划线和连字符，最多 64 字符")
	}
	if len(content) > 256*1024 || !strings.HasPrefix(content, "---\n") {
		return errors.New("SKILL.md 需要 YAML frontmatter，最多 256 KiB")
	}
	rootPath, skillDir, e := m.skillRoot(wid, opts)
	if e != nil {
		return e
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return e
	}
	defer root.Close()
	dir := skillDir + "/" + name
	if e = root.MkdirAll(dir, 0700); e != nil {
		return e
	}
	temp := dir + "/.SKILL-" + store.ID() + ".tmp"
	f, e := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = io.WriteString(f, content)
	defer root.Remove(temp)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return root.Rename(temp, dir+"/SKILL.md")
}
func (m *Manager) ReadSkill(wid, name string, opts ...string) (string, error) {
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
	f, e := root.Open(skillDir + "/" + name + "/SKILL.md")
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 256*1024))
	return string(b), e
}
func (m *Manager) ToggleSkill(wid, path string, enabled bool, ids ...string) error {
	configMu.Lock()
	defer configMu.Unlock()
	raw, e := m.Skills(wid, ids...)
	if e != nil {
		return e
	}
	var r struct {
		Data []struct {
			Skills []Skill `json:"skills"`
		} `json:"data"`
	}
	if e = json.Unmarshal(raw, &r); e != nil {
		return e
	}
	found := false
	for _, d := range r.Data {
		for _, s := range d.Skills {
			if s.Path == path {
				found = true
			}
		}
	}
	if !found {
		return errors.New("Skill 路径不在当前清单中")
	}
	_, e = m.ConfigCall(wid, "skills/config/write", map[string]any{"path": path, "enabled": enabled}, ids...)
	return e
}

type nativeConfig struct {
	Config map[string]any `json:"config"`
	Layers []struct {
		Config map[string]any `json:"config"`
		Name   struct {
			Type string `json:"type"`
			File string `json:"file"`
		} `json:"name"`
		Version string `json:"version"`
	} `json:"layers"`
	Origins map[string]any `json:"origins"`
}

func (m *Manager) readConfig(wid string, ids ...string) (nativeConfig, error) {
	w, e := m.Workspace(wid)
	if e != nil {
		return nativeConfig{}, e
	}
	raw, e := m.ConfigCall(wid, "config/read", map[string]any{"cwd": w.Path, "includeLayers": true}, ids...)
	if e != nil {
		return nativeConfig{}, e
	}
	var c nativeConfig
	e = json.Unmarshal(raw, &c)
	return c, e
}
func userMCP(c nativeConfig) (map[string]any, string) {
	for _, l := range c.Layers {
		if l.Name.Type == "user" {
			v, _ := l.Config["mcp_servers"].(map[string]any)
			if v == nil {
				v = map[string]any{}
			}
			return v, l.Version
		}
	}
	return map[string]any{}, ""
}
func maskConfig(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			if k == "env" || k == "http_headers" {
				if fields, ok := v.(map[string]any); ok {
					masked := map[string]any{}
					for name := range fields {
						masked[name] = "[redacted]"
					}
					out[k] = masked
				} else {
					out[k] = "[redacted]"
				}
			} else {
				if sensitiveField(k) {
					out[k] = "[redacted]"
				} else {
					out[k] = maskConfig(v)
				}
			}
		}
		return out
	case []any:
		for i, v := range x {
			x[i] = maskConfig(v)
		}
		return x
	default:
		return x
	}
}
func (m *Manager) MCP(wid string, ids ...string) (any, error) {
	c, e := m.readConfig(wid, ids...)
	if e != nil {
		return nil, e
	}
	user, version := userMCP(c)
	status, se := m.ConfigCall(wid, "mcpServerStatus/list", map[string]any{"limit": 100}, ids...)
	var state any
	if se != nil {
		state = map[string]string{"error": se.Error()}
	} else {
		_ = json.Unmarshal(status, &state)
	}
	return map[string]any{"userServers": maskConfig(user), "effectiveServers": maskConfig(c.Config["mcp_servers"]), "version": version, "origins": c.Origins, "status": state}, nil
}
func (m *Manager) SaveMCP(wid, name, version string, value map[string]any, remove bool, ids ...string) (any, error) {
	if !slug.MatchString(name) {
		return nil, errors.New("无效 MCP 名称")
	}
	configMu.Lock()
	defer configMu.Unlock()
	c, e := m.readConfig(wid, ids...)
	if e != nil {
		return nil, e
	}
	servers, current := userMCP(c)
	if version != current {
		return nil, errors.New("配置已变更，请刷新后重试")
	}
	if remove {
		delete(servers, name)
	} else {
		old, _ := servers[name].(map[string]any)
		value, e = prepareMCP(value, old)
		if e != nil {
			return nil, e
		}
		servers[name] = value
	}
	return m.writeMCP(wid, current, servers, ids...)
}
func (m *Manager) writeMCP(wid, current string, servers map[string]any, ids ...string) (any, error) {
	p := map[string]any{"keyPath": "mcp_servers", "value": servers, "mergeStrategy": "replace"}
	if current != "" {
		p["expectedVersion"] = current
	}
	res, e := m.ConfigCall(wid, "config/value/write", p, ids...)
	if e != nil {
		return nil, e
	}
	_, reloadErr := m.ConfigCall(wid, "config/mcpServer/reload", map[string]any{}, ids...)
	result := map[string]any{"saved": true, "write": res, "applies": "会话在下一次运行前重新加载 MCP 配置"}
	if reloadErr != nil {
		result["reloadError"] = reloadErr.Error()
	} else {
		result["configurationConnectionReloaded"] = true
	}
	return result, nil
}
