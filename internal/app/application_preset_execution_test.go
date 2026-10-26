package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func disableRuntimePreset(t *testing.T, m *Manager, p ApplicationRuntimePreset) {
	t.Helper()
	enabled := false
	_, err := m.PutApplicationRuntimePreset(p.AppID, p.ID, ApplicationRuntimePresetInput{Name: p.Name, Description: p.Description, InstanceID: p.InstanceID, WorkspaceID: p.WorkspaceID, Enabled: &enabled, Revision: &p.Revision})
	if err != nil {
		t.Fatal(err)
	}
}
func presetSession(t *testing.T, m *Manager, p ApplicationRuntimePreset) Session {
	t.Helper()
	s, err := m.CreateSessionWithSource(p.WorkspaceID, "Preset project", "", p.InstanceID, SessionSource{Kind: "application", AppID: p.AppID, PresetID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestRuntimePresetSkillsArePreparedScopedAndSanitized(t *testing.T) {
	m, docker, a, p := runtimePresetRegistryFixture(t)
	_, token, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Preset read/run", PresetIDs: []string{p.ID}, Scopes: []string{"read", "run"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	path := "/api/v1/applications/" + a.AppID + "/runtime/skills?presetId=" + p.ID
	check := func(want int) string {
		t.Helper()
		r := appRequest(h, "GET", path, "", token, "")
		if r.Code != want {
			t.Fatal(r.Code, r.Body.String())
		}
		return r.Body.String()
	}
	check(409)
	if len(docker.containers) != 0 || docker.commands != 0 {
		t.Fatal("read started unprepared runtime")
	}
	if err = m.SaveSkill(p.WorkspaceID, "video-director", "---\nname: video-director\ndescription: private narrative\n---\nprivate instructions", p.InstanceID, "instance"); err != nil {
		t.Fatal(err)
	}
	_, err = m.PrepareApplicationRuntime(a.AppID, ApplicationRuntimeInput{PresetID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	commands := docker.commands
	check(409)
	if docker.commands != commands {
		t.Fatal("read started native configuration process")
	}
	_, err = m.PrepareApplicationRuntime(a.AppID, ApplicationRuntimeInput{PresetID: p.ID, Probe: true})
	if err != nil {
		t.Fatal(err)
	}
	body := check(200)
	var view ApplicationRuntimeSkills
	if err = json.Unmarshal([]byte(body), &view); err != nil || view.AppID != a.AppID || view.PresetID != p.ID || view.InstanceID != p.InstanceID || view.WorkspaceID != p.WorkspaceID || len(view.Skills) != 1 || view.Skills[0].Name != "video-director" || !view.Skills[0].Enabled {
		t.Fatal(body, err)
	}
	if !filepath.IsAbs(view.Skills[0].Path) {
		t.Fatal("not a native Skill reference", view)
	}
	for _, bad := range []string{"private narrative", "private instructions", "description", "scope", "origins", "mcp_servers", "PRIVATE"} {
		if strings.Contains(body, bad) {
			t.Fatal("unexpected configuration field", bad, body)
		}
	}
	for _, denied := range []string{"/api/v1/instances/" + p.InstanceID + "/configuration", "/api/v1/applications/" + a.AppID + "/runtime/skills", "/api/v1/applications/" + a.AppID + "/runtime/skills?presetId=other"} {
		r := appRequest(h, "GET", denied, "", token, "")
		if r.Code != 403 {
			t.Fatal(denied, r.Code, r.Body.String())
		}
	}
	// Disabled history remains readable, without creating a new process.
	disableRuntimePreset(t, m, p)
	check(200)
}
func TestRuntimePresetConnectIsIndependentAndApplicationKeyReadOnly(t *testing.T) {
	m, _, a, p := runtimePresetRegistryFixture(t)
	_, token, err := m.CreateApplicationKey(a.AppID, KeyInput{Name: "Preset", PresetIDs: []string{p.ID}, Scopes: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	in := ApplicationConnect{Name: a.Name, InstallationID: "video-install", PresetID: p.ID, Defaults: ApplicationDefaults{Model: "must-not-change-model", Skills: map[string]map[string]string{"director": {"SKILL.md": "---\nname: director\ndescription: produce\n---\ninitial"}}, MCP: map[string]map[string]any{"video": {"url": "http://localhost:9988/mcp", "enabled": true}}}}
	body, _ := json.Marshal(in)
	r := appRequest(h, "POST", "/api/v1/applications/"+a.AppID+"/connect", string(body), token, "")
	if r.Code != 409 || !strings.Contains(r.Body.String(), "preset_configuration_required") {
		t.Fatal(r.Code, r.Body.String())
	}
	if _, err = m.SkillBundle(p.WorkspaceID, "director", p.InstanceID, "instance"); err == nil {
		t.Fatal("read-only connect seeded skills")
	}
	connected, err := m.ConnectApplication(a.AppID, in)
	if err != nil || connected.Status != "connected" || connected.PresetID != p.ID || connected.Application.PresetID != p.ID {
		t.Fatal(connected, err)
	}
	current, _, err := m.applicationRuntimeBinding(a.AppID)
	if err != nil || current.InstanceID != a.InstanceID || current.WorkspaceID != a.WorkspaceID {
		t.Fatal("changed legacy default", current, err)
	}
	if err = m.SaveSkill(p.WorkspaceID, "director", "---\nname: director\ndescription: produce\n---\nadmin changed", p.InstanceID, "instance"); err != nil {
		t.Fatal(err)
	}
	in.Defaults.Skills["director"]["SKILL.md"] = "---\nname: director\ndescription: produce\n---\nreplace administrator"
	body, _ = json.Marshal(in)
	for _, credential := range []string{token, userTestAdmin} {
		r = appRequest(h, "POST", "/api/v1/applications/"+a.AppID+"/connect", string(body), credential, "")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	content, err := m.ReadSkill(p.WorkspaceID, "director", p.InstanceID, "instance")
	if err != nil || !strings.Contains(content, "admin changed") {
		t.Fatal(content, err)
	}
	i, _ := m.Instance(p.InstanceID)
	if i.DefaultModel == in.Defaults.Model {
		t.Fatal("preset seeded application model")
	}
	var legacy applicationConnection
	if err = m.Store.Get("application-connection", a.AppID, &legacy); err == nil {
		t.Fatal("preset overwrote default initialization checkpoint")
	}
	// A different preset needs its own administrator initialization.
	secondI, secondW := freshRuntimePresetPair(t, m, "Second")
	second, err := m.PutApplicationRuntimePreset(a.AppID, "second", ApplicationRuntimePresetInput{Name: "Second", InstanceID: secondI.ID, WorkspaceID: secondW.ID})
	if err != nil {
		t.Fatal(err)
	}
	in.PresetID = second.ID
	in.Defaults.MCP = nil
	connected, err = m.ConnectApplication(a.AppID, in)
	if err != nil || connected.Application.InstanceID != second.InstanceID {
		t.Fatal(connected, err)
	}
	secondText, err := m.ReadSkill(second.WorkspaceID, "director", second.InstanceID, "instance")
	if err != nil || !strings.Contains(secondText, "replace administrator") {
		t.Fatal("reused another preset's checkpoint", secondText, err)
	}
}
func TestRuntimePresetDisableDoesNotInterruptAcceptedRun(t *testing.T) {
	m, _, _, p := runtimePresetRegistryFixture(t)
	s := presetSession(t, m, p)
	// Pause native connection after Start accepts, so disable always precedes
	// connect. The admitted run must retain its original resource identity.
	m.executionMu.Lock()
	_, err := m.Start(s.ID, Input{Text: "审批"})
	if err != nil {
		m.executionMu.Unlock()
		t.Fatal(err)
	}
	disableRuntimePreset(t, m, p)
	m.executionMu.Unlock()
	waiting := waitState(t, m, s.ID, "waiting")
	_, err = m.Steer(s.ID, SteerInput{Input: Input{Text: "new instruction"}, ExpectedTurnID: waiting.TurnID, RequestID: "disabled-steer-123"})
	expectDockerCode(t, err, "runtime_preset_disabled")
	if err = m.Stop(s.ID); err != nil {
		t.Fatal("disabled preset cannot stop existing work", err)
	}
	waitState(t, m, s.ID, "interrupted")
	_, err = m.PrepareRecovery(s.ID, waiting.RunID)
	expectDockerCode(t, err, "runtime_preset_disabled")
}
func TestRuntimePresetDisableIsRecheckedAfterSessionAdmissionWait(t *testing.T) {
	m, docker, _, p := runtimePresetRegistryFixture(t)
	s := presetSession(t, m, p)
	h, _ := m.getHandle(s.ID)
	h.admission.Lock()
	result := make(chan error, 1)
	go func() { _, err := m.Start(s.ID, Input{Text: "Produce draft"}); result <- err }()
	// Start acquires op only after input validation; admission deliberately keeps
	// it blocked before the final preset check.
	until := time.Now().Add(5 * time.Second)
	locked := false
	for time.Now().Before(until) {
		if !h.op.TryLock() {
			locked = true
			break
		}
		h.op.Unlock()
		time.Sleep(time.Millisecond)
	}
	if !locked {
		h.admission.Unlock()
		t.Fatal("start did not reach admission")
	}
	disableRuntimePreset(t, m, p)
	h.admission.Unlock()
	select {
	case err := <-result:
		expectDockerCode(t, err, "runtime_preset_disabled")
	case <-time.After(5 * time.Second):
		t.Fatal("start did not return")
	}
	current, _ := m.Session(s.ID)
	if current.Status != "idle" || current.RunID != "" || len(docker.containers) != 0 {
		t.Fatal("disabled waiter was accepted", current)
	}
}
func TestRuntimePresetRecoveryAndSkillValidationCannotStartDisabledEnvironment(t *testing.T) {
	m, docker, _, p := runtimePresetRegistryFixture(t)
	s := presetSession(t, m, p)
	if err := m.SaveSkill(p.WorkspaceID, "director", "---\nname: director\ndescription: produce\n---\ntext", p.InstanceID, "instance"); err != nil {
		t.Fatal(err)
	}
	disableRuntimePreset(t, m, p)
	w, _ := m.Workspace(p.WorkspaceID)
	_, err := m.Start(s.ID, Input{Text: "Draft", Skills: []Skill{{Name: "director", Path: filepath.Join(w.Path, "unknown")}}})
	expectDockerCode(t, err, "runtime_preset_disabled")
	_, err = m.PrepareRecovery(s.ID, "prior-run")
	expectDockerCode(t, err, "runtime_preset_disabled")
	_, err = m.PrepareApplicationRuntime(p.AppID, ApplicationRuntimeInput{PresetID: p.ID, Probe: true})
	expectDockerCode(t, err, "runtime_preset_disabled")
	if len(docker.containers) != 0 || docker.commands != 0 {
		t.Fatal("disabled operation started environment", docker.calls)
	}
	// Native files remain intact and the exact stored binding is kept.
	if _, err = os.Stat(w.Path); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimePresetRejectsPreparedForeignSessionAndReplacedDirectory(t *testing.T) {
	m, docker, base, defaultW := dockerTestApp(t)
	a, err := m.RegisterApplication("late-reservation", ApplicationInput{Name: "Video", InstanceID: base.ID, WorkspaceID: defaultW.ID})
	if err != nil {
		t.Fatal(err)
	}
	i, w := freshRuntimePresetPair(t, m, "Pending human session")
	prepared, err := m.prepareSession(w.ID, "Prepared before reservation", "", i.ID, SessionSource{Kind: "human"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.PutApplicationRuntimePreset(a.AppID, "late", ApplicationRuntimePresetInput{Name: "Late", InstanceID: i.ID, WorkspaceID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	// This reproduces a session creation that passed its old source check before
	// registration but was persisted after it. It must never execute there.
	if err = m.Store.Put("session", prepared.ID, prepared); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.sessions[prepared.ID] = &prepared
	m.mu.Unlock()
	_, err = m.Start(prepared.ID, Input{Text: "read files"})
	expectDockerCode(t, err, "runtime_preset_binding_conflict")
	s := presetSession(t, m, p)
	original := w.Path + "-original"
	if err = os.Rename(w.Path, original); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(defaultW.Path, w.Path); err != nil {
		t.Fatal(err)
	}
	_, err = m.Start(s.ID, Input{Text: "read files"})
	expectDockerCode(t, err, "runtime_preset_workspace_changed")
	if len(docker.containers) != 0 || docker.commands != 0 {
		t.Fatal("foreign source or replaced directory started execution")
	}
}
