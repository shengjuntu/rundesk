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

func memberPresetFixture(t *testing.T, m *Manager, appID string) (ApplicationRuntimePreset, Session) {
	t.Helper()
	i, err := m.CreateInstance(InstancePatch{Name: "Animation runtime"})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "docker", Image: "video-animation:test"})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := m.CreateWorkspace("Animation project", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureApplicationEnvironment(i.ID, ws.ID); err != nil {
		t.Fatal(err)
	}
	preset, err := m.PutApplicationRuntimePreset(appID, "animated", ApplicationRuntimePresetInput{Name: "Animated", InstanceID: i.ID, WorkspaceID: ws.ID})
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSessionWithSource(ws.ID, "Animation history", "", i.ID, SessionSource{Kind: "application", AppID: appID, PresetID: preset.ID})
	if err != nil {
		t.Fatal(err)
	}
	return preset, session
}

func TestMemberPresetGrantIsExplicitAndKeepsLegacyProjectScope(t *testing.T) {
	m, legacy, legacyCode, _, _, base, _ := userFixture(t)
	preset, animated := memberPresetFixture(t, m, base.Source.AppID)
	if legacy.Grants[0].ID != keyHash(legacy.Grants[0].AppID + "/" + legacy.Grants[0].WorkspaceID)[:24] {
		t.Fatal("legacy grant URL changed")
	}
	grant := UserGrant{AppID: base.Source.AppID, PresetID: preset.ID, WorkspaceID: preset.WorkspaceID, Role: "runner"}
	u, code, err := m.CreateUser(UserInput{Name: "Animation editor", Grants: []UserGrant{grant}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/member/" + u.Grants[0].ID
	legacyRoot := "/api/v1/member/" + legacy.Grants[0].ID
	for _, tc := range []struct {
		path, token, own, foreign string
	}{{root, code, animated.ID, base.ID}, {legacyRoot, legacyCode, base.ID, animated.ID}} {
		r := appRequest(h, "GET", tc.path+"/sessions", "", tc.token, "")
		if r.Code != 200 || !strings.Contains(r.Body.String(), tc.own) || strings.Contains(r.Body.String(), tc.foreign) {
			t.Fatal("preset session scope leaked", r.Code, r.Body.String())
		}
		r = appRequest(h, "GET", tc.path+"/sessions/"+tc.foreign+"/trace", "", tc.token, "")
		if r.Code != 403 {
			t.Fatal("foreign runtime trace readable", r.Code, r.Body.String())
		}
	}
	// Existing member clients only send workspaceId; the sole explicit preset
	// grant must resolve to its frozen pair without falling back to the default.
	body := `{"workspaceId":"` + preset.WorkspaceID + `","title":"member animation"}`
	r := appRequest(h, "POST", root+"/sessions", body, code, "member-animation-create-001")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	created, err := m.Session(object(t, r)["id"].(string))
	if err != nil || created.InstanceID != preset.InstanceID || created.WorkspaceID != preset.WorkspaceID || created.Source.AppID != grant.AppID || created.Source.PresetID != preset.ID {
		t.Fatal("member preset pair was not frozen", created, err)
	}
	for _, body := range []string{
		`{"workspaceId":"` + base.WorkspaceID + `"}`,
		`{"workspaceId":"` + preset.WorkspaceID + `","instanceId":"` + base.InstanceID + `"}`,
	} {
		r = appRequest(h, "POST", root+"/sessions", body, code, "")
		if r.Code != 403 {
			t.Fatal("preset/default cross-product admitted", r.Code, r.Body.String())
		}
	}
	for _, bad := range []UserGrant{
		{AppID: grant.AppID, PresetID: preset.ID, WorkspaceID: base.WorkspaceID, Role: "runner"},
		{AppID: grant.AppID, WorkspaceID: preset.WorkspaceID, Role: "runner"},
		{AppID: grant.AppID, PresetID: "unregistered", WorkspaceID: preset.WorkspaceID, Role: "runner"},
	} {
		if _, _, err := m.CreateUser(UserInput{Name: "Invalid grant", Grants: []UserGrant{bad}}); err == nil {
			t.Fatal("invalid preset grant accepted", bad)
		}
	}
	// A preset grant does not expose administrator-only routes through the proxy.
	for _, path := range []string{"/applications/" + grant.AppID + "/runtime", "/instances", "/tasks", "/environments"} {
		if r := appRequest(h, "GET", root+path, "", code, ""); r.Code != 403 {
			t.Fatal("member preset widened route allowlist", path, r.Code)
		}
	}
	// Docker admission follows this grant's selected runtime, not the default
	// application runtime. A later local-mode change cannot grant host execution.
	baseInstance, err := m.Instance(base.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetExecution(baseInstance.ID, baseInstance.Revision, ExecutionSpec{Mode: "local"}); err != nil {
		t.Fatal(err)
	}
	r = appRequest(h, "POST", root+"/sessions", body, code, "member-animation-create-002")
	if r.Code != 200 {
		t.Fatal("default runtime mode affected the granted preset", r.Code, r.Body.String())
	}
	presetInstance, err := m.Instance(preset.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetExecution(presetInstance.ID, presetInstance.Revision, ExecutionSpec{Mode: "local"}); err != nil {
		t.Fatal(err)
	}
	if r := appRequest(h, "POST", root+"/sessions", body, code, "member-animation-create-003"); r.Code != 403 {
		t.Fatal("preset grant admitted execution on the host", r.Code, r.Body.String())
	}
}

func TestDisabledMemberPresetRetainsHistoryAndBlocksNewExecution(t *testing.T) {
	m, _, _, _, _, base, _ := userFixture(t)
	preset, animated := memberPresetFixture(t, m, base.Source.AppID)
	u, code, err := m.CreateUser(UserInput{Name: "Animation editor", Grants: []UserGrant{{AppID: base.Source.AppID, PresetID: preset.ID, WorkspaceID: preset.WorkspaceID, Role: "runner"}}})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	_, err = m.PutApplicationRuntimePreset(preset.AppID, preset.ID, ApplicationRuntimePresetInput{Name: preset.Name, InstanceID: preset.InstanceID, WorkspaceID: preset.WorkspaceID, Enabled: &disabled, Revision: &preset.Revision})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/member/" + u.Grants[0].ID
	if r := appRequest(h, "GET", root+"/sessions/"+animated.ID, "", code, ""); r.Code != 200 {
		t.Fatal("disabled preset hid its history", r.Code, r.Body.String())
	}
	for _, path := range []string{"/sessions", "/sessions/" + animated.ID + "/turns", "/sessions/" + animated.ID + "/steer", "/sessions/" + animated.ID + "/recover"} {
		body := `{"workspaceId":"` + preset.WorkspaceID + `","text":"continue"}`
		if r := appRequest(h, "POST", root+path, body, code, ""); r.Code != 403 {
			t.Fatal("disabled preset admitted new execution", path, r.Code, r.Body.String())
		}
	}
	catalog, err := m.memberCatalog(u)
	if err != nil || len(catalog) != 1 || catalog[0]["canRun"] != false {
		t.Fatal("disabled preset still advertised runnable", catalog, err)
	}
	// The member proxy must still pass explicit stop requests to the normal
	// authorization gate rather than strand an already running task.
	p, err := m.authenticateUser(code)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	srv := &Server{Manager: m}
	gate := srv.userGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		k := principal(r)
		if k == nil || !k.owns(animated.InstanceID, animated.WorkspaceID, animated.Source) || !k.scope("run") {
			t.Error("stop lost the selected preset grant")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := withHuman(httptest.NewRequest("POST", root+"/sessions/"+animated.ID+"/stop", nil), p)
	// userGate receives the unversioned path after apiBoundary.
	req.URL.Path = strings.Replace(req.URL.Path, "/api/v1/", "/api/", 1)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if !called || rec.Code != http.StatusNoContent {
		t.Fatal("disabled preset prevented stopping old execution", rec.Code)
	}
}
