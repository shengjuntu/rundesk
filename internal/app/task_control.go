package app

import "github.com/shengjuntu/rundesk/internal/store"

// TaskControl is the platform task lifecycle contract. It is shared by API
// facades and never exposes a native client, a worker process or Manager state.
// Authorization and idempotency remain the responsibility of the caller's API
// boundary; admission rechecks the persisted submitting credential.
type TaskControl interface {
	Enqueue(TaskSpec) (Task, error)
	Task(string) (Task, error)
	Tasks() []Task
	CancelTask(string) (Task, error)
}

// TaskSubmission describes new work, optionally continuing an existing native
// session. Every submission still receives a distinct platform Task.
type TaskSubmission struct {
	Spec          TaskSpec
	SessionID     string
	ExpectedRunID string
	// Deadline is an absolute Unix-nanosecond host deadline, including queue time.
	// Zero keeps the existing unbounded-by-task admission behavior.
	Deadline int64
	// Records binds facade metadata (for example a response or retry receipt)
	// atomically to the accepted task. It runs before persistence, under admission
	// locks, and must only construct records: it must not call Manager methods or
	// have side effects. An error aborts the admission. Core task/session records
	// cannot be replaced by this hook.
	Records func(Task, Session) ([]store.Record, error)
}

// TaskSubmissionControl adds transactional protocol binding and continuation
// without requiring queue readers or cancellation-only clients to implement it.
type TaskSubmissionControl interface {
	SubmitTask(TaskSubmission) (Task, error)
}

// QueueControl is an administrator capability, separate from submitting work.
type QueueControl interface {
	Queue() QueueSettings
	SaveQueue(QueueSettings) (QueueSettings, error)
}

// TaskResourceControl is a read-through inspection capability. Export creates
// a disposable view, never another writable task store or an artifact archive.
type TaskResourceControl interface {
	TaskResources(string) (TaskResourceView, error)
	ExportTaskResources(string) (TaskResourceExport, error)
}

var (
	_ TaskControl           = (*Manager)(nil)
	_ TaskSubmissionControl = (*Manager)(nil)
	_ QueueControl          = (*Manager)(nil)
	_ TaskResourceControl   = (*Manager)(nil)
)

func (s *Server) taskControl() TaskControl {
	if s.Tasks != nil {
		return s.Tasks
	}
	return s.Manager
}
