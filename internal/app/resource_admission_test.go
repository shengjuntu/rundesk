package app

import (
	"encoding/json"
	"testing"

	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Exercise the real HTTP/queue/native-demo path, then change the declaration.
// Historical evidence must stay fixed while the inspection view stays current.
func TestAdmittedResourceDeclarationsSurviveConfigChange(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	w := m.Workspaces()[0]
	r := v1Request(h, "POST", "/tasks", `{"workspaceId":"`+w.ID+`","input":{"text":"resource integration"}}`, "resource-integration-task")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	var task Task
	if err := json.Unmarshal(r.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	task = taskWait(t, m, task.ID, "completed")
	rows, err := m.Store.QueryEvents(task.SessionID, 0, 10, store.EventFilter{Method: "run/input"})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	var admitted struct {
		RunID      string            `json:"runId"`
		Basis      string            `json:"resourceBasis"`
		ObservedAt string            `json:"resourceObservedAt"`
		Resources  resource.Manifest `json:"resources"`
	}
	if err := json.Unmarshal(rows[0].Data, &admitted); err != nil {
		t.Fatal(err)
	}
	if admitted.RunID != task.RunID || admitted.Basis != "declared_at_admission" || admitted.ObservedAt == "" || admitted.Resources.Schema != 1 || admitted.Resources.Scope.TaskID != task.ID {
		t.Fatalf("missing admitted resource evidence: %+v", admitted)
	}
	var oldPolicy resource.Ref
	for _, binding := range admitted.Resources.Bindings {
		if binding.Role == resource.RolePolicy {
			oldPolicy = binding.Ref
		}
	}
	i, err := m.Instance(task.Spec.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	permissions := i.Permissions
	permissions.Sandbox = "read-only"
	if _, err := m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Revision: i.Revision, Permissions: &permissions}); err != nil {
		t.Fatal(err)
	}
	r = v1Request(h, "GET", "/tasks/"+task.ID+"/resources", "", "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var view TaskResourceView
	if err := json.Unmarshal(r.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Task.Status != "completed" || view.ConfigurationBasis != "current_bindings" || view.Runtime == nil || view.Runtime.NativeAssociation != "current_run" {
		t.Fatalf("incorrect current resource view: %+v", view)
	}
	changed := false
	for _, binding := range view.Resources.Bindings {
		if binding.Role == resource.RolePolicy {
			changed = oldPolicy.ID == binding.Ref.ID && oldPolicy.Version != binding.Ref.Version
		}
	}
	if !changed {
		t.Fatal("current policy did not distinguish its new revision")
	}
	again, err := m.Store.QueryEvents(task.SessionID, 0, 10, store.EventFilter{Method: "run/input"})
	if err != nil || len(again) != 1 || string(again[0].Data) != string(rows[0].Data) {
		t.Fatal("current configuration rewrote historical evidence", err)
	}
}
