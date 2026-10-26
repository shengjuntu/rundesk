package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/resource"
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
	var attachBody bytes.Buffer
	form := multipart.NewWriter(&attachBody)
	if e := form.WriteField("libraryFileId", f.ID); e != nil {
		t.Fatal(e)
	}
	if e := form.Close(); e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("POST", "http://localhost/api/v1/member/"+u.Grants[0].ID+"/workspaces/"+shared.WorkspaceID+"/uploads", &attachBody)
	req.Header.Set("Authorization", "Bearer "+code)
	req.Header.Set("Content-Type", form.FormDataContentType())
	r = httptest.NewRecorder()
	h.ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	attached := object(t, r)
	p := attached["path"].(string)
	r = appRequest(h, "GET", "/api/v1/member/"+u.Grants[0].ID+"/workspaces/"+shared.WorkspaceID+"/file?path="+p, "", code, "")
	if r.Code != 200 {
		t.Fatal("own attachment unavailable", r.Code, r.Body.String())
	}
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
	if first.Storage != resource.Reference || first.Source == nil {
		t.Fatal("generated artifact was not registered as a reference", first)
	}
	if _, e = os.Stat(m.libraryPath(first)); !os.IsNotExist(e) {
		t.Fatal("automatic capture copied source bytes", e)
	}
	first, e = m.SnapshotPersonal(u.ID, first.ID)
	if e != nil || first.Storage != resource.Snapshot {
		t.Fatal("explicit archive failed", first, e)
	}
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

func TestPersonalSourceReferenceIdentityAndRemoval(t *testing.T) {
	m, u, code, _, other, session, _ := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	r := libraryUpload(t, h, "/api/v1/member/"+u.Grants[0].ID+"/workspaces/"+session.WorkspaceID+"/uploads", code, "source.txt", "original")
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	files, err := m.PersonalFiles(u.ID, "")
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	f := files[0]
	if f.Storage != resource.Reference || f.Source == nil || f.Source.WorkspaceID != session.WorkspaceID {
		t.Fatal("upload did not retain source identity", f)
	}
	if _, err := os.Stat(m.libraryPath(f)); !os.IsNotExist(err) {
		t.Fatal("uploaded source was duplicated", err)
	}
	r = appRequest(h, "GET", "/api/v1/library/"+f.ID+"/content", "", code, "")
	if r.Code != 200 || r.Body.String() != "original" {
		t.Fatal("reference content unavailable", r.Code, r.Body.String())
	}
	r = appRequest(h, "POST", "/api/v1/library/"+f.ID+"/snapshot", "{}", other, "snapshot-foreign-001")
	if r.Code != 404 {
		t.Fatal("another owner archived a reference", r.Code, r.Body.String())
	}
	attached, err := m.AttachPersonal(u.ID, f.ID, session.WorkspaceID)
	if err != nil || attached["path"] != f.Source.Path || attached["materialized"] != false {
		t.Fatal("same workspace upload was copied", attached, err)
	}
	target, err := m.CreateWorkspace("materialized destination", "")
	if err != nil {
		t.Fatal(err)
	}
	materialized, err := m.AttachPersonal(u.ID, f.ID, target.ID)
	if err != nil || materialized["materialized"] != true {
		t.Fatal("cross-workspace input was not materialized", materialized, err)
	}
	b, err := os.ReadFile(filepath.Join(target.Path, materialized["path"].(string)))
	if err != nil || string(b) != "original" {
		t.Fatal("materialized content mismatch", string(b), err)
	}
	ws, _ := m.Workspace(session.WorkspaceID)
	source := filepath.Join(ws.Path, filepath.FromSlash(f.Source.Path))
	if err := os.WriteFile(source, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	r = appRequest(h, "GET", "/api/v1/library/"+f.ID+"/content", "", code, "")
	if r.Code != 409 || !strings.Contains(r.Body.String(), "personal_file_source_changed") {
		t.Fatal("new bytes served as historical artifact", r.Code, r.Body.String())
	}
	if _, err := m.SnapshotPersonal(u.ID, f.ID); err == nil {
		t.Fatal("changed bytes archived as original")
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	r = appRequest(h, "GET", "/api/v1/library/"+f.ID+"/content", "", code, "")
	if r.Code != 410 || !strings.Contains(r.Body.String(), "personal_file_source_missing") {
		t.Fatal("missing source not reported", r.Code, r.Body.String())
	}
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.DeletePersonal(u.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(source)
	if err != nil || string(b) != "original" {
		t.Fatal("catalog deletion removed source", err, string(b))
	}
	if err := m.validateInput(ws, Input{Files: []string{f.Source.Path}}); err != nil {
		t.Fatal("removing catalog entry revoked original source", err)
	}
	if err := m.validateInput(target, Input{Files: []string{materialized["path"].(string)}}); err == nil {
		t.Fatal("materialized historical attachment ignored deletion")
	}
}

func TestPersonalSnapshotAndLegacyStorage(t *testing.T) {
	m, u, code, _, _, session, _ := userFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	r := libraryUpload(t, h, "/api/v1/member/"+u.Grants[0].ID+"/workspaces/"+session.WorkspaceID+"/uploads", code, "snapshot.txt", "frozen")
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	files, _ := m.PersonalFiles(u.ID, "")
	f := files[0]
	r = appRequest(h, "POST", "/api/v1/library/"+f.ID+"/snapshot", "{}", code, "snapshot-first-001")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var snapshot PersonalFile
	if err := json.Unmarshal(r.Body.Bytes(), &snapshot); err != nil || snapshot.Storage != resource.Snapshot || snapshot.ID != f.ID || snapshot.SnapshotAt == "" {
		t.Fatal("invalid promotion", snapshot, err)
	}
	second, err := m.SnapshotPersonal(u.ID, f.ID)
	if err != nil || second.SnapshotAt != snapshot.SnapshotAt {
		t.Fatal("snapshot retry changed identity", second, err)
	}
	ws, _ := m.Workspace(session.WorkspaceID)
	if err := os.Remove(filepath.Join(ws.Path, filepath.FromSlash(f.Source.Path))); err != nil {
		t.Fatal(err)
	}
	r = appRequest(h, "GET", "/api/v1/library/"+f.ID+"/content", "", code, "")
	if r.Code != 200 || r.Body.String() != "frozen" {
		t.Fatal("snapshot depends on source", r.Code, r.Body.String())
	}
	// Records predating storage policies still own their historical bytes.
	snapshot.Storage, snapshot.Source = "", nil
	if err := m.Store.Put("personal-file", snapshot.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	r = appRequest(h, "GET", "/api/v1/library/"+snapshot.ID+"/content", "", code, "")
	if r.Code != 200 || r.Body.String() != "frozen" {
		t.Fatal("v0.52.0 stored file no longer readable", r.Code, r.Body.String())
	}
	legacy, err := m.ownedFile(u.ID, snapshot.ID)
	if err != nil || legacy.Storage != resource.Managed {
		t.Fatal("legacy record not interpreted as managed", legacy, err)
	}
	legacy, err = m.SnapshotPersonal(u.ID, snapshot.ID)
	if err != nil || legacy.Storage != resource.Managed {
		t.Fatal("legacy snapshot retry changed storage", legacy, err)
	}
}

func TestPersonalReferenceDownloadPinsVerifiedBytes(t *testing.T) {
	m, u, _, _, _, session, _ := userFixture(t)
	ws, _ := m.Workspace(session.WorkspaceID)
	if err := os.MkdirAll(filepath.Join(ws.Path, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws.Path, "uploads", "source.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := m.saveUploaded(u.ID, ws.ID, "uploads/source.txt", "source.txt")
	if err != nil {
		t.Fatal(err)
	}
	download, cleanup, err := m.openPersonalDownload(f)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := os.WriteFile(path, []byte("changed after open"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(download)
	if err != nil || string(b) != "original" {
		t.Fatal("response bytes changed after validation", err, string(b))
	}
	cleanup()
	if _, err := os.Stat(download.Name()); !os.IsNotExist(err) {
		t.Fatal("temporary download bytes retained", err)
	}
	if _, err := os.Stat(m.libraryPath(f)); !os.IsNotExist(err) {
		t.Fatal("download archived a reference implicitly", err)
	}
}

func TestPersonalReferenceRejectsSourceAliasesAndCopyChanges(t *testing.T) {
	m, u, _, _, _, session, _ := userFixture(t)
	ws, _ := m.Workspace(session.WorkspaceID)
	if err := os.MkdirAll(filepath.Join(ws.Path, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws.Path, "uploads", "source.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := m.saveUploaded(u.ID, ws.ID, "uploads/source.txt", "source.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".real", path); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	_, err = m.openPersonalContent(f)
	var api *apiError
	if !errors.As(err, &api) || api.Code != "personal_file_source_invalid" {
		t.Fatal("symlink source accepted", err)
	}
	if err := copyPersonalVerified(io.Discard, strings.NewReader("different"), f); err == nil {
		t.Fatal("copy registered content differing from recorded hash")
	}
	for _, p := range []string{"../outside", "uploads/../../outside", "/absolute", "uploads\\source.txt"} {
		if _, err := m.openArtifactSource(resource.FileSource{WorkspaceID: ws.ID, Path: p}); err == nil {
			t.Fatal("unsafe source accepted", p)
		}
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
