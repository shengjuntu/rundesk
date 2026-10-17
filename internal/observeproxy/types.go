// Package observeproxy records a bounded copy of HTTP application bodies while
// forwarding the original request and response without API translation.
package observeproxy

import "time"

type Labels struct {
	CaptureID string `json:"captureId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	RunID     string `json:"runId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	TurnID    string `json:"turnId,omitempty"`
}

// Chunk describes an observer read/write segment, not an HTTP or SSE frame.
type Chunk struct {
	Offset        int64 `json:"offset"`
	Size          int   `json:"size"`
	ElapsedMicros int64 `json:"elapsedMicros"`
}

type Body struct {
	Bytes                  []byte  `json:"bytes,omitempty"`
	Size                   int64   `json:"size"`
	Complete               bool    `json:"complete"`
	Truncated              bool    `json:"truncated"`
	Chunks                 []Chunk `json:"chunks,omitempty"`
	ChunkMetadataTruncated bool    `json:"chunkMetadataTruncated,omitempty"`
	Error                  string  `json:"error,omitempty"`
}

type Message struct {
	Method          string              `json:"method,omitempty"`
	URL             string              `json:"url,omitempty"`
	Host            string              `json:"host,omitempty"`
	Status          int                 `json:"status,omitempty"`
	Headers         map[string][]string `json:"headers,omitempty"`
	Trailers        map[string][]string `json:"trailers,omitempty"`
	RedactedHeaders []string            `json:"redactedHeaders,omitempty"`
	Body            Body                `json:"body"`
}

// Duplex holds the exact WebSocket frame bytes in each direction. Client frames
// retain their original masking. Frame/message decoding is a read-side concern.
type Duplex struct {
	Encoding   string `json:"encoding"`
	ToUpstream Body   `json:"toUpstream"`
	ToClient   Body   `json:"toClient"`
}

type Record struct {
	ID                    string  `json:"id"`
	Labels                Labels  `json:"labels"`
	Association           string  `json:"association"`
	ConnectionStartLabels *Labels `json:"connectionStartLabels,omitempty"`
	Started               string  `json:"started"`
	Ended                 string  `json:"ended"`
	DurationMS            int64   `json:"durationMs"`
	Request               Message `json:"request"`
	Response              Message `json:"response"`
	WebSocket             *Duplex `json:"websocket,omitempty"`
	Error                 string  `json:"error,omitempty"`
	Incomplete            bool    `json:"incomplete"`
	CaptureLoss           string  `json:"captureLoss,omitempty"`
}

type Config struct {
	// ListenAddr defaults to a new loopback port. Register returns a capability
	// URL, which must be kept private. No arbitrary forward-proxy endpoint exists.
	ListenAddr string
	Record     func(Record) error
	// MaxBodyBytes bounds each direction. QueueBytes bounds the combined retained
	// body bytes in active captures and the recorder queue, not just queued data.
	MaxBodyBytes int64
	QueueBytes   int64
	QueueSize    int
	// DialTimeout defaults to 30 seconds. Streams have no duration limit.
	DialTimeout time.Duration
}

type Stats struct {
	ActiveRequests int64  `json:"activeRequests"`
	PendingRecords int64  `json:"pendingRecords"`
	RetainedBytes  int64  `json:"retainedBytes"`
	DroppedRecords uint64 `json:"droppedRecords"`
	RecordErrors   uint64 `json:"recordErrors"`
	LastError      string `json:"lastError,omitempty"`
}
