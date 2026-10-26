package tracequery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/observation"
	"github.com/shengjuntu/rundesk/internal/redaction"
)

// Transcript is a presentation projection, not another event store. Its activity
// identity and provenance are shared with the metadata-only observation model.
type TranscriptArgs struct {
	RunID  string `json:"runId,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type TranscriptBlock struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Label    string `json:"label,omitempty"`
	Language string `json:"language,omitempty"`
	Path     string `json:"path,omitempty"`
}
type TranscriptEntry struct {
	ID                 string               `json:"id"`
	Activity           observation.Activity `json:"activity"`
	Role               string               `json:"role"`
	Title              string               `json:"title"`
	Blocks             []TranscriptBlock    `json:"blocks"`
	ContentTruncated   bool                 `json:"contentTruncated"`
	ContentUnavailable bool                 `json:"contentUnavailable"`
	ContentPartial     bool                 `json:"contentPartial"`
}
type TranscriptPage struct {
	SchemaVersion   int               `json:"schemaVersion"`
	SessionID       string            `json:"sessionId"`
	SnapshotThrough int64             `json:"snapshotThrough"`
	Entries         []TranscriptEntry `json:"entries"`
	Total           int               `json:"total"`
	Offset          int               `json:"offset"`
	NextOffset      int               `json:"nextOffset"`
	HasMore         bool              `json:"hasMore"`
	Coverage        string            `json:"coverage"`
}

const transcriptEntryCharacters = 12000
const transcriptSourceBytes = 16 << 20

func (r *Reader) Transcript(ctx context.Context, a TranscriptArgs) (TranscriptPage, error) {
	page := TranscriptPage{SchemaVersion: observation.SchemaVersion, SessionID: r.SessionID, SnapshotThrough: r.Through, Entries: []TranscriptEntry{}, Coverage: "Reading projection of retained runtime evidence, not a complete internal model transcript. Text is structurally redacted; free text is not credential-scanned. Display blocks are bounded to 12000 Unicode characters per entry and source reads to 16 MiB per page. Transport payloads are read separately. Preserve content and evidence completeness flags; missing records do not prove absence."}
	if a.Offset < 0 || a.Limit < 0 || a.Limit > 50 || utf8.RuneCountInString(a.RunID) > 256 {
		return page, errors.New("invalid transcript selectors; limit must be at most 50")
	}
	if a.Limit == 0 {
		a.Limit = 20
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	x, err := r.unifiedIndex(ctx)
	if err != nil {
		return page, err
	}
	value, err := r.unifiedActivity(ctx, x, UnifiedArgs{RunID: a.RunID, Limit: 1 << 30})
	if err != nil {
		return page, err
	}
	activities := value.(observation.ActivityPage).Activity
	steps := map[string]Step{}
	prompts := map[string]map[string]int{}
	for _, s := range r.steps {
		steps[s.ID] = s

	}
	selected := make([]observation.Activity, 0, len(activities))
	for _, act := range activities {
		s := steps[act.ID]
		if !transcriptVisible(act, s) {
			continue
		}
		// Display-only echo suppression consumes one preceding host input per
		// exact full-text match. Repeated or mixed-content native inputs survive.
		if (s.Type == "run/input" || s.Type == "run/steer") && s.contentFingerprint != "" {
			if prompts[act.RunID] == nil {
				prompts[act.RunID] = map[string]int{}
			}
			prompts[act.RunID][s.contentFingerprint]++
		}
		if act.Type == "userMessage" && s.contentFingerprint != "" && prompts[act.RunID][s.contentFingerprint] > 0 {
			prompts[act.RunID][s.contentFingerprint]--
			continue
		}
		selected = append(selected, act)
	}
	page.Total = len(selected)
	page.Offset = a.Offset
	if page.Offset > page.Total {
		page.Offset = page.Total
	}
	page.NextOffset = page.Offset + a.Limit
	if page.NextOffset > page.Total {
		page.NextOffset = page.Total
	}
	page.HasMore = page.NextOffset < page.Total
	sourceBudget := transcriptSourceBytes
	openItems := map[string]int{}
	for _, act := range activities {
		if act.Source == observation.CodexAppServer && !act.EndObserved && act.Native.TurnID != "" && act.Native.ItemID != "" {
			openItems[key(act.Native.TurnID, act.Native.ItemID)]++
		}
	}
	for _, act := range selected[page.Offset:page.NextOffset] {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		s := steps[act.ID]
		entry := TranscriptEntry{ID: act.ID, Activity: act, Role: "system", Title: act.Title, Blocks: []TranscriptBlock{}}
		if act.Kind == "transport" {
			page.Entries = append(page.Entries, entry)
			continue
		}
		payload := map[string]any{}
		for _, ev := range act.Evidence {
			data, available, err := r.transcriptEvent(ctx, ev.EventID, &sourceBudget)
			if err != nil {
				return page, err
			}
			if !available {
				entry.ContentUnavailable = true
				continue
			}
			native := data
			if act.Source == observation.KunSDK {
				native = obj(data["data"])
			}
			if act.Source == observation.CodexAppServer && (x.Events[ev.EventID].Method == "item/started" || x.Events[ev.EventID].Method == "item/completed") {
				native = obj(obj(data["params"])["item"])
			}
			transcriptMerge(payload, native)
		}
		if entry.ContentUnavailable {
			// Still make the already-bounded projection readable, clearly marked as a
			// preview. A raw source exceeding this page's budget is never silently full.
			for k, v := range s.Payload {
				if _, ok := payload[k]; !ok {
					payload[k] = v
				}
			}
			entry.ContentTruncated = true
		}
		transcriptContent(&entry, payload)
		if act.Source == observation.CodexAppServer && !act.EndObserved && (act.Type == "agentMessage" || act.Type == "reasoning") {
			entry.ContentPartial = true
			if openItems[key(act.Native.TurnID, act.Native.ItemID)] > 1 {
				entry.Activity.Issues = appendUnique(entry.Activity.Issues, "ambiguous_pair")
				entry.ContentUnavailable = true
			}
			if err := r.transcriptDeltas(ctx, &entry, &sourceBudget); err != nil {
				return page, err
			}
		}
		if len(entry.Blocks) == 0 && (entry.Role == "user" || entry.Role == "assistant" || entry.Role == "reasoning") {
			entry.ContentUnavailable = true
		}
		page.Entries = append(page.Entries, entry)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		return page, err
	}
	if len(encoded) > 4<<20 {
		return page, fmt.Errorf("%w: transcript page exceeds 4 MiB", ErrProjectionLimit)
	}
	return page, nil
}

func transcriptVisible(a observation.Activity, s Step) bool {
	if a.Type == "run" {
		return false
	}
	switch a.Type {
	case "run/input", "run/steer", "userMessage", "agentMessage", "reasoning", "fastReply", "modelCall", "commandExecution", "mcpToolCall", "dynamicToolCall", "collabAgentToolCall", "fileChange", "webSearch", "imageView", "imageGeneration", "toolCall", "toolReuse", "toolReplay", "mcpExchange", "approval", "approval/expired", "error", "warning", "configWarning", "kun/mcp.failed", "kun/run.paused", "thread/compacted", "contextCompaction", "fastFallback":
		return true
	case "run/state", "turn/completed", "kun/run.finished":
		return s.Status == "failed"
	case "kun/control.queued":
		return str(obj(s.Payload["command"])["operation"]) == "steer"
	}
	return a.Kind == "transport"
}
func (r *Reader) transcriptEvent(ctx context.Context, id int64, budget *int) (map[string]any, bool, error) {
	var size int
	if err := r.db.QueryRowContext(ctx, `SELECT length(CAST(data AS BLOB)) FROM events WHERE session=? AND id=? AND id<=?`, r.SessionID, id, r.Through).Scan(&size); err != nil {
		return nil, false, err
	}
	if size > 8<<20 || size > *budget {
		return nil, false, nil
	}
	*budget -= size
	var raw []byte
	if err := r.db.QueryRowContext(ctx, `SELECT data FROM events WHERE session=? AND id=? AND id<=?`, r.SessionID, id, r.Through).Scan(&raw); err != nil {
		return nil, false, err
	}
	var v any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		return nil, false, err
	}
	return obj(redaction.Fields(v)), true, nil
}

func transcriptFingerprint(kind string, p map[string]any) string {
	text := ""
	switch kind {
	case "run/input", "run/steer":
		text = str(obj(p["input"])["text"])
	case "userMessage":
		content, ok := p["content"].([]any)
		if !ok {
			return ""
		}
		for _, part := range content {
			v, ok := part.(map[string]any)
			if !ok || (str(v["type"]) != "text" && str(v["type"]) != "inputText") {
				return ""
			}
		}
		text = transcriptText(content)
	}
	if text == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
}

// End records may omit nested call arguments supplied by the start record.
func transcriptMerge(dst, src map[string]any) {
	for k, v := range src {
		child, ok := v.(map[string]any)
		if old, exists := dst[k].(map[string]any); ok && exists {
			transcriptMerge(old, child)
		} else {
			dst[k] = v
		}
	}
}
func transcriptText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := []string{}
		for _, item := range x {
			if s := transcriptText(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if s := str(x["text"]); s != "" {
			return s
		}
		if s := str(x["content"]); s != "" {
			return s
		}
	}
	return ""
}
func (e *TranscriptEntry) add(kind, text, label, path string) {
	if text == "" && path == "" {
		return
	}
	remaining := transcriptEntryCharacters
	for _, b := range e.Blocks {
		remaining -= utf8.RuneCountInString(b.Text) + utf8.RuneCountInString(b.Path)
	}
	if remaining <= 0 || len(e.Blocks) >= 32 {
		e.ContentTruncated = true
		return
	}
	if n := utf8.RuneCountInString(path); n > 1024 {
		path = string([]rune(path)[:1024])
		e.ContentTruncated = true
	}
	remaining -= utf8.RuneCountInString(path)
	if remaining < 0 {
		e.ContentTruncated = true
		return
	}
	if utf8.RuneCountInString(text) > remaining {
		text = string([]rune(text)[:remaining])
		e.ContentTruncated = true
	}
	language := ""
	if kind == "json" {
		language = "json"
	}
	if kind == "diff" {
		language = "diff"
	}
	e.Blocks = append(e.Blocks, TranscriptBlock{Kind: kind, Text: text, Label: label, Path: path, Language: language})
}
func (e *TranscriptEntry) structured(v any, label string) {
	if v == nil {
		return
	}
	if s, ok := v.(string); ok {
		// Arguments are often a JSON string inside an otherwise structured event.
		// Redact that JSON too before presenting it as structured arguments.
		var parsed any
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if json.Valid([]byte(s)) && dec.Decode(&parsed) == nil {
			v = redaction.Fields(parsed)
		} else {
			e.add("code", s, label, "")
			return
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		e.add("json", string(b), label, "")
	}
}
func transcriptContent(e *TranscriptEntry, p map[string]any) {
	switch e.Activity.Type {
	case "run/input", "run/steer":
		e.Role = "user"
		e.Title = "用户"
		if e.Activity.Type == "run/steer" {
			e.Title = "补充指令"
		}
		in := obj(p["input"])
		e.add("text", str(in["text"]), "", "")
		if files, ok := in["files"].([]any); ok {
			for _, f := range files {
				if path := str(f); path != "" {
					e.add("file", "", "附件", path)
				}
			}
		}
	case "kun/control.queued":
		e.Role = "user"
		e.Title = "补充指令"
		e.add("text", str(obj(p["command"])["text"]), "", "")
	case "userMessage":
		e.Role = "user"
		e.Title = "用户"
		e.add("text", transcriptText(p["content"]), "", "")
		if parts, ok := p["content"].([]any); ok {
			for _, part := range parts {
				v := obj(part)
				kind := str(v["type"])
				if kind == "text" || kind == "inputText" {
					continue
				}
				path := first(str(v["path"]), str(v["url"]))
				if path != "" && !strings.HasPrefix(path, "data:") {
					e.add("file", "", "附件", path)
				} else {
					e.add("text", "此消息包含附加内容，请查看来源事件。", "附件", "")
				}
			}
		}
	case "agentMessage":
		e.Role = "assistant"
		e.Title = "助手"
		if str(p["phase"]) == "commentary" {
			e.Title = "阶段说明"
		}
		e.add("text", first(str(p["text"]), transcriptText(p["content"])), "", "")
	case "reasoning":
		e.Role = "reasoning"
		e.Title = "可见推理"
		e.add("text", first(transcriptText(p["summary"]), transcriptText(p["content"]), str(p["text"])), "", "")
	case "modelCall", "fastReply":
		text := transcriptText(obj(p["message"])["content"])
		e.Title = "模型调用"
		if str(p["status"]) == "rejected" {
			e.Role = "system"
			e.Title = "被拒绝的模型回复"
		} else if str(p["purpose"]) == "plan" {
			e.Role = "reasoning"
			e.Title = "显式规划"
		} else if text != "" {
			e.Role = "assistant"
			e.Title = "助手"
		}
		e.add("text", text, "", "")
		if hasError(p["error"]) {
			e.structured(p["error"], "错误")
		}
	case "commandExecution":
		e.Role = "tool"
		e.Title = first(str(p["command"]), "执行命令")
		e.add("code", str(p["command"]), "命令", "")
		e.add("code", str(p["aggregatedOutput"]), "结果", "")
	case "fileChange":
		e.Role = "tool"
		e.Title = "文件变更"
		if changes, ok := p["changes"].([]any); ok {
			for _, v := range changes {
				c := obj(v)
				path := str(c["path"])
				diff := first(str(c["diff"]), str(c["patch"]))
				if diff != "" {
					e.add("diff", diff, "变更", path)
				} else {
					e.add("file", "", "变更文件", path)
				}
			}
		}
	case "collabAgentToolCall":
		e.Role = "tool"
		e.Title = "子 Agent · " + first(str(p["tool"]), "协作调用")
		e.add("text", str(p["prompt"]), "指令", "")
		e.structured(p["receiverThreadIds"], "接收线程")
		e.structured(p["agentsStates"], "状态")
	case "mcpToolCall", "dynamicToolCall":
		e.Role = "tool"
		e.Title = first(str(p["tool"]), "工具调用")
		if server := str(p["server"]); server != "" {
			e.Title = server + " / " + e.Title
		}
		e.structured(p["arguments"], "参数")
		e.structured(p["result"], "结果")
		e.structured(p["contentItems"], "结果")
		e.structured(p["error"], "错误")
	case "toolCall", "toolReuse", "toolReplay":
		e.Role = "tool"
		fn := obj(obj(p["call"])["function"])
		e.Title = first(str(fn["name"]), "工具调用")
		if e.Activity.Type == "toolReplay" {
			e.Title = "历史结果 · " + e.Title
		}
		if e.Activity.Type == "toolReuse" {
			e.Title = "复用结果 · " + e.Title
		}
		e.structured(fn["arguments"], "参数")
		if output, ok := p["output"].(string); ok {
			e.add("code", output, "结果", "")
		} else {
			e.structured(p["output"], "结果")
		}
		e.structured(p["error"], "错误")
	case "mcpExchange":
		e.Role = "tool"
		e.Title = first(str(p["server"]), "MCP") + " / " + str(p["method"])
		e.structured(p["params"], "参数")
		e.structured(p["result"], "结果")
		e.structured(p["error"], "错误")
	case "webSearch":
		e.Role = "tool"
		e.Title = "搜索"
		e.structured(p["action"], "搜索")
		e.add("text", str(p["query"]), "", "")
	case "imageView", "imageGeneration":
		e.Role = "tool"
		e.Title = "图像"
		e.add("file", "", "图像文件", first(str(p["path"]), str(p["outputPath"])))
		e.add("text", str(p["prompt"]), "", "")
	case "approval":
		e.Title = "审批"
		if p["request"] != nil {
			e.structured(obj(p["request"])["params"], "请求")
		} else {
			e.structured(p, "请求")
		}
		if p["decision"] != nil {
			e.structured(p["decision"], "决定")
		}
	default:
		e.Title = map[string]string{"warning": "运行提醒", "configWarning": "配置提醒", "error": "运行错误", "kun/mcp.failed": "MCP 错误", "run/state": "运行错误", "turn/completed": "运行错误", "kun/run.finished": "运行错误", "kun/run.paused": "运行已暂停", "thread/compacted": "上下文已压缩", "contextCompaction": "上下文压缩", "fastFallback": "快速路径回退", "approval/expired": "审批已失效"}[e.Activity.Type]
		if e.Title == "" {
			e.Title = e.Activity.Title
		}
		if p["error"] != nil {
			e.structured(p["error"], "错误")
		}
		params := obj(p["params"])
		if len(params) > 0 {
			e.structured(params, "")
		} else {
			e.add("text", first(str(p["message"]), str(p["reason"]), str(p["phase"])), "", "")
		}
	}
	e.Title = shortText(e.Title, 256)
}

// Only merge retained deltas into a uniquely identified, still-open item. Deltas
// do not establish completion and cannot cross native turns or the fixed cursor.
func (r *Reader) transcriptDeltas(ctx context.Context, e *TranscriptEntry, budget *int) error {
	n := e.Activity.Native
	if n.TurnID == "" || n.ItemID == "" {
		return nil
	}
	for _, issue := range e.Activity.Issues {
		if issue == "ambiguous_pair" {
			return nil
		}
	}
	method := "item/agentMessage/delta"
	if e.Activity.Type == "reasoning" {
		method = "item/reasoning/summaryTextDelta"
	}
	firstID := int64(0)
	if len(e.Activity.Evidence) > 0 {
		firstID = e.Activity.Evidence[0].EventID
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,CASE WHEN json_valid(data) THEN substr(json_extract(data,'$.params.delta'),1,12001) END,length(CAST(data AS BLOB)) FROM events WHERE session=? AND id>? AND id<=? AND method=? AND direction='in' AND CASE WHEN json_valid(data) THEN json_extract(data,'$.params.turnId') END=? AND CASE WHEN json_valid(data) THEN json_extract(data,'$.params.itemId') END=? ORDER BY id LIMIT 10001`, r.SessionID, firstID, r.Through, method, n.TurnID, n.ItemID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var text strings.Builder
	chars, count := 0, 0
	for rows.Next() {
		var id int64
		var delta *string
		var size int
		if err = rows.Scan(&id, &delta, &size); err != nil {
			return err
		}
		count++
		if count > 10000 || size > *budget {
			e.ContentTruncated = true
			break
		}
		*budget -= size
		if delta == nil {
			continue
		}
		value := str(redaction.Fields(*delta))
		runes := []rune(value)
		remaining := transcriptEntryCharacters - chars
		if len(runes) > remaining {
			runes = runes[:remaining]
			e.ContentTruncated = true
		}
		text.WriteString(string(runes))
		chars += len(runes)
		if len(e.Activity.Evidence) < 16 {
			e.Activity.Evidence = append(e.Activity.Evidence, observation.Evidence{EventID: id, ReadWith: "event"})
		} else {
			e.Activity.EvidenceTruncated = true
		}
		if chars >= transcriptEntryCharacters {
			e.ContentTruncated = true
			break
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if text.Len() > 0 {
		initial := ""
		for _, block := range e.Blocks {
			if block.Kind == "text" {
				initial += block.Text
			}
		}
		e.Blocks = []TranscriptBlock{}
		e.add("text", initial+text.String(), "", "")
		e.ContentPartial = true
	}
	return nil
}
