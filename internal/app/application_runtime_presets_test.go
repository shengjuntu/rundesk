package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func freshRuntimePresetPair(t *testing.T, m *Manager, name string) (Instance, Workspace) {
	t.Helper()
	i, err := m.CreateInstance(InstancePatch{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "docker", Image: "video-animation:test"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := m.CreateWorkspace(name, "")
	if err != nil {
		t.Fatal(err)
	}
	return i, w
}

func runtimePresetRegistryFixture(t *testing.T) (*Manager, *fakeDocker, Application, ApplicationRuntimePreset) {
	t.Helper()
	m, docker, base, workspace := dockerTestApp(t)
	a, err := m.RegisterApplication("preset-video", ApplicationInput{Name: "Video", InstanceID: base.ID, WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatal(err)
	}
	i, w := freshRuntimePresetPair(t, m, "Animated")
	p, err := m.PutApplicationRuntimePreset(a.AppID, "animated", ApplicationRuntimePresetInput{Name: "Animated", InstanceID: i.ID, WorkspaceID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	return m, docker, a, p
}

func presetTaskSpec(p ApplicationRuntimePreset) TaskSpec {
	return TaskSpec{InstanceID: p.InstanceID, WorkspaceID: p.WorkspaceID, Source: SessionSource{Kind: "application", AppID: p.AppID, PresetID: p.ID}, Input: Input{Text: "Produce a news video"}}
}

func TestRuntimePresetAPIKeepsBindingsImmutableAndRetriesIdempotent(t *testing.T) {
	m, docker, a, p := runtimePresetRegistryFixture(t)
	h := NewHandler(m, userTestAdmin, true)
	path := "/api/v1/applications/" + a.AppID + "/runtime-presets/" + p.ID
	input := ApplicationRuntimePresetInput{Name: p.Name, Description: p.Description, InstanceID: p.InstanceID, WorkspaceID: p.WorkspaceID}
	call := func(in ApplicationRuntimePresetInput) *http.Response {
		t.Helper()
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		r := appRequest(h, "PUT", path, string(body), userTestAdmin, "")
		return r.Result()
	}
	if res := call(input); res.StatusCode != 200 {
		t.Fatal("identical retry failed", res.StatusCode)
	}
	again, err := m.applicationPreset(a.AppID, p.ID)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("retry changed the binding or revision", again, err)
	}
	otherInstance, otherWorkspace := freshRuntimePresetPair(t, m, "Other")
	for _, mutate := range []func(*ApplicationRuntimePresetInput){
		func(in *ApplicationRuntimePresetInput) { in.InstanceID = otherInstance.ID },
		func(in *ApplicationRuntimePresetInput) { in.WorkspaceID = otherWorkspace.ID },
	} {
		changed := input
		changed.Revision = &p.Revision
		mutate(&changed)
		if res := call(changed); res.StatusCode != 409 {
			t.Fatal("runtime tuple was reassigned", res.StatusCode)
		}
	}
	disabled := false
	input.Enabled = &disabled
	input.Revision = &p.Revision
	if res := call(input); res.StatusCode != 200 {
		t.Fatal("disable failed", res.StatusCode)
	}
	updated, err := m.applicationPreset(a.AppID, p.ID)
	if err != nil || updated.Enabled || updated.Revision != p.Revision+1 || updated.Created != p.Created {
		t.Fatal("invalid disable transition", updated, err)
	}
	// Replaying the acknowledged edit is safe even with the old revision.
	if res := call(input); res.StatusCode != 200 {
		t.Fatal("lost disable acknowledgement cannot be retried", res.StatusCode)
	}
	input.Name = "Stale rename"
	if res := call(input); res.StatusCode != 409 {
		t.Fatal("stale edit overwrote newer metadata", res.StatusCode)
	}
	var after Application
	if err := m.Store.Get("application", a.AppID, &after); err != nil || !reflect.DeepEqual(a, after) {
		t.Fatal("preset edit mutated the default application binding", after, err)
	}
	docker.mu.Lock()
	calls := len(docker.calls)
	docker.mu.Unlock()
	if calls != 0 {
		t.Fatal("catalog edits executed Docker", calls)
	}
}

func TestRuntimePresetKeysAreExplicitAndDoNotExpandLegacyAuthority(t *testing.T) {
	m, docker, base, workspace := dockerTestApp(t)
	a, err := m.RegisterApplication("preset-keys", ApplicationInput{Name: "Video", InstanceID: base.ID, WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatal(err)
	}
	legacyKey, legacyToken, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Existing client", WorkspaceIDs: []string{workspace.ID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	i, w := freshRuntimePresetPair(t, m, "Animated")
	p, err := m.PutApplicationRuntimePreset(a.AppID, "animated", ApplicationRuntimePresetInput{Name: "Animated", InstanceID: i.ID, WorkspaceID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	presetKey, presetToken, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Animation client", PresetIDs: []string{p.ID}, Scopes: []string{"read", "run", "files", "schedules"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(presetKey.WorkspaceIDs) != 0 || !reflect.DeepEqual(presetKey.PresetBindings, []ApplicationPresetBinding{{PresetID: p.ID, InstanceID: p.InstanceID, WorkspaceID: p.WorkspaceID}}) {
		t.Fatal("preset authority was not frozen separately", presetKey)
	}
	h := NewHandler(m, userTestAdmin, true)
	for _, tc := range []struct {
		token string
		count int
	}{{legacyToken, 0}, {presetToken, 1}} {
		r := appRequest(h, "GET", "/api/v1/applications/"+a.AppID+"/runtime-presets", "", tc.token, "")
		var rows []ApplicationRuntimePreset
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rows) != nil || len(rows) != tc.count {
			t.Fatal("catalog ignored explicit preset scope", r.Code, r.Body.String())
		}
	}
	for _, prefix := range []string{"/api", "/api/v1"} {
		body := `{"source":{"presetId":"` + p.ID + `"},"title":"Animation"}`
		r := appRequest(h, "POST", prefix+"/sessions", body, presetToken, "animation-create-"+strings.ReplaceAll(prefix, "/", ""))
		if r.Code != 200 {
			t.Fatal("explicit preset could not create a session", prefix, r.Code, r.Body.String())
		}
		session, err := m.Session(object(t, r)["id"].(string))
		if err != nil || session.InstanceID != i.ID || session.WorkspaceID != w.ID || session.Source.PresetID != p.ID {
			t.Fatal("preset session ran under a different binding", session, err)
		}
		for _, tc := range []struct{ token, body string }{
			{legacyToken, body},
			{legacyToken, `{"instanceId":"` + i.ID + `","workspaceId":"` + w.ID + `"}`},
			{presetToken, `{"workspaceId":"` + workspace.ID + `"}`},
			{presetToken, `{"instanceId":"` + base.ID + `","workspaceId":"` + w.ID + `","source":{"presetId":"` + p.ID + `"}}`},
		} {
			if r := appRequest(h, "POST", prefix+"/sessions", tc.body, tc.token, ""); r.Code != 403 {
				t.Fatal("unauthorized default/preset combination admitted", prefix, r.Code, r.Body.String())
			}
		}
	}
	if _, _, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Implicit runtime", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read"}}); err == nil {
		t.Fatal("new legacy workspace grant bypassed explicit preset authorization")
	}
	if _, _, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Unknown runtime", PresetIDs: []string{"unknown"}, Scopes: []string{"read"}}); err == nil {
		t.Fatal("unknown preset was granted")
	}
	_, _, err = m.CreateUser(UserInput{Name: "Implicit member runtime", Grants: []UserGrant{{AppID: a.AppID, WorkspaceID: p.WorkspaceID, Role: "viewer"}}})
	appCode(t, err, "invalid_grant")
	if r := appRequest(h, "POST", "/api/v1/sessions", `{"workspaceId":"`+workspace.ID+`"}`, legacyToken, "legacy-still-default-001"); r.Code != 200 || object(t, r)["instanceId"] != base.ID {
		t.Fatal("legacy default behavior changed", r.Code, r.Body.String())
	}
	if err := m.checkTaskAuthorization(legacyKey.ID, presetTaskSpec(p), false); err == nil {
		t.Fatal("queued execution expanded the old key")
	}
	if err := m.checkTaskAuthorization(presetKey.ID, presetTaskSpec(p), true); err != nil {
		t.Fatal("explicit scheduled task lost its preset authority", err)
	}
	docker.mu.Lock()
	calls := len(docker.calls)
	docker.mu.Unlock()
	if calls != 0 {
		t.Fatal("session/catalog tests invoked Docker", calls)
	}
}

func TestRuntimePresetDisabledRemainsDiscoverableAndHistoryReadable(t *testing.T) {
	m, _, a, p := runtimePresetRegistryFixture(t)
	key, token, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Animation", PresetIDs: []string{p.ID}, Scopes: []string{"read", "run"}})
	if err != nil {
		t.Fatal(err)
	}
	spec := presetTaskSpec(p)
	session, err := m.CreateSessionWithSource(p.WorkspaceID, "Existing animation", "", p.InstanceID, spec.Source)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := m.PutApplicationRuntimePreset(a.AppID, p.ID, ApplicationRuntimePresetInput{Name: p.Name, InstanceID: p.InstanceID, WorkspaceID: p.WorkspaceID, Enabled: &disabled, Revision: &p.Revision}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	r := appRequest(h, "GET", "/api/v1/applications/"+a.AppID+"/runtime-presets", "", token, "")
	var rows []ApplicationRuntimePreset
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].Enabled {
		t.Fatal("disabled runtime disappeared from authorized catalog", r.Code, r.Body.String())
	}
	if r := appRequest(h, "GET", "/api/v1/sessions/"+session.ID, "", token, ""); r.Code != 200 {
		t.Fatal("disabled preset hid history", r.Code, r.Body.String())
	}
	r = appRequest(h, "POST", "/api/v1/sessions", `{"source":{"presetId":"`+p.ID+`"}}`, token, "disabled-preset-session-001")
	if r.Code != 409 || object(t, r)["code"] != "runtime_preset_disabled" {
		t.Fatal("disabled preset accepted a new session", r.Code, r.Body.String())
	}
	appCode(t, m.checkTaskAuthorization(key.ID, spec, false), "runtime_preset_disabled")
	appCode(t, m.checkTaskAuthorization("", spec, false), "runtime_preset_disabled")
}

func TestRuntimePresetRegistrationRejectsPreviouslyUsedResources(t *testing.T) {
	for _, kind := range []string{"session", "task", "schedule", "application-key", "upload-access", "user", "nonempty"} {
		t.Run(kind, func(t *testing.T) {
			m, _, base, workspace := dockerTestApp(t)
			a, err := m.RegisterApplication("dirty-preset", ApplicationInput{Name: "Video", InstanceID: base.ID, WorkspaceID: workspace.ID})
			if err != nil {
				t.Fatal(err)
			}
			i, w := freshRuntimePresetPair(t, m, "Previously used")
			var record any
			switch kind {
			case "session":
				if _, err := m.CreateSession(w.ID, "Prior human session", "", i.ID); err != nil {
					t.Fatal(err)
				}
			case "task":
				record = Task{ID: "old-record", Spec: TaskSpec{InstanceID: i.ID, WorkspaceID: workspace.ID}}
			case "schedule":
				record = Schedule{ID: "old-record", Spec: ScheduleSpec{Task: TaskSpec{InstanceID: base.ID, WorkspaceID: w.ID}}}
			case "application-key":
				record = keyRecord{Key: ApplicationKey{AppID: a.AppID, InstanceID: base.ID, WorkspaceIDs: []string{w.ID}}}
			case "upload-access":
				record = uploadAccess{AppID: a.AppID, InstanceID: i.ID, WorkspaceID: workspace.ID}
			case "user":
				record = userRecord{User: User{ID: "old-record", Enabled: true, Grants: []UserGrant{{AppID: a.AppID, WorkspaceID: w.ID, Role: "viewer"}}}}
			case "nonempty":
				if err := os.WriteFile(filepath.Join(w.Path, "prior-news.txt"), []byte("Original material"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if record != nil {
				if err := m.Store.Put(kind, "old-record", record); err != nil {
					t.Fatal(err)
				}
			}
			_, err = m.PutApplicationRuntimePreset(a.AppID, "animated", ApplicationRuntimePresetInput{Name: "Animated", InstanceID: i.ID, WorkspaceID: w.ID})
			code := "runtime_preset_history_conflict"
			if kind == "nonempty" {
				code = "runtime_preset_workspace_not_empty"
			}
			appCode(t, err, code)
			if _, err := m.applicationPreset(a.AppID, "animated"); err == nil {
				t.Fatal("rejected registration still created a preset")
			}
		})
	}
}

func TestRuntimePresetRegistrationRejectsWorkspaceAliasesAndNesting(t *testing.T) {
	for _, kind := range []string{"same-path", "child", "parent", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			m, _, base, workspace := dockerTestApp(t)
			a, err := m.RegisterApplication("alias-preset", ApplicationInput{Name: "Video", InstanceID: base.ID, WorkspaceID: workspace.ID})
			if err != nil {
				t.Fatal(err)
			}
			i, w := freshRuntimePresetPair(t, m, "Empty candidate")
			path := w.Path
			switch kind {
			case "child":
				path = filepath.Join(w.Path, "nested")
			case "parent":
				path = filepath.Dir(w.Path)
			case "symlink":
				path = filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(w.Path, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.CreateWorkspace("Conflicting alias", path); err != nil {
				t.Fatal("pre-registration alias setup failed", err)
			}
			_, err = m.PutApplicationRuntimePreset(a.AppID, "animated", ApplicationRuntimePresetInput{Name: "Animated", InstanceID: i.ID, WorkspaceID: w.ID})
			appCode(t, err, "runtime_preset_workspace_overlap")
		})
	}
}

func TestRuntimePresetReservationRejectsLaterWorkspaceAliasesAndForeignSessions(t *testing.T) {
	m, _, a, p := runtimePresetRegistryFixture(t)
	w, err := m.Workspace(p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{w.Path, filepath.Join(w.Path, "nested"), filepath.Dir(w.Path)} {
		_, err := m.CreateWorkspace("Alias after reservation", path)
		appCode(t, err, "runtime_preset_workspace_overlap")
	}
	alias := filepath.Join(t.TempDir(), "preset-alias")
	if err := os.Symlink(w.Path, alias); err != nil {
		t.Fatal(err)
	}
	_, err = m.CreateWorkspace("Symlink alias", alias)
	appCode(t, err, "runtime_preset_workspace_overlap")
	for _, tc := range []struct {
		name, iid, wid string
		source         SessionSource
	}{
		{"human-both", p.InstanceID, p.WorkspaceID, SessionSource{Kind: "human"}},
		{"human-instance", p.InstanceID, a.WorkspaceID, SessionSource{Kind: "human"}},
		{"human-workspace", a.InstanceID, p.WorkspaceID, SessionSource{Kind: "human"}},
		{"missing-preset", p.InstanceID, p.WorkspaceID, SessionSource{Kind: "application", AppID: a.AppID}},
		{"wrong-app", p.InstanceID, p.WorkspaceID, SessionSource{Kind: "application", AppID: "foreign-app", PresetID: p.ID}},
		{"wrong-instance", a.InstanceID, p.WorkspaceID, SessionSource{Kind: "application", AppID: a.AppID, PresetID: p.ID}},
		{"wrong-workspace", p.InstanceID, a.WorkspaceID, SessionSource{Kind: "application", AppID: a.AppID, PresetID: p.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.CreateSessionWithSource(tc.wid, tc.name, "", tc.iid, tc.source); err == nil {
				t.Fatal("reserved runtime accepted an unrelated session")
			}
			spec := TaskSpec{InstanceID: tc.iid, WorkspaceID: tc.wid, Source: tc.source}
			if err := m.checkTaskAuthorization("", spec, false); err == nil {
				t.Fatal("trusted task path bypassed the preset reservation")
			}
		})
	}
	if _, err := m.CreateSessionWithSource(p.WorkspaceID, "Correct preset", "", p.InstanceID, presetTaskSpec(p).Source); err != nil {
		t.Fatal("reservation rejected its own exact binding", err)
	}
	other, otherWorkspace := freshRuntimePresetPair(t, m, "Another app")
	if _, err := m.RegisterApplication("steal-preset-instance", ApplicationInput{Name: "Foreign", InstanceID: p.InstanceID, WorkspaceID: otherWorkspace.ID}); err == nil {
		t.Fatal("another application claimed a reserved preset instance")
	}
	if _, err := m.RegisterApplication("steal-preset-workspace", ApplicationInput{Name: "Foreign", InstanceID: other.ID, WorkspaceID: p.WorkspaceID}); err == nil {
		t.Fatal("another application claimed a reserved preset workspace")
	}
}
