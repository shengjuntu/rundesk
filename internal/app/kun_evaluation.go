package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

const kunEvaluator = "reply-rules-v1"
const maxKunEvaluationReply = 256 << 10

type KunReplyRule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type KunRubricInput struct {
	PreviewID           string         `json:"previewId"`
	ExpectedPreviewHash string         `json:"expectedPreviewHash"`
	Title               string         `json:"title"`
	Rules               []KunReplyRule `json:"rules"`
}
type KunRubric struct {
	Schema       int            `json:"schema"`
	Evaluator    string         `json:"evaluator"`
	ID           string         `json:"id"`
	Hash         string         `json:"hash"`
	Title        string         `json:"title"`
	CreatedAt    string         `json:"createdAt"`
	PreviewID    string         `json:"previewId"`
	BaselineHash string         `json:"baselineHash"`
	RuleCount    int            `json:"ruleCount"`
	Rules        []KunReplyRule `json:"rules"`
}
type KunEvaluationInput struct {
	RubricID               string `json:"rubricId"`
	ExpectedRubricHash     string `json:"expectedRubricHash"`
	PreviewID              string `json:"previewId"`
	Against                string `json:"against"`
	LeftThrough            *int64 `json:"leftThrough"`
	RightThrough           *int64 `json:"rightThrough"`
	ExpectedComparisonHash string `json:"expectedComparisonHash"`
}
type KunRuleResult struct {
	Index   int    `json:"index"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	EventID int64  `json:"eventId,omitempty"`
}
type KunEvaluationReply struct {
	Status   string `json:"status"`
	Hash     string `json:"hash,omitempty"`
	EventID  int64  `json:"eventId,omitempty"`
	Sequence int64  `json:"sequence,omitempty"`
	Bytes    int    `json:"bytes"`
}
type KunEvaluationSide struct {
	Status  string             `json:"status"`
	Passed  int                `json:"passed"`
	Failed  int                `json:"failed"`
	Unknown int                `json:"unknown"`
	Total   int                `json:"total"`
	Score   *float64           `json:"score"`
	Reply   KunEvaluationReply `json:"reply"`
	Results []KunRuleResult    `json:"results"`
}
type KunEvaluation struct {
	Schema                int               `json:"schema"`
	Evaluator             string            `json:"evaluator"`
	ID                    string            `json:"id"`
	Hash                  string            `json:"hash"`
	CreatedAt             string            `json:"createdAt"`
	PreviewID             string            `json:"previewId"`
	Rubric                KunRubric         `json:"rubric"`
	Comparison            KunForkComparison `json:"comparison"`
	Left                  KunEvaluationSide `json:"left"`
	Right                 KunEvaluationSide `json:"right"`
	ScoreDelta            *float64          `json:"scoreDelta"`
	IndependentValidation bool              `json:"independentValidation"`
	Warnings              []string          `json:"warnings"`
}

func rubricHash(r KunRubric) string         { r.Hash = ""; return forkDigest(r) }
func evaluationHash(r KunEvaluation) string { r.Hash = ""; return forkDigest(r) }
func validateReplyRules(rules []KunReplyRule) error {
	if len(rules) < 1 || len(rules) > 12 {
		return failure(400, "rubric_rules_invalid", "需要 1–12 条文本规则；运行成功结束为固定的额外一项")
	}
	seen := map[string]bool{}
	total := 0
	for _, rule := range rules {
		if rule.Kind != "reply_contains" && rule.Kind != "reply_not_contains" && rule.Kind != "reply_equals" {
			return failure(400, "rubric_rule_unsupported", "只支持最终回复包含、不包含或完全等于指定文本")
		}
		if !utf8.ValidString(rule.Value) || strings.TrimSpace(rule.Value) == "" || utf8.RuneCountInString(rule.Value) > 2048 || len(rule.Value) > 8192 {
			return failure(400, "rubric_text_invalid", "每条规则需要 1–2048 字符的有效文本，不能全为空白")
		}
		key := forkDigest(rule)
		if seen[key] {
			return failure(400, "rubric_rule_duplicate", "不接受重复规则")
		}
		seen[key] = true
		total += len(rule.Value)
	}
	if total > 16<<10 {
		return failure(400, "rubric_too_large", "规则文本总计不能超过 16 KiB UTF-8")
	}
	return nil
}
func (m *Manager) CreateKunRubric(in KunRubricInput) (KunRubric, error) {
	var out KunRubric
	if err := experimentTitle(in.Title); err != nil {
		return out, err
	}
	if err := validateReplyRules(in.Rules); err != nil {
		return out, err
	}
	d, err := m.kunForkDraft(in.PreviewID)
	if err != nil {
		return out, err
	}
	if in.ExpectedPreviewHash == "" || in.ExpectedPreviewHash != d.Preview.Hash {
		return out, failure(409, "fork_preview_changed", "预览指纹不一致")
	}
	out = KunRubric{Schema: 1, Evaluator: kunEvaluator, ID: store.ID(), Title: in.Title, CreatedAt: store.Now(), PreviewID: in.PreviewID, BaselineHash: kunBaselineHash(d), RuleCount: len(in.Rules) + 1, Rules: in.Rules}
	out.Hash = rubricHash(out)
	return out, m.Store.Put("kun_rubric", out.ID, out)
}
func (m *Manager) KunRubric(id string) (KunRubric, error) {
	var out KunRubric
	if err := m.Store.Get("kun_rubric", id, &out); err != nil {
		return out, failure(404, "rubric_not_found", "评分规则不存在")
	}
	if out.ID != id || out.Schema != 1 || out.Evaluator != kunEvaluator || out.Hash != rubricHash(out) || out.RuleCount != len(out.Rules)+1 || validateReplyRules(out.Rules) != nil {
		return out, failure(409, "rubric_integrity", "评分规则版本或指纹不一致")
	}
	return out, nil
}

// This examines the full retained host reply from the SAME bounded read as the
// comparison. Never grade the 2048-character display snippet or private tape.
// A later act request/completion supersedes an earlier reply, including empty,
// rejected or tool-calling responses. Inherited and planning text do not count.
func evaluationReply(side KunCompareSide, events []store.Event) (KunEvaluationReply, string, error) {
	out := KunEvaluationReply{Status: "incomplete"}
	if !side.Complete {
		return out, "", nil
	}
	out.Status = "missing"
	var text string
	for _, host := range events {
		if host.Method != "kun/model.started" && host.Method != "kun/model.completed" {
			continue
		}
		raw, err := debugJSON(host.Data)
		if err != nil {
			return out, "", err
		}
		var event p.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return out, "", err
		}
		var data struct {
			Purpose string    `json:"purpose"`
			Status  string    `json:"status"`
			Message p.Message `json:"message"`
		}
		if err = json.Unmarshal(event.Data, &data); err != nil {
			return out, "", err
		}
		if data.Purpose == "plan" {
			continue
		}
		out, text = KunEvaluationReply{Status: "missing"}, ""
		if event.Type != "kun/model.completed" || data.Message.Role != "assistant" || data.Status == "rejected" || len(data.Message.ToolCalls) > 0 || data.Message.Content == "" {
			continue
		}
		text = data.Message.Content
		out = KunEvaluationReply{Status: "available", Hash: forkDigest(text), EventID: host.ID, Sequence: event.Sequence, Bytes: len(text)}
		if len(text) > maxKunEvaluationReply {
			out.Status = "too_large"
			text = ""
		}
	}
	return out, text, nil
}
func scoreKunSide(side KunCompareSide, events []store.Event, rules []KunReplyRule) (KunEvaluationSide, error) {
	reply, text, err := evaluationReply(side, events)
	if err != nil {
		return KunEvaluationSide{}, err
	}
	out := KunEvaluationSide{Status: "unknown", Total: len(rules) + 1, Reply: reply, Results: []KunRuleResult{}}
	add := func(index int, kind, status, reason string, eventID int64) {
		out.Results = append(out.Results, KunRuleResult{Index: index, Kind: kind, Status: status, Reason: reason, EventID: eventID})
		switch status {
		case "pass":
			out.Passed++
		case "fail":
			out.Failed++
		default:
			out.Unknown++
		}
	}
	if !side.Complete {
		add(0, "run_completed", "unknown", "incomplete_records", 0)
	} else if side.Status == "completed" {
		add(0, "run_completed", "pass", "completed", side.LastEventID)
	} else if side.Status == "failed" || side.Status == "interrupted" || side.Status == "cancelled" {
		add(0, "run_completed", "fail", "terminal_not_completed", side.LastEventID)
	} else {
		add(0, "run_completed", "unknown", "terminal_status_missing", side.LastEventID)
	}
	for i, rule := range rules {
		if !side.Complete {
			add(i+1, rule.Kind, "unknown", "incomplete_records", 0)
			continue
		}
		if reply.Status == "too_large" {
			add(i+1, rule.Kind, "unknown", "reply_exceeds_256_kib", reply.EventID)
			continue
		}
		if reply.Status != "available" {
			add(i+1, rule.Kind, "fail", "no_final_reply", 0)
			continue
		}
		pass := false
		switch rule.Kind {
		case "reply_contains":
			pass = strings.Contains(text, rule.Value)
		case "reply_not_contains":
			pass = !strings.Contains(text, rule.Value)
		case "reply_equals":
			pass = text == rule.Value
		}
		if pass {
			add(i+1, rule.Kind, "pass", "matched", reply.EventID)
		} else {
			add(i+1, rule.Kind, "fail", "not_matched", reply.EventID)
		}
	}
	if out.Failed > 0 {
		out.Status = "fail"
	} else if out.Unknown == 0 {
		out.Status = "pass"
	}
	if out.Unknown == 0 {
		score := float64(out.Passed) * 100 / float64(out.Total)
		out.Score = &score
	}
	return out, nil
}

func (m *Manager) CreateKunEvaluation(ctx context.Context, in KunEvaluationInput) (KunEvaluation, error) {
	var out KunEvaluation
	if in.LeftThrough == nil || in.RightThrough == nil || *in.LeftThrough < 0 || *in.RightThrough < 0 || in.ExpectedComparisonHash == "" || len(in.Against) > 128 {
		return out, failure(400, "evaluation_range_required", "请先读取对照并提交两侧固定宿主上界和对照指纹；零也是有效上界")
	}
	rubric, err := m.KunRubric(in.RubricID)
	if err != nil {
		return out, err
	}
	if in.ExpectedRubricHash == "" || in.ExpectedRubricHash != rubric.Hash {
		return out, failure(409, "rubric_changed", "规则指纹不一致，请重新读取")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var evidence kunComparisonEvidence
	comparison, err := m.compareKunFork(ctx, in.PreviewID, in.Against, in.LeftThrough, in.RightThrough, &evidence)
	if err != nil {
		return out, err
	}
	if comparison.Baseline.Hash != rubric.BaselineHash {
		return out, failure(409, "rubric_baseline_mismatch", "规则只适用于其绑定的同一来源安全点和记录截止")
	}
	if comparison.Hash != in.ExpectedComparisonHash {
		return out, failure(409, "evaluation_records_changed", "固定记录内容已变化或缺失，请重新读取对照")
	}
	out = KunEvaluation{Schema: 1, Evaluator: kunEvaluator, ID: store.ID(), CreatedAt: store.Now(), PreviewID: in.PreviewID, Rubric: rubric, Comparison: comparison, Warnings: []string{"这是固定日志上的文本规则评分，不验证事实、产物或外部副作用，也不是独立验证集成绩。", "规则可在看到结果后创建；没有预注册或测试集隔离，不能据此认定优化收益。", "计分包含运行成功结束及每条文本规则，各占一项；未知不按零分处理。", "规则匹配完整保留的最终回复，区分大小写、空白和 Unicode 编码，不执行正则、脚本或模型裁判。"}}
	out.Left, err = scoreKunSide(comparison.Left, evidence.Left, rubric.Rules)
	if err != nil {
		return out, err
	}
	out.Right, err = scoreKunSide(comparison.Right, evidence.Right, rubric.Rules)
	if err != nil {
		return out, err
	}
	if out.Left.Score != nil && out.Right.Score != nil {
		delta := *out.Right.Score - *out.Left.Score
		out.ScoreDelta = &delta
	}
	out.Hash = evaluationHash(out)
	if len(p.JSON(out)) > 4<<20 {
		return out, failure(413, "evaluation_too_large", "固定报告超过 4 MiB")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return out, m.Store.Put("kun_evaluation", out.ID, out)
}
func (m *Manager) KunEvaluation(id string) (KunEvaluation, error) {
	var out KunEvaluation
	if err := m.Store.Get("kun_evaluation", id, &out); err != nil {
		return out, failure(404, "evaluation_not_found", "评分报告不存在")
	}
	if out.ID != id || out.Schema != 1 || out.Evaluator != kunEvaluator || out.Hash != evaluationHash(out) || out.Rubric.Hash != rubricHash(out.Rubric) || out.Comparison.Hash != kunComparisonHash(out.Comparison) {
		return out, failure(409, "evaluation_integrity", "固定报告版本或指纹不一致")
	}
	return out, nil
}

func (s *Server) kunEvaluationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kun-rubrics", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in KunRubricInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.CreateKunRubric(in)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("POST /api/kun-evaluations", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in KunEvaluationInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.CreateKunEvaluation(r.Context(), in)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-rubrics/{rid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := s.Manager.KunRubric(r.PathValue("rid"))
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-evaluations/{eid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := s.Manager.KunEvaluation(r.PathValue("eid"))
		debugRespond(w, out, err)
	})
	list := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			q, err := experimentQuery(r, "previewId", "offset", "limit")
			if err != nil {
				respond(w, nil, err)
				return
			}
			if q.Get("previewId") == "" || len(q.Get("previewId")) > 128 {
				respond(w, nil, failure(400, "evaluation_preview_required", "需要有效的预览 ID"))
				return
			}
			offset, limit, err := experimentPage(q, 20)
			if err != nil {
				respond(w, nil, experimentPageError(err))
				return
			}
			key := q.Get("previewId")
			if kind == "rubrics" {
				d, e := s.Manager.kunForkDraft(key)
				if e != nil {
					respond(w, nil, e)
					return
				}
				key = kunBaselineHash(d)
			}
			items, err := s.Manager.Store.KunEvaluationList(r.Context(), kind, key, offset, limit)
			if err != nil {
				respond(w, nil, err)
				return
			}
			more := len(items) > limit
			if more {
				items = items[:limit]
			}
			debugRespond(w, map[string]any{"items": items, "nextOffset": offset + len(items), "hasMore": more}, nil)
		}
	}
	mux.HandleFunc("GET /api/kun-rubrics", list("rubrics"))
	mux.HandleFunc("GET /api/kun-evaluations", list("evaluations"))
}
