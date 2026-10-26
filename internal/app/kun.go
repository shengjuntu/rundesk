package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	kc "github.com/your-org/kun/sdk/client"
	p "github.com/your-org/kun/sdk/protocol"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func runtimeKind(kind string) string {
	if kind == "" {
		return "codex"
	}
	return kind
}
func (m *Manager) kunExecutable() string {
	if m.Kun != "" {
		return m.Kun
	}
	exe, _ := os.Executable()
	name := "kun"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}
func (m *Manager) SetAgentRuntime(id string, revision int, config p.Config) (Instance, error) {
	config = config.Normalized()
	if m.Demo && config.Kind == "kun" {
		return Instance{}, failure(400, "demo_no_model_calls", "Demo 模式不调用真实模型，请正常启动 RunDesk 后配置 Kun")
	}
	if e := config.Validate(); e != nil {
		return Instance{}, failure(400, "invalid_kun_config", e.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.instances[id]
	if v == nil {
		return Instance{}, failure(404, "instance_not_found", "实例不存在")
	}
	if v.Revision != revision {
		return Instance{}, failure(409, "revision_conflict", "配置已变化，请刷新")
	}
	for _, s := range m.sessions {
		if s.InstanceID == id && active(s.Status) {
			return Instance{}, failure(409, "instance_busy", "请先结束该实例的运行")
		}
	}
	if config.Kind == "kun" && v.Execution.normalized().Mode != "local" {
		return Instance{}, failure(400, "kun_local_only", "首版 Kun 仅支持本机 worker")
	}
	if config.AllowWrite && v.Permissions.normalized().Sandbox == "read-only" {
		return Instance{}, failure(400, "kun_read_only", "实例为只读，不能启用写文件")
	}
	if config.AllowBash && v.Permissions.normalized().Sandbox != "danger-full-access" {
		return Instance{}, failure(400, "kun_bash_host_permission", "Bash 使用宿主权限；请先将实例权限设为完整访问，再明确启用 Bash")
	}
	next := *v
	next.AgentRuntime = config
	next.Revision++
	if e := m.Store.Put("instance", id, next); e != nil {
		return Instance{}, e
	}
	*v = next
	return next, nil
}
func (m *Manager) kunClient(sid string) (*kc.Client, error) {
	s, e := m.Session(sid)
	if e != nil {
		return nil, e
	}
	if s.RuntimeKind != "kun" {
		return nil, failure(400, "not_kun", "该会话不使用 Kun")
	}
	m.mu.Lock()
	h := m.handles[sid]
	m.mu.Unlock()
	if h == nil {
		return nil, failure(409, "kun_offline", "Kun worker 尚未启动或已回收；历史记录仍可在轨迹中查看")
	}
	h.mu.Lock()
	c := h.kun
	h.mu.Unlock()
	if c == nil {
		return nil, failure(409, "kun_offline", "Kun worker 尚未启动或已回收；历史记录仍可在轨迹中查看")
	}
	select {
	case <-c.Done():
		return nil, failure(409, "kun_offline", "Kun worker 已退出；历史记录仍可在轨迹中查看")
	default:
	}
	return c, nil
}

func requireKunCapabilities(c *kc.Client, names ...string) error {
	info := c.Info()
	missing := []string{}
	for _, name := range names {
		if !kunHasCapability(info, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if err := c.RequireCapabilities(missing...); err != nil {
		return failure(409, "kun_capability_unsupported", err.Error())
	}
	return nil
}

func kunHasCapability(info p.Hello, name string) bool {
	if info.Capabilities[name] {
		return true
	}
	// Kun 0.16 exposed the stable snapshot method as "inspect". Only this
	// explicitly known legacy release receives that capability alias.
	return name == "snapshot" && info.EngineVersion == "0.16.0" && info.Capabilities["inspect"]
}

func kunControlCapability(operation string) string {
	switch operation {
	case "pause", "resume":
		return "pause"
	case "step", "steer":
		return operation
	case "set_breakpoints":
		return "conditionalBreakpoints"
	case "set_harness":
		return "runtimeHarness"
	}
	return ""
}
func (m *Manager) steerKun(s Session, in SteerInput) (SteerReceipt, error) {
	if s.KunCase != nil {
		return SteerReceipt{}, failure(409, "case_input_fixed", "案例输入已固定；修改输入请创建新的任务集与预览")
	}
	receipt := SteerReceipt{RequestID: in.RequestID, TurnID: in.ExpectedTurnID}
	if s.TurnID != in.ExpectedTurnID || !active(s.Status) {
		return receipt, failure(409, "turn_conflict", "目标轮次已结束或变化")
	}
	if len(in.Files) > 0 || len(in.Skills) > 0 {
		return receipt, failure(400, "kun_steer_text_only", "首版 Kun 补充指令仅支持文本")
	}
	c, e := m.kunClient(s.ID)
	if e != nil {
		return receipt, e
	}
	if err := requireKunCapabilities(c, "steer"); err != nil {
		return receipt, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	var result p.Receipt
	e = c.Call(ctx, "control", p.Control{RequestID: in.RequestID, RunID: s.RunID, ExpectedRevision: -1, Operation: "steer", Text: in.Text}, &result)
	receipt.Status = result.Status

	return receipt, e
}
func (s *Server) kunRoutes(mux *http.ServeMux) {
	s.diagnosticReviewRoutes(mux)
	s.kunCheckpointRoutes(mux)
	s.kunDebugRoutes(mux)
	mux.HandleFunc("PUT /api/instances/{iid}/agent-runtime", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int      `json:"revision"`
			Config   p.Config `json:"config"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.SetAgentRuntime(r.PathValue("iid"), in.Revision, in.Config)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/sessions/{sid}/kun/state", func(w http.ResponseWriter, r *http.Request) {
		c, e := s.Manager.kunClient(r.PathValue("sid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		var state p.State
		e = c.Call(r.Context(), "state", nil, &state)
		respond(w, redact(state), e)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/kun/control", func(w http.ResponseWriter, r *http.Request) {
		var in p.Control
		if !decode(w, r, &in) {
			return
		}
		if session, err := s.Manager.Session(r.PathValue("sid")); err == nil && session.KunCase != nil && (in.Operation == "steer" || in.Operation == "set_harness") {
			respond(w, nil, failure(409, "case_input_fixed", "案例输入和 Harness 已固定；请创建新预览"))
			return
		}
		c, e := s.Manager.kunClient(r.PathValue("sid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		if in.ExpectedRevision < 0 {
			respond(w, nil, failure(400, "revision_required", "调试控制需要当前状态版本"))
			return
		}
		if capability := kunControlCapability(in.Operation); capability != "" {
			if err := requireKunCapabilities(c, capability); err != nil {
				respond(w, nil, err)
				return
			}
		}
		var result p.Receipt
		e = c.Call(r.Context(), "control", in, &result)
		var remote *kc.RemoteError
		if errors.As(e, &remote) {
			e = failure(409, "kun_control_rejected", remote.Error())
		}
		respond(w, result, e)
	})
	mux.HandleFunc("GET /api/sessions/{sid}/kun/snapshots/{sequence}", func(w http.ResponseWriter, r *http.Request) {
		var seq int64
		if _, e := fmt.Sscan(r.PathValue("sequence"), &seq); e != nil || seq < 1 {
			respond(w, nil, failure(400, "invalid_sequence", "无效快照序号"))
			return
		}
		c, e := s.Manager.kunClient(r.PathValue("sid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		var result p.Snapshot
		if err := requireKunCapabilities(c, "snapshot"); err != nil {
			respond(w, nil, err)
			return
		}
		e = c.Call(r.Context(), "snapshot", map[string]any{"sequence": seq}, &result)
		respond(w, redact(result), e)
	})
}

type kunSkill struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Scope       string `json:"scope"`
}

func (m *Manager) kunSkills(w Workspace, i Instance) ([]kunSkill, error) {
	out := []kunSkill{}
	for _, base := range []struct{ root, dir, scope string }{{w.Path, ".agents/skills", "project"}, {i.CodexHome, "skills", "instance"}} {
		root, e := os.OpenRoot(base.root)
		if e != nil {
			return nil, e
		}
		dir, e := root.Open(base.dir)
		if os.IsNotExist(e) {
			root.Close()
			continue
		}
		if e != nil {
			root.Close()
			return nil, e
		}
		entries, e := dir.ReadDir(1001)
		dir.Close()
		if e != nil && e != io.EOF {
			root.Close()
			return nil, e
		}
		for n, entry := range entries {
			if n >= 1000 {
				break
			}
			if !entry.IsDir() || !slug.MatchString(entry.Name()) {
				continue
			}
			rel := filepath.Join(base.dir, entry.Name(), "SKILL.md")
			f, e := root.Open(rel)
			if e != nil {
				continue
			}
			b, e := io.ReadAll(io.LimitReader(f, 256*1024+1))
			f.Close()
			if e != nil || len(b) > 256*1024 {
				continue
			}
			enabled := true
			var disabled bool
			path := filepath.Join(base.root, rel)
			_ = m.Store.Get("kun-skill-disabled", i.ID+":"+path, &disabled)
			enabled = !disabled
			description := ""
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "description:") {
					description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"'")
					break
				}
			}
			out = append(out, kunSkill{Name: entry.Name(), Path: path, Description: description, Enabled: enabled, Scope: base.scope})
		}
		root.Close()
	}
	return out, nil
}
func (m *Manager) kunInputSkills(w Workspace, i Instance, selected []Skill) ([]p.Skill, error) {
	out := []p.Skill{}
	if len(selected) == 0 {
		return out, nil
	}
	available, e := m.kunSkills(w, i)
	if e != nil {
		return nil, e
	}
	total := 0
	for _, want := range selected {
		found := false
		for _, skill := range available {
			if skill.Name != want.Name || skill.Path != want.Path || !skill.Enabled {
				continue
			}
			rootPath := w.Path
			if skill.Scope == "instance" {
				rootPath = i.CodexHome
			}
			root, e := os.OpenRoot(rootPath)
			if e != nil {
				return nil, e
			}
			rel, e := filepath.Rel(rootPath, skill.Path)
			if e != nil {
				root.Close()
				return nil, e
			}
			f, e := root.Open(rel)
			if e != nil {
				root.Close()
				return nil, e
			}
			b, e := io.ReadAll(io.LimitReader(f, 256*1024+1))
			f.Close()
			root.Close()
			if e != nil {
				return nil, e
			}
			total += len(b)
			if total > 512*1024 {
				return nil, fmt.Errorf("selected Skills exceed 512 KiB")
			}
			out = append(out, p.Skill{Name: skill.Name, Path: skill.Path, Content: string(b), Hash: fmt.Sprintf("%x", sha256.Sum256(b))})
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("Skill unavailable: %s", want.Name)
		}
	}
	return out, nil
}
