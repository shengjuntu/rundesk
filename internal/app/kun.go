package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	kc "github.com/shengjuntu/rundesk/internal/adapters/kun"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
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
	next := *v
	next.AgentRuntime = config
	next.Revision++
	if e := m.Store.Put("instance", id, next); e != nil {
		return Instance{}, e
	}
	*v = next
	return next, nil
}
func (m *Manager) kunStartRequest(s Session, w Workspace, in Input) (p.Start, error) {
	i, err := m.Instance(s.InstanceID)
	if err != nil {
		return p.Start{}, err
	}
	cfg := i.AgentRuntime.Normalized()
	if s.Model != "" {
		cfg.Model = s.Model
	}
	if i.Permissions.normalized().Sandbox == "read-only" {
		cfg.AllowWrite = false
	}
	key := ""
	if cfg.APIKeyEnv != "" {
		key = os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return p.Start{}, fmt.Errorf("Kun 凭证环境变量未设置：%s", cfg.APIKeyEnv)
		}
	}
	if s.TraceOrigin != nil {
		return m.kunDiagnosticRequest(s, w, in, cfg, key)
	}
	mcpServers, err := m.kunRuntimeMCP(i.ID)
	if err != nil {
		return p.Start{}, err
	}
	skills, err := m.kunInputSkills(w, i, in.Skills)
	if err != nil {
		return p.Start{}, err
	}
	input := in.Text
	if w.Notes != "" {
		input += "\n<project-notes>\n" + w.Notes + "\n</project-notes>"
	}
	input += "\nSave deliverables under outputs/" + s.ID + "/."
	for _, f := range in.Files {
		input += "\nUploaded file (use read_file): " + f
	}
	request := p.Start{SessionID: s.ID, RunID: s.RunID, Input: input, Workspace: w.Path, Config: cfg, APIKey: key, Skills: skills, MCP: mcpServers, ApprovalPolicy: i.Permissions.normalized().ApprovalPolicy}
	request.ContextRevision = fmt.Sprintf("%s:%d/%s:%d", i.ID, i.Revision, w.ID, w.Revision)
	if in.KunResume != nil {
		request.Resume = &in.KunResume.Selection
	}
	return request, nil
}

// Caller holds handle.op and owns a process reservation when launching.
func (m *Manager) ensureKunWorker(s Session, h *handle) (*kc.Client, error) {
	var err error
	h.mu.Lock()
	client := h.kun
	h.mu.Unlock()
	if client != nil {
		select {
		case <-client.Done():
			client = nil
		default:
		}
	}
	if client == nil {
		client, err = kc.Launch(m.ctx, m.kunExecutable(), filepath.Join(m.Data, "kun", "sessions", s.ID))
		if err != nil {
			return nil, err
		}
		m.loaded.Add(1)
		go func(c *kc.Client) { <-c.Done(); m.loaded.Add(-1) }(client)
		h.mu.Lock()
		h.kun = client
		h.instanceID = s.InstanceID
		h.mu.Unlock()
	}
	h.mu.Lock()
	h.last = time.Now()
	h.mu.Unlock()
	return client, nil
}
func (m *Manager) runKun(s Session, w Workspace, h *handle, in Input) {
	start, err := m.kunStartRequest(s, w, in)
	if err != nil {
		m.finish(s.ID, "failed", err.Error())
		return
	}
	cfg := start.Config
	client, err := m.ensureKunWorker(s, h)
	if err != nil {
		m.finish(s.ID, "failed", "启动 Kun："+err.Error())
		return
	}
	_ = m.Store.Put("runtime", s.ID, RuntimeStatus{ID: s.ID, InstanceID: s.InstanceID, WorkspaceID: s.WorkspaceID, ConnectionID: fmt.Sprint(client.PID()), ExecutionMode: "local", Live: true, ConnectedAt: store.Now(), Notices: []RuntimeNotice{}, UserAgent: "Kun/" + p.EngineVersion, Effective: map[string]any{"runtimeKind": "kun", "model": cfg.Model, "endpoint": cfg.Endpoint, "allowWrite": cfg.AllowWrite}})
	h.mu.Lock()
	canceled := h.canceled
	h.mu.Unlock()
	if canceled || m.ctx.Err() != nil {
		client.Close()
		m.finish(s.ID, "interrupted", "")
		return
	}
	var result p.State
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	err = client.Call(ctx, "start", start, &result)
	cancel()
	if err != nil {
		client.Close()
		m.finish(s.ID, "failed", "Kun 提交未确认，请检查执行记录："+err.Error())
		return
	}
	_ = m.update(s.ID, func(v *Session) {
		if v.RunID == s.RunID {
			v.TurnID = s.RunID
			if v.Status == "starting" {
				v.Status = "running"
			}
		}
	})
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := m.watchKun(s, h, client); err != nil {
			client.Close()
			current, e := m.Session(s.ID)
			if e == nil && current.RunID == s.RunID && active(current.Status) {
				status := "failed"
				if m.ctx.Err() != nil {
					status = "interrupted"
				}
				m.finish(s.ID, status, err.Error())
			}
		}
	}()
	h.mu.Lock()
	canceled = h.canceled
	h.mu.Unlock()
	if canceled {
		go m.stopKun(s, h)
	}
}
func (m *Manager) watchKun(session Session, h *handle, client *kc.Client) error {
	cursor, err := m.Store.KunCursor(session.ID)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var events []p.Event
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		err = client.Call(ctx, "events", map[string]any{"after": cursor}, &events)
		cancel()
		if err != nil {
			return err
		}
		finished := false
		for _, ev := range events {
			if ev.SessionID != session.ID {
				return fmt.Errorf("Kun session mismatch")
			}
			var data struct {
				Status  string    `json:"status"`
				Error   string    `json:"error"`
				Receipt p.Receipt `json:"receipt"`
				Command p.Control `json:"command"`
			}
			_ = json.Unmarshal(ev.Data, &data)
			m.mu.Lock()
			current := m.sessions[session.ID]
			if current == nil {
				m.mu.Unlock()
				return fmt.Errorf("session removed")
			}
			next := *current
			records := []store.Record{}
			if ev.RunID == next.RunID {
				switch ev.Type {
				case "kun/run.started":
					if next.Status != "stopping" {
						next.Status = "running"
					}
					next.TurnID = ev.RunID
				case "kun/run.paused", "kun/approval.requested":
					if next.Status != "stopping" {
						next.Status = "waiting"
					}
				case "kun/control.applied":
					if data.Command.Operation == "resume" || data.Command.Operation == "step" || data.Command.Operation == "approve" || data.Command.Operation == "reject" {
						next.Status = "running"
					}
				case "kun/run.finished":
					next.Status = data.Status
					next.Error = data.Error
					finished = true
				}
				next.Updated = store.Now()
				records = append(records, store.Record{Kind: "session", ID: next.ID, Value: next})
			}
			ok, e := m.Store.ImportKun(session.ID, ev.Sequence, ev.Time, ev.Type, redact(ev), records...)
			if e == nil && ok && ev.RunID == next.RunID {
				*current = next
			}
			m.mu.Unlock()
			if e != nil {
				return e
			}
			cursor = ev.Sequence
		}
		if finished {
			return m.event(session.ID, "internal", "run/state", map[string]string{"runId": session.RunID, "status": func() string { s, _ := m.Session(session.ID); return s.Status }()})
		}
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		case <-client.Done():
			return fmt.Errorf("Kun worker exited; execution checkpoint retained")
		case <-ticker.C:
		}
	}
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
	return c, nil
}
func (m *Manager) stopKun(s Session, h *handle) {
	h.mu.Lock()
	c := h.kun
	h.mu.Unlock()
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, "control", p.Control{RequestID: store.ID(), RunID: s.RunID, ExpectedRevision: -1, Operation: "cancel"}, nil); err != nil {
		c.Close()
		current, e := m.Session(s.ID)
		if e == nil && active(current.Status) {
			m.finish(s.ID, "interrupted", err.Error())
		}
	}
}
func (m *Manager) steerKun(s Session, in SteerInput) (SteerReceipt, error) {
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
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	var result p.Receipt
	e = c.Call(ctx, "control", p.Control{RequestID: in.RequestID, RunID: s.RunID, ExpectedRevision: -1, Operation: "steer", Text: in.Text}, &result)
	receipt.Status = result.Status

	return receipt, e
}
func (s *Server) kunRoutes(mux *http.ServeMux) {
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
		c, e := s.Manager.kunClient(r.PathValue("sid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		if in.ExpectedRevision < 0 {
			respond(w, nil, failure(400, "revision_required", "调试控制需要当前状态版本"))
			return
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
