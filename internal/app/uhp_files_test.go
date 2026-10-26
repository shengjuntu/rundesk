package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uhpFileErrorCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var api *apiError
	if !errors.As(err, &api) || api.Status != status || api.Code != code {
		t.Fatalf("wanted %d %s, got %v", status, code, err)
	}
}

func TestUHPUploadMaterializesOnceAndChecksOwner(t *testing.T) {
	m := testManager(t)
	f := uhpFiles{Manager: m}
	w := m.Workspaces()[0]
	r, err := f.Upload("actor-a", "../../report.txt", strings.NewReader("input data"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Filename != "report.txt" || r.Bytes != 10 || !strings.HasPrefix(r.ID, "file_") {
		t.Fatalf("bad upload: %+v", r)
	}
	for _, secret := range []string{"owner", "path", "workspaceId", "sha256"} {
		if _, ok := r.Wire()[secret]; ok {
			t.Fatalf("internal field exposed: %s", secret)
		}
	}
	_, err = f.PrepareInput("actor-b", w.ID, r.ID)
	uhpFileErrorCode(t, err, 404, "file_not_found")
	p1, err := f.PrepareInput("actor-a", w.ID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := f.PrepareInput("actor-a", w.ID, r.ID)
	if err != nil || p1 != p2 {
		t.Fatalf("attachment was duplicated: %q %q %v", p1, p2, err)
	}
	data, err := os.ReadFile(filepath.Join(w.Path, filepath.FromSlash(p1)))
	if err != nil || string(data) != "input data" {
		t.Fatalf("attachment mismatch: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, filepath.FromSlash(p1)), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = f.PrepareInput("actor-a", w.ID, r.ID)
	uhpFileErrorCode(t, err, 409, "rundesk_file_changed")
}

func TestUHPInlineNoManagedCopyAndInvalidEncodingCleanup(t *testing.T) {
	m := testManager(t)
	f := uhpFiles{Manager: m}
	w := m.Workspaces()[0]
	p, err := f.Inline("actor", w.ID, "small.txt", "data:text/plain;base64,"+base64.StdEncoding.EncodeToString([]byte("inline")))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(w.Path, filepath.FromSlash(p)))
	if err != nil || string(data) != "inline" {
		t.Fatalf("inline bytes missing: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(m.Data, "uhp", "uploads")); !os.IsNotExist(err) {
		t.Fatalf("inline created a second managed copy: %v", err)
	}
	for _, url := range []string{"https://internal.invalid/file", "data:text/plain,not-base64", "data:text/plain;base64,!!!"} {
		_, err := f.Inline("actor", w.ID, "bad.txt", url)
		uhpFileErrorCode(t, err, 400, "invalid_input")
	}
	entries, err := os.ReadDir(filepath.Join(w.Path, "uploads"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed inline left bytes behind: %d %v", len(entries), err)
	}
}

func TestUHPImageBytesSelectNativeType(t *testing.T) {
	m := testManager(t)
	f := uhpFiles{Manager: m}
	w := m.Workspaces()[0]
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aXioAAAAASUVORK5CYII="
	png, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Upload("actor", "image.bin", bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.PrepareImageInput("actor", w.ID, r.ID)
	if err != nil || filepath.Ext(p) != ".png" {
		t.Fatalf("image not normalized for native runtime: %q %v", p, err)
	}
	p2, err := f.PrepareImageInput("actor", w.ID, r.ID)
	if err != nil || p != p2 {
		t.Fatalf("image rematerialized: %q %q %v", p, p2, err)
	}
	inline, err := f.InlineImage("actor", w.ID, "mislabelled.jpg", "data:image/jpeg;base64,"+pngBase64)
	if err != nil || filepath.Ext(inline) != ".png" {
		t.Fatalf("image metadata overrode actual bytes: %q %v", inline, err)
	}
	bad, err := f.Upload("actor", "pretend.png", strings.NewReader("not image data"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.PrepareImageInput("actor", w.ID, bad.ID)
	uhpFileErrorCode(t, err, 422, "rundesk_image_unsupported")
	_, err = f.InlineImage("actor", w.ID, "remote.png", "https://example.invalid/image.png")
	uhpFileErrorCode(t, err, 400, "invalid_input")
}

type uhpZeroReader struct{}

func (uhpZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestUHPFileSizeLimitDoesNotTruncate(t *testing.T) {
	m := testManager(t)
	f := uhpFiles{Manager: m}
	_, err := f.Upload("actor", "too-big.bin", io.LimitReader(uhpZeroReader{}, UHPFileLimit+1))
	uhpFileErrorCode(t, err, 413, "file_too_large")
	entries, err := os.ReadDir(filepath.Join(m.Data, "uhp", "uploads"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("oversized upload retained: %d %v", len(entries), err)
	}
	rows, err := m.Store.List("uhp-file")
	if err != nil || len(rows) != 0 {
		t.Fatalf("oversized upload registered: %d %v", len(rows), err)
	}
}

func TestUHPArtifactsReferencesAllTurnsScopeAndMutation(t *testing.T) {
	m := testManager(t)
	f := uhpFiles{Manager: m}
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "files", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(w.Path, "outputs", s.ID)
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	one, err := f.DiscoverArtifacts("actor", "hsessone", s.ID, "cntr_one")
	if err != nil || len(one) != 1 {
		t.Fatalf("first discovery: %+v %v", one, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "second.txt"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	two, err := f.DiscoverArtifacts("actor", "hsessone", s.ID, "cntr_one")
	if err != nil || len(two) != 2 || two[0].ID != one[0].ID || two[1].Filename != "nested/second.txt" {
		t.Fatalf("earlier task file missing or reidentified: %+v %v", two, err)
	}
	for _, pair := range [][3]string{{"other", "cntr_one", one[0].ID}, {"actor", "cntr_other", one[0].ID}, {"actor", "cntr_one", "file_../../secret"}, {"actor", "cntr_../secret", one[0].ID}} {
		_, _, _, err := f.Content(pair[0], pair[1], pair[2])
		uhpFileErrorCode(t, err, 404, "file_not_found")
	}
	_, src, cleanup, err := f.Content("actor", "cntr_one", one[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(src)
	if err != nil || !bytes.Equal(data, []byte("first")) {
		t.Fatalf("download changed during response: %q %v", data, err)
	}
	_, _, _, err = f.Content("actor", "cntr_one", one[0].ID)
	uhpFileErrorCode(t, err, 409, "rundesk_file_changed")
	changed, err := f.DiscoverArtifacts("actor", "hsessone", s.ID, "cntr_one")
	if err != nil || len(changed) != 3 {
		t.Fatalf("replacement lost earlier identity: %+v %v", changed, err)
	}
	oldRetained, replacement := false, ""
	for _, item := range changed {
		if item.ID == one[0].ID {
			oldRetained = item.Availability == "changed"
		} else if item.Filename == "first.txt" && item.Availability == "available" {
			replacement = item.ID
		}
	}
	if !oldRetained || replacement == "" {
		t.Fatalf("historical/current identities not distinguished: %+v", changed)
	}
	if err := os.Remove(filepath.Join(dir, "nested", "second.txt")); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.DiscoverArtifacts("actor", "hsessone", s.ID, "cntr_one")
	if err != nil || len(deleted) != 3 || deleted[2].Availability != "missing" {
		t.Fatalf("deleted original disappeared from history: %+v %v", deleted, err)
	}
	if _, err := os.Stat(filepath.Join(m.Data, "uhp", "uploads")); !os.IsNotExist(err) {
		t.Fatalf("artifacts were copied into managed uploads: %v", err)
	}
	if err := f.DeleteSession("actor", "hsessone"); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = f.Content("actor", "cntr_one", changed[0].ID)
	uhpFileErrorCode(t, err, 404, "file_not_found")
	if _, err := os.Stat(filepath.Join(dir, "first.txt")); err != nil {
		t.Fatalf("reference revocation unexpectedly removed task resources: %v", err)
	}
}

func TestUHPArtifactsRejectSymlinkEscape(t *testing.T) {
	m := testManager(t)
	f := uhpFiles{Manager: m}
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "files", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(w.Path, "outputs", s.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, err := f.DiscoverArtifacts("actor", "hsessone", s.ID, "cntr_one")
	if err != nil || len(files) != 0 {
		t.Fatalf("symlink became artifact: %+v %v", files, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), dir); err != nil {
		t.Fatal(err)
	}
	_, err = f.DiscoverArtifacts("actor", "hsessone", s.ID, "cntr_one")
	uhpFileErrorCode(t, err, 409, "rundesk_file_source_invalid")
}
