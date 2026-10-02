package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
)

func (s *Store) PutMessageFeedback(session string, eventID int64, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	// One statement makes deletion vs. feedback atomic: no orphan can be
	// inserted after the source event is deleted, even with concurrent clients.
	r, err := s.db.Exec(`INSERT INTO objects(kind,id,data)
	 SELECT 'message-feedback',?,? WHERE EXISTS(SELECT 1 FROM events WHERE session=? AND id=?)
	 ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data`, session+":"+strconv.FormatInt(eventID, 10), b, session, eventID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) MessageFeedback(session string) ([]json.RawMessage, error) {
	rows, err := s.db.Query("SELECT data FROM objects WHERE kind='message-feedback' AND json_extract(data,'$.sessionId')=? ORDER BY id", session)
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
