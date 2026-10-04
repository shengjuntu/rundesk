package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func libraryUpload(t *testing.T, h http.Handler, p, token, name, text string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, e := form.CreateFormFile("file", name)
	if e != nil {
		t.Fatal(e)
	}
	f.Write([]byte(text))
	form.Close()
	r := httptest.NewRequest("POST", "http://localhost"+p, &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestPersonalLibraryOwnershipAndDeletion(t *testing.T) {
	m, u, code, _, other, shared, _ := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	r := libraryUpload(t, h, "/api/v1/library", code, "notes.txt", "private research")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var f PersonalFile
	json.Unmarshal(r.Body.Bytes(), &f)
	if f.Owner != u.ID {
		t.Fatal("wrong owner", f)
	}
	for _, token := range []string{other, userTestAdmin} {
		r = appRequest(h, "GET", "/api/v1/library/"+f.ID+"/content", "", token, "")
		if r.Code != 404 {
			t.Fatal("private file leaked", r.Code)
		}
		r = appRequest(h, "DELETE", "/api/v1/library/"+f.ID, "", token, "")
		if r.Code != 404 {
			t.Fatal("foreign delete", r.Code)
		}
	}
	r = appRequest(h, "GET", "/api/v1/library", "", other, "")
	if r.Code != 200 || strings.Contains(r.Body.String(), "notes.txt") {
		t.Fatal("foreign list", r.Body.String())
	}
	attached, e := m.AttachPersonal(u.ID, f.ID, shared.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	p := attached["path"].(string)
	ws, _ := m.Workspace(shared.WorkspaceID)
	b, e := os.ReadFile(filepath.Join(ws.Path, p))
	if e != nil || string(b) != "private research" {
		t.Fatal(e, string(b))
	}
	second, e := m.CreateWorkspace("another project", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.AttachPersonal(u.ID, f.ID, second.ID); e != nil {
		t.Fatal("cross session workspace reference failed", e)
	}
	r = appRequest(h, "DELETE", "/api/v1/library/"+f.ID, "", code, "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = appRequest(h, "GET", "/api/v1/library/"+f.ID+"/content", "", code, "")
	if r.Code != 410 {
		t.Fatal("deleted content served", r.Code)
	}
	if _, e = m.AttachPersonal(u.ID, f.ID, shared.WorkspaceID); e == nil {
		t.Fatal("deleted file attached")
	}
	if e = m.validateInput(ws, Input{Files: []string{p}}); e == nil {
		t.Fatal("deleted attachment accepted")
	}
	r = appRequest(h, "GET", "/api/v1/member/"+u.Grants[0].ID+"/workspaces/"+ws.ID+"/file?path="+p, "", code, "")
	if r.Code != 410 {
		t.Fatal("historical link not deleted", r.Code)
	}
	r = appRequest(h, "GET", "/api/v1/library/deleted-references", "", code, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), p) {
		t.Fatal("deleted reference missing", r.Body.String())
	}
	if _, e = os.Stat(m.libraryPath(f)); !os.IsNotExist(e) {
		t.Fatal("personal bytes retained")
	}
}
func TestPersonalUploadAutoSaveAndApplicationDenied(t *testing.T) {
	m, u, code, _, _, shared, _ := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	r := libraryUpload(t, h, "/api/v1/member/"+u.Grants[0].ID+"/workspaces/"+shared.WorkspaceID+"/uploads", code, "photo.txt", "uploaded")
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	files, e := m.PersonalFiles(u.ID, "")
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	_, token, e := m.CreateApplicationKey("news-members", KeyInput{Name: "no-library", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"/api/v1/library", "/api/v1/library/" + files[0].ID + "/content", "/api/v1/library/deleted-references"} {
		r = appRequest(h, "GET", p, "", token, "")
		if r.Code != 403 {
			t.Fatal("application accessed personal library", p, r.Code)
		}
	}
}
func TestGeneratedPersonalFilesSurviveSessionAndDoNotResurrect(t *testing.T) {
	m, u, _, v, _, s, _ := userFixture(t)
	ws, _ := m.Workspace(s.WorkspaceID)
	dir := filepath.Join(ws.Path, "outputs", s.ID)
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0600)
	s.RunID = "run-owner-a"
	if e := m.preparePersonalRun(s, s.RunID, u.ID); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0600)
	m.capturePersonalRun(s)
	files, e := m.PersonalFiles(u.ID, "")
	if e != nil || len(files) != 1 || files[0].Name != "new.txt" {
		t.Fatal(files, e)
	}
	first := files[0]
	s.RunID = "run-owner-b"
	if e = m.preparePersonalRun(s, s.RunID, v.ID); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("changed by B"), 0600)
	m.capturePersonalRun(s)
	files, e = m.PersonalFiles(v.ID, "")
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	a, e := os.ReadFile(m.libraryPath(first))
	if e != nil || string(a) != "new" {
		t.Fatal("old personal file changed", e)
	}
	if e = m.DeletePersonal(v.ID, files[0].ID); e != nil {
		t.Fatal(e)
	}
	m.capturePersonalRun(s)
	files, e = m.PersonalFiles(v.ID, "")
	if e != nil || len(files) != 0 {
		t.Fatal("deleted generated file resurrected", files, e)
	}
	if e = m.DeleteSession(s.ID); e != nil {
		t.Fatal(e)
	}
	a, e = os.ReadFile(m.libraryPath(first))
	if e != nil || string(a) != "new" {
		t.Fatal("session deletion removed personal file", e)
	}
}

func TestPersonalQueueOwnerPersists(t *testing.T) {
	m, u, _, _, _, s, _ := userFixture(t)
	q := m.Queue()
	q.Paused = true
	if _, e := m.SaveQueue(q); e != nil {
		t.Fatal(e)
	}
	task, e := m.Enqueue(TaskSpec{WorkspaceID: s.WorkspaceID, InstanceID: s.InstanceID, Source: s.Source, Input: Input{Text: "queued personal task", LibraryOwner: u.ID}})
	if e != nil {
		t.Fatal(e)
	}
	var stored Task
	if e = m.Store.Get("task", task.ID, &stored); e != nil {
		t.Fatal(e)
	}
	if stored.FileOwner != u.ID {
		t.Fatal("queued owner lost", stored.FileOwner)
	}
}
