package app

import (
	"context"
	"encoding/json"
	"fmt"

	kc "github.com/your-org/kun/sdk/client"
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

// This is the composition boundary. Only host-side adapters know Manager;
// backends receive purpose-specific service interfaces and cannot reach its
// maps, locks, queue, Store, or credential management.
func (m *Manager) runtimeServices() runtimeServices {
	return runtimeServices{
		resources: m.ResourceService(), state: runtimeSessionHost{m}, policy: runtimePolicyHost{m},
		instances: runtimeInstanceHost{m}, codex: codexRuntimeHost{m},
		kunPrepare: kunPreparationHost{m}, kunProcess: kunProcessHost{m},
		kunJournal: kunJournalHost{m},
	}
}
func (m *Manager) runtimeBackend(kind string) (RuntimeBackend, error) {
	return newRuntimeBackend(kind, m.runtimeServices())
}
func (m *Manager) codexRuntime() codexBackend {
	s := m.runtimeServices()
	return codexBackend{resources: s.resources, state: s.state, policy: s.policy, transport: s.codex}
}
func (m *Manager) kunRuntime() kunBackend {
	s := m.runtimeServices()
	return kunBackend{resources: s.resources, state: s.state, policy: s.policy, instances: s.instances, preparation: s.kunPrepare, process: s.kunProcess, journal: s.kunJournal}
}

// Native diagnostic/configuration entry points use the same backend modules.
func (m *Manager) kunStartRequest(s Session, w Workspace, in Input) (p.Start, error) {
	return m.kunRuntime().startRequest(s, w, in)
}
func (m *Manager) ensureKunWorker(s Session, h *handle) (*kc.Client, error) {
	return m.kunRuntime().ensureWorker(s, h)
}
func (m *Manager) interrupt(id string, h *handle, expectedRunIDs ...string) {
	m.codexRuntime().interrupt(id, h, expectedRunIDs...)
}

type runtimeSessionHost struct{ m *Manager }

func (s runtimeSessionHost) Context() context.Context           { return s.m.ctx }
func (s runtimeSessionHost) Session(id string) (Session, error) { return s.m.Session(id) }
func (s runtimeSessionHost) Update(id string, change func(*Session)) error {
	return s.m.update(id, change)
}
func (s runtimeSessionHost) Finish(id, status, message string) { s.m.finish(id, status, message) }
func (s runtimeSessionHost) Event(id, direction, method string, value any) error {
	return s.m.event(id, direction, method, value)
}
func (s runtimeSessionHost) Go(work func()) {
	s.m.wg.Add(1)
	go func() { defer s.m.wg.Done(); work() }()
}

type runtimePolicyHost struct{ m *Manager }

func (s runtimePolicyHost) Permissions(session Session) (Permissions, error) {
	policy, err := s.m.PolicyService().ForSession(session)
	return policy.Permissions, err
}

type runtimeInstanceHost struct{ m *Manager }

func (s runtimeInstanceHost) Instance(ids ...string) (Instance, error) { return s.m.Instance(ids...) }

type codexRuntimeHost struct{ m *Manager }

func (s codexRuntimeHost) Connect(id string, w Workspace, h *handle, ids ...string) (*rpc.Client, error) {
	return s.m.connect(id, w, h, ids...)
}
func (s codexRuntimeHost) ValidateRecovery(session Session, h *handle, recovery *RecoveryPlan) error {
	return s.m.validateRecovery(session, h, recovery)
}
func (s codexRuntimeHost) CheckBuildAssistantExecution(session Session) error {
	return s.m.checkBuildAssistantExecution(session)
}
func (s codexRuntimeHost) ConfigureBuildAssistant(session Session, w Workspace, params map[string]any) error {
	return s.m.configureBuildAssistant(session, w, params)
}
func (s codexRuntimeHost) ConfigureHTTPObservation(ctx context.Context, client *rpc.Client, session Session, w Workspace, params map[string]any) bool {
	return s.m.configureHTTPObservation(ctx, client, session, w, params)
}
func (s codexRuntimeHost) ConfigureTraceAnalysis(session Session, params map[string]any) error {
	return s.m.configureTraceAnalysis(session, params)
}
func (s codexRuntimeHost) ObservationDisconnected(id string) { s.m.observationDisconnected(id) }
func (s codexRuntimeHost) ObservationApplied(id string, enabled bool) {
	s.m.observationApplied(id, enabled)
}
func (s codexRuntimeHost) RecordEffective(id string, raw json.RawMessage, permissions Permissions) {
	s.m.recordEffective(id, raw, permissions)
}
func (s codexRuntimeHost) ExpireApprovals(id string, h *handle, runs ...string) {
	s.m.expire(id, h, runs...)
}

type kunPreparationHost struct{ m *Manager }

func (s kunPreparationHost) CaseStartRequest(session Session, w Workspace, in Input) (p.Start, error) {
	return s.m.kunCaseStartRequest(session, w, in)
}
func (s kunPreparationHost) DiagnosticRequest(session Session, w Workspace, in Input, config p.Config, key string) (p.Start, error) {
	return s.m.kunDiagnosticRequest(session, w, in, config, key)
}
func (s kunPreparationHost) ForkStartRequest(session Session, w Workspace, in Input, config p.Config, key string) (p.Start, error) {
	return s.m.kunForkStartRequest(session, w, in, config, key)
}
func (s kunPreparationHost) RuntimeMCP(id string) ([]p.MCPServer, error) {
	return s.m.kunRuntimeMCP(id)
}
func (s kunPreparationHost) InputSkills(w Workspace, i Instance, skills []Skill) ([]p.Skill, error) {
	return s.m.kunInputSkills(w, i, skills)
}
func (s kunPreparationHost) AttachSkill(session Session, w Workspace, in Input, request p.Start) (p.Start, error) {
	request, err := s.m.attachKunSkill(session, w, in, request)
	if err != nil {
		return request, err
	}
	request, err = s.m.configureSetupAgent(session, w, request)
	if err != nil {
		return request, err
	}
	return s.m.configureKunBuildAssistant(session, w, request)
}
func (s kunPreparationHost) ValidateResume(session Session, w Workspace, h *handle, in Input) ([]Skill, error) {
	return s.m.validateKunResume(session, w, h, in)
}

type kunProcessHost struct{ m *Manager }

func (s kunProcessHost) Executable() string { return s.m.kunExecutable() }

func (s kunProcessHost) Count(delta int32) { s.m.loaded.Add(delta) }

type kunJournalHost struct{ m *Manager }

func (s kunJournalHost) Cursor(id string) (int64, error) { return s.m.Store.KunCursor(id) }
func (s kunJournalHost) RecordRuntime(id string, status RuntimeStatus) error {
	return s.m.Store.Put("runtime", id, status)
}
func (s kunJournalHost) Import(id string, ev p.Event) (bool, error) {
	if ev.SessionID != id {
		return false, fmt.Errorf("Kun session mismatch")
	}
	var data struct {
		Status  string    `json:"status"`
		Error   string    `json:"error"`
		Command p.Control `json:"command"`
	}
	_ = json.Unmarshal(ev.Data, &data)
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	current := s.m.sessions[id]
	if current == nil {
		return false, fmt.Errorf("session removed")
	}
	next := *current
	records := []store.Record{}
	finished := false
	if ev.RunID == next.RunID {
		switch ev.Type {
		case "kun/run.started":
			if next.Status != "stopping" {
				next.Status = "running"
			}
			next.TurnID = ev.RunID
		case "kun/run.paused", "kun/approval.requested":
			if next.Status != "stopping" {
				next.Status = "waiting"
			}
		case "kun/control.applied":
			if data.Command.Operation == "resume" || data.Command.Operation == "step" || data.Command.Operation == "approve" || data.Command.Operation == "reject" {
				next.Status = "running"
			}
		case "kun/run.finished":
			next.Status = data.Status
			next.Error = data.Error
			finished = true
		}
		next.Updated = store.Now()
		records = append(records, store.Record{Kind: "session", ID: next.ID, Value: next})
	}
	ok, err := s.m.Store.ImportKun(id, ev.Sequence, ev.Time, ev.Type, redact(ev), records...)
	if err == nil && ok && ev.RunID == next.RunID {
		*current = next
	}
	return finished, err
}

var (
	_ RuntimeSessionService  = runtimeSessionHost{}
	_ RuntimePolicyService   = runtimePolicyHost{}
	_ RuntimeInstanceService = runtimeInstanceHost{}
	_ CodexRuntimeService    = codexRuntimeHost{}
	_ KunPreparationService  = kunPreparationHost{}
	_ KunProcessService      = kunProcessHost{}
	_ KunJournalService      = kunJournalHost{}
)
