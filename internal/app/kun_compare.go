package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

type KunCompareReply struct {
	Text       string `json:"text"`
	Characters int    `json:"characters"`
	Truncated  bool   `json:"truncated"`
	Hash       string `json:"hash"`
	EventID    int64  `json:"eventId"`
	Sequence   int64  `json:"sequence"`
}
type KunCompareTool struct {
	Hypothetical int    `json:"hypothetical"`
	Name         string `json:"name"`
	Dispatched   int    `json:"dispatched"`
	Replayed     int    `json:"replayed"`
	Failed       int    `json:"failed"`
	Declined     int    `json:"declined"`
	Unsettled    int    `json:"unsettled"`
}
type KunCompareSide struct {
	Hypothesis         *KunCompareHypothesis `json:"hypothesis,omitempty"`
	PreviewID          string                `json:"previewId,omitempty"`
	SessionID          string                `json:"sessionId"`
	RunID              string                `json:"runId"`
	Mode               string                `json:"mode"`
	Through            int64                 `json:"through"`
	AfterSequence      int64                 `json:"afterSequence"`
	LastSequence       int64                 `json:"lastSequence"`
	EventCount         int                   `json:"eventCount"`
	FirstEventID       int64                 `json:"firstEventId"`
	LastEventID        int64                 `json:"lastEventId"`
	Complete           bool                  `json:"complete"`
	Status             string                `json:"status"`
	Harness            p.Harness             `json:"harness"`
	ModelCalls         int                   `json:"modelCalls"`
	PlanningCalls      int                   `json:"planningCalls"`
	ModelCompletions   int                   `json:"modelCompletions"`
	ReportedTokens     int64                 `json:"reportedTokens"`
	UsageMissing       int                   `json:"usageMissing"`
	TokenUsageComplete bool                  `json:"tokenUsageComplete"`
	ActiveMillis       *int64                `json:"activeMillis"`
	WaitMillis         *int64                `json:"waitMillis"`
	Tools              []KunCompareTool      `json:"tools"`
	LastReply          *KunCompareReply      `json:"lastReply"`
	Warnings           []string              `json:"warnings"`
}
type KunCompareHypothesis struct {
	ParentPreviewID    string `json:"parentPreviewId"`
	Hash               string `json:"hash"`
	Position           int    `json:"position"`
	SourceSequence     int64  `json:"sourceSequence"`
	OriginalOutputHash string `json:"originalOutputHash"`
	OutputHash         string `json:"outputHash"`
	Status             string `json:"status"`
	EventID            int64  `json:"eventId,omitempty"`
}
type KunCompareDelta struct {
	ModelCalls     *int64 `json:"modelCalls"`
	ReportedTokens *int64 `json:"reportedTokens"`
	ActiveMillis   *int64 `json:"activeMillis"`
	WaitMillis     *int64 `json:"waitMillis"`
}
type KunForkComparison struct {
	Schema    int    `json:"schema"`
	PreviewID string `json:"previewId"`
	Against   string `json:"against"`
	ReadOnly  bool   `json:"readOnly"`
	Benchmark bool   `json:"benchmark"`
	Baseline  struct {
		SessionID string        `json:"sessionId"`
		RunID     string        `json:"runId"`
		Sequence  int64         `json:"sequence"`
		Through   int64         `json:"through"`
		Model     string        `json:"model"`
		Harness   p.Harness     `json:"harness"`
		Step      int           `json:"step"`
		Budget    p.BudgetUsage `json:"budget"`
	} `json:"baseline"`
	Left     KunCompareSide  `json:"left"`
	Right    KunCompareSide  `json:"right"`
	Delta    KunCompareDelta `json:"delta"`
	Warnings []string        `json:"warnings"`
}

func compareTokenUsage(raw json.RawMessage) (int64, bool) {
	var v struct {
		Total  *int64 `json:"total_tokens"`
		Input  *int64 `json:"prompt_tokens"`
		Output *int64 `json:"completion_tokens"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return 0, false
	}
	if v.Total != nil && *v.Total >= 0 && *v.Total <= 2_000_000_000 {
		return *v.Total, true
	}
	if v.Total == nil && v.Input != nil && v.Output != nil && *v.Input >= 0 && *v.Output >= 0 && *v.Input <= 1_000_000_000 && *v.Output <= 1_000_000_000 {
		return *v.Input + *v.Output, true
	}
	return 0, false
}

// Only selected summary fields leave the server. Full request bodies, private
// fork bundles, credentials, tool arguments and tool outputs are never returned.
func reduceKunComparison(side KunCompareSide, events []store.Event, baseline p.State, expectedEnd int64, origin *p.ForkOrigin) (KunCompareSide, error) {
	side.Harness = baseline.Harness
	side.Tools, side.Warnings = []KunCompareTool{}, []string{}
	side.Status = "no_records"
	last, contiguous, terminal, identity := side.AfterSequence, true, false, origin == nil
	started, completed := map[int]bool{}, map[int]bool{}
	tools, dispatched := map[string]*KunCompareTool{}, map[string]string{}
	tool := func(name string) *KunCompareTool {
		if tools[name] == nil {
			tools[name] = &KunCompareTool{Name: name}
		}
		return tools[name]
	}
	type timing struct {
		ActiveMillis *int64 `json:"activeMillis"`
		WaitMillis   *int64 `json:"waitMillis"`
	}
	var endBudget *timing
	for _, host := range events {
		raw, err := debugJSON(host.Data)
		if err != nil {
			return side, err
		}
		var event p.Event
		if json.Unmarshal(raw, &event) != nil || event.SessionID != side.SessionID || event.RunID != side.RunID || event.Type != host.Method || event.Sequence <= last {
			return side, failure(409, "comparison_record_identity", "对照记录的会话、轮次或顺序不一致")
		}
		if terminal {
			contiguous = false
		}
		if event.Sequence != last+1 {
			contiguous = false
		}
		last = event.Sequence
		if side.EventCount == 0 {
			side.FirstEventID = host.ID
			side.Status = "in_progress"
		}
		side.EventCount++
		side.LastEventID = host.ID
		side.LastSequence = event.Sequence
		var data struct {
			Step          int               `json:"step"`
			Purpose       string            `json:"purpose"`
			Status        string            `json:"status"`
			Usage         json.RawMessage   `json:"usage"`
			Message       p.Message         `json:"message"`
			Call          p.ToolCall        `json:"call"`
			Replay        *p.ReplayEvidence `json:"replay"`
			Budget        *timing           `json:"budget"`
			Fork          *p.ForkState      `json:"fork"`
			Command       *p.Control        `json:"command"`
			HarnessChange *struct {
				Current p.Harness `json:"current"`
			} `json:"harnessChange"`
		}
		if json.Unmarshal(event.Data, &data) != nil {
			return side, failure(409, "comparison_record_invalid", "对照事件内容无效")
		}
		switch event.Type {
		case "kun/run.started":
			if origin != nil {
				if data.Fork == nil || data.Fork.Origin != *origin || data.Fork.InheritedStep != baseline.Step || forkDigest(data.Fork.InheritedBudget) != forkDigest(baseline.Budget) {
					return side, failure(409, "comparison_origin_mismatch", "分支记录与固定来源不一致")
				}
				identity = true
				if side.Hypothesis != nil && data.Fork.HypothesisHash != side.Hypothesis.Hash {
					return side, failure(409, "comparison_hypothesis_mismatch", "启动记录与假设指纹不一致")
				}
			}
		case "kun/model.started":
			side.Status = "in_progress"
			if started[data.Step] || data.Step <= baseline.Step {
				return side, failure(409, "comparison_model_identity", "模型步骤身份不一致")
			}
			started[data.Step] = true
			side.ModelCalls++
			if data.Purpose == "plan" {
				side.PlanningCalls++
			}
		case "kun/model.completed":
			if completed[data.Step] {
				return side, failure(409, "comparison_model_identity", "模型完成记录重复")
			}
			completed[data.Step] = true
			side.ModelCompletions++
			if !started[data.Step] {
				contiguous = false
			}
			if n, ok := compareTokenUsage(data.Usage); ok {
				side.ReportedTokens += n
			} else {
				side.UsageMissing++
			}
			if data.Purpose != "plan" && data.Status != "rejected" && len(data.Message.ToolCalls) == 0 && data.Message.Content != "" {
				text := []rune(data.Message.Content)
				side.LastReply = &KunCompareReply{Text: string(text[:min(len(text), 2048)]), Characters: len(text), Truncated: len(text) > 2048, Hash: forkDigest(data.Message.Content), EventID: host.ID, Sequence: event.Sequence}
			}
		case "kun/tool.started":
			side.Status = "in_progress"
			if data.Call.ID == "" || dispatched[data.Call.ID] != "" {
				return side, failure(409, "comparison_tool_identity", "工具派发身份不一致")
			}
			dispatched[data.Call.ID] = data.Call.Function.Name
			tool(data.Call.Function.Name).Dispatched++
		case "kun/tool.completed":
			v := tool(data.Call.Function.Name)
			if data.Status == "replayed" && data.Replay != nil && !data.Replay.Executed {
				if data.Replay.Mode == "hypothetical" {
					h := side.Hypothesis
					if h == nil || h.EventID != 0 || data.Replay.HypothesisHash != h.Hash || data.Replay.Position != h.Position || data.Replay.SourceSequence != h.SourceSequence || data.Replay.OriginalOutputHash != h.OriginalOutputHash || data.Replay.OutputHash != h.OutputHash || origin == nil || data.Replay.BundleHash != origin.BundleHash {
						return side, failure(409, "comparison_hypothesis_mismatch", "工具回放记录与假设指纹不一致")
					}
					h.Status, h.EventID = "applied", host.ID
					v.Hypothetical++
				} else if data.Replay.Mode == "recorded" {
					v.Replayed++
				} else {
					contiguous = false
				}
				if data.Replay.RecordedStatus == "failed" {
					v.Failed++
				}
			} else if data.Status == "declined" || data.Status == "rejected" {
				v.Declined++
			} else if data.Status == "succeeded" || data.Status == "failed" {
				if dispatched[data.Call.ID] != data.Call.Function.Name {
					contiguous = false
				}
				delete(dispatched, data.Call.ID)
				if data.Status == "failed" {
					v.Failed++
				}
			} else {
				contiguous = false
			}
		case "kun/control.applied":
			if data.Command != nil && (data.Command.Operation == "resume" || data.Command.Operation == "step") {
				side.Status = "in_progress"
			}
			if data.HarnessChange != nil {
				side.Harness = data.HarnessChange.Current
			}
		case "kun/run.paused":
			side.Status = "paused"
		case "kun/run.finished":
			terminal = true
			side.Status = data.Status
			endBudget = data.Budget
		}
	}
	for _, name := range dispatched {
		tool(name).Unsettled++
	}
	for _, v := range tools {
		side.Tools = append(side.Tools, *v)
	}
	sort.Slice(side.Tools, func(i, j int) bool { return side.Tools[i].Name < side.Tools[j].Name })
	for step := range started {
		if !completed[step] {
			side.UsageMissing++
		}
	}
	side.Complete = contiguous && identity && terminal && (expectedEnd == 0 || last == expectedEnd)
	if side.Hypothesis != nil {
		if side.Complete && side.Hypothesis.EventID == 0 {
			side.Hypothesis.Status = "not_reached"
		}
		side.Warnings = append(side.Warnings, "此分支包含人工假设输出；仅 applied 表示已记录使用，not_reached 表示完整记录中未使用，unknown 表示当前记录不足以判断。")
	}
	side.TokenUsageComplete = side.Complete && side.ModelCalls == side.ModelCompletions && side.UsageMissing == 0
	if side.Complete && endBudget != nil {
		if endBudget.ActiveMillis != nil && *endBudget.ActiveMillis >= baseline.Budget.ActiveMillis {
			n := *endBudget.ActiveMillis - baseline.Budget.ActiveMillis
			side.ActiveMillis = &n
		}
		if endBudget.WaitMillis != nil && *endBudget.WaitMillis >= baseline.Budget.WaitMillis {
			n := *endBudget.WaitMillis - baseline.Budget.WaitMillis
			side.WaitMillis = &n
		}
	}
	if !side.Complete {
		side.Warnings = append(side.Warnings, "记录尚未结束、尚未同步完整或已缺失；计数仅代表当前固定范围，不能作为完整运行差值。")
	}
	if !side.TokenUsageComplete {
		side.Warnings = append(side.Warnings, "token 用量不完整；已报告 token 只是已知部分，缺失不按零计算。")
	}
	if side.ActiveMillis == nil || side.WaitMillis == nil {
		side.Warnings = append(side.Warnings, "缺少有效的结束预算，新增活动/等待耗时未知。")
	}
	return side, nil
}

func (m *Manager) kunCompareSide(ctx context.Context, d kunForkDraft, source bool, through *int64) (KunCompareSide, error) {
	s := d.Bundle.State
	side := KunCompareSide{SessionID: s.SessionID, RunID: s.RunID, Mode: "source", AfterSequence: d.Preview.Origin.Sequence}
	end := d.Preview.Origin.Through
	var origin *p.ForkOrigin
	if !source {
		side.SessionID, side.RunID, side.Mode, side.AfterSequence, side.PreviewID = d.Target.ID, "", d.Preview.Origin.Mode, 0, d.Preview.ID
		end, origin = 0, &d.Preview.Origin
		if h := d.Bundle.Hypothesis; h != nil {
			side.Hypothesis = &KunCompareHypothesis{ParentPreviewID: h.ParentPreviewID, Hash: p.HypothesisHash(*h), Position: h.Position, SourceSequence: h.SourceSequence, OriginalOutputHash: h.OriginalOutputHash, OutputHash: h.OutputHash, Status: "unknown"}
		}
	}
	upper, events, err := m.Store.KunCompareEvents(ctx, side.SessionID, side.RunID, through, side.AfterSequence, end)
	if err != nil {
		if errors.Is(err, store.ErrKunCompareLimit) {
			return side, failure(413, "comparison_too_large", err.Error())
		}
		return side, failure(409, "comparison_records_unavailable", err.Error())
	}
	if !source && len(events) > 0 {
		var first p.Event
		if json.Unmarshal(events[0].Data, &first) != nil || first.RunID == "" {
			return side, failure(409, "comparison_record_identity", "分支轮次身份缺失")
		}
		side.RunID = first.RunID
	}
	side.Through = upper
	return reduceKunComparison(side, events, s, end, origin)
}

// No worker, provider, project files or external service is touched. Same-source
// previews are compared using their immutable checkpoint plus fixed host logs.
func (m *Manager) CompareKunFork(ctx context.Context, id, against string, leftThrough, rightThrough *int64) (KunForkComparison, error) {
	out := KunForkComparison{Schema: 1, PreviewID: id, Against: against, ReadOnly: true, Benchmark: false, Warnings: []string{"这是固定记录的描述性对照；未进行独立任务评分或实际费用核算，不判断哪条路径更优。"}}
	d, err := m.kunForkDraft(id)
	if err != nil {
		return out, err
	}
	left := d
	if against != "" {
		if against == id {
			return out, failure(400, "comparison_same_branch", "请选择另一个分支或来源运行")
		}
		left, err = m.kunForkDraft(against)
		if err != nil {
			return out, err
		}
		if forkDigest(left.Bundle.State) != forkDigest(d.Bundle.State) || left.Bundle.Selection.SourceRunID != d.Bundle.Selection.SourceRunID || left.Bundle.Selection.Sequence != d.Bundle.Selection.Sequence || left.Bundle.Selection.Through != d.Bundle.Selection.Through || left.Bundle.EnvironmentHash != d.Bundle.EnvironmentHash || left.Bundle.CatalogHash != d.Bundle.CatalogHash {
			return out, failure(409, "comparison_baseline_mismatch", "只能对照同一来源轮次、同一安全点和记录截止的分支")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out.Left, err = m.kunCompareSide(ctx, left, against == "", leftThrough)
	if err != nil {
		return out, err
	}
	out.Right, err = m.kunCompareSide(ctx, d, false, rightThrough)
	if err != nil {
		return out, err
	}
	s := d.Bundle.State
	out.Baseline.SessionID, out.Baseline.RunID, out.Baseline.Sequence, out.Baseline.Through = s.SessionID, s.RunID, d.Preview.Origin.Sequence, d.Preview.Origin.Through
	out.Baseline.Model, out.Baseline.Harness, out.Baseline.Step, out.Baseline.Budget = s.Config.Model, s.Harness, s.Step, s.Budget
	if out.Left.Complete && out.Right.Complete {
		n := int64(out.Right.ModelCalls - out.Left.ModelCalls)
		out.Delta.ModelCalls = &n
		if out.Left.TokenUsageComplete && out.Right.TokenUsageComplete {
			n := out.Right.ReportedTokens - out.Left.ReportedTokens
			out.Delta.ReportedTokens = &n
		}
		if out.Left.ActiveMillis != nil && out.Right.ActiveMillis != nil {
			n := *out.Right.ActiveMillis - *out.Left.ActiveMillis
			out.Delta.ActiveMillis = &n
		}
		if out.Left.WaitMillis != nil && out.Right.WaitMillis != nil {
			n := *out.Right.WaitMillis - *out.Left.WaitMillis
			out.Delta.WaitMillis = &n
		}
	}
	if d.Preview.Instruction != "" || against != "" && left.Preview.Instruction != "" {
		out.Warnings = append(out.Warnings, "分支包含补充指令；输入条件可能不同。")
	}
	if out.Left.Mode != out.Right.Mode {
		out.Warnings = append(out.Warnings, "执行模式不同；真实派发与录制回放不能直接比较工具成本或当前世界结果。")
	}
	if out.Left.Mode == "live" || out.Right.Mode == "live" {
		out.Warnings = append(out.Warnings, "Live 使用执行时的项目文件和外部服务，未保存或回滚外部环境。")
	}
	if forkDigest(out.Left.Harness) != forkDigest(out.Right.Harness) {
		out.Warnings = append(out.Warnings, "所选范围结束时的 Harness 不同；数值差异不能单独归因于组合变化。")
	}
	return out, nil
}

func (s *Server) kunComparisonRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kun-forks/comparisons/{fid}", func(w http.ResponseWriter, r *http.Request) {
		q, err := experimentQuery(r, "against", "leftThrough", "rightThrough")
		if err != nil {
			respond(w, nil, err)
			return
		}
		var cursors [2]*int64
		for n, key := range []string{"leftThrough", "rightThrough"} {
			if q.Has(key) {
				v, e := strconv.ParseInt(q.Get(key), 10, 64)
				if e != nil || v < 0 {
					respond(w, nil, failure(400, "comparison_cursor_invalid", fmt.Sprintf("%s 必须是非负宿主事件 ID", key)))
					return
				}
				cursors[n] = &v
			}
		}
		if len(q.Get("against")) > 128 {
			respond(w, nil, failure(400, "comparison_against_invalid", "对照分支 ID 过长"))
			return
		}
		out, err := s.Manager.CompareKunFork(r.Context(), r.PathValue("fid"), q.Get("against"), cursors[0], cursors[1])
		debugRespond(w, out, err)
	})
}
