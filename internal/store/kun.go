package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

func (s *Store) KunCursor(session string) (int64, error) {
	var cursor int64
	e := s.Get("kun-cursor", session, &cursor)
	if e == sql.ErrNoRows {
		return 0, nil
	}
	return cursor, e
}

// ImportKun commits event, business projection and cursor together. Workers own execution state.
func (s *Store) ImportKun(session string, seq int64, at, method string, data any, records ...Record) (bool, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var raw []byte
	var cursor int64
	e = tx.QueryRow("SELECT data FROM objects WHERE kind='kun-cursor' AND id=?", session).Scan(&raw)
	if e != nil && e != sql.ErrNoRows {
		return false, e
	}
	if e == nil {
		if e = json.Unmarshal(raw, &cursor); e != nil {
			return false, e
		}
	}
	if seq <= cursor {
		return false, nil
	}
	if seq != cursor+1 {
		return false, fmt.Errorf("Kun event gap: want %d got %d", cursor+1, seq)
	}
	b, e := json.Marshal(data)
	if e != nil {
		return false, e
	}
	if _, e = tx.Exec("INSERT INTO events(session,time,direction,method,data) VALUES(?,?,?,?,?)", session, at, "in", method, b); e != nil {
		return false, e
	}
	records = append(records, Record{Kind: "kun-cursor", ID: session, Value: seq})
	for _, r := range records {
		b, e = json.Marshal(r.Value)
		if e != nil {
			return false, e
		}
		if _, e = tx.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", r.Kind, r.ID, b); e != nil {
			return false, e
		}
	}
	return true, tx.Commit()
}
