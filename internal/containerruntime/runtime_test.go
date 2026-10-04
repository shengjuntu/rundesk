package containerruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedPreservesEditsAndCopiesCompleteSkill(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "skills", "news", "scripts"), 0700)
	os.WriteFile(filepath.Join(src, "config.toml"), []byte("image-default"), 0600)
	os.WriteFile(filepath.Join(src, "auth.json"), []byte("must-not-copy"), 0600)
	os.WriteFile(filepath.Join(src, "skills", "news", "SKILL.md"), []byte("research"), 0600)
	script := filepath.Join("skills", "news", "scripts", "lookup.sh")
	os.WriteFile(filepath.Join(src, script), []byte("echo hello"), 0700)
	os.WriteFile(filepath.Join(dst, "config.toml"), []byte("user-config"), 0600)
	if e := Seed(src, dst); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dst, "config.toml"))
	if e != nil || string(b) != "user-config" {
		t.Fatal("overwrote user config")
	}
	if _, e = os.Stat(filepath.Join(dst, "auth.json")); !os.IsNotExist(e) {
		t.Fatal("copied auth")
	}
	info, e := os.Stat(filepath.Join(dst, script))
	if e != nil || info.Mode()&0100 == 0 {
		t.Fatal("executable skill missing")
	}
	os.WriteFile(filepath.Join(src, "skills", "news", "SKILL.md"), []byte("changed-image"), 0600)
	if e = Seed(src, dst); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(filepath.Join(dst, "skills", "news", "SKILL.md"))
	if string(b) != "research" {
		t.Fatal("reseeded existing environment")
	}
}
func TestSeedRejectsLinks(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if e := os.Symlink("/etc/passwd", filepath.Join(src, "config.toml")); e != nil {
		t.Skip(e)
	}
	if e := Seed(src, dst); e == nil {
		t.Fatal("seed link accepted")
	}
}
