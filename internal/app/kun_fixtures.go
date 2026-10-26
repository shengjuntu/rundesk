package app

import (
	"encoding/json"
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
	"strings"
)

func bindKunWorkspaceFixture(request *p.Start, fixture *p.WorkspaceFixture) {
	request.WorkspaceFixture = fixture
	if fixture != nil {
		request.Input = strings.TrimSuffix(request.Input, "\nSave deliverables under outputs/"+request.SessionID+"/.") + "\nSave deliverables inside this evaluation directory using workspace-relative paths."
	}
}

// Fixture setup is a required item in addition to reply/artifact quality. Its
// event binding prevents missing or other-run setup from looking like isolation.
func scoreKunWorkspaceFixture(out *KunEvaluationSide, side KunCompareSide, events []store.Event, preview KunCaseRunPreview) *p.WorkspaceFixtureState {
	fixture := preview.Case.WorkspaceFixture
	if fixture == nil {
		return nil
	}
	var proof *p.WorkspaceFixtureState
	var eventID int64
	if side.Complete {
		for _, host := range events {
			if host.Method != "kun/run.started" {
				continue
			}
			var ev p.Event
			var data struct {
				Fixture *p.WorkspaceFixtureState `json:"workspaceFixture"`
			}
			if json.Unmarshal(host.Data, &ev) != nil || ev.Type != "kun/run.started" || ev.SessionID != side.SessionID || ev.RunID != side.RunID || json.Unmarshal(ev.Data, &data) != nil {
				continue
			}
			total := 0
			for _, f := range fixture.Files {
				total += len(f.Content)
			}
			f := data.Fixture
			if f != nil && f.Mode == p.FixtureWorkspaceMode && f.Root == preview.ExecutionWorkspace && f.Hash == p.FixtureHash(fixture) && f.FileCount == len(fixture.Files) && f.Bytes == total {
				proof = f
				eventID = host.ID
			}
		}
	}
	result := KunRuleResult{Index: out.Total, Kind: "workspace_fixture_ready", Status: "unknown", Reason: "fixture_evidence_missing"}
	if !side.Complete {
		result.Reason = "incomplete_records"
	}
	if proof != nil {
		result.Status = "pass"
		result.Reason = "fresh_fixture_recorded"
		result.EventID = eventID
		out.Passed++
	} else {
		out.Unknown++
	}
	out.Results = append(out.Results, result)
	out.Total++
	out.Score = nil
	out.Status = "unknown"
	if out.Failed > 0 {
		out.Status = "fail"
	} else if out.Unknown == 0 {
		out.Status = "pass"
	}
	if out.Unknown == 0 {
		score := 100 * float64(out.Passed) / float64(out.Total)
		out.Score = &score
	}
	return proof
}
