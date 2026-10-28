package profiletemplates

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRecipe(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "example")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"Dockerfile": "FROM scratch\n", "profile.lock.json": `{"profile":{"id":"example","version":"1.0.0"}}`, "SHA256SUMS": "stale on-disk checksums are regenerated"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RUNDESK_RECIPES_DIR", root)
	return directory
}

func zipFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range archive.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(content)
	}
	return files
}

func TestRecipeChangesAreReadWithoutRestart(t *testing.T) {
	directory := testRecipe(t)
	before, err := Files.ReadFile("example.zip")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Files.ReadFile("example.zip")
	if err != nil || !bytes.Equal(before, again) {
		t.Fatal("same files did not yield a deterministic snapshot", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "Dockerfile"), []byte("FROM busybox\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := Files.ReadFile("example.zip")
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("source edits were not reloaded", err)
	}
	oldFiles, newFiles := zipFiles(t, before), zipFiles(t, after)
	if oldFiles["Dockerfile"] != "FROM scratch\n" || newFiles["Dockerfile"] != "FROM busybox\n" || oldFiles["SHA256SUMS"] == newFiles["SHA256SUMS"] || strings.Contains(newFiles["SHA256SUMS"], "stale") {
		t.Fatal("snapshot was changed or its checksum was not regenerated")
	}
}

func TestExplicitRecipeDirectoryDoesNotFallBack(t *testing.T) {
	t.Setenv("RUNDESK_RECIPES_DIR", filepath.Join(t.TempDir(), "missing"))
	if _, err := Files.ReadFile("news2douyin.zip"); err == nil || !strings.Contains(err.Error(), "RUNDESK_RECIPES_DIR") {
		t.Fatal("invalid explicit recipe source silently fell back", err)
	}
}

func TestRecipePathAndLinksAreRejected(t *testing.T) {
	directory := testRecipe(t)
	for _, name := range []string{"../example.zip", "example/../example.zip", "example\\test.zip", "example", ".zip"} {
		if _, err := Files.ReadFile(name); err == nil {
			t.Fatal("invalid recipe path accepted", name)
		}
	}
	if err := os.Symlink(filepath.Join(directory, "Dockerfile"), filepath.Join(directory, "linked")); err != nil {
		t.Skip("platform does not permit symlink fixtures")
	}
	if _, err := Files.ReadFile("example.zip"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatal("recipe link accepted", err)
	}
}
