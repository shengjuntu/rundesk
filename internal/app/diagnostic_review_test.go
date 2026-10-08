package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestDiagnosticReviewLifecycle(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	dataDir := t.TempDir()
	m, err := New(dataDir, "no-codex-needed", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	m.Kun = buildKunTestBinary(t)
	var run string
	var cited int64
	var diagnosisCalls, sourceCalls atomic.Int32
	var captured []p.Message
	var captureMu sync.Mutex
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []p.Message       `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		message := p.Message{Role: "assistant", Content: "fixture complete"}
		finish := "stop"
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "RunDesk diagnostic assistant") {
			if len(req.Tools) != 7 {
				t.Error("diagnostic catalog changed")
			}
			if diagnosisCalls.Add(1) == 1 {
				finish = "tool_calls"
				message.Content = ""
				for _, kind := range []string{"steer", "inspect"} {
					message.ToolCalls = append(message.ToolCalls, p.ToolCall{ID: kind, Type: "function", Function: p.Function{Name: "trace_propose", Arguments: fmt.Sprintf(`{"runId":%q,"proposal":{"kind":%q,"reason":"recorded pause","text":"original suggestion","evidenceIds":[%d]}}`, run, kind, cited)}})
				}
			}
		} else {
			sourceCalls.Add(1)
			captureMu.Lock()
			captured = req.Messages
			captureMu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
	}))
	defer model.Close()
	instance, _ := m.Instance()
	if _, err = m.SetAgentRuntime(instance.ID, instance.Revision, p.Config{Kind: "kun", Endpoint: model.URL + "/v1", Model: "review-fixture", PauseBeforeModel: true}); err != nil {
		t.Fatal(err)
	}
	source, err := m.CreateSession(m.Workspaces()[0].ID, "paused source", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "source question"}); err != nil {
		t.Fatal(err)
	}
	source = waitState(t, m, source.ID, "waiting")
	run = source.RunID
	page, _ := m.Store.DebugEvents(source.ID, 0, nil, 200)
	cited = page.Events[0].ID
	diagnosis, err := m.CreateTraceAnalysis(TraceSelection{SessionID: source.ID, RunID: run})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(diagnosis.ID, Input{Text: "analyse source"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, diagnosis.ID, "completed")
	events, _ := m.Store.Events(diagnosis.ID, 0, 1000)
	var suggestionID, inspectID int64
	for _, ev := range events {
		if ev.Method != "kun/tool.completed" {
			continue
		}
		var v struct {
			Data struct {
				Result struct {
					Kind string `json:"kind"`
				} `json:"result"`
			} `json:"data"`
		}
		json.Unmarshal(ev.Data, &v)
		if v.Data.Result.Kind == "steer" {
			suggestionID = ev.ID
		}
		if v.Data.Result.Kind == "inspect" {
			inspectID = ev.ID
		}
	}
	if suggestionID == 0 || inspectID == 0 {
		t.Fatal("missing real worker suggestions")
	}
	client, _ := m.kunClient(source.ID)
	current := func() p.State {
		var s p.State
		if e := client.Call(context.Background(), "state", nil, &s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	before := current()
	beforeEvents, _ := m.Store.DebugEvents(source.ID, 0, nil, 1)
	handler := NewHandler(m, userTestAdmin, true)
	path := "/api/v1/sessions/" + diagnosis.ID + "/diagnostic/reviews"
	preview := func(id int64, text string, want int) DiagnosticReview {
		r := appRequest(handler, "POST", path, string(p.JSON(DiagnosticReviewInput{ProposalEventID: id, Text: text})), userTestAdmin, "")
		if r.Code != want {
			t.Fatalf("preview %d: %s", r.Code, r.Body.String())
		}
		var v DiagnosticReview
		if want == 200 {
			if e := json.Unmarshal(r.Body.Bytes(), &v); e != nil {
				t.Fatal(e)
			}
		}
		return v
	}
	preview(cited, "not a diagnostic event", 400)
	preview(inspectID, "inspect cannot become steer", 400)
	preview(suggestionID, " ", 400)
	r := appRequest(handler, "POST", path, fmt.Sprintf(`{"proposalEventId":%d,"text":"edited","sourceSessionId":"foreign"}`, suggestionID), userTestAdmin, "")
	if r.Code != 400 {
		t.Fatal("client selected target", r.Code)
	}
	first := preview(suggestionID, "reviewed instruction", 200)
	if first.Command.ExpectedRevision != before.Revision || first.Command.RunID != run || first.Outcome != "preview" {
		t.Fatal(first)
	}
	afterEvents, _ := m.Store.DebugEvents(source.ID, 0, nil, 1)
	if current().Revision != before.Revision || afterEvents.Through != beforeEvents.Through || sourceCalls.Load() != 0 {
		t.Fatal("preview changed source")
	}
	_, _, key := setupKey(t, m, "review-denied", source.WorkspaceID, "read", "run", "approvals")
	for _, method := range []string{"GET", "POST"} {
		r = appRequest(handler, method, path+fmt.Sprintf("?proposalEventId=%d", suggestionID), `{}`, key, "")
		if r.Code != 403 {
			t.Fatal("app accessed review", method, r.Code)
		}
	}
	r = appRequest(handler, "POST", path+"/"+first.ID+"/apply", `{}`, key, "")
	if r.Code != 403 {
		t.Fatal("app applied review", r.Code)
	}
	// Advance the revision without executing a model. The preview must stay frozen.
	_, err = func() (p.Receipt, error) {
		var receipt p.Receipt
		e := client.Call(context.Background(), "control", p.Control{RequestID: "change-before-apply", RunID: run, ExpectedRevision: before.Revision, Operation: "set_breakpoints", Debug: &p.DebugPolicy{}}, &receipt)
		return receipt, e
	}()
	if err != nil {
		t.Fatal(err)
	}
	r = appRequest(handler, "POST", path+"/"+first.ID+"/apply", `{}`, userTestAdmin, "")
	if r.Code != 409 || len(current().Queued) != 0 {
		t.Fatal("stale preview executed", r.Code, r.Body.String())
	}
	fresh := preview(suggestionID, "reviewed instruction", 200)
	r = appRequest(handler, "POST", path+"/"+fresh.ID+"/apply", `{"operation":"resume"}`, userTestAdmin, "")
	if r.Code != 400 {
		t.Fatal("apply body changed command", r.Code)
	}
	other, err := m.CreateTraceAnalysis(TraceSelection{SessionID: source.ID, RunID: run})
	if err != nil {
		t.Fatal(err)
	}
	r = appRequest(handler, "POST", "/api/v1/sessions/"+other.ID+"/diagnostic/reviews/"+fresh.ID+"/apply", `{}`, userTestAdmin, "")
	if r.Code != 404 {
		t.Fatal("cross-diagnosis review applied", r.Code)
	}
	// Simulate dispatch succeeding with the host receipt lost. Replay must use the
	// persisted command and be deduplicated by the real worker's command journal.
	fresh.Outcome = "unknown"
	fresh.SubmittedAt = store.Now()
	if err = m.Store.Put("diagnostic_review", fresh.ID, fresh); err != nil {
		t.Fatal(err)
	}
	var receipt p.Receipt
	if err = client.Call(context.Background(), "control", fresh.Command, &receipt); err != nil {
		t.Fatal(err)
	}
	queuedRevision := current().Revision
	apply := func() DiagnosticReview {
		r := appRequest(handler, "POST", path+"/"+fresh.ID+"/apply", `{}`, userTestAdmin, "")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var v DiagnosticReview
		json.Unmarshal(r.Body.Bytes(), &v)
		return v
	}
	got := apply()
	apply()
	after := current()
	if got.Outcome != "queued" || after.Revision != queuedRevision || after.Status != "paused" || len(after.Queued) != 1 || after.Queued[0].Text != "reviewed instruction" || sourceCalls.Load() != 0 {
		t.Fatal("retry duplicated/resumed source", got, after)
	}
	// The explicit ordinary resume is separate from suggestion application.
	if err = client.Call(context.Background(), "control", p.Control{RequestID: "resume-after-review", RunID: run, ExpectedRevision: after.Revision, Operation: "resume"}, &receipt); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, source.ID, "completed")
	for n := 0; n < 40; n++ {
		got = apply()
		if got.Outcome == "applied" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got.Outcome != "applied" || got.ReceiptEventID == 0 || sourceCalls.Load() != 1 {
		t.Fatal("missing final receipt", got)
	}
	captureMu.Lock()
	count := 0
	for _, v := range captured {
		if v.Role == "user" && v.Content == "reviewed instruction" {
			count++
		}
		if v.Content == "original suggestion" {
			t.Error("unedited text executed")
		}
	}
	captureMu.Unlock()
	if count != 1 {
		t.Fatal("reviewed text occurrence", count)
	}
	preview(suggestionID, "old finished run", 409)
	if _, err = m.Start(source.ID, Input{Text: "new source round"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, source.ID, "waiting")
	preview(suggestionID, "must not retarget new run", 409)
	if err = m.Stop(source.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, source.ID, "interrupted")
	// Audit reads and accepted replays survive host restart without opening workers.
	m.Close()
	m, err = New(dataDir, "no-codex-needed", false)
	if err != nil {
		t.Fatal(err)
	}
	handler = NewHandler(m, userTestAdmin, true)
	list, err := m.DiagnosticReviews(diagnosis.ID, suggestionID)
	if err != nil || len(list) != 2 || list[0].Outcome != "applied" || list[1].Outcome != "rejected" {
		t.Fatal(list, err)
	}
	got = apply()
	if got.Outcome != "applied" || len(m.handles) != 0 {
		t.Fatal("history read started worker", got, len(m.handles))
	}
}

func TestDiagnosticReviewMemberDefaultDeny(t *testing.T) {
	m, user, code, _, viewerCode, source, _ := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{code, viewerCode} {
		for _, prefix := range []string{"/api/v1", "/api/v1/member/" + user.Grants[0].ID} {
			path := prefix + "/sessions/" + source.ID + "/diagnostic/reviews"
			for _, route := range []struct{ method, path string }{{"GET", path + "?proposalEventId=1"}, {"POST", path}, {"POST", path + "/test-review/apply"}} {
				r := appRequest(h, route.method, route.path, `{}`, token, "")
				if r.Code != 403 {
					t.Fatal("member accessed review", route, r.Code, r.Body.String())
				}
			}
		}
	}
}
