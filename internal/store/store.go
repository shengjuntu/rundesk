package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

type Store struct{ db *sql.DB }
type Event struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"sessionId"`
	Time      string          `json:"time"`
	Direction string          `json:"direction"`
	Method    string          `json:"method"`
	Data      json.RawMessage `json:"data"`
}

func ID() string {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func Open(path string) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS objects(kind TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL, PRIMARY KEY(kind,id));
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,session TEXT NOT NULL,time TEXT NOT NULL,direction TEXT NOT NULL,method TEXT NOT NULL,data BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS events_session ON events(session,id);
 CREATE INDEX IF NOT EXISTS events_method_id ON events(method,id);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(kind, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", kind, id, b)
	return e
}
func (s *Store) Get(kind, id string, v any) error {
	var b []byte
	e := s.db.QueryRow("SELECT data FROM objects WHERE kind=? AND id=?", kind, id).Scan(&b)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *Store) List(kind string) ([]json.RawMessage, error) {
	rows, e := s.db.Query("SELECT data FROM objects WHERE kind=? ORDER BY id", kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func (s *Store) Add(session, direction, method string, v any) (Event, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return Event{}, e
	}
	ev := Event{SessionID: session, Time: Now(), Direction: direction, Method: method, Data: b}
	r, e := s.db.Exec("INSERT INTO events(session,time,direction,method,data) VALUES(?,?,?,?,?)", session, ev.Time, direction, method, b)
	if e != nil {
		return ev, fmt.Errorf("persist event: %w", e)
	}
	ev.ID, e = r.LastInsertId()
	return ev, e
}
func (s *Store) Events(session string, after int64, limit int) ([]Event, error) {
	return s.QueryEvents(session, after, limit, EventFilter{})
}

type EventFilter struct {
	Direction string
	Method    string
	Query     string
	From      string
	To        string
	Category  string
}

func (s *Store) QueryEvents(session string, after int64, limit int, f EventFilter) ([]Event, error) {
	if limit < 1 || limit > 1000 {
		limit = 250
	}
	query := "SELECT id,time,direction,method,data FROM events WHERE session=? AND id>?"
	args := []any{session, after}
	if f.Direction != "" {
		query += " AND direction=?"
		args = append(args, f.Direction)
	}
	if f.Method != "" {
		query += " AND instr(method,?)>0"
		args = append(args, f.Method)
	}
	if f.Query != "" {
		query += " AND instr(lower(CAST(data AS TEXT)),lower(?))>0"
		args = append(args, f.Query)
	}
	if f.From != "" {
		query += " AND julianday(time)>=julianday(?)"
		args = append(args, f.From)
	}
	if f.To != "" {
		query += " AND julianday(time)<=julianday(?)"
		args = append(args, f.To)
	}
	switch f.Category {
	case "tools":
		query += " AND (instr(method,'commandExecution')>0 OR instr(method,'fileChange')>0 OR instr(method,'mcp')>0 OR instr(method,'/tool/')>0 OR json_extract(data,'$.params.item.type') IN ('commandExecution','mcpToolCall','fileChange','dynamicToolCall'))"
	case "approvals":
		query += " AND (instr(method,'Approval')>0 OR instr(method,'approval/')>0 OR instr(method,'requestUserInput')>0 OR instr(method,'elicitation')>0)"
	case "errors":
		query += " AND (method='process/stderr' OR instr(lower(method),'error')>0 OR coalesce(json_extract(data,'$.error'),'')!='' OR coalesce(json_extract(data,'$.params.turn.error'),'')!='')"
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit)
	rows, e := s.db.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		v := Event{SessionID: session}
		if e = rows.Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Data); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSession(id string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM events WHERE session=?", "DELETE FROM objects WHERE kind='message-feedback' AND json_extract(data,'$.sessionId')=?", "DELETE FROM objects WHERE kind='steer' AND json_extract(data,'$.sessionId')=?", "DELETE FROM objects WHERE kind='approval' AND json_extract(data,'$.sessionId')=?", "DELETE FROM objects WHERE kind='recovery-plan' AND json_extract(data,'$.sessionId')=?", "DELETE FROM objects WHERE kind='runtime' AND id=?", "DELETE FROM objects WHERE kind='session' AND id=?"} {
		if _, e = tx.Exec(q, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (f EventFilter) Validate() error {
	if len(f.Query) > 500 || len(f.Method) > 150 {
		return fmt.Errorf("筛选文本过长")
	}
	if f.Direction != "" && !strings.Contains("|in|out|internal|stderr|", "|"+f.Direction+"|") {
		return fmt.Errorf("无效方向")
	}
	if f.Category != "" && f.Category != "tools" && f.Category != "approvals" && f.Category != "errors" {
		return fmt.Errorf("无效事件分类")
	}
	for _, v := range []string{f.From, f.To} {
		if v != "" {
			if _, e := time.Parse(time.RFC3339Nano, v); e != nil {
				return fmt.Errorf("时间需要 RFC3339 格式")
			}
		}
	}
	if f.From != "" && f.To != "" {
		a, _ := time.Parse(time.RFC3339Nano, f.From)
		b, _ := time.Parse(time.RFC3339Nano, f.To)
		if a.After(b) {
			return fmt.Errorf("开始时间不能晚于结束时间")
		}
	}
	return nil
}

func (s *Store) LatestEvent(session, method string) (Event, error) {
	v := Event{SessionID: session}
	e := s.db.QueryRow("SELECT id,time,direction,method,data FROM events WHERE session=? AND method=? ORDER BY id DESC LIMIT 1", session, method).Scan(&v.ID, &v.Time, &v.Direction, &v.Method, &v.Data)
	return v, e
}

func (s *Store) DeleteObject(kind, id string) error {
	_, e := s.db.Exec("DELETE FROM objects WHERE kind=? AND id=?", kind, id)
	return e
}
