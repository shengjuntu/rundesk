package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func v1Request(h http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:3210/api/v1"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func object(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
		t.Fatal(r.Code, r.Body.String(), err)
	}
	return data
}
func TestV1AuthAndErrorContract(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "private-long-token-1234567890", true)
	r := v1Request(h, "GET", "/meta", "", "")
	data := object(t, r)
	if r.Code != 401 || data["code"] != "unauthorized" || data["requestId"] == "" || data["retryable"] != false || r.Header().Get("RunDesk-API-Version") != "v1" {
		t.Fatal(r.Code, data, r.Header())
	}
	r = request(h, "GET", "/api/v1/meta", "", "private-long-token-1234567890", "")
	if r.Code != 200 || object(t, r)["version"] != Version {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(h, "POST", "/api/v1/login", `{"token":"private-long-token-1234567890"}`, "", "")
	if r.Code != 200 || len(r.Result().Cookies()) != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	cookie := r.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "http://127.0.0.1:3210/api/v1/sessions", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r = request(h, "GET", "/api/v1/meta", "", "private-long-token-1234567890", "https://evil.example")
	if r.Code != 403 {
		t.Fatal("v1 origin bypass")
	}
	public := NewHandler(m, "", true)
	r = v1Request(public, "GET", "/sessions/no-such-session", "", "")
	if r.Code != 404 || object(t, r)["code"] != "session_not_found" {
		t.Fatal(r.Code, r.Body.String())
	}
	legacy := request(public, "GET", "/api/sessions/no-such-session", "", "", "")
	if legacy.Code != 400 {
		t.Fatal("legacy status changed", legacy.Code)
	}
	r = v1Request(public, "GET", "/does-not-exist", "", "")
	if r.Code != 404 || object(t, r)["code"] != "not_found" {
		t.Fatal(r.Code)
	}
}
func TestV1CreationReceiptsSourceAndLegacy(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	wid := m.Workspaces()[0].ID
	body := `{"workspaceId":"` + wid + `","source":{"kind":"application","appId":"news2douyin","taskId":"research-42"}}`
	r := v1Request(h, "POST", "/sessions", body, "")
	if r.Code != 400 || object(t, r)["code"] != "idempotency_key_required" {
		t.Fatal(r.Code, r.Body.String())
	}
	r = v1Request(h, "POST", "/sessions", body, "create-session-42")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	first := object(t, r)
	r = v1Request(h, "POST", "/sessions", body, "create-session-42")
	if r.Header().Get("Idempotency-Replayed") != "true" || object(t, r)["id"] != first["id"] || len(m.Sessions()) != 1 {
		t.Fatal("duplicate creation", r.Body.String())
	}
	r = v1Request(h, "POST", "/sessions", `{"workspaceId":"`+wid+`"}`, "create-session-42")
	if r.Code != 409 || object(t, r)["code"] != "idempotency_conflict" {
		t.Fatal(r.Code, r.Body.String())
	}
	r = v1Request(h, "GET", "/requests/create-session-42", "", "")
	receipt := object(t, r)
	if receipt["state"] != "completed" || receipt["response"].(map[string]any)["id"] != first["id"] || receipt["owner"] != nil || receipt["fingerprint"] != nil {
		t.Fatal(receipt)
	}
	r = v1Request(h, "GET", "/sessions?appId=other", "", "")
	if strings.TrimSpace(r.Body.String()) != "[]" {
		t.Fatal(r.Body.String())
	}
	r = v1Request(h, "GET", "/sessions?appId=news2douyin&taskId=research-42", "", "")
	if !strings.Contains(r.Body.String(), first["id"].(string)) {
		t.Fatal(r.Body.String())
	}
	r = request(h, "POST", "/api/sessions", `{"workspaceId":"`+wid+`"}`, "", "")
	if r.Code != 200 || len(m.Sessions()) != 2 {
		t.Fatal("old client broken", r.Code)
	}
	r = v1Request(h, "POST", "/sessions", `{"workspaceId":"`+wid+`","source":{"kind":"application"}}`, "invalid-source-42")
	if r.Code != 400 || object(t, r)["code"] != "invalid_source" || len(m.Sessions()) != 2 {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestV1ConcurrentTurnReplayRunsOnlyOnce(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	session, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	path := "/sessions/" + session.ID + "/turns"
	body := `{"text":"a single task","skills":[],"files":[]}`
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- v1Request(h, "POST", path, body, "one-logical-task-42") }()
	}
	wg.Wait()
	close(responses)
	runID := ""
	for r := range responses {
		v := object(t, r)
		if r.Code == 409 && v["code"] == "request_in_progress" {
			continue
		}
		if r.Code != 202 {
			t.Fatal(r.Code, v)
		}
		if runID == "" {
			runID = v["runId"].(string)
		}
		if runID != v["runId"] {
			t.Fatal("different runs")
		}
	}
	completed := waitState(t, m, session.ID, "completed")
	if completed.RunID != runID {
		t.Fatal(completed)
	}
	replay := v1Request(h, "POST", path, body, "one-logical-task-42")
	if replay.Code != 202 || object(t, replay)["runId"] != runID {
		t.Fatal(replay.Body.String())
	}
	events, _ := m.Store.Events(session.ID, 0, 1000)
	count := 0
	for _, e := range events {
		if e.Method == "run/input" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("started %d runs", count)
	}
	conflict := v1Request(h, "POST", path, `{"text":"different"}`, "one-logical-task-42")
	if conflict.Code != 409 {
		t.Fatal(conflict.Code)
	}
	// A fresh key intentionally starts a new run with identical content.
	next := v1Request(h, "POST", path, body, "second-logical-task")
	if next.Code != 202 || object(t, next)["runId"] == runID {
		t.Fatal(next.Body.String())
	}
	waitState(t, m, session.ID, "completed")
}
func TestV1ReceiptsSurviveRestartAndFenceUnknown(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, "", true)
	body := `{"name":"dedicated"}`
	first := v1Request(h, "POST", "/instances", body, "persistent-instance")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	id := object(t, first)["id"]
	// Simulate a crash after reservation but before recording the response.
	uncertain := v1Request(h, "POST", "/instances", `{"name":"uncertain"}`, "uncertain-instance")
	if uncertain.Code != 200 {
		t.Fatal(uncertain.Code)
	}
	var saved requestRecord
	m.Store.Get("api-request", requestStoreKey("uncertain-instance"), &saved)
	saved.State = "processing"
	saved.Response = nil
	m.Store.Put("api-request", requestStoreKey(saved.Key), saved)
	r := v1Request(h, "POST", "/instances", `{"name":"uncertain"}`, "uncertain-instance")
	if object(t, r)["code"] != "request_unconfirmed" {
		t.Fatal(r.Body.String())
	}
	before := len(m.Instances())
	m.Close()
	m, err = New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	h = NewHandler(m, "", true)
	replay := v1Request(h, "POST", "/instances", body, "persistent-instance")
	if replay.Code != 200 || object(t, replay)["id"] != id {
		t.Fatal(replay.Body.String())
	}
	r = v1Request(h, "POST", "/instances", `{"name":"uncertain"}`, "uncertain-instance")
	if r.Code != 409 || object(t, r)["code"] != "request_unconfirmed" || len(m.Instances()) != before {
		t.Fatal(r.Code, r.Body.String())
	}
	r = v1Request(h, "GET", "/requests/uncertain-instance", "", "")
	if object(t, r)["state"] != "unconfirmed" {
		t.Fatal(r.Body.String())
	}
}
func TestV1ConfigurationScopeAndHistoricalModel(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	work := m.Workspaces()[0]
	i, _ := m.CreateInstance(InstancePatch{Name: "Research", DefaultModel: "model-before"})
	other, _ := m.CreateInstance(InstancePatch{Name: "Other"})
	r := v1Request(h, "GET", "/instances/"+i.ID+"/configuration?workspaceId="+work.ID, "", "")
	if r.Code != 200 || object(t, r)["probed"] != false || m.loaded.Load() != 0 {
		t.Fatal(r.Code, r.Body.String(), m.loaded.Load())
	}
	content := "---\nname: research-guide\ndescription: research\n---\nRead evidence."
	if err := m.SaveSkill(work.ID, "research-guide", content, i.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	m.SaveSkill(work.ID, "other-guide", strings.ReplaceAll(content, "research-guide", "other-guide"), other.ID, "instance")
	m.SaveMCP(work.ID, "search", "0", map[string]any{"url": "https://example.com/mcp?secret=not-in-overview", "enabled": false, "http_headers": map[string]any{"Authorization": "private-secret"}}, false, i.ID)
	r = v1Request(h, "GET", "/instances/"+i.ID+"/configuration?workspaceId="+work.ID+"&probe=1", "", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "research-guide") || strings.Contains(r.Body.String(), "other-guide") || strings.Contains(r.Body.String(), "private-secret") || strings.Contains(r.Body.String(), "not-in-overview") {
		t.Fatal(r.Code, r.Body.String())
	}
	session, _ := m.CreateSession(work.ID, "", "", i.ID)
	if _, err := m.Start(session.ID, Input{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "completed")
	m.PatchInstance(i.ID, InstancePatch{Name: i.Name, DefaultModel: "model-after", Revision: 0})
	r = v1Request(h, "GET", "/sessions/"+session.ID+"/configuration", "", "")
	value := object(t, r)
	if value["instance"].(map[string]any)["defaultModel"] != "model-after" || value["session"].(map[string]any)["model"] != "model-before" || value["lastSubmission"].(map[string]any)["model"] != "model-before" {
		t.Fatal(value)
	}
}
func TestV1OpenAPIAndPayloadBounds(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	r := v1Request(h, "GET", "/openapi.json", "", "")
	spec := object(t, r)
	if spec["openapi"] != "3.1.0" {
		t.Fatal(spec)
	}
	paths := spec["paths"].(map[string]any)
	for _, path := range []string{"/sessions/{sid}/turns", "/sessions/{sid}/steer", "/sessions/{sid}/events", "/instances/{iid}/configuration", "/requests/{key}"} {
		if paths[path] == nil {
			t.Fatal(path)
		}
	}
	r = v1Request(h, "POST", "/instances", `{"name":"`+strings.Repeat("x", 1024*1024)+`"}`, "large-request-body")
	if r.Code != 413 || object(t, r)["code"] != "body_too_large" {
		t.Fatal(r.Code)
	}
	for _, body := range []string{"null", "[]", `{} {}`} {
		r = v1Request(h, "POST", "/instances", body, "bad-body-request")
		if r.Code != 400 {
			t.Fatal(r.Code, body)
		}
	}
}

func TestV1PartialInstancePatchAndStopPrecondition(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	instance, _ := m.CreateInstance(InstancePatch{Name: "original", Description: "keep-description", DefaultModel: "keep-model"})
	r := v1Request(h, "PATCH", "/instances/"+instance.ID, `{"name":"renamed","revision":0}`, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	value := object(t, r)
	if value["defaultModel"] != "keep-model" || value["description"] != "keep-description" {
		t.Fatal(value)
	}
	r = v1Request(h, "PATCH", "/instances/"+instance.ID, `{"revision":0}`, "")
	if r.Code != 409 || object(t, r)["code"] != "revision_conflict" {
		t.Fatal(r.Code, r.Body.String())
	}
	r = v1Request(h, "PATCH", "/instances/"+instance.ID, `{"name":"bad"}`, "")
	if r.Code != 400 {
		t.Fatal(r.Code)
	}
	session, _ := m.CreateSession(m.Workspaces()[0].ID, "", "", instance.ID)
	m.Start(session.ID, Input{Text: "审批"})
	first := waitState(t, m, session.ID, "waiting")
	path := "/sessions/" + session.ID + "/stop"
	r = v1Request(h, "POST", path, `{"expectedRunId":"a-stale-run"}`, "")
	if r.Code != 409 || object(t, r)["code"] != "run_conflict" {
		t.Fatal(r.Code, r.Body.String())
	}
	still, _ := m.Session(session.ID)
	if still.Status != "waiting" {
		t.Fatal(still)
	}
	r = v1Request(h, "POST", path, `{"expectedRunId":"`+first.RunID+`"}`, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	waitState(t, m, session.ID, "interrupted")
	r = v1Request(h, "POST", path, `{"expectedRunId":"`+first.RunID+`"}`, "")
	if r.Code != 200 {
		t.Fatal("stop replay", r.Code, r.Body.String())
	}
	m.Start(session.ID, Input{Text: "审批 again"})
	next := waitState(t, m, session.ID, "waiting")
	handle, _ := m.getHandle(session.ID)
	m.interrupt(session.ID, handle, first.RunID)
	still, _ = m.Session(session.ID)
	if still.RunID != next.RunID || still.Status != "waiting" {
		t.Fatal("old queued interrupt crossed runs", still)
	}
	m.StopRun(session.ID, next.RunID)
	waitState(t, m, session.ID, "interrupted")
}

func TestV1SSEUsesDurableCursorWithoutBuffering(t *testing.T) {
	m := testManager(t)
	session, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	for n := 0; n < 3; n++ {
		m.event(session.ID, "internal", "test/event", map[string]int{"number": n})
	}
	events, _ := m.Store.Events(session.ID, 0, 100)
	server := httptest.NewServer(NewHandler(m, "", true))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/sessions/"+session.ID+"/events?stream=1&after=0", nil)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(events[1].ID, 10))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("RunDesk-API-Version") != "v1" {
		t.Fatal(response.Status, response.Header)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "id: ") {
			got := strings.TrimPrefix(scanner.Text(), "id: ")
			if got != strconv.FormatInt(events[2].ID, 10) {
				t.Fatal("wrong resumed event", got)
			}
			return
		}
	}
	t.Fatal("SSE did not flush a resumed event", scanner.Err())
}
