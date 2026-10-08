package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	kc "github.com/shengjuntu/rundesk/internal/adapters/kun"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/redaction"
	"github.com/shengjuntu/rundesk/internal/store"
)

type KunForkInput struct {
	Mode        string          `json:"mode,omitempty"`
	SessionID   string          `json:"sessionId"`
	Selection   p.ForkSelection `json:"selection"`
	Title       string          `json:"title"`
	Instruction string          `json:"instruction"`
}
type KunForkPreview struct {
	Live            *KunLivePreview `json:"live,omitempty"`
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	CreatedAt       string          `json:"createdAt"`
	Hash            string          `json:"hash"`
	Origin          p.ForkOrigin    `json:"origin"`
	TargetSessionID string          `json:"targetSessionId"`
	Instruction     string          `json:"instruction"`
	Phase           string          `json:"phase"`
	Step            int             `json:"step"`
	Pending         int             `json:"pending"`
	Budget          p.BudgetUsage   `json:"budget"`
	Limits          p.BudgetLimits  `json:"limits"`
	MaxSteps        int             `json:"maxSteps"`
	RecordCount     int             `json:"recordCount"`
}
type KunLiveToolPreview struct {
	CallID           string `json:"callId"`
	Name             string `json:"name"`
	ArgumentsPreview string `json:"argumentsPreview"`
	Truncated        bool   `json:"truncated"`
}
type KunLivePreview struct {
	Workspace      string               `json:"workspace"`
	AllowWrite     bool                 `json:"allowWrite"`
	ApprovalPolicy string               `json:"approvalPolicy"`
	MCPTools       int                  `json:"mcpTools"`
	PendingTools   []KunLiveToolPreview `json:"pendingTools"`
}
type kunForkDraft struct {
	Preview    KunForkPreview `json:"preview"`
	Target     Session        `json:"target"`
	ConfigHash string         `json:"configHash"`
	Bundle     p.ForkBundle   `json:"bundle"`
}
type kunForkInput struct {
	PreviewID   string
	ConfirmLive bool
}

func forkDigest(v any) string { return fmt.Sprintf("%x", sha256.Sum256(p.JSON(v))) }
func forkDraftHash(d kunForkDraft) string {
	d.Preview.Hash = ""
	return forkDigest(struct {
		Preview                KunForkPreview
		Target                 Session
		ConfigHash, BundleHash string
	}{d.Preview, d.Target, d.ConfigHash, d.Bundle.ContentHash})
}
func (m *Manager) forkHostConfigHash(s Session, w Workspace) (string, error) {
	i, err := m.Instance(s.InstanceID)
	if err != nil {
		return "", err
	}
	mcp, err := m.kunMCPConfig(i.ID)
	if err != nil {
		return "", err
	}
	return forkDigest(struct {
		Instance  Instance
		Workspace Workspace
		MCP       any
		Model     string
	}{i, w, mcp, s.Model}), nil
}

// Caller holds source handle.op, so deletion/new-run admission cannot race the
// export. Reopening a worker only reconciles its journal; no model/MCP startup.
func (m *Manager) forkSource(sid string, fn func(Session, Workspace, *kc.Client) error) error {
	s, err := m.Session(sid)
	if err != nil {
		return err
	}
	if s.RuntimeKind != "kun" || s.TraceOrigin != nil || s.KunFork != nil {
		return failure(409, "fork_source_unsupported", "仅普通 Kun 会话可作为分叉来源")
	}
	if err = m.checkSessionExecution(s); err != nil {
		return err
	}
	h, err := m.getHandle(sid)
	if err != nil {
		return err
	}
	h.op.Lock()
	defer h.op.Unlock()
	h.admission.Lock()
	defer h.admission.Unlock()
	s, err = m.Session(sid)
	if err != nil {
		return err
	}
	if active(s.Status) || s.RunID == "" || m.queueReserved(sid) {
		return failure(409, "fork_source_busy", "请先结束来源运行并取消其排队任务")
	}
	w, err := m.Workspace(s.WorkspaceID)
	if err != nil {
		return err
	}
	if err = m.reserveProcess(sid, h); err != nil {
		return err
	}
	defer m.unreserveProcess(sid)
	c, err := m.ensureKunWorker(s, h)
	if err != nil {
		return err
	}
	return fn(s, w, c)
}
func (m *Manager) KunForkPoints(sid string, offset, limit int) (p.ForkPoints, error) {
	var out p.ForkPoints
	err := m.forkSource(sid, func(s Session, w Workspace, c *kc.Client) error {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		if err := c.Call(ctx, "fork.points", map[string]int{"offset": offset, "limit": limit}, &out); err != nil {
			return failure(409, "fork_points_unavailable", err.Error())
		}
		if out.Selection.SourceRunID != s.RunID {
			return failure(409, "fork_source_changed", "宿主与 worker 轮次不一致")
		}
		return nil
	})
	return out, err
}
func (m *Manager) CreateKunFork(in KunForkInput) (KunForkPreview, error) {
	var out KunForkPreview
	if in.Mode == "" {
		in.Mode = "hybrid"
	}
	if in.Mode != "hybrid" && in.Mode != "live" {
		return out, failure(400, "fork_mode_invalid", "分叉模式必须为 hybrid 或 live")
	}
	if err := experimentTitle(in.Title); err != nil {
		return out, err
	}
	if utf8.RuneCountInString(in.Instruction) > 16000 {
		return out, failure(400, "fork_instruction_too_long", "补充指令最多 16000 个字符")
	}
	err := m.forkSource(in.SessionID, func(s Session, w Workspace, c *kc.Client) error {
		initialConfig, err := m.forkHostConfigHash(s, w)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		defer cancel()
		var b p.ForkBundle
		if err := c.Call(ctx, "fork.export", p.ForkExport{Selection: in.Selection, Mode: in.Mode}, &b); err != nil {
			return failure(409, "fork_export_rejected", err.Error())
		}
		if b.Mode != in.Mode || b.State.SessionID != s.ID || b.State.RunID != s.RunID || b.Selection != in.Selection || b.Schema != p.ForkSchema || b.ContentHash != p.ForkHash(b) || len(p.JSON(b)) > p.MaxForkBytes {
			return failure(409, "fork_integrity", "分叉记录身份或指纹不一致")
		}
		// Check the original configuration locally, without connecting MCP. Do not
		// persist resolved tool credentials in the recording or public preview.
		i, err := m.Instance(s.InstanceID)
		if err != nil {
			return err
		}
		cfg := i.AgentRuntime.Normalized()
		if s.Model != "" {
			cfg.Model = s.Model
		}
		if i.Permissions.normalized().Sandbox == "read-only" {
			cfg.AllowWrite = false
		}
		sk := []Skill{}
		for _, v := range b.State.Skills {
			sk = append(sk, Skill{Name: v.Name, Path: v.Path})
		}
		skills, err := m.kunInputSkills(w, i, sk)
		if err != nil {
			return err
		}
		servers, err := m.kunRuntimeMCP(i.ID)
		if err != nil {
			return err
		}
		// Empty optional arrays are omitted by the start protocol and reach the
		// worker as nil; compare the same wire representation as the manifest.
		if len(skills) == 0 {
			skills = nil
		}
		if len(servers) == 0 {
			servers = nil
		}
		workspace := filepath.Clean(w.Path)
		if real, e := filepath.EvalSymlinks(workspace); e == nil {
			workspace = real
		}
		manifest := b.State.Manifest
		if manifest == nil || manifest.Workspace != workspace || manifest.ConfigHash != forkDigest(cfg) || manifest.SkillsHash != forkDigest(skills) || manifest.MCPHash != forkDigest(servers) || manifest.ContextRevision != fmt.Sprintf("%s:%d/%s:%d", i.ID, i.Revision, w.ID, w.Revision) || b.State.ApprovalPolicy != i.Permissions.normalized().ApprovalPolicy {
			return failure(409, "fork_configuration_changed", "来源配置、项目、技能或 MCP 清单已变化；不能使用旧执行状态")
		}
		target, err := m.prepareSession(s.WorkspaceID, in.Title, s.Model, s.InstanceID, SessionSource{Kind: "human"}, nil)
		if err != nil {
			return err
		}
		if target.RuntimeKind != "kun" || target.ExecutionMode != "local" {
			return failure(409, "fork_backend_changed", "当前实例不再使用本地 Kun")
		}
		id := store.ID()
		origin := p.ForkOrigin{PreviewID: id, SessionID: s.ID, RunID: s.RunID, Sequence: b.Selection.Sequence, Through: b.Selection.Through, BundleHash: b.ContentHash, Mode: in.Mode}
		target.KunFork = &origin
		configHash, err := m.forkHostConfigHash(target, w)
		if err != nil {
			return err
		}
		if configHash != initialConfig {
			return failure(409, "fork_configuration_changed", "生成预览时配置已变化，请重新读取来源")
		}
		out = KunForkPreview{ID: id, Title: in.Title, CreatedAt: store.Now(), Origin: origin, TargetSessionID: target.ID, Instruction: in.Instruction, Phase: b.State.Phase, Step: b.State.Step, Pending: len(b.State.Pending), Budget: b.State.Budget, Limits: b.State.Config.Budget, MaxSteps: b.State.Config.MaxSteps, RecordCount: len(b.Records)}
		if in.Mode == "live" {
			out.Live = &KunLivePreview{Workspace: workspace, AllowWrite: cfg.AllowWrite, ApprovalPolicy: b.State.ApprovalPolicy, MCPTools: len(b.State.MCPTools), PendingTools: []KunLiveToolPreview{}}
			for _, call := range b.State.Pending {
				var args any
				decoder := json.NewDecoder(bytes.NewBufferString(call.Function.Arguments))
				decoder.UseNumber()
				preview := "（参数不可解析，请检查来源快照）"
				if decoder.Decode(&args) == nil {
					preview = string(p.JSON(redaction.Fields(args)))
				}
				runes := []rune(preview)
				truncated := len(runes) > 2048
				if truncated {
					preview = string(runes[:2048])
				}
				out.Live.PendingTools = append(out.Live.PendingTools, KunLiveToolPreview{CallID: call.ID, Name: call.Function.Name, ArgumentsPreview: preview, Truncated: truncated})
			}
		}
		draft := kunForkDraft{Preview: out, Target: target, ConfigHash: configHash, Bundle: b}
		out.Hash = forkDraftHash(draft)
		draft.Preview = out
		return m.Store.PutMany(store.Record{Kind: "kun_fork_bundle", ID: id, Value: draft}, store.Record{Kind: "kun_fork_preview", ID: id, Value: out})
	})
	return out, err
}
func (m *Manager) kunForkDraft(id string) (kunForkDraft, error) {
	var d kunForkDraft
	if err := m.Store.Get("kun_fork_bundle", id, &d); err != nil {
		return d, failure(404, "fork_not_found", "分叉预览不存在")
	}
	if d.Preview.ID != id || d.Preview.Hash != forkDraftHash(d) || d.Bundle.ContentHash != p.ForkHash(d.Bundle) || d.Target.KunFork == nil || *d.Target.KunFork != d.Preview.Origin || d.Target.ID != d.Preview.TargetSessionID {
		return d, failure(409, "fork_integrity", "分叉记录指纹不一致")
	}
	return d, nil
}
func (m *Manager) StartKunFork(id, expected string, confirmLive bool) (Session, error) {
	m.kunForkMu.Lock()
	defer m.kunForkMu.Unlock()
	d, err := m.kunForkDraft(id)
	if err != nil {
		return Session{}, err
	}
	if expected == "" || expected != d.Preview.Hash {
		return Session{}, failure(409, "fork_preview_changed", "预览指纹不一致")
	}
	if d.Preview.Origin.Mode == "live" && !confirmLive {
		return Session{}, failure(400, "fork_live_confirmation_required", "请先核对 Live 预览并明确确认真实工具执行及重复副作用")
	}
	var deleted struct {
		SessionID string `json:"sessionId"`
	}
	if err := m.Store.Get("kun_fork_deleted", id, &deleted); err == nil {
		return Session{}, failure(409, "fork_target_deleted", "分支会话已删除；请从来源创建新预览")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Session{}, err
	}
	// One preview owns one fixed session, including after a lost response or
	// restart. Once a run ID exists, returning it never starts another run.
	s, err := m.Session(d.Target.ID)
	if err == nil && s.RunID != "" {
		return s, nil
	}
	w, err := m.Workspace(d.Target.WorkspaceID)
	if err != nil {
		return Session{}, err
	}
	current, err := m.forkHostConfigHash(d.Target, w)
	if err != nil {
		return Session{}, err
	}
	if current != d.ConfigHash {
		return Session{}, failure(409, "fork_configuration_changed", "预览后配置已变化，请重新创建预览")
	}
	if d.Preview.Origin.Mode == "live" {
		if _, _, _, err = m.kunLiveInputs(d.Target, w, d.Bundle); err != nil {
			return Session{}, err
		}
	}
	if _, err = m.Session(d.Target.ID); err != nil {
		m.mu.Lock()
		if err = m.Store.Put("session", d.Target.ID, d.Target); err == nil {
			copy := d.Target
			m.sessions[copy.ID] = &copy
		}
		m.mu.Unlock()
		if err != nil {
			return Session{}, err
		}
	}
	text := "启动 Hybrid 分叉实验（模型重算，工具录制回放）"
	if d.Preview.Origin.Mode == "live" {
		text = "启动已确认的 Live 分叉（真实工具执行，当前项目文件与 MCP，可能重复来源副作用）"
	}
	return m.start(d.Target.ID, Input{Text: text, KunFork: &kunForkInput{PreviewID: id, ConfirmLive: confirmLive}}, nil)
}
func (m *Manager) kunForkStartRequest(s Session, w Workspace, in Input, cfg p.Config, key string) (p.Start, error) {
	if in.KunFork == nil || s.KunFork == nil || in.KunFork.PreviewID != s.KunFork.PreviewID || len(in.Files) > 0 || len(in.Skills) > 0 || in.KunResume != nil {
		return p.Start{}, failure(409, "hybrid_start_only", "分叉只接受固定预览的首次启动")
	}
	d, err := m.kunForkDraft(in.KunFork.PreviewID)
	if err != nil {
		return p.Start{}, err
	}
	if d.Target.ID != s.ID || *s.KunFork != d.Preview.Origin {
		return p.Start{}, failure(409, "fork_target_mismatch", "分叉目标不一致")
	}
	current, err := m.forkHostConfigHash(s, w)
	if err != nil {
		return p.Start{}, err
	}
	if current != d.ConfigHash || forkDigest(cfg) != forkDigest(d.Bundle.State.Config) {
		return p.Start{}, failure(409, "fork_configuration_changed", "启动前配置已变化")
	}
	if d.Preview.Origin.Mode == "live" {
		if !in.KunFork.ConfirmLive {
			return p.Start{}, failure(400, "fork_live_confirmation_required", "Live 启动需要明确确认")
		}
		skills, servers, revision, err := m.kunLiveInputs(s, w, d.Bundle)
		if err != nil {
			return p.Start{}, err
		}
		return p.Start{SessionID: s.ID, RunID: s.RunID, Input: in.Text, Workspace: w.Path, Config: cfg, APIKey: key, Skills: skills, MCP: servers, ContextRevision: revision, ApprovalPolicy: d.Bundle.State.ApprovalPolicy, Fork: &p.ForkStart{Origin: d.Preview.Origin, Bundle: d.Bundle, Instruction: d.Preview.Instruction, ConfirmLive: true}}, nil
	}
	workspace := filepath.Join(m.Data, "kun", "sessions", s.ID, "hybrid-workspace")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		return p.Start{}, err
	}
	return p.Start{SessionID: s.ID, RunID: s.RunID, Input: in.Text, Workspace: workspace, Config: cfg, APIKey: key, ApprovalPolicy: d.Bundle.State.ApprovalPolicy, Fork: &p.ForkStart{Origin: d.Preview.Origin, Bundle: d.Bundle, Instruction: d.Preview.Instruction}}, nil
}
func (s *Server) kunForkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kun-forks/sources/{sid}", func(w http.ResponseWriter, r *http.Request) {
		q, err := experimentQuery(r, "offset", "limit")
		if err != nil {
			respond(w, nil, err)
			return
		}
		offset, limit, err := experimentPage(q, 50)
		if err == nil && offset > 1000000 {
			err = fmt.Errorf("offset exceeds 1000000")
		}
		if err != nil {
			respond(w, nil, experimentPageError(err))
			return
		}
		out, err := s.Manager.KunForkPoints(r.PathValue("sid"), offset, limit)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("POST /api/kun-forks", func(w http.ResponseWriter, r *http.Request) {
		var in KunForkInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.CreateKunFork(in)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-forks", func(w http.ResponseWriter, r *http.Request) {
		q, err := experimentQuery(r, "offset", "limit")
		if err != nil {
			respond(w, nil, err)
			return
		}
		offset, limit, err := experimentPage(q, 20)
		if err != nil {
			respond(w, nil, experimentPageError(err))
			return
		}
		items, err := s.Manager.Store.KunForkPreviews(r.Context(), offset, limit)
		if err != nil {
			respond(w, nil, err)
			return
		}
		more := len(items) > limit
		if more {
			items = items[:limit]
		}
		debugRespond(w, map[string]any{"items": items, "hasMore": more, "nextOffset": offset + len(items)}, nil)
	})
	mux.HandleFunc("GET /api/kun-forks/{fid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var out KunForkPreview
		err := s.Manager.Store.Get("kun_fork_preview", r.PathValue("fid"), &out)
		if err != nil {
			err = failure(404, "fork_not_found", "分叉预览不存在")
		}
		debugRespond(w, out, err)
	})
	mux.HandleFunc("POST /api/kun-forks/{fid}/start", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ExpectedHash string `json:"expectedHash"`
			ConfirmLive  bool   `json:"confirmLive"`
		}
		if !decode(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.ExpectedHash) == "" {
			respond(w, nil, failure(400, "fork_hash_required", "请先生成并检查预览"))
			return
		}
		out, err := s.Manager.StartKunFork(r.PathValue("fid"), in.ExpectedHash, in.ConfirmLive)
		respond(w, out, err)
	})
}
