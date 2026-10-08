package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrKunCompareLimit = errors.New("comparison exceeds 4096 events, 32 MiB total or 8 MiB per event")

// KunCompareEvents freezes a host cursor in a read transaction. Worker sequence
// bounds belong to this exact session/run; host cursors are never worker IDs.
// Measure before reading payloads and reject rather than silently truncate.
func (s *Store) KunCompareEvents(ctx context.Context, sid, run string, through *int64, afterSequence, throughSequence int64) (int64, []Event, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	var upper int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(id),0) FROM events WHERE session=?", sid).Scan(&upper); err != nil {
		return 0, nil, err
	}
	if through != nil {
		if *through < 0 || *through > upper {
			return 0, nil, fmt.Errorf("invalid comparison host cursor")
		}
		upper = *through
	}
	const where = ` FROM events WHERE session=? AND id<=? AND (?='' OR json_extract(data,'$.runId')=?) AND substr(method,1,4)='kun/' AND json_extract(data,'$.sequence')>? AND (?=0 OR json_extract(data,'$.sequence')<=?)`
	args := []any{sid, upper, run, run, afterSequence, throughSequence, throughSequence}
	var count, total, largest int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(CAST(data AS BLOB))),0),coalesce(max(length(CAST(data AS BLOB))),0)`+where, args...).Scan(&count, &total, &largest); err != nil {
		return 0, nil, err
	}
	if count > 4096 || total > 32<<20 || largest > 8<<20 {
		return 0, nil, ErrKunCompareLimit
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,time,direction,method,data`+where+` ORDER BY id`, args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		v := Event{SessionID: sid}
		if err = rows.Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Data); err != nil {
			return 0, nil, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return upper, out, nil
}
