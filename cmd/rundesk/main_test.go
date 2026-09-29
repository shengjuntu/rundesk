package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyDataDirectorySelection(t *testing.T) {
	base := t.TempDir()
	old := filepath.Join(base, "codex-base")
	current := filepath.Join(base, "rundesk")
	if defaultDataDir(base) != current {
		t.Fatal("new install should use rundesk")
	}
	_ = os.MkdirAll(old, 0700)
	_ = os.WriteFile(filepath.Join(old, "state.db"), []byte("fixture"), 0600)
	if defaultDataDir(base) != old {
		t.Fatal("legacy data was ignored")
	}
	_ = os.MkdirAll(current, 0700)
	_ = os.WriteFile(filepath.Join(current, "state.db"), []byte("fixture"), 0600)
	if defaultDataDir(base) != current {
		t.Fatal("new data should take precedence")
	}
}
