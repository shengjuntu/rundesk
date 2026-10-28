package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationProjectsBindIndependentWorkspacesAndAuthorizeTasks(t *testing.T) {
	m, _, app, preset := runtimePresetRegistryFixture(t)
	key, token, err := m.CreateApplicationKey(app.AppID, KeyInput{Name: "VideoAPP", PresetIDs: []string{preset.ID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	_, defaultToken, err := m.CreateApplicationKey(app.AppID, KeyInput{Name: "default only", WorkspaceIDs: []string{app.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	q := m.Queue()
	q.Paused = true
	if _, err = m.SaveQueue(q); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	ids := []string{"aaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbb"}
	bound := []ApplicationProjectEnvironment{}
	for _, id := range ids {
		path := "/api/v1/applications/" + app.AppID + "/projects/" + id + "/environments/" + preset.ID
		r := appRequest(h, "PUT", path, `{"name":"新闻项目"}`, token, "")
		if r.Code != 200 || strings.Contains(r.Body.String(), `"path"`) {
			t.Fatal(r.Code, r.Body.String())
		}
		var b ApplicationProjectEnvironment
		if err = json.Unmarshal(r.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		if b.InstanceID != preset.InstanceID || b.ProjectID != id || b.WorkspaceID == preset.WorkspaceID {
			t.Fatal(b)
		}
		bound = append(bound, b)
		if retry := appRequest(h, "PUT", path, `{"name":"不应改名"}`, token, ""); retry.Code != 200 || !strings.Contains(retry.Body.String(), b.WorkspaceID) {
			t.Fatal("PUT replay changed binding", retry.Body.String())
		}
		if denied := appRequest(h, "PUT", path, `{"name":"wrong key"}`, defaultToken, ""); denied.Code != 403 {
			t.Fatal("old key expanded", denied.Code)
		}
	}
	if bound[0].WorkspaceID == bound[1].WorkspaceID {
		t.Fatal("projects shared workspace")
	}
	for _, b := range bound {
		w, err := m.Workspace(b.WorkspaceID)
		if err != nil || !strings.HasPrefix(w.Path, filepath.Join(m.Data, "workspaces")+string(os.PathSeparator)) {
			t.Fatal(w, err)
		}
	}
	get := "/api/v1/applications/" + app.AppID + "/projects/" + ids[0] + "/environments"
	if r := appRequest(h, "GET", get, "", defaultToken, ""); r.Code != 200 || strings.Contains(r.Body.String(), bound[0].WorkspaceID) {
		t.Fatal("unauthorized project listed", r.Code, r.Body.String())
	}
	request := `{"source":{"kind":"application","appId":"` + app.AppID + `","presetId":"` + preset.ID + `","projectId":"` + ids[0] + `"},"input":{"text":"produce video"}}`
	r := appRequest(h, "POST", "/api/v1/tasks", request, token, "project-task-idem-001")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	item := object(t, r)
	if item["spec"].(map[string]any)["workspaceId"] != bound[0].WorkspaceID {
		t.Fatal("wrong project workspace", item)
	}
	if r := appRequest(h, "GET", "/api/v1/tasks/"+item["id"].(string), "", defaultToken, ""); r.Code != 403 {
		t.Fatal("foreign key read task", r.Code)
	}
	spoof := `{"workspaceId":"` + bound[1].WorkspaceID + `","source":{"kind":"application","appId":"` + app.AppID + `","presetId":"` + preset.ID + `","projectId":"` + ids[0] + `"},"input":{"text":"spoof"}}`
	if r := appRequest(h, "POST", "/api/v1/tasks", spoof, token, "project-task-idem-002"); r.Code != 403 {
		t.Fatal("cross-project task admitted", r.Code)
	}
	if _, err = m.Enqueue(TaskSpec{InstanceID: preset.InstanceID, WorkspaceID: bound[0].WorkspaceID, Input: Input{Text: "unattributed"}}); err == nil {
		t.Fatal("project workspace accepted without project provenance")
	}
	if _, err = m.Task(item["id"].(string)); err != nil {
		t.Fatal(err)
	}
	var stored keyRecord
	if err = m.Store.Get("application-key", key.ID, &stored); err != nil || len(stored.Key.ProjectBindings) != 0 {
		t.Fatal("dynamic projects persisted into key", err)
	}
	if err = m.Store.Get("application-project-environment", projectEnvironmentKey(app.AppID, ids[0], preset.ID), &ApplicationProjectEnvironment{}); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationProjectCopiesAgentConfigurationAndInjectsProjectMCPIdentity(t *testing.T) {
	m, docker, app, preset := runtimePresetRegistryFixture(t)
	base, err := m.EnsureApplicationEnvironment(preset.InstanceID, preset.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base.CodexHome, "auth.json"), []byte("private-auth"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base.CodexHome, "config.toml"), []byte("[mcp_servers.video_app_media]\nenv_vars = [\"VIDEO_APP_PROJECT_ID\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(base.CodexHome, "skills", "video"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base.CodexHome, "skills", "video", "SKILL.md"), []byte("# Video\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id := "cccccccccccccccccccccccc"
	b, err := m.PutApplicationProjectEnvironment(app.AppID, id, preset.ID, ApplicationProjectEnvironmentInput{Name: "Video C"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := m.EnsureApplicationEnvironment(preset.InstanceID, b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.ContainerName, "animated") {
		t.Fatal("container name did not include the Agent", env.ContainerName)
	}
	for path, want := range map[string]string{"auth.json": "private-auth", "config.toml": "env_vars", "skills/video/SKILL.md": "# Video"} {
		content, readErr := os.ReadFile(filepath.Join(env.CodexHome, path))
		if readErr != nil || !strings.Contains(string(content), want) {
			t.Fatal(path, readErr)
		}
	}
	if _, err = m.EnvironmentAction(env.ID, "start", env.Revision); err != nil {
		t.Fatal(err)
	}
	docker.mu.Lock()
	calls := append([][]string(nil), docker.calls...)
	docker.mu.Unlock()
	found := false
	for _, args := range calls {
		if len(args) > 1 && args[0] == "container" && args[1] == "create" && strings.Contains(strings.Join(args, " "), "VIDEO_APP_PROJECT_ID="+id) {
			found = true
		}
	}
	if !found {
		t.Fatal("project ID was not confined to its container environment")
	}
	if err = os.WriteFile(filepath.Join(base.CodexHome, "config.toml"), []byte("[mcp_servers.video_app_media.env]\nVIDEO_APP_PROJECT_ID = \"wrong\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = m.PutApplicationProjectEnvironment(app.AppID, "dddddddddddddddddddddddd", preset.ID, ApplicationProjectEnvironmentInput{})
	expectDockerCode(t, err, "application_project_static_mcp")
}

func TestApplicationProjectDefaultEnvironmentUsesExplicitProjectIdentity(t *testing.T) {
	m, _, instance, workspace := dockerTestApp(t)
	a, err := m.RegisterApplication("video-default", ApplicationInput{Name: "Video", InstanceID: instance.ID, WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "default", WorkspaceIDs: []string{workspace.ID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	id := "eeeeeeeeeeeeeeeeeeeeeeee"
	path := "/api/v1/applications/" + a.AppID + "/projects/" + id + "/environments/__default__"
	r := appRequest(h, "PUT", path, `{"name":"My video"}`, token, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var b ApplicationProjectEnvironment
	if err = json.Unmarshal(r.Body.Bytes(), &b); err != nil || b.PresetID != "" || b.WorkspaceID == workspace.ID {
		t.Fatal(b, err)
	}
	if _, _, err = m.CreateApplicationKey(a.AppID, KeyInput{Name: "spoof", WorkspaceIDs: []string{b.WorkspaceID}, Scopes: []string{"read", "run"}}); err == nil {
		t.Fatal("project workspace accepted as legacy grant")
	}
	if _, err = m.RegisterApplication("other-video", ApplicationInput{Name: "Other", InstanceID: instance.ID, WorkspaceID: b.WorkspaceID}); err == nil {
		t.Fatal("project workspace registered as another application")
	}
	q := m.Queue()
	q.Paused = true
	if _, err = m.SaveQueue(q); err != nil {
		t.Fatal(err)
	}
	body := `{"source":{"kind":"application","appId":"` + a.AppID + `","projectId":"` + id + `"},"input":{"text":"video draft"}}`
	if r = appRequest(h, "POST", "/api/v1/tasks", body, token, "default-project-task-001"); r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	if got := object(t, r)["spec"].(map[string]any)["workspaceId"]; got != b.WorkspaceID {
		t.Fatal(got, b.WorkspaceID)
	}
}
