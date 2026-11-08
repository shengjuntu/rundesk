package app

import (
	"context"
	"encoding/json"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/rpc"
)

// RuntimeRun is one admitted execution. Session and Workspace are value
// snapshots; Handle only carries the native connection and admission locks.
// A backend owns its native protocol, never the platform's queue or database.
type RuntimeRun struct {
	Session   Session
	Workspace Workspace
	Handle    *handle
	Input     Input
	Recovery  *RecoveryPlan
}

type RuntimeAdmission struct {
	Session   Session
	Workspace Workspace
	Handle    *handle
	Input     Input
	Recovery  *RecoveryPlan
	Queued    bool
}

type RuntimeStop struct {
	Session Session
	Handle  *handle
}

// RuntimeSessionService is the common execution lifecycle. It deliberately has
// no task scheduling, arbitrary object storage, credential, or config methods.
type RuntimeSessionService interface {
	Context() context.Context
	Session(string) (Session, error)
	Update(string, func(*Session)) error
	Finish(id, status, message string)
	Event(id, direction, method string, value any) error
	Go(func())
}

// RuntimePolicyService resolves the effective permission policy for a run.
// Native policy encoding remains the responsibility of each backend.
type RuntimePolicyService interface {
	Permissions(Session) (Permissions, error)
}

type RuntimeInstanceService interface {
	Instance(...string) (Instance, error)
}

// CodexRuntimeService owns host integrations around the native transport.
// Thread/turn orchestration lives in codexBackend, not in this service.
type CodexRuntimeService interface {
	Connect(string, Workspace, *handle, ...string) (*rpc.Client, error)
	ValidateRecovery(Session, *handle, *RecoveryPlan) error
	CheckBuildAssistantExecution(Session) error
	ConfigureBuildAssistant(Session, Workspace, map[string]any) error
	ConfigureHTTPObservation(context.Context, *rpc.Client, Session, Workspace, map[string]any) bool
	ConfigureTraceAnalysis(Session, map[string]any) error
	ObservationDisconnected(string)
	ObservationApplied(string, bool)
	RecordEffective(string, json.RawMessage, Permissions)
	ExpireApprovals(string, *handle, ...string)
}

// KunPreparationService exposes explicit optional Kun extensions. These are
// not promoted to the lowest common denominator of all harnesses.
type KunPreparationService interface {
	CaseStartRequest(Session, Workspace, Input) (p.Start, error)
	DiagnosticRequest(Session, Workspace, Input, p.Config, string) (p.Start, error)
	ForkStartRequest(Session, Workspace, Input, p.Config, string) (p.Start, error)
	RuntimeMCP(string) ([]p.MCPServer, error)
	InputSkills(Workspace, Instance, []Skill) ([]p.Skill, error)
	AttachSkill(Session, Workspace, Input, p.Start) (p.Start, error)
	ValidateResume(Session, Workspace, *handle, Input) ([]Skill, error)
}

type KunProcessService interface {
	Executable() string
	Count(int32)
}

// KunJournalService makes event insertion, cursor advancement, and session
// projection one host transaction. A backend cannot update those separately.
type KunJournalService interface {
	Cursor(string) (int64, error)
	Import(string, p.Event) (terminal bool, err error)
	RecordRuntime(string, RuntimeStatus) error
}

type runtimeServices struct {
	resources  RuntimeResourceService
	state      RuntimeSessionService
	policy     RuntimePolicyService
	instances  RuntimeInstanceService
	codex      CodexRuntimeService
	kunPrepare KunPreparationService
	kunProcess KunProcessService
	kunJournal KunJournalService
}

// Compile-time contract checks avoid silently widening backend dependencies.
var (
	_ RuntimeBackend = codexBackend{}
	_ RuntimeBackend = kunBackend{}
)
