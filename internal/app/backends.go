package app

import "net/http"

// RuntimeBackend is the host lifecycle boundary. Each implementation owns its
// execution protocol; common services are injected at construction. Optional
// debug and experiment operations retain their own contracts, and a missing
// capability is never emulated. This is an internal contract, not a dynamic
// plugin SDK.
type RuntimeBackend interface {
	Info() BackendInfo
	Prepare(RuntimeAdmission) (Input, error)
	Run(RuntimeRun)
	// Stop initiates cancellation synchronously under the host admission lock,
	// then schedules native control without blocking the caller.
	Stop(RuntimeStop)
}

type BackendInfo struct {
	Kind           string   `json:"kind"`
	ExecutionModes []string `json:"executionModes"`
	Capabilities   []string `json:"capabilities"`
}

type codexBackend struct {
	resources RuntimeResourceService
	state     RuntimeSessionService
	policy    RuntimePolicyService
	transport CodexRuntimeService
}
type kunBackend struct {
	resources   RuntimeResourceService
	state       RuntimeSessionService
	policy      RuntimePolicyService
	instances   RuntimeInstanceService
	preparation KunPreparationService
	process     KunProcessService
	journal     KunJournalService
}

func (codexBackend) Info() BackendInfo {
	return BackendInfo{"codex", []string{"local", "docker"}, []string{"tasks", "sessions", "cancel", "events", "artifacts", "approvals", "trace_inspection"}}
}
func (kunBackend) Info() BackendInfo {
	return BackendInfo{"kun", []string{"local"}, []string{"tasks", "sessions", "cancel", "events", "artifacts", "approvals", "trace_inspection", "step", "breakpoints", "snapshots", "hybrid_replay", "live_fork", "evaluation", "validated_skills"}}
}

// Registrations contain factories, not process-global host references. Unknown
// kinds fail closed; a session never silently migrates to another backend.
var agentBackends = map[string]func(runtimeServices) RuntimeBackend{
	"codex": func(s runtimeServices) RuntimeBackend {
		return codexBackend{resources: s.resources, state: s.state, policy: s.policy, transport: s.codex}
	},
	"kun": func(s runtimeServices) RuntimeBackend {
		return kunBackend{resources: s.resources, state: s.state, policy: s.policy, instances: s.instances, preparation: s.kunPrepare, process: s.kunProcess, journal: s.kunJournal}
	},
}

func newRuntimeBackend(kind string, services runtimeServices) (RuntimeBackend, error) {
	factory, ok := agentBackends[runtimeKind(kind)]
	if !ok {
		return nil, failure(400, "backend_unsupported", "Unsupported Agent backend: "+kind)
	}
	return factory(services), nil
}

// Metadata callers do not need a running Manager or native connection.
func agentBackend(kind string) (RuntimeBackend, error) {
	return newRuntimeBackend(kind, runtimeServices{})
}
func backendCatalog() []BackendInfo {
	out := []BackendInfo{}
	for _, kind := range []string{"kun", "codex"} {
		backend, _ := agentBackend(kind)
		out = append(out, backend.Info())
	}
	return out
}
func (s *Server) backendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backends", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"backends": backendCatalog(), "availability": "Use setup/environment for a configured instance; capabilities do not imply readiness."})
	})
}
