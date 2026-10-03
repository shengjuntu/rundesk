// Package tracequery exposes typed, read-only queries over one journal snapshot.
package tracequery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Reader struct {
	db        *sql.DB
	SessionID string
	Through   int64
	runs      []Run
	steps     []Step
	loaded    bool
}
type Run struct {
	ID           string `json:"id"`
	Question     string `json:"question"`
	Status       string `json:"status"`
	Start        string `json:"start"`
	End          string `json:"end,omitempty"`
	InputEventID int64  `json:"inputEventId"`
}
type Step struct {
	ID          string         `json:"id"`
	RunID       string         `json:"runId"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Status      string         `json:"status"`
	Start       string         `json:"start,omitempty"`
	End         string         `json:"end,omitempty"`
	DurationMS  *int64         `json:"durationMs,omitempty"`
	Point       bool           `json:"point"`
	EventIDs    []int64        `json:"eventIds"`
	Payload     map[string]any `json:"preview"`
	PreviewOnly bool           `json:"previewOnly"`
	Clock       string         `json:"clock"`
}
type Args struct {
	RunID   string `json:"runId,omitempty"`
	Query   string `json:"query,omitempty"`
	Type    string `json:"type,omitempty"`
	Status  string `json:"status,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	StepID  string `json:"stepId,omitempty"`
	EventID int64  `json:"eventId,omitempty"`
}

func Open(path, session string, through int64) (*Reader, error) {
	if session == "" || through <= 0 {
		return nil, errors.New("source session and positive snapshot cursor required")
	}
	absolute, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	q := uri.Query()
	q.Set("mode", "ro")
	q.Set("_pragma", "query_only(1)")
	q.Add("_pragma", "busy_timeout(3000)")
	uri.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var exists int
	e = db.QueryRow("SELECT count(*) FROM objects WHERE kind='session' AND id=?", session).Scan(&exists)
	if e != nil || exists != 1 {
		db.Close()
		return nil, errors.New("source session is unavailable")
	}
	return &Reader{db: db, SessionID: session, Through: through}, nil
}
func (r *Reader) Close() error { return r.db.Close() }
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func clipped(v any, budget *int, depth int) any {
	if depth > 10 || *budget <= 0 {
		return "[preview truncated]"
	}
	switch x := v.(type) {
	case string:
		chars := []rune(x)
		n := len(chars)
		if n > 4096 {
			n = 4096
		}
		if n > *budget {
			n = *budget
		}
		*budget -= n
		if n < len(chars) {
			return string(chars[:n]) + " [preview truncated]"
		}
		return x
	case []any:
		out := []any{}
		for i, v := range x {
			if i >= 64 || *budget <= 0 {
				out = append(out, "[preview truncated]")
				break
			}
			out = append(out, clipped(v, budget, depth+1))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		keys := []string{}
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[k] = clipped(x[k], budget, depth+1)
		}
		return out
	default:
		return v
	}
}
func isFailure(s string) bool {
	return s == "failed" || s == "error" || s == "declined" || s == "interrupted" || s == "expired" || s == "cancelled" || s == "canceled"
}
func (r *Reader) load() error {
	if r.loaded {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, e := r.db.QueryContext(ctx, `SELECT id,time,method,data FROM events WHERE session=? AND id<=? AND method IN ('run/input','run/steer','turn/started','turn/completed','run/state','item/started','item/completed','approval/pending','approval/resolved','approval/expired','error','warning','configWarning','thread/compacted') ORDER BY id`, r.SessionID, r.Through)
	if e != nil {
		return e
	}
	defer rows.Close()
	runs := []Run{}
	steps := []Step{}
	runIndex := map[string]int{}
	turns := map[string]string{}
	indices := map[string]int{}
	current := ""
	count := 0
	for rows.Next() {
		count++
		if count > 100000 {
			return errors.New("snapshot exceeds 100,000 lifecycle records; narrow the source session")
		}
		var id int64
		var at, method string
		var raw []byte
		if e = rows.Scan(&id, &at, &method, &raw); e != nil {
			return e
		}
		var d map[string]any
		if e = json.Unmarshal(raw, &d); e != nil {
			return e
		}
		p := obj(d["params"])
		if method == "run/input" {
			current = str(d["runId"])
			if current == "" {
				current = fmt.Sprint("observed-", id)
			}
			question := []rune(str(obj(d["input"])["text"]))
			if len(question) > 2000 {
				question = append(question[:2000], []rune(" [preview truncated]")...)
			}
			runs = append(runs, Run{ID: current, Question: string(question), Status: "running", Start: at, InputEventID: id})
			runIndex[current] = len(runs) - 1
		}
		tid := str(p["turnId"])
		if tid == "" {
			tid = str(obj(p["turn"])["id"])
		}
		rid := str(d["runId"])
		if rid == "" {
			rid = turns[tid]
		}
		if rid == "" {
			rid = current
		}
		if rid == "" {
			rid = "observed"
			runs = append(runs, Run{ID: rid, Question: "已有原生事件", Status: "unknown", Start: at})
			runIndex[rid] = len(runs) - 1
			current = rid
		}
		if tid != "" {
			turns[tid] = rid
		}
		ri, known := runIndex[rid]
		if !known {
			return errors.New("journal run references are incomplete")
		}
		if method == "turn/completed" || method == "run/state" {
			status := str(obj(p["turn"])["status"])
			if method == "run/state" {
				status = str(d["status"])
			}
			if status == "" {
				status = "unknown"
			}
			runs[ri].Status = status
			runs[ri].End = at
			if d["error"] == nil && obj(p["turn"])["error"] == nil {
				continue
			}
		}
		if method == "turn/started" {
			continue
		}
		item := obj(p["item"])
		kind := str(item["type"])
		title := kind
		key := ""
		status := "completed"
		start, end := at, at
		payload := d
		switch method {
		case "item/started", "item/completed":
			key = rid + ":" + tid + ":" + str(item["id"])
			payload = item
			switch kind {
			case "commandExecution":
				title = str(item["command"])
			case "mcpToolCall":
				title = str(item["server"]) + " / " + str(item["tool"])
			case "webSearch":
				title = "Web search"
			case "agentMessage":
				title = "Assistant reply"
			case "reasoning":
				title = "Public reasoning summary"
			}
			if method == "item/started" {
				status = "running"
				end = ""
			} else {
				start = ""
				if v := str(item["status"]); v != "" {
					status = v
				}
				code, hasCode := item["exitCode"].(float64)
				if hasCode && code != 0 || item["success"] == false || item["error"] != nil || obj(item["result"])["isError"] == true {
					status = "failed"
				}
			}
		case "approval/pending":
			key = "approval:" + str(d["id"])
			kind = "approval"
			title = "Approval"
			status = "pending"
			end = ""
		case "approval/resolved":
			key = "approval:" + str(d["id"])
			kind = "approval"
			title = "Approval"
			status = "accepted"
			start = ""
			if d["decision"] == "decline" || d["decision"] == "cancel" {
				status = "declined"
			}
		default:
			kind = method
			title = method
			if method == "error" {
				status = "failed"
			}
			if method == "warning" || method == "configWarning" {
				status = "warning"
			}
		}
		if key == "" {
			key = fmt.Sprint("event:", id)
		}
		index, exists := indices[key]
		if !exists {
			index = len(steps)
			indices[key] = index
			point := method != "item/started" && method != "item/completed" && kind != "approval"
			steps = append(steps, Step{ID: fmt.Sprint("step-", id), RunID: rid, Type: kind, Title: title, Start: start, Point: point, PreviewOnly: true, Clock: "journal receive time; durations require both lifecycle boundaries", Payload: map[string]any{}})
		}
		s := &steps[index]
		if title != "" && title != " / " {
			s.Title = title
		}
		s.Status = status
		s.End = end
		s.EventIDs = append(s.EventIDs, id)
		if len(s.EventIDs) > 16 {
			s.EventIDs = append(s.EventIDs[:1], s.EventIDs[len(s.EventIDs)-15:]...)
		}
		budget := 8192
		preview := obj(clipped(payload, &budget, 0))
		for k, v := range preview {
			s.Payload[k] = v
		}
		if s.Start != "" && s.End != "" && !s.Point {
			a, x := time.Parse(time.RFC3339Nano, s.Start)
			b, y := time.Parse(time.RFC3339Nano, s.End)
			if x == nil && y == nil && !b.Before(a) {
				n := b.Sub(a).Milliseconds()
				s.DurationMS = &n
			}
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	for i := range steps {
		s := &steps[i]
		if (s.Status == "running" || s.Status == "pending") && runs[runIndex[s.RunID]].Status != "running" {
			s.Status = "unknown"
		}
	}
	r.runs = runs
	r.steps = steps
	r.loaded = true
	return nil
}
func (r *Reader) Runs() ([]Run, error) {
	if e := r.load(); e != nil {
		return nil, e
	}
	return r.runs, nil
}
func (r *Reader) Call(name string, a Args) (any, error) {
	if a.Offset < 0 || a.Limit < 0 || a.Limit > 20000 || len(a.Query) > 1000 {
		return nil, errors.New("invalid query limits")
	}
	if name == "trace_read_event" {
		var raw []byte
		var method, at string
		e := r.db.QueryRow("SELECT data,method,time FROM events WHERE session=? AND id=? AND id<=?", r.SessionID, a.EventID, r.Through).Scan(&raw, &method, &at)
		if e != nil {
			return nil, errors.New("event is outside the source snapshot or unavailable")
		}
		chars := []rune(string(raw))
		if a.Offset > len(chars) {
			return nil, errors.New("offset exceeds event size")
		}
		limit := a.Limit
		if limit == 0 || limit > 16000 {
			limit = 16000
		}
		end := a.Offset + limit
		if end > len(chars) {
			end = len(chars)
		}
		return map[string]any{"eventId": a.EventID, "method": method, "time": at, "jsonChunk": string(chars[a.Offset:end]), "offset": a.Offset, "nextOffset": end, "totalCharacters": len(chars), "hasMore": end < len(chars), "sessionId": r.SessionID, "snapshotThrough": r.Through}, nil
	}
	if e := r.load(); e != nil {
		return nil, e
	}
	if a.RunID != "" {
		exists := false
		for _, run := range r.runs {
			if run.ID == a.RunID {
				exists = true
			}
		}
		if !exists {
			return nil, errors.New("run is outside source snapshot")
		}
	}
	if name == "trace_get_step" {
		for _, s := range r.steps {
			if s.ID == a.StepID {
				return s, nil
			}
		}
		return nil, errors.New("step not found in source snapshot")
	}
	if name == "trace_list_runs" {
		offset := a.Offset
		if offset > len(r.runs) {
			offset = len(r.runs)
		}
		end := offset + 50
		if end > len(r.runs) {
			end = len(r.runs)
		}
		return map[string]any{"sessionId": r.SessionID, "snapshotThrough": r.Through, "runs": r.runs[offset:end], "total": len(r.runs), "nextOffset": end, "hasMore": end < len(r.runs)}, nil
	}
	if name != "trace_find_steps" && name != "trace_statistics" {
		return nil, errors.New("unknown read-only tool")
	}
	found := []Step{}
	for _, s := range r.steps {
		if a.RunID != "" && s.RunID != a.RunID || a.Type != "" && s.Type != a.Type || a.Status != "" && s.Status != a.Status {
			continue
		}
		b, _ := json.Marshal(s)
		if a.Query != "" && !strings.Contains(strings.ToLower(string(b)), strings.ToLower(a.Query)) {
			continue
		}
		found = append(found, s)
	}
	if name == "trace_statistics" {
		statuses, types := map[string]int{}, map[string]int{}
		var sum int64
		complete, unknown, issues := 0, 0, 0
		for _, s := range found {
			statuses[s.Status]++
			types[s.Type]++
			if isFailure(s.Status) || s.Status == "warning" || s.Status == "pending" {
				issues++
			}
			if s.DurationMS != nil {
				sum += *s.DurationMS
				complete++
			} else if !s.Point {
				unknown++
			}
		}
		return map[string]any{"steps": len(found), "byStatus": statuses, "byType": types, "attentionSteps": issues, "completeDurationSteps": complete, "withoutCompleteDuration": unknown, "sumRecordedDurationsMs": sum, "durationNote": "Journal receive times. Sum of step durations is not wall-clock runtime; parallel steps may overlap. Missing boundaries are not zero-duration steps.", "outcomeNote": "Tool completion is not proof that the user's goal was achieved."}, nil
	}
	offset := a.Offset
	if offset > len(found) {
		offset = len(found)
	}
	limit := a.Limit
	if limit == 0 || limit > 50 {
		limit = 50
	}
	end := offset + limit
	if end > len(found) {
		end = len(found)
	}
	return map[string]any{"steps": found[offset:end], "total": len(found), "nextOffset": end, "hasMore": end < len(found), "previewOnly": true, "note": "Search matches recorded previews. Use trace_read_event for full output; not finding text in previews does not prove absence."}, nil
}
