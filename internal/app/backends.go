package app

import "net/http"

// AgentBackend is the host lifecycle boundary. Optional debug and experiment
// operations retain their own contracts; a missing capability is never emulated.
// Execution environments and model providers are separate from backend identity.
type AgentBackend interface {
	Info() BackendInfo
	Run(*Manager, Session, Workspace, *handle, Input, *RecoveryPlan)
	Stop(*Manager, Session, *handle)
}

type BackendInfo struct {
	Kind           string   `json:"kind"`
	ExecutionModes []string `json:"executionModes"`
	Capabilities   []string `json:"capabilities"`
}

type codexBackend struct{}
type kunBackend struct{}

func (codexBackend) Info() BackendInfo {
	return BackendInfo{"codex", []string{"local", "docker"}, []string{"tasks", "sessions", "cancel", "events", "artifacts", "approvals", "trace_inspection"}}
}
func (kunBackend) Info() BackendInfo {
	return BackendInfo{"kun", []string{"local"}, []string{"tasks", "sessions", "cancel", "events", "artifacts", "approvals", "trace_inspection", "step", "breakpoints", "snapshots", "hybrid_replay", "live_fork", "evaluation", "validated_skills"}}
}
func (codexBackend) Run(m *Manager, s Session, w Workspace, h *handle, in Input, recovery *RecoveryPlan) {
	m.runCodex(s, w, h, in, recovery)
}
func (kunBackend) Run(m *Manager, s Session, w Workspace, h *handle, in Input, _ *RecoveryPlan) {
	m.runKun(s, w, h, in)
}
func (codexBackend) Stop(m *Manager, s Session, h *handle) { m.interrupt(s.ID, h, s.RunID) }
func (kunBackend) Stop(m *Manager, s Session, h *handle)   { m.stopKun(s, h) }

// Immutable registration. Unknown kinds fail closed; sessions never silently
// migrate to another backend when configuration or availability changes.
var agentBackends = map[string]AgentBackend{"codex": codexBackend{}, "kun": kunBackend{}}

func agentBackend(kind string) (AgentBackend, error) {
	b, ok := agentBackends[runtimeKind(kind)]
	if !ok {
		return nil, failure(400, "backend_unsupported", "Unsupported Agent backend: "+kind)
	}
	return b, nil
}
func backendCatalog() []BackendInfo {
	out := []BackendInfo{}
	for _, kind := range []string{"kun", "codex"} {
		out = append(out, agentBackends[kind].Info())
	}
	return out
}
func (s *Server) backendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backends", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"backends": backendCatalog(), "availability": "Use setup/environment for a configured instance; capabilities do not imply readiness."})
	})
}
