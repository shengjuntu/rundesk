package store

import (
	"path/filepath"
	"testing"
)

func TestHistoricalEventFilters(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows := []struct {
		dir, method, time string
		data              any
	}{
		{"in", "item/completed", "2026-09-27T10:00:00Z", map[string]any{"params": map[string]any{"item": map[string]any{"type": "mcpToolCall", "server": "vision", "tool": "crop"}}}},
		{"internal", "run/state", "2026-09-27T10:00:01Z", map[string]any{"status": "completed", "error": ""}},
		{"in", "turn/completed", "2026-09-27T10:00:02Z", map[string]any{"params": map[string]any{"turn": map[string]any{"error": nil}}}},
		{"in", "item/commandExecution/requestApproval", "2026-09-27T10:00:03Z", map[string]any{"params": map[string]string{"command": "echo 100%"}}},
		{"in", "", "2026-09-27T10:00:04Z", map[string]any{"error": map[string]any{"code": -1, "message": "broken"}}},
	}
	ids := []int64{}
	for _, row := range rows {
		ev, e := s.Add("a", row.dir, row.method, row.data)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, ev.ID)
		if _, e = s.db.Exec("UPDATE events SET time=? WHERE id=?", row.time, ev.ID); e != nil {
			t.Fatal(e)
		}
	}
	_, _ = s.Add("other", "in", "item/completed", map[string]string{"tool": "crop"})
	cases := []struct {
		filter EventFilter
		count  int
	}{{EventFilter{Category: "tools"}, 2}, {EventFilter{Category: "errors"}, 1}, {EventFilter{Category: "approvals"}, 1}, {EventFilter{Query: "100%"}, 1}, {EventFilter{Direction: "in", Query: "crop"}, 1}, {EventFilter{Method: "turn/"}, 1}, {EventFilter{From: rows[1].time, To: rows[2].time}, 2}}
	for _, c := range cases {
		events, e := s.QueryEvents("a", 0, 100, c.filter)
		if e != nil || len(events) != c.count {
			t.Fatalf("%+v: %d want %d err %v", c.filter, len(events), c.count, e)
		}
	}
	next, e := s.QueryEvents("a", ids[0], 1, EventFilter{})
	if e != nil || len(next) != 1 || next[0].ID != ids[1] {
		t.Fatal("cursor not honored")
	}
	if (EventFilter{From: "yesterday"}).Validate() == nil {
		t.Fatal("invalid time accepted")
	}
	if (EventFilter{Direction: "in' OR 1=1"}).Validate() == nil {
		t.Fatal("invalid direction accepted")
	}
}
