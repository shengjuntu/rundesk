package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TraceEvents pages lifecycle events only. Deltas remain in the original journal.
func (s *Store) TraceEvents(session string, after, through int64, limit int) ([]Event, int64, error) {
	if through <= 0 {
		if err := s.db.QueryRow("SELECT coalesce(max(id),0) FROM events WHERE session=?", session).Scan(&through); err != nil {
			return nil, 0, err
		}
	}
	if limit < 1 || limit > 501 {
		return nil, 0, fmt.Errorf("invalid trace page size")
	}
	rows, err := s.db.Query(`SELECT id,time,direction,method,data FROM events WHERE session=? AND id>? AND id<=? AND (method LIKE 'kun/%' OR method IN ('run/input','run/steer','run/retry','thread/start','turn/start','turn/started','turn/completed','run/state','item/started','item/completed','approval/pending','approval/resolved','approval/expired','serverRequest/resolved','thread/tokenUsage/updated','thread/compacted','error','warning','configWarning','deprecationNotice','runtime/effective')) ORDER BY id LIMIT ?`, session, after, through, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		v := Event{SessionID: session}
		if err = rows.Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Data); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, through, rows.Err()
}

func (s *Store) Event(session string, id int64) (Event, error) {
	v := Event{SessionID: session, ID: id}
	err := s.db.QueryRow("SELECT time,direction,method,data FROM events WHERE session=? AND id=?", session, id).Scan(&v.Time, &v.Direction, &v.Method, &v.Data)
	return v, err
}

// RunHistoryEvidence returns only attribution metadata, never the prompt,
// workspace location, tool payloads or native worker state. Both reads honor
// the session's fixed journal cursor when called from a historical trace.
func (s *Store) RunHistoryEvidence(ctx context.Context, session, run string, through *int64) (Event, *Event, error) {
	var upper int64
	if err := s.db.QueryRowContext(ctx, "SELECT coalesce(max(id),0) FROM events WHERE session=?", session).Scan(&upper); err != nil {
		return Event{}, nil, err
	}
	if through != nil {
		if *through < 0 || *through > upper {
			return Event{}, nil, ErrTraceCursor
		}
		upper = *through
	}
	input := Event{SessionID: session, Method: "run/input"}
	err := s.db.QueryRowContext(ctx, `SELECT id,time,CAST(json_object(
		'execution',json_extract(data,'$.execution'), 'instanceId',json_extract(data,'$.instanceId'),
		'model',json_extract(data,'$.model'), 'instanceRevision',json_extract(data,'$.instanceRevision'),
		'declaredEngine', CASE WHEN json_extract(data,'$.execution') IS NULL THEN
			(SELECT CASE WHEN count(DISTINCT json_extract(value,'$.configuration.backend'))=1
			 THEN max(json_extract(value,'$.configuration.backend')) END FROM json_each(events.data,'$.resources.resources')) END,
		'taskId',json_extract(data,'$.taskId'), 'scope',json_extract(data,'$.resources.scope')) AS BLOB)
		FROM events WHERE session=? AND method='run/input' AND direction='internal'
		AND json_extract(data,'$.runId')=? AND id<=? ORDER BY id LIMIT 1`,
		session, run, upper).Scan(&input.ID, &input.Time, &input.Data)
	if err != nil {
		return Event{}, nil, err
	}
	native := Event{SessionID: session}
	err = s.db.QueryRowContext(ctx, `SELECT id,time,method,CAST(json_object(
		'nativeSessionId', CASE method WHEN 'run/turn' THEN json_extract(data,'$.threadId') ELSE session END,
		'nativeRunId', CASE method WHEN 'run/turn' THEN json_extract(data,'$.turnId') ELSE json_extract(data,'$.runId') END) AS BLOB)
		FROM events WHERE session=? AND id>? AND id<=? AND json_extract(data,'$.runId')=?
		AND ((method='run/turn' AND direction='internal' AND length(json_extract(data,'$.threadId'))>0 AND length(json_extract(data,'$.turnId'))>0)
		OR (method='kun/run.started' AND direction='in' AND json_extract(data,'$.sessionId')=session AND json_extract(data,'$.type')=method))
		ORDER BY id LIMIT 1`, session, input.ID, upper, run).Scan(&native.ID, &native.Time, &native.Method, &native.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return input, nil, nil
	}
	return input, &native, err
}

var ErrTraceCursor = errors.New("invalid trace cursor")
