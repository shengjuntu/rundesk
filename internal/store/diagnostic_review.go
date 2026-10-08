package store

import "encoding/json"

// DiagnosticReviews returns a bounded newest-first audit history for one proposal.
func (s *Store) DiagnosticReviews(prefix string) ([]json.RawMessage, error) {
	rows, err := s.db.Query("SELECT data FROM objects WHERE kind='diagnostic_review' AND id>=? AND id<? ORDER BY rowid DESC LIMIT 20", prefix, prefix+"\uffff")
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

// DiagnosticControlReceipt reads only imported worker receipts, never starts a worker.
func (s *Store) DiagnosticControlReceipt(session, requestID string) (Event, error) {
	v := Event{SessionID: session}
	err := s.db.QueryRow(`SELECT id,time,direction,method,data FROM events WHERE session=? AND method IN ('kun/control.queued','kun/control.applied','kun/control.rejected') AND json_extract(data,'$.data.command.requestId')=? ORDER BY id DESC LIMIT 1`, session, requestID).Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Data)
	return v, err
}
