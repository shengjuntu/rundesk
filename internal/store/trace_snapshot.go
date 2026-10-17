package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ExportTraceSnapshot copies one fixed source session, without unrelated
// sessions or application metadata. Observation payloads retain their captured
// bytes and redacted headers; no reconstruction from previews is involved.
func (s *Store) ExportTraceSnapshot(path, session string, through int64) error {
	if _, e := os.Stat(path); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
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
	ctx := context.Background()
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	if _, e = conn.ExecContext(ctx, "ATTACH DATABASE ? AS trace_export", tmp); e != nil {
		return e
	}
	attached := true
	defer func() {
		if attached {
			_, _ = conn.ExecContext(ctx, "DETACH DATABASE trace_export")
		}
	}()
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var count, total, largestOrdinary int64
	if e = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(CAST(data AS BLOB))),0),coalesce(max(CASE WHEN method!='llm/exchange' THEN length(CAST(data AS BLOB)) ELSE 0 END),0) FROM main.events WHERE session=? AND id<=?`, session, through).Scan(&count, &total, &largestOrdinary); e != nil {
		return e
	}
	if count > 100000 || total > 128<<20 || largestOrdinary > 8<<20 {
		return fmt.Errorf("trace snapshot exceeds 100000 events, 128 MiB total or 8 MiB per non-observation event")
	}
	// Copy inside SQLite so an observation larger than the regular event parsing
	// bound never needs to be allocated in the Go snapshot exporter.
	if _, e = tx.ExecContext(ctx, `CREATE TABLE trace_export.objects(kind TEXT,id TEXT,data BLOB,PRIMARY KEY(kind,id));CREATE TABLE trace_export.events(id INTEGER PRIMARY KEY,session TEXT,time TEXT,direction TEXT,method TEXT,data BLOB);CREATE INDEX trace_export.events_session ON events(session,id);CREATE INDEX trace_export.events_session_method_id ON events(session,method,id);`); e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, "INSERT INTO trace_export.objects SELECT kind,id,data FROM main.objects WHERE kind='session' AND id=?", session)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("source session unavailable")
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO trace_export.events SELECT id,session,time,direction,method,data FROM main.events WHERE session=? AND id<=? ORDER BY id", session, through); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if _, e = conn.ExecContext(ctx, "DETACH DATABASE trace_export"); e != nil {
		return e
	}
	attached = false
	return os.Rename(tmp, path)
}
