package kun

import (
	"database/sql"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	_ "modernc.org/sqlite"
	"time"
)

type journal struct{ db *sql.DB }

func openJournal(path string) (*journal, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS state(id INTEGER PRIMARY KEY CHECK(id=1), data BLOB NOT NULL); CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,data BLOB NOT NULL,snapshot BLOB NOT NULL); CREATE TABLE IF NOT EXISTS commands(id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL,receipt BLOB NOT NULL); CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL); CREATE TABLE IF NOT EXISTS checkpoints(run_id TEXT PRIMARY KEY, seq INTEGER NOT NULL, snapshot BLOB NOT NULL, consumed_by TEXT NOT NULL DEFAULT '');")
	if e != nil {
		db.Close()
		return nil, e
	}
	return &journal{db}, nil
}
func (j *journal) load() (p.State, error) {
	var b []byte
	e := j.db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&b)
	if e == sql.ErrNoRows {
		return p.State{Schema: 1, Status: "idle", Actions: map[string]string{}}, nil
	}
	var s p.State
	if e == nil {
		e = json.Unmarshal(b, &s)
	}
	if e == nil && s.Schema != 1 {
		e = fmt.Errorf("unsupported checkpoint schema")
	}
	return s, e
}
func (j *journal) commit(s p.State, kind string, data any, commandID, fingerprint string, receipt *p.Receipt) (p.Event, error) {
	ev := p.Event{SessionID: s.SessionID, RunID: s.RunID, Revision: s.Revision, Time: time.Now().UTC().Format(time.RFC3339Nano), Type: kind, Data: p.JSON(data)}
	tx, e := j.db.Begin()
	if e != nil {
		return ev, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO state VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", []byte(p.JSON(s))); e != nil {
		return ev, e
	}
	r, e := tx.Exec("INSERT INTO events(data,snapshot) VALUES(?,?)", []byte(p.JSON(ev)), []byte(p.JSON(s)))
	if e != nil {
		return ev, e
	}
	ev.Sequence, e = r.LastInsertId()
	if e != nil {
		return ev, e
	}
	if commandID != "" {
		if receipt == nil {
			_, e = tx.Exec("INSERT INTO runs VALUES(?,?)", commandID, fingerprint)
		} else {
			_, e = tx.Exec("INSERT INTO commands VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET receipt=excluded.receipt", commandID, fingerprint, []byte(p.JSON(receipt)))
		}
		if e != nil {
			return ev, e
		}
	}
	if kind == "kun/run.started" && s.ResumedFrom != nil {
		r, err := tx.Exec("UPDATE checkpoints SET consumed_by=? WHERE run_id=? AND seq=? AND consumed_by=''", s.RunID, s.ResumedFrom.SourceRunID, s.ResumedFrom.Sequence)
		if err != nil {
			return ev, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return ev, err
		}
		if n != 1 {
			return ev, fmt.Errorf("checkpoint already consumed or changed")
		}
	}
	if p.Active(s.Status) {
		if checkpointUnsafe(s) {
			if _, e = tx.Exec("DELETE FROM checkpoints WHERE run_id=? AND consumed_by=''", s.RunID); e != nil {
				return ev, e
			}
		} else if checkpointSafe(s) {
			if _, e = tx.Exec("INSERT INTO checkpoints(run_id,seq,snapshot) VALUES(?,?,?) ON CONFLICT(run_id) DO UPDATE SET seq=excluded.seq,snapshot=excluded.snapshot WHERE consumed_by=''", s.RunID, ev.Sequence, []byte(p.JSON(s))); e != nil {
				return ev, e
			}
		}
	}
	return ev, tx.Commit()
}
func (j *journal) events(after int64, limit int) ([]p.Event, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	rows, e := j.db.Query("SELECT seq,data FROM events WHERE seq>? ORDER BY seq LIMIT ?", after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []p.Event{}
	bytesRead := 0
	for rows.Next() {
		var seq int64
		var b []byte
		var ev p.Event
		if e = rows.Scan(&seq, &b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &ev); e != nil {
			return nil, e
		}
		if bytesRead+len(b) > 4<<20 && len(out) > 0 {
			break
		}
		bytesRead += len(b)
		ev.Sequence = seq
		out = append(out, ev)
	}
	return out, rows.Err()
}
func (j *journal) snapshot(seq int64) (p.Snapshot, error) {
	var b []byte
	e := j.db.QueryRow("SELECT snapshot FROM events WHERE seq=?", seq).Scan(&b)
	v := p.Snapshot{Sequence: seq}
	if e == nil {
		e = json.Unmarshal(b, &v.State)
	}
	return v, e
}
