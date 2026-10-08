package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// Capture only records with an explicit run identity. No inferred cross-run
// association is used for an editable experiment. Measure payloads before reading.
func (s *Store) ExperimentEvents(ctx context.Context, sid, run string, through int64) ([]Event, error) {
	const where = ` FROM events WHERE session=? AND id<=? AND json_extract(data,'$.runId')=? AND (substr(method,1,4)='kun/' OR method IN ('run/input','run/state','run/steer'))`
	var count, total, largest int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(CAST(data AS BLOB))),0),coalesce(max(length(CAST(data AS BLOB))),0)`+where, sid, through, run).Scan(&count, &total, &largest); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("no explicit run records in this scope")
	}
	if count > 2000 || total > 16<<20 || largest > 4<<20 {
		return nil, fmt.Errorf("recording exceeds 2000 events, 16 MiB total or 4 MiB per event")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,time,direction,method,data`+where+` ORDER BY id`, sid, through, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		v := Event{SessionID: sid}
		var raw []byte
		if err = rows.Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &raw); err != nil {
			return nil, err
		}
		v.Data = raw
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Experiments(ctx context.Context, sid string, offset, limit int) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM objects WHERE kind='experiment' AND (?='' OR json_extract(data,'$.source.sessionId')=?) ORDER BY rowid DESC LIMIT ? OFFSET ?`, sid, sid, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
