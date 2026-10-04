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

const userTestAdmin = "administrator-token-for-user-test-123456789"

func userFixture(t *testing.T) (*Manager, User, string, User, string, Session, Session) {
	t.Helper()
	m, _, i, w := dockerTestApp(t)
	a, e := m.RegisterApplication("news-members", ApplicationInput{Name: "News", InstanceID: i.ID, WorkspaceID: w.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.EnsureApplicationEnvironment(i.ID, w.ID); e != nil {
		t.Fatal(e)
	}
	other, e := m.CreateWorkspace("private-project", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.EnsureApplicationEnvironment(i.ID, other.ID); e != nil {
		t.Fatal(e)
	}
	owner, code, e := m.CreateUser(UserInput{Name: "Runner", Grants: []UserGrant{{AppID: a.AppID, WorkspaceID: w.ID, Role: "runner"}}})
	if e != nil {
		t.Fatal(e)
	}
	viewer, readCode, e := m.CreateUser(UserInput{Name: "Viewer", Grants: []UserGrant{{AppID: a.AppID, WorkspaceID: w.ID, Role: "viewer"}}})
	if e != nil {
		t.Fatal(e)
	}
	s, e := m.CreateSessionWithSource(w.ID, "shared task", "", i.ID, SessionSource{Kind: "application", AppID: a.AppID})
	if e != nil {
		t.Fatal(e)
	}
	private, e := m.CreateSessionWithSource(other.ID, "private task", "", i.ID, SessionSource{Kind: "application", AppID: a.AppID})
	if e != nil {
		t.Fatal(e)
	}
	return m, owner, code, viewer, readCode, s, private
}
func TestMemberProjectIsolationAndDefaultDeny(t *testing.T) {
	m, u, code, _, readCode, shared, private := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/member/" + u.Grants[0].ID
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		return appRequest(h, method, path, body, token, "")
	}
	r := call("GET", root+"/sessions", "", code)
	if r.Code != 200 || !strings.Contains(r.Body.String(), shared.ID) || strings.Contains(r.Body.String(), private.ID) {
		t.Fatal("session list leaked", r.Code, r.Body.String())
	}
	for _, path := range []string{"/sessions/" + private.ID, "/sessions/" + private.ID + "/trace", "/sessions/" + private.ID + "/events?stream=1", "/sessions/" + private.ID + "/export", "/sessions/" + private.ID + "/messages/1", "/workspaces/" + private.WorkspaceID + "/file?path=outputs/x"} {
		r = call("GET", root+path, "", code)
		if r.Code != 403 {
			t.Fatal("cross-project access", path, r.Code)
		}
	}
	for _, path := range []string{"/instances", "/users", "/applications", "/environments", "/docker/status", "/sessions", "/queue", "/schedules", "/instances/" + shared.InstanceID + "/images", "/workspaces/" + shared.WorkspaceID + "/skills"} {
		r = call("GET", "/api/v1"+path, "", code)
		if r.Code != 403 {
			t.Fatal("admin endpoint exposed", path, r.Code)
		}
	}
	for _, path := range []string{"/instances", "/tasks", "/schedules", "/users", "/workspaces/" + shared.WorkspaceID + "/account", "/sessions/" + shared.ID + "/configuration"} {
		r = call("GET", root+path, "", code)
		if r.Code != 403 {
			t.Fatal("member proxy escaped allowlist", path, r.Code)
		}
	}
	for _, path := range []string{"/sessions", "/sessions/" + shared.ID + "/turns", "/sessions/" + shared.ID + "/stop", "/workspaces/" + shared.WorkspaceID + "/uploads"} {
		r = call("POST", root+path, `{}`, readCode)
		if r.Code != 403 {
			t.Fatal("viewer wrote", path, r.Code)
		}
	}
	if r = call("DELETE", root+"/sessions/"+shared.ID, "", code); r.Code != 403 {
		t.Fatal("runner deleted shared history", r.Code)
	}
	r = call("GET", "/api/member/"+u.Grants[0].ID+"/sessions/"+private.ID, "", code)
	if r.Code != 403 {
		t.Fatal("legacy alias bypass")
	}
	r = call("GET", "/api/v1/member/othergrant/sessions", "", code)
	if r.Code != 403 {
		t.Fatal("foreign grant accepted")
	}
}
func TestMemberCreationScopesAndIdempotency(t *testing.T) {
	m, u, code, _, _, shared, private := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/member/" + u.Grants[0].ID
	body := `{"workspaceId":"` + shared.WorkspaceID + `","title":"member created"}`
	r := appRequest(h, "POST", root+"/sessions", body, code, "shared-logical-key-001")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	id := object(t, r)["id"].(string)
	again := appRequest(h, "POST", root+"/sessions", body, code, "shared-logical-key-001")
	if object(t, again)["id"] != id {
		t.Fatal("retry duplicated session")
	}
	s, _ := m.Session(id)
	if s.Source.AppID != "news-members" || s.Source.Kind != "application" || s.InstanceID != shared.InstanceID {
		t.Fatal("member source not bound", s)
	}
	r = appRequest(h, "POST", root+"/sessions", `{"workspaceId":"`+private.WorkspaceID+`"}`, code, "foreign-project-key-01")
	if r.Code != 403 {
		t.Fatal("cross-project create accepted", r.Code)
	}
	r = appRequest(h, "POST", root+"/sessions", `{"workspaceId":"`+shared.WorkspaceID+`","source":{"kind":"application","appId":"forged"}}`, code, "forged-source-key-001")
	if r.Code != 403 {
		t.Fatal("source spoof accepted", r.Code)
	}
	other, otherCode, e := m.CreateUser(UserInput{Name: "another runner", Grants: u.Grants})
	if e != nil {
		t.Fatal(e)
	}
	r = appRequest(h, "POST", "/api/v1/member/"+other.Grants[0].ID+"/sessions", body, otherCode, "shared-logical-key-001")
	if r.Code != 200 || object(t, r)["id"] == id {
		t.Fatal("receipt shared across users")
	}
	i, _ := m.Instance(shared.InstanceID)
	if _, e = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "local"}); e != nil {
		t.Fatal(e)
	}
	r = appRequest(h, "POST", root+"/sessions", body, code, "native-mode-key-001")
	if r.Code != 403 {
		t.Fatal("member could execute on host")
	}
}
func userCookieRequest(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:3210"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestUserLoginRevocationRotationAndSecureStorage(t *testing.T) {
	m, u, code, _, _, _, _ := userFixture(t)
	h := NewHandler(m, userTestAdmin, true, "https://desk.example.test")
	// Use a regular origin handler for the fixture; cookie secure is validated separately.
	h = NewHandler(m, userTestAdmin, true)
	body, _ := json.Marshal(map[string]string{"token": code})
	r := userCookieRequest(h, "POST", "/api/v1/login", string(body), nil)
	if r.Code != 200 || len(r.Result().Cookies()) != 1 {
		t.Fatal(r.Code, r.Body.String())
	}
	cookie := r.Result().Cookies()[0]
	if cookie.Value == code || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 43200 {
		t.Fatal("unsafe browser credential", cookie)
	}
	r = userCookieRequest(h, "GET", "/api/v1/whoami", "", cookie)
	if r.Code != 200 || object(t, r)["kind"] != "user" {
		t.Fatal(r.Code, r.Body.String())
	}
	rows, _ := m.Store.List("user")
	rows2, _ := m.Store.List("user-login")
	for _, b := range append(rows, rows2...) {
		if strings.Contains(string(b), code) || strings.Contains(string(b), cookie.Value) {
			t.Fatal("plaintext credential stored")
		}
	}
	p, e := m.authenticateUserCookie(cookie.Value)
	if e != nil {
		t.Fatal(e)
	}
	req := withHuman(httptest.NewRequest("GET", "/", nil), p)
	srv := &Server{Manager: m, Token: userTestAdmin}
	if !srv.userStillValid(req) {
		t.Fatal("valid stream rejected")
	}
	r = userCookieRequest(h, "POST", "/api/v1/logout", "", cookie)
	if r.Code != 200 {
		t.Fatal(r.Code)
	}
	if srv.userStillValid(req) {
		t.Fatal("logout left stream authorized")
	}
	r = userCookieRequest(h, "GET", "/api/v1/whoami", "", cookie)
	if r.Code != 401 {
		t.Fatal("logout cookie reusable")
	}
	u, newCode, e := m.RotateUserCode(u.ID, u.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.authenticateUser(code); e == nil {
		t.Fatal("rotated access code accepted")
	}
	if _, e = m.authenticateUser(newCode); e != nil {
		t.Fatal(e)
	}
	fresh, e := m.loginUser(newCode)
	if e != nil {
		t.Fatal(e)
	}
	u, e = m.UpdateUser(u.ID, UserInput{Name: u.Name, Enabled: false, Revision: u.Revision, Grants: u.Grants})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.authenticateUserCookie(fresh); e == nil {
		t.Fatal("disabled browser session accepted")
	}
	if _, e = m.authenticateUser(newCode); e == nil {
		t.Fatal("disabled access code accepted")
	}
	if _, e = m.UpdateUser(u.ID, UserInput{Name: u.Name, Enabled: true, Revision: u.Revision - 1, Grants: u.Grants}); e == nil {
		t.Fatal("stale revision accepted")
	}
	r = userCookieRequest(NewHandler(m, "", true), "GET", "/api/v1/meta", "", nil)
	if r.Code != 503 {
		t.Fatal("removing admin token opened access", r.Code)
	}
}
func TestUserExpiredLoginAndGrantChange(t *testing.T) {
	m, u, code, _, _, _, _ := userFixture(t)
	cookie, e := m.loginUser(code)
	if e != nil {
		t.Fatal(e)
	}
	id := secretID(cookie, "rd_browser_")
	var rec userLogin
	m.Store.Get("user-login", id, &rec)
	rec.Expires = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	m.Store.Put("user-login", id, rec)
	if _, e = m.authenticateUserCookie(cookie); e == nil {
		t.Fatal("expired login accepted")
	}
	cookie, e = m.loginUser(code)
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.UpdateUser(u.ID, UserInput{Name: u.Name, Enabled: true, Revision: u.Revision, Grants: []UserGrant{}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.authenticateUserCookie(cookie); e == nil {
		t.Fatal("old grant session accepted")
	}
	current, e := m.authenticateUser(code)
	if e != nil || len(current.User.Grants) != 0 {
		t.Fatal("bearer retained removed grant", e)
	}
}

func TestUserRevocationClosesSSE(t *testing.T) {
	m, u, code, _, _, shared, _ := userFixture(t)
	server := httptest.NewServer(NewHandler(m, userTestAdmin, true))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/member/"+u.Grants[0].ID+"/sessions/"+shared.ID+"/events?stream=1", nil)
	req.Header.Set("Authorization", "Bearer "+code)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	_, e = m.UpdateUser(u.ID, UserInput{Name: u.Name, Enabled: false, Revision: u.Revision, Grants: u.Grants})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = io.ReadAll(res.Body); e != nil {
		t.Fatal("revoked user stream did not close", e)
	}
}
