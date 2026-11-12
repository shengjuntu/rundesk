package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/resource"
)

func TestConfigurationRevisionReusedAcrossRunsAndFrozenAcrossEdits(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	first, err := m.CreateSession(w.ID, "first", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreateSession(w.ID, "second", "different-turn-model")
	if err != nil {
		t.Fatal(err)
	}
	second.TaskID = "next-task"
	capture := func(s Session) ConfigurationRevision {
		t.Helper()
		manifest, err := m.SessionResources(s)
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.captureConfigurationRevision(s, manifest)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b := capture(first), capture(second)
	rows, err := m.Store.List(configurationRevisionKind)
	if err != nil || len(rows) != 1 || a.ID != b.ID || len(a.Bindings) != 5 || a.Basis != "declared_at_admission" {
		t.Fatal("configuration identity depends on session-specific state", a, b, err)
	}
	data := string(rows[0])
	for _, unwanted := range []string{first.ID, second.ID, second.TaskID, w.Path} {
		if strings.Contains(data, unwanted) {
			t.Fatal("per-run data persisted in the declaration", unwanted)
		}
	}
	for _, item := range a.Bindings {
		if item.Role == resource.RoleMCP && item.Resource.VersionKind != "live" {
			t.Fatal("native MCP contents claimed as frozen")
		}
	}
	p := codexProviderBinding{CodexProviderView: CodexProviderView{State: "configured", CodexProviderConfig: CodexProviderConfig{
		Model: "native-model", Revision: 2, BaseURL: "https://user:private@example.com/secret-path", APIKeyEnv: "PRIVATE_PROVIDER_KEY",
	}}, CredentialHash: "private-hash"}
	if err := m.Store.Put("codex-provider-binding", m.codexProviderKey(DefaultInstance, w.ID), p); err != nil {
		t.Fatal(err)
	}
	changed := capture(first)
	if changed.ID == a.ID || changed.Provider.Model != p.Model {
		t.Fatal("provider binding change reused old revision", changed)
	}
	raw, _ := json.Marshal(changed)
	for _, fragment := range []string{"private-hash", "PRIVATE_PROVIDER_KEY", "secret-path"} {
		if bytes.Contains(raw, []byte(fragment)) {
			t.Fatal("provider private field persisted")
		}
	}
	historical, err := m.configurationRevisionForScope(a.ID, a.InstanceID, a.WorkspaceID)
	if err != nil || historical.Provider.State != "unmanaged" {
		t.Fatal("prior declaration did not survive an edit", historical, err)
	}
	if _, err := m.configurationRevisionForScope(a.ID, a.InstanceID, "other-workspace"); err == nil {
		t.Fatal("cross-scope revision returned")
	}
	mutated := historical
	mutated.DefaultModel = "unauthorized-overwrite"
	if err := m.Store.PutImmutable(configurationRevisionKind, historical.ID, mutated); err == nil {
		t.Fatal("immutable identity overwritten")
	}
}

func TestConfigSnapshotLegacyCodexEnvelopeUpgradesOnNextSync(t *testing.T) {
	m, w, log := configFixtureManager(t)
	i, _ := m.Instance()
	identity, err := m.configSnapshotIdentity(i, w)
	if err != nil {
		t.Fatal(err)
	}
	p := configSnapshotPair{i.ID, w.ID}
	old := map[string]any{"schemaVersion": 1, "instanceId": i.ID, "workspaceId": w.ID, "identity": identity, "epoch": 0,
		"value":    json.RawMessage(`{"config":{"model":"v071","api_key":"legacy-private"},"origins":{},"layers":[]}`),
		"snapshot": configSnapshotStatus{SyncedAt: "2026-01-01T00:00:00Z", LastAttemptAt: "2026-01-01T00:00:00Z"}}
	path := m.configSnapshotPath(p)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	after, fileErr := os.ReadFile(path)
	if err != nil || fileErr != nil || !bytes.Equal(before, after) || view.Config["model"] != "v071" || view.Config["api_key"] != "[redacted]" || configCallCount(log, "initialize") != 0 {
		t.Fatal("disk-only legacy read mutated content or consulted engine", view, err, fileErr)
	}
	if _, err := m.ConfigSnapshot(context.Background(), w.ID, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	var envelope configSnapshotRecord
	if err != nil || json.Unmarshal(data, &envelope) != nil || envelope.SchemaVersion != 2 || envelope.Engine != "codex" || envelope.Format != codexConfigFormat {
		t.Fatal("missing versioned engine envelope", string(data), err)
	}
	envelope.Engine = "another-engine"
	if err := m.saveSnapshotLocked(p, envelope); err != nil {
		t.Fatal(err)
	}
	view, err = m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || view.Snapshot.ErrorCode != "not_synced" {
		t.Fatal("decoded foreign engine payload", view, err)
	}
}

func TestConfigurationRevisionKunUsesOwnDeclaredBindings(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	i, err := m.CreateInstance(InstancePatch{Name: "maintenance fixture"})
	if err != nil {
		t.Fatal(err)
	}
	// A restored maintenance instance needs no live worker for this inspection.
	m.mu.Lock()
	m.instances[i.ID].AgentRuntime.Kind = "kun"
	m.instances[i.ID].AgentRuntime.Model = "maintenance-model"
	m.mu.Unlock()
	servers := kunMCPConfig{Revision: 3, Servers: map[string]any{"build-center": map[string]any{"command": "private-command", "env": map[string]any{"KEY": "private-secret"}}}}
	if err := m.Store.Put("kun-mcp-config", i.ID, servers); err != nil {
		t.Fatal(err)
	}
	session := Session{ID: "maintenance-session", InstanceID: i.ID, WorkspaceID: w.ID, RuntimeKind: "kun", ExecutionMode: "local"}
	manifest, err := m.SessionResources(session)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := m.captureConfigurationRevision(session, manifest)
	if err != nil || rev.Engine != "kun" || rev.Provider.Source != "kun-runtime-config" || rev.Provider.Model != "maintenance-model" || m.loaded.Load() != 0 {
		t.Fatal("Kun declaration used an incorrect configuration source", rev, err)
	}
	data, _ := json.Marshal(rev)
	if !bytes.Contains(data, []byte("build-center")) || bytes.Contains(data, []byte("private-command")) || bytes.Contains(data, []byte("private-secret")) {
		t.Fatal("Kun declaration did not preserve metadata-only MCP bindings", string(data))
	}
	m.mu.Lock()
	m.instances[i.ID].AgentRuntime.Kind = "unregistered"
	m.mu.Unlock()
	if _, err := m.ConfigSnapshot(context.Background(), w.ID, true, i.ID); err == nil {
		t.Fatal("unknown engine used Codex snapshot adapter")
	}
	if _, err := m.readConfig(w.ID, i.ID); err == nil || m.loaded.Load() != 0 {
		t.Fatal("unknown engine used Codex native transport", err)
	}
}
