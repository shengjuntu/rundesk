package kunproto

import (
	"crypto/sha256"
	"fmt"
)

const ForkSchema = 3
const MaxForkBytes = 4 << 20
const MaxReplayRecords = 128

// A runtime fork uses a private, unredacted worker recording. It must never be
// reconstructed from the host's redacted debug events or K3-A text overlays.
type ForkSelection struct {
	SourceRunID      string `json:"sourceRunId"`
	Sequence         int64  `json:"sequence"`
	Through          int64  `json:"through"`
	ExpectedRevision int64  `json:"expectedStateRevision"`
	WorkerEpoch      string `json:"workerEpoch"`
}
type ForkPoint struct {
	Sequence int64  `json:"sequence"`
	Phase    string `json:"phase"`
	Step     int    `json:"step"`
	Pending  int    `json:"pending"`
}
type ForkPoints struct {
	Selection  ForkSelection `json:"selection"`
	Items      []ForkPoint   `json:"items"`
	NextOffset int           `json:"nextOffset"`
	HasMore    bool          `json:"hasMore"`
}
type ForkExport struct {
	Selection ForkSelection `json:"selection"`
	Mode      string        `json:"mode,omitempty"`
}
type ReplayRecord struct {
	Sequence      int64  `json:"sequence"`
	Tool          string `json:"tool"`
	SchemaHash    string `json:"schemaHash"`
	ArgumentsHash string `json:"argumentsHash"`
	IdentityHash  string `json:"identityHash"`
	Output        string `json:"output"`
	IsError       bool   `json:"isError"`
	Status        string `json:"status"`
}
type ForkBundle struct {
	Hypothesis      *ReplayHypothesis `json:"hypothesis,omitempty"`
	Mode            string            `json:"mode"`
	Schema          int               `json:"schema"`
	Selection       ForkSelection     `json:"selection"`
	State           State             `json:"state"`
	CatalogHash     string            `json:"catalogHash"`
	EnvironmentHash string            `json:"environmentHash"`
	Records         []ReplayRecord    `json:"records"`
	ContentHash     string            `json:"contentHash"`
}

func ForkHash(b ForkBundle) string {
	b.ContentHash = ""
	return fmt.Sprintf("%x", sha256.Sum256(JSON(b)))
}

type ForkOrigin struct {
	PreviewID  string `json:"previewId"`
	SessionID  string `json:"sessionId"`
	RunID      string `json:"runId"`
	Sequence   int64  `json:"sequence"`
	Through    int64  `json:"through"`
	BundleHash string `json:"bundleHash"`
	Mode       string `json:"mode"`
}
type ForkState struct {
	HypothesisHash  string      `json:"hypothesisHash,omitempty"`
	Origin          ForkOrigin  `json:"origin"`
	InheritedStep   int         `json:"inheritedStep"`
	InheritedBudget BudgetUsage `json:"inheritedBudget"`
	ReplayCursor    int         `json:"replayCursor"`
	ReplayTotal     int         `json:"replayTotal"`
}
type ForkStart struct {
	ConfirmLive bool       `json:"confirmLive,omitempty"`
	Origin      ForkOrigin `json:"origin"`
	Bundle      ForkBundle `json:"bundle"`
	Instruction string     `json:"instruction"`
}

func (f *ForkState) Hybrid() bool { return f != nil && f.Origin.Mode == "hybrid" }

type ReplayEvidence struct {
	HypothesisHash     string `json:"hypothesisHash,omitempty"`
	OriginalOutputHash string `json:"originalOutputHash,omitempty"`
	OutputHash         string `json:"outputHash,omitempty"`
	Mode               string `json:"mode"`
	SourceSequence     int64  `json:"sourceSequence"`
	Position           int    `json:"position"`
	BundleHash         string `json:"bundleHash"`
	RecordedStatus     string `json:"recordedStatus"`
	Executed           bool   `json:"executed"`
}
