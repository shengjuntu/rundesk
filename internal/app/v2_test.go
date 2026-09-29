package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestSessionManagementAndFileRetention(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, e := m.CreateSession(w.ID, "old title", "")
	if e != nil {
		t.Fatal(e)
	}
	title := "new title"
	yes, no := true, false
	v, e := m.PatchSession(s.ID, SessionPatch{Title: &title, Pinned: &yes, Archived: &yes})
	if e != nil || v.Title != title || !v.Pinned || !v.Archived {
		t.Fatal(v, e)
	}
	if _, e = m.Start(s.ID, Input{Text: "do not start archived"}); e == nil {
		t.Fatal("archived session started")
	}
	if _, e = m.PatchSession(s.ID, SessionPatch{Archived: &no}); e != nil {
		t.Fatal(e)
	}
	if e = m.update(s.ID, func(s *Session) { s.Status = "running" }); e != nil {
		t.Fatal(e)
	}
	if _, e = m.PatchSession(s.ID, SessionPatch{Archived: &yes}); e == nil {
		t.Fatal("archived running session")
	}
	if e = m.DeleteSession(s.ID); e == nil {
		t.Fatal("deleted running session")
	}
	_ = m.update(s.ID, func(s *Session) { s.Status = "completed" })
	file := filepath.Join(w.Path, "outputs", s.ID, "kept.txt")
	_ = os.MkdirAll(filepath.Dir(file), 0700)
	_ = os.WriteFile(file, []byte("keep"), 0600)
	_, _ = m.Store.Add(s.ID, "internal", "run/input", map[string]any{"input": Input{Text: "hello"}})
	_ = m.Store.Put("approval", "old-approval", Approval{ID: "old-approval", SessionID: s.ID, Status: "expired"})
	if e = m.DeleteSession(s.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Session(s.ID); e == nil {
		t.Fatal("deleted session still visible")
	}
	events, e := m.Store.Events(s.ID, 0, 1000)
	if e != nil || len(events) != 0 {
		t.Fatal("events retained", e)
	}
	if b, e := os.ReadFile(file); e != nil || string(b) != "keep" {
		t.Fatal("artifact deleted", e)
	}
	var approval Approval
	if e = m.Store.Get("approval", "old-approval", &approval); e == nil {
		t.Fatal("approval orphaned")
	}
	// Metadata-only operations must not consume the App Server connection limit.
	for i := 0; i < 25; i++ {
		s, _ := m.CreateSession(w.ID, "many", "")
		if _, e = m.PatchSession(s.ID, SessionPatch{Pinned: &yes}); e != nil {
			t.Fatal(e)
		}
	}
	if m.loaded.Load() != 0 {
		t.Fatal("metadata operation started a process")
	}
}

func TestMarkdownDoesNotDuplicateDeltasOrLeakInjectedNotes(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, _ := m.CreateSession(w.ID, "export", "")
	_, _ = m.Store.Add(s.ID, "internal", "run/input", map[string]any{"input": Input{Text: "Question"}, "notes": "private injected note"})
	_, _ = m.Store.Add(s.ID, "in", "item/agentMessage/delta", map[string]any{"params": map[string]any{"itemId": "a", "delta": "An"}})
	_, _ = m.Store.Add(s.ID, "in", "item/agentMessage/delta", map[string]any{"params": map[string]any{"itemId": "a", "delta": "swer"}})
	_, _ = m.Store.Add(s.ID, "in", "item/completed", map[string]any{"params": map[string]any{"item": map[string]any{"id": "a", "type": "agentMessage", "text": "Answer"}}})
	markdown, e := m.Markdown(s.ID)
	if e != nil || strings.Count(markdown, "Answer") != 1 || strings.Contains(markdown, "private injected note") {
		t.Fatal(markdown, e)
	}
}

func TestMCPBundleImportValidatesBeforeWriting(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	if _, e := m.SaveMCP(w.ID, "original", "0", map[string]any{"command": "example", "env": map[string]any{"KEY": "private-value"}}, false); e != nil {
		t.Fatal(e)
	}
	exported, e := m.ExportMCP(w.ID)
	if e != nil {
		t.Fatal(e)
	}
	bytes, _ := json.Marshal(exported)
	if strings.Contains(string(bytes), "private-value") {
		t.Fatal("secret exported")
	}
	var bundle MCPBundle
	if e = json.Unmarshal(bytes, &bundle); e != nil {
		t.Fatal(e)
	}
	if _, e = m.ImportMCP(w.ID, "1", bundle, false); e == nil {
		t.Fatal("implicit overwrite accepted")
	}
	if _, e = m.ImportMCP(w.ID, "1", bundle, true); e != nil {
		t.Fatal(e)
	}
	config, _ := m.readConfig(w.ID)
	servers, version := userMCP(config)
	if servers["original"].(map[string]any)["env"].(map[string]any)["KEY"] != "private-value" {
		t.Fatal("redacted placeholder replaced secret")
	}
	bad := MCPBundle{SchemaVersion: 1, Kind: "rundesk.mcp", Servers: map[string]map[string]any{"valid": {"command": "unused"}, "invalid": {"url": "file:///bad"}}}
	if _, e = m.ImportMCP(w.ID, version, bad, false); e == nil {
		t.Fatal("invalid import accepted")
	}
	config, _ = m.readConfig(w.ID)
	servers, after := userMCP(config)
	if len(servers) != 1 || after != version {
		t.Fatal("partial config write")
	}
	bundle.Servers = map[string]map[string]any{"another": {"command": "unused", "env": map[string]any{"KEY": "[redacted]"}}}
	if _, e = m.ImportMCP(w.ID, version, bundle, false); e == nil {
		t.Fatal("missing imported secret accepted")
	}
}

func TestSkillRemovalCreatesRecoverableBackup(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	content := "---\r\nname: guide\r\ndescription: test\r\n---\r\n\r\nOriginal skill.\r\n"
	if e := m.SaveSkill(w.ID, "guide", content); e != nil {
		t.Fatal(e)
	}
	support := filepath.Join(w.Path, ".agents", "skills", "guide", "example.txt")
	_ = os.WriteFile(support, []byte("supporting file"), 0600)
	backup, e := m.DeleteSkill(w.ID, "guide")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.ReadSkill(w.ID, "guide"); e == nil {
		t.Fatal("skill still discoverable")
	}
	got, e := os.ReadFile(filepath.Join(w.Path, backup))
	if e != nil || !strings.Contains(string(got), "Original skill") {
		t.Fatal("backup missing", e)
	}
	if _, e = os.Stat(support); e != nil {
		t.Fatal("supporting file lost")
	}
}

func TestV01DatabaseUpgradePreservesPathsAndThreads(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "original-workspace")
	_ = os.MkdirAll(workspace, 0700)
	db, e := store.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	_ = db.Put("workspace", "old-workspace", map[string]any{"id": "old-workspace", "name": "Legacy", "path": workspace, "notes": "keep me", "revision": 3})
	_ = db.Put("session", "old-session", map[string]any{"id": "old-session", "workspaceId": "old-workspace", "title": "Legacy chat", "threadId": "native-thread-123", "status": "completed", "created": store.Now(), "updated": store.Now()})
	_, _ = db.Add("old-session", "internal", "old/event", map[string]string{"value": "old event"})
	_ = db.Close()
	m, e := New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	s, e := m.Session("old-session")
	if e != nil || s.ThreadID != "native-thread-123" || s.Archived || s.Pinned {
		t.Fatal(s, e)
	}
	w, e := m.Workspace("old-workspace")
	if e != nil || w.Path != workspace || w.Notes != "keep me" || w.Revision != 3 {
		t.Fatal(w, e)
	}
	events, e := m.Store.Events(s.ID, 0, 1000)
	if e != nil || len(events) != 1 {
		t.Fatal("old history lost", e)
	}
}
