package app

import (
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/store"
	"regexp"
	"strings"
)

type ExecutionSpec struct {
	Mode           string `json:"mode"`
	Image          string `json:"image,omitempty"`
	ImageID        string `json:"imageId,omitempty"`
	ImageVersionID string `json:"imageVersionId,omitempty"`
	CPUs           int    `json:"cpus,omitempty"`
	MemoryMB       int    `json:"memoryMB,omitempty"`
	PidsLimit      int    `json:"pidsLimit,omitempty"`
	IdleMinutes    int    `json:"idleMinutes,omitempty"`
	Network        string `json:"network,omitempty"`
}

func (s ExecutionSpec) normalized() ExecutionSpec {
	if s.Mode == "" || s.Mode == "local" {
		return ExecutionSpec{Mode: "local"}
	}
	s.Image = strings.TrimSpace(s.Image)
	if s.CPUs == 0 {
		s.CPUs = 2
	}
	if s.MemoryMB == 0 {
		s.MemoryMB = 2048
	}
	if s.PidsLimit == 0 {
		s.PidsLimit = 256
	}
	if s.IdleMinutes == 0 {
		s.IdleMinutes = 15
	}
	if s.Network == "" {
		s.Network = "bridge"
	}
	return s
}

var imageReference = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./:@-]{0,254}$`)

func (s ExecutionSpec) validate() error {
	s = s.normalized()
	if s.Mode == "local" {
		return nil
	}
	if s.Mode != "docker" || !imageReference.MatchString(s.Image) || s.CPUs < 1 || s.CPUs > 32 || s.MemoryMB < 256 || s.MemoryMB > 65536 || s.PidsLimit < 32 || s.PidsLimit > 4096 || s.IdleMinutes < 1 || s.IdleMinutes > 1440 || (s.Network != "bridge" && s.Network != "none") {
		return failure(400, "invalid_execution", "Docker 需有效镜像、1–32 CPU、256–65536 MiB 内存、32–4096 进程、1–1440 分钟空闲时间及 bridge/none 网络")
	}
	return nil
}
func (m *Manager) SetExecution(iid string, revision int, spec ExecutionSpec) (Instance, error) {
	if e := spec.validate(); e != nil {
		return Instance{}, e
	}
	spec = spec.normalized()
	if iid == DefaultInstance && spec.Mode != "local" {
		return Instance{}, failure(400, "assistant_requires_local", "通用助手固定使用本机 Codex；请在应用中配置 Docker")
	}
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	i, e := m.Instance(iid)
	if e != nil {
		return i, e
	}
	if i.AgentRuntime.Kind == "kun" && spec.Mode != "local" {
		return i, failure(400, "kun_local_only", "首版 Kun 仅支持本机 worker")
	}
	if i.Revision != revision {
		return i, failure(409, "revision_conflict", "配置已变化，请刷新后重试")
	}
	if spec.Mode == "docker" && (spec.ImageID != "" || spec.ImageVersionID != "") {
		v, err := m.imageVersion(iid, spec.ImageVersionID)
		if err != nil {
			return i, err
		}
		if v.ImageID != spec.ImageID || v.Reference != spec.Image {
			return i, failure(400, "image_version_mismatch", "镜像配置必须与登记版本一致")
		}
	}
	if i.Execution.normalized() == spec {
		return i, nil
	}
	// Docker environments keep their own snapshot until explicitly updated.
	keepEnvironments := i.Execution.normalized().Mode == "docker" && spec.Mode == "docker"
	if !keepEnvironments {
		r, e := m.ReloadInstance(iid)
		if e != nil {
			return i, e
		}
		if r.(map[string]any)["busyConnections"].(int) > 0 {
			return i, failure(409, "execution_busy", "应用仍有运行或配置请求，请结束后修改")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if !keepEnvironments && s.InstanceID == iid && active(s.Status) {
			return i, failure(409, "execution_busy", "应用仍有运行中的任务")
		}
	}
	current := m.instances[iid]
	if current.Revision != revision {
		return i, failure(409, "revision_conflict", "配置已变化，请刷新后重试")
	}
	next := *current
	next.Execution = spec
	next.Revision++
	if e = m.Store.Put("instance", iid, next); e != nil {
		return i, e
	}
	*current = next
	return next, nil
}
func (m *Manager) checkSessionExecution(s Session) error {
	i, e := m.Instance(s.InstanceID)
	if e != nil {
		return e
	}
	if runtimeKind(s.RuntimeKind) != runtimeKind(i.AgentRuntime.Kind) {
		return failure(409, "session_runtime_changed", "此会话属于原 Agent 后端，请恢复原配置或新建会话")
	}
	if s.RuntimeKind == "kun" && i.Execution.normalized().Mode != "local" {
		return failure(400, "kun_local_only", "首版 Kun 仅支持本机 worker")
	}
	mode := s.ExecutionMode
	if mode == "" {
		mode = "local"
	}
	if mode != i.Execution.normalized().Mode {
		return failure(409, "session_execution_changed", "此会话属于原运行方式。请恢复原运行方式后继续，或新建会话使用当前环境；原生历史不会自动迁移")
	}
	if mode == "docker" && s.EnvironmentID != environmentID(s.InstanceID, s.WorkspaceID) {
		return failure(409, "session_environment_mismatch", "会话运行环境不匹配")
	}
	return nil
}
func (m *Manager) effectiveInstance(i Instance, w Workspace) (Instance, *Environment, error) {
	i.Execution = i.Execution.normalized()
	if i.Execution.Mode != "docker" {
		return i, nil, nil
	}
	v, e := m.ensureEnvironment(i, w)
	if e != nil {
		return i, nil, e
	}
	i.CodexHome = v.CodexHome
	return i, &v, nil
}
func (m *Manager) loadEnvironments() error {
	m.environments = map[string]*Environment{}
	rows, e := m.Store.List("environment")
	if e != nil {
		return e
	}
	for _, b := range rows {
		var v Environment
		if e = json.Unmarshal(b, &v); e != nil {
			return e
		}
		v.ActiveConnections = 0
		v.State = "unknown"
		v.Error = ""
		m.environments[v.ID] = &v
		if e = m.clearOldLeases(v.ID); e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) saveEnvironmentLocked(v *Environment) error {
	copy := *v
	copy.ActiveConnections = 0
	copy.ObservedAt = store.Now()
	if e := m.Store.Put("environment", v.ID, copy); e != nil {
		return e
	}
	m.environments[v.ID] = &copy
	*v = copy
	return nil
}
