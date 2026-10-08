package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestApplicationFileDownloadsAreScopedWithinSharedWorkspace(t *testing.T) {
	m := testManager(t)
	ws := m.Workspaces()[0]
	a, _, secret := setupKey(t, m, "file-a", ws.ID, "read", "files", "run", "schedules")
	b, _, other := setupKey(t, m, "file-b", ws.ID, "read", "files")
	own, e := m.CreateSessionWithSource(ws.ID, "own", "", a.InstanceID, SessionSource{Kind: "application", AppID: a.AppID})
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := m.CreateSessionWithSource(ws.ID, "foreign", "", b.InstanceID, SessionSource{Kind: "application", AppID: b.AppID})
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, userTestAdmin, true)
	write := func(p string) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(filepath.Join(ws.Path, p)), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(ws.Path, p), []byte("secret"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ownPath := "outputs/" + own.ID + "/report.txt"
	foreignPath := "outputs/" + foreign.ID + "/report.txt"
	write(ownPath)
	write(foreignPath)
	write("uploads/legacy.txt")
	download := func(prefix, path, token string, want int) {
		t.Helper()
		r := appRequest(h, "GET", prefix+"/workspaces/"+ws.ID+"/file?path="+url.QueryEscape(path), "", token, "")
		if r.Code != want {
			t.Fatalf("%s: %d: %s", path, r.Code, r.Body.String())
		}
	}
	for _, prefix := range []string{"/api", "/api/v1"} {
		download(prefix, ownPath, secret, 200)
		download(prefix, foreignPath, secret, 403)
		download(prefix, ownPath, other, 403)
		download(prefix, "outputs/"+own.ID+"/../"+foreign.ID+"/report.txt", secret, 403)
		download(prefix, "uploads/legacy.txt", secret, 403)
		download(prefix, "uploads/legacy.txt", userTestAdmin, 200)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, e := form.CreateFormFile("file", "input.txt")
	if e != nil {
		t.Fatal(e)
	}
	f.Write([]byte("input"))
	form.Close()
	req := httptest.NewRequest("POST", "http://127.0.0.1:3210/api/v1/workspaces/"+ws.ID+"/uploads", &body)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var uploaded struct {
		Path string `json:"path"`
	}
	if e := json.Unmarshal(rec.Body.Bytes(), &uploaded); e != nil {
		t.Fatal(e)
	}
	download("/api/v1", uploaded.Path, secret, http.StatusOK)
	download("/api/v1", uploaded.Path, other, http.StatusForbidden)
	_, replacement, e := m.CreateApplicationKey(a.AppID, KeyInput{Name: "replacement", WorkspaceIDs: []string{ws.ID}, Scopes: []string{"read", "files"}})
	if e != nil {
		t.Fatal(e)
	}
	download("/api/v1", uploaded.Path, replacement, 200)
	q := m.Queue()
	q.Paused = true
	if _, e := m.SaveQueue(q); e != nil {
		t.Fatal(e)
	}
	for _, route := range []string{"/tasks", "/schedules", "/sessions/" + own.ID + "/turns", "/sessions/" + own.ID + "/steer"} {
		body := `{"text":"x","files":["uploads/legacy.txt"]}`
		if route == "/tasks" {
			body = `{"workspaceId":"` + ws.ID + `","input":` + body + `}`
		}
		if route == "/schedules" {
			body = `{"task":{"workspaceId":"` + ws.ID + `","input":` + body + `}}`
		}
		r := appRequest(h, "POST", "/api/v1"+route, body, secret, "")
		if r.Code != 403 {
			t.Fatalf("foreign input %s: %d %s", route, r.Code, r.Body.String())
		}
	}
	taskBody := `{"workspaceId":"` + ws.ID + `","input":{"text":"x","files":["` + uploaded.Path + `"]}}`
	r := appRequest(h, "POST", "/api/v1/tasks", taskBody, secret, "owned-file-task-0001")
	if r.Code != 202 {
		t.Fatal("own attachment", r.Code, r.Body.String())
	}
	alias := "outputs/" + own.ID + "/alias.txt"
	if err := os.Symlink(filepath.Join(ws.Path, foreignPath), filepath.Join(ws.Path, alias)); err == nil {
		download("/api/v1", alias, secret, 403)
	}
}

func TestMemberFileDownloadUsesApplicationGrant(t *testing.T) {
	m, user, code, viewer, readCode, shared, _ := userFixture(t)
	ws, err := m.Workspace(shared.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	otherApp, _, _ := setupKey(t, m, "member-other", ws.ID, "read", "files")
	foreign, err := m.CreateSessionWithSource(ws.ID, "other app", "", otherApp.InstanceID, SessionSource{Kind: "application", AppID: otherApp.AppID})
	if err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{shared.ID, foreign.ID} {
		dir := filepath.Join(ws.Path, "outputs", sid)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("report"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, actor := range []struct{ grant, token string }{{user.Grants[0].ID, code}, {viewer.Grants[0].ID, readCode}} {
		for _, tc := range []struct {
			sid  string
			want int
		}{{shared.ID, 200}, {foreign.ID, 403}} {
			r := appRequest(h, "GET", "/api/v1/member/"+actor.grant+"/workspaces/"+ws.ID+"/file?path=outputs/"+tc.sid+"/report.txt", "", actor.token, "")
			if r.Code != tc.want {
				t.Fatal(tc.sid, r.Code, r.Body.String())
			}
		}
	}
}
