package app

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testBundle() []skillAsset {
	return []skillAsset{{"SKILL.md", []byte("---\nname: research\ndescription: complete skill\n---\nRead references and run scripts."), 0600}, {"scripts/run.sh", []byte("#!/bin/sh\necho fixture\n"), 0700}, {"references/background.md", []byte("Evidence\n"), 0600}, {"assets/template.bin", []byte{0, 1, 2, 255}, 0600}}
}
func zipAssets(t *testing.T, files []skillAsset) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.Path, Method: zip.Deflate}
		h.SetMode(f.Mode)
		w, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		w.Write(f.Data)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestSkillBundleRoundTripAndBackup(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	i, _ := m.CreateInstance(InstancePatch{Name: "App"})
	files := testBundle()
	got, e := m.ImportSkillBundle(wid, "research", files, false, i.ID, "instance")
	if e != nil || len(got.Files) != 4 {
		t.Fatal(got, e)
	}
	if _, e = m.SkillBundle(wid, "research", "default", "instance"); e == nil {
		t.Fatal("bundle crossed configuration scope")
	}
	if _, e = m.SkillBundle(wid, "research", i.ID, "project"); e == nil {
		t.Fatal("bundle crossed project scope")
	}
	data, e := m.ExportSkillBundle(wid, "research", i.ID, "instance")
	if e != nil {
		t.Fatal(e)
	}
	round, e := readBundleZIP(data)
	if e != nil || !reflect.DeepEqual(round, files) {
		t.Fatal("directory bytes or executable flag lost", round, e)
	}
	v, e := m.PreviewSkillFile(wid, "research", "assets/template.bin", i.ID, "instance")
	if e != nil || v.(map[string]any)["binary"] != true {
		t.Fatal(v, e)
	}
	if e = m.SaveSkill(wid, "research", "---\nname: research\n---\nEdited MD", i.ID, "instance"); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(i.CodexHome, "skills/research/scripts/run.sh"))
	if e != nil || !bytes.Equal(b, files[3].Data) && string(b) != "#!/bin/sh\necho fixture\n" {
		t.Fatal("editing MD lost sibling", e)
	}
	_, e = m.ImportSkillBundle(wid, "research", testBundle(), false, i.ID, "instance")
	appCode(t, e, "skill_exists")
	newFiles := []skillAsset{{"SKILL.md", []byte("---\nname: research\n---\nNew"), 0600}, {"scripts/new.py", []byte("print('new')"), 0600}}
	result, e := m.ImportSkillBundle(wid, "research", newFiles, true, i.ID, "instance")
	if e != nil || result.BackupPath == "" {
		t.Fatal(result, e)
	}
	if b, e = os.ReadFile(filepath.Join(i.CodexHome, result.BackupPath, "scripts/run.sh")); e != nil || string(b) != "#!/bin/sh\necho fixture\n" {
		t.Fatal("backup incomplete", e)
	}
	if _, e = os.Stat(filepath.Join(i.CodexHome, "skills/research/scripts/run.sh")); !os.IsNotExist(e) {
		t.Fatal("replace kept stale file")
	}
	backup, e := m.RemoveSkillBundle(wid, "research", i.ID, "instance")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(i.CodexHome, backup, "scripts/new.py")); e != nil {
		t.Fatal("remove failed to backup attached file", e)
	}
	if _, e = m.SkillBundle(wid, "research", i.ID, "instance"); e == nil {
		t.Fatal("removed bundle still visible")
	}
}
func TestSkillBundleRejectsUnsafeArchives(t *testing.T) {
	for _, p := range []string{"../outside", "/absolute", "C:/drive", "scripts/../../secret", "scripts\\secret", "NUL.txt", "scripts/../x"} {
		files := testBundle()
		files = append(files, skillAsset{p, []byte("bad"), 0600})
		if _, e := readBundleZIP(zipAssets(t, files)); e == nil {
			t.Fatal("accepted unsafe path", p)
		}
	}
	files := testBundle()
	files = append(files, skillAsset{"references/link", []byte("/etc/passwd"), os.ModeSymlink | 0777})
	if _, e := readBundleZIP(zipAssets(t, files)); e == nil {
		t.Fatal("accepted symlink")
	}
	files = testBundle()
	files = append(files, skillAsset{"skill.md", []byte("collision"), 0600})
	if _, e := readBundleZIP(zipAssets(t, files)); e == nil {
		t.Fatal("accepted case collision")
	}
	files = []skillAsset{{"a/SKILL.md", testBundle()[0].Data, 0600}, {"b/SKILL.md", testBundle()[0].Data, 0600}}
	if _, e := readBundleZIP(zipAssets(t, files)); e == nil {
		t.Fatal("flattened multiple skills")
	}
	files = testBundle()
	files = append(files, skillAsset{"huge.bin", make([]byte, skillBundleLimit+1), 0600})
	if _, e := readBundleZIP(zipAssets(t, files)); e == nil {
		t.Fatal("accepted zip expansion over limit")
	}
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	i, _ := m.Instance()
	m.ImportSkillBundle(wid, "safe", testBundle(), false, "default", "instance")
	if e := os.Symlink(filepath.Join(t.TempDir(), "secret"), filepath.Join(i.CodexHome, "skills/safe/references/link")); e != nil {
		t.Fatal(e)
	}
	if _, e := m.ExportSkillBundle(wid, "safe", "default", "instance"); e == nil {
		t.Fatal("export followed link")
	}
}
func TestSkillBundleHTTPDirectoryAndZIP(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	wid := m.Workspaces()[0].ID
	base := "/api/v1/workspaces/" + wid + "/skill-bundles"
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, f := range testBundle() {
		w, e := form.CreateFormFile("research/"+f.Path, filepath.Base(f.Path))
		if e != nil {
			t.Fatal(e)
		}
		w.Write(f.Data)
	}
	form.Close()
	r := httptest.NewRequest("POST", "http://127.0.0.1:3210"+base+"?name=research&scope=instance", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:3210"+base+"/research/export?scope=instance", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(w.Code, w.Body.String())
	}
	data, _ := io.ReadAll(w.Result().Body)
	r = httptest.NewRequest("POST", "http://127.0.0.1:3210"+base+"?name=copy&scope=project", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/zip")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, e := m.SkillBundle(wid, "copy", "default", "project")
	if e != nil || len(got.Files) != 4 {
		t.Fatal(got, e)
	}
	auth := NewHandler(m, "private-long-token-1234567890", true)
	r = httptest.NewRequest("GET", "http://127.0.0.1:3210"+base+"/research/export?scope=instance", nil)
	w = httptest.NewRecorder()
	auth.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("bundle export unauthenticated", w.Code)
	}
}
