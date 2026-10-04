package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/store"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Environment struct {
	DeployedAt        string        `json:"deployedAt,omitempty"`
	ID                string        `json:"id"`
	InstanceID        string        `json:"instanceId"`
	WorkspaceID       string        `json:"workspaceId"`
	ContainerName     string        `json:"containerName"`
	ContainerID       string        `json:"containerId,omitempty"`
	ImageID           string        `json:"imageId,omitempty"`
	Spec              ExecutionSpec `json:"spec"`
	CodexHome         string        `json:"codexHome"`
	State             string        `json:"state"`
	Error             string        `json:"error,omitempty"`
	ExitCode          int           `json:"exitCode"`
	OOMKilled         bool          `json:"oomKilled"`
	Revision          int           `json:"revision"`
	Created           string        `json:"created"`
	ObservedAt        string        `json:"observedAt"`
	LastUsed          string        `json:"lastUsed"`
	ActiveConnections int32         `json:"activeConnections"`
}

func environmentID(iid, wid string) string {
	sum := sha256.Sum256([]byte(iid + "/" + wid))
	return hex.EncodeToString(sum[:12])
}
func (m *Manager) environmentRoot(id string) string { return filepath.Join(m.Data, "environments", id) }
func (m *Manager) environmentOwner() string {
	sum := sha256.Sum256([]byte(m.Data))
	return hex.EncodeToString(sum[:12])
}
func (m *Manager) environmentRefs(id string) *atomic.Int32 {
	v, _ := m.environmentLeases.LoadOrStore(id, &atomic.Int32{})
	return v.(*atomic.Int32)
}
func (m *Manager) clearOldLeases(id string) error {
	path := filepath.Join(m.environmentRoot(id), "leases")
	items, e := os.ReadDir(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, v := range items {
		if !v.IsDir() {
			if e = os.Remove(filepath.Join(path, v.Name())); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m *Manager) validateDockerWorkspace(w Workspace) error {
	real, e := filepath.EvalSymlinks(w.Path)
	if e != nil {
		return e
	}
	if real != w.Path || within(w.Path, m.Data) || within(filepath.Join(m.Data, "instances"), w.Path) || within(filepath.Join(m.Data, "environments"), w.Path) || within(filepath.Join(m.Data, "runtime"), w.Path) {
		return failure(400, "unsafe_docker_workspace", "Docker 项目不能包含 RunDesk 数据根目录、应用配置、运行控制目录或使用符号链接路径")
	}
	for _, i := range m.Instances() {
		if within(w.Path, i.CodexHome) || within(i.CodexHome, w.Path) {
			return failure(400, "unsafe_docker_workspace", "项目目录不能包含助手或应用的 Codex 配置目录")
		}
	}
	for _, other := range m.Workspaces() {
		if other.ID != w.ID && (within(w.Path, other.Path) || within(other.Path, w.Path)) {
			return failure(409, "overlapping_docker_workspace", "Docker 项目不能与其他项目使用相同或互相包含的目录")
		}
	}
	return nil
}
func (m *Manager) ensureEnvironment(i Instance, w Workspace) (Environment, error) {
	m.environmentMu.Lock()
	defer m.environmentMu.Unlock()
	return m.ensureEnvironmentLocked(i, w)
}
func (m *Manager) ensureEnvironmentLocked(i Instance, w Workspace) (Environment, error) {
	id := environmentID(i.ID, w.ID)
	if v := m.environments[id]; v != nil {
		copy := *v
		copy.ActiveConnections = m.environmentRefs(id).Load()
		return copy, nil
	}
	if i.Execution.normalized().Mode != "docker" {
		return Environment{}, failure(400, "not_docker_application", "请先为应用配置 Docker 镜像")
	}
	if e := m.validateDockerWorkspace(w); e != nil {
		return Environment{}, e
	}
	for _, v := range m.environments {
		if v.WorkspaceID == w.ID && v.InstanceID != i.ID {
			return Environment{}, failure(409, "workspace_already_bound", "此项目已绑定另一个 Docker 应用，请创建独立项目")
		}
	}
	root := m.environmentRoot(id)
	for _, dir := range []string{"codex", "home", "leases", "traces"} {
		if e := os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return Environment{}, e
		}
	}
	v := Environment{ID: id, InstanceID: i.ID, WorkspaceID: w.ID, ContainerName: "rundesk-" + m.environmentOwner()[:8] + "-" + id, Spec: i.Execution.normalized(), CodexHome: filepath.Join(root, "codex"), State: "not_created", Revision: 1, Created: store.Now(), LastUsed: store.Now()}
	if e := m.saveEnvironmentLocked(&v); e != nil {
		return Environment{}, e
	}
	return v, nil
}
func (m *Manager) EnsureApplicationEnvironment(iid, wid string) (Environment, error) {
	m.executionMu.RLock()
	defer m.executionMu.RUnlock()
	i, e := m.Instance(iid)
	if e != nil {
		return Environment{}, e
	}
	if i.Execution.normalized().Mode != "docker" {
		return Environment{}, failure(400, "not_docker_application", "请先为应用配置 Docker 镜像")
	}
	w, e := m.Workspace(wid)
	if e != nil {
		return Environment{}, e
	}
	return m.ensureEnvironment(i, w)
}
func (m *Manager) Environments(iid string) []Environment {
	m.environmentMu.Lock()
	defer m.environmentMu.Unlock()
	out := []Environment{}
	for _, v := range m.environments {
		if iid == "" || v.InstanceID == iid {
			copy := *v
			copy.ActiveConnections = m.environmentRefs(v.ID).Load()
			out = append(out, copy)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created < out[b].Created })
	return out
}

type containerInspection struct {
	ID     string
	Image  string
	Config struct{ Labels map[string]string }
	State  struct {
		Status    string
		ExitCode  int
		OOMKilled bool
		Error     string
	}
}

func (m *Manager) inspectEnvironmentLocked(v *Environment) (bool, error) {
	out, e := m.dockerRun(10*time.Second, "container", "ls", "-a", "--filter", "name=^/"+v.ContainerName+"$", "--format", "{{.ID}}")
	if e != nil {
		return false, e
	}
	if strings.TrimSpace(string(out)) == "" {
		v.ContainerID = ""
		v.State = "not_created"
		return false, nil
	}
	out, e = m.dockerRun(10*time.Second, "container", "inspect", v.ContainerName)
	if e != nil {
		return false, e
	}
	var items []containerInspection
	if json.Unmarshal(out, &items) != nil || len(items) != 1 {
		return false, failure(502, "docker_invalid_response", "Docker inspect 响应格式无效")
	}
	item := items[0]
	if item.Config.Labels["io.rundesk.owner"] != m.environmentOwner() || item.Config.Labels["io.rundesk.environment"] != v.ID {
		return false, failure(409, "container_owner_mismatch", "同名容器不属于此运行环境，未接管或删除")
	}
	if v.ImageID != "" && item.Image != v.ImageID {
		return false, failure(409, "container_image_mismatch", "容器镜像与环境记录不一致")
	}
	v.ContainerID = item.ID
	v.State = item.State.Status
	v.ExitCode = item.State.ExitCode
	v.OOMKilled = item.State.OOMKilled
	v.Error = item.State.Error
	return true, nil
}
func (m *Manager) runtimeBinary() (string, error) {
	src, e := os.Executable()
	if e != nil {
		return "", e
	}
	path := filepath.Join(m.Data, "runtime", Version+"-"+runtime.GOARCH, "rundesk")
	if _, e = os.Stat(path); e == nil {
		return path, nil
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", e
	}
	in, e := os.Open(src)
	if e != nil {
		return "", e
	}
	defer in.Close()
	out, e := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0500)
	if e != nil {
		return "", e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return "", e
	}
	if ce != nil {
		return "", ce
	}
	if e = os.Rename(path+".tmp", path); e != nil {
		return "", e
	}
	return path, nil
}
func mountArg(src, dst string, ro bool) (string, error) {
	if strings.ContainsAny(src+dst, ",\n\r\x00") {
		return "", failure(400, "unsupported_mount_path", "Docker 挂载路径不能包含逗号、换行或 NUL")
	}
	arg := "type=bind,src=" + src + ",dst=" + dst
	if ro {
		arg += ",readonly"
	}
	return arg, nil
}
func (m *Manager) createContainerLocked(v *Environment, w Workspace) error {
	if runtime.GOOS != "linux" {
		return failure(400, "docker_platform_unsupported", "Docker 运行环境目前只支持 Linux 本机 Engine")
	}
	if e := m.validateDockerWorkspace(w); e != nil {
		return e
	}
	ref := v.ImageID
	if ref == "" {
		ref = v.Spec.Image
		if v.Spec.ImageID != "" {
			ref = v.Spec.ImageID
		}
	}
	item, e := m.inspectImage(ref)
	if e != nil {
		return e
	}
	if v.ImageID == "" {
		v.ImageID = item.ID
		if e = m.saveEnvironmentLocked(v); e != nil {
			return e
		}
	}
	// Rootless Docker maps container root to the daemon user, rather than the
	// numeric host uid; regular Docker uses the RunDesk service uid/gid directly.
	out, e := m.dockerRun(10*time.Second, "info", "--format", "{{json .SecurityOptions}}")
	if e != nil {
		return e
	}
	var security []string
	if json.Unmarshal(out, &security) != nil {
		return failure(502, "docker_invalid_response", "无法读取 Docker 用户映射模式")
	}
	user, e := containerUser(os.Getuid(), os.Getgid(), security)
	if e != nil {
		return e
	}
	helper, e := m.runtimeBinary()
	if e != nil {
		return e
	}
	root := m.environmentRoot(v.ID)
	args := []string{"container", "create", "--name", v.ContainerName, "--label", "io.rundesk.owner=" + m.environmentOwner(), "--label", "io.rundesk.environment=" + v.ID, "--init", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", user, "--cpus", strconv.Itoa(v.Spec.CPUs), "--memory", strconv.Itoa(v.Spec.MemoryMB) + "m", "--memory-swap", strconv.Itoa(v.Spec.MemoryMB) + "m", "--pids-limit", strconv.Itoa(v.Spec.PidsLimit), "--network", v.Spec.Network, "--restart", "no", "--log-driver", "local", "--log-opt", "max-size=10m", "--log-opt", "max-file=2", "--tmpfs", "/tmp:rw,nosuid,nodev,size=256m,mode=1777", "--tmpfs", "/run:rw,nosuid,nodev,size=16m,mode=755", "--workdir", w.Path, "--env", "CODEX_HOME=" + v.CodexHome, "--env", "CODEX_SQLITE_HOME=" + v.CodexHome, "--env", "HOME=" + filepath.Join(root, "home"), "--env", "XDG_CACHE_HOME=" + filepath.Join(root, "home", ".cache"), "--env", "XDG_CONFIG_HOME=" + filepath.Join(root, "home", ".config")}
	if v.Spec.Network == "bridge" {
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
	}
	for _, mt := range []struct {
		src, dst string
		ro       bool
	}{{w.Path, w.Path, false}, {v.CodexHome, v.CodexHome, false}, {filepath.Join(root, "home"), filepath.Join(root, "home"), false}, {filepath.Join(root, "leases"), filepath.Join(root, "leases"), true}, {filepath.Join(root, "traces"), filepath.Join(root, "traces"), true}, {helper, "/opt/rundesk/bin/rundesk", true}} {
		arg, e := mountArg(mt.src, mt.dst, mt.ro)
		if e != nil {
			return e
		}
		args = append(args, "--mount", arg)
	}
	args = append(args, "--entrypoint", "/opt/rundesk/bin/rundesk", v.ImageID, "__container_idle")
	out, e = m.dockerRun(30*time.Second, args...)
	if e != nil {
		return e
	}
	v.ContainerID = strings.TrimSpace(string(out))
	v.State = "created"
	v.DeployedAt = store.Now()
	return m.saveEnvironmentLocked(v)
}
func (m *Manager) startEnvironmentLocked(v *Environment, w Workspace) error {
	exists, e := m.inspectEnvironmentLocked(v)
	if e != nil {
		return e
	}
	if !exists {
		if e = m.createContainerLocked(v, w); e != nil {
			return e
		}
	}
	if v.State != "running" {
		out, e := m.dockerRun(10*time.Second, "container", "ls", "--filter", "label=io.rundesk.owner="+m.environmentOwner(), "--filter", "status=running", "--format", "{{.ID}}")
		if e != nil {
			return e
		}
		if len(strings.Fields(string(out))) >= 16 {
			return failure(429, "docker_environment_limit", "最多同时运行 16 个应用环境，请停止空闲环境")
		}
		if _, e = m.dockerRun(30*time.Second, "container", "start", v.ContainerName); e != nil {
			return e
		}
		v.State = "running"
	}
	if _, e = m.dockerRun(30*time.Second, "exec", v.ContainerName, "/opt/rundesk/bin/rundesk", "__container_seed", v.CodexHome); e != nil {
		return &apiError{Status: 503, Code: "docker_seed_failed", Message: "容器配置初始化失败，请检查目录权限和镜像技能目录", Cause: e}
	}
	if _, e = m.dockerRun(15*time.Second, "exec", v.ContainerName, "/opt/rundesk/bin/rundesk", "__container_probe"); e != nil {
		return &apiError{Status: 503, Code: "docker_runtime_invalid", Message: "容器缺少 Codex，或内核/安全策略不支持进程监管", Cause: e}
	}
	v.LastUsed = store.Now()
	v.Error = ""
	return m.saveEnvironmentLocked(v)
}

// A connection gets its own lease. Cleanup intentionally acquires no Manager
// locks because rpc.Close is also used while holding session/manager locks.
func (m *Manager) prepareCommand(w Workspace, i Instance) (*exec.Cmd, Instance, func(), error) {
	if i.Execution.normalized().Mode != "docker" {
		return m.command(w, i), i, func() {}, nil
	}
	m.environmentMu.Lock()
	defer m.environmentMu.Unlock()
	v, e := m.ensureEnvironmentLocked(i, w)
	if e != nil {
		return nil, i, nil, e
	}
	if e = m.startEnvironmentLocked(&v, w); e != nil {
		v.Error = e.Error()
		_ = m.saveEnvironmentLocked(&v)
		return nil, i, nil, e
	}
	i.CodexHome = v.CodexHome
	lease := filepath.Join(m.environmentRoot(v.ID), "leases", store.ID())
	if e = os.WriteFile(lease, []byte("rundesk execution\n"), 0600); e != nil {
		return nil, i, nil, e
	}
	refs := m.environmentRefs(v.ID)
	refs.Add(1)
	done := make(chan struct{})
	var once sync.Once
	cleanup := func() { once.Do(func() { close(done); _ = os.Remove(lease); refs.Add(-1) }) }
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-m.ctx.Done():
				cleanup()
				return
			case <-tick.C:
				now := time.Now()
				if e := os.Chtimes(lease, now, now); e != nil {
					cleanup()
					return
				}
			}
		}
	}()
	args := []string{"exec", "-i", "--workdir", w.Path, v.ContainerName, "/opt/rundesk/bin/rundesk", "__container_exec", lease, "codex", "app-server"}
	return m.docker.Command(args...), i, cleanup, nil
}
func (m *Manager) closeEnvironmentConnections(v Environment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Admission can race with the UI, but an accepted pending task has no process
	// until connect obtains executionMu. Never close an already running task.
	for key, h := range m.handles {
		s := m.sessions[key]
		if !(s != nil && s.EnvironmentID == v.ID) && key != "config-"+v.InstanceID+"-"+v.WorkspaceID {
			continue
		}
		if s != nil && active(s.Status) {
			return failure(409, "environment_busy", "项目仍有运行中的任务，未停止容器")
		}
		if !h.op.TryLock() {
			return failure(409, "environment_busy", "项目正在连接或配置")
		}
		if !h.mu.TryLock() {
			h.op.Unlock()
			return failure(409, "environment_busy", "项目连接正在使用")
		}
		if h.client != nil {
			h.client.Close()
			h.client = nil
			h.thread = ""
		}
		h.mu.Unlock()
		h.op.Unlock()
	}
	return nil
}
func (m *Manager) EnvironmentAction(id, action string, revision int, applicationRevision ...int) (Environment, error) {
	if action != "start" && action != "stop" && action != "remove" && action != "recreate" && action != "inspect" {
		return Environment{}, failure(400, "invalid_environment_action", "操作必须为 start、stop、remove、recreate 或 inspect")
	}
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	m.environmentMu.Lock()
	p := m.environments[id]
	var v Environment
	if p != nil {
		v = *p
	}
	m.environmentMu.Unlock()
	if p == nil {
		return v, failure(404, "environment_not_found", "运行环境不存在")
	}
	if action != "inspect" && revision != v.Revision {
		return v, failure(409, "revision_conflict", "环境已变化，请刷新后重试")
	}
	i, e := m.Instance(v.InstanceID)
	if e != nil {
		return v, e
	}
	if action == "start" || action == "recreate" {
		if i.Execution.normalized().Mode != "docker" {
			return v, failure(409, "not_docker_application", "应用当前使用本机运行方式")
		}
	}
	var targetImage string
	if action == "recreate" {
		if len(applicationRevision) > 0 && applicationRevision[0] != i.Revision {
			return v, failure(409, "revision_conflict", "应用目标版本已变化，请刷新后重试")
		}
		ref := i.Execution.Image
		if i.Execution.ImageID != "" {
			ref = i.Execution.ImageID
		}
		item, err := m.inspectImage(ref)
		if err != nil {
			return v, err
		}
		targetImage = item.ID
	}
	if action != "inspect" {
		if e = m.closeEnvironmentConnections(v); e != nil {
			return v, e
		}
	}
	m.environmentMu.Lock()
	defer m.environmentMu.Unlock()
	v = *m.environments[id]
	if action != "inspect" && m.environmentRefs(id).Load() != 0 {
		return v, failure(409, "environment_busy", "环境仍有活动连接")
	}
	w, e := m.Workspace(v.WorkspaceID)
	if e != nil {
		return v, e
	}
	if action == "inspect" {
		_, e = m.inspectEnvironmentLocked(&v)
	} else if action == "start" {
		e = m.startEnvironmentLocked(&v, w)
	} else {
		var exists bool
		exists, e = m.inspectEnvironmentLocked(&v)
		if e == nil && exists {
			_, e = m.dockerRun(20*time.Second, "container", "stop", "--time", "5", v.ContainerName)
			if e == nil {
				v.State = "exited"
			}
		}
		if e == nil && (action == "remove" || action == "recreate") {
			if exists {
				_, e = m.dockerRun(15*time.Second, "container", "rm", v.ContainerName)
			}
			if e == nil {
				v.ContainerID = ""
				v.State = "not_created"
			}
		}
		if e == nil && action == "recreate" {
			v.Spec = i.Execution.normalized()
			v.ImageID = targetImage
			e = m.startEnvironmentLocked(&v, w)
		}
	}
	if action != "inspect" {
		v.Revision++
	}
	if e != nil {
		v.Error = e.Error()
	} else if action != "inspect" {
		v.Error = ""
	}
	saveErr := m.saveEnvironmentLocked(&v)
	v.ActiveConnections = m.environmentRefs(v.ID).Load()
	if e != nil {
		return v, e
	}
	return v, saveErr
}
func (m *Manager) reapEnvironments() {
	for _, v := range m.Environments("") {
		if v.State != "running" && v.State != "unknown" {
			continue
		}
		cutoff := time.Now().Add(-time.Duration(v.Spec.IdleMinutes) * time.Minute)
		last, e := time.Parse(time.RFC3339Nano, v.LastUsed)
		if e != nil || last.After(cutoff) {
			continue
		}
		idle := true
		m.mu.Lock()
		for key, h := range m.handles {
			s := m.sessions[key]
			if !(s != nil && s.EnvironmentID == v.ID) && key != "config-"+v.InstanceID+"-"+v.WorkspaceID {
				continue
			}
			if s != nil {
				updated, _ := time.Parse(time.RFC3339Nano, s.Updated)
				if active(s.Status) || updated.After(cutoff) {
					idle = false
					break
				}
			}
			if !h.mu.TryLock() {
				idle = false
				break
			}
			recent := h.last.After(cutoff)
			h.mu.Unlock()
			if recent {
				idle = false
				break
			}
		}
		m.mu.Unlock()
		if idle {
			_, _ = m.EnvironmentAction(v.ID, "stop", v.Revision)
		}
	}
}

func containerUser(uid, gid int, security []string) (string, error) {
	for _, s := range security {
		if s == "name=rootless" {
			return "0:0", nil
		}
	}
	for _, s := range security {
		if s == "name=userns" {
			return "", failure(400, "docker_userns_unsupported", "当前不支持 daemon userns-remap 的目录映射，请使用普通或 Rootless Docker")
		}
	}
	return strconv.Itoa(uid) + ":" + strconv.Itoa(gid), nil
}
