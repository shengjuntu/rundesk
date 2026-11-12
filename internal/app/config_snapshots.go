package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

const configSnapshotInterval = 5 * time.Minute
const configSnapshotSchema = 2
const codexConfigFormat = "codex.config-read.v1"

type configSnapshotPair struct{ Instance, Workspace string }
type configSnapshotStatus struct {
	Source                 string `json:"source"`
	SyncedAt               string `json:"syncedAt"`
	LastAttemptAt          string `json:"lastAttemptAt"`
	Stale                  bool   `json:"stale"`
	ErrorCode              string `json:"errorCode,omitempty"`
	RefreshIntervalSeconds int    `json:"refreshIntervalSeconds"`
}
type configSnapshotRecord struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Engine        string               `json:"engine"`
	Format        string               `json:"format"`
	InstanceID    string               `json:"instanceId"`
	WorkspaceID   string               `json:"workspaceId"`
	Identity      string               `json:"identity"`
	Epoch         uint64               `json:"epoch"`
	Value         json.RawMessage      `json:"value"`
	Status        configSnapshotStatus `json:"snapshot"`
	codexView     nativeConfig
}
type configSnapshotView struct {
	nativeConfig
	Snapshot configSnapshotStatus `json:"snapshot"`
}

// This is a display copy only. No caller may use it for writes or credential
// recovery. Native config/read remains the authority for layered configuration.
func sanitizedConfigSnapshot(c nativeConfig) nativeConfig {
	var out nativeConfig
	data, _ := json.Marshal(snapshotRedact(redact(c)))
	_ = json.Unmarshal(data, &out)
	return out
}
func snapshotRedact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			normal := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
			_, credentialText := value.(string)
			if sensitiveField(key) || credentialText && (strings.Contains(normal, "token") || strings.Contains(normal, "secret") || strings.Contains(normal, "password")) || normal == "args" || normal == "command" {
				x[key] = "[redacted]"
			} else {
				x[key] = snapshotRedact(value)
			}
		}
	case []any:
		for n, value := range x {
			x[n] = snapshotRedact(value)
		}
	case string:
		if u, err := url.Parse(x); err == nil && u.Host != "" {
			if u.User != nil {
				u.User = url.User("[redacted]")
			}
			// URL queries and fragments can carry capability tokens with arbitrary names.
			if u.RawQuery != "" {
				u.RawQuery = "redacted"
			}
			if u.Fragment != "" {
				u.Fragment = "redacted"
			}
			return u.String()
		}
	}
	return v
}

// Inspect metadata only: effectiveInstance would create environment directories.
func (m *Manager) configSnapshotIdentity(i Instance, w Workspace) (string, error) {
	var environment any
	if i.Execution.normalized().Mode == "docker" {
		i.CodexHome = filepath.Join(m.environmentRoot(environmentID(i.ID, w.ID)), "codex")
		detail := struct {
			Home     string
			Revision int
			Spec     ExecutionSpec
		}{i.CodexHome, 1, i.Execution.normalized()}
		m.environmentMu.Lock()
		v := m.environments[environmentID(i.ID, w.ID)]
		if v != nil {
			i.CodexHome = v.CodexHome
			detail.Home, detail.Revision, detail.Spec = v.CodexHome, v.Revision, v.Spec
		}
		m.environmentMu.Unlock()
		environment = detail
	}
	provider, err := m.codexProviderBinding(i.ID, w.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	project, _, err := m.projectEnvironmentForWorkspace(w.ID)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal([]any{configConnectionIdentity(m, i, w), environment, provider, project, w.Revision})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
func (m *Manager) configSnapshotPath(p configSnapshotPair) string {
	return filepath.Join(m.Data, "config-snapshots", p.Instance, p.Workspace+".json")
}
func snapshotRelativePath(p configSnapshotPair) string {
	return filepath.Join("config-snapshots", p.Instance, p.Workspace+".json")
}
func snapshotDirectorySafe(root *os.Root, p configSnapshotPair) bool {
	for _, path := range []string{"config-snapshots", filepath.Join("config-snapshots", p.Instance)} {
		info, err := root.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
func (m *Manager) configSnapshotEpochLocked() uint64 {
	var epoch uint64
	if m.Store.Get("system", "config-snapshot-epoch", &epoch) == nil && epoch > m.configSnapshotEpoch {
		m.configSnapshotEpoch = epoch
	}
	return m.configSnapshotEpoch
}
func (m *Manager) invalidateConfigSnapshots() {
	m.configSnapshotMu.Lock()
	defer m.configSnapshotMu.Unlock()
	m.configSnapshotEpoch = m.configSnapshotEpochLocked() + 1
	_ = m.Store.Put("system", "config-snapshot-epoch", m.configSnapshotEpoch)
}
func (m *Manager) snapshotRecordLocked(p configSnapshotPair, identity string) (configSnapshotRecord, bool) {
	var r configSnapshotRecord
	root, err := os.OpenRoot(m.Data)
	if err != nil {
		return r, false
	}
	defer root.Close()
	if !snapshotDirectorySafe(root, p) {
		return r, false
	}
	info, err := root.Lstat(snapshotRelativePath(p))
	if err != nil || !info.Mode().IsRegular() {
		return r, false
	}
	f, err := root.Open(snapshotRelativePath(p))
	if err != nil {
		return r, false
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return r, false
	}
	data, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err != nil || json.Unmarshal(data, &r) != nil || r.InstanceID != p.Instance || r.WorkspaceID != p.Workspace || r.Identity != identity {
		return configSnapshotRecord{}, false
	}
	// 0.71 only persisted Codex config/read. Upgrade its envelope in memory;
	// a disk-only read must neither rewrite the file nor contact an engine.
	if r.SchemaVersion == 1 && r.Engine == "" && r.Format == "" {
		r.SchemaVersion, r.Engine, r.Format = configSnapshotSchema, "codex", codexConfigFormat
	}
	if r.SchemaVersion != configSnapshotSchema || r.Engine != "codex" || r.Format != codexConfigFormat || len(r.Value) == 0 || r.Value[0] != '{' || json.Unmarshal(r.Value, &r.codexView) != nil {
		return configSnapshotRecord{}, false
	}
	if _, err = time.Parse(time.RFC3339Nano, r.Status.SyncedAt); err != nil {
		return configSnapshotRecord{}, false
	}
	r.codexView = sanitizedConfigSnapshot(r.codexView)
	r.Value, err = json.Marshal(r.codexView)
	if err != nil {
		return configSnapshotRecord{}, false
	}
	if r.Status.ErrorCode != "" {
		r.Status.ErrorCode = "sync_failed"
	}
	r.Status.Source = "snapshot"
	r.Status.RefreshIntervalSeconds = int(configSnapshotInterval / time.Second)
	r.Status.Stale = r.Epoch != m.configSnapshotEpochLocked() || r.Status.ErrorCode != "" || time.Since(mustSnapshotTime(r.Status.SyncedAt)) >= configSnapshotInterval
	return r, true
}
func mustSnapshotTime(s string) time.Time { v, _ := time.Parse(time.RFC3339Nano, s); return v }
func (m *Manager) saveSnapshotLocked(p configSnapshotPair, r configSnapshotRecord) error {
	root, err := os.OpenRoot(m.Data)
	if err != nil {
		return err
	}
	defer root.Close()
	dir := filepath.Dir(snapshotRelativePath(p))
	if err := root.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if !snapshotDirectorySafe(root, p) {
		return errors.New("invalid snapshot directory")
	}
	temp := filepath.Join(dir, ".snapshot-"+store.ID()+".tmp")
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(r)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, snapshotRelativePath(p))
}
func (m *Manager) observeConfigSnapshot(p configSnapshotPair, identity string, epoch uint64, c nativeConfig, err error) {
	i, e := m.Instance(p.Instance)
	if e != nil || runtimeKind(i.AgentRuntime.Kind) != "codex" {
		return
	}
	w, e := m.Workspace(p.Workspace)
	if e != nil {
		return
	}
	current, e := m.configSnapshotIdentity(i, w)
	if e != nil || current != identity {
		return
	}
	m.configSnapshotMu.Lock()
	defer m.configSnapshotMu.Unlock()
	if m.configSnapshotPairs == nil {
		m.configSnapshotPairs = map[configSnapshotPair]bool{}
	}
	m.configSnapshotPairs[p] = true
	if epoch != m.configSnapshotEpochLocked() {
		return
	}
	now := store.Now()
	if err != nil {
		if r, ok := m.snapshotRecordLocked(p, identity); ok {
			r.Status.LastAttemptAt = now
			r.Status.ErrorCode = "sync_failed"
			r.Status.Stale = true
			_ = m.saveSnapshotLocked(p, r)
		}
		return
	}
	value, err := json.Marshal(sanitizedConfigSnapshot(c))
	if err != nil {
		return
	}
	r := configSnapshotRecord{SchemaVersion: configSnapshotSchema, Engine: "codex", Format: codexConfigFormat, InstanceID: p.Instance, WorkspaceID: p.Workspace, Identity: identity, Epoch: epoch, Value: value, Status: configSnapshotStatus{Source: "snapshot", SyncedAt: now, LastAttemptAt: now, RefreshIntervalSeconds: int(configSnapshotInterval / time.Second)}}
	_ = m.saveSnapshotLocked(p, r)
}

func (m *Manager) ConfigSnapshot(ctx context.Context, wid string, refresh bool, ids ...string) (configSnapshotView, error) {
	i, err := m.Instance(ids...)
	if err != nil {
		return configSnapshotView{}, err
	}
	w, err := m.Workspace(wid)
	if err != nil {
		return configSnapshotView{}, err
	}
	if i.AgentRuntime.Kind == "kun" {
		c, e := m.readConfigContext(ctx, wid, ids...)
		return configSnapshotView{nativeConfig: sanitizedConfigSnapshot(c), Snapshot: configSnapshotStatus{Source: "live", SyncedAt: store.Now()}}, e
	}
	if runtimeKind(i.AgentRuntime.Kind) != "codex" {
		return configSnapshotView{}, failure(400, "configuration_adapter_unsupported", "此引擎没有已注册的配置快照适配器")
	}
	if !slug.MatchString(i.ID) || !slug.MatchString(w.ID) {
		return configSnapshotView{}, failure(400, "config_snapshot_scope", "配置范围无效")
	}
	identity, err := m.configSnapshotIdentity(i, w)
	if err != nil {
		return configSnapshotView{}, err
	}
	p := configSnapshotPair{i.ID, w.ID}
	m.configSnapshotMu.Lock()
	if m.configSnapshotPairs == nil {
		m.configSnapshotPairs = map[configSnapshotPair]bool{}
	}
	m.configSnapshotPairs[p] = true
	r, ok := m.snapshotRecordLocked(p, identity)
	m.configSnapshotMu.Unlock()
	if ok && !refresh {
		return configSnapshotView{r.codexView, r.Status}, nil
	}
	if !refresh {
		return configSnapshotView{nativeConfig: nativeConfig{Config: map[string]any{}, Origins: map[string]any{}}, Snapshot: configSnapshotStatus{Source: "snapshot", Stale: true, ErrorCode: "not_synced", RefreshIntervalSeconds: int(configSnapshotInterval / time.Second)}}, nil
	}
	attempt := time.Now()
	c, err := m.readConfigContext(ctx, wid, ids...)
	if err == nil {
		status := configSnapshotStatus{Source: "live", SyncedAt: store.Now(), LastAttemptAt: store.Now(), RefreshIntervalSeconds: int(configSnapshotInterval / time.Second)}
		m.configSnapshotMu.Lock()
		saved, exists := m.snapshotRecordLocked(p, identity)
		epoch := m.configSnapshotEpochLocked()
		m.configSnapshotMu.Unlock()
		if !exists || saved.Epoch != epoch || mustSnapshotTime(saved.Status.LastAttemptAt).Before(attempt) {
			status.Stale = true
			status.ErrorCode = "snapshot_save_failed"
		}
		return configSnapshotView{sanitizedConfigSnapshot(c), status}, nil
	}
	m.configSnapshotMu.Lock()
	r, ok = m.snapshotRecordLocked(p, identity)
	m.configSnapshotMu.Unlock()
	if ok {
		r.Status.Stale = true
		r.Status.ErrorCode = "sync_failed"
		return configSnapshotView{r.codexView, r.Status}, nil
	}
	return configSnapshotView{}, err
}

// One permanent worker owns all periodic syncs; it never calls connect or starts
// an environment. A reaped/closed client remains asleep until explicit user use.
func (m *Manager) configSnapshotLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(configSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.syncWarmConfigSnapshots()
		}
	}
}
func (m *Manager) snapshotExecutionAllowed(p configSnapshotPair) bool {
	project, found, err := m.projectEnvironmentForWorkspace(p.Workspace)
	if err != nil {
		return false
	}
	if found {
		return m.checkApplicationPresetExecution(SessionSource{Kind: "application", AppID: project.AppID, ProjectID: project.ProjectID, PresetID: project.PresetID}, p.Instance, p.Workspace) == nil
	}
	presets, err := m.allApplicationPresets()
	if err != nil {
		return false
	}
	for _, preset := range presets {
		if preset.InstanceID == p.Instance && preset.WorkspaceID == p.Workspace {
			return m.checkApplicationPresetExecution(SessionSource{Kind: "application", AppID: preset.AppID, PresetID: preset.ID}, p.Instance, p.Workspace) == nil
		}
	}
	return m.checkApplicationPresetExecution(SessionSource{}, p.Instance, p.Workspace) == nil
}
func (m *Manager) syncWarmConfigSnapshots() {
	if !m.configSnapshotCycle.TryLock() {
		return
	}
	defer m.configSnapshotCycle.Unlock()
	m.configSnapshotMu.Lock()
	pairs := make([]configSnapshotPair, 0, len(m.configSnapshotPairs))
	for p := range m.configSnapshotPairs {
		pairs = append(pairs, p)
	}
	m.configSnapshotMu.Unlock()
	for _, p := range pairs {
		if m.ctx.Err() != nil {
			return
		}
		if !m.snapshotExecutionAllowed(p) {
			continue
		}
		i, err := m.Instance(p.Instance)
		if err != nil || runtimeKind(i.AgentRuntime.Kind) != "codex" {
			continue
		}
		w, err := m.Workspace(p.Workspace)
		if err != nil {
			continue
		}
		identity, err := m.configSnapshotIdentity(i, w)
		if err != nil {
			continue
		}
		m.mu.Lock()
		h := m.handles["config-"+i.ID+"-"+w.ID]
		m.mu.Unlock()
		if h == nil || !h.op.TryRLock() {
			continue
		}
		h.mu.Lock()
		c := h.client
		ready := c != nil && h.configRetiring != c && h.configIdentity == configConnectionIdentity(m, i, w) && !c.Closing() && h.configWriters == 0
		h.mu.Unlock()
		if !ready {
			h.op.RUnlock()
			continue
		}
		m.configSnapshotMu.Lock()
		epoch := m.configSnapshotEpochLocked()
		m.configSnapshotMu.Unlock()
		ctx, cancel := context.WithTimeout(m.ctx, configCallTimeout)
		raw, readErr, owned := configReadCall(ctx, h, c, "config/read", map[string]any{"cwd": w.Path, "includeLayers": true})
		cancel()
		var transport *rpc.TransportError
		if errors.As(readErr, &transport) || c.Closing() {
			detachBrokenConfigClient(h, c)
		} else if owned && errors.Is(readErr, context.DeadlineExceeded) {
			retireConfigClient(h, c)
		}
		h.op.RUnlock()
		closeRetiredConfigClient(h)
		var value nativeConfig
		if readErr == nil {
			readErr = json.Unmarshal(raw, &value)
		}
		m.observeConfigSnapshot(p, identity, epoch, value, readErr)
	}
}
