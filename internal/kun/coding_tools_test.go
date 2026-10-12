package kun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodingEditsAreExactAndAllOrNothing(t *testing.T) {
	for _, tc := range []struct {
		name, original, want string
		edits                []textEdit
		fail                 bool
	}{
		{"original offsets", "alpha beta gamma", "A B gamma", []textEdit{{"beta", "B"}, {"alpha", "A"}}, false},
		{"replacements are not rematched", "alpha beta", "beta gamma", []textEdit{{"alpha", "beta"}, {"beta", "gamma"}}, false},
		{"missing", "alpha beta", "alpha beta", []textEdit{{"alpha", "A"}, {"absent", "B"}}, true},
		{"overlap", "abcdef", "abcdef", []textEdit{{"abc", "A"}, {"bcde", "B"}}, true},
		{"ambiguous", "aaa", "aaa", []textEdit{{"aa", "A"}}, true},
		{"utf8", "你好 world", "你好 世界", []textEdit{{"world", "世界"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "code.txt")
			os.WriteFile(file, []byte(tc.original), 0600)
			_, err := executeEdit(dir, codingCall("edit_file", map[string]any{"path": "code.txt", "edits": tc.edits}))
			if (err != nil) != tc.fail {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(file)
			if string(data) != tc.want {
				t.Fatal(string(data))
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatal("temporary edit leaked")
			}
		})
	}
}
func TestCodingWorkspaceAndWriteBoundary(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	for _, name := range []string{"link", "../outside.txt", outside} {
		if _, err := executeEdit(dir, codingCall("edit_file", map[string]any{"path": name, "edits": []textEdit{{"secret", "bad"}}})); err == nil {
			t.Fatal("unsafe edit", name)
		}
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "secret" {
		t.Fatal("outside changed")
	}
	os.WriteFile(filepath.Join(dir, "code.txt"), []byte("original"), 0600)
	if _, err := executeTool(dir, false, codingCall("edit_file", map[string]any{"path": "code.txt", "edits": []textEdit{{"original", "bad"}}})); err == nil {
		t.Fatal("read-only edit")
	}
	data, _ = os.ReadFile(filepath.Join(dir, "code.txt"))
	if string(data) != "original" {
		t.Fatal("read-only changed")
	}
	if _, err := executeSearch(context.Background(), dir, codingCall("grep", map[string]any{"pattern": "secret", "path": "link"})); err == nil {
		t.Fatal("symlink search")
	}
}
func TestCodingSearchAndCancellation(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "nested"), 0700)
	os.Mkdir(filepath.Join(dir, ".git"), 0700)
	for file, body := range map[string]string{"a.go": "package main\nHELLO.x\nhello.x\n", "nested/b.go": "你好 hello.x\n", ".git/hidden.go": "hello.x", "binary.go": "hello.x\x00", "skip.txt": "hello.x"} {
		os.WriteFile(filepath.Join(dir, file), []byte(body), 0600)
	}
	output, err := executeSearch(context.Background(), dir, codingCall("grep", map[string]any{"pattern": "hello.x", "literal": true, "ignoreCase": true, "glob": "*.go"}))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Matches []struct {
			Path string
			Line int
			Text string
		}
		Truncated    bool
		SkippedFiles int
	}
	if err = json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 3 || result.Truncated || result.SkippedFiles != 1 {
		t.Fatal(output)
	}
	for _, m := range result.Matches {
		if m.Line < 1 || strings.HasPrefix(m.Path, ".git") {
			t.Fatal(output)
		}
	}
	output, err = executeSearch(context.Background(), dir, codingCall("find", map[string]any{"pattern": "*.go", "limit": 1}))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(output), &result)
	if len(result.Matches) != 1 || !result.Truncated {
		t.Fatal(output)
	}
	for _, args := range []map[string]any{{"pattern": "["}, {"pattern": "x", "path": ".."}, {"pattern": "x", "limit": 501}, {"pattern": "x", "glob": "**/*.go"}} {
		if _, err := executeSearch(context.Background(), dir, codingCall("grep", args)); err == nil {
			t.Fatal("invalid search accepted", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executeSearch(ctx, dir, codingCall("find", map[string]any{"pattern": "*.go"})); err != context.Canceled {
		t.Fatal(err)
	}
}
