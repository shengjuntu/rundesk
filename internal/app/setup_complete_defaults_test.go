package app

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSetupCompletionRecoversStaleMaintenanceWorkspace(t *testing.T) {
	m := testManager(t)
	ordinary := m.Workspaces()[0]
	maintenance, err := m.CreateWorkspace("Kun repair", filepath.Join(m.Data, "setup-agent-workspace", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Store.Put("system", "setup", SetupState{WorkspaceID: maintenance.ID}); err != nil {
		t.Fatal(err)
	}
	before, _ := m.Instance()
	saved, err := m.completeSetup(SetupCompleteInput{Revision: 0})
	if err != nil || saved.WorkspaceID != ordinary.ID || !saved.Completed {
		t.Fatal(saved, err)
	}
	session, err := m.CreateSession(saved.WorkspaceID, "ready after setup", "")
	if err != nil || session.InstanceID != DefaultInstance || session.RuntimeKind != "codex" {
		t.Fatal(session, err)
	}
	after, _ := m.Instance()
	if before.ID != after.ID || before.AgentRuntime.Kind != after.AgentRuntime.Kind {
		t.Fatal("completion replaced the daily backend", before, after)
	}
}

func TestSetupCompletionRejectsExplicitMaintenanceAndUnknownWorkspace(t *testing.T) {
	m := testManager(t)
	maintenance, err := m.CreateWorkspace("Kun repair", filepath.Join(m.Data, "setup-repair-workspaces", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(m, "", true)
	before, _ := m.setupState()
	rec := v1Request(handler, "POST", "/setup/complete", fmt.Sprintf(`{"workspaceId":%q,"revision":0}`, maintenance.ID), strings.Repeat("a", 32))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "maintenance_workspace_reserved") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	after, _ := m.setupState()
	if after != before {
		t.Fatal("rejected workspace changed setup", before, after)
	}
	if _, err := m.completeSetup(SetupCompleteInput{WorkspaceID: "missing-workspace", Revision: 0}); err == nil {
		t.Fatal("explicit missing workspace silently fell back")
	}
	chosen, err := m.CreateWorkspace("Chosen ordinary workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := m.completeSetup(SetupCompleteInput{WorkspaceID: chosen.ID, Revision: 0})
	if err != nil || saved.WorkspaceID != chosen.ID {
		t.Fatal("explicit ordinary workspace changed", saved, err)
	}
}

func TestSetupCompletionCreatesOneManagedDailyWorkspaceWhenOnlyMaintenanceRemains(t *testing.T) {
	m := testManager(t)
	for _, work := range m.Workspaces() {
		if err := m.Store.DeleteObject("workspace", work.ID); err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		delete(m.workspaces, work.ID)
		m.mu.Unlock()
	}
	maintenance, err := m.CreateWorkspace("Kun repair", filepath.Join(m.Data, "setup-agent-workspace", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Store.Put("system", "setup", SetupState{WorkspaceID: maintenance.ID}); err != nil {
		t.Fatal(err)
	}
	saved, err := m.completeSetup(SetupCompleteInput{Revision: 0})
	if err != nil || saved.WorkspaceID == maintenance.ID || saved.WorkspaceID == "" {
		t.Fatal(saved, err)
	}
	workspace, err := m.Workspace(saved.WorkspaceID)
	if err != nil || m.maintenanceWorkspace(workspace) || !within(filepath.Join(m.Data, "workspaces"), workspace.Path) {
		t.Fatal(workspace, err)
	}
	count := len(m.Workspaces())
	again, err := m.completeSetup(SetupCompleteInput{Revision: saved.Revision})
	if err != nil || again.WorkspaceID != saved.WorkspaceID || len(m.Workspaces()) != count {
		t.Fatal("completion created another daily workspace", again, err)
	}
	session, err := m.CreateSession(again.WorkspaceID, "ready", "")
	if err != nil || session.InstanceID != DefaultInstance || session.RuntimeKind != "codex" {
		t.Fatal(session, err)
	}
}

func TestSetupCompletionPreservesExplicitApplicationWorkspaceBinding(t *testing.T) {
	m := testManager(t)
	work, err := m.CreateWorkspace("Application project", "")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := m.CreateInstance(InstancePatch{Name: "Application Codex"})
	if err != nil {
		t.Fatal(err)
	}
	application, err := m.RegisterApplication("video", ApplicationInput{Name: "Video", InstanceID: agent.ID, WorkspaceID: work.ID, EntryURL: "http://localhost/video"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := m.completeSetup(SetupCompleteInput{WorkspaceID: work.ID, Revision: 0})
	if err != nil || saved.WorkspaceID != work.ID {
		t.Fatal(saved, err)
	}
	var after Application
	if err = m.Store.Get("application", application.AppID, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(application, after) {
		t.Fatal("setup changed application ownership", application, after)
	}
}
