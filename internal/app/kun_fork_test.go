package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

func TestHybridHostLifecycle(t *testing.T) {
	m := kunMCPManager(t)
	binary := buildKunTestBinary(t)
	m.Kun = binary
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Messages []p.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		calls.Add(1)
		msg := p.Message{Role: "assistant", Content: "done"}
		if q.Messages[len(q.Messages)-1].Role != "tool" {
			msg.Content = ""
			msg.ToolCalls = []p.ToolCall{{ID: "read", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"input.txt"}`}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 5}})
	}))
	defer model.Close()
	i, _ := m.Instance()
	cfg := i.AgentRuntime
	cfg.Endpoint = model.URL + "/v1"
	if _, err := m.SetAgentRuntime(i.ID, i.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	ws := m.Workspaces()[0]
	os.WriteFile(filepath.Join(ws.Path, "input.txt"), []byte("private-recorded-body"), 0600)
	source, err := m.CreateSession(ws.ID, "source", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "read input"}); err != nil {
		t.Fatal(err)
	}
	source = waitState(t, m, source.ID, "completed")
	var points p.ForkPoints
	for n := 0; n < 30; n++ {
		points, err = m.KunForkPoints(source.ID, 0, 50)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	for _, pt := range points.Items {
		if pt.Phase == "before_model" && pt.Step == 0 {
			selection.Sequence = pt.Sequence
		}
	}
	if selection.Sequence == 0 {
		t.Fatal(points)
	}
	h := NewHandler(m, userTestAdmin, true)
	call := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		t.Helper()
		raw := ""
		if body != nil {
			raw = string(p.JSON(body))
		}
		return appRequest(h, method, "/api/v1"+path, raw, userTestAdmin, key)
	}
	input := KunForkInput{SessionID: source.ID, Selection: selection, Title: "Hybrid test"}
	r := call("POST", "/kun-forks", input, "preview-first")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var preview KunForkPreview
	json.Unmarshal(r.Body.Bytes(), &preview)
	if calls.Load() != 2 || preview.RecordCount != 1 || preview.TargetSessionID == source.ID {
		t.Fatal(preview, calls.Load())
	}
	duplicate := call("POST", "/kun-forks", input, "preview-first")
	var retryPreview KunForkPreview
	json.Unmarshal(duplicate.Body.Bytes(), &retryPreview)
	if duplicate.Code != 200 || retryPreview.ID != preview.ID || retryPreview.Hash != preview.Hash {
		t.Fatal("preview retry changed", duplicate.Code, duplicate.Body.String())
	}
	for _, path := range []string{"/kun-forks", "/kun-forks/" + preview.ID} {
		r = call("GET", path, nil, "")
		if r.Code != 200 || strings.Contains(r.Body.String(), "private-recorded-body") || strings.Contains(r.Body.String(), `"messages"`) {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for _, query := range []string{"?limit=51", "?offset=-1", "?offset=1&offset=2", "?unknown=1"} {
		r = call("GET", "/kun-forks/sources/"+source.ID+query, nil, "")
		if r.Code != 400 {
			t.Fatal(query, r.Code)
		}
	}
	// Create an unused preview and preserve it across source deletion and restart.
	input.Title = "restart branch"
	later, err := m.CreateKunFork(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Title = "drift branch"
	drift, err := m.CreateKunFork(input)
	if err != nil {
		t.Fatal(err)
	}
	stale := input
	stale.Selection.WorkerEpoch = "stale"
	if _, err = m.CreateKunFork(stale); err == nil {
		t.Fatal("stale source accepted")
	}
	path := "/kun-forks/" + preview.ID + "/start"
	if r = call("POST", path, map[string]string{"expectedHash": "changed"}, "bad-hash"); r.Code != 409 {
		t.Fatal(r.Code)
	}
	r = call("POST", path, map[string]string{"expectedHash": preview.Hash}, "start-first")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var branch Session
	json.Unmarshal(r.Body.Bytes(), &branch)
	waitState(t, m, branch.ID, "completed")
	if branch.KunFork == nil || branch.ID != preview.TargetSessionID || calls.Load() != 4 {
		t.Fatal(branch, calls.Load())
	}
	r = call("GET", "/kun-forks/comparisons/"+preview.ID, nil, "")
	var comparison KunForkComparison
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &comparison) != nil || !comparison.Left.Complete || !comparison.Right.Complete || comparison.Left.ModelCalls != 2 || comparison.Right.ModelCalls != 2 || comparison.Left.ReportedTokens != 10 || comparison.Right.ReportedTokens != 10 || len(comparison.Right.Tools) != 1 || comparison.Right.Tools[0].Replayed != 1 || comparison.Right.Tools[0].Dispatched != 0 || comparison.Left.Tools[0].Dispatched != 1 || calls.Load() != 4 || strings.Contains(r.Body.String(), "private-recorded-body") {
		t.Fatal("real-worker comparison", r.Code, r.Body.String(), calls.Load())
	}
	for _, key := range []string{"start-first", "start-new-key"} {
		r = call("POST", path, map[string]string{"expectedHash": preview.Hash}, key)
		var retry Session
		json.Unmarshal(r.Body.Bytes(), &retry)
		if r.Code != 200 || retry.RunID != branch.RunID || calls.Load() != 4 {
			t.Fatal(r.Code, retry, calls.Load())
		}
	}
	if _, err = m.Start(branch.ID, Input{Text: "ordinary turn"}); err == nil {
		t.Fatal("escaped Hybrid")
	}
	after, _ := m.KunForkPoints(source.ID, 0, 50)
	if after.Selection != points.Selection {
		t.Fatal("source changed", after.Selection, points.Selection)
	}
	if err = m.DeleteSession(branch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.StartKunFork(preview.ID, preview.Hash, false); err == nil {
		t.Fatal("deleted target resurrected")
	}
	if _, err = m.Session(branch.ID); err == nil {
		t.Fatal("deleted target exists")
	}
	if err = m.DeleteSession(source.ID); err != nil {
		t.Fatal(err)
	}
	data := m.Data
	m.Close()
	m2, err := New(data, "missing-codex-for-kun", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	m2.Kun = binary
	if _, err = m2.StartKunFork(preview.ID, preview.Hash, false); err == nil {
		t.Fatal("restart lost deletion tombstone")
	}
	restarted, err := m2.StartKunFork(later.ID, later.Hash, false)
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m2, restarted.ID, "completed")
	if calls.Load() != 6 {
		t.Fatal(calls.Load())
	}
	instance, _ := m2.Instance()
	cfg = instance.AgentRuntime
	cfg.MaxSteps++
	if _, err = m2.SetAgentRuntime(instance.ID, instance.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err = m2.StartKunFork(drift.ID, drift.Hash, false); err == nil {
		t.Fatal("config drift allowed")
	}
	if calls.Load() != 6 {
		t.Fatal("unexpected model call")
	}
}

func TestHybridAuthorizationDefaultDeny(t *testing.T) {
	m, user, code, _, viewer, source, _ := userFixture(t)
	_, _, key := setupKey(t, m, "hybrid-key", source.WorkspaceID, "read", "run", "approvals")
	h := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{code, viewer, key} {
		for _, prefix := range []string{"/api", "/api/v1", "/api/v1/member/" + user.Grants[0].ID} {
			for _, route := range []struct{ method, path string }{{"GET", "/kun-forks"}, {"POST", "/kun-forks"}, {"GET", "/kun-forks/id"}, {"GET", "/kun-forks/comparisons/id"}, {"POST", "/kun-forks/id/start"}, {"GET", "/kun-forks/sources/" + source.ID}} {
				r := appRequest(h, route.method, prefix+route.path, `{}`, token, "hybrid-denied")
				if r.Code != 403 {
					t.Fatal(fmt.Sprint(route), prefix, r.Code)
				}
			}
		}
	}
}
