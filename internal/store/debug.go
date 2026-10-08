package store

import "fmt"

// DebugEventMeta deliberately excludes the payload, so listing a page cannot
// allocate hundreds of large model responses or tool outputs.
type DebugEventMeta struct {
	ID        int64  `json:"id"`
	Time      string `json:"time"`
	Direction string `json:"direction"`
	Method    string `json:"method"`
	Bytes     int64  `json:"bytes"`
}
type DebugEventPage struct {
	Events     []DebugEventMeta `json:"events"`
	Through    int64            `json:"through"`
	NextCursor int64            `json:"nextCursor"`
	HasMore    bool             `json:"hasMore"`
}

func (s *Store) DebugEvents(session string, after int64, through *int64, limit int) (DebugEventPage, error) {
	out := DebugEventPage{Events: []DebugEventMeta{}, NextCursor: after}
	if limit < 1 || limit > 200 || after < 0 {
		return out, fmt.Errorf("invalid debug page")
	}
	var latest int64
	if err := s.db.QueryRow("SELECT coalesce(max(id),0) FROM events WHERE session=?", session).Scan(&latest); err != nil {
		return out, err
	}
	out.Through = latest
	if through != nil {
		if *through < 0 || *through > latest || after > *through {
			return out, fmt.Errorf("invalid debug cursor")
		}
		out.Through = *through
	}
	rows, err := s.db.Query("SELECT id,time,direction,method,length(CAST(data AS BLOB)) FROM events WHERE session=? AND id>? AND id<=? ORDER BY id LIMIT ?", session, after, out.Through, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v DebugEventMeta
		if err = rows.Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Bytes); err != nil {
			return out, err
		}
		if len(out.Events) == limit {
			out.HasMore = true
			break
		}
		out.Events = append(out.Events, v)
		out.NextCursor = v.ID
	}
	return out, rows.Err()
}

// Payload size is measured before retrieval. The service redacts the complete
// JSON value before any character slicing, including secrets spanning chunks.
func (s *Store) DebugEventSize(session string, id int64) (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT length(CAST(data AS BLOB)) FROM events WHERE session=? AND id=?", session, id).Scan(&n)
	return n, err
}
