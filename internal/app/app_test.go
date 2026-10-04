package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__demo_agent" {
		if mode := os.Getenv("RUNDESK_APP_FAILURE_FIXTURE"); mode != "" {
			appFailureFixture(mode)
			os.Exit(0)
		}
		DemoAgent()
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func testManager(t *testing.T) *Manager {
	t.Helper()
	m, e := New(t.TempDir(), "", true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m
}
func waitState(t *testing.T, m *Manager, id string, want string) Session {
	t.Helper()
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		s, e := m.Session(id)
		if e != nil {
			t.Fatal(e)
		}
		if s.Status == want {
			return s
		}
		if s.Status == "failed" {
			t.Fatalf("run failed: %s", s.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := m.Session(id)
	t.Fatalf("wanted %s, got %+v", want, s)
	return s
}
func TestConcurrentSessionsApprovalReplayAndResume(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s1, _ := m.CreateSession(w.ID, "", "")
	s2, _ := m.CreateSession(w.ID, "", "")
	if _, e := m.Start(s1.ID, Input{Text: "审批"}); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s1.ID, "waiting")
	if _, e := m.Start(s2.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	a := m.Approvals(s1.ID)
	if len(a) != 1 || len(m.Approvals(s2.ID)) != 0 {
		t.Fatal("approvals crossed sessions")
	}
	if e := m.Approve(s2.ID, a[0].ID, "accept", nil, nil); e == nil {
		t.Fatal("cross-session approval accepted")
	}
	if e := m.Approve(s1.ID, a[0].ID, "accept", nil, nil); e != nil {
		t.Fatal(e)
	}
	if e := m.Approve(s1.ID, a[0].ID, "accept", nil, nil); e == nil {
		t.Fatal("duplicate approval accepted")
	}
	one := waitState(t, m, s1.ID, "completed")
	waitState(t, m, s2.ID, "completed")
	events, e := m.Store.Events(s1.ID, 0, 1000)
	if e != nil || len(events) < 5 {
		t.Fatal(e)
	}
	pivot := events[len(events)/2].ID
	tail, e := m.Store.Events(s1.ID, pivot, 1000)
	if e != nil || len(tail) != len(events)-len(events)/2-1 {
		t.Fatal("cursor replay mismatch", e)
	}
	for _, ev := range tail {
		if ev.ID <= pivot || ev.SessionID != s1.ID {
			t.Fatal("invalid replay")
		}
	}
	if _, e = m.Start(s1.ID, Input{Text: "continue"}); e != nil {
		t.Fatal(e)
	}
	two := waitState(t, m, s1.ID, "completed")
	if one.ThreadID != two.ThreadID || one.RunID == two.RunID {
		t.Fatal("thread continuity failed")
	}
	evs, _ := m.Store.Events(s1.ID, 0, 1000)
	starts := 0
	for _, ev := range evs {
		if ev.Direction == "out" && ev.Method == "thread/start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("thread restarted %d times", starts)
	}
}
func TestStopWaitingAndRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	m, e := New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "", "")
	if _, e = m.Start(s.ID, Input{Text: "审批"}); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s.ID, "waiting")
	if e = m.Stop(s.ID); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s.ID, "interrupted")
	if len(m.Approvals(s.ID)) != 0 {
		t.Fatal("stale pending approval")
	}
	_ = m.update(s.ID, func(s *Session) { s.Status = "running" })
	m.Close()
	m2, e := New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer m2.Close()
	recovered, e := m2.Session(s.ID)
	if e != nil || recovered.Status != "interrupted" {
		t.Fatal("restart recovery", recovered, e)
	}
}
func TestStopDuringStart(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "", "")
	if _, e := m.Start(s.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	if e := m.Stop(s.ID); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s.ID, "interrupted")
}

func TestReconnectAfterIdleProcessExit(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "", "")
	if _, e := m.Start(s.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	first := waitState(t, m, s.ID, "completed")
	h, _ := m.getHandle(s.ID)
	h.mu.Lock()
	c := h.client
	h.mu.Unlock()
	c.Close()
	<-c.Done()
	if _, e := m.Start(s.ID, Input{Text: "continue"}); e != nil {
		t.Fatal(e)
	}
	second := waitState(t, m, s.ID, "completed")
	if second.ThreadID != first.ThreadID {
		t.Fatal("native thread lost after reconnect")
	}
	events, _ := m.Store.Events(s.ID, 0, 1000)
	resumes := 0
	for _, ev := range events {
		if ev.Direction == "out" && ev.Method == "thread/resume" {
			resumes++
		}
	}
	if resumes != 1 {
		t.Fatalf("expected one resume, got %d", resumes)
	}
}
func request(h http.Handler, method, path, body, token, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:3210"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAuthOriginAndFileBoundaries(t *testing.T) {
	m := testManager(t)
	token := "a-long-test-token-with-32-characters"
	h := NewHandler(m, token, true)
	if r := request(h, "GET", "/api/sessions", "", "", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	if r := request(h, "GET", "/api/sessions", "", token, ""); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(h, "POST", "/api/sessions", "{}", token, "https://evil.example"); r.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	w := m.Workspaces()[0]
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("SECRET"), 0600)
	_ = os.MkdirAll(filepath.Join(w.Path, "uploads"), 0700)
	if e := os.Symlink(outside, filepath.Join(w.Path, "uploads", "escape.txt")); e == nil {
		r := request(h, "GET", "/api/workspaces/"+w.ID+"/file?path=uploads/escape.txt", "", token, "")
		if r.Code == 200 || strings.Contains(r.Body.String(), "SECRET") {
			t.Fatal("symlink escaped root")
		}
	}
	for _, p := range []string{"../secret.txt", "/etc/passwd", "uploads/../secret.txt"} {
		if safePath(p) && !strings.HasPrefix(p, "/") {
			t.Fatalf("unsafe accepted %q", p)
		}
	}
	_ = os.WriteFile(filepath.Join(w.Path, "uploads", "payload.html"), []byte("<script>alert(1)</script>"), 0600)
	r := request(h, "GET", "/api/workspaces/"+w.ID+"/file?path=uploads/payload.html&preview=1", "", token, "")
	if r.Code != 200 || !strings.HasPrefix(r.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("active HTML served inline")
	}
}
func TestSSELastEventID(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "", "")
	first, _ := m.Store.Add(s.ID, "internal", "first", map[string]int{"n": 1})
	second, _ := m.Store.Add(s.ID, "internal", "second", map[string]int{"n": 2})
	ts := httptest.NewServer(NewHandler(m, "", true))
	defer ts.Close()
	r, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+s.ID+"/events?stream=1", nil)
	r.Header.Set("Last-Event-ID", jsonNumber(first.ID))
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data: ") {
			var ev store.Event
			if e = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); e != nil {
				t.Fatal(e)
			}
			if ev.ID != second.ID {
				t.Fatal("SSE replay duplicated or missed cursor")
			}
			return
		}
	}
	t.Fatal("no SSE event")
}
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }
func TestNotesVersionAndMCPSecretPreservation(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	if _, e := m.Notes(w.ID, "first", 0); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Notes(w.ID, "stale", 0); e == nil {
		t.Fatal("stale notes write accepted")
	}
	if _, e := m.SaveMCP(w.ID, "example", "0", map[string]any{"command": "example", "env": map[string]any{"KEY": "test-secret"}}, false); e != nil {
		t.Fatal(e)
	}
	v, e := m.MCP(w.ID)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if bytes.Contains(b, []byte("test-secret")) {
		t.Fatal("secret exposed")
	}
	if _, e = m.SaveMCP(w.ID, "example", "0", map[string]any{"command": "other"}, false); e == nil {
		t.Fatal("stale MCP write accepted")
	}
	if _, e = m.SaveMCP(w.ID, "example", "1", map[string]any{"command": "new", "env": map[string]any{"KEY": "[redacted]"}}, false); e != nil {
		t.Fatal(e)
	}
	c, e := m.readConfig(w.ID)
	if e != nil {
		t.Fatal(e)
	}
	user, _ := userMCP(c)
	config := user["example"].(map[string]any)
	if config["env"].(map[string]any)["KEY"] != "test-secret" {
		t.Fatal("secret was overwritten")
	}
}
func TestHTTPUpload(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	body := "--testboundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"note.txt\"\r\nContent-Type: text/plain\r\n\r\nhello upload\r\n--testboundary--\r\n"
	req := httptest.NewRequest("POST", "http://127.0.0.1:3210/api/workspaces/"+w.ID+"/uploads", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=testboundary")
	r := httptest.NewRecorder()
	h := NewHandler(m, "", true)
	h.ServeHTTP(r, req)
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var f struct{ Path string }
	_ = json.Unmarshal(r.Body.Bytes(), &f)
	resp := request(h, "GET", "/api/workspaces/"+w.ID+"/file?path="+f.Path, "", "", "")
	raw, _ := io.ReadAll(resp.Body)
	if string(raw) != "hello upload" {
		t.Fatal("upload corrupt")
	}
}

func TestExplicitHTTPSProxyOrigin(t *testing.T) {
	m := testManager(t)
	token := "a-long-test-token-with-32-characters"
	h := NewHandler(m, token, true, "https://codex.example.com")
	req := httptest.NewRequest("POST", "http://codex.example.com/api/login", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set("Origin", "https://codex.example.com")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("proxy cookie missing secure flags")
	}
	req = httptest.NewRequest("POST", "http://codex.example.com/api/login", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set("Origin", "https://attacker.example.com")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("unexpected proxy origin accepted")
	}
}
