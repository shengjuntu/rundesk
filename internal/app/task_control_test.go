package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This is a separate task service, not a Manager wrapper. It verifies that the
// transport can depend on the lifecycle contract without owning native engines.
type taskControlFixture struct {
	input    TaskSpec
	canceled string
}

func (f *taskControlFixture) Enqueue(in TaskSpec) (Task, error) {
	f.input = in
	return Task{ID: "task-contract", Status: "queued"}, nil
}
func (f *taskControlFixture) Task(id string) (Task, error) {
	return Task{ID: id, Status: "queued"}, nil
}
func (f *taskControlFixture) Tasks() []Task {
	return []Task{{ID: "task-contract", Status: "queued"}}
}
func (f *taskControlFixture) CancelTask(id string) (Task, error) {
	f.canceled = id
	return Task{ID: id, Status: "canceled"}, nil
}

func TestTaskRoutesUseLifecycleContract(t *testing.T) {
	f := &taskControlFixture{}
	s := &Server{Tasks: f}
	mux := http.NewServeMux()
	s.queueRoutes(mux)
	for _, req := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/tasks", `{"workspaceId":"project","input":{"text":"run"}}`, 202},
		{"GET", "/api/tasks/task-contract", "", 200},
		{"GET", "/api/tasks", "", 200},
		{"POST", "/api/tasks/task-contract/cancel", "", 200},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(req.method, req.path, strings.NewReader(req.body))
		mux.ServeHTTP(w, r)
		if w.Code != req.status {
			t.Fatalf("%s: %d %s", req.path, w.Code, w.Body.String())
		}
	}
	if f.input.WorkspaceID != "project" || f.input.Input.Text != "run" || f.canceled != "task-contract" {
		t.Fatalf("transport did not use injected task control: %+v", f)
	}
}
