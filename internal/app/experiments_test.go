package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	x "github.com/shengjuntu/rundesk/internal/experiment"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestExperimentFrozenOfflineBranches(t *testing.T) {
	m := kunMCPManager(t)
	source, err := m.CreateSession(m.Workspaces()[0].ID, strings.Repeat("来源", 250), "")
	if err != nil {
		t.Fatal(err)
	}
	add := func(sid, run, kind string, seq int64, data any) store.Event {
		v, e := m.Store.Add(sid, "in", kind, p.Event{SessionID: sid, RunID: run, Type: kind, Sequence: seq, Data: p.JSON(data)})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	add(source.ID, "selected", "kun/run.started", 1, map[string]any{"apiKey": "SECRET-MUST-NOT-COPY", "largeNumber": json.Number("9007199254740993")})
	add(source.ID, "selected", "kun/model.started", 2, map[string]any{"request": "original"})
	tool := add(source.ID, "selected", "kun/tool.completed", 3, map[string]any{"call": p.ToolCall{ID: "c", Function: p.Function{Name: "read_file"}}, "status": "succeeded", "output": "original observation"})
	add(source.ID, "selected", "kun/model.completed", 4, map[string]any{"message": p.Message{Role: "assistant", Content: "original answer"}})
	last := add(source.ID, "selected", "kun/run.finished", 5, map[string]any{"status": "completed"})
	future := add(source.ID, "selected", "kun/tool.completed", 6, map[string]any{"output": "FUTURE-NOT-COPIED"})
	add(source.ID, "different", "kun/run.started", 7, map[string]any{"text": "OTHER-RUN-NOT-COPIED"})
	other, _ := m.CreateSession(source.WorkspaceID, "other", "")
	foreign := add(other.ID, "selected", "kun/run.started", 1, map[string]any{"text": "OTHER-SESSION-NOT-COPIED"})
	before, _ := m.Store.Events(source.ID, 0, 1000)
	sessionBefore, _ := m.Session(source.ID)
	h := NewHandler(m, userTestAdmin, true)
	call := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		raw := ""
		if body != nil {
			raw = string(p.JSON(body))
		}
		return appRequest(h, method, "/api/v1"+path, raw, userTestAdmin, key)
	}
	input := ExperimentInput{SessionID: source.ID, RunID: "selected", Through: &last.ID, Title: "baseline"}
	r := call("POST", "/experiments", input, "")
	if r.Code != 400 {
		t.Fatal("missing idempotency key", r.Code)
	}
	create := func() x.Branch {
		r := call("POST", "/experiments", input, "experiment-root-0001")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var b x.Branch
		json.Unmarshal(r.Body.Bytes(), &b)
		return b
	}
	root := create()
	if root.Source.Title != strings.Repeat("来源", 60) {
		t.Fatal("source display title was not bounded")
	}
	replay := create()
	if root.ID != replay.ID || root.EventCount != 5 {
		t.Fatal(root, replay)
	}
	r = call("POST", "/experiments", map[string]any{"sessionId": source.ID, "runId": "selected", "title": "bad", "mode": "live"}, "experiment-no-live")
	if r.Code != 400 {
		t.Fatal("live requested", r.Code)
	}
	zero := int64(0)
	bad := input
	bad.Through = &zero
	if _, err = m.CreateExperiment(context.Background(), bad); err == nil {
		t.Fatal("zero cursor advanced")
	}
	beyond := future.ID + 100
	bad.Through = &beyond
	if _, err = m.CreateExperiment(context.Background(), bad); err == nil {
		t.Fatal("future cursor accepted")
	}
	var pack x.Bundle
	if err = m.Store.Get("experiment_bundle", root.ID, &pack); err != nil {
		t.Fatal(err)
	}
	raw := string(p.JSON(pack))
	for _, secret := range []string{"SECRET-MUST-NOT-COPY", "FUTURE-NOT-COPIED", "OTHER-RUN-NOT-COPIED", "OTHER-SESSION-NOT-COPIED"} {
		if strings.Contains(raw, secret) {
			t.Fatal("capture leaked", secret)
		}
	}
	if !strings.Contains(raw, "9007199254740993") {
		t.Fatal("integer precision lost")
	}
	childInput := ExperimentForkInput{Title: "edited", ExpectedParentHash: root.ContentHash, Change: x.Change{EventID: tool.ID, Operation: "replace", Text: "hypothetical", Reason: "test result dependence"}}
	fork := func() x.Branch {
		r := call("POST", "/experiments/"+root.ID+"/branches", childInput, "experiment-child-0001")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var b x.Branch
		json.Unmarshal(r.Body.Bytes(), &b)
		return b
	}
	child := fork()
	if fork().ID != child.ID {
		t.Fatal("duplicate branch")
	}
	_, counts := x.Views(child, pack)
	if counts != (x.Counts{Recorded: 2, Edited: 1, Stale: 2}) {
		t.Fatal(counts)
	}
	for _, id := range []int64{future.ID, foreign.ID, last.ID} {
		badFork := childInput
		badFork.Change.EventID = id
		if _, err = m.ForkExperiment(root.ID, badFork); err == nil {
			t.Fatal("invalid patch accepted", id)
		}
	}
	badFork := childInput
	badFork.ExpectedParentHash = "bad"
	if _, err = m.ForkExperiment(root.ID, badFork); err == nil {
		t.Fatal("bad parent accepted")
	}
	r = call("GET", "/experiments/"+child.ID+"/diff", nil, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"total":3`) {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, path := range []string{"/experiments?limit=51", "/experiments?offset=-1", "/experiments?limit=1&limit=2", "/experiments/" + child.ID + "?mode=live", "/experiments/" + child.ID + "/events?sql=delete", "/experiments/" + child.ID + "/diff?limit=17"} {
		r = call("GET", path, nil, "")
		if r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	r = call("POST", "/experiments/"+child.ID+"/execute", map[string]any{}, "")
	if r.Code != 404 {
		t.Fatal("execution route unexpectedly exists", r.Code)
	}
	r = call("GET", fmt.Sprintf("/experiments/%s/events/%d", child.ID, future.ID), nil, "")
	if r.Code != 404 {
		t.Fatal("future event readable", r.Code)
	}
	r = call("GET", fmt.Sprintf("/experiments/%s/events/%d?offset=0&limit=20", child.ID, tool.ID), nil, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"hasMore":true`) {
		t.Fatal(r.Code, r.Body.String())
	}
	after, _ := m.Store.Events(source.ID, 0, 1000)
	sessionAfter, _ := m.Session(source.ID)
	if string(p.JSON(before)) != string(p.JSON(after)) || sessionBefore != sessionAfter || len(m.handles) != 0 {
		t.Fatal("offline operation changed source or opened worker")
	}
	// A persisted recording remains usable after its source session is removed and
	// a new host process opens the database. No source lookup is needed for replay.
	if err = m.DeleteSession(source.ID); err != nil {
		t.Fatal(err)
	}
	dataDir := m.Data
	m.Close()
	fresh, err := New(dataDir, "missing-worker", false)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	b, stored, err := fresh.experiment(child.ID)
	if err != nil || b.ContentHash != child.ContentHash || x.Hash(stored) != x.Hash(pack) || len(fresh.handles) != 0 {
		t.Fatal("offline reopen", b, err)
	}
	lineage, err := fresh.experimentLineage(b)
	if err != nil || len(lineage) != 2 || lineage[0].ID != root.ID {
		t.Fatal(lineage, err)
	}
	restore, err := fresh.ForkExperiment(child.ID, ExperimentForkInput{Title: "restore", ExpectedParentHash: child.ContentHash, Change: x.Change{EventID: tool.ID, Operation: "restore", Reason: "discard assumption"}})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := x.Diff(root, restore, stored)
	if err != nil || len(changes) != 0 {
		t.Fatal("restoration did not match recording", changes, err)
	}
}

// Maximum-size escaped replacements on both sides must still fit the public
// response ceiling and remain completely reachable through pagination.
func TestExperimentLargeDifferencePaging(t *testing.T) {
	m := kunMCPManager(t)
	pack := x.Bundle{Schema: x.Schema, Source: x.Source{Backend: "kun", SessionID: "fixture", RunID: "fixed"}, Events: []x.Event{}}
	for i := int64(1); i <= 32; i++ {
		pack.Events = append(pack.Events, x.Event{ID: i, Method: "kun/tool.completed", Data: p.JSON(map[string]any{"data": map[string]any{"call": p.ToolCall{ID: fmt.Sprint(i), Function: p.Function{Name: "read_file"}}, "output": "original", "status": "succeeded"}})})
	}
	root := x.NewRoot("root", "baseline", store.Now(), pack)
	ends := []x.Branch{}
	for _, marker := range []string{"<", ">"} {
		b := root
		for i := int64(1); i <= 32; i++ {
			var err error
			b, err = x.Fork(b, pack, store.ID(), "large", store.Now(), b.ContentHash, x.Change{EventID: i, Operation: "replace", Text: strings.Repeat(marker, 16000), Reason: strings.Repeat(marker, 2000)})
			if err != nil {
				t.Fatal(err)
			}
		}
		ends = append(ends, b)
	}
	// Only the two endpoints and shared bundle are needed by the diff route;
	// lineage traversal and persistence are covered by the lifecycle test.
	if err := m.Store.PutMany(store.Record{Kind: "experiment_bundle", ID: root.ID, Value: pack}, store.Record{Kind: "experiment", ID: ends[0].ID, Value: ends[0]}, store.Record{Kind: "experiment", ID: ends[1].ID, Value: ends[1]}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	seen := 0
	for offset := 0; ; {
		r := appRequest(h, "GET", fmt.Sprintf("/api/v1/experiments/%s/diff?against=%s&offset=%d", ends[1].ID, ends[0].ID, offset), "", userTestAdmin, "")
		if r.Code != 200 || r.Body.Len() > 4<<20 {
			t.Fatal(r.Code, r.Body.Len())
		}
		var page struct {
			Items      []x.Difference `json:"items"`
			NextOffset int            `json:"nextOffset"`
			HasMore    bool           `json:"hasMore"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 16 || page.NextOffset != offset+16 {
			t.Fatal("incorrect page", len(page.Items), page.NextOffset)
		}
		for _, item := range page.Items {
			if len(item.Before.Patch.Text) != 16000 || len(item.After.Patch.Reason) != 2000 {
				t.Fatal("truncated replacement")
			}
		}
		seen += len(page.Items)
		if !page.HasMore {
			break
		}
		offset = page.NextOffset
	}
	if seen != 32 {
		t.Fatal("missing changes", seen)
	}
}

func TestExperimentAuthorizationDefaultDeny(t *testing.T) {
	m, user, code, _, viewer, source, _ := userFixture(t)
	_, _, key := setupKey(t, m, "experiment-key", source.WorkspaceID, "read", "run", "approvals")
	h := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{code, viewer, key} {
		for _, prefix := range []string{"/api/v1", "/api/v1/member/" + user.Grants[0].ID} {
			for _, route := range []struct{ method, path string }{{"GET", "/experiments"}, {"POST", "/experiments"}, {"GET", "/experiments/id"}, {"POST", "/experiments/id/branches"}, {"GET", "/experiments/id/events"}, {"GET", "/experiments/id/events/1"}, {"GET", "/experiments/id/diff"}} {
				r := appRequest(h, route.method, prefix+route.path, `{}`, token, "experiment-denied")
				if r.Code != 403 {
					t.Fatal(route, prefix, r.Code)
				}
			}
		}
	}
}
