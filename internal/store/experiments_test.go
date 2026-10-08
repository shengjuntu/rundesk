package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestExperimentCaptureBounds(t *testing.T) {
	t.Run("single and frozen scope", func(t *testing.T) {
		s, e := Open(filepath.Join(t.TempDir(), "state.db"))
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		first, e := s.Add("s", "in", "kun/run.started", map[string]string{"runId": "r"})
		if e != nil {
			t.Fatal(e)
		}
		big, e := s.Add("s", "in", "kun/tool.completed", map[string]string{"runId": "r", "output": strings.Repeat("x", (4<<20)+1)})
		if e != nil {
			t.Fatal(e)
		}
		rows, e := s.ExperimentEvents(context.Background(), "s", "r", first.ID)
		if e != nil || len(rows) != 1 {
			t.Fatal("future size affected capture", len(rows), e)
		}
		if _, e = s.ExperimentEvents(context.Background(), "s", "r", big.ID); e == nil {
			t.Fatal("oversized event accepted")
		}
	})
	t.Run("count", func(t *testing.T) {
		s, e := Open(filepath.Join(t.TempDir(), "state.db"))
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		_, e = s.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2001) INSERT INTO events(session,time,direction,method,data) SELECT 's','now','in','kun/run.started','{"runId":"r"}' FROM n`)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.ExperimentEvents(context.Background(), "s", "r", 2001); e == nil {
			t.Fatal("event count overflow")
		}
		rows, e := s.ExperimentEvents(context.Background(), "s", "r", 2000)
		if e != nil || len(rows) != 2000 {
			t.Fatal(len(rows), e)
		}
	})
	t.Run("total", func(t *testing.T) {
		s, e := Open(filepath.Join(t.TempDir(), "state.db"))
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		var last Event
		for n := 0; n < 17; n++ {
			last, e = s.Add("s", "in", "kun/tool.completed", map[string]string{"runId": "r", "output": strings.Repeat("x", 1<<20)})
			if e != nil {
				t.Fatal(e)
			}
		}
		if _, e = s.ExperimentEvents(context.Background(), "s", "r", last.ID); e == nil {
			t.Fatal("total size overflow")
		}
	})
}
