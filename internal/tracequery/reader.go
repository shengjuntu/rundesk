// Package tracequery exposes typed, read-only queries over one journal snapshot.
package tracequery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/shengjuntu/rundesk/internal/redaction"
	_ "modernc.org/sqlite"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
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
	ID           string  `json:"id"`
	Question     string  `json:"question"`
	Status       string  `json:"status"`
	Start        string  `json:"start"`
	End          string  `json:"end,omitempty"`
	InputEventID int64   `json:"inputEventId"`
	Association  string  `json:"association"`
	EventIDs     []int64 `json:"eventIds"`
}
type Step struct {
	ID                 string         `json:"id"`
	RunID              string         `json:"runId"`
	Type               string         `json:"type"`
	Title              string         `json:"title"`
	Status             string         `json:"status"`
	Start              string         `json:"start,omitempty"`
	End                string         `json:"end,omitempty"`
	DurationMS         *int64         `json:"durationMs,omitempty"`
	Point              bool           `json:"point"`
	EventIDs           []int64        `json:"eventIds"`
	Payload            map[string]any `json:"preview"`
	PreviewOnly        bool           `json:"previewOnly"`
	Clock              string         `json:"clock"`
	Association        string         `json:"association"`
	StartObserved      bool           `json:"startObserved"`
	EndObserved        bool           `json:"endObserved"`
	Missing            []string       `json:"missing"`
	Issues             []string       `json:"issues"`
	PreviewTruncated   bool           `json:"previewTruncated"`
	EvidenceTruncated  bool           `json:"evidenceTruncated"`
	WorkerSequences    []int64        `json:"workerSequences,omitempty"`
	ReportedDurationMS *int64         `json:"reportedDurationMs,omitempty"`
}
type Args struct {
	Proposal *ProposalInput `json:"proposal,omitempty"`
	RunID    string         `json:"runId,omitempty"`
	Query    string         `json:"query,omitempty"`
	Type     string         `json:"type,omitempty"`
	Status   string         `json:"status,omitempty"`
	Offset   int            `json:"offset,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	StepID   string         `json:"stepId,omitempty"`
	EventID  int64          `json:"eventId,omitempty"`
}

func Open(path, session string, through int64) (*Reader, error) {
	return OpenContext(context.Background(), path, session, through)
}
func OpenContext(ctx context.Context, path, session string, through int64) (*Reader, error) {
	if session == "" || through < 0 {
		return nil, errors.New("source session and nonnegative fixed snapshot cursor required")
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
	e = db.QueryRowContext(ctx, "SELECT count(*) FROM objects WHERE kind='session' AND id=?", session).Scan(&exists)
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
func isFailure(s string) bool {
	return s == "failed" || s == "error" || s == "declined" || s == "interrupted" || s == "expired" || s == "cancelled" || s == "canceled"
}

// The same fixed-snapshot projector serves legacy trace tools and DebugService.
func (r *Reader) load(ctx context.Context) error {
	if r.loaded {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var count, total, largest int64
	where := ` FROM events WHERE session=? AND id<=? AND (` + lifecycleSQL + `)`
	if err := r.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(CAST(data AS BLOB))),0),coalesce(max(length(CAST(data AS BLOB))),0)`+where, r.SessionID, r.Through).Scan(&count, &total, &largest); err != nil {
		return err
	}
	if count > 20000 || total > 64<<20 || largest > 8<<20 {
		return ErrProjectionLimit
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,time,method,data`+where+` ORDER BY id`, r.SessionID, r.Through)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := newProjection()
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var id int64
		var at, method string
		var raw []byte
		if err = rows.Scan(&id, &at, &method, &raw); err != nil {
			return err
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err = decoder.Decode(&value); err != nil {
			return errors.New("invalid retained lifecycle JSON")
		}
		index.ingest(id, at, method, obj(redaction.Fields(value)))
	}
	if err = rows.Err(); err != nil {
		return err
	}
	index.finish()
	r.runs = index.runs
	r.steps = index.steps
	r.loaded = true
	return nil
}
func (r *Reader) Runs() ([]Run, error) {
	if e := r.load(context.Background()); e != nil {
		return nil, e
	}
	return r.runs, nil
}
func (r *Reader) Call(name string, a Args) (any, error) {
	return r.CallContext(context.Background(), name, a)
}
func (r *Reader) CallContext(ctx context.Context, name string, a Args) (any, error) {
	if a.Offset < 0 || a.Limit < 0 || a.Limit > 20000 || utf8.RuneCountInString(a.Query) > 1000 {
		return nil, errors.New("invalid query limits")
	}
	if name == "trace_read_event" {
		var raw []byte
		var method, at string
		var size int64
		if e := r.db.QueryRowContext(ctx, "SELECT length(CAST(data AS BLOB)) FROM events WHERE session=? AND id=? AND id<=?", r.SessionID, a.EventID, r.Through).Scan(&size); e != nil {
			return nil, errors.New("event is outside the source snapshot or unavailable")
		}
		if size > 8<<20 {
			return nil, ErrProjectionLimit
		}
		e := r.db.QueryRowContext(ctx, "SELECT data,method,time FROM events WHERE session=? AND id=? AND id<=?", r.SessionID, a.EventID, r.Through).Scan(&raw, &method, &at)
		if e != nil {
			return nil, errors.New("event is outside the source snapshot or unavailable")
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if e = decoder.Decode(&value); e != nil {
			return nil, e
		}
		raw, e = json.Marshal(redaction.Fields(value))
		if e != nil {
			return nil, e
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
	if e := r.load(ctx); e != nil {
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
	if name == "trace_propose" {
		return r.propose(ctx, a.RunID, a.Proposal)
	}
	if name == "trace_get_step" {
		for _, s := range r.steps {
			if s.ID == a.StepID && (a.RunID == "" || a.RunID == s.RunID) {
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
		limit := a.Limit
		if limit == 0 || limit > 50 {
			limit = 50
		}
		end := offset + limit
		if end > len(r.runs) {
			end = len(r.runs)
		}
		return map[string]any{"sessionId": r.SessionID, "snapshotThrough": r.Through, "projectionVersion": 2, "runs": r.runs[offset:end], "total": len(r.runs), "nextOffset": end, "hasMore": end < len(r.runs)}, nil
	}
	if name != "trace_find_steps" && name != "trace_statistics" && name != "trace_find_issues" {
		return nil, errors.New("unknown read-only tool")
	}
	found := []Step{}
	for _, s := range r.steps {
		if name == "trace_find_issues" && len(s.Issues) == 0 {
			continue
		}
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
			if len(s.Issues) > 0 {
				issues++
			}
			if s.DurationMS != nil {
				sum += *s.DurationMS
				complete++
			} else if !s.Point {
				unknown++
			}
		}
		return map[string]any{"steps": len(found), "byStatus": statuses, "byType": types, "attentionSteps": issues, "completeDurationSteps": complete, "withoutCompleteDuration": unknown, "sumRecordedDurationsMs": sum, "sessionId": r.SessionID, "snapshotThrough": r.Through, "projectionVersion": 2, "durationNote": "Host receive or Kun worker event times, as labeled on each step. Sum of step durations is not wall-clock runtime; parallel steps may overlap. Missing boundaries are not zero-duration steps.", "outcomeNote": "Tool completion is not proof that the user's goal was achieved."}, nil
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
	return map[string]any{"steps": found[offset:end], "total": len(found), "nextOffset": end, "hasMore": end < len(found), "previewOnly": true, "sessionId": r.SessionID, "snapshotThrough": r.Through, "projectionVersion": 2, "note": "Search matches recorded previews. Use trace_read_event for full output; not finding text in previews does not prove absence."}, nil
}
