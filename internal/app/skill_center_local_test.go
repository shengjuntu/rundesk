package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/skillsafety"
)

func skillLocalUpload(t *testing.T, h http.Handler, contentType string, data []byte, suffix string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1:3210/api/v1/skill-center/import"+suffix, bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+userTestAdmin)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func skillLocalResult(t *testing.T, w *httptest.ResponseRecorder) SkillPack {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Pack SkillPack          `json:"pack"`
		Scan skillsafety.Report `json:"scan"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Pack.ID == "" || response.Scan.Digest != response.Pack.Digest {
		t.Fatal("local acquisition lost the bound scan", w.Body.String())
	}
	return response.Pack
}

func skillLocalFolder(t *testing.T, files []skillAsset) (string, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		// Only the form field carries the source path. A hostile browser-provided
		// filename must never become an extraction or source-provenance path.
		part, err := writer.CreateFormFile("research/"+file.Path, "../../ignored-host-file")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.FormDataContentType(), body.Bytes()
}

func TestSkillLocalZIPAndFolderFreezeOneUninstalledSnapshot(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	files := testBundle()
	pack := skillLocalResult(t, skillLocalUpload(t, h, "application/zip", zipAssets(t, files), ""))
	if pack.Name != "research" || pack.SourceURL != "local-upload://research" || pack.Revision != pack.Digest || len(pack.Files) != len(files) {
		t.Fatalf("upload source identity is wrong: %+v", pack)
	}
	if pack.Scan.Status != "incomplete" || pack.Scan.Coverage.FilesSkipped != 1 {
		t.Fatal("binary upload was rejected or promoted to a complete scan", pack.Scan)
	}
	typ, body := skillLocalFolder(t, files)
	copy := skillLocalResult(t, skillLocalUpload(t, h, typ, body, ""))
	if pack.ID != copy.ID || pack.Digest != copy.Digest {
		t.Fatal("equivalent ZIP and directory bytes created different snapshots", pack, copy)
	}
	if rows, err := m.Store.List("skill-center-pack"); err != nil || len(rows) != 1 {
		t.Fatal("acquisition retry duplicated a frozen snapshot", len(rows), err)
	}
	_, frozen, err := m.skillPackAssets(pack.ID)
	if err != nil || len(frozen) != len(files) {
		t.Fatal(frozen, err)
	}
	if !bytes.Equal(frozen[len(frozen)-1].Data, []byte("#!/bin/sh\necho fixture\n")) || frozen[len(frozen)-1].Mode&0111 == 0 {
		t.Fatal("directory attachment or execute bit was lost", frozen)
	}
	wid := m.Workspaces()[0].ID
	skillCenterAssertNotInstalled(t, m, wid, DefaultInstance, "research")
	if _, err := m.SkillBundle(wid, "research", DefaultInstance, "project"); err == nil {
		t.Fatal("acquisition installed into project scope")
	}
}

func TestSkillLocalNameNeverComesFromUploadFilename(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	unnamed := []skillAsset{{Path: "SKILL.md", Data: []byte("---\nname: 中文的复杂名字\ndescription: local fixture\n---\nRead supplied documents."), Mode: 0600}}
	pack := skillLocalResult(t, skillLocalUpload(t, h, "application/zip", zipAssets(t, unnamed), ""))
	if pack.Name != "skill-"+strings.TrimPrefix(pack.Digest, "sha256:")[:12] {
		t.Fatal("unsafe metadata became a path/name", pack.Name)
	}
	for _, bad := range []string{"../escape", "space name", "", strings.Repeat("a", 65)} {
		if bad == "" {
			continue // Omitted or empty name is the documented derived-name path.
		}
		w := skillLocalUpload(t, h, "application/zip", zipAssets(t, unnamed), "?name="+url.QueryEscape(bad))
		if w.Code != 400 || object(t, w)["code"] != "invalid_skill_name" {
			t.Fatal("accepted an unsafe explicit upload name", bad, w.Code, w.Body.String())
		}
	}
	renamed := skillLocalResult(t, skillLocalUpload(t, h, "application/zip", zipAssets(t, unnamed), "?name=local-copy"))
	if renamed.Name != "local-copy" || renamed.SourceURL != "local-upload://local-copy" || renamed.Digest != pack.Digest {
		t.Fatal("explicit name changed source content or leaked a filename", renamed)
	}
}

func TestSkillLocalRejectsUnsafeAndOversizedSourcesBeforeStaging(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	for _, bad := range []skillAsset{
		{Path: "../outside", Data: []byte("bad"), Mode: 0600},
		{Path: "skill.md", Data: []byte("collision"), Mode: 0600},
		{Path: "references/link", Data: []byte("/etc/passwd"), Mode: os.ModeSymlink | 0777},
		{Path: "references/pipe", Mode: os.ModeNamedPipe | 0600},
	} {
		files := append(testBundle(), bad)
		w := skillLocalUpload(t, h, "application/zip", zipAssets(t, files), "")
		if w.Code != 400 {
			t.Fatal("accepted an invalid source", bad.Path, w.Code, w.Body.String())
		}
	}
	files := append(testBundle(), skillAsset{Path: "huge.txt", Data: bytes.Repeat([]byte("x"), skillBundleLimit+1), Mode: 0600})
	w := skillLocalUpload(t, h, "application/zip", zipAssets(t, files), "")
	if w.Code != 413 {
		t.Fatal("accepted an expanding ZIP over the content budget", w.Code, w.Body.String())
	}
	w = skillLocalUpload(t, h, "application/zip", make([]byte, skillLocalUploadLimit+1), "")
	if w.Code != 413 {
		t.Fatal("wire body limit did not stop the upload", w.Code, w.Body.String())
	}
	var folder bytes.Buffer
	form := multipart.NewWriter(&folder)
	part, _ := form.CreateFormFile("../SKILL.md", "safe-looking-name")
	_, _ = io.WriteString(part, "---\nname: research\n---\nBad path")
	_ = form.Close()
	w = skillLocalUpload(t, h, form.FormDataContentType(), folder.Bytes(), "")
	if w.Code != 400 {
		t.Fatal("multipart relative path escaped validation", w.Code)
	}
	if rows, err := m.Store.List("skill-center-pack"); err != nil || len(rows) != 0 {
		t.Fatal("invalid acquisition created metadata", len(rows), err)
	}
}

func TestSkillLocalInstalledSnapshotsRespectScopeAndRemainImmutable(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	i, err := m.CreateInstance(InstancePatch{Name: "Selected local environment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ImportSkillBundle(wid, "research", testBundle(), false, i.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	project := []skillAsset{{Path: "SKILL.md", Data: []byte("---\nname: project-only\n---\nProject fixture"), Mode: 0600}}
	if _, err = m.ImportSkillBundle(wid, "project-only", project, false, i.ID, "project"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	query := "?workspaceId=" + wid + "&instanceId=" + i.ID + "&scope=instance"
	w := skillCenterCall(t, h, "GET", "/installed"+query, nil)
	var list SkillInstalledList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || len(list.Items) != 1 || list.Items[0].Name != "research" || list.Scope != "instance" || list.InstanceID != i.ID {
		t.Fatal("installed candidates crossed scope", w.Code, list, err)
	}
	w = skillCenterCall(t, h, "GET", "/installed?workspaceId="+wid+"&instanceId="+i.ID, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || len(list.Items) != 1 || list.Items[0].Name != "project-only" || list.Scope != "project" {
		t.Fatal("default scope is not the selected project", w.Code, list, err)
	}
	before, err := m.ExportSkillBundle(wid, "research", i.ID, "instance")
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"workspaceId": wid, "instanceId": i.ID, "scope": "instance", "name": "research"}
	pack := skillLocalResult(t, skillCenterCall(t, h, "POST", "/from-installed", input))
	if pack.SourceURL != "rundesk://"+i.ID+"/"+wid+"/instance/research" || pack.Revision != pack.Digest || len(pack.Files) != 4 {
		t.Fatal("installed provenance or attachments were lost", pack)
	}
	after, err := m.ExportSkillBundle(wid, "research", i.ID, "instance")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("snapshot creation modified the installed skill", err)
	}
	if err = m.SaveSkill(wid, "research", "---\nname: research\n---\nEdited after the snapshot", i.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	_, frozen, err := m.skillPackAssets(pack.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frozen {
		if f.Path == "SKILL.md" && bytes.Contains(f.Data, []byte("Edited after")) {
			t.Fatal("installed edits mutated an earlier snapshot")
		}
		if runtime.GOOS != "windows" && f.Path == "scripts/run.sh" && f.Mode&0111 == 0 {
			t.Fatal("snapshot discarded an installed executable bit")
		}
	}
	updated := skillLocalResult(t, skillCenterCall(t, h, "POST", "/from-installed", input))
	if updated.ID == pack.ID || updated.Digest == pack.Digest {
		t.Fatal("changed installed bytes reused the previous snapshot")
	}
	if receipts, err := m.Store.List("skill-center-install"); err != nil || len(receipts) != 0 {
		t.Fatal("snapshotting created a new installation receipt", err)
	}
}

func TestSkillLocalInstalledRejectsLinksAndInvalidSelection(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	i, err := m.CreateInstance(InstancePatch{Name: "Link boundary"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportSkillBundle(wid, "research", testBundle(), false, i.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []map[string]any{
		{"workspaceId": wid, "instanceId": i.ID, "scope": "outside", "name": "research"},
		{"workspaceId": "", "instanceId": i.ID, "scope": "instance", "name": "research"},
		{"workspaceId": wid, "instanceId": i.ID, "scope": "instance", "name": "../research"},
		{"workspaceId": "not-a-workspace", "instanceId": i.ID, "scope": "instance", "name": "research"},
	} {
		w := skillCenterCall(t, NewHandler(m, userTestAdmin, true), "POST", "/from-installed", input)
		if w.Code == 200 {
			t.Fatal("invalid source selection was accepted", input)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("symbolic link fixture requires host support")
	}
	if err := os.Symlink(filepath.Join(i.CodexHome, "skills/research"), filepath.Join(i.CodexHome, "skills/linked")); err != nil {
		t.Fatal(err)
	}
	list, err := m.InstalledSkillCandidates(wid, i.ID, "instance")
	if err != nil || len(list.Items) != 1 || list.Items[0].Name != "research" || len(list.Warnings) == 0 {
		t.Fatal("listing followed a directory link", list, err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "private"), filepath.Join(i.CodexHome, "skills/research/references/link")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SnapshotInstalledSkill(wid, i.ID, "instance", "research"); err == nil {
		t.Fatal("snapshot followed an attachment link")
	}
	if rows, err := m.Store.List("skill-center-pack"); err != nil || len(rows) != 0 {
		t.Fatal("linked or invalid source was staged", err)
	}
}

func TestSkillLocalInstalledListingDisclosesItsLimit(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	i, err := m.CreateInstance(InstancePatch{Name: "Bounded installed listing"})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(i.CodexHome, "skills")
	for n := 0; n < 1001; n++ {
		name := filepath.Join(base, fmt.Sprintf("skill-%04d", n))
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(name, "SKILL.md"), []byte("---\nname: listed\n---\nOnly listing"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.InstalledSkillCandidates(wid, i.ID, "instance")
	if err != nil || len(list.Items) != 1000 || !list.Truncated || len(list.Warnings) == 0 {
		t.Fatalf("a limited directory scan looked complete: items=%d truncated=%v warnings=%v err=%v", len(list.Items), list.Truncated, list.Warnings, err)
	}
	if rows, err := m.Store.List("skill-center-pack"); err != nil || len(rows) != 0 {
		t.Fatal("listing installed candidates created snapshots", err)
	}
}

func TestSkillLocalRoutesAreAdministratorOnly(t *testing.T) {
	m, _, memberToken, _, _, shared, _ := userFixture(t)
	_, appToken, err := m.CreateApplicationKey("news-members", KeyInput{Name: "No local skill acquisition", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	for _, route := range []struct{ method, path string }{{"POST", "/import"}, {"POST", "/from-installed"}, {"GET", "/installed?workspaceId=" + shared.WorkspaceID}} {
		for _, prefix := range []string{"/api", "/api/v1"} {
			for _, token := range []string{memberToken, appToken} {
				w := appRequest(h, route.method, prefix+"/skill-center"+route.path, `{}`, token, "")
				if w.Code != 403 {
					t.Fatal("local skill administration leaked", route, w.Code, w.Body.String())
				}
			}
		}
	}
}
