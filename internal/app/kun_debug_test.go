package app

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKunDebugReadScopesAndControlProjection(t *testing.T) {
	m := kunMCPManager(t)
	m.Kun = buildKunTestBinary(t)
	workspace := m.Workspaces()[0]
	application, _, runner := setupKey(t, m, "debug-owner", workspace.ID, "read", "run")
	_, reader, err := m.CreateApplicationKey(application.AppID, KeyInput{Name: "reader", WorkspaceIDs: []string{workspace.ID}, Scopes: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, foreign := setupKey(t, m, "debug-foreign", workspace.ID, "read", "run")
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: "finished"}, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	i, _ := m.Instance(application.InstanceID)
	_, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: provider.URL + "/v1", Model: "fixture", Debug: p.DebugPolicy{Breakpoints: []p.Breakpoint{{ID: "before", Phase: "before_model", Once: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(m, "admin-debug-01234567890123456789", true)
	response := appRequest(handler, "POST", "/api/v1/sessions", string(p.JSON(map[string]string{"workspaceId": workspace.ID})), runner, "debug-session-001")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var session Session
	json.Unmarshal(response.Body.Bytes(), &session)
	if _, err = m.Start(session.ID, Input{Text: "answer once"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "waiting")
	path := "/api/v1/sessions/" + session.ID + "/kun/"
	response = appRequest(handler, "GET", path+"state", "", reader, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var before p.State
	json.Unmarshal(response.Body.Bytes(), &before)
	if before.Debug.Pause == nil {
		t.Fatal("missing breakpoint pause")
	}
	for _, kind := range []string{"run", "context", "tools", "budget", "modules", "breakpoints", "actions"} {
		response = appRequest(handler, "GET", path+"query?kind="+kind, "", reader, "")
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		var result p.DebugResult
		json.Unmarshal(response.Body.Bytes(), &result)
		if result.RunID != before.RunID || result.Revision != before.Revision {
			t.Fatal("query changed run", result)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("queries called provider")
	}
	response = appRequest(handler, "GET", path+"query?kind=context", "", foreign, "")
	if response.Code != 403 {
		t.Fatal("cross-app query allowed", response.Code)
	}
	for _, query := range []string{"kind=shell", "kind=run&sequence=-1", "kind=run&sequence=1foo"} {
		response = appRequest(handler, "GET", path+"query?"+query, "", reader, "")
		if response.Code != 400 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	command := p.Control{RequestID: "edit-debug-policy", RunID: before.RunID, ExpectedRevision: before.Revision, Operation: "set_breakpoints", Debug: &p.DebugPolicy{Breakpoints: []p.Breakpoint{{ID: "after", Phase: "after_model"}}}}
	body := string(p.JSON(command))
	response = appRequest(handler, "POST", path+"control", body, reader, "")
	if response.Code != 403 {
		t.Fatal("read-only principal changed rules", response.Code)
	}
	response = appRequest(handler, "POST", path+"control", body, runner, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var receipt p.Receipt
	json.Unmarshal(response.Body.Bytes(), &receipt)
	response = appRequest(handler, "POST", path+"control", body, runner, "")
	var retry p.Receipt
	json.Unmarshal(response.Body.Bytes(), &retry)
	if response.Code != 200 || retry != receipt {
		t.Fatal("policy receipt not idempotent", response.Code, retry, receipt)
	}
	response = appRequest(handler, "POST", path+"control", string(p.JSON(p.Control{RequestID: "resume-before-boundary", RunID: before.RunID, ExpectedRevision: receipt.Revision, Operation: "resume"})), runner, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	// The terminal model response must still yield the after_model breakpoint.
	var after p.State
	for n := 0; n < 100; n++ {
		response = appRequest(handler, "GET", path+"state", "", reader, "")
		json.Unmarshal(response.Body.Bytes(), &after)
		if after.Status == "paused" && after.Phase == "after_model" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after.Phase != "after_model" || after.Status != "paused" || calls.Load() != 1 {
		t.Fatal(after)
	}
	response = appRequest(handler, "POST", path+"control", string(p.JSON(p.Control{RequestID: "resume-stale-proposal", RunID: before.RunID, ExpectedRevision: before.Revision, Operation: "resume"})), runner, "")
	if response.Code != 409 {
		t.Fatal("stale console proposal accepted", response.Code)
	}
	response = appRequest(handler, "POST", path+"control", string(p.JSON(p.Control{RequestID: "resume-final-boundary", RunID: after.RunID, ExpectedRevision: after.Revision, Operation: "resume"})), runner, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	waitState(t, m, session.ID, "completed")
	if calls.Load() != 1 {
		t.Fatal("extra model call")
	}
	response = appRequest(handler, "GET", path+"query?kind=context", "", reader, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "resume-stale-proposal") {
		t.Fatal("console control polluted messages", response.Body.String())
	}
}
