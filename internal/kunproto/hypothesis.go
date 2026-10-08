package kunproto

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxHypothesisOutputBytes = 64 << 10

// An overlay retains the authoritative recording unchanged. Exactly one output
// may be substituted; tool identity, order, arguments and error status cannot.
type ReplayHypothesis struct {
	ParentPreviewID    string `json:"parentPreviewId"`
	ParentHash         string `json:"parentHash"`
	ParentBundleHash   string `json:"parentBundleHash"`
	Position           int    `json:"position"`
	SourceSequence     int64  `json:"sourceSequence"`
	RecordHash         string `json:"recordHash"`
	OriginalOutputHash string `json:"originalOutputHash"`
	OutputHash         string `json:"outputHash"`
	Output             string `json:"output"`
	Reason             string `json:"reason"`
}

func TextHash(s string) string                 { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func ReplayRecordHash(r ReplayRecord) string   { return TextHash(string(JSON(r))) }
func HypothesisHash(h ReplayHypothesis) string { return TextHash(string(JSON(h))) }

// Both host and worker validate the overlay before any execution is admitted.
func ValidateHypothesis(b ForkBundle) error {
	h := b.Hypothesis
	if h == nil {
		return nil
	}
	if b.Mode != "hybrid" || h.Position < 0 || h.Position >= len(b.Records) || len(h.ParentPreviewID) == 0 || len(h.ParentPreviewID) > 128 || len(h.ParentHash) != 64 {
		return fmt.Errorf("invalid hypothesis parent or position")
	}
	if !utf8.ValidString(h.Output) || !utf8.ValidString(h.Reason) || len(h.Output) > MaxHypothesisOutputBytes || strings.TrimSpace(h.Reason) == "" || utf8.RuneCountInString(h.Reason) > 1000 {
		return fmt.Errorf("hypothesis requires UTF-8 output up to 64 KiB and a reason of 1–1000 characters")
	}
	r := b.Records[h.Position]
	b.Hypothesis = nil
	if h.ParentBundleHash != ForkHash(b) || h.SourceSequence != r.Sequence || h.RecordHash != ReplayRecordHash(r) || h.OriginalOutputHash != TextHash(r.Output) || h.OutputHash != TextHash(h.Output) || h.Output == r.Output {
		return fmt.Errorf("hypothesis recording or output fingerprint mismatch")
	}
	return nil
}
