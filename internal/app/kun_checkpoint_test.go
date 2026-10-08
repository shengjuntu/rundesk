package app

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestKunCheckpointAfterWorkerKillAndHostRestart(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	data := t.TempDir()
	m, err := New(data, "unused-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	binary := buildKunTestBinary(t)
	m.Kun = binary
	var models atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: "continued"}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 9}})
	}))
	defer provider.Close()
	i, _ := m.Instance()
	i, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: provider.URL + "/v1", Model: "fixture", PauseBeforeModel: true})
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSession(m.Workspaces()[0].ID, "restart", "", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err = m.Start(session.ID, Input{Text: "continue after a worker crash"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "waiting")
	client, err := m.kunClient(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	pid := client.PID()
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err = process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "failed")
	m.Close()
	m, err = New(data, "unused-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Kun = binary
	const adminToken = "admin-checkpoint-01234567890123456789"
	handler := NewHandler(m, adminToken, true)
	path := "/api/v1/sessions/" + session.ID + "/kun/"
	response := appRequest(handler, "GET", path+"checkpoint", "", adminToken, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var check p.CheckpointCheck
	json.Unmarshal(response.Body.Bytes(), &check)
	if !check.Eligible || check.Selection.SourceRunID != session.RunID || models.Load() != 0 {
		t.Fatal(check, models.Load())
	}
	current, _ := m.kunClient(session.ID)
	if current.PID() == pid {
		t.Fatal("worker was not replaced")
	}
	body := string(p.JSON(check.Selection))
	response = appRequest(handler, "POST", path+"resume", body, adminToken, "")
	if response.Code != 400 {
		t.Fatal("missing idempotency key accepted", response.Code, response.Body.String())
	}
	stale := check.Selection
	stale.Sequence++
	response = appRequest(handler, "POST", path+"resume", string(p.JSON(stale)), adminToken, "stale-checkpoint-001")
	if response.Code != 409 {
		t.Fatal(response.Code, response.Body.String())
	}
	// Recovery is admitted through the existing global/per-instance capacity gate.
	m.queueMu.Lock()
	oldMax := m.queue.MaxConcurrent
	m.queue.MaxConcurrent = 0
	m.queueMu.Unlock()
	response = appRequest(handler, "POST", path+"resume", body, adminToken, "capacity-checkpoint-001")
	if response.Code != 429 {
		t.Fatal("capacity bypass", response.Code, response.Body.String())
	}
	m.queueMu.Lock()
	m.queue.MaxConcurrent = oldMax
	m.queueMu.Unlock()
	response = appRequest(handler, "POST", path+"resume", body, adminToken, "recover-checkpoint-001")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var resumed Session
	json.Unmarshal(response.Body.Bytes(), &resumed)
	if resumed.RunID == session.RunID {
		t.Fatal("recovery reused run ID")
	}
	original := response.Body.String()
	response = appRequest(handler, "POST", path+"resume", body, adminToken, "recover-checkpoint-001")
	if response.Code != 200 || strings.TrimSpace(response.Body.String()) != strings.TrimSpace(original) || response.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("idempotent response changed", response.Code, response.Body.String())
	}
	waitState(t, m, session.ID, "waiting")
	response = appRequest(handler, "GET", path+"state", "", adminToken, "")
	var state p.State
	json.Unmarshal(response.Body.Bytes(), &state)
	if state.ResumedFrom == nil || state.ResumedFrom.SourceRunID != session.RunID || models.Load() != 0 {
		t.Fatal(state, models.Load())
	}
	response = appRequest(handler, "POST", path+"control", string(p.JSON(p.Control{RequestID: "finish-recovered-run", RunID: state.RunID, ExpectedRevision: state.Revision, Operation: "resume"})), adminToken, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	waitState(t, m, session.ID, "completed")
	if models.Load() != 1 {
		t.Fatal("duplicate model call", models.Load())
	}
	response = appRequest(handler, "POST", path+"resume", body, adminToken, "consumed-checkpoint-001")
	if response.Code != 409 {
		t.Fatal("consumed checkpoint reused", response.Code, response.Body.String())
	}
}
func TestKunCheckpointApplicationScopes(t *testing.T) {
	m := kunMCPManager(t)
	w := m.Workspaces()[0]
	application, _, key := setupKey(t, m, "checkpoint-owner", w.ID, "read", "run")
	_, readKey, err := m.CreateApplicationKey("checkpoint-owner", KeyInput{Name: "reader", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, foreign := setupKey(t, m, "checkpoint-foreign", w.ID, "read", "run")
	i, _ := m.Instance(application.InstanceID)
	_, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(m, "admin-secret-01234567890123456789", true)
	response := appRequest(handler, "POST", "/api/v1/sessions", string(p.JSON(map[string]string{"workspaceId": w.ID})), key, "checkpoint-create-001")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var session Session
	json.Unmarshal(response.Body.Bytes(), &session)
	path := "/api/v1/sessions/" + session.ID + "/kun/"
	response = appRequest(handler, "GET", path+"checkpoint", "", readKey, "")
	if response.Code != 200 {
		t.Fatal("read scope cannot inspect", response.Code, response.Body.String())
	}
	for _, token := range []string{readKey, foreign} {
		response = appRequest(handler, "POST", path+"resume", `{}`, token, "checkpoint-denied-001")
		if response.Code != 403 {
			t.Fatal("resume authorization bypass", response.Code, response.Body.String())
		}
	}
	response = appRequest(handler, "GET", path+"checkpoint", "", foreign, "")
	if response.Code != 403 {
		t.Fatal("foreign checkpoint visible", response.Code, response.Body.String())
	}
}
