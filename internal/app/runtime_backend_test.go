package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestRuntimeAdmissionKeepsNativeCapabilitiesSeparate(t *testing.T) {
	for _, test := range []struct {
		kind      string
		admission RuntimeAdmission
		code      string
	}{
		{"kun", RuntimeAdmission{Input: Input{Text: "image", Files: []string{"uploads/a.PNG"}}}, "kun_text_only"},
		{"kun", RuntimeAdmission{Input: Input{Text: "recover"}, Recovery: &RecoveryPlan{}}, "kun_recovery_unsupported"},
		{"kun", RuntimeAdmission{Session: Session{TraceOrigin: &TraceOrigin{}}, Input: Input{Text: "diagnose", Skills: []Skill{{Name: "extra"}}}}, "diagnostic_input_only"},
		{"codex", RuntimeAdmission{Input: Input{Text: "resume", KunResume: &kunResumeInput{}}}, "not_kun"},
		{"codex", RuntimeAdmission{Input: Input{Text: "fast", FastSkillID: "kun-skill"}}, "skill_ordinary_application_only"},
	} {
		t.Run(test.kind+"/"+test.code, func(t *testing.T) {
			backend, err := newRuntimeBackend(test.kind, runtimeServices{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = backend.Prepare(test.admission)
			var api *apiError
			if !errors.As(err, &api) || api.Code != test.code {
				t.Fatalf("got %v, want %s", err, test.code)
			}
		})
	}
}

// A stop captured during an earlier starting run must not close a later run's
// connection, even if the stop dispatch is delayed until after replacement.
func TestRuntimeStaleCodexStopDoesNotCloseReplacement(t *testing.T) {
	m := testManager(t)
	s, err := m.CreateSession(m.Workspaces()[0].ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(s.ID, Input{Text: "审批 first"}); err != nil {
		t.Fatal(err)
	}
	first := waitState(t, m, s.ID, "waiting")
	stale := first
	stale.Status, stale.TurnID = "starting", ""
	if err := m.StopRun(s.ID, first.RunID); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "interrupted")
	if _, err := m.Start(s.ID, Input{Text: "审批 second"}); err != nil {
		t.Fatal(err)
	}
	second := waitState(t, m, s.ID, "waiting")
	h, _ := m.getHandle(s.ID)
	h.mu.Lock()
	client := h.client
	h.mu.Unlock()
	m.codexRuntime().Stop(RuntimeStop{Session: stale, Handle: h})
	if client.Closing() {
		t.Fatal("stale starting-run stop closed the replacement transport")
	}
	current, err := m.Session(s.ID)
	if err != nil || current.RunID != second.RunID || current.Status != "waiting" {
		t.Fatal(current, err)
	}
	if err := m.StopRun(s.ID, second.RunID); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "interrupted")
}

func TestRuntimeKunJournalKeepsCursorAndProjectionAtomic(t *testing.T) {
	m := testManager(t)
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	if err := m.update(s.ID, func(s *Session) { s.RunID = "current"; s.Status = "starting" }); err != nil {
		t.Fatal(err)
	}
	journal := kunJournalHost{m}
	event := p.Event{SessionID: s.ID, RunID: "current", Sequence: 1, Time: store.Now(), Type: "kun/run.started", Data: p.JSON(map[string]any{})}
	if terminal, err := journal.Import(s.ID, event); err != nil || terminal {
		t.Fatal(terminal, err)
	}
	event.Sequence, event.Type, event.Data = 3, "kun/run.finished", p.JSON(map[string]string{"status": "completed"})
	if _, err := journal.Import(s.ID, event); err == nil {
		t.Fatal("gap accepted")
	}
	current, _ := m.Session(s.ID)
	cursor, _ := journal.Cursor(s.ID)
	if current.Status != "running" || cursor != 1 {
		t.Fatal("failed import changed projection", current, cursor)
	}
	event.Sequence, event.RunID = 2, "old"
	if terminal, err := journal.Import(s.ID, event); err != nil || terminal {
		t.Fatal(terminal, err)
	}
	current, _ = m.Session(s.ID)
	if current.Status != "running" {
		t.Fatal("old run event completed current run", current)
	}
	event.Sequence, event.RunID = 3, "current"
	if terminal, err := journal.Import(s.ID, event); err != nil || !terminal {
		t.Fatal(terminal, err)
	}
	current, _ = m.Session(s.ID)
	var saved Session
	if err := m.Store.Get("session", s.ID, &saved); err != nil {
		t.Fatal(err)
	}
	cursor, _ = journal.Cursor(s.ID)
	if current.Status != "completed" || saved.Status != current.Status || cursor != 3 {
		t.Fatal("projection not committed together", current, saved, cursor)
	}
}

type remappedRuntimeResources struct {
	original RuntimeResourceService
	seen     map[resource.Role]bool
}

func (s remappedRuntimeResources) Resolve(session Session, role resource.Role) (resource.Resolved, error) {
	s.seen[role] = true
	resolved, err := s.original.Resolve(session, role)
	if err == nil && role == resource.RoleOutputs {
		resolved.RelativePath = filepath.Join("outputs", "bound-destination")
		workspace, e := s.original.Resolve(session, resource.RoleWorkspace)
		if e != nil {
			return resolved, e
		}
		resolved.Path = filepath.Join(workspace.Path, resolved.RelativePath)
	}
	return resolved, err
}

func TestRuntimeKunRequestConsumesResourceAndPolicyServices(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	i, _ := m.Instance()
	config := p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "fixture", AllowWrite: true}
	if _, err := m.SetAgentRuntime(i.ID, i.Revision, config); err != nil {
		t.Fatal(err)
	}
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	backend := m.kunRuntime()
	seen := map[resource.Role]bool{}
	backend.resources = remappedRuntimeResources{original: m.ResourceService(), seen: seen}
	request, err := backend.startRequest(s, w, Input{Text: "do the work"})
	if err != nil {
		t.Fatal(err)
	}
	if !seen[resource.RoleWorkspace] || !seen[resource.RoleOutputs] || request.Workspace != w.Path || !strings.Contains(request.Input, "outputs/bound-destination") {
		t.Fatal("request ignored its resource bindings", request, seen)
	}
	i, _ = m.Instance()
	readOnly := Permissions{Sandbox: "read-only", ApprovalPolicy: "never"}
	if _, err = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Revision: i.Revision, Permissions: &readOnly}); err != nil {
		t.Fatal(err)
	}
	request, err = backend.startRequest(s, w, Input{Text: "read only"})
	if err != nil || request.Config.AllowWrite || request.ApprovalPolicy != "never" {
		t.Fatal("request ignored policy", request.Config, request.ApprovalPolicy, err)
	}
}
