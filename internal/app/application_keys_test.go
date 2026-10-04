package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func appRequest(h http.Handler, method, path, body, token, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:3210"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func setupKey(t *testing.T, m *Manager, id, wid string, scopes ...string) (Application, ApplicationKey, string) {
	t.Helper()
	i, e := m.CreateInstance(InstancePatch{Name: id})
	if e != nil {
		t.Fatal(e)
	}
	a, e := m.RegisterApplication(id, ApplicationInput{Name: id, InstanceID: i.ID, WorkspaceID: wid})
	if e != nil {
		t.Fatal(e)
	}
	k, secret, e := m.CreateApplicationKey(id, KeyInput{Name: "test", WorkspaceIDs: []string{wid}, Scopes: scopes})
	if e != nil {
		t.Fatal(e)
	}
	return a, k, secret
}
func TestApplicationCredentialsScopeOwnershipAndAliases(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	w := m.Workspaces()[0]
	other, _ := m.CreateWorkspace("other", "")
	a, k, secret := setupKey(t, m, "news", w.ID, "read", "run", "schedules")
	b, _, foreign := setupKey(t, m, "video", other.ID, "read", "run")
	h := NewHandler(m, "admin-secret-01234567890123456789", true)
	call := func(method, path, body, key string) *httptest.ResponseRecorder {
		return appRequest(h, method, "/api/v1"+path, body, secret, key)
	}
	data := `{"workspaceId":"` + w.ID + `","input":{"text":"news research"}}`
	r := call("POST", "/tasks", data, "app-task-key-0001")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	task := object(t, r)
	spec := task["spec"].(map[string]any)
	if spec["instanceId"] != a.InstanceID || spec["source"].(map[string]any)["appId"] != a.AppID {
		t.Fatal(spec)
	}
	human, _ := m.CreateSession(w.ID, "human", "", a.InstanceID)
	for _, p := range []string{"/sessions/" + human.ID, "/sessions/" + task["sessionId"].(string) + "/files", "/workspaces/" + w.ID + "/file?path=outputs/test.txt", "/instances", "/applications", "/queue", "/workspaces/" + w.ID + "/skills", "/sessions/" + task["sessionId"].(string) + "/configuration", "/applications/news/keys"} {
		if r = call("GET", p, "", ""); r.Code != 403 {
			t.Fatal(p, r.Code)
		}
	}
	for _, p := range []string{"/api/v1/tasks/" + task["id"].(string), "/api/tasks/" + task["id"].(string), "/api/v1/sessions/" + task["sessionId"].(string) + "/export"} {
		r = appRequest(h, "GET", p, "", foreign, "")
		if r.Code != 403 {
			t.Fatal(p, r.Code)
		}
	}
	r = call("GET", "/workspaces", "", "")
	var work []map[string]any
	json.Unmarshal(r.Body.Bytes(), &work)
	if len(work) != 1 || work[0]["path"] != nil || work[0]["notes"] != nil {
		t.Fatal(work)
	}
	r = appRequest(h, "GET", "/api/v1/tasks", "", foreign, "")
	if len(object(t, r)["items"].([]any)) != 0 {
		t.Fatal("foreign task leaked")
	}
	for n, body := range []string{`{"workspaceId":"` + w.ID + `","WorkspaceId":"` + other.ID + `","input":{"text":"x"}}`, `{"workspaceId":"` + w.ID + `","instanceId":"` + b.InstanceID + `","input":{"text":"x"}}`, `{"workspaceId":"` + w.ID + `","source":{"kind":"application","appId":"video"},"input":{"text":"x"}}`} {
		if r = call("POST", "/tasks", body, "spoof-task-key-000"+string(rune('a'+n))); r.Code != 403 {
			t.Fatal("spoof accepted", r.Code)
		}
	}
	r = call("POST", "/sessions", `{"workspaceId":"`+w.ID+`","WorkspaceId":"`+other.ID+`"}`, "case-session-001")
	if r.Code != 403 {
		t.Fatal("case alias session", r.Code)
	}
	var stored keyRecord
	if e := m.Store.Get("application-key", k.ID, &stored); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(stored)
	if strings.Contains(string(raw), secret) || stored.Hash == "" {
		t.Fatal("plaintext key persisted")
	}
	list, _ := m.ApplicationKeys(a.AppID)
	raw, _ = json.Marshal(list)
	if strings.Contains(string(raw), stored.Hash) {
		t.Fatal("hash leaked")
	}
}
func TestApplicationCredentialReceiptsRevokeAndCookie(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	w := m.Workspaces()[0]
	a, k, secret := setupKey(t, m, "news", w.ID, "read", "run")
	_, second, e := m.CreateApplicationKey(a.AppID, KeyInput{Name: "rotation", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	admin := "admin-secret-01234567890123456789"
	h := NewHandler(m, admin, true)
	body := `{"workspaceId":"` + w.ID + `","input":{"text":"x"}}`
	first := appRequest(h, "POST", "/api/v1/tasks", body, secret, "same-operation-key")
	next := appRequest(h, "POST", "/api/v1/tasks", body, second, "same-operation-key")
	if first.Code != 202 || next.Code != 202 || object(t, first)["id"] == object(t, next)["id"] {
		t.Fatal("credential receipt namespaces not distinct")
	}
	replay := appRequest(h, "POST", "/api/v1/tasks", body, secret, "same-operation-key")
	if object(t, replay)["id"] != object(t, first)["id"] {
		t.Fatal("same credential replay failed")
	}
	login := request(h, "POST", "/api/v1/login", `{"token":"`+admin+`"}`, "", "")
	cookie := login.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "http://127.0.0.1:3210/api/v1/instances", nil)
	req.AddCookie(cookie)
	req.Header.Set("Authorization", "Bearer "+secret)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatal("admin cookie elevated app key")
	}
	m.RevokeApplicationKey(a.AppID, k.ID)
	if r := appRequest(h, "POST", "/api/v1/tasks", body, secret, "same-operation-key"); r.Code != 401 {
		t.Fatal("revoked replay allowed", r.Code)
	}
	req.Header.Set("Authorization", "Bearer invalid")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 401 {
		t.Fatal("invalid token inherited cookie")
	}
	key, exp, _ := m.CreateApplicationKey(a.AppID, KeyInput{Name: "exp", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read"}, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	m.keyMu.Lock()
	var kr keyRecord
	m.Store.Get("application-key", key.ID, &kr)
	kr.Key.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	m.Store.Put("application-key", key.ID, kr)
	m.keyMu.Unlock()
	if r := appRequest(h, "GET", "/api/v1/whoami", "", exp, ""); r.Code != 401 {
		t.Fatal("expired key accepted")
	}
}
func TestApplicationScopesApprovalAndSSERevocation(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	a, k, secret := setupKey(t, m, "news", w.ID, "read", "run")
	_, readonly, _ := m.CreateApplicationKey(a.AppID, KeyInput{Name: "reader", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read"}})
	h := NewHandler(m, "", true)
	body := `{"workspaceId":"` + w.ID + `","input":{"text":"审批"}}`
	if r := appRequest(h, "POST", "/api/v1/tasks", body, readonly, "readonly-task-001"); r.Code != 403 {
		t.Fatal("read key ran task")
	}
	r := appRequest(h, "POST", "/api/v1/tasks", body, secret, "approval-task-001")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	id := object(t, r)["id"].(string)
	v := taskWait(t, m, id, "waiting")
	approval := m.Approvals(v.SessionID)[0]
	r = appRequest(h, "POST", "/api/v1/sessions/"+v.SessionID+"/approvals/"+approval.ID, `{"decision":"accept"}`, secret, "")
	if r.Code != 403 {
		t.Fatal("approval without scope")
	}
	r = appRequest(h, "POST", "/api/v1/schedules", `{}`, secret, "schedule-denied-001")
	if r.Code != 403 {
		t.Fatal("schedule without scope")
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/sessions/"+v.SessionID+"/events?stream=1", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	m.RevokeApplicationKey(a.AppID, k.ID)
	_, e = io.ReadAll(res.Body)
	if e != nil {
		t.Fatal("revocation did not close stream", e)
	}
	if r = appRequest(h, "GET", "/api/v1/whoami", "", secret, ""); r.Code != 401 {
		t.Fatal("revocation ignored")
	}
	m.CancelTask(id)
}

func TestApplicationScheduleOwnershipAndReceiptRead(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	a, _, secret := setupKey(t, m, "news-schedules", w.ID, "read", "run", "schedules")
	_, _, foreign := setupKey(t, m, "video-schedules", w.ID, "read", "run", "schedules")
	h := NewHandler(m, "", true)
	body := `{"name":"daily","cron":"0 9 * * *","timezone":"Asia/Shanghai","enabled":false,"task":{"workspaceId":"` + w.ID + `","input":{"text":"news"}}}`
	r := appRequest(h, "POST", "/api/v1/schedules", body, secret, "schedule-owner-001")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	obj := object(t, r)
	id := obj["id"].(string)
	spec := obj["spec"].(map[string]any)
	if spec["task"].(map[string]any)["instanceId"] != a.InstanceID {
		t.Fatal("schedule binding absent")
	}
	r = appRequest(h, "GET", "/api/v1/schedules", "", foreign, "")
	if strings.TrimSpace(r.Body.String()) != "[]" {
		t.Fatal("foreign schedule leaked")
	}
	for _, method := range []string{"GET", "DELETE", "PUT"} {
		r = appRequest(h, method, "/api/v1/schedules/"+id, `{"revision":1}`, foreign, "")
		if r.Code != 403 {
			t.Fatal(method, r.Code)
		}
	}
	r = appRequest(h, "GET", "/api/v1/requests/schedule-owner-001", "", foreign, "")
	if r.Code != 404 {
		t.Fatal("foreign receipt exposed", r.Code)
	}
	r = appRequest(h, "GET", "/api/v1/requests/schedule-owner-001", "", secret, "")
	if r.Code != 200 || object(t, r)["state"] != "completed" {
		t.Fatal("own receipt not found")
	}
}
