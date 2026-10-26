package app

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/your-org/kun/sdk/validation"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"strings"
	"time"
)

type KunSkillInput struct {
	Title                  string              `json:"title"`
	Domain                 string              `json:"domain"`
	Applicability          string              `json:"applicability"`
	FailureCases           []string            `json:"failureCases"`
	SourcePlanIDs          []string            `json:"sourcePlanIds"`
	ValidationTaskSetID    string              `json:"validationTaskSetId"`
	ExpectedValidationHash string              `json:"expectedValidationHash"`
	InputSchema            json.RawMessage     `json:"inputSchema"`
	Steps                  []p.FastInstruction `json:"steps,omitempty"`
}
type KunSkillSource struct {
	PlanID       string `json:"planId"`
	SessionID    string `json:"sessionId"`
	RunID        string `json:"runId"`
	InputHash    string `json:"inputHash"`
	EvidenceHash string `json:"evidenceHash"`
	Through      int64  `json:"through"`
}
type KunSkill struct {
	Schema        int              `json:"schema"`
	ID            string           `json:"id"`
	Hash          string           `json:"hash"`
	Title         string           `json:"title"`
	CreatedAt     string           `json:"createdAt"`
	Applicability string           `json:"applicability"`
	FailureCases  []string         `json:"failureCases"`
	InstanceID    string           `json:"instanceId"`
	WorkspaceID   string           `json:"workspaceId"`
	Sources       []KunSkillSource `json:"sources"`
	ValidationSet KunTaskSet       `json:"validationSet"`
	Program       p.FastProgram    `json:"program"`
}
type KunSkillValidation struct {
	ID            string              `json:"id"`
	Hash          string              `json:"hash"`
	CandidateID   string              `json:"candidateId"`
	CandidateHash string              `json:"candidateHash"`
	CreatedAt     string              `json:"createdAt"`
	Plans         []KunCaseRunPreview `json:"plans"`
}
type KunCaseScore struct {
	WorkspaceFixture *p.WorkspaceFixtureState `json:"workspaceFixture,omitempty"`
	PlanID           string                   `json:"planId"`
	CaseKey          string                   `json:"caseKey"`
	Summary          KunCompareSide           `json:"summary"`
	Evaluation       KunEvaluationSide        `json:"evaluation"`
	FastCompleted    bool                     `json:"fastCompleted"`
	FastFallback     bool                     `json:"fastFallback,omitempty"`
	EvidenceHash     string                   `json:"evidenceHash"`
	Artifacts        *p.ArtifactEvidence      `json:"artifacts,omitempty"`
}
type KunSkillReport struct {
	ID            string         `json:"id"`
	Hash          string         `json:"hash"`
	CandidateID   string         `json:"candidateId"`
	CandidateHash string         `json:"candidateHash"`
	ValidationID  string         `json:"validationId"`
	CreatedAt     string         `json:"createdAt"`
	Status        string         `json:"status"`
	Passed        int            `json:"passed"`
	Failed        int            `json:"failed"`
	Unknown       int            `json:"unknown"`
	Total         int            `json:"total"`
	Score         *float64       `json:"score"`
	Cases         []KunCaseScore `json:"cases"`
}
type kunFastCapability struct {
	AppID         string `json:"appId"`
	CandidateID   string `json:"candidateId"`
	CandidateHash string `json:"candidateHash"`
	ReportID      string `json:"reportId"`
	ReportHash    string `json:"reportHash"`
	Enabled       bool   `json:"enabled"`
	UpdatedAt     string `json:"updatedAt"`
}

func skillHash(v KunSkill) string                { v.Hash = ""; return forkDigest(v) }
func validationHash(v KunSkillValidation) string { v.Hash = ""; return forkDigest(v) }
func skillReportHash(v KunSkillReport) string    { v.Hash = ""; return forkDigest(v) }
func canonicalCaseInput(input string) string {
	var v any
	if json.Unmarshal([]byte(input), &v) == nil {
		return forkDigest(v)
	}
	return forkDigest(strings.TrimSpace(input))
}
func (m *Manager) KunCaseScore(ctx context.Context, id string) (KunCaseScore, error) {
	v, err := m.KunCaseRun(id)
	out := KunCaseScore{PlanID: id, CaseKey: v.Preview.Case.Key}
	if err != nil {
		return out, err
	}
	upper, events, err := m.Store.KunCompareEvents(ctx, v.Preview.SessionID, v.Preview.RunID, nil, 0, 0)
	if err != nil {
		return out, err
	}
	out.Summary, err = reduceKunComparison(KunCompareSide{SessionID: v.Preview.SessionID, RunID: v.Preview.RunID, Mode: "case", Through: upper}, events, p.State{}, 0, nil)
	if err != nil {
		return out, err
	}
	out.Evaluation, err = scoreKunSide(out.Summary, events, v.Preview.Case.Rules)
	if err != nil {
		return out, err
	}
	out.WorkspaceFixture = scoreKunWorkspaceFixture(&out.Evaluation, out.Summary, events, v.Preview)
	out.Artifacts = scoreKunArtifacts(&out.Evaluation, out.Summary, events, v.Preview.Case.ArtifactRules)
	out.EvidenceHash = forkDigest(events)
	for _, raw := range events {
		var ev p.Event
		_ = json.Unmarshal(raw.Data, &ev)
		if ev.Type == "kun/fast.fallback" {
			out.FastCompleted = false
			out.FastFallback = true
			break
		}
		if ev.Type == "kun/fast.completed" {
			var x struct {
				CandidateID   string `json:"candidateId"`
				CandidateHash string `json:"candidateHash"`
			}
			_ = json.Unmarshal(ev.Data, &x)
			out.FastCompleted = x.CandidateID == v.Preview.CandidateID && x.CandidateHash == v.Preview.CandidateHash && x.CandidateID != ""
		}
	}
	return out, nil
}
func (m *Manager) kunCaseState(id string) (p.State, error) {
	var out p.State
	v, err := m.KunCaseRun(id)
	if err != nil {
		return out, err
	}
	if v.Session == nil || v.Session.Status != "completed" {
		return out, failure(409, "skill_source_not_completed", "来源案例必须成功结束")
	}
	s := *v.Session
	h, err := m.getHandle(s.ID)
	if err != nil {
		return out, err
	}
	h.op.Lock()
	defer h.op.Unlock()
	if err = m.reserveProcess(s.ID, h); err != nil {
		return out, err
	}
	defer m.unreserveProcess(s.ID)
	c, err := m.ensureKunWorker(s, h)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	if err = c.Call(ctx, "state", nil, &out); err != nil {
		return out, err
	}
	if out.SessionID != s.ID || out.RunID != v.Preview.RunID || out.Status != "completed" || out.Fast != nil || out.Manifest == nil {
		return out, failure(409, "skill_source_identity", "来源必须是完整的动态案例运行")
	}
	return out, nil
}

// Distillation copies only successful tool structure. It substitutes uniquely
// matching input fields; it never copies assistant prose or terminal answers.
func distillFastSteps(s p.State, input string) ([]p.FastInstruction, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &fields) != nil {
		return nil, failure(400, "skill_json_input", "快速路径需要 JSON 对象输入")
	}
	steps := []p.FastInstruction{}
	parameterized := false
	for _, msg := range s.Messages {
		for _, call := range msg.ToolCalls {
			if s.Actions[call.ID] != "succeeded" {
				return nil, failure(409, "skill_source_failed_action", "来源包含非成功动作")
			}
			var args map[string]json.RawMessage
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
				return nil, failure(400, "skill_arguments", "来源工具参数不是对象")
			}
			step := p.FastInstruction{Op: "tool", Tool: call.Function.Name, Store: fmt.Sprintf("result%d", len(steps)), Args: map[string]p.FastValue{}}
			for key, value := range args {
				ref := ""
				matches := 0
				for name, field := range fields {
					var a, b any
					_ = json.Unmarshal(value, &a)
					_ = json.Unmarshal(field, &b)
					if forkDigest(a) == forkDigest(b) {
						ref = "input#/" + strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
						matches++
					}
				}
				expr := p.FastValue{Literal: value}
				if matches == 1 {
					expr = p.FastValue{Ref: ref}
					parameterized = true
				}
				step.Args[key] = expr
			}
			steps = append(steps, step)
		}
	}
	if len(steps) == 0 || len(steps) > 63 || !parameterized {
		return nil, failure(400, "skill_no_parameterization", "需要含可参数化输入的成功工具轨迹；可提交受限 IR 草稿")
	}
	steps = append(steps, p.FastInstruction{Op: "finish", Value: p.FastValue{Ref: steps[len(steps)-1].Store}})
	return steps, nil
}
func (m *Manager) CreateKunSkill(in KunSkillInput) (KunSkill, error) {
	var out KunSkill
	if err := experimentTitle(in.Title); err != nil {
		return out, err
	}
	if len(in.SourcePlanIDs) < 1 || len(in.SourcePlanIDs) > 10 || strings.TrimSpace(in.Applicability) == "" || len(in.Applicability) > 8192 || len(in.FailureCases) < 1 || len(in.FailureCases) > 20 {
		return out, failure(400, "skill_metadata", "需要 1–10 个来源、适用范围及 1–20 个失败条件")
	}
	for _, v := range in.FailureCases {
		if strings.TrimSpace(v) == "" || len(v) > 2048 {
			return out, failure(400, "skill_failure_case", "失败条件需要 1–2048 字节")
		}
	}
	set, err := m.KunTaskSet(in.ValidationTaskSetID)
	if err != nil {
		return out, err
	}
	if set.Hash != in.ExpectedValidationHash || len(set.Cases) < 2 {
		return out, failure(409, "skill_validation_set", "需要指纹匹配且至少含两个案例的独立验证集")
	}
	heldout := map[string]bool{}
	for _, c := range set.Cases {
		key := canonicalCaseInput(c.Input)
		if heldout[key] {
			return out, failure(400, "skill_validation_duplicate", "验证集输入不能重复")
		}
		heldout[key] = true
	}
	out = KunSkill{Schema: 1, ID: store.ID(), Title: in.Title, CreatedAt: store.Now(), Applicability: in.Applicability, FailureCases: in.FailureCases, ValidationSet: set, Sources: []KunSkillSource{}}
	seen := map[string]bool{}
	for n, id := range in.SourcePlanIDs {
		if seen[id] {
			return out, failure(400, "skill_duplicate_source", "来源不能重复")
		}
		seen[id] = true
		v, err := m.KunCaseRun(id)
		if err != nil {
			return out, err
		}
		if v.Preview.CandidateID != "" || v.Preview.TaskSetID == set.ID || heldout[canonicalCaseInput(v.Preview.Case.Input)] {
			return out, failure(409, "skill_holdout_overlap", "来源与验证集必须隔离，不能有相同输入")
		}
		score, err := m.KunCaseScore(m.ctx, id)
		if err != nil {
			return out, err
		}
		if score.Evaluation.Status != "pass" {
			return out, failure(409, "skill_source_not_passed", "来源必须通过全部固定评分规则")
		}
		state, err := m.kunCaseState(id)
		if err != nil {
			return out, err
		}
		for _, status := range state.Actions {
			if status != "succeeded" {
				return out, failure(409, "skill_source_failed_action", "来源包含非成功动作，不能提炼为候选")
			}
		}
		guard := validation.FastGuardFor(state)
		for _, task := range set.Cases {
			if p.FixtureMode(task.WorkspaceFixture) != guard.WorkspaceMode {
				return out, failure(409, "skill_workspace_mode", "来源与全部验证案例必须使用相同的工作区模式")
			}
		}
		if n == 0 {
			out.InstanceID = v.Preview.InstanceID
			out.WorkspaceID = v.Preview.WorkspaceID
			steps := in.Steps
			if len(steps) == 0 {
				steps, err = distillFastSteps(state, v.Preview.Case.Input)
				if err != nil {
					return out, err
				}
			}
			out.Program = p.FastProgram{Schema: 1, Domain: in.Domain, InputSchema: in.InputSchema, Guard: guard, Steps: steps}
		} else if forkDigest(guard) != forkDigest(out.Program.Guard) {
			return out, failure(409, "skill_source_environment", "来源环境或能力版本不一致")
		}
		out.Sources = append(out.Sources, KunSkillSource{PlanID: id, SessionID: state.SessionID, RunID: state.RunID, InputHash: canonicalCaseInput(v.Preview.Case.Input), EvidenceHash: score.EvidenceHash, Through: score.Summary.Through})
	}
	if err = validation.ValidateFastProgram(out.Program); err != nil {
		return out, failure(400, "skill_ir_invalid", err.Error())
	}
	_ = json.Unmarshal(p.JSON(out), &out)
	out.Hash = skillHash(out)
	return out, m.Store.Put("kun_skill", out.ID, out)
}
func (m *Manager) KunSkill(id string) (KunSkill, error) {
	var v KunSkill
	if err := m.Store.Get("kun_skill", id, &v); err != nil {
		return v, failure(404, "skill_candidate_missing", "Skill 候选不存在")
	}
	if v.ID != id || v.Schema != 1 || v.Hash != skillHash(v) || validation.ValidateFastProgram(v.Program) != nil {
		return v, failure(409, "skill_candidate_integrity", "Skill 候选指纹不一致")
	}
	return v, nil
}
func (m *Manager) CreateKunSkillValidation(id, expected string) (KunSkillValidation, error) {
	var out KunSkill
	out, err := m.KunSkill(id)
	if err != nil {
		return KunSkillValidation{}, err
	}
	if out.Hash != expected {
		return KunSkillValidation{}, failure(409, "skill_candidate_changed", "Skill 候选指纹变化")
	}
	v := KunSkillValidation{ID: store.ID(), CandidateID: id, CandidateHash: out.Hash, CreatedAt: store.Now(), Plans: []KunCaseRunPreview{}}
	for _, c := range out.ValidationSet.Cases {
		plan, err := m.CreateKunCaseRun(KunCaseRunInput{TaskSetID: out.ValidationSet.ID, ExpectedTaskSetHash: out.ValidationSet.Hash, CaseKey: c.Key, WorkspaceID: out.WorkspaceID, InstanceID: out.InstanceID, CandidateID: id})
		if err != nil {
			return v, err
		}
		v.Plans = append(v.Plans, plan.Preview)
	}
	v.Hash = validationHash(v)
	return v, m.Store.Put("kun_skill_validation", v.ID, v)
}
func (m *Manager) KunSkillValidation(id string) (KunSkillValidation, error) {
	var v KunSkillValidation
	if err := m.Store.Get("kun_skill_validation", id, &v); err != nil {
		return v, failure(404, "skill_validation_missing", "验证批次不存在")
	}
	if v.ID != id || v.Hash != validationHash(v) {
		return v, failure(409, "skill_validation_integrity", "验证批次指纹不一致")
	}
	return v, nil
}
func (m *Manager) EvaluateKunSkill(id, expected string) (KunSkillReport, error) {
	var out KunSkillReport
	v, err := m.KunSkillValidation(id)
	if err != nil {
		return out, err
	}
	if v.Hash != expected {
		return out, failure(409, "skill_validation_changed", "验证批次指纹变化")
	}
	c, err := m.KunSkill(v.CandidateID)
	if err != nil {
		return out, err
	}
	if c.Hash != v.CandidateHash || len(v.Plans) != len(c.ValidationSet.Cases) {
		return out, failure(409, "skill_validation_binding", "验证批次绑定不一致")
	}
	out = KunSkillReport{ID: store.ID(), CandidateID: c.ID, CandidateHash: c.Hash, ValidationID: id, CreatedAt: store.Now(), Status: "unknown", Total: len(v.Plans), Cases: []KunCaseScore{}}
	for n, plan := range v.Plans {
		if plan.CandidateID != c.ID || plan.CandidateHash != c.Hash || plan.TaskSetID != c.ValidationSet.ID || plan.TaskSetHash != c.ValidationSet.Hash || plan.Case.Key != c.ValidationSet.Cases[n].Key {
			return out, failure(409, "skill_validation_binding", "验证案例绑定不一致")
		}
		score, err := m.KunCaseScore(m.ctx, plan.ID)
		if err != nil {
			return out, err
		}
		out.Cases = append(out.Cases, score)
		if score.Evaluation.Status == "unknown" {
			out.Unknown++
		} else if score.Evaluation.Status == "pass" && score.FastCompleted {
			out.Passed++
		} else {
			out.Failed++
		}
	}
	if out.Unknown == 0 {
		v := 100 * float64(out.Passed) / float64(out.Total)
		out.Score = &v
		out.Status = "fail"
		if out.Passed == out.Total {
			out.Status = "pass"
		}
	}
	out.Hash = skillReportHash(out)
	return out, m.Store.Put("kun_skill_report", out.ID, out)
}
func (m *Manager) PromoteKunSkill(id, expected, reportID, reportHash, appID string, revision int) (kunFastCapability, error) {
	var out kunFastCapability
	c, err := m.KunSkill(id)
	if err != nil {
		return out, err
	}
	if c.Program.Guard.WorkspaceMode != "" {
		return out, failure(409, "skill_evaluation_only", "独立目录候选只用于评测；常规应用需在实际工作区重新生成并验证候选")
	}
	var report KunSkillReport
	if err = m.Store.Get("kun_skill_report", reportID, &report); err != nil {
		return out, err
	}
	if c.Hash != expected || report.Hash != reportHash || report.Hash != skillReportHash(report) || report.CandidateID != c.ID || report.CandidateHash != c.Hash || report.Status != "pass" || report.Total < 2 || report.Passed != report.Total || report.Unknown != 0 {
		return out, failure(409, "skill_promotion_gate", "只有同一候选的完整独立验证全部通过才能启用")
	}
	var a Application
	if err = m.Store.Get("application", appID, &a); err != nil {
		return out, failure(404, "application_missing", "应用不存在")
	}
	if a.Revision != revision || a.InstanceID != c.InstanceID || a.WorkspaceID != c.WorkspaceID {
		return out, failure(409, "skill_application_scope", "只能启用到相同实例和工作区的指定应用，且应用版本须匹配")
	}
	out = kunFastCapability{AppID: appID, CandidateID: id, CandidateHash: c.Hash, ReportID: report.ID, ReportHash: report.Hash, Enabled: true, UpdatedAt: store.Now()}
	return out, m.Store.Put("kun_fast_capability", appID, out)
}
func (m *Manager) attachKunSkill(s Session, w Workspace, in Input, request p.Start) (p.Start, error) {
	if in.FastSkillID == "" {
		return request, nil
	}
	if s.Source.AppID == "" || in.KunResume != nil || len(in.Files) > 0 || len(in.Skills) > 0 {
		return request, failure(409, "skill_application_required", "快速 Skill 只接受已启用应用的普通 JSON 输入")
	}
	var cap kunFastCapability
	if err := m.Store.Get("kun_fast_capability", s.Source.AppID, &cap); err != nil || !cap.Enabled || cap.CandidateID != in.FastSkillID {
		return request, failure(409, "skill_not_enabled", "应用未启用该 Skill")
	}
	var app Application
	if err := m.Store.Get("application", s.Source.AppID, &app); err != nil || app.InstanceID != s.InstanceID || app.WorkspaceID != w.ID {
		return request, failure(409, "skill_application_changed", "当前应用范围已变化")
	}
	c, err := m.KunSkill(cap.CandidateID)
	if err != nil {
		return request, err
	}
	if c.Hash != cap.CandidateHash || s.InstanceID != c.InstanceID || w.ID != c.WorkspaceID {
		return request, failure(409, "skill_scope_changed", "Skill 应用范围变化")
	}
	if len(in.Text) > 32<<10 || !json.Valid([]byte(in.Text)) {
		return request, failure(400, "skill_json_input", "快速 Skill 需要不超过 32 KiB 的 JSON 输入")
	}
	request.Fast = &p.FastStart{CandidateID: c.ID, CandidateHash: c.Hash, Program: c.Program, Input: json.RawMessage(in.Text)}
	return request, nil
}

func (s *Server) kunSkillRoutes(mux *http.ServeMux) {
	for _, entry := range []struct{ Path, Kind string }{{"/api/kun-skills", "kun_skill"}, {"/api/kun-skill-validations", "kun_skill_validation"}} {
		entry := entry
		mux.HandleFunc("GET "+entry.Path, func(w http.ResponseWriter, r *http.Request) {
			q, err := experimentQuery(r, "offset", "limit", "candidateId")
			if err != nil {
				respond(w, nil, err)
				return
			}
			if entry.Kind == "kun_skill_validation" && q.Get("candidateId") == "" {
				respond(w, nil, failure(400, "candidate_required", "需要候选 ID"))
				return
			}
			offset, limit, err := experimentPage(q, 50)
			if err != nil {
				respond(w, nil, experimentPageError(err))
				return
			}
			items, err := s.Manager.Store.KunSkillList(r.Context(), entry.Kind, q.Get("candidateId"), offset, limit)
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

	mux.HandleFunc("POST /api/kun-skills", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in KunSkillInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.CreateKunSkill(in)
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/kun-skills/{kid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		v, e := s.Manager.KunSkill(r.PathValue("kid"))
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/kun-case-runs/{cid}/score", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		v, e := s.Manager.KunCaseScore(r.Context(), r.PathValue("cid"))
		debugRespond(w, v, e)
	})
	mux.HandleFunc("POST /api/kun-skills/{kid}/validations", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in struct {
			ExpectedHash string `json:"expectedHash"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.CreateKunSkillValidation(r.PathValue("kid"), in.ExpectedHash)
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/kun-skill-validations/{vid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		v, e := s.Manager.KunSkillValidation(r.PathValue("vid"))
		debugRespond(w, v, e)
	})
	mux.HandleFunc("POST /api/kun-skill-validations/{vid}/start", func(w http.ResponseWriter, r *http.Request) {
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
		v, e := s.Manager.KunSkillValidation(r.PathValue("vid"))
		if e != nil {
			debugRespond(w, nil, e)
			return
		}
		if v.Hash != in.ExpectedHash || !in.ConfirmExecute {
			respond(w, nil, failure(409, "skill_validation_confirmation", "需要核对固定验证批次并确认真实工具执行"))
			return
		}
		results := []map[string]any{}
		for _, plan := range v.Plans {
			run, err := s.Manager.StartKunCaseRun(plan.ID, plan.Hash, true)
			item := map[string]any{"planId": plan.ID, "session": run}
			if err != nil {
				item["error"] = err.Error()
			}
			results = append(results, item)
		}
		debugRespond(w, map[string]any{"results": results}, nil)
	})
	mux.HandleFunc("POST /api/kun-skill-validations/{vid}/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in struct {
			ExpectedHash string `json:"expectedHash"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.EvaluateKunSkill(r.PathValue("vid"), in.ExpectedHash)
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/kun-skill-reports/{rid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var v KunSkillReport
		e := s.Manager.Store.Get("kun_skill_report", r.PathValue("rid"), &v)
		if e == nil && v.Hash != skillReportHash(v) {
			e = failure(409, "skill_report_integrity", "报告指纹不一致")
		}
		debugRespond(w, v, e)
	})
	mux.HandleFunc("POST /api/kun-skills/{kid}/promote", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in struct {
			ExpectedHash        string `json:"expectedHash"`
			ReportID            string `json:"reportId"`
			ReportHash          string `json:"reportHash"`
			AppID               string `json:"appId"`
			ExpectedAppRevision int    `json:"expectedAppRevision"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.PromoteKunSkill(r.PathValue("kid"), in.ExpectedHash, in.ReportID, in.ReportHash, in.AppID, in.ExpectedAppRevision)
		debugRespond(w, v, e)
	})
	mux.HandleFunc("POST /api/kun-skills/{kid}/disable", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in struct {
			AppID string `json:"appId"`
		}
		if !decode(w, r, &in) {
			return
		}
		var v kunFastCapability
		e := s.Manager.Store.Get("kun_fast_capability", in.AppID, &v)
		if e == nil && v.CandidateID != r.PathValue("kid") {
			e = failure(409, "skill_capability_changed", "应用能力已变化")
		}
		if e == nil {
			v.Enabled = false
			v.UpdatedAt = store.Now()
			e = s.Manager.Store.Put("kun_fast_capability", in.AppID, v)
		}
		debugRespond(w, v, e)
	})
}
