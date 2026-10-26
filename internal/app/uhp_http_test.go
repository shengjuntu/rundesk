package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func uhpTestCall(h http.Handler, method, path, body, token string, headers ...map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:3210"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for _, values := range headers {
		for k, v := range values {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func uhpTestJSON(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if w.Code != status {
		t.Fatalf("wanted HTTP %d, got %d: %s", status, w.Code, w.Body.String())
	}
	if w.Header().Get("UHP-Version") != UHPVersion || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing protocol/security headers: %v", w.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %s (%v)", w.Body.String(), err)
	}
	return body
}

func uhpTestError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	body := uhpTestJSON(t, w, status)
	err, ok := body["error"].(map[string]any)
	if !ok || err["code"] != code || err["type"] == "" || err["message"] == "" {
		t.Fatalf("invalid error envelope: %v", body)
	}
	for _, key := range []string{"param", "detail"} {
		if _, ok := err[key]; !ok {
			t.Fatalf("missing %s in error: %v", key, err)
		}
	}
}

func uhpTestResponse(t *testing.T, w *httptest.ResponseRecorder) uhpResponse {
	t.Helper()
	uhpTestJSON(t, w, 200)
	var response uhpResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response.ID, "resp_") || response.Object != "response" || response.Metadata["session_id"] == nil {
		t.Fatalf("invalid response identity: %+v", response)
	}
	return response
}

func uhpTestRecord(t *testing.T, m *Manager, responseID string) uhpRecord {
	t.Helper()
	var record uhpRecord
	if err := m.Store.Get("uhp-response", responseID, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func uhpTestWaitResponse(t *testing.T, h http.Handler, id, token, status string) uhpResponse {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var response uhpResponse
	for time.Now().Before(deadline) {
		response = uhpTestResponse(t, uhpTestCall(h, "GET", "/v1/responses/"+id, "", token))
		if response.Status == status {
			return response
		}
		if response.Status != "in_progress" {
			t.Fatalf("wanted %s, got terminal %+v", status, response)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("wanted %s, got %+v", status, response)
	return response
}

func uhpTestSSE(t *testing.T, w *httptest.ResponseRecorder, start int) []map[string]any {
	t.Helper()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("invalid SSE response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	scanner := bufio.NewScanner(bytes.NewReader(w.Body.Bytes()))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	events := []map[string]any{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event["sequence_number"] != float64(start+len(events)) {
			t.Fatalf("non-contiguous sequence: %v", event)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestUHPHTTPDiscoveryAuthenticationAndVersion(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	discovery := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/uhp", "", ""), 200)
	if discovery["protocol"] != "uhp" || discovery["object"] != "uhp.discovery" || discovery["default_version"] != UHPVersion {
		t.Fatal(discovery)
	}
	caps := discovery["capabilities"].(map[string]any)
	for _, capability := range []string{"streaming", "sessions", "cancellation", "idempotency"} {
		if caps[capability] != true {
			t.Fatalf("missing core capability %s", capability)
		}
	}
	for _, capability := range []string{"harness_management", "plugins", "session_sharing", "memories", "environments"} {
		if caps[capability] != false {
			t.Fatalf("advertised unimplemented capability %s", capability)
		}
	}
	uhpTestError(t, uhpTestCall(h, "GET", "/v1/harnesses", "", ""), 401, "missing_credential")
	for _, auth := range []string{"unknown-token", "Basic token", "Bearer ", "Bearer a b"} {
		header := auth
		if auth == "unknown-token" {
			header = "Bearer " + auth
		}
		uhpTestError(t, uhpTestCall(h, "GET", "/v1/harnesses", "", "", map[string]string{"Authorization": header}), 401, "invalid_credential")
	}
	badVersion := uhpTestCall(h, "GET", "/v1/uhp", "", "", map[string]string{"UHP-Version": "1900-01-01"})
	uhpTestError(t, badVersion, 400, "unsupported_protocol_version")
	var envelope struct {
		Error uhpError `json:"error"`
	}
	if err := json.Unmarshal(badVersion.Body.Bytes(), &envelope); err != nil || !strings.Contains(string(badVersion.Body.Bytes()), UHPVersion) {
		t.Fatalf("supported versions missing: %s %v", badVersion.Body.String(), err)
	}
	harnesses := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/harnesses", "", userTestAdmin), 200)["harnesses"].([]any)
	if len(harnesses) == 0 {
		t.Fatal("no configured harnesses")
	}
	id := harnesses[0].(map[string]any)["id"].(string)
	if !strings.HasPrefix(id, "chrn_") {
		t.Fatal(id)
	}
	models := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/harnesses/"+id+"/models", "", userTestAdmin), 200)
	if models["default"] != "demo-fixture" {
		t.Fatal(models)
	}
	uhpTestError(t, uhpTestCall(h, "GET", "/v1/responses/resp_absent", "", userTestAdmin), 404, "response_not_found")
	uhpTestError(t, uhpTestCall(h, "GET", "/v1/harnesses/chrn_absent", "", userTestAdmin), 404, "harness_not_found")
}

func TestUHPHTTPContinuationUsesOneNativeSessionAndDistinctTasks(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	first := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"first UHP turn"}`, userTestAdmin))
	if first.Status != "completed" {
		t.Fatal(first)
	}
	old := uhpTestRecord(t, m, first.ID)
	before, err := m.Session(old.HostSessionID)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"input":"follow up","previous_response_id":%q}`, first.ID)
	second := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", body, userTestAdmin))
	current := uhpTestRecord(t, m, second.ID)
	after, err := m.Session(current.HostSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "completed" || second.PreviousResponseID == nil || *second.PreviousResponseID != first.ID || first.Metadata["session_id"] != second.Metadata["session_id"] || old.TaskID == current.TaskID || old.HostSessionID != current.HostSessionID || before.ThreadID != after.ThreadID || before.WorkspaceID != after.WorkspaceID {
		t.Fatalf("broken continuation: first=%+v second=%+v before=%+v after=%+v", first, second, before, after)
	}
	if len(m.Tasks()) != 2 || len(m.Sessions()) != 1 {
		t.Fatalf("unexpected native/task copies: tasks=%d sessions=%d", len(m.Tasks()), len(m.Sessions()))
	}
	retained := uhpTestResponse(t, uhpTestCall(h, "GET", "/v1/responses/"+first.ID, "", userTestAdmin))
	if !reflect.DeepEqual(first, retained) {
		t.Fatal("later continuation changed prior response")
	}
	sid := first.Metadata["session_id"].(string)
	turns := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/sessions/"+sid+"/turns", "", userTestAdmin), 200)["turns"].([]any)
	if len(turns) != 2 || turns[0].(map[string]any)["id"] != first.ID || turns[1].(map[string]any)["id"] != second.ID {
		t.Fatalf("bad ordered turns: %v", turns)
	}
	input := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/responses/"+second.ID+"/input_items", "", userTestAdmin), 200)
	if !strings.Contains(fmt.Sprint(input["data"]), "follow up") {
		t.Fatal("input history missing", input)
	}
	uhpTestError(t, uhpTestCall(h, "POST", "/v1/responses", fmt.Sprintf(`{"input":"wrong harness","previous_response_id":%q,"metadata":{"harness_id":"chrn_other"}}`, first.ID), userTestAdmin), 409, "harness_mismatch")
}

func TestUHPHTTPSSEAndStoredResponseHaveOneTerminalProjection(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	w := uhpTestCall(h, "POST", "/v1/responses", `{"input":"stream UHP text","stream":true}`, userTestAdmin)
	events := uhpTestSSE(t, w, 0)
	if len(events) < 4 || events[0]["type"] != "response.created" || events[len(events)-1]["type"] != "response.completed" {
		t.Fatalf("invalid stream lifecycle: %v", events)
	}
	terminals, deltas := 0, 0
	for _, event := range events {
		switch event["type"] {
		case "response.completed", "response.failed", "response.incomplete":
			terminals++
		case "response.output_text.delta":
			deltas++
		}
	}
	if terminals != 1 || deltas < 2 || events[0]["response"].(map[string]any)["status"] != "in_progress" {
		t.Fatalf("terminal/delta/initial snapshot mismatch: terminals=%d deltas=%d", terminals, deltas)
	}
	terminal := events[len(events)-1]["response"].(map[string]any)
	id := terminal["id"].(string)
	stored := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/responses/"+id, "", userTestAdmin), 200)
	if !reflect.DeepEqual(terminal, stored) {
		t.Fatalf("SSE and GET disagree\nSSE: %v\nGET: %v", terminal, stored)
	}
	resumed := uhpTestSSE(t, uhpTestCall(h, "GET", "/v1/responses/"+id+"/events", "", userTestAdmin, map[string]string{"Last-Event-ID": "1"}), 2)
	if !reflect.DeepEqual(resumed, events[2:]) {
		t.Fatal("Last-Event-ID did not replay exact persisted suffix")
	}
	uhpTestError(t, uhpTestCall(h, "GET", "/v1/responses/"+id+"/events", "", userTestAdmin, map[string]string{"Last-Event-ID": "-1"}), 400, "invalid_input")
}

func TestUHPHTTPStreamDisconnectKeepsTaskRunning(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, err := http.NewRequest("POST", srv.URL+"/v1/responses", strings.NewReader(`{"input":"审批","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+userTestAdmin)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("stream refused %d %s", res.StatusCode, body)
	}
	scanner := bufio.NewScanner(res.Body)
	var first map[string]any
	for scanner.Scan() {
		if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
			if err := json.Unmarshal([]byte(data), &first); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	res.Body.Close()
	if first["type"] != "response.created" {
		t.Fatalf("stream buffered/invalid first event: %v (%v)", first, scanner.Err())
	}
	id := first["response"].(map[string]any)["id"].(string)
	record := uhpTestRecord(t, m, id)
	taskWait(t, m, record.TaskID, "waiting")
	current := uhpTestResponse(t, uhpTestCall(h, "GET", "/v1/responses/"+id, "", userTestAdmin))
	if current.Status != "in_progress" {
		t.Fatal("disconnect stopped the task", current)
	}
	uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses/"+id+"/cancel", `{}`, userTestAdmin))
	uhpTestWaitResponse(t, h, id, userTestAdmin, "cancelled")
}

func TestUHPHTTPConcurrentIdempotentRetriesWaitForOriginal(t *testing.T) {
	m := testManager(t)
	pauseSubmissionQueue(t, m)
	h := NewHandler(m, userTestAdmin, true)
	headers := map[string]string{"Idempotency-Key": "uhp-single-execution"}
	first := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"original task","background":true}`, userTestAdmin, headers))
	if first.Status != "in_progress" {
		t.Fatal(first)
	}
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- uhpTestCall(h, "POST", "/v1/responses", `{"input":"retry must not replace original","background":true}`, userTestAdmin, headers)
		}()
	}
	select {
	case early := <-results:
		t.Fatalf("in-flight retry returned before completion: %d %s", early.Code, early.Body.String())
	case <-time.After(150 * time.Millisecond):
	}
	if len(m.Tasks()) != 1 {
		t.Fatal("retry started a second execution")
	}
	q := m.Queue()
	q.Paused = false
	if _, err := m.SaveQueue(q); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		select {
		case result := <-results:
			response := uhpTestResponse(t, result)
			if response.ID != first.ID || response.Status != "completed" {
				t.Fatal("retry did not return original result", response)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("retry never completed")
		}
	}
	wg.Wait()
	if len(m.Tasks()) != 1 {
		t.Fatal("idempotency spawned another task")
	}
	stored := uhpTestRecord(t, m, first.ID)
	if string(stored.Input) != `"original task"` {
		t.Fatal("retry changed original input", string(stored.Input))
	}
}

func TestUHPHTTPBusyCancellationAndTimeout(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	first := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"审批","background":true}`, userTestAdmin))
	record := uhpTestRecord(t, m, first.ID)
	taskWait(t, m, record.TaskID, "waiting")
	uhpTestError(t, uhpTestCall(h, "POST", "/v1/responses", fmt.Sprintf(`{"input":"busy followup","previous_response_id":%q}`, first.ID), userTestAdmin), 409, "session_busy")
	uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses/"+first.ID+"/cancel", `{}`, userTestAdmin))
	cancelled := uhpTestWaitResponse(t, h, first.ID, userTestAdmin, "cancelled")
	again := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses/"+first.ID+"/cancel", `{}`, userTestAdmin))
	if !reflect.DeepEqual(cancelled, again) {
		t.Fatal("terminal cancel changed response")
	}
	cancelEvents := uhpTestSSE(t, uhpTestCall(h, "GET", "/v1/responses/"+first.ID+"/events", "", userTestAdmin), 0)
	last := cancelEvents[len(cancelEvents)-1]
	if last["type"] != "response.failed" || last["response"].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancel SSE status wrong: %v", last)
	}
	follow := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", fmt.Sprintf(`{"input":"continue after cancellation","previous_response_id":%q,"background":true,"timeout_seconds":1}`, first.ID), userTestAdmin))
	incomplete := uhpTestWaitResponse(t, h, follow.ID, userTestAdmin, "incomplete")
	if incomplete.IncompleteDetails["reason"] != "timeout" || len(incomplete.Output) == 0 {
		t.Fatalf("timeout discarded partial work or wrong reason: %+v", incomplete)
	}
	if incomplete.Metadata["session_id"] != first.Metadata["session_id"] {
		t.Fatal("cancellation lost continuable session")
	}
}

func TestUHPHTTPDeletingResponseDoesNotCancelTask(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"审批","background":true}`, userTestAdmin))
	record := uhpTestRecord(t, m, response.ID)
	taskWait(t, m, record.TaskID, "waiting")
	deleted := uhpTestJSON(t, uhpTestCall(h, "DELETE", "/v1/responses/"+response.ID, "", userTestAdmin), 200)
	if deleted["deleted"] != true {
		t.Fatal(deleted)
	}
	uhpTestError(t, uhpTestCall(h, "GET", "/v1/responses/"+response.ID, "", userTestAdmin), 404, "response_not_found")
	task, err := m.Task(record.TaskID)
	if err != nil || task.Status != "waiting" || task.CancelRequested {
		t.Fatalf("history deletion cancelled task: %+v %v", task, err)
	}
	sid := response.Metadata["session_id"].(string)
	uhpTestJSON(t, uhpTestCall(h, "POST", "/v1/sessions/"+sid+"/cancel", `{}`, userTestAdmin), 200)
	taskWait(t, m, record.TaskID, "interrupted")
}

func TestUHPHTTPCancellationPreservesPartialText(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"partial output cancellation","background":true}`, userTestAdmin))
	textOf := func(r uhpResponse) string {
		var text strings.Builder
		for _, item := range r.Output {
			if item["type"] != "message" {
				continue
			}
			parts, _ := item["content"].([]any)
			for _, value := range parts {
				part, _ := value.(map[string]any)
				chunk, _ := part["text"].(string)
				text.WriteString(chunk)
			}
		}
		return text.String()
	}
	partial := ""
	deadline := time.Now().Add(5 * time.Second)
	for partial == "" && time.Now().Before(deadline) {
		current := uhpTestResponse(t, uhpTestCall(h, "GET", "/v1/responses/"+response.ID, "", userTestAdmin))
		partial = textOf(current)
		if current.Status != "in_progress" {
			t.Fatal("task reached terminal before partial text could be observed")
		}
		if partial == "" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if partial == "" {
		t.Fatal("no progressive text observed")
	}
	uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses/"+response.ID+"/cancel", `{}`, userTestAdmin))
	cancelled := uhpTestWaitResponse(t, h, response.ID, userTestAdmin, "cancelled")
	if !strings.HasPrefix(textOf(cancelled), partial) {
		t.Fatalf("cancel discarded partial text %q: %+v", partial, cancelled)
	}
}

func TestUHPHTTPInvalidRequestsAndIgnoredFields(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	for _, body := range []string{``, `{`, `{}`, `{"input":null}`, `{"input":42}`, `{"input":""}`, `{"input":"valid","stream":"true"}`, `{"input":"valid","timeout_seconds":0}`, `{"input":"valid","metadata":{"harness_id":3}}`} {
		uhpTestError(t, uhpTestCall(h, "POST", "/v1/responses", body, userTestAdmin), 400, "invalid_input")
	}
	uhpTestError(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"valid","model":"never-existing-model"}`, userTestAdmin), 422, "model_unavailable")
	uhpTestError(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"valid","previous_response_id":"resp_unknown"}`, userTestAdmin), 404, "response_not_found")
	if len(m.Tasks()) != 0 {
		t.Fatal("invalid requests admitted tasks")
	}
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"observable ignored fields","tools":[],"include":[],"store":false,"future_feature":{"foo":1}}`, userTestAdmin))
	ignored, ok := response.Metadata["ignored_fields"].([]any)
	if !ok || !reflect.DeepEqual(ignored, []any{"future_feature", "include", "store", "tools"}) {
		t.Fatalf("ignored fields inaccurate: %v", response.Metadata)
	}
	if !response.Store {
		t.Fatal("retained audit result incorrectly claims store:false was honored")
	}
	retained := uhpTestResponse(t, uhpTestCall(h, "GET", "/v1/responses/"+response.ID, "", userTestAdmin))
	if !reflect.DeepEqual(response, retained) {
		t.Fatal("ignored retention request changed retained result")
	}
}

func TestUHPHTTPCrossPrincipalObjectsStayHidden(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	app, key, token := setupKey(t, m, "uhp-owner", wid, "read", "run", "files")
	_, other, err := m.CreateApplicationKey(app.AppID, KeyInput{Name: "other principal", WorkspaceIDs: []string{wid}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"owned response"}`, token))
	sid := response.Metadata["session_id"].(string)
	for _, principal := range []string{other, userTestAdmin} {
		for _, route := range []struct{ method, path, body, code string }{
			{"GET", "/v1/responses/" + response.ID, "", "response_not_found"},
			{"DELETE", "/v1/responses/" + response.ID, "", "response_not_found"},
			{"POST", "/v1/responses/" + response.ID + "/cancel", `{}`, "response_not_found"},
			{"POST", "/v1/responses", fmt.Sprintf(`{"input":"foreign continue","previous_response_id":%q}`, response.ID), "response_not_found"},
			{"GET", "/v1/sessions/" + sid, "", "session_not_found"},
			{"GET", "/v1/sessions/" + sid + "/turns", "", "session_not_found"},
			{"GET", "/v1/sessions/" + sid + "/files", "", "session_not_found"},
		} {
			uhpTestError(t, uhpTestCall(h, route.method, route.path, route.body, principal), 404, route.code)
		}
		list := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/sessions", "", principal), 200)["sessions"].([]any)
		if len(list) != 0 {
			t.Fatal("session list leaked another principal's object", list)
		}
	}
	files := uhpTestJSON(t, uhpTestCall(h, "GET", "/v1/sessions/"+sid+"/files", "", token), 200)["files"].([]any)
	if len(files) == 0 {
		t.Fatal("native demo artifact was not discovered")
	}
	file := files[0].(map[string]any)
	path := fmt.Sprintf("/v1/containers/%s/files/%s/content", file["container_id"], file["id"])
	uhpTestError(t, uhpTestCall(h, "GET", path, "", other), 404, "file_not_found")
	if download := uhpTestCall(h, "GET", path, "", token); download.Code != 200 || !strings.Contains(download.Body.String(), "RunDesk") || download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("artifact unavailable: %d %s", download.Code, download.Body.String())
	}
	if _, err := m.RevokeApplicationKey(app.AppID, key.ID); err != nil {
		t.Fatal(err)
	}
	uhpTestError(t, uhpTestCall(h, "GET", "/v1/responses/"+response.ID, "", token), 401, "invalid_credential")
}

func TestUHPHTTPFileOperationsRespectApplicationScope(t *testing.T) {
	m := testManager(t)
	_, _, token := setupKey(t, m, "uhp-no-files", m.Workspaces()[0].ID, "read", "run")
	h := NewHandler(m, userTestAdmin, true)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "private.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "private file content"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	w := uhpTestCall(h, "POST", "/v1/files", body.String(), token, map[string]string{"Content-Type": writer.FormDataContentType()})
	uhpTestError(t, w, 403, "insufficient_scope")
}

func TestUHPHTTPReceiptPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	headers := map[string]string{"Idempotency-Key": "surviving-uhp-receipt"}
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"retained result"}`, userTestAdmin, headers))
	m.Close()
	m, err = New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	h = NewHandler(m, userTestAdmin, true)
	replayed := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"must not run again"}`, userTestAdmin, headers))
	if !reflect.DeepEqual(response, replayed) || len(m.Tasks()) != 1 {
		t.Fatalf("restart lost receipt or duplicated task: old=%+v new=%+v tasks=%d", response, replayed, len(m.Tasks()))
	}
	uhpTestJSON(t, uhpTestCall(h, "DELETE", "/v1/responses/"+response.ID, "", userTestAdmin), 200)
	deletedReplay := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"deleted history must not permit reexecution"}`, userTestAdmin, headers))
	if !reflect.DeepEqual(response, deletedReplay) || len(m.Tasks()) != 1 {
		t.Fatal("deleting history invalidated idempotency protection")
	}
}

func TestUHPHTTPCorruptReceiptFailsClosed(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	const key = "corrupt-uhp-receipt"
	if err := m.Store.Put("uhp-receipt", uhpReceiptID("administrator", key), "not a receipt object"); err != nil {
		t.Fatal(err)
	}
	w := uhpTestCall(h, "POST", "/v1/responses", `{"input":"must not execute"}`, userTestAdmin, map[string]string{"Idempotency-Key": key})
	uhpTestError(t, w, 500, "rundesk_storage_error")
	if len(m.Tasks()) != 0 {
		t.Fatal("unreadable idempotency receipt admitted a duplicate task")
	}
}
