package kunproto

import (
	"strings"
	"testing"
)

func hypothesisFixture() ForkBundle {
	b := ForkBundle{Mode: "hybrid", Schema: ForkSchema, Records: []ReplayRecord{{Sequence: 12, Tool: "read_file", Output: "original", Status: "failed", IsError: true}}}
	b.ContentHash = ForkHash(b)
	r := b.Records[0]
	b.Hypothesis = &ReplayHypothesis{ParentPreviewID: "parent", ParentHash: strings.Repeat("a", 64), ParentBundleHash: b.ContentHash, Position: 0, SourceSequence: r.Sequence, RecordHash: ReplayRecordHash(r), OriginalOutputHash: TextHash(r.Output), OutputHash: TextHash(""), Output: "", Reason: "test empty failed result"}
	b.ContentHash = ForkHash(b)
	return b
}
func TestHypothesisIntegrityAndBounds(t *testing.T) {
	b := hypothesisFixture()
	if err := ValidateHypothesis(b); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ForkBundle){
		"live":       func(b *ForkBundle) { b.Mode = "live" },
		"negative":   func(b *ForkBundle) { b.Hypothesis.Position = -1 },
		"absent":     func(b *ForkBundle) { b.Hypothesis.Position = 1 },
		"identity":   func(b *ForkBundle) { b.Hypothesis.SourceSequence++ },
		"record":     func(b *ForkBundle) { b.Records[0].Status = "succeeded" },
		"original":   func(b *ForkBundle) { b.Records[0].Output = "tampered" },
		"parent":     func(b *ForkBundle) { b.Hypothesis.ParentBundleHash = "wrong" },
		"outputHash": func(b *ForkBundle) { b.Hypothesis.Output = "different" },
		"noop": func(b *ForkBundle) {
			b.Hypothesis.Output = b.Records[0].Output
			b.Hypothesis.OutputHash = TextHash(b.Hypothesis.Output)
		},
		"bytes":       func(b *ForkBundle) { b.Hypothesis.Output = strings.Repeat("界", MaxHypothesisOutputBytes/3+1) },
		"invalidUTF8": func(b *ForkBundle) { b.Hypothesis.Output = string([]byte{255}) },
		"reason":      func(b *ForkBundle) { b.Hypothesis.Reason = " \n" },
		"longReason":  func(b *ForkBundle) { b.Hypothesis.Reason = strings.Repeat("界", 1001) },
	} {
		t.Run(name, func(t *testing.T) {
			b := hypothesisFixture()
			change(&b)
			b.ContentHash = ForkHash(b)
			if ValidateHypothesis(b) == nil {
				t.Fatal("accepted invalid overlay")
			}
		})
	}
	b.Hypothesis.Output = strings.Repeat("x", MaxHypothesisOutputBytes)
	b.Hypothesis.OutputHash = TextHash(b.Hypothesis.Output)
	if err := ValidateHypothesis(b); err != nil {
		t.Fatal("maximum output rejected", err)
	}
}
