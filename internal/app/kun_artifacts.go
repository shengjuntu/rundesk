package app

import (
	"encoding/json"
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Scoring reads only the original bounded event snapshot, never today's files.
func scoreKunArtifacts(out *KunEvaluationSide, side KunCompareSide, events []store.Event, rules []p.ArtifactRule) *p.ArtifactEvidence {
	if len(rules) == 0 {
		return nil
	}
	var evidence *p.ArtifactEvidence
	var eventID int64
	if side.Complete {
		for _, host := range events {
			if host.Method != "kun/run.finished" {
				continue
			}
			var ev p.Event
			var data struct {
				Status    string              `json:"status"`
				Artifacts *p.ArtifactEvidence `json:"artifacts"`
			}
			if json.Unmarshal(host.Data, &ev) != nil || ev.Type != "kun/run.finished" || ev.SessionID != side.SessionID || ev.RunID != side.RunID || json.Unmarshal(ev.Data, &data) != nil {
				continue
			}
			evidence = nil
			eventID = 0
			if data.Status == "completed" && validArtifactEvidence(data.Artifacts, rules) {
				evidence = data.Artifacts
				eventID = host.ID
			}
		}
	}
	for i, r := range rules {
		result := KunRuleResult{Index: out.Total, Kind: r.Kind, Status: "unknown", Reason: "artifact_evidence_missing", EventID: eventID}
		if !side.Complete {
			result.Reason = "incomplete_records"
		} else if evidence != nil {
			result.Status = evidence.Results[i].Status
			result.Reason = evidence.Results[i].Reason
		}
		out.Results = append(out.Results, result)
		out.Total++
		switch result.Status {
		case "pass":
			out.Passed++
		case "fail":
			out.Failed++
		default:
			out.Unknown++
		}
	}
	out.Status = "unknown"
	out.Score = nil
	if out.Failed > 0 {
		out.Status = "fail"
	} else if out.Unknown == 0 {
		out.Status = "pass"
	}
	if out.Unknown == 0 {
		score := float64(out.Passed) * 100 / float64(out.Total)
		out.Score = &score
	}
	return evidence
}
func validArtifactEvidence(v *p.ArtifactEvidence, rules []p.ArtifactRule) bool {
	if v == nil || v.RulesHash != p.ArtifactRulesHash(rules) || len(v.Results) != len(rules) {
		return false
	}
	for i, r := range v.Results {
		if r.Index != i || r.Kind != rules[i].Kind || r.Path != rules[i].Path {
			return false
		}
		switch r.Status {
		case "pass":
			if r.Reason != "matched" {
				return false
			}
		case "fail":
			if r.Reason != "missing" && r.Reason != "not_regular" && r.Reason != "not_matched" {
				return false
			}
		case "unknown":
			if r.Reason != "unavailable" && r.Reason != "workspace_unavailable" && r.Reason != "too_large" && r.Reason != "changed_during_capture" && r.Reason != "invalid_utf8" {
				return false
			}
		default:
			return false
		}
		if r.Reason == "matched" || r.Reason == "not_matched" || r.Reason == "invalid_utf8" {
			if r.Bytes == nil || *r.Bytes < 0 || *r.Bytes > 1<<20 || p.ValidateArtifactRules([]p.ArtifactRule{{Path: r.Path, Kind: "file_sha256", Value: r.SHA256}}) != nil {
				return false
			}
		}
	}
	return true
}
