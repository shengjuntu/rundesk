// Package observation defines the versioned, read-only model shared by runtime
// events and model transport observations. Payloads remain in the event journal.
package observation

const SchemaVersion = 1

const (
	Host           = "host"
	CodexAppServer = "codex_app_server"
	KunSDK         = "kun_sdk"
	ObserveProxy   = "observeproxy"
)

// NativeIDs retain source identities without equating a worker sequence, RPC
// identifier, model step or transport request with a host event ID.
type NativeIDs struct {
	RPCID          string `json:"rpcId,omitempty"`
	ThreadID       string `json:"threadId,omitempty"`
	TurnID         string `json:"turnId,omitempty"`
	ItemID         string `json:"itemId,omitempty"`
	ItemType       string `json:"itemType,omitempty"`
	CallID         string `json:"callId,omitempty"`
	Step           int64  `json:"step,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	WorkerSequence int64  `json:"workerSequence,omitempty"`
	WorkerRevision int64  `json:"workerRevision,omitempty"`
	MCPServer      string `json:"mcpServer,omitempty"`
	MCPExchangeID  string `json:"mcpExchangeId,omitempty"`
	CaptureID      string `json:"captureId,omitempty"`
	TransportID    string `json:"transportId,omitempty"`
}

type Evidence struct {
	EventID        int64  `json:"eventId"`
	ReadWith       string `json:"readWith"`
	WorkerSequence int64  `json:"workerSequence,omitempty"`
}

// Usage describes semantics, not calculated billing. Raw counters are available
// through evidence. Cumulative thread totals must not be summed with call usage.
type Usage struct {
	Scope       string     `json:"scope"`
	Aggregation string     `json:"aggregation"`
	Evidence    []Evidence `json:"evidence"`
}

type Transport struct {
	Method          string `json:"method,omitempty"`
	StatusCode      int64  `json:"statusCode,omitempty"`
	WebSocket       bool   `json:"websocket"`
	RequestBytes    int64  `json:"requestBytes"`
	ResponseBytes   int64  `json:"responseBytes"`
	Incomplete      bool   `json:"incomplete"`
	Truncated       bool   `json:"truncated"`
	CaptureLoss     bool   `json:"captureLoss"`
	HeadersRedacted bool   `json:"headersRedacted"`
	Complete        bool   `json:"complete"`
}

// Event is a metadata-only envelope. Method preserves unrecognized events;
// sourceBasis describes how historical journal records identify their source.
type Event struct {
	ID               string     `json:"id"`
	EventID          int64      `json:"eventId"`
	SessionID        string     `json:"sessionId"`
	TaskID           string     `json:"taskId,omitempty"`
	RunID            string     `json:"runId,omitempty"`
	Source           string     `json:"source"`
	SourceBasis      string     `json:"sourceBasis"`
	Kind             string     `json:"kind"`
	Method           string     `json:"method"`
	Direction        string     `json:"direction"`
	Time             string     `json:"time"`
	Clock            string     `json:"clock"`
	Phase            string     `json:"phase"`
	Status           string     `json:"status"`
	Association      string     `json:"association"`
	AssociationBasis string     `json:"associationBasis"`
	Native           NativeIDs  `json:"native"`
	Evidence         []Evidence `json:"evidence"`
	Issues           []string   `json:"issues"`
	Usage            *Usage     `json:"usage,omitempty"`
	Transport        *Transport `json:"transport,omitempty"`
}

// Activity is either a projected run/step or a separate transport span. A run
// association is not a claim that a model step and HTTP attempt are the same call.
type Activity struct {
	ID                 string     `json:"id"`
	SessionID          string     `json:"sessionId"`
	TaskID             string     `json:"taskId,omitempty"`
	RunID              string     `json:"runId,omitempty"`
	Source             string     `json:"source"`
	SourceBasis        string     `json:"sourceBasis"`
	Kind               string     `json:"kind"`
	Type               string     `json:"type"`
	Title              string     `json:"title"`
	Status             string     `json:"status"`
	Start              string     `json:"start,omitempty"`
	End                string     `json:"end,omitempty"`
	Clock              string     `json:"clock"`
	DurationMS         *int64     `json:"durationMs,omitempty"`
	ReportedDurationMS *int64     `json:"reportedDurationMs,omitempty"`
	Point              bool       `json:"point"`
	StartObserved      bool       `json:"startObserved"`
	EndObserved        bool       `json:"endObserved"`
	Association        string     `json:"association"`
	AssociationBasis   string     `json:"associationBasis"`
	Native             NativeIDs  `json:"native"`
	Evidence           []Evidence `json:"evidence"`
	EvidenceTruncated  bool       `json:"evidenceTruncated"`
	Missing            []string   `json:"missing"`
	Issues             []string   `json:"issues"`
	Usage              *Usage     `json:"usage,omitempty"`
	Transport          *Transport `json:"transport,omitempty"`
}

type EventPage struct {
	SchemaVersion   int     `json:"schemaVersion"`
	SessionID       string  `json:"sessionId"`
	SnapshotThrough int64   `json:"snapshotThrough"`
	Observations    []Event `json:"observations"`
	NextAfter       int64   `json:"nextAfter"`
	HasMore         bool    `json:"hasMore"`
	Coverage        string  `json:"coverage"`
}

type ActivityPage struct {
	SchemaVersion   int        `json:"schemaVersion"`
	SessionID       string     `json:"sessionId"`
	SnapshotThrough int64      `json:"snapshotThrough"`
	Activity        []Activity `json:"activity"`
	Total           int        `json:"total"`
	Offset          int        `json:"offset"`
	NextOffset      int        `json:"nextOffset"`
	HasMore         bool       `json:"hasMore"`
	Coverage        string     `json:"coverage"`
}
