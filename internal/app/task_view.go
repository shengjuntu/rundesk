package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
)

// TaskResourceView is a point-in-time projection of platform state and resource
// references. Configuration is the current binding, not a claim that a past run
// executed with today's instance settings. Original bytes stay with their owner.
type TaskResourceView struct {
	Schema             int               `json:"schema"`
	ObservedAt         string            `json:"observedAt"`
	StateSource        string            `json:"stateSource"`
	Availability       string            `json:"availability"`
	UnavailableReason  string            `json:"unavailableReason,omitempty"`
	ConfigurationBasis string            `json:"configurationBasis"`
	Task               TaskStateView     `json:"task"`
	Runtime            *TaskRuntimeView  `json:"runtime,omitempty"`
	Resources          resource.Manifest `json:"resources"`
}

type TaskStateView struct {
	ID              string `json:"id"`
	SessionID       string `json:"sessionId"`
	RunID           string `json:"runId,omitempty"`
	Status          string `json:"status"`
	CancelRequested bool   `json:"cancelRequested"`
	Created         string `json:"created"`
	Updated         string `json:"updated"`
	StartedAt       string `json:"startedAt,omitempty"`
	FinishedAt      string `json:"finishedAt,omitempty"`
}

type TaskRuntimeView struct {
	Kind              string `json:"kind"`
	ExecutionMode     string `json:"executionMode"`
	EnvironmentID     string `json:"environmentId,omitempty"`
	SessionID         string `json:"sessionId"`
	RunID             string `json:"runId,omitempty"`
	ThreadID          string `json:"threadId,omitempty"`
	TurnID            string `json:"turnId,omitempty"`
	NativeAssociation string `json:"nativeAssociation"`
}

type TaskResourceExport struct {
	TaskID     string `json:"taskId"`
	ObservedAt string `json:"observedAt"`
	Path       string `json:"path"`
}

func (m *Manager) TaskResources(id string) (TaskResourceView, error) {
	var view TaskResourceView
	// Match queue -> session lock ordering used by admission/reconciliation.
	// Do not reconcile or submit work as a side effect of inspecting a task.
	m.queueMu.Lock()
	t, ok := m.tasks[id]
	if !ok {
		m.queueMu.Unlock()
		return view, failure(404, "task_not_found", "任务不存在")
	}
	s, err := m.Session(t.SessionID)
	m.queueMu.Unlock()
	view = TaskResourceView{
		Schema: 1, ObservedAt: store.Now(), StateSource: "platform_sqlite",
		Availability: "available", ConfigurationBasis: "current_bindings",
		Task: TaskStateView{
			ID: t.ID, SessionID: t.SessionID, RunID: t.RunID, Status: t.Status,
			CancelRequested: t.CancelRequested, Created: t.Created, Updated: t.Updated,
			StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		},
		Resources: resource.Manifest{
			Schema: 1,
			Scope: resource.Scope{InstanceID: t.Spec.InstanceID, WorkspaceID: t.Spec.WorkspaceID,
				SessionID: t.SessionID, TaskID: t.ID},
			Resources: []resource.Descriptor{}, Bindings: []resource.Binding{},
		},
	}
	if err != nil {
		var unavailable *apiError
		if errors.As(err, &unavailable) && unavailable.Code == "session_not_found" {
			// Deleting a terminal session does not delete the task record or
			// original files. Preserve the known task facts, without recreating
			// a session or inferring native identity from today's instance.
			view.Availability, view.UnavailableReason = "unavailable", "session_not_found"
			view.ConfigurationBasis = "unavailable"
			return view, nil
		}
		return TaskResourceView{}, err
	}
	manifest, err := m.SessionResources(s)
	if err != nil {
		return TaskResourceView{}, err
	}
	view.Resources = manifest
	view.Runtime = &TaskRuntimeView{
		Kind: runtimeKind(s.RuntimeKind), ExecutionMode: s.ExecutionMode,
		EnvironmentID: s.EnvironmentID, SessionID: s.ID, RunID: t.RunID,
		NativeAssociation: "not_started",
	}
	if t.RunID != "" {
		view.Runtime.NativeAssociation = "unavailable"
		// A session can later be continued manually. Never attribute that newer
		// turn/thread to the task's historical run.
		if s.RunID == t.RunID {
			view.Runtime.NativeAssociation = "current_run"
			view.Runtime.ThreadID, view.Runtime.TurnID = s.ThreadID, s.TurnID
		}
	}
	return view, nil
}

// ExportTaskResources refreshes one atomic JSON view. Deleting or editing this
// file cannot change the task; a subsequent refresh reconstructs it from the
// platform records. The API caller must be an administrator.
func (m *Manager) ExportTaskResources(id string) (TaskResourceExport, error) {
	var result TaskResourceExport
	view, err := m.TaskResources(id)
	if err != nil {
		return result, err
	}
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00:") {
		return result, failure(400, "invalid_task_id", "任务编号不能用于目录视图")
	}
	data, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return result, err
	}
	data = append(data, '\n')
	root, err := os.OpenRoot(m.Data)
	if err != nil {
		return result, err
	}
	defer root.Close()
	dir := filepath.Join("tasks", id)
	// A task directory is host metadata, never a symlink into native state or
	// the workspace. OpenRoot also confines every subsequent write to Data.
	for _, path := range []string{"tasks", dir} {
		info, e := root.Lstat(path)
		if os.IsNotExist(e) {
			if e = root.Mkdir(path, 0700); e != nil && !os.IsExist(e) {
				return result, e
			}
			info, e = root.Lstat(path)
		}
		if e != nil {
			return result, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, failure(409, "task_view_path_invalid", "任务视图目录必须为普通目录")
		}
	}
	tmp := filepath.Join(dir, ".resources-"+store.ID()+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	defer root.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return result, err
	}
	dest := filepath.Join(dir, "resources.json")
	if err = root.Rename(tmp, dest); err != nil {
		return result, err
	}
	return TaskResourceExport{TaskID: id, ObservedAt: view.ObservedAt, Path: filepath.Join(m.Data, dest)}, nil
}
