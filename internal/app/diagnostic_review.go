package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	kc "github.com/shengjuntu/rundesk/internal/adapters/kun"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
	"github.com/shengjuntu/rundesk/internal/tracequery"
)

type DiagnosticReviewInput struct {
	ProposalEventID int64  `json:"proposalEventId"`
	Text            string `json:"text"`
}

// Command is immutable after preview. Retrying an uncertain dispatch uses the
// same worker request ID, including after a host restart.
type DiagnosticReview struct {
	ID                  string              `json:"id"`
	DiagnosticSessionID string              `json:"diagnosticSessionId"`
	ProposalEventID     int64               `json:"proposalEventId"`
	Proposal            tracequery.Proposal `json:"proposal"`
	Command             p.Control           `json:"command"`
	ObservedStatus      string              `json:"observedStatus"`
	ObservedPhase       string              `json:"observedPhase"`
	CreatedAt           string              `json:"createdAt"`
	SubmittedAt         string              `json:"submittedAt,omitempty"`
	Outcome             string              `json:"outcome"`
	Error               string              `json:"error,omitempty"`
	Receipt             *p.Receipt          `json:"receipt,omitempty"`
	ReceiptEventID      int64               `json:"receiptEventId,omitempty"`
}

func reviewPrefix(sid string, eventID int64) string { return fmt.Sprintf("%s.%d.", sid, eventID) }

func (m *Manager) diagnosticSource(sid string) (Session, Session, error) {
	d, err := m.Session(sid)
	if err != nil {
		return d, Session{}, err
	}
	if d.RuntimeKind != "kun" || d.TraceOrigin == nil {
		return d, Session{}, failure(400, "diagnostic_required", "仅 Kun 独立诊断会话支持建议审核")
	}
	source, err := m.Session(d.TraceOrigin.SessionID)
	if err != nil {
		return d, source, err
	}
	if source.RuntimeKind != "kun" || source.InstanceID != d.InstanceID || source.WorkspaceID != d.WorkspaceID {
		return d, source, failure(409, "diagnostic_source_changed", "来源后端或归属已改变")
	}
	return d, source, nil
}

// Resolve the suggestion from the retained worker event, not client-supplied text
// pretending to be a model proposal. Revalidate its fixed evidence scope.
func (m *Manager) diagnosticProposal(ctx context.Context, sid string, eventID int64) (tracequery.Proposal, Session, error) {
	var proposal tracequery.Proposal
	d, source, err := m.diagnosticSource(sid)
	if err != nil {
		return proposal, source, err
	}
	size, err := m.Store.DebugEventSize(sid, eventID)
	if eventID < 1 || err != nil || size > 1<<20 {
		return proposal, source, failure(400, "invalid_suggestion", "建议事件不存在或过大")
	}
	event, err := m.Store.Event(sid, eventID)
	if err != nil {
		return proposal, source, err
	}
	var worker p.Event
	var data struct {
		Call    p.ToolCall          `json:"call"`
		Status  string              `json:"status"`
		IsError bool                `json:"isError"`
		Result  tracequery.Proposal `json:"result"`
	}
	if event.Method != "kun/tool.completed" || json.Unmarshal(event.Data, &worker) != nil || worker.SessionID != sid || worker.Type != event.Method || json.Unmarshal(worker.Data, &data) != nil || data.Call.Function.Name != "trace_propose" || data.Status != "succeeded" || data.IsError {
		return proposal, source, failure(400, "invalid_suggestion", "需要成功生成建议的 Kun 工具事件")
	}
	proposal = data.Result
	origin := d.TraceOrigin
	if proposal.Status != "suggestion_only" || proposal.SessionID != source.ID || proposal.RunID != origin.RunID || proposal.Through != origin.Through {
		return proposal, source, failure(409, "suggestion_scope_changed", "建议与诊断来源范围不一致")
	}
	reader, err := tracequery.Open(filepath.Join(m.Data, "kun", "sessions", sid, "trace-source.db"), source.ID, origin.Through)
	if err != nil {
		return proposal, source, err
	}
	defer reader.Close()
	checked, err := reader.CallContext(ctx, "trace_propose", tracequery.Args{RunID: proposal.RunID, Proposal: &proposal.ProposalInput})
	if err != nil {
		return proposal, source, failure(409, "suggestion_evidence_unavailable", err.Error())
	}
	if checked.(tracequery.Proposal).ID != proposal.ID {
		return proposal, source, failure(409, "suggestion_changed", "建议内容与已验证标识不一致")
	}
	return proposal, source, nil
}

func (m *Manager) PreviewDiagnosticReview(ctx context.Context, sid string, in DiagnosticReviewInput) (DiagnosticReview, error) {
	var review DiagnosticReview
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if strings.TrimSpace(in.Text) == "" || utf8.RuneCountInString(in.Text) > 4000 {
		return review, failure(400, "invalid_review_text", "审核后的补充指令应为 1–4000 个字符")
	}
	proposal, source, err := m.diagnosticProposal(ctx, sid, in.ProposalEventID)
	if err != nil {
		return review, err
	}
	if proposal.Kind != "steer" {
		return review, failure(400, "suggestion_not_executable", "本版仅接通补充指令建议；检查和配置建议请人工处理")
	}
	client, err := m.kunClient(source.ID)
	if err != nil {
		return review, err
	}
	var current p.State
	if err = client.Call(ctx, "state", nil, &current); err != nil {
		return review, err
	}
	if current.RunID != proposal.RunID || source.RunID != proposal.RunID || !p.Active(current.Status) {
		return review, failure(409, "suggestion_run_changed", "来源轮次已结束或改变；不能把历史建议发送到新运行")
	}
	id := store.ID()
	review = DiagnosticReview{ID: reviewPrefix(sid, in.ProposalEventID) + id, DiagnosticSessionID: sid, ProposalEventID: in.ProposalEventID, Proposal: proposal, Command: p.Control{RequestID: "diagnostic-" + id, RunID: current.RunID, ExpectedRevision: current.Revision, Operation: "steer", Text: in.Text}, ObservedStatus: current.Status, ObservedPhase: current.Phase, CreatedAt: store.Now(), Outcome: "preview"}
	err = m.Store.Put("diagnostic_review", review.ID, review)
	return review, err
}

func (m *Manager) observedDiagnosticReview(review DiagnosticReview) (DiagnosticReview, error) {
	ev, err := m.Store.DiagnosticControlReceipt(review.Proposal.SessionID, review.Command.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return review, nil
	}
	if err != nil {
		return review, err
	}
	var worker p.Event
	var data struct {
		Command p.Control `json:"command"`
		Receipt p.Receipt `json:"receipt"`
	}
	if json.Unmarshal(ev.Data, &worker) != nil || json.Unmarshal(worker.Data, &data) != nil || worker.RunID != review.Command.RunID || data.Receipt.RequestID != review.Command.RequestID {
		return review, fmt.Errorf("invalid diagnostic control receipt")
	}
	// Host events are redacted, so text may differ; IDs and operation establish the link.
	if data.Command.Operation != "steer" || data.Command.RunID != review.Command.RunID {
		return review, fmt.Errorf("diagnostic control target mismatch")
	}
	if data.Receipt.Status != "queued" && data.Receipt.Status != "applied" && data.Receipt.Status != "rejected" {
		return review, fmt.Errorf("unknown diagnostic control receipt")
	}
	review.Receipt = &data.Receipt
	review.Outcome = data.Receipt.Status
	review.ReceiptEventID = ev.ID
	review.Error = ""
	return review, nil
}

func (m *Manager) DiagnosticReviews(sid string, eventID int64) ([]DiagnosticReview, error) {
	if _, _, err := m.diagnosticSource(sid); err != nil {
		return nil, err
	}
	if eventID < 1 {
		return nil, failure(400, "invalid_suggestion", "需要建议事件编号")
	}
	raw, err := m.Store.DiagnosticReviews(reviewPrefix(sid, eventID))
	if err != nil {
		return nil, err
	}
	result := []DiagnosticReview{}
	for _, row := range raw {
		var review DiagnosticReview
		if err = json.Unmarshal(row, &review); err != nil {
			return nil, err
		}
		review, err = m.observedDiagnosticReview(review)
		if err != nil {
			return nil, err
		}
		result = append(result, review)
	}
	return result, nil
}

func (m *Manager) ApplyDiagnosticReview(ctx context.Context, sid, id string) (DiagnosticReview, error) {
	m.diagnosticReviewMu.Lock()
	defer m.diagnosticReviewMu.Unlock()
	var review DiagnosticReview
	if _, _, err := m.diagnosticSource(sid); err != nil {
		return review, err
	}
	if err := m.Store.Get("diagnostic_review", id, &review); err != nil {
		return review, failure(404, "review_not_found", "审核预览不存在")
	}
	if review.DiagnosticSessionID != sid {
		return DiagnosticReview{}, failure(404, "review_not_found", "审核预览不属于此诊断会话")
	}
	var err error
	review, err = m.observedDiagnosticReview(review)
	if err != nil {
		return review, err
	}
	if review.Receipt != nil {
		return review, nil
	}
	if review.Outcome == "rejected" {
		return review, failure(409, "review_rejected", "此预览已被拒绝，请重新预览当前状态")
	}
	proposal, source, err := m.diagnosticProposal(ctx, sid, review.ProposalEventID)
	if err != nil {
		return review, err
	}
	if proposal.ID != review.Proposal.ID {
		return review, failure(409, "suggestion_changed", "建议内容已改变")
	}
	client, err := m.kunClient(source.ID)
	if err != nil {
		return review, err
	}
	// Persist the command before dispatch; a dropped reply must not create a new command.
	if review.SubmittedAt == "" {
		review.SubmittedAt = store.Now()
	}
	review.Outcome = "unknown"
	if err = m.Store.Put("diagnostic_review", id, review); err != nil {
		return review, err
	}
	var receipt p.Receipt
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = client.Call(callCtx, "control", review.Command, &receipt)
	if err != nil {
		review.Error = err.Error()
		var remote *kc.RemoteError
		if errors.As(err, &remote) {
			review.Outcome = "rejected"
		}
		if saveErr := m.Store.Put("diagnostic_review", id, review); saveErr != nil {
			return review, saveErr
		}
		if review.Outcome == "rejected" {
			return review, failure(409, "review_stale", "来源状态已变化或不能接收指令，请重新预览："+err.Error())
		}
		return review, failure(502, "review_outcome_unknown", "发送结果未知；请刷新执行记录或重试同一预览，不要重复新建："+err.Error())
	}
	review.Receipt = &receipt
	review.Outcome = receipt.Status
	review.Error = ""
	err = m.Store.Put("diagnostic_review", id, review)
	return review, err
}

func (s *Server) diagnosticReviewRoutes(mux *http.ServeMux) {
	// Existing gates default-deny these administrator-only routes for app/member keys.
	mux.HandleFunc("GET /api/sessions/{sid}/diagnostic/reviews", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.URL.Query().Get("proposalEventId"), 10, 64)
		if err != nil {
			respond(w, nil, failure(400, "invalid_suggestion", "需要建议事件编号"))
			return
		}
		v, err := s.Manager.DiagnosticReviews(r.PathValue("sid"), id)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/diagnostic/reviews", func(w http.ResponseWriter, r *http.Request) {
		var in DiagnosticReviewInput
		if !decode(w, r, &in) {
			return
		}
		v, err := s.Manager.PreviewDiagnosticReview(r.Context(), r.PathValue("sid"), in)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/diagnostic/reviews/{rid}/apply", func(w http.ResponseWriter, r *http.Request) {
		var in struct{}
		if !decode(w, r, &in) {
			return
		}
		v, err := s.Manager.ApplyDiagnosticReview(r.Context(), r.PathValue("sid"), r.PathValue("rid"))
		respond(w, v, err)
	})
}
