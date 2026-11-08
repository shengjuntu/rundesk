package app

import (
	"context"
	"fmt"
	kc "github.com/your-org/kun/sdk/client"
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
	"os"
	"path/filepath"
	"time"
)

func (b kunBackend) startRequest(s Session, w Workspace, in Input) (p.Start, error) {
	if s.KunCase != nil {
		return b.preparation.CaseStartRequest(s, w, in)
	}
	i, err := b.instances.Instance(s.InstanceID)
	if err != nil {
		return p.Start{}, err
	}
	permissions, err := b.policy.Permissions(s)
	if err != nil {
		return p.Start{}, err
	}
	cfg := i.AgentRuntime.Normalized()
	if s.Model != "" {
		cfg.Model = s.Model
	}
	if permissions.Sandbox == "read-only" {
		cfg.AllowWrite = false
	}
	if cfg.AllowBash && permissions.Sandbox != "danger-full-access" {
		return p.Start{}, failure(409, "kun_bash_permission_changed", "Bash 需要实例完整访问权限，请重新核对配置")
	}
	key := ""
	if cfg.APIKeyEnv != "" {
		key = os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return p.Start{}, fmt.Errorf("Kun 凭证环境变量未设置：%s", cfg.APIKeyEnv)
		}
	}
	if s.TraceOrigin != nil {
		return b.preparation.DiagnosticRequest(s, w, in, cfg, key)
	}
	if s.KunFork != nil {
		return b.preparation.ForkStartRequest(s, w, in, cfg, key)
	}
	workspace, err := b.resources.Resolve(s, resource.RoleWorkspace)
	if err != nil {
		return p.Start{}, err
	}
	w.Path = workspace.Path
	outputs, err := b.resources.Resolve(s, resource.RoleOutputs)
	if err != nil {
		return p.Start{}, err
	}
	mcpServers, err := b.preparation.RuntimeMCP(i.ID)
	if err != nil {
		return p.Start{}, err
	}
	skills, err := b.preparation.InputSkills(w, i, in.Skills)
	if err != nil {
		return p.Start{}, err
	}
	input := in.Text
	if w.Notes != "" {
		input += "\n<project-notes>\n" + w.Notes + "\n</project-notes>"
	}
	input += "\nSave deliverables under " + filepath.ToSlash(outputs.RelativePath) + "/."
	for _, f := range in.Files {
		input += "\nUploaded file (use read_file): " + f
	}
	request := p.Start{SessionID: s.ID, RunID: s.RunID, Input: input, Workspace: w.Path, Config: cfg, APIKey: key, Skills: skills, MCP: mcpServers, ApprovalPolicy: permissions.ApprovalPolicy}
	request.ContextRevision = fmt.Sprintf("%s:%d/%s:%d", i.ID, i.Revision, w.ID, w.Revision)
	if in.KunResume != nil {
		request.Resume = &in.KunResume.Selection
	}
	return b.preparation.AttachSkill(s, w, in, request)
}

// Caller holds handle.op and owns a process reservation when launching.
func (b kunBackend) ensureWorker(s Session, h *handle) (*kc.Client, error) {
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
		state, err := b.resources.Resolve(s, resource.RoleRuntimeState)
		if err != nil {
			return nil, err
		}
		client, err = kc.Launch(b.state.Context(), b.process.Executable(), state.Path)
		if err != nil {
			return nil, err
		}
		b.process.Count(1)
		go func(c *kc.Client) { <-c.Done(); b.process.Count(-1) }(client)
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
func (b kunBackend) Run(run RuntimeRun) {
	s, w, h, in := run.Session, run.Workspace, run.Handle, run.Input
	start, err := b.startRequest(s, w, in)
	if err != nil {
		b.state.Finish(s.ID, "failed", err.Error())
		return
	}
	cfg := start.Config
	client, err := b.ensureWorker(s, h)
	if err != nil {
		b.state.Finish(s.ID, "failed", "启动 Kun："+err.Error())
		return
	}
	info := client.Info()
	_ = b.journal.RecordRuntime(s.ID, RuntimeStatus{ID: s.ID, InstanceID: s.InstanceID, WorkspaceID: s.WorkspaceID, ConnectionID: fmt.Sprint(client.PID()), ExecutionMode: "local", Live: true, ConnectedAt: store.Now(), Notices: []RuntimeNotice{}, UserAgent: "Kun/" + info.EngineVersion, Effective: map[string]any{"runtimeKind": "kun", "engineVersion": info.EngineVersion, "protocolVersion": info.ProtocolVersion, "capabilities": info.Capabilities, "model": cfg.Model, "endpoint": cfg.Endpoint, "allowWrite": cfg.AllowWrite, "allowBash": cfg.AllowBash, "bashApproval": cfg.BashApproval}})
	if start.Diagnostic != nil {
		if err := client.RequireCapabilities("diagnosticProvider"); err != nil {
			b.state.Finish(s.ID, "failed", "Kun 诊断需要支持 diagnosticProvider 的独立引擎："+err.Error())
			return
		}
	}
	if start.Resume != nil {
		if err := requireKunCapabilities(client, "resumeCheckpoint"); err != nil {
			b.state.Finish(s.ID, "failed", err.Error())
			return
		}
	}
	if start.Fork != nil {
		caps := kunForkCapabilities(start.Fork.Bundle.Mode)
		if start.Fork.Bundle.Hypothesis != nil {
			caps = append(caps, "forkHypothesis")
		}
		if err := requireKunCapabilities(client, caps...); err != nil {
			b.state.Finish(s.ID, "failed", err.Error())
			return
		}
	}
	h.mu.Lock()
	canceled := h.canceled
	h.mu.Unlock()
	if canceled || b.state.Context().Err() != nil {
		client.Close()
		b.state.Finish(s.ID, "interrupted", "")
		return
	}
	var result p.State
	ctx, cancel := context.WithTimeout(b.state.Context(), 15*time.Second)
	err = client.Call(ctx, "start", start, &result)
	cancel()
	if err != nil {
		client.Close()
		b.state.Finish(s.ID, "failed", "Kun 提交未确认，请检查执行记录："+err.Error())
		return
	}
	_ = b.state.Update(s.ID, func(v *Session) {
		if v.RunID == s.RunID {
			v.TurnID = s.RunID
			if v.Status == "starting" {
				v.Status = "running"
			}
		}
	})
	b.state.Go(func() {
		if err := b.watch(s, client); err != nil {
			client.Close()
			current, e := b.state.Session(s.ID)
			if e == nil && current.RunID == s.RunID && active(current.Status) {
				status := "failed"
				if b.state.Context().Err() != nil {
					status = "interrupted"
				}
				b.state.Finish(s.ID, status, err.Error())
			}
		}
	})
	h.mu.Lock()
	canceled = h.canceled
	h.mu.Unlock()
	if canceled {
		b.Stop(RuntimeStop{Session: s, Handle: h})
	}
}
func (b kunBackend) watch(session Session, client *kc.Client) error {
	cursor, err := b.journal.Cursor(session.ID)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var events []p.Event
		ctx, cancel := context.WithTimeout(b.state.Context(), 10*time.Second)
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
			terminal, e := b.journal.Import(session.ID, ev)
			if e != nil {
				return e
			}
			finished = finished || terminal
			cursor = ev.Sequence
		}
		if finished {
			return b.state.Event(session.ID, "internal", "run/state", map[string]string{"runId": session.RunID, "status": func() string { s, _ := b.state.Session(session.ID); return s.Status }()})
		}
		select {
		case <-b.state.Context().Done():
			return b.state.Context().Err()
		case <-client.Done():
			return fmt.Errorf("Kun worker exited; execution checkpoint retained")
		case <-ticker.C:
		}
	}
}

// Capture the native connection while the host still holds admission. A late
// cancellation must never attach itself to a replacement worker.
func (b kunBackend) Stop(stop RuntimeStop) {
	h := stop.Handle
	h.mu.Lock()
	client := h.kun
	h.mu.Unlock()
	if client == nil {
		return
	}
	b.state.Go(func() { b.stop(stop, client) })
}

func (b kunBackend) stop(stop RuntimeStop, client *kc.Client) {
	s, h := stop.Session, stop.Handle
	current, err := b.state.Session(s.ID)
	if err != nil || current.RunID != s.RunID || !active(current.Status) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, "control", p.Control{RequestID: store.ID(), RunID: s.RunID, ExpectedRevision: -1, Operation: "cancel"}, nil); err != nil {
		// Protect replacement admission while closing a failed connection. The
		// worker may have finished the old run before replying to cancellation.
		h.admission.Lock()
		defer h.admission.Unlock()
		current, e := b.state.Session(s.ID)
		if e != nil || current.RunID != s.RunID || !active(current.Status) {
			return
		}
		h.mu.Lock()
		same := h.kun == client
		h.mu.Unlock()
		if same {
			client.Close()
			b.state.Finish(s.ID, "interrupted", err.Error())
		}
	}
}
