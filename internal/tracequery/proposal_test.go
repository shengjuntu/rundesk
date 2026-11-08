package tracequery

import (
	"context"
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/store"
	"path/filepath"
	"testing"
)

func TestTraceProposalReferencesAndArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Put("session", "s", map[string]string{"id": "s"})
	s.Put("session", "other", map[string]string{"id": "other"})
	first, _ := s.Add("s", "internal", "run/input", map[string]any{"runId": "r", "input": map[string]string{"text": "source"}})
	last, _ := s.Add("s", "in", "warning", map[string]any{"runId": "r", "message": "failed lookup"})
	wrong, _ := s.Add("s", "internal", "run/input", map[string]any{"runId": "other-round", "input": map[string]string{"text": "other"}})
	foreign, _ := s.Add("other", "in", "warning", map[string]string{"message": "foreign"})
	r, err := Open(path, "s", wrong.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	call := func(ids []int64) (any, error) {
		raw, _ := json.Marshal(map[string]any{"runId": "r", "proposal": ProposalInput{Kind: "steer", Reason: "recorded lookup failed", Text: "check the URL", EvidenceIDs: ids}})
		return r.CallJSONContext(context.Background(), "trace_propose", raw)
	}
	a, err := call([]int64{first.ID, last.ID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := call([]int64{first.ID, last.ID})
	if err != nil || a.(Proposal).ID != b.(Proposal).ID || a.(Proposal).Status != "suggestion_only" {
		t.Fatal(a, b, err)
	}
	for _, ids := range [][]int64{{foreign.ID}, {wrong.ID}, {last.ID, last.ID}, {9999}, {}} {
		if _, err := call(ids); err == nil {
			t.Fatalf("invalid evidence accepted: %v", ids)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"trace_propose", `{"runId":"r","proposal":{"kind":"apply","reason":"a","text":"b","evidenceIds":[1]}}`},
		{"trace_propose", `{"runId":"r","proposal":{"kind":"steer","reason":"a","text":"b","evidenceIds":[1],"execute":true}}`},
		{"trace_statistics", `{"runId":"r","proposal":{}}`}, {"trace_read_event", `{}`}, {"trace_get_step", `{"stepId":null}`}, {"trace_statistics", `{"sql":"select *"}`},
	} {
		if _, err := r.CallJSONContext(context.Background(), tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Fatal(tc)
		}
	}
	page, _ := s.DebugEvents("s", 0, nil, 10)
	if page.Through != wrong.ID || len(page.Events) != 3 {
		t.Fatal("proposal mutated the source", page)
	}
}
