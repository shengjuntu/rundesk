package app

import (
	"database/sql"
	"errors"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestParticipationAdoptsLegacyThreadAfterNativeResume(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	var old Session
	func() {
		defer first.Close()
		old, err = first.CreateSession(first.Workspaces()[0].ID, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = first.Start(old.ID, Input{Text: "first"}); err != nil {
			t.Fatal(err)
		}
		old = waitState(t, first, old.ID, "completed")
		// Reproduce a 0.71 session with a real thread but no binding object.
		prior, e := first.currentParticipantLocked(old.ID)
		if e != nil {
			t.Fatal(e)
		}
		for _, key := range [][2]string{{nativeBindingKind, prior.NativeOwnerID}, {activeParticipationKind, old.ID}, {participationKind, prior.ID}} {
			if err = first.Store.Delete(key[0], key[1]); err != nil {
				t.Fatal(err)
			}
		}
	}()

	m, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var b nativeSessionBinding
	if err := m.Store.Get(nativeBindingKind, participationScopeOf(old).nativeOwnerID(), &b); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("restart invented a native confirmation: %v", err)
	}
	if _, err := m.Start(old.ID, Input{Text: "continue"}); err != nil {
		t.Fatal(err)
	}
	next := waitState(t, m, old.ID, "completed")
	if err := m.Store.Get(nativeBindingKind, participationScopeOf(old).nativeOwnerID(), &b); err != nil {
		t.Fatal(err)
	}
	if next.ID != old.ID || next.RunID == old.RunID || b.NativeSessionID != old.ThreadID ||
		b.NativeSessionID != next.ThreadID || b.OwnerID != participationScopeOf(old).nativeOwnerID() {
		t.Fatalf("logical/native continuity lost: old=%+v next=%+v binding=%+v", old, next, b)
	}
	if err := m.DeleteSession(old.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.Get(nativeBindingKind, participationScopeOf(old).nativeOwnerID(), &b); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted session left its native thread binding", err)
	}
}

func TestParticipationChecksOwnerBeforeResumeAndAcknowledgement(t *testing.T) {
	m := testManager(t)
	s, err := m.CreateSession(m.Workspaces()[0].ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	host := runtimeSessionHost{m}
	if err = m.update(s.ID, func(v *Session) { v.RunID, v.Status = "run-a", "starting" }); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Session(s.ID)
	if _, err = m.sessionParticipation(s); err != nil {
		t.Fatal(err)
	}
	owner, err := host.NativeBinding(s, "/native/a")
	if err != nil {
		t.Fatal(err)
	}
	if err := host.ConfirmNativeBinding(s, owner, "thread-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := host.NativeBinding(s, "/native/b"); err == nil {
		t.Fatal("resumed in a different native state directory")
	}
	for _, change := range []func(*Session){
		func(v *Session) { v.InstanceID = "other" },
		func(v *Session) { v.RuntimeKind = "kun" },
		func(v *Session) { v.WorkspaceID = "other" },
		func(v *Session) { v.ExecutionMode, v.EnvironmentID = "docker", "another-environment" },
	} {
		other := s
		change(&other)
		if participationScopeOf(other).nativeOwnerID() == owner.OwnerID {
			t.Fatal("owner collision")
		}
		if err := m.update(s.ID, func(v *Session) { *v = other }); err != nil {
			t.Fatal(err)
		}
		if _, err := host.NativeBinding(other, "/native/a"); err == nil {
			t.Fatal("foreign owner received old thread")
		}
		if err := host.ConfirmNativeBinding(other, owner, "thread-b"); err == nil {
			t.Fatal("stale native confirmation overwrote foreign owner")
		}
	}
	if err := m.update(s.ID, func(v *Session) { *v = s; v.RunID = "run-b" }); err != nil {
		t.Fatal(err)
	}
	if err := host.ConfirmNativeBinding(s, owner, "thread-late"); err == nil {
		t.Fatal("delayed callback confirmed a different run")
	}
	var saved nativeSessionBinding
	if err := m.Store.Get(nativeBindingKind, owner.OwnerID, &saved); err != nil || saved.NativeSessionID != "thread-a" {
		t.Fatal("rejected writes changed binding", saved, err)
	}
}

func TestParticipationKunWorkerConfirmsBindingWithCursor(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	i, err := m.CreateInstance(InstancePatch{Name: "Maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	w := m.Workspaces()[0]
	if _, err := m.CreateSession(w.ID, "chat", "", i.ID); err == nil {
		t.Fatal("Kun appeared in daily admission")
	}
	s, err := createInternalSessionFixture(m, w.ID, "", "", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.update(s.ID, func(v *Session) { v.RunID, v.Status = "one", "starting" }); err != nil {
		t.Fatal(err)
	}
	journal := kunJournalHost{m}
	ev := p.Event{SessionID: s.ID, RunID: "one", Type: "kun/run.started", Time: store.Now(), Sequence: 2}
	var b nativeSessionBinding
	if _, err = journal.Import(s.ID, ev); err == nil {
		t.Fatal("journal gap accepted")
	}
	if err := m.Store.Get(nativeBindingKind, participationScopeOf(s).nativeOwnerID(), &b); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed worker import persisted a binding", err)
	}
	ev.Sequence = 1
	if _, err = journal.Import(s.ID, ev); err != nil {
		t.Fatal(err)
	}
	if err = m.Store.Get(nativeBindingKind, participationScopeOf(s).nativeOwnerID(), &b); err != nil || b.NativeSessionID != s.ID || b.OwnerID != participationScopeOf(s).nativeOwnerID() || b.Scope.Engine != "kun" {
		t.Fatal("worker acknowledgement lost", b, err)
	}
}

// Exercise the storage/executor cutover internally with the available Codex
// demo adapter. There is intentionally no public operation changing owners.
func TestParticipationHandoffReturnsToOriginalNativeBinding(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "same conversation", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.CreateInstance(InstancePatch{Name: "second environment"})
	if err != nil {
		t.Fatal(err)
	}
	var spans []sessionParticipant
	var completed []Session
	for _, iid := range []string{s.InstanceID, other.ID, s.InstanceID} {
		// Leave the legacy thread projection in place while changing the
		// selected owner. It must never leak into the new executor.
		if err := m.update(s.ID, func(v *Session) { v.InstanceID = iid }); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Start(s.ID, Input{Text: "continue the same conversation"}); err != nil {
			t.Fatal(err)
		}
		done := waitState(t, m, s.ID, "completed")
		m.mu.Lock()
		span, err := m.currentParticipantLocked(s.ID)
		m.mu.Unlock()
		if err != nil || done.ID != s.ID || span.Scope.InstanceID != iid || span.Ordinal != len(spans)+1 {
			t.Fatal(span, done, err)
		}
		spans = append(spans, span)
		completed = append(completed, done)
	}
	if spans[0].ID == spans[2].ID || spans[1].PreviousID != spans[0].ID || spans[2].PreviousID != spans[1].ID ||
		spans[0].NativeOwnerID != spans[2].NativeOwnerID || spans[0].NativeOwnerID == spans[1].NativeOwnerID {
		t.Fatal("participation lineage lost", spans)
	}
	if completed[0].ThreadID == completed[1].ThreadID || completed[0].ThreadID != completed[2].ThreadID {
		t.Fatal("native continuation crossed executors", completed)
	}
	first, err := m.participationForRun(spans[0].ID, spans[0].Scope)
	if err != nil || first.Ordinal != 1 || first.PreviousID != "" {
		t.Fatal("historical lookup followed the current pointer", first, err)
	}
	if _, err := m.participationForRun(spans[1].ID, spans[0].Scope); err == nil {
		t.Fatal("historical lookup crossed executor scopes")
	}
	bindings, err := m.Store.List(nativeBindingKind)
	if err != nil || len(bindings) != 2 {
		t.Fatal("expected two persistent native bindings", len(bindings), err)
	}
	if err = m.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{participationKind, nativeBindingKind, activeParticipationKind} {
		rows, err := m.Store.List(kind)
		if err != nil || len(rows) != 0 {
			t.Fatal("session deletion left executor state", kind, err)
		}
	}
}
