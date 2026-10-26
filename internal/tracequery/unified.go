package tracequery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/observation"
)

type UnifiedArgs struct {
	RunID  string `json:"runId,omitempty"`
	Source string `json:"source,omitempty"`
	Kind   string `json:"recordKind,omitempty"`
	After  int64  `json:"after,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (a UnifiedArgs) Validate(kind string) error {
	if kind != "observations" && kind != "activity" {
		return errors.New("unsupported unified query")
	}
	if a.After < 0 || a.Offset < 0 || a.Limit < 0 || a.Limit > 200 || utf8.RuneCountInString(a.RunID) > 256 {
		return errors.New("invalid unified selectors; limit must be at most 200")
	}
	if kind == "observations" && a.Offset != 0 || kind == "activity" && a.After != 0 {
		return errors.New("observations use after; activity uses offset")
	}
	switch a.Source {
	case "", observation.Host, observation.CodexAppServer, observation.KunSDK, observation.ObserveProxy:
	default:
		return errors.New("unknown observation source")
	}
	switch a.Kind {
	case "", "run", "model", "tool", "mcp", "approval", "message", "reasoning", "context", "usage", "transport", "error", "event":
	default:
		return errors.New("unknown observation kind")
	}
	return nil
}

const unifiedCoverage = "Retained journal evidence only. Runtime model steps and transport attempts are distinct; run membership does not prove call identity. Absence does not prove no activity or upstream request."

func (r *Reader) Unified(ctx context.Context, kind string, a UnifiedArgs) (any, error) {
	if err := a.Validate(kind); err != nil {
		return nil, err
	}
	if a.After > r.Through {
		return nil, errors.New("after exceeds the fixed snapshot")
	}
	if a.Limit == 0 {
		a.Limit = 50
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	index, err := r.unifiedIndex(ctx)
	if err != nil {
		return nil, err
	}
	var result any
	if kind == "observations" {
		result, err = r.unifiedEvents(ctx, index, a)
	} else {
		result, err = r.unifiedActivity(ctx, index, a)
	}
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 4<<20 {
		return nil, fmt.Errorf("%w: unified page exceeds 4 MiB", ErrProjectionLimit)
	}
	return result, nil
}

// Only explicitly selected scalar metadata crosses the SQL boundary. No
// request/response body, tool arguments, output, headers or URL is selected,
// including for unrecognized runtime events with arbitrarily large payloads.
func unifiedScalar(path string) string {
	v := `json_extract(data,'` + path + `')`
	return `CASE WHEN json_type(data,'` + path + `') IN ('text','integer') AND length(CAST(` + v + ` AS TEXT))<=256 THEN CAST(` + v + ` AS TEXT) END`
}
func unifiedNumber(path string) string {
	return `CASE WHEN json_type(data,'` + path + `')='integer' THEN json_extract(data,'` + path + `') ELSE 0 END`
}

var unifiedMetadata = func() string {
	fields := []string{}
	for _, f := range [][2]string{
		{"run", "$.runId"}, {"task", "$.taskId"}, {"rpc", "$.id"},
		{"turn", "$.params.turnId"}, {"turnObject", "$.params.turn.id"}, {"hostTurn", "$.turnId"},
		{"thread", "$.params.threadId"}, {"threadObject", "$.params.thread.id"},
		{"item", "$.params.item.id"}, {"itemType", "$.params.item.type"}, {"itemRef", "$.params.itemId"},
		{"status", "$.status"}, {"turnStatus", "$.params.turn.status"}, {"itemStatus", "$.params.item.status"}, {"workerStatus", "$.data.status"},
		{"call", "$.data.call.id"}, {"callRef", "$.data.callId"}, {"purpose", "$.data.purpose"}, {"server", "$.data.server"}, {"exchange", "$.data.exchangeId"},
		{"operation", "$.data.command.operation"}, {"commandCall", "$.data.command.callId"}, {"decision", "$.decision"},
		{"labelRun", "$.labels.runId"}, {"labelThread", "$.labels.threadId"}, {"labelTurn", "$.labels.turnId"}, {"capture", "$.labels.captureId"},
		{"started", "$.started"}, {"ended", "$.ended"}, {"httpMethod", "$.request.method"},
	} {
		fields = append(fields, "'"+f[0]+"',"+unifiedScalar(f[1]))
	}
	for _, f := range [][2]string{
		{"sequence", "$.sequence"}, {"revision", "$.revision"}, {"step", "$.data.step"},
		{"statusCode", "$.response.status"}, {"requestBytes", "$.request.body.size"}, {"responseBytes", "$.response.body.size"},
	} {
		fields = append(fields, "'"+f[0]+"',"+unifiedNumber(f[1]))
	}
	fields = append(fields,
		`'websocket',json_type(data,'$.websocket')='object'`,
		`'incomplete',json_extract(data,'$.incomplete')=1`,
		`'truncated',json_extract(data,'$.request.body.truncated')=1 OR json_extract(data,'$.response.body.truncated')=1 OR json_extract(data,'$.websocket.toUpstream.truncated')=1 OR json_extract(data,'$.websocket.toClient.truncated')=1`,
		`'captureLoss',coalesce(length(json_extract(data,'$.captureLoss')),0)>0`,
		`'headersRedacted',coalesce(json_array_length(data,'$.request.redactedHeaders'),0)>0 OR coalesce(json_array_length(data,'$.response.redactedHeaders'),0)>0`,
		`'error',json_type(data,'$.error') NOT IN ('null','false') AND coalesce(json_extract(data,'$.error'),'')<>''`,
		`'workerError',json_type(data,'$.data.error') NOT IN ('null','false') AND coalesce(json_extract(data,'$.data.error'),'')<>''`,
		`'usage',json_type(data,'$.data.usage')='object' OR json_type(data,'$.data.tokenUsage')='object'`,
		`'bodyComplete',json_extract(data,'$.request.body.complete')=1 AND json_extract(data,'$.response.body.complete')=1`,
		`'duplexComplete',json_extract(data,'$.websocket.toUpstream.complete')=1 AND json_extract(data,'$.websocket.toClient.complete')=1`,
		`'itemFailure',(json_type(data,'$.params.item.exitCode')='integer' AND json_extract(data,'$.params.item.exitCode')<>0) OR json_type(data,'$.params.item.success')='false' OR (json_type(data,'$.params.item.error') NOT IN ('null','false') AND coalesce(json_extract(data,'$.params.item.error'),'')<>'') OR json_extract(data,'$.params.item.result.isError')=1`,
		`'workerFailure',json_extract(data,'$.data.isError')=1 OR json_extract(data,'$.data.result.isError')=1`,
		`'recordedReplay',json_extract(data,'$.data.replay.mode')='recorded' AND json_type(data,'$.data.replay.executed')='false'`,
		`'reusedResult',json_extract(data,'$.data.status')='reused' AND json_type(data,'$.data.reusedFrom')='text' AND length(json_extract(data,'$.data.reusedFrom'))>0`,
	)
	return `CASE WHEN json_valid(data,6) THEN json_object(` + strings.Join(fields, ",") + `) ELSE '{"invalid":1}' END`
}()

// OFFSET is an intentional SQLite flattening barrier. The inner coroutine
// parses each retained JSON value into JSONB once; scalar extracts reuse that
// value. This streams rows instead of materializing all large transport bodies.
func unifiedSelect(scope string) string {
	return `SELECT id,time,method,direction,` + unifiedMetadata + ` FROM (SELECT id,time,method,direction,CASE WHEN json_valid(data) THEN jsonb(data) ELSE data END AS data` + scope + ` ORDER BY id LIMIT -1 OFFSET 0) AS metadata ORDER BY id`
}

const unifiedSourceSQL = `CASE WHEN method='llm/exchange' THEN 'observeproxy' WHEN method LIKE 'kun/%' THEN 'kun_sdk' WHEN direction IN ('in','out','stderr') THEN 'codex_app_server' ELSE 'host' END`

type unifiedMeta struct {
	ID                      int64
	Time, Method, Direction string
	Fields                  map[string]any
}

func (m unifiedMeta) s(k string) string { return str(m.Fields[k]) }
func (m unifiedMeta) n(k string) int64  { n, _ := integer(m.Fields[k]); return n }
func (m unifiedMeta) b(k string) bool   { return m.n(k) == 1 }
func first(values ...string) string {
	for _, s := range values {
		if s != "" {
			return s
		}
	}
	return ""
}
func (m unifiedMeta) source() (string, string) {
	if m.Method == observationMethod {
		return observation.ObserveProxy, "journal_method"
	}
	if strings.HasPrefix(m.Method, "kun/") {
		return observation.KunSDK, "journal_method"
	}
	if m.Direction == "in" || m.Direction == "out" || m.Direction == "stderr" {
		return observation.CodexAppServer, "rpc_direction"
	}
	if m.Direction == "internal" {
		return observation.Host, "internal_direction"
	}
	return observation.Host, "unclassified_host_event"
}
func (m unifiedMeta) native() observation.NativeIDs {
	source, _ := m.source()
	n := observation.NativeIDs{}
	switch source {
	case observation.CodexAppServer:
		n.RPCID = m.s("rpc")
		n.ThreadID = first(m.s("thread"), m.s("threadObject"))
		n.TurnID = first(m.s("turn"), m.s("turnObject"))
		n.ItemID = first(m.s("item"), m.s("itemRef"))
		n.ItemType = m.s("itemType")
	case observation.KunSDK:
		n.WorkerSequence = m.n("sequence")
		n.WorkerRevision = m.n("revision")
		n.Step = m.n("step")
		n.Purpose = m.s("purpose")
		n.CallID = first(m.s("call"), m.s("callRef"), m.s("commandCall"))
		n.MCPServer = m.s("server")
		n.MCPExchangeID = m.s("exchange")
	case observation.ObserveProxy:
		n.CaptureID = m.s("capture")
		n.TransportID = m.s("rpc")
		if !m.b("websocket") {
			n.ThreadID = m.s("labelThread")
			n.TurnID = m.s("labelTurn")
		}
	default:
		n.TurnID = m.s("hostTurn")
	}
	return n
}
func unifiedKind(method, itemType string) string {
	switch {
	case method == observationMethod:
		return "transport"
	case method == "thread/tokenUsage/updated":
		return "usage"
	case strings.HasPrefix(method, "kun/model."):
		return "model"
	case strings.HasPrefix(method, "kun/tool."):
		return "tool"
	case strings.HasPrefix(method, "kun/mcp."):
		return "mcp"
	case strings.HasPrefix(method, "approval/") || strings.HasPrefix(method, "kun/approval."):
		return "approval"
	case strings.HasPrefix(method, "run/") || strings.HasPrefix(method, "kun/run.") || method == "turn/started" || method == "turn/completed":
		return "run"
	case strings.HasPrefix(method, "kun/context.") || method == "thread/compacted":
		return "context"
	case method == "error" || method == "kun/mcp.failed":
		return "error"
	case strings.HasPrefix(method, "item/"):
		switch itemType {
		case "reasoning":
			return "reasoning"
		case "agentMessage", "userMessage":
			return "message"
		case "mcpToolCall":
			return "mcp"
		case "commandExecution", "fileChange", "dynamicToolCall", "webSearch", "imageGeneration", "collabAgentToolCall":
			return "tool"
		}
		if strings.HasPrefix(method, "item/agentMessage/") {
			return "message"
		}
		if strings.HasPrefix(method, "item/reasoning/") {
			return "reasoning"
		}
	}
	return "event"
}

// The identity-only projector uses the existing run/turn resolution rules, with
// scalar metadata rather than payloads. It never consults mutable task/session
// objects and therefore cannot import identities from beyond the fixed cursor.
type unifiedIdentity struct{ Run, Association string }
type unifiedIndex struct {
	Events        map[int64]unifiedMeta
	Identity      map[int64]unifiedIdentity
	Turns         map[string]string
	Tasks         map[string]string
	TaskConflicts map[string]bool
	RunEventCount map[string]int
	RunClock      map[string]string
}

func (m unifiedMeta) projectionData() map[string]any {
	d := map[string]any{"runId": m.s("run"), "id": m.s("rpc"), "sequence": json.Number(fmt.Sprint(m.n("sequence"))), "status": m.s("status"), "decision": m.s("decision")}
	d["params"] = map[string]any{"turnId": m.s("turn"), "turn": map[string]any{"id": m.s("turnObject"), "status": m.s("turnStatus")}, "item": map[string]any{"id": m.s("item"), "type": m.s("itemType"), "status": m.s("itemStatus")}}
	d["data"] = map[string]any{"step": json.Number(fmt.Sprint(m.n("step"))), "purpose": m.s("purpose"), "call": map[string]any{"id": m.s("call")}, "callId": m.s("callRef"), "server": m.s("server"), "exchangeId": json.Number(m.s("exchange")), "status": m.s("workerStatus"), "command": map[string]any{"callId": m.s("commandCall"), "operation": m.s("operation")}}
	return d
}
func (r *Reader) unifiedIndex(ctx context.Context) (*unifiedIndex, error) {
	x := &unifiedIndex{Events: map[int64]unifiedMeta{}, Identity: map[int64]unifiedIdentity{}, Tasks: map[string]string{}, TaskConflicts: map[string]bool{}, RunEventCount: map[string]int{}, RunClock: map[string]string{}}
	var count int
	where := ` FROM events WHERE session=? AND id<=? AND (` + lifecycleSQL + `)`
	if err := r.db.QueryRowContext(ctx, `SELECT count(*)`+where, r.SessionID, r.Through).Scan(&count); err != nil {
		return nil, err
	}
	if count > 20000 {
		return nil, ErrProjectionLimit
	}
	rows, err := r.db.QueryContext(ctx, unifiedSelect(where), r.SessionID, r.Through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p := newProjection()
	for rows.Next() {
		m, err := scanUnified(rows)
		if err != nil {
			return nil, err
		}
		d := m.projectionData()
		rid, association, _ := p.resolve(m.ID, m.Method, d, obj(d["params"]))
		p.ingest(m.ID, m.Time, m.Method, d)
		x.Events[m.ID] = m
		x.Identity[m.ID] = unifiedIdentity{rid, association}
		x.RunEventCount[rid]++
		clock := "host_journal"
		if strings.HasPrefix(m.Method, "kun/") {
			clock = "kun_worker"
		}
		if old := x.RunClock[rid]; old != "" && old != clock {
			x.RunClock[rid] = "mixed_host_and_worker"
		} else {
			x.RunClock[rid] = clock
		}
		if m.Method == "run/input" && m.s("run") != "" && m.s("task") != "" {
			run, task := m.s("run"), m.s("task")
			if previous := x.Tasks[run]; previous != "" && previous != task {
				x.TaskConflicts[run] = true
			}
			if !x.TaskConflicts[run] {
				x.Tasks[run] = task
			} else {
				delete(x.Tasks, run)
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	x.Turns = p.turns
	return x, nil
}

type unifiedScanner interface{ Scan(...any) error }

func scanUnified(row unifiedScanner) (unifiedMeta, error) {
	m := unifiedMeta{}
	var raw []byte
	if err := row.Scan(&m.ID, &m.Time, &m.Method, &m.Direction, &raw); err != nil {
		return m, err
	}
	fields, err := observationDecode(raw)
	m.Fields = fields
	return m, err
}
func (x *unifiedIndex) identity(m unifiedMeta) (string, string, string) {
	source, _ := m.source()
	if source == observation.ObserveProxy {
		if m.b("websocket") {
			return "", "missing", "connection_can_span_runs"
		}
		if run := m.s("labelRun"); run != "" {
			return run, "observed", "request_start_labels"
		}
		return "", "missing", "request_has_no_run_label"
	}
	if run := m.s("run"); run != "" {
		return run, "observed", "native_run_id"
	}
	if v, ok := x.Identity[m.ID]; ok {
		return v.Run, v.Association, "lifecycle_projection"
	}
	if source == observation.CodexAppServer {
		if turn := first(m.s("turn"), m.s("turnObject")); turn != "" {
			if run := x.Turns[turn]; run != "" {
				return run, "reconstructed", "native_turn_id"
			}
		}
	}
	return "", "missing", "no_run_evidence"
}
func (x *unifiedIndex) event(session string, m unifiedMeta) observation.Event {
	source, basis := m.source()
	run, association, associationBasis := x.identity(m)
	e := observation.Event{ID: fmt.Sprint("event-", m.ID), EventID: m.ID, SessionID: session, TaskID: x.Tasks[run], RunID: run, Source: source, SourceBasis: basis, Kind: unifiedKind(m.Method, m.s("itemType")), Method: shortText(m.Method, 256), Direction: m.Direction, Time: m.Time, Clock: "host_journal", Phase: "point", Status: "recorded", Association: association, AssociationBasis: associationBasis, Native: m.native(), Issues: []string{}}
	e.Evidence = []observation.Evidence{{EventID: m.ID, ReadWith: "event", WorkerSequence: e.Native.WorkerSequence}}
	if source == observation.KunSDK {
		e.Clock = "kun_worker"
	}
	e.Phase, e.Status = unifiedEventState(m)
	if m.b("invalid") {
		e.Issues = append(e.Issues, "invalid_retained_json")
	}
	if x.TaskConflicts[run] {
		e.Issues = append(e.Issues, "ambiguous_task_identity")
	}
	if e.Association == "missing" {
		e.Issues = append(e.Issues, "missing_run_association")
	}
	if m.Method == "thread/tokenUsage/updated" {
		e.Usage = &observation.Usage{Scope: "thread", Aggregation: "cumulative", Evidence: e.Evidence}
	}
	if m.Method == "kun/model.completed" && m.b("usage") {
		e.Usage = &observation.Usage{Scope: "model_call", Aggregation: "delta", Evidence: e.Evidence}
	}
	if source == observation.ObserveProxy {
		e.Evidence[0].ReadWith = "llm_request"
		e.Phase = "span"
		e.Status = "unknown"
		e.Transport = &observation.Transport{Method: m.s("httpMethod"), StatusCode: m.n("statusCode"), WebSocket: m.b("websocket"), RequestBytes: m.n("requestBytes"), ResponseBytes: m.n("responseBytes"), Incomplete: m.b("incomplete"), Truncated: m.b("truncated"), CaptureLoss: m.b("captureLoss"), HeadersRedacted: m.b("headersRedacted")}
		e.Transport.Complete = m.b("bodyComplete") && (!m.b("websocket") || m.b("duplexComplete")) && !m.b("incomplete") && !m.b("truncated") && !m.b("captureLoss")
		if e.Transport.Complete && m.n("statusCode") >= 100 {
			e.Status = "completed"
		}
		if m.b("incomplete") || m.b("truncated") || m.b("captureLoss") {
			e.Status = "incomplete"
		}
		if m.b("error") || m.n("statusCode") >= 400 {
			e.Status = "failed"
		}
	}
	return e
}

// Only known protocol methods have normalized lifecycle semantics. An unknown
// extension ending in "completed" is still retained as an uninterpreted event.
func unifiedEventState(m unifiedMeta) (string, string) {
	phase, status := "point", "recorded"
	switch m.Method {
	case "turn/started", "item/started", "kun/model.started", "kun/tool.started", "kun/mcp.request", "kun/run.started":
		phase, status = "start", "running"
	case "item/completed":
		phase, status = "end", first(m.s("itemStatus"), "completed")
	case "kun/model.completed", "kun/mcp.response":
		phase, status = "end", "completed"
	case "kun/tool.completed":
		phase, status = "end", first(m.s("workerStatus"), "completed")
	case "turn/completed":
		phase, status = "end", first(m.s("turnStatus"), "unknown")
	case "kun/run.finished":
		phase, status = "end", first(m.s("workerStatus"), "unknown")
	case "run/state":
		status = first(m.s("status"), "unknown")
	case "kun/run.paused":
		status = "paused"
	case "approval/pending", "kun/approval.requested":
		phase, status = "start", "pending"
	case "approval/resolved":
		phase, status = "end", "unknown"
		switch m.s("decision") {
		case "accept", "acceptForSession", "acceptForTurn":
			status = "accepted"
		case "decline", "cancel":
			status = "declined"
		}
	case "approval/expired":
		status = "expired"
	case "kun/fast.completed":
		status = "completed"
	case "error", "kun/mcp.failed":
		status = "failed"
	case "warning", "configWarning":
		status = "warning"
	}
	if (m.Method == "kun/model.completed" || m.Method == "kun/fast.completed") && m.s("workerStatus") == "rejected" {
		status = "rejected"
	}
	if m.b("error") || m.b("workerError") || m.b("workerFailure") || m.b("itemFailure") {
		status = "failed"
	}
	if m.Method == "kun/tool.completed" {
		// Recorded failures describe an earlier result, not a newly executed
		// failing tool. Match the lifecycle projector's replay precedence.
		if m.b("recordedReplay") {
			return "point", "replayed"
		}
		if status == "reused" && m.b("reusedResult") {
			phase = "point"
		}
	}
	return phase, status
}
func unifiedMatches(a UnifiedArgs, run, source, kind string) bool {
	return (a.RunID == "" || a.RunID == run) && (a.Source == "" || a.Source == source) && (a.Kind == "" || a.Kind == kind)
}
func (r *Reader) unifiedEvents(ctx context.Context, x *unifiedIndex, a UnifiedArgs) (any, error) {
	page := observation.EventPage{SchemaVersion: observation.SchemaVersion, SessionID: r.SessionID, SnapshotThrough: r.Through, Observations: []observation.Event{}, NextAfter: a.After, Coverage: unifiedCoverage}
	query := ` FROM events WHERE session=? AND id>? AND id<=?`
	params := []any{r.SessionID, a.After, r.Through}
	if a.Source != "" {
		query += ` AND (` + unifiedSourceSQL + `)=?`
		params = append(params, a.Source)
	}
	rows, err := r.db.QueryContext(ctx, unifiedSelect(query), params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanUnified(rows)
		if err != nil {
			return nil, err
		}
		e := x.event(r.SessionID, m)
		if !unifiedMatches(a, e.RunID, e.Source, e.Kind) {
			continue
		}
		if len(page.Observations) == a.Limit {
			page.HasMore = true
			break
		}
		page.Observations = append(page.Observations, e)
		page.NextAfter = m.ID
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return page, nil
}

func activityKind(t string) string {
	switch t {
	case "modelCall":
		return "model"
	case "toolCall", "toolReuse", "toolReplay", "commandExecution", "fileChange", "dynamicToolCall", "webSearch", "imageGeneration", "collabAgentToolCall":
		return "tool"
	case "mcpExchange", "mcpToolCall":
		return "mcp"
	case "approval", "approval/expired":
		return "approval"
	case "reasoning":
		return "reasoning"
	case "agentMessage", "userMessage", "fastReply":
		return "message"
	}
	return unifiedKind(t, "")
}
func activityTitle(t string) string {
	switch t {
	case "commandExecution":
		return "Command execution"
	case "fileChange":
		return "File change"
	case "toolReplay":
		return "Recorded tool result"
	case "toolReuse":
		return "Reused tool result"
	}
	switch activityKind(t) {
	case "model":
		return "Model call"
	case "tool":
		return "Tool call"
	case "mcp":
		return "MCP exchange"
	case "message":
		return "Message"
	case "reasoning":
		return "Public reasoning"
	case "approval":
		return "Approval"
	case "run":
		return "Run event"
	case "context":
		return "Context event"
	case "error":
		return "Error"
	}
	return "Runtime event"
}
func evidenceFor(ids []int64, x *unifiedIndex) ([]observation.Evidence, bool) {
	out := []observation.Evidence{}
	truncated := len(ids) > 16
	if truncated {
		ids = append(append([]int64{}, ids[:1]...), ids[len(ids)-15:]...)
	}
	for _, id := range ids {
		m := x.Events[id]
		out = append(out, observation.Evidence{EventID: id, ReadWith: "event", WorkerSequence: m.n("sequence")})
	}
	return out, truncated
}
func (r *Reader) unifiedActivity(ctx context.Context, x *unifiedIndex, a UnifiedArgs) (any, error) {
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	all := []observation.Activity{}
	for _, run := range r.runs {
		evidence, truncated := evidenceFor(run.EventIDs, x)
		id := run.InputEventID
		if id == 0 && len(run.EventIDs) > 0 {
			id = run.EventIDs[0]
		}
		m := x.Events[id]
		e := x.event(r.SessionID, m)
		item := observation.Activity{ID: "run-" + run.ID, SessionID: r.SessionID, TaskID: x.Tasks[run.ID], RunID: run.ID, Source: observation.Host, SourceBasis: "lifecycle_projection", Kind: "run", Type: "run", Title: "Run", Status: run.Status, Start: run.Start, End: run.End, Clock: x.RunClock[run.ID], StartObserved: run.InputEventID > 0, EndObserved: run.End != "", Association: run.Association, AssociationBasis: "lifecycle_projection", Native: e.Native, Evidence: evidence, EvidenceTruncated: truncated || x.RunEventCount[run.ID] > len(evidence), Missing: []string{}, Issues: []string{}}
		if !item.StartObserved {
			item.Missing = append(item.Missing, "start")
		}
		if x.TaskConflicts[run.ID] {
			item.Issues = append(item.Issues, "ambiguous_task_identity")
		}
		if unifiedMatches(a, item.RunID, item.Source, item.Kind) {
			all = append(all, item)
		}
	}
	for _, step := range r.steps {
		id := int64(0)
		if len(step.EventIDs) > 0 {
			id = step.EventIDs[0]
		}
		m := x.Events[id]
		e := x.event(r.SessionID, m)
		evidence, truncated := evidenceFor(step.EventIDs, x)
		item := observation.Activity{ID: step.ID, SessionID: r.SessionID, TaskID: x.Tasks[step.RunID], RunID: step.RunID, Source: e.Source, SourceBasis: e.SourceBasis, Kind: activityKind(step.Type), Type: shortText(step.Type, 256), Title: activityTitle(step.Type), Status: shortText(step.Status, 256), Start: step.Start, End: step.End, Clock: e.Clock, DurationMS: step.DurationMS, ReportedDurationMS: step.ReportedDurationMS, Point: step.Point, StartObserved: step.StartObserved, EndObserved: step.EndObserved, Association: step.Association, AssociationBasis: "lifecycle_projection", Native: e.Native, Evidence: evidence, EvidenceTruncated: step.EvidenceTruncated || truncated, Missing: append([]string{}, step.Missing...), Issues: append([]string{}, step.Issues...), Usage: e.Usage}
		// Completion may carry usage which the start event does not contain.
		for _, eventID := range step.EventIDs {
			if v := x.event(r.SessionID, x.Events[eventID]); v.Usage != nil {
				item.Usage = v.Usage
			}
		}
		if x.TaskConflicts[step.RunID] {
			item.Issues = append(item.Issues, "ambiguous_task_identity")
		}
		if unifiedMatches(a, item.RunID, item.Source, item.Kind) {
			all = append(all, item)
		}
	}
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*)`+observationScope, r.SessionID, r.Through).Scan(&count); err != nil {
		return nil, err
	}
	if count > 20000 {
		return nil, fmt.Errorf("%w: more than 20000 retained transport spans", ErrProjectionLimit)
	}
	rows, err := r.db.QueryContext(ctx, unifiedSelect(observationScope), r.SessionID, r.Through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanUnified(rows)
		if err != nil {
			return nil, err
		}
		e := x.event(r.SessionID, m)
		item := observation.Activity{ID: fmt.Sprint("transport-", m.ID), SessionID: r.SessionID, TaskID: e.TaskID, RunID: e.RunID, Source: e.Source, SourceBasis: e.SourceBasis, Kind: "transport", Type: "http", Title: "Model transport", Status: e.Status, Start: m.s("started"), End: m.s("ended"), Clock: "host_transport", Association: e.Association, AssociationBasis: e.AssociationBasis, Native: e.Native, Evidence: e.Evidence, Missing: []string{}, Issues: e.Issues, Transport: e.Transport}
		item.StartObserved = item.Start != ""
		item.EndObserved = item.End != ""
		if !item.StartObserved {
			item.Missing = append(item.Missing, "start")
		}
		if !item.EndObserved {
			item.Missing = append(item.Missing, "end")
		}
		if m.b("websocket") {
			item.Type = "websocket"
		}
		start, se := time.Parse(time.RFC3339Nano, item.Start)
		end, ee := time.Parse(time.RFC3339Nano, item.End)
		if se == nil && ee == nil && !end.Before(start) {
			duration := end.Sub(start).Milliseconds()
			item.DurationMS = &duration
		} else if item.StartObserved && item.EndObserved {
			item.Issues = append(item.Issues, "invalid_clock")
		}
		if unifiedMatches(a, item.RunID, item.Source, item.Kind) {
			all = append(all, item)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		firstID := func(v observation.Activity) int64 {
			if len(v.Evidence) > 0 {
				return v.Evidence[0].EventID
			}
			return 0
		}
		x, y := firstID(all[i]), firstID(all[j])
		if x == y {
			return all[i].ID < all[j].ID
		}
		return x < y
	})
	offset := a.Offset
	if offset > len(all) {
		offset = len(all)
	}
	end := offset + a.Limit
	if end > len(all) {
		end = len(all)
	}
	return observation.ActivityPage{SchemaVersion: observation.SchemaVersion, SessionID: r.SessionID, SnapshotThrough: r.Through, Activity: all[offset:end], Total: len(all), Offset: offset, NextOffset: end, HasMore: end < len(all), Coverage: unifiedCoverage}, nil
}
