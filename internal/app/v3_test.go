package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestInstanceConfigurationAndSkillScopes(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	a, e := m.CreateInstance(InstancePatch{Name: "A", DefaultModel: "model-a"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := m.CreateInstance(InstancePatch{Name: "B"})
	if e != nil {
		t.Fatal(e)
	}
	if a.CodexHome == b.CodexHome {
		t.Fatal("homes overlap")
	}
	for _, i := range []Instance{a, b} {
		_, e = m.SaveMCP(w.ID, "service", "0", map[string]any{"command": i.Name, "enabled": false}, false, i.ID)
		if e != nil {
			t.Fatal(e)
		}
		content := "---\nname: guide\ndescription: guide\n---\n" + i.Name
		if e = m.SaveSkill(w.ID, "guide", content, i.ID, "instance"); e != nil {
			t.Fatal(e)
		}
	}
	for _, i := range []Instance{a, b} {
		config, e := m.readConfig(w.ID, i.ID)
		if e != nil {
			t.Fatal(e)
		}
		servers, _ := userMCP(config)
		if servers["service"].(map[string]any)["command"] != i.Name {
			t.Fatal("MCP crossed instances")
		}
		content, e := m.ReadSkill(w.ID, "guide", i.ID, "instance")
		if e != nil || !strings.HasSuffix(content, i.Name) {
			t.Fatal(content, e)
		}
	}
	shared := "---\nname: shared\ndescription: shared\n---\nshared"
	if e = m.SaveSkill(w.ID, "shared", shared); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(w.Path, ".agents", "skills", "shared", "SKILL.md")
	if e = m.ToggleSkill(w.ID, path, false, a.ID); e != nil {
		t.Fatal(e)
	}
	input := Input{Text: "hello", Skills: []Skill{{Name: "shared", Path: path}}}
	if e = m.validateInput(w, input, a.ID); e == nil {
		t.Fatal("disabled skill accepted")
	}
	if e = m.validateInput(w, input, b.ID); e != nil {
		t.Fatal("shared skill toggle crossed instances", e)
	}
	input.Skills = []Skill{{Name: "guide", Path: filepath.Join(a.CodexHome, "skills", "guide", "SKILL.md")}}
	if e = m.validateInput(w, input, b.ID); e == nil {
		t.Fatal("foreign instance skill accepted")
	}
	other, e := m.CreateWorkspace("other", "")
	if e != nil {
		t.Fatal(e)
	}
	config, e := m.readConfig(other.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	servers, _ := userMCP(config)
	if servers["service"].(map[string]any)["command"] != "A" {
		t.Fatal("instance config lost across workspaces")
	}
	if _, e = m.SaveMCP(w.ID, "stale", "0", map[string]any{"command": "x"}, false, a.ID); e == nil {
		t.Fatal("stale write accepted")
	}
	backup, e := m.DeleteSkill(w.ID, "guide", a.ID, "instance")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(a.CodexHome, backup)); e != nil {
		t.Fatal(e)
	}
	if _, e = m.ReadSkill(w.ID, "guide", b.ID, "instance"); e != nil {
		t.Fatal("deletion crossed instances", e)
	}
	if _, e = m.PatchInstance(a.ID, InstancePatch{Name: "renamed", Revision: 0}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.PatchInstance(a.ID, InstancePatch{Name: "stale", Revision: 0}); e == nil {
		t.Fatal("stale instance write accepted")
	}
}

func TestInstanceMigrationAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	w := Workspace{ID: "old-workspace", Path: t.TempDir()}
	if e = db.Put("workspace", w.ID, w); e != nil {
		t.Fatal(e)
	}
	if e = db.Put("session", "old", map[string]any{"id": "old", "workspaceId": w.ID, "threadId": "keep-thread", "status": "completed"}); e != nil {
		t.Fatal(e)
	}
	db.Close()
	m, e := New(dir, "codex", true)
	if e != nil {
		t.Fatal(e)
	}
	s, e := m.Session("old")
	if e != nil || s.InstanceID != DefaultInstance || s.ThreadID != "keep-thread" {
		t.Fatal(s, e)
	}
	i, e := m.CreateInstance(InstancePatch{Name: "persistent", DefaultModel: "model-x"})
	if e != nil {
		t.Fatal(e)
	}
	s, e = m.CreateSession(w.ID, "new", "", i.ID)
	if e != nil || s.InstanceID != i.ID || s.Model != "model-x" {
		t.Fatal(s, e)
	}
	if _, e = m.CreateSession(w.ID, "invalid", "", "unknown"); e == nil {
		t.Fatal("unknown instance accepted")
	}
	if e = m.SaveSkill(w.ID, "escape", "---\nname: escape\n---\n", i.ID, "unknown"); e == nil {
		t.Fatal("unknown scope accepted")
	}
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(dir, "foreign"))
	t.Setenv("RUNDESK_TEST_INHERIT", "kept")
	cmd := m.command(w, i)
	joined := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joined, "CODEX_HOME="+i.CodexHome) || strings.Contains(joined, "CODEX_SQLITE_HOME=") || !strings.Contains(joined, "RUNDESK_TEST_INHERIT=kept") {
		t.Fatal("wrong child environment")
	}
	m.Close()
	m, e = New(dir, "codex", true)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	got, e := m.Session(s.ID)
	if e != nil || got.InstanceID != i.ID || got.Model != "model-x" {
		t.Fatal(got, e)
	}
	gotInstance, e := m.Instance(i.ID)
	if e != nil || gotInstance.CodexHome != i.CodexHome {
		t.Fatal(gotInstance, e)
	}
}

func TestInstanceReloadPreservesActiveTurnAndResume(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	i, e := m.CreateInstance(InstancePatch{Name: "reload"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := m.CreateSession(w.ID, "", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Skills(w.ID, i.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(s.ID, Input{Text: "审批"}); e != nil {
		t.Fatal(e)
	}
	first := waitState(t, m, s.ID, "waiting")
	r, e := m.ReloadInstance(i.ID)
	if e != nil {
		t.Fatal(e)
	}
	counts := r.(map[string]any)
	if counts["closedConnections"] != 1 || counts["busyConnections"] != 1 {
		t.Fatal(r)
	}
	if len(m.Approvals(s.ID)) != 1 {
		t.Fatal("reload lost pending approval")
	}
	if e = m.Stop(s.ID); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s.ID, "interrupted")
	if _, e = m.ReloadInstance(i.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(s.ID, Input{Text: "审批 again"}); e != nil {
		t.Fatal(e)
	}
	second := waitState(t, m, s.ID, "waiting")
	if first.ThreadID != second.ThreadID || second.InstanceID != i.ID {
		t.Fatal("resume moved thread")
	}
	rows, e := m.Store.Events(s.ID, 0, 1000)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, row := range rows {
		if row.Method == "run/input" {
			var data map[string]any
			_ = json.Unmarshal(row.Data, &data)
			if data["instanceId"] == i.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing instance journal context")
	}
}
