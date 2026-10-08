package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestKunCompareBoundsAndCursors(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(sid, run string, seq int, text string) Event {
		e, err := s.Add(sid, "in", "kun/model.completed", map[string]any{"runId": run, "sequence": seq, "text": text})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	first := add("s", "run", 4, "old")
	add("other", "run", 5, "other session")
	last := add("s", "run", 5, "selected")
	add("s", "next", 6, "next run")
	upper, rows, err := s.KunCompareEvents(context.Background(), "s", "run", &last.ID, 4, 5)
	if err != nil || upper != last.ID || len(rows) != 1 || rows[0].ID != last.ID {
		t.Fatal(upper, rows, err)
	}
	_, rows, err = s.KunCompareEvents(context.Background(), "s", "run", &first.ID, 4, 5)
	if err != nil || len(rows) != 0 {
		t.Fatal("host and worker cursors mixed", rows, err)
	}
	zero := int64(0)
	_, rows, err = s.KunCompareEvents(context.Background(), "s", "", &zero, 0, 0)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	big := add("s", "run", 7, strings.Repeat("x", 8<<20))
	if _, _, err = s.KunCompareEvents(context.Background(), "s", "run", &big.ID, 0, 7); !errors.Is(err, ErrKunCompareLimit) {
		t.Fatal("oversized payload accepted", err)
	}
	if _, _, err = s.KunCompareEvents(context.Background(), "s", "run", &last.ID, 4, 5); err != nil {
		t.Fatal("future payload affected fixed scope", err)
	}
	_, err = s.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<4097) INSERT INTO events(session,time,direction,method,data) SELECT 'many','now','in','kun/model.started',json_object('runId','r','sequence',x) FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.KunCompareEvents(context.Background(), "many", "r", nil, 0, 0); !errors.Is(err, ErrKunCompareLimit) {
		t.Fatal("event count unbounded", err)
	}
}
