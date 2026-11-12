package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestConfigurationReadsAdmittedRunAndNativeEvidenceAtCursor(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "recorded run", "")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := m.Start(s.ID, Input{Text: "a private prompt"})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitState(t, m, s.ID, "completed")
	h := NewHandler(m, "", true)
	url := "/sessions/" + s.ID + "/configuration?runId=" + accepted.RunID
	private := NewHandler(m, "private-run-history-test-token", true)
	if r := v1Request(private, "GET", url, "", ""); r.Code != 401 {
		t.Fatal("historical read bypassed administrator authentication", r.Code)
	}
	if r := v1Request(h, "GET", url+"&through=-1", "", ""); r.Code != 400 {
		t.Fatal("invalid trace cursor accepted", r.Code)
	}
	if r := v1Request(h, "GET", url+"&through=0", "", ""); r.Code != 404 {
		t.Fatal("empty trace included a run", r.Code)
	}
	if r := v1Request(h, "GET", url+"&through=999999999999", "", ""); r.Code != 400 {
		t.Fatal("trace cursor beyond session journal accepted", r.Code)
	}
	inspect := func(path string) RunAttribution {
		t.Helper()
		reply := v1Request(h, "GET", path, "", "")
		if reply.Code != 200 {
			t.Fatal(reply.Code, reply.Body.String())
		}
		if strings.Contains(reply.Body.String(), "a private prompt") || strings.Contains(reply.Body.String(), w.Path) {
			t.Fatal("historical response leaked a workspace path or input")
		}
		var body struct {
			Run                   RunAttribution
			ConfigurationRevision *struct{ ID string }
		}
		if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.ConfigurationRevision == nil || body.ConfigurationRevision.ID != body.Run.ConfigurationRevisionID {
			t.Fatal("missing historical configuration", body)
		}
		return body.Run
	}
	run := inspect(url)
	if run.Native == nil || run.Native.NativeSessionID != completed.ThreadID || run.Native.NativeRunID != completed.TurnID ||
		run.Native.Source != "run/turn" || run.SessionID != s.ID || run.InstanceID != DefaultInstance || run.WorkspaceID != w.ID ||
		run.Engine != "codex" || run.Basis != "declared_at_admission" || run.Participation == nil || run.Participation.ID != run.ParticipationID || run.Participation.Ordinal != 1 || run.Native.SourceEventID <= run.InputEventID {
		t.Fatalf("incomplete run attribution: %+v", run)
	}
	beforeNative := inspect(fmt.Sprintf("%s&through=%d", url, run.InputEventID))
	if beforeNative.Native != nil {
		t.Fatal("later native acknowledgment escaped fixed cursor")
	}
	foreign, err := m.CreateSession(w.ID, "foreign", "")
	if err != nil {
		t.Fatal(err)
	}
	if r := v1Request(h, "GET", "/sessions/"+foreign.ID+"/configuration?runId="+accepted.RunID, "", ""); r.Code != 404 {
		t.Fatal("run escaped session scope", r.Code)
	}

	// Alter present-day execution state. Reading run history must neither use it
	// nor return a new native thread under the old run's identity.
	i, _ := m.Instance()
	p := i.Permissions
	p.Sandbox = "read-only"
	if _, err := m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Revision: i.Revision, Permissions: &p}); err != nil {
		t.Fatal(err)
	}
	other, err := m.Start(s.ID, Input{Text: "another turn"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "completed")
	next := inspect("/sessions/" + s.ID + "/configuration?runId=" + other.RunID)
	again := inspect(url)
	if next.Native == nil || again.Native == nil || next.ConfigurationRevisionID == run.ConfigurationRevisionID || next.ParticipationID != run.ParticipationID || next.Native.NativeRunID == run.Native.NativeRunID || again.ConfigurationRevisionID != run.ConfigurationRevisionID || *again.Native != *run.Native {
		t.Fatalf("historical attribution moved: old=%+v new=%+v again=%+v", run, next, again)
	}
}

func TestLegacyRunAttributionOmitsUnknownsAndValidatesKunScope(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "historical fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	const rid = "legacy-run"
	_, err = m.Store.Add(s.ID, "internal", "run/input", map[string]any{"runId": rid, "instanceId": "recorded-instance", "input": map[string]string{"text": "secret-original"}, "resources": map[string]any{"scope": map[string]string{"sessionId": s.ID, "workspaceId": "recorded-workspace"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Store.Add(s.ID, "in", "kun/run.started", map[string]string{"type": "kun/run.started", "sessionId": "foreign", "runId": rid})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := m.runAttribution(context.Background(), s.ID, rid, nil)
	if err != nil || legacy.Native != nil || legacy.Engine != "" || legacy.InstanceID != "recorded-instance" || legacy.WorkspaceID != "recorded-workspace" || legacy.ParticipationID != "" || legacy.ConfigurationRevisionID != "" || legacy.Basis != "legacy_evidence" {
		t.Fatal("guessed historical fields", legacy, err)
	}
	ev, err := m.Store.Add(s.ID, "in", "kun/run.started", map[string]string{"type": "kun/run.started", "sessionId": s.ID, "runId": rid})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := m.runAttribution(context.Background(), s.ID, rid, &ev.ID)
	if err != nil || confirmed.Engine != "kun" || confirmed.Native == nil || confirmed.Native.NativeSessionID != s.ID || confirmed.Native.NativeRunID != rid || confirmed.Native.SourceEventID != ev.ID {
		t.Fatal(confirmed, err)
	}
	_, err = m.Store.Add(s.ID, "internal", "run/input", map[string]any{
		"runId": "older-declaration", "instanceRevision": 0,
		"resources": map[string]any{
			"scope":     map[string]string{"sessionId": s.ID, "instanceId": "older-agent", "workspaceId": "older-work"},
			"resources": []any{map[string]any{"configuration": map[string]string{"backend": "codex"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := m.runAttribution(context.Background(), s.ID, "older-declaration", nil)
	if err != nil || declaration.Engine != "codex" || declaration.InstanceRevision == nil || *declaration.InstanceRevision != 0 || declaration.Native != nil || declaration.ConfigurationRevisionID != "" {
		t.Fatal("legacy declaration or zero revision lost", declaration, err)
	}
}
