package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestInstanceStorageLegacyKnownAdapterRoundTrip(t *testing.T) {
	legacy := []byte(`{"id":"maint","name":"maintenance","codexHome":"/native-state","agentRuntime":{"kind":"kun","endpoint":"http://localhost:8000/v1","model":"m","futureOption":{"opaque":5},"harness":{"futureSetting":true}}}`)
	var restored persistedInstance
	if err := json.Unmarshal(legacy, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Name = "new name"
	restored.AgentRuntime.Model = "new-model"
	data, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		SchemaVersion int
		AgentRuntime  map[string]json.RawMessage
		CodexHome     string
	}
	if err = json.Unmarshal(data, &disk); err != nil || disk.SchemaVersion != 1 || disk.CodexHome != "/native-state" || string(disk.AgentRuntime["futureOption"]) != `{"opaque":5}` || string(disk.AgentRuntime["harness"]) != `{"futureSetting":true}` {
		t.Fatal("lost original settings", string(data), err)
	}
	var v071 Instance
	if err = json.Unmarshal(data, &v071); err != nil || v071.AgentRuntime.Kind != "kun" || v071.AgentRuntime.Model != "new-model" || v071.AgentRuntime.Endpoint != "http://localhost:8000/v1" {
		t.Fatal("old reader lost supported native options", v071, err)
	}
}

func TestInstanceStorageOpaqueEngineSurvivesRegistrationEditRestart(t *testing.T) {
	data := t.TempDir()
	raw := json.RawMessage(`{"schemaVersion":1,"id":"default","name":"future","codexHome":"/native-home","execution":{"mode":"docker","image":"example:stable"},"agentRuntime":{"kind":"pi-agent","endpoint":{"futureShape":true},"secret":"private-adapter-data"}}`)
	db, err := store.Open(filepath.Join(data, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Put("instance", DefaultInstance, raw); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := New(data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	i, err := m.Instance(DefaultInstance)
	if err != nil {
		m.Close()
		t.Fatal(err)
	}
	public, err := json.Marshal(i)
	if err != nil || bytes.Contains(public, []byte("private-adapter-data")) || i.AgentRuntime.Kind != "pi-agent" || i.CodexHome != "/native-home" || i.Execution.Mode != "docker" {
		m.Close()
		t.Fatal("leaked opaque options or lost kind", string(public), err)
	}
	w := m.Workspaces()[0]
	envID := environmentID(i.ID, w.ID)
	m.environmentMu.Lock()
	m.environments[envID] = &Environment{ID: envID, InstanceID: i.ID, WorkspaceID: w.ID, Revision: 1, Spec: i.Execution}
	m.environmentMu.Unlock()
	for _, action := range []string{"start", "recreate"} {
		if _, err = m.EnvironmentAction(envID, action, 1); err == nil {
			m.Close()
			t.Fatal("unregistered environment action accepted", action)
		} else if e, ok := err.(*apiError); !ok || e.Code != "backend_unsupported" {
			m.Close()
			t.Fatal("environment action ran past adapter admission", action, err)
		}
	}
	if _, err = m.Diagnostics(w.ID, i.ID); err == nil {
		m.Close()
		t.Fatal("unregistered engine probed as Codex")
	}
	if _, err = m.CreateSession(w.ID, "", "", i.ID); err == nil {
		m.Close()
		t.Fatal("unregistered engine admitted")
	}
	if _, err = m.PatchInstance(i.ID, InstancePatch{Name: "renamed", Revision: i.Revision}); err != nil {
		m.Close()
		t.Fatal(err)
	}
	var stored json.RawMessage
	if err = m.Store.Get("instance", i.ID, &stored); err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()
	if !bytes.Contains(stored, []byte(`"endpoint":{"futureShape":true}`)) || !bytes.Contains(stored, []byte("private-adapter-data")) {
		t.Fatal("opaque native options were lost on metadata save", string(stored))
	}
	m, err = New(data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	i, err = m.Instance(DefaultInstance)
	if err != nil || i.Name != "renamed" || i.AgentRuntime.Kind != "pi-agent" {
		t.Fatal("future environment was not restored", i, err)
	}
}

func TestInstanceStorageRejectsNewerSchemaBeforeWriting(t *testing.T) {
	data := t.TempDir()
	path := filepath.Join(data, "state.db")
	raw := json.RawMessage(`{"id":"default","schemaVersion":99,"agentRuntime":{"kind":"codex"}}`)
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Put("instance", "default", raw); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if m, err := New(data, "", true); err == nil {
		m.Close()
		t.Fatal("opened unknown instance schema")
	} else if !strings.Contains(err.Error(), "unsupported instance storage schema") {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var existing json.RawMessage
	if err = db.Get("instance", "default", &existing); err != nil || !bytes.Equal(existing, raw) {
		t.Fatal("failed open overwrote existing instance", err, string(existing))
	}
	if _, err = os.Stat(filepath.Join(data, "demo-codex")); !os.IsNotExist(err) {
		t.Fatal("failed schema validation provisioned a home", err)
	}
}
