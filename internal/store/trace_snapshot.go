package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// ExportTraceSnapshot never copies other sessions, credentials or application metadata.
func (s *Store) ExportTraceSnapshot(path, session string, through int64) error {
	if _, e := os.Stat(path); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	var count, total, largest int64
	if err := s.db.QueryRow(`SELECT count(*),coalesce(sum(length(CAST(data AS BLOB))),0),coalesce(max(length(CAST(data AS BLOB))),0) FROM events WHERE session=? AND id<=?`, session, through).Scan(&count, &total, &largest); err != nil {
		return err
	}
	if count > 100000 || total > 128<<20 || largest > 8<<20 {
		return fmt.Errorf("trace snapshot exceeds 100000 events, 128 MiB total or 8 MiB per event")
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".trace-*.db")
	if e != nil {
		return e
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	db, e := sql.Open("sqlite", tmp)
	if e != nil {
		return e
	}
	defer db.Close()
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE objects(kind TEXT,id TEXT,data BLOB,PRIMARY KEY(kind,id));CREATE TABLE events(id INTEGER PRIMARY KEY,session TEXT,time TEXT,direction TEXT,method TEXT,data BLOB);CREATE INDEX events_session ON events(session,id);`); e != nil {
		return e
	}
	var data []byte
	if e = s.db.QueryRow("SELECT data FROM objects WHERE kind='session' AND id=?", session).Scan(&data); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO objects VALUES('session',?,?)", session, data); e != nil {
		return e
	}
	rows, e := s.db.Query("SELECT id,time,direction,method,data FROM events WHERE session=? AND id<=? ORDER BY id", session, through)
	if e != nil {
		return e
	}
	defer rows.Close()
	stmt, e := tx.Prepare("INSERT INTO events VALUES(?,?,?,?,?,?)")
	if e != nil {
		return e
	}
	defer stmt.Close()
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Data); e != nil {
			return e
		}
		if _, e = stmt.Exec(v.ID, session, v.Time, v.Direction, v.Method, []byte(v.Data)); e != nil {
			return e
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if e = db.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
