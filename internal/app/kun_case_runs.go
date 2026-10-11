package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

type KunCaseOrigin struct {
	PlanID      string `json:"planId"`
	TaskSetID   string `json:"taskSetId"`
	TaskSetHash string `json:"taskSetHash"`
	CaseKey     string `json:"caseKey"`
	InputHash   string `json:"inputHash"`
	RunID       string `json:"runId"`
}
type KunCaseRunInput struct {
	CandidateID         string `json:"candidateId,omitempty"`
	TaskSetID           string `json:"taskSetId"`
	ExpectedTaskSetHash string `json:"expectedTaskSetHash"`
	CaseKey             string `json:"caseKey"`
	WorkspaceID         string `json:"workspaceId"`
	InstanceID          string `json:"instanceId"`
}
type KunCaseRunPreview struct {
	ExecutionWorkspace string      `json:"executionWorkspace,omitempty"`
	BenchmarkID        string      `json:"benchmarkId,omitempty"`
	CandidateID        string      `json:"candidateId,omitempty"`
	CandidateHash      string      `json:"candidateHash,omitempty"`
	Schema             int         `json:"schema"`
	ID                 string      `json:"id"`
	Hash               string      `json:"hash"`
	CreatedAt          string      `json:"createdAt"`
	EngineVersion      string      `json:"engineVersion"`
	ProtocolVersion    int         `json:"protocolVersion"`
	TaskSetID          string      `json:"taskSetId"`
	TaskSetHash        string      `json:"taskSetHash"`
	TaskSetTitle       string      `json:"taskSetTitle"`
	Case               KunTaskCase `json:"case"`
	SessionID          string      `json:"sessionId"`
	RunID              string      `json:"runId"`
	InstanceID         string      `json:"instanceId"`
	InstanceName       string      `json:"instanceName"`
	WorkspaceID        string      `json:"workspaceId"`
	Workspace          string      `json:"workspace"`
	Config             p.Config    `json:"config"`
	EffectiveInput     string      `json:"effectiveInput"`
	InputHash          string      `json:"inputHash"`
	ApprovalPolicy     string      `json:"approvalPolicy"`
	MCPServers         []string    `json:"mcpServers"`
}
type KunCaseRunView struct {
	Preview KunCaseRunPreview `json:"preview"`
	Status  string            `json:"status"`
	Session *Session          `json:"session,omitempty"`
}
type kunCaseDraft struct {
	Fast           *p.FastStart      `json:"fast,omitempty"`
	Preview        KunCaseRunPreview `json:"preview"`
	Target         Session           `json:"target"`
	HostConfigHash string            `json:"hostConfigHash"`
	RequestHash    string            `json:"requestHash"`
}
type kunCaseInput struct {
	PlanID, ExpectedHash, RunID string
	ConfirmExecute              bool
}

func caseDraftHash(d kunCaseDraft) string { d.Preview.Hash = ""; return forkDigest(d) }

// Persist only a digest of resolved MCP parameters, never their credentials.
// Model API keys may rotate; the endpoint/model/config must still match.
func caseRequestHash(v p.Start) string { v.APIKey = ""; return forkDigest(v) }

func (m *Manager) prepareKunCaseRun(in KunCaseRunInput) (kunCaseDraft, error) {
	var out kunCaseDraft
	set, err := m.KunTaskSet(in.TaskSetID)
	if err != nil {
		return out, err
	}
	if in.ExpectedTaskSetHash == "" || in.ExpectedTaskSetHash != set.Hash {
		return out, failure(409, "task_set_changed", "任务集指纹不一致")
	}
	var chosen *KunTaskCase
	for _, c := range set.Cases {
		if c.Key == in.CaseKey {
			copy := c
			chosen = &copy
			break
		}
	}
	if chosen == nil {
		return out, failure(400, "case_not_found", "案例标识不属于此任务集")
	}
	i, err := m.Instance(in.InstanceID)
	if err != nil {
		return out, err
	}
	if m.Demo || i.AgentRuntime.Kind != "kun" || i.Execution.normalized().Mode != "local" {
		return out, failure(409, "case_runtime_unsupported", "案例执行需要本机 Kun 实例，不能使用 Demo 或 Codex")
	}
	w, err := m.Workspace(in.WorkspaceID)
	if err != nil {
		return out, err
	}
	title := set.Title + " · " + chosen.Title
	if utf8.RuneCountInString(title) > 120 {
		title = string([]rune(title)[:120])
	}
	target, err := m.prepareSession(w.ID, title, i.AgentRuntime.Model, i.ID, SessionSource{}, nil)
	if err != nil {
		return out, err
	}
	runID, id := store.ID(), store.ID()
	target.RunID = runID
	request, err := m.kunStartRequest(target, w, Input{Text: chosen.Input})
	if err != nil {
		return out, err
	}
	bindKunWorkspaceFixture(&request, chosen.WorkspaceFixture)
	if request.WorkspaceFixture != nil && len(request.MCP) > 0 {
		return out, failure(409, "fixture_mcp_unsupported", "独立评测目录仅支持内置文件工具；请使用未启用 MCP 的 Kun 实例")
	}
	request.ArtifactRules = append([]p.ArtifactRule(nil), chosen.ArtifactRules...)
	if err = request.Config.Validate(); err != nil {
		return out, failure(400, "case_config_invalid", err.Error())
	}
	configHash, err := m.forkHostConfigHash(target, w)
	if err != nil {
		return out, err
	}
	preview := KunCaseRunPreview{Schema: 1, ID: id, CreatedAt: store.Now(), EngineVersion: p.EngineVersion, ProtocolVersion: p.Version, TaskSetID: set.ID, TaskSetHash: set.Hash, TaskSetTitle: set.Title, Case: *chosen, SessionID: target.ID, RunID: runID, InstanceID: i.ID, InstanceName: i.Name, WorkspaceID: w.ID, Workspace: w.Path, Config: request.Config, EffectiveInput: request.Input, InputHash: forkDigest(request.Input), ApprovalPolicy: request.ApprovalPolicy, MCPServers: []string{}}
	if chosen.WorkspaceFixture != nil {
		preview.ExecutionWorkspace, err = p.FixtureRoot(w.Path, target.ID)
		if err != nil {
			return out, err
		}
	}
	for _, server := range request.MCP {
		preview.MCPServers = append(preview.MCPServers, server.Name)
	}
	target.RunID = ""
	target.KunCase = &KunCaseOrigin{PlanID: id, TaskSetID: set.ID, TaskSetHash: set.Hash, CaseKey: chosen.Key, InputHash: forkDigest(chosen.Input), RunID: runID}
	var fast *p.FastStart
	if in.CandidateID != "" {
		c, err := m.KunSkill(in.CandidateID)
		if err != nil {
			return out, err
		}
		if c.ValidationSet.ID != set.ID || c.ValidationSet.Hash != set.Hash || c.InstanceID != i.ID || c.WorkspaceID != w.ID {
			return out, failure(409, "skill_validation_scope", "候选只可运行其固定验证集和环境")
		}
		fast = &p.FastStart{CandidateID: c.ID, CandidateHash: c.Hash, Program: c.Program, Input: p.JSON(nil)}
		fast.Input = []byte(chosen.Input)
		if !json.Valid(fast.Input) {
			return out, failure(400, "skill_json_input", "验证输入需要 JSON")
		}
		preview.CandidateID, preview.CandidateHash = c.ID, c.Hash
		request.Fast = fast
	}
	draft := kunCaseDraft{Fast: fast, Preview: preview, Target: target, HostConfigHash: configHash, RequestHash: caseRequestHash(request)}
	draft.Preview.Hash = caseDraftHash(draft)
	if len(p.JSON(draft)) > 512<<10 {
		return out, failure(413, "case_preview_too_large", "案例运行预览超过 512 KiB")
	}
	return draft, nil
}
func (m *Manager) CreateKunCaseRun(in KunCaseRunInput) (KunCaseRunView, error) {
	draft, err := m.prepareKunCaseRun(in)
	if err != nil {
		return KunCaseRunView{}, err
	}
	if err = m.Store.Put("kun_case_plan", draft.Preview.ID, draft); err != nil {
		return KunCaseRunView{}, err
	}
	return KunCaseRunView{Preview: draft.Preview, Status: "planned"}, nil
}
func (m *Manager) kunCaseDraft(id string) (kunCaseDraft, error) {
	var d kunCaseDraft
	if err := m.Store.Get("kun_case_plan", id, &d); err != nil {
		return d, failure(404, "case_plan_not_found", "案例运行预览不存在")
	}
	pv := d.Preview
	expectedRoot := ""
	if pv.Case.WorkspaceFixture != nil {
		var err error
		expectedRoot, err = p.FixtureRoot(pv.Workspace, pv.SessionID)
		if err != nil {
			return d, failure(409, "case_fixture_integrity", "评测目录身份不一致")
		}
	}
	if expectedRoot != pv.ExecutionWorkspace || p.ValidateWorkspaceFixture(pv.Case.WorkspaceFixture) != nil {
		return d, failure(409, "case_fixture_integrity", "评测目录或固定文件不一致")
	}
	if pv.ID != id || pv.Schema != 1 || pv.Hash != caseDraftHash(d) || pv.SessionID != d.Target.ID || d.Target.KunCase == nil || *d.Target.KunCase != (KunCaseOrigin{PlanID: id, TaskSetID: pv.TaskSetID, TaskSetHash: pv.TaskSetHash, CaseKey: pv.Case.Key, InputHash: forkDigest(pv.Case.Input), RunID: pv.RunID}) || pv.InputHash != forkDigest(pv.EffectiveInput) {
		return d, failure(409, "case_plan_integrity", "案例运行预览指纹或绑定不一致")
	}
	return d, nil
}
func (m *Manager) KunCaseRun(id string) (KunCaseRunView, error) {
	d, err := m.kunCaseDraft(id)
	out := KunCaseRunView{Preview: d.Preview, Status: "planned"}
	if err != nil {
		return out, err
	}
	var deleted struct {
		SessionID string `json:"sessionId"`
	}
	if err = m.Store.Get("kun_case_deleted", id, &deleted); err == nil {
		out.Status = "deleted"
		return out, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	s, err := m.Session(d.Target.ID)
	if err == nil {
		if s.KunCase == nil || *s.KunCase != *d.Target.KunCase || (s.RunID != "" && s.RunID != d.Preview.RunID) {
			return out, failure(409, "case_binding_mismatch", "案例会话绑定不一致")
		}
		out.Status = s.Status
		out.Session = &s
	}
	return out, nil
}
func (m *Manager) StartKunCaseRun(id, expected string, confirmed bool) (Session, error) {
	return m.startKunCaseRun(id, expected, confirmed, "")
}
func (m *Manager) startKunCaseRun(id, expected string, confirmed bool, benchmarkID string) (Session, error) {
	// Shared with fork starts and deletion: an old preview cannot resurrect a session.
	m.kunForkMu.Lock()
	defer m.kunForkMu.Unlock()
	view, err := m.KunCaseRun(id)
	if err != nil {
		return Session{}, err
	}
	if view.Preview.BenchmarkID != benchmarkID {
		return Session{}, failure(409, "benchmark_start_only", "对照案例只能由其固定批次按顺序启动")
	}
	if expected == "" || expected != view.Preview.Hash {
		return Session{}, failure(409, "case_plan_changed", "请重新核对案例运行预览")
	}
	if !confirmed {
		return Session{}, failure(400, "case_confirmation_required", "请核对输入、模型和真实工具范围，并明确确认执行")
	}
	if view.Status == "deleted" {
		return Session{}, failure(409, "case_target_deleted", "案例会话已删除；请创建新预览")
	}
	if view.Session != nil && view.Session.RunID != "" {
		return *view.Session, nil
	}
	d, err := m.kunCaseDraft(id)
	if err != nil {
		return Session{}, err
	}
	in := Input{Text: d.Preview.Case.Input, KunCase: &kunCaseInput{PlanID: id, ExpectedHash: expected, RunID: d.Preview.RunID, ConfirmExecute: true}}
	w, err := m.Workspace(d.Target.WorkspaceID)
	if err != nil {
		return Session{}, err
	}
	if _, err = m.kunCaseStartRequest(d.Target, w, in); err != nil {
		return Session{}, err
	}
	if view.Session == nil {
		m.mu.Lock()
		err = m.Store.Put("session", d.Target.ID, d.Target)
		if err == nil {
			copy := d.Target
			m.sessions[copy.ID] = &copy
		}
		m.mu.Unlock()
		if err != nil {
			return Session{}, err
		}
	}
	return m.start(d.Target.ID, in, nil)
}
func (m *Manager) kunCaseStartRequest(s Session, w Workspace, in Input) (p.Start, error) {
	var out p.Start
	if s.KunCase == nil || in.KunCase == nil || !in.KunCase.ConfirmExecute || s.KunCase.PlanID != in.KunCase.PlanID || len(in.Files) > 0 || len(in.Skills) > 0 || in.KunResume != nil || in.KunFork != nil {
		return out, failure(409, "case_start_only", "案例会话只接受固定预览启动")
	}
	d, err := m.kunCaseDraft(in.KunCase.PlanID)
	if err != nil {
		return out, err
	}
	if s.ID != d.Target.ID || *s.KunCase != *d.Target.KunCase || in.KunCase.ExpectedHash != d.Preview.Hash || in.KunCase.RunID != d.Preview.RunID || in.Text != d.Preview.Case.Input || (s.RunID != "" && s.RunID != d.Preview.RunID) {
		return out, failure(409, "case_binding_mismatch", "案例输入或运行身份不一致")
	}
	if d.Preview.EngineVersion != p.EngineVersion || d.Preview.ProtocolVersion != p.Version {
		return out, failure(409, "case_runtime_incompatible", "请使用当前 Kun 重新生成案例预览")
	}
	set, err := m.KunTaskSet(d.Preview.TaskSetID)
	if err != nil {
		return out, err
	}
	if set.Hash != d.Preview.TaskSetHash {
		return out, failure(409, "task_set_changed", "任务集已变化")
	}
	current, err := m.forkHostConfigHash(s, w)
	if err != nil {
		return out, err
	}
	if current != d.HostConfigHash {
		return out, failure(409, "case_configuration_changed", "预览后实例、工作区或 MCP 配置变化，请重新预览")
	}
	copy := s
	copy.KunCase = nil
	copy.RunID = d.Preview.RunID
	out, err = m.kunStartRequest(copy, w, Input{Text: d.Preview.Case.Input})
	if err != nil {
		return out, err
	}
	out.Fast = d.Fast
	bindKunWorkspaceFixture(&out, d.Preview.Case.WorkspaceFixture)
	if out.WorkspaceFixture != nil && len(out.MCP) > 0 {
		return p.Start{}, failure(409, "fixture_mcp_unsupported", "独立评测目录仅支持内置文件工具")
	}
	out.ArtifactRules = append([]p.ArtifactRule(nil), d.Preview.Case.ArtifactRules...)
	if caseRequestHash(out) != d.RequestHash {
		return p.Start{}, failure(409, "case_request_changed", "有效输入、权限或 MCP 参数已变化，请重新预览")
	}
	return out, nil
}

func (s *Server) kunCaseRunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kun-case-runs", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in KunCaseRunInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.CreateKunCaseRun(in)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-case-runs/{cid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := s.Manager.KunCaseRun(r.PathValue("cid"))
		debugRespond(w, out, err)
	})
	mux.HandleFunc("POST /api/kun-case-runs/{cid}/start", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in struct {
			ExpectedHash   string `json:"expectedHash"`
			ConfirmExecute bool   `json:"confirmExecute"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.StartKunCaseRun(r.PathValue("cid"), in.ExpectedHash, in.ConfirmExecute)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-case-runs", func(w http.ResponseWriter, r *http.Request) {
		q, err := experimentQuery(r, "taskSetId", "offset", "limit")
		if err != nil {
			respond(w, nil, err)
			return
		}
		if q.Get("taskSetId") == "" || len(q.Get("taskSetId")) > 128 {
			respond(w, nil, failure(400, "task_set_required", "需要任务集 ID"))
			return
		}
		offset, limit, err := experimentPage(q, 50)
		if err != nil {
			respond(w, nil, experimentPageError(err))
			return
		}
		items, err := s.Manager.Store.KunCaseRunList(r.Context(), q.Get("taskSetId"), offset, limit)
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
}
