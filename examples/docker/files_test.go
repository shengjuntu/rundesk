package dockertemplate

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestGenericTemplateReadsExternalChanges(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "generic")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "Dockerfile")
	if err := os.WriteFile(file, []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNDESK_RECIPES_DIR", root)
	before, err := Files.ReadFile("Dockerfile")
	if err != nil || string(before) != "FROM scratch\n" {
		t.Fatal(string(before), err)
	}
	if err := os.WriteFile(file, []byte("FROM busybox\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := Files.ReadFile("Dockerfile")
	if err != nil || string(after) != "FROM busybox\n" {
		t.Fatal("generic template was cached", string(after), err)
	}
	count := 0
	if err := fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if !entry.IsDir() {
			count++
		}
		return err
	}); err != nil || count != 1 {
		t.Fatal("external template fs interface is not compatible", count, err)
	}
	if _, err := Files.ReadFile("../Dockerfile"); err == nil {
		t.Fatal("template path escaped")
	}
}
