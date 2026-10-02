package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/store"
)

const DefaultInstance = "default"

// An instance is a persistent configuration identity, not a single process.
type Instance struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	DefaultModel string      `json:"defaultModel"`
	Permissions  Permissions `json:"permissions"`
	CodexHome    string      `json:"codexHome"`
	Managed      bool        `json:"managed"`
	Revision     int         `json:"revision"`
	Created      string      `json:"created"`
}
type InstancePatch struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	DefaultModel string       `json:"defaultModel"`
	Permissions  *Permissions `json:"permissions,omitempty"`
	Revision     int          `json:"revision"`
}

func instanceID(ids []string) string {
	if len(ids) > 0 && ids[0] != "" {
		return ids[0]
	}
	return DefaultInstance
}
func (m *Manager) loadInstances() error {
	rows, err := m.Store.List("instance")
	if err != nil {
		return err
	}
	for _, b := range rows {
		var v Instance
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		v.Permissions = v.Permissions.normalized()
		m.instances[v.ID] = &v
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		home = filepath.Join(user, ".codex")
	}
	if m.Demo {
		home = filepath.Join(m.Data, "demo-codex")
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return err
	}
	// The default follows the service's existing environment, including on upgrades.
	v := m.instances[DefaultInstance]
	if v == nil {
		v = &Instance{ID: DefaultInstance, Name: "默认实例", Created: store.Now()}
	}
	v.CodexHome, v.Managed = home, false
	v.Permissions = v.Permissions.normalized()
	m.instances[v.ID] = v
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	return m.Store.Put("instance", v.ID, v)
}
func (m *Manager) Instances() []Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Instance{}
	for _, v := range m.instances {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == DefaultInstance || out[j].ID == DefaultInstance {
			return out[i].ID == DefaultInstance && out[j].ID != DefaultInstance
		}
		if out[i].Created == out[j].Created {
			return out[i].ID < out[j].ID
		}
		return out[i].Created < out[j].Created
	})
	return out
}
func (m *Manager) Instance(ids ...string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.instances[instanceID(ids)]
	if v == nil {
		return Instance{}, failure(404, "instance_not_found", "实例不存在")
	}
	return *v, nil
}
func validateInstance(p InstancePatch) error {
	if p.Permissions != nil {
		if e := p.Permissions.validate(); e != nil {
			return e
		}
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 || len(p.Description) > 4096 || len(p.DefaultModel) > 160 {
		return errors.New("实例名称不能为空，最多 160 字节；描述最多 4 KiB，模型名称最多 160 字节")
	}
	return nil
}
func (m *Manager) CreateInstance(p InstancePatch) (Instance, error) {
	if err := validateInstance(p); err != nil {
		return Instance{}, err
	}
	id := store.ID()
	v := Instance{ID: id, Name: strings.TrimSpace(p.Name), Description: p.Description, DefaultModel: strings.TrimSpace(p.DefaultModel), Managed: true, Created: store.Now(), CodexHome: filepath.Join(m.Data, "instances", id, "codex")}
	v.Permissions = Permissions{}.normalized()
	if p.Permissions != nil {
		v.Permissions = p.Permissions.normalized()
	}
	if err := os.MkdirAll(v.CodexHome, 0700); err != nil {
		return Instance{}, err
	}
	if err := m.Store.Put("instance", id, v); err != nil {
		return Instance{}, err
	}
	m.mu.Lock()
	m.instances[id] = &v
	m.mu.Unlock()
	return v, nil
}
func (m *Manager) PatchInstance(id string, p InstancePatch) (Instance, error) {
	if err := validateInstance(p); err != nil {
		return Instance{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.instances[id]
	if v == nil {
		return Instance{}, failure(404, "instance_not_found", "实例不存在")
	}
	if p.Revision != v.Revision {
		return Instance{}, failure(409, "revision_conflict", "实例配置已更新，请刷新后重试")
	}
	next := *v
	next.Name = strings.TrimSpace(p.Name)
	next.Description = p.Description
	next.DefaultModel = strings.TrimSpace(p.DefaultModel)
	if p.Permissions != nil {
		next.Permissions = p.Permissions.normalized()
	}
	next.Revision++
	if err := m.Store.Put("instance", id, next); err != nil {
		return Instance{}, err
	}
	*v = next
	return next, nil
}

// opts = instance ID, scope. Legacy callers edit project skills by default.
func (m *Manager) skillRoot(wid string, opts []string) (string, string, error) {
	w, err := m.Workspace(wid)
	if err != nil {
		return "", "", err
	}
	i, err := m.Instance(instanceID(opts))
	if err != nil {
		return "", "", err
	}
	scope := "project"
	if len(opts) > 1 && opts[1] != "" {
		scope = opts[1]
	}
	switch scope {
	case "project":
		return w.Path, ".agents/skills", nil
	case "instance":
		return i.CodexHome, "skills", nil
	default:
		return "", "", errors.New("scope 必须为 project 或 instance")
	}
}
