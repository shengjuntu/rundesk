package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pausedResourceTask(t *testing.T, m *Manager) Task {
	t.Helper()
	q := m.Queue()
	q.Paused = true
	if _, err := m.SaveQueue(q); err != nil {
		t.Fatal(err)
	}
	task, err := m.Enqueue(TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "private-input-should-not-appear"}})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTaskResourceViewIsReferenceOnlyAndRegenerable(t *testing.T) {
	m := testManager(t)
	task := pausedResourceTask(t, m)
	w, _ := m.Workspace(task.Spec.WorkspaceID)
	output := filepath.Join(w.Path, "outputs", task.SessionID, "large-file.txt")
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("artifact stays at its original location"), 0600); err != nil {
		t.Fatal(err)
	}
	view, err := m.TaskResources(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Task.Status != "queued" || view.StateSource != "platform_sqlite" || view.Availability != "available" || view.ConfigurationBasis != "current_bindings" || view.ObservedAt == "" || len(view.Resources.Resources) == 0 {
		t.Fatalf("incomplete read-through view: %+v", view)
	}
	if _, err := os.Stat(filepath.Join(m.Data, "tasks")); !os.IsNotExist(err) {
		t.Fatalf("inspection unexpectedly materialized a directory: %v", err)
	}
	export, err := m.ExportTaskResources(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Dir(export.Path))
	if err != nil || len(files) != 1 || files[0].Name() != "resources.json" {
		t.Fatalf("export copied more than the reference view: %v %v", files, err)
	}
	data, err := os.ReadFile(export.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{m.Data, w.Path, "private-input-should-not-appear", "artifact stays at its original location"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("view leaked original bytes, prompt, or host location: %q", forbidden)
		}
	}
	// The generated file is not a control surface or another source of truth.
	if err := os.WriteFile(export.Path, []byte(`{"task":{"status":"completed"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	view, err = m.TaskResources(task.ID)
	if err != nil || view.Task.Status != "queued" {
		t.Fatalf("edited export changed task state: %+v %v", view, err)
	}
	if _, err := m.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ExportTaskResources(task.ID); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(export.Path)
	if err != nil || json.Unmarshal(data, &view) != nil || view.Task.Status != "canceled" {
		t.Fatalf("refresh did not reflect canonical state: %s %v", data, err)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "artifact stays at its original location" {
		t.Fatalf("export modified original output: %s %v", data, err)
	}
}

func TestTaskResourceViewPreservesTaskAfterSessionDeletion(t *testing.T) {
	m := testManager(t)
	task := pausedResourceTask(t, m)
	if _, err := m.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteSession(task.SessionID); err != nil {
		t.Fatal(err)
	}
	view, err := m.TaskResources(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Task.ID != task.ID || view.Task.Status != "canceled" || view.Availability != "unavailable" || view.UnavailableReason != "session_not_found" || view.ConfigurationBasis != "unavailable" || view.Runtime != nil {
		t.Fatalf("missing session lost task facts or invented native identity: %+v", view)
	}
	if view.Resources.Scope.TaskID != task.ID || view.Resources.Scope.WorkspaceID != task.Spec.WorkspaceID || view.Resources.Scope.SessionID != task.SessionID || len(view.Resources.Resources) != 0 || len(view.Resources.Bindings) != 0 {
		t.Fatalf("unexpected unavailable resource scope: %+v", view.Resources)
	}
	exported, err := m.ExportTaskResources(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exported.Path)
	if err != nil || strings.Contains(string(data), `"runtime"`) {
		t.Fatalf("unavailable view exported native identity: %s %v", data, err)
	}
	if _, err := m.Session(task.SessionID); err == nil {
		t.Fatal("inspection recreated the deleted session")
	}
}

func TestTaskResourceViewDoesNotAttributeLaterNativeTurn(t *testing.T) {
	m := testManager(t)
	task := pausedResourceTask(t, m)
	m.queueMu.Lock()
	task.RunID, task.Status = "original-run", "completed"
	err := m.saveTaskLocked(task)
	m.queueMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.update(task.SessionID, func(s *Session) {
		s.RunID, s.ThreadID, s.TurnID = "original-run", "original-thread", "original-turn"
	}); err != nil {
		t.Fatal(err)
	}
	view, err := m.TaskResources(task.ID)
	if err != nil || view.Runtime.NativeAssociation != "current_run" || view.Runtime.TurnID != "original-turn" {
		t.Fatalf("lost matching native identity: %+v %v", view.Runtime, err)
	}
	if err = m.update(task.SessionID, func(s *Session) {
		s.RunID, s.ThreadID, s.TurnID = "later-run", "later-thread", "later-turn"
	}); err != nil {
		t.Fatal(err)
	}
	view, err = m.TaskResources(task.ID)
	if err != nil || view.Runtime.RunID != "original-run" || view.Runtime.NativeAssociation != "unavailable" || view.Runtime.ThreadID != "" || view.Runtime.TurnID != "" {
		t.Fatalf("later native run was attributed to the original task: %+v %v", view.Runtime, err)
	}
}

func TestTaskResourceRoutesRespectOwnershipAndExportPrivilege(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	wid := m.Workspaces()[0].ID
	_, _, own := setupKey(t, m, "resource-owner", wid, "read", "run")
	_, _, foreign := setupKey(t, m, "resource-foreign", wid, "read", "run")
	h := NewHandler(m, userTestAdmin, true)
	r := appRequest(h, "POST", "/api/v1/tasks", `{"workspaceId":"`+wid+`","input":{"text":"private"}}`, own, "resource-task-0001")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	var task Task
	if err := json.Unmarshal(r.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/api/v1", "/api"} {
		path := prefix + "/tasks/" + task.ID + "/resources"
		r = appRequest(h, "GET", path, "", own, "")
		if r.Code != 200 || strings.Contains(r.Body.String(), m.Data) {
			t.Fatalf("owner view: %d %s", r.Code, r.Body.String())
		}
		r = appRequest(h, "GET", path, "", foreign, "")
		if r.Code != 403 {
			t.Fatalf("foreign task view: %d %s", r.Code, r.Body.String())
		}
		r = appRequest(h, "POST", path+"/export", `{}`, own, "resource-export-app-0001")
		if r.Code != 403 {
			t.Fatalf("application exported host directory: %d %s", r.Code, r.Body.String())
		}
	}
	r = appRequest(h, "POST", "/api/v1/tasks/"+task.ID+"/resources/export", `{}`, userTestAdmin, "resource-export-admin-0001")
	if r.Code != 200 {
		t.Fatalf("admin export: %d %s", r.Code, r.Body.String())
	}
	var exported TaskResourceExport
	if err := json.Unmarshal(r.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exported.Path); err != nil {
		t.Fatal(err)
	}
	first := exported
	r = appRequest(h, "POST", "/api/v1/tasks/"+task.ID+"/resources/export", `{}`, userTestAdmin, "resource-export-admin-0001")
	if err := json.Unmarshal(r.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || exported != first || r.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("export receipt was not replayed: %d %s %v", r.Code, r.Body.String(), r.Header())
	}
}

func TestTaskResourceExportRejectsDirectoryAliases(t *testing.T) {
	m := testManager(t)
	task := pausedResourceTask(t, m)
	target := filepath.Join(m.Data, "native-data")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(m.Data, "tasks")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := m.ExportTaskResources(task.ID); err == nil {
		t.Fatal("export followed a directory alias into native state")
	}
	files, err := os.ReadDir(target)
	if err != nil || len(files) != 0 {
		t.Fatalf("export mutated aliased data: %v %v", files, err)
	}
}
