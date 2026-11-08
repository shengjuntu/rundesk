package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestTracePagingPreviewAndSessionBoundary(t *testing.T) {
	m := testManager(t)
	a, _ := m.CreateSession(m.Workspaces()[0].ID, "trace", "")
	b, _ := m.CreateSession(m.Workspaces()[0].ID, "other", "")
	input, _ := m.Store.Add(a.ID, "internal", "run/input", map[string]any{"runId": "r1", "input": map[string]any{"text": "hello"}})
	for i := 0; i < 15; i++ {
		m.Store.Add(a.ID, "in", "item/agentMessage/delta", map[string]any{"delta": "noise"})
	}
	complete, _ := m.Store.Add(a.ID, "in", "item/completed", map[string]any{"params": map[string]any{"turnId": "t1", "item": map[string]any{"id": "i1", "type": "commandExecution", "status": "failed", "exitCode": 1, "aggregatedOutput": strings.Repeat("中文", 10000)}}})
	h := NewHandler(m, "", true)
	get := func(path string) (int, []byte) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost"+path, nil))
		return w.Code, w.Body.Bytes()
	}
	type page struct {
		Events   []store.Event `json:"events"`
		Cursor   int64         `json:"nextCursor"`
		More     bool          `json:"hasMore"`
		Snapshot int64         `json:"snapshot"`
	}
	code, body := get("/api/sessions/" + a.ID + "/trace?limit=1")
	var first page
	if code != 200 || json.Unmarshal(body, &first) != nil || !first.More || len(first.Events) != 1 || first.Events[0].ID != input.ID {
		t.Fatalf("bad first page %d %s", code, body)
	}
	newer, _ := m.Store.Add(a.ID, "internal", "run/state", map[string]string{"status": "completed"})
	code, body = get(fmt.Sprintf("/api/sessions/%s/trace?after=%d&through=%d&limit=1", a.ID, first.Cursor, first.Snapshot))
	var second page
	if code != 200 || json.Unmarshal(body, &second) != nil || second.More || second.Events[0].ID != complete.ID || second.Cursor != first.Snapshot {
		t.Fatalf("unstable snapshot %s", body)
	}
	if len(second.Events[0].Data) > 18000 || !strings.Contains(string(second.Events[0].Data), "i1") || !strings.Contains(string(second.Events[0].Data), "预览已截断") {
		t.Fatal("preview lost identity or not bounded")
	}
	code, body = get(fmt.Sprintf("/api/sessions/%s/trace?after=%d", a.ID, second.Cursor))
	var third page
	if code != 200 || json.Unmarshal(body, &third) != nil || len(third.Events) != 1 || third.Events[0].ID != newer.ID {
		t.Fatal("incremental update lost")
	}
	code, body = get(fmt.Sprintf("/api/sessions/%s/events/%d", a.ID, complete.ID))
	if code != 200 || !strings.Contains(string(body), strings.Repeat("中文", 10000)) {
		t.Fatal("source event was truncated")
	}
	code, _ = get(fmt.Sprintf("/api/sessions/%s/events/%d", b.ID, complete.ID))
	if code != 404 {
		t.Fatal("cross-session event exposed")
	}
	code, _ = get("/api/sessions/" + a.ID + "/trace?after=-1")
	if code != 400 {
		t.Fatal("negative cursor accepted")
	}
	// A delta-only tail still advances the cursor, preventing endless rereads.
	tail, _ := m.Store.Add(a.ID, "in", "item/agentMessage/delta", map[string]string{"delta": "tail"})
	_, body = get(fmt.Sprintf("/api/sessions/%s/trace?after=%d", a.ID, newer.ID))
	var end page
	if json.Unmarshal(body, &end) != nil || len(end.Events) != 0 || end.Cursor != tail.ID {
		t.Fatal("cursor stuck on deltas")
	}
}
