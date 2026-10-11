package app

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
	"strings"
	"testing"
)

func TestKunArtifactEvidenceScopeUnknownAndFailure(t *testing.T) {
	rules := []p.ArtifactRule{{Path: "out.txt", Kind: "file_equals", Value: "OK"}}
	size := int64(2)
	evidence := p.ArtifactEvidence{RulesHash: p.ArtifactRulesHash(rules), Results: []p.ArtifactResult{{Index: 0, Path: "out.txt", Kind: "file_equals", Status: "fail", Reason: "not_matched", Bytes: &size, SHA256: strings.Repeat("a", 64)}}}
	side := KunCompareSide{Complete: true, SessionID: "session", RunID: "run", Status: "completed"}
	score := func(s KunCompareSide, ev p.Event) (KunEvaluationSide, *p.ArtifactEvidence) {
		out := KunEvaluationSide{Status: "pass", Total: 2, Passed: 2}
		proof := scoreKunArtifacts(&out, s, []store.Event{{ID: 7, Method: "kun/run.finished", Data: p.JSON(ev)}}, rules)
		return out, proof
	}
	event := p.Event{Type: "kun/run.finished", SessionID: "session", RunID: "run", Data: p.JSON(map[string]any{"status": "completed", "artifacts": evidence})}
	out, proof := score(side, event)
	if out.Status != "fail" || out.Failed != 1 || out.Total != 3 || out.Unknown != 0 || proof == nil || out.Results[0].EventID != 7 {
		t.Fatal(out, proof)
	}
	event.RunID = "another"
	out, proof = score(side, event)
	if out.Status != "unknown" || out.Score != nil || out.Unknown != 1 || proof != nil {
		t.Fatal("wrong run accepted", out)
	}
	event.RunID = "run"
	side.Complete = false
	out, proof = score(side, event)
	if out.Unknown != 1 || proof != nil {
		t.Fatal("incomplete evidence graded")
	}
	side.Complete = true
	evidence.RulesHash = "changed"
	event.Data = p.JSON(map[string]any{"status": "completed", "artifacts": evidence})
	out, proof = score(side, event)
	if out.Unknown != 1 || proof != nil {
		t.Fatal("changed rules graded")
	}
	event.Data = p.JSON(map[string]any{"status": "completed"})
	out, proof = score(side, event)
	if out.Unknown != 1 || proof != nil {
		t.Fatal("legacy missing evidence passed")
	}
}
func TestKunArtifactTaskSetCopyAndLegacySerialization(t *testing.T) {
	m := testManager(t)
	in := taskSetFixture()
	in.Cases[0].ArtifactRules = []p.ArtifactRule{{Path: "result", Kind: "file_exists"}}
	saved, err := m.CreateKunTaskSet(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Cases[0].ArtifactRules[0].Path = "mutated"
	if saved.Hash != taskSetHash(saved) || saved.Cases[0].ArtifactRules[0].Path != "result" {
		t.Fatal("caller alias changed immutable set")
	}
	// Recreate exact pre-0.42 struct ordering; optional zero fields must not alter its JSON.
	type oldCase struct {
		Key   string         `json:"key"`
		Title string         `json:"title"`
		Input string         `json:"input"`
		Rules []KunReplyRule `json:"rules"`
	}
	old := oldCase{Key: "old", Title: "legacy", Input: "hello", Rules: []KunReplyRule{{Kind: "reply_equals", Value: "ok"}}}
	var current KunTaskCase
	if err = json.Unmarshal(p.JSON(old), &current); err != nil || string(p.JSON(old)) != string(p.JSON(current)) {
		t.Fatal("legacy task hashes changed", err)
	}
	legacyScore := KunCaseScore{PlanID: "legacy"}
	if strings.Contains(string(p.JSON(legacyScore)), "artifacts") {
		t.Fatal("legacy report hashes changed")
	}
}
