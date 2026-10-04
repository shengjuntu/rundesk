package store

import "fmt"

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
	rows, err := s.db.Query(`SELECT id,time,direction,method,data FROM events WHERE session=? AND id>? AND id<=? AND method IN ('run/input','run/steer','run/retry','thread/start','turn/start','turn/started','turn/completed','run/state','item/started','item/completed','approval/pending','approval/resolved','approval/expired','serverRequest/resolved','thread/tokenUsage/updated','thread/compacted','error','warning','configWarning','deprecationNotice','runtime/effective') ORDER BY id LIMIT ?`, session, after, through, limit)
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
