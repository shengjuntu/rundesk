package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/shengjuntu/rundesk/internal/store"
)

const (
	participationKind       = "session-participation"
	activeParticipationKind = "session-active-participation"
	nativeBindingKind       = "session-native-binding"
)

// A conversation may keep an executor's native state across several distinct
// spans of responsibility. The owner of that private state includes its
// workspace and environment, even for two instances of the same engine.
type participationScope struct {
	SessionID     string `json:"sessionId"`
	InstanceID    string `json:"instanceId"`
	WorkspaceID   string `json:"workspaceId"`
	Engine        string `json:"engine"`
	ExecutionMode string `json:"executionMode"`
	EnvironmentID string `json:"environmentId,omitempty"`
}

func participationScopeOf(s Session) participationScope {
	mode := s.ExecutionMode
	if mode == "" {
		mode = "local" // Session rows predating Docker execution.
	}
	return participationScope{s.ID, s.InstanceID, s.WorkspaceID, runtimeKind(s.RuntimeKind), mode, s.EnvironmentID}
}

func (p participationScope) nativeOwnerID() string {
	b, _ := json.Marshal(p)
	return fmt.Sprintf("native-%x", sha256.Sum256(b))
}

type sessionParticipant struct {
	SchemaVersion int                `json:"schemaVersion"`
	ID            string             `json:"id"`
	PreviousID    string             `json:"previousId,omitempty"`
	Ordinal       int                `json:"ordinal"`
	Scope         participationScope `json:"scope"`
	NativeOwnerID string             `json:"nativeOwnerId"`
}

// RunParticipationInfo exposes the historical interval without its private
// native handle. Select only by the ID and scope recorded on the accepted run.
type RunParticipationInfo struct {
	ID         string `json:"id"`
	PreviousID string `json:"previousId,omitempty"`
	Ordinal    int    `json:"ordinal"`
}

func (m *Manager) participationForRun(id string, scope participationScope) (RunParticipationInfo, error) {
	var p sessionParticipant
	if err := m.Store.Get(participationKind, id, &p); err != nil {
		return RunParticipationInfo{}, err
	}
	if p.SchemaVersion != 1 || p.ID != id || p.Scope != scope || p.Ordinal < 1 || p.NativeOwnerID != scope.nativeOwnerID() {
		return RunParticipationInfo{}, failure(409, "run_participation_mismatch", "执行段记录与该轮的归属不一致")
	}
	return RunParticipationInfo{ID: p.ID, PreviousID: p.PreviousID, Ordinal: p.Ordinal}, nil
}

func (m *Manager) currentParticipantLocked(sid string) (sessionParticipant, error) {
	var p sessionParticipant
	var id string
	if err := m.Store.Get(activeParticipationKind, sid, &id); err != nil {
		return p, err
	}
	if err := m.Store.Get(participationKind, id, &p); err != nil {
		return p, failure(409, "participation_missing", "当前执行段记录不可读取")
	}
	if p.SchemaVersion != 1 || p.Scope.SessionID != sid || p.ID != id || p.Ordinal < 1 || p.NativeOwnerID != p.Scope.nativeOwnerID() {
		return p, failure(409, "participation_invalid", "当前执行段记录与会话不匹配")
	}
	return p, nil
}

// The current pointer is mutable, the segment records are append-only. Returning
// to a former executor uses its native owner again but starts a new segment.
func (m *Manager) prepareParticipantLocked(s Session) (sessionParticipant, []store.Record, error) {
	scope := participationScopeOf(s)
	previous, err := m.currentParticipantLocked(s.ID)
	if err == nil && previous.Scope == scope {
		return previous, nil, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sessionParticipant{}, nil, err
	}
	next := sessionParticipant{SchemaVersion: 1, ID: store.ID(), PreviousID: previous.ID,
		Ordinal: previous.Ordinal + 1, Scope: scope, NativeOwnerID: scope.nativeOwnerID()}
	return next, []store.Record{
		{Kind: participationKind, ID: next.ID, Value: next},
		{Kind: activeParticipationKind, ID: s.ID, Value: next.ID},
	}, nil
}

func (m *Manager) sessionParticipation(s Session) (sessionParticipant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.sessions[s.ID]
	if current == nil || participationScopeOf(*current) != participationScopeOf(s) {
		return sessionParticipant{}, failure(409, "participation_changed", "会话当前执行者已变化")
	}
	p, records, err := m.prepareParticipantLocked(s)
	if err == nil && len(records) > 0 {
		err = m.Store.PutMany(records...)
	}
	return p, err
}

type nativeSessionBinding struct {
	SchemaVersion   int                `json:"schemaVersion"`
	OwnerID         string             `json:"ownerId"`
	Scope           participationScope `json:"scope"`
	StateRootKey    string             `json:"stateRootKey"`
	NativeSessionID string             `json:"nativeSessionId"`
	// Captured from the active span, never persisted on a reusable native binding.
	ParticipationID string `json:"-"`
}

func newNativeBinding(p sessionParticipant, stateRoot string) nativeSessionBinding {
	return nativeSessionBinding{
		SchemaVersion: 1, OwnerID: p.NativeOwnerID, Scope: p.Scope, ParticipationID: p.ID,
		StateRootKey: fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(stateRoot)))),
	}
}

func (m *Manager) loadNativeBinding(want *nativeSessionBinding) error {
	var saved nativeSessionBinding
	if err := m.Store.Get(nativeBindingKind, want.OwnerID, &saved); err != nil {
		return err
	}
	if saved.SchemaVersion != want.SchemaVersion || saved.OwnerID != want.OwnerID ||
		saved.Scope != want.Scope || saved.StateRootKey != want.StateRootKey || saved.NativeSessionID == "" {
		return failure(409, "native_binding_mismatch", "原生会话绑定与当前执行环境不匹配")
	}
	want.NativeSessionID = saved.NativeSessionID
	return nil
}

// Cornerstone of legacy adoption: an old ThreadID belongs only to the original
// participation. Later spans without a binding always start a new native thread.
func (h runtimeSessionHost) NativeBinding(s Session, stateRoot string) (nativeSessionBinding, error) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	p, err := h.m.currentParticipantLocked(s.ID)
	if err != nil {
		return nativeSessionBinding{}, err
	}
	current := h.m.sessions[s.ID]
	if current == nil || current.RunID != s.RunID || p.Scope != participationScopeOf(*current) || p.Scope != participationScopeOf(s) {
		return nativeSessionBinding{}, failure(409, "participation_changed", "会话当前执行者或运行已变化")
	}
	b := newNativeBinding(p, stateRoot)
	if err := h.m.loadNativeBinding(&b); !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	if p.Ordinal == 1 {
		b.NativeSessionID = current.ThreadID
	}
	return b, nil
}

func (h runtimeSessionHost) ConfirmNativeBinding(s Session, b nativeSessionBinding, nativeID string) error {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	current := h.m.sessions[s.ID]
	p, err := h.m.currentParticipantLocked(s.ID)
	if err != nil {
		return err
	}
	if current == nil || current.RunID != s.RunID || !active(current.Status) || b.Scope != participationScopeOf(*current) ||
		p.Scope != b.Scope || p.ID != b.ParticipationID || p.NativeOwnerID != b.OwnerID {
		return failure(409, "participation_changed", "原生响应已不属于当前会话执行")
	}
	expected := b.NativeSessionID
	err = h.m.loadNativeBinding(&b)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if nativeID == "" || (expected != "" && expected != nativeID) || (b.NativeSessionID != "" && b.NativeSessionID != nativeID) {
		return failure(409, "native_session_mismatch", "原生响应未确认请求的会话")
	}
	if err == nil && current.ThreadID == nativeID {
		return nil
	}
	b.NativeSessionID = nativeID
	next := *current
	next.ThreadID, next.Updated = nativeID, store.Now()
	if err = h.m.Store.PutMany(store.Record{Kind: nativeBindingKind, ID: b.OwnerID, Value: b}, store.Record{Kind: "session", ID: s.ID, Value: next}); err != nil {
		return err
	}
	*current = next
	return nil
}

// Called under m.mu. Native acknowledgement, event insertion, cursor advancement
// and the session projection are committed by Store.ImportKun together.
func (m *Manager) kunBindingRecords(s Session) ([]store.Record, error) {
	p, records, err := m.prepareParticipantLocked(s)
	if err != nil {
		return nil, err
	}
	b := newNativeBinding(p, filepath.Join(m.Data, "kun", "sessions", s.ID))
	err = m.loadNativeBinding(&b)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if b.NativeSessionID != "" && b.NativeSessionID != s.ID {
		return nil, failure(409, "native_binding_mismatch", "Kun worker 会话身份不匹配")
	}
	if err == nil {
		return records, nil
	}
	b.NativeSessionID = s.ID
	return append(records, store.Record{Kind: nativeBindingKind, ID: b.OwnerID, Value: b}), nil
}
