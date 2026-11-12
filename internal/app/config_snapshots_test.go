package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestConfigSnapshotDiskOnlyMissDoesNotStartCodex(t *testing.T) {
	m, w, log := configFixtureManager(t)
	v, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || v.Snapshot.ErrorCode != "not_synced" || !v.Snapshot.Stale || v.Snapshot.SyncedAt != "" {
		t.Fatal(v, err)
	}
	m.syncWarmConfigSnapshots()
	if configCallCount(log, "initialize") != 0 || m.loaded.Load() != 0 {
		t.Fatal("snapshot read/worker started a process")
	}
}
func TestConfigSnapshotNativeAuthorityAndPersistence(t *testing.T) {
	m, w, log := configFixtureManager(t)
	first, err := m.ConfigSnapshot(context.Background(), w.ID, true)
	if err != nil || first.Snapshot.Source != "live" || first.Snapshot.ErrorCode != "" {
		t.Fatal(first, err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || view.Snapshot.Stale || view.Snapshot.Source != "snapshot" || view.Snapshot.SyncedAt == "" {
		t.Fatal(view, err)
	}
	if configCallCount(log, "config/read") != 1 {
		t.Fatal("disk view made native request")
	}
	if _, err = m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	if configCallCount(log, "config/read") != 2 {
		t.Fatal("ordinary native read used snapshot")
	}
	path := m.configSnapshotPath(configSnapshotPair{DefaultInstance, w.ID})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("snapshot permissions", info.Mode())
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path))} {
		info, err = os.Stat(dir)
		if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
			t.Fatal("directory permissions", dir, info, err)
		}
	}
	// A separately opened manager recovers the same sanitized view without a process.
	reopened, err := New(m.Data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || got.Snapshot.SyncedAt == "" || reopened.loaded.Load() != 0 {
		t.Fatal(got, err)
	}
}
func TestConfigSnapshotRedactsBeforeDiskAndDoesNotMutateNative(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	i, _ := m.Instance()
	identity, err := m.configSnapshotIdentity(i, w)
	if err != nil {
		t.Fatal(err)
	}
	c := nativeConfig{Config: map[string]any{"model": "local", "env": map[string]any{"PLAIN_NAME": "env-value-private"}, "http_headers": map[string]any{"X-Client": "header-private"}, "experimental_bearer_token": "bearer-private", "api_key": "key-private", "args": []any{"--password=argv-private"}, "url": "https://user:url-private@example.com/api?arbitrary=query-private#fragment-private", "model_max_output_tokens": 1234}}
	m.observeConfigSnapshot(configSnapshotPair{i.ID, w.ID}, identity, 0, c, nil)
	b, err := os.ReadFile(m.configSnapshotPath(configSnapshotPair{i.ID, w.ID}))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"env-value-private", "header-private", "bearer-private", "key-private", "argv-private", "url-private", "query-private", "fragment-private"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("secret persisted", secret)
		}
	}
	if c.Config["api_key"] != "key-private" {
		t.Fatal("native value mutated")
	}
	if !strings.Contains(string(b), `"model":"local"`) || !strings.Contains(string(b), `"model_max_output_tokens":1234`) {
		t.Fatal("useful ordinary configuration lost", string(b))
	}
}
func TestConfigSnapshotWritesInvalidateSharedHomeAndKeepNativeFresh(t *testing.T) {
	m, w, log := configFixtureManager(t)
	other, err := m.CreateWorkspace("other", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, wid := range []string{w.ID, other.ID} {
		if _, err = m.readConfig(wid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = m.ConfigCall(w.ID, "config/value/write", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	for _, wid := range []string{w.ID, other.ID} {
		view, e := m.ConfigSnapshot(context.Background(), wid, false)
		if e != nil || !view.Snapshot.Stale {
			t.Fatal("shared-home snapshot remained current", wid, view, e)
		}
	}
	c, err := m.readConfig(w.ID)
	if err != nil || c.Config["version"] != float64(1) {
		t.Fatal("native read not fresh", c, err)
	}
	if configCallCount(log, "config/read") != 3 {
		t.Fatal("native writes/reads used cached data")
	}
}
func TestConfigSnapshotFailureRetainsLastSuccessWithoutErrorSecrets(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	if _, err := m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := m.ConfigSnapshot(context.Background(), w.ID, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view, err := m.ConfigSnapshot(ctx, w.ID, true)
	if err != nil || view.Snapshot.ErrorCode != "sync_failed" || !view.Snapshot.Stale || view.Snapshot.SyncedAt != before.Snapshot.SyncedAt {
		t.Fatal(view, err)
	}
	i, _ := m.Instance()
	identity, _ := m.configSnapshotIdentity(i, w)
	m.observeConfigSnapshot(configSnapshotPair{i.ID, w.ID}, identity, 0, nativeConfig{}, errors.New("api_key=error-private"))
	b, _ := os.ReadFile(m.configSnapshotPath(configSnapshotPair{i.ID, w.ID}))
	if strings.Contains(string(b), "error-private") {
		t.Fatal("raw error persisted")
	}
}
func TestConfigSnapshotInvalidatedReadCannotRepublishOldEpoch(t *testing.T) {
	m, w, log := configFixtureManager(t)
	t.Setenv("RUNDESK_CONFIG_READ_DELAY_MS", "100")
	done := make(chan error, 1)
	go func() { _, err := m.readConfig(w.ID); done <- err }()
	until := time.Now().Add(time.Second)
	for configCallCount(log, "config/read") == 0 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	m.invalidateConfigSnapshots()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || view.Snapshot.ErrorCode != "not_synced" {
		t.Fatal("old in-flight read republished invalidated snapshot", view, err)
	}
}
func TestConfigSnapshotIdentityTracksProviderAndEffectiveDockerHome(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	if _, err := m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	b := codexProviderBinding{CodexProviderView: CodexProviderView{CodexProviderConfig: CodexProviderConfig{Revision: 1, Model: "changed"}}}
	if err := m.Store.Put("codex-provider-binding", m.codexProviderKey(DefaultInstance, w.ID), b); err != nil {
		t.Fatal(err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || view.Snapshot.ErrorCode != "not_synced" {
		t.Fatal("provider change exposed old snapshot", view, err)
	}
	i, _ := m.CreateInstance(InstancePatch{Name: "docker"})
	i.Execution = ExecutionSpec{Mode: "docker", Image: "example:test"}.normalized()
	before, err := m.configSnapshotIdentity(i, w)
	if err != nil {
		t.Fatal(err)
	}
	id := environmentID(i.ID, w.ID)
	m.environmentMu.Lock()
	m.environments[id] = &Environment{ID: id, InstanceID: i.ID, WorkspaceID: w.ID, CodexHome: filepath.Join(m.environmentRoot(id), "codex"), Revision: 1, Spec: i.Execution}
	m.environmentMu.Unlock()
	after, err := m.configSnapshotIdentity(i, w)
	if err != nil || before != after {
		t.Fatal("first environment creation invalidated its own snapshot", before, after, err)
	}
	m.environmentMu.Lock()
	m.environments[id].CodexHome = filepath.Join(m.Data, "different-home")
	m.environmentMu.Unlock()
	changed, _ := m.configSnapshotIdentity(i, w)
	if changed == after {
		t.Fatal("effective Docker home omitted from identity")
	}
}
func TestConfigSnapshotBackgroundUsesWarmOnlyAndDoesNotExtendIdleLease(t *testing.T) {
	m, w, log := configFixtureManager(t)
	if _, err := m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	h, _ := m.getHandle("config-" + DefaultInstance + "-" + w.ID)
	h.mu.Lock()
	last := h.last
	h.mu.Unlock()
	m.syncWarmConfigSnapshots()
	if configCallCount(log, "config/read") != 2 {
		t.Fatal("warm snapshot was not synchronized")
	}
	h.mu.Lock()
	same := h.last.Equal(last)
	h.mu.Unlock()
	if !same {
		t.Fatal("background sync extended idle lease")
	}
	if _, err := m.ReloadInstance(DefaultInstance); err != nil {
		t.Fatal(err)
	}
	m.syncWarmConfigSnapshots()
	if configCallCount(log, "initialize") != 1 || configCallCount(log, "config/read") != 2 {
		t.Fatal("background restarted reaped connection")
	}
	m.configSnapshotCycle.Lock()
	m.syncWarmConfigSnapshots()
	m.configSnapshotCycle.Unlock()
}
func TestConfigSnapshotPersistenceFailureIsVisible(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	if err := os.WriteFile(filepath.Join(m.Data, "config-snapshots"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, true)
	if err != nil || view.Snapshot.ErrorCode != "snapshot_save_failed" || !view.Snapshot.Stale || view.Config == nil {
		t.Fatal(view, err)
	}
}
func TestConfigSnapshotMalformedFileIsIgnored(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	i, _ := m.Instance()
	identity, _ := m.configSnapshotIdentity(i, w)
	r := configSnapshotRecord{SchemaVersion: 1, InstanceID: i.ID, WorkspaceID: w.ID, Identity: identity, Status: configSnapshotStatus{SyncedAt: "not a date"}}
	m.configSnapshotMu.Lock()
	err := m.saveSnapshotLocked(configSnapshotPair{i.ID, w.ID}, r)
	m.configSnapshotMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || view.Snapshot.ErrorCode != "not_synced" {
		data, _ := json.Marshal(view)
		t.Fatal(string(data), err)
	}
}

func TestConfigSnapshotPairIsolationAndSymlinkRejection(t *testing.T) {
	m, w, _ := configFixtureManager(t)
	if _, err := m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	i, err := m.CreateInstance(InstancePatch{Name: "separate"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.ConfigSnapshot(context.Background(), w.ID, false, i.ID)
	if err != nil || other.Snapshot.ErrorCode != "not_synced" {
		t.Fatal("other instance received default snapshot", other, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	path := m.configSnapshotPath(configSnapshotPair{DefaultInstance, w.ID})
	target := filepath.Join(t.TempDir(), "external.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	view, err := m.ConfigSnapshot(context.Background(), w.ID, false)
	if err != nil || view.Snapshot.ErrorCode != "not_synced" {
		t.Fatal("snapshot followed a symlink", view, err)
	}
}

func TestConfigSnapshotBackgroundCancelsDuringShutdown(t *testing.T) {
	t.Setenv("RUNDESK_CONFIG_CONCURRENCY_FIXTURE", "1")
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("RUNDESK_CONFIG_FIXTURE_LOG", log)
	m, err := New(t.TempDir(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			m.Close()
		}
	}()
	w := m.Workspaces()[0]
	if _, err = m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	// The child inherits delay at process launch, so reopen the config client.
	if _, err = m.ReloadInstance(DefaultInstance); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNDESK_CONFIG_READ_DELAY_MS", "200")
	if _, err = m.readConfig(w.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.syncWarmConfigSnapshots(); close(done) }()
	until := time.Now().Add(time.Second)
	for configCallCount(log, "config/read") < 3 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	m.Close()
	closed = true
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background read survived shutdown")
	}
	if time.Since(start) > time.Second {
		t.Fatal("shutdown waited for full configuration timeout")
	}
}
