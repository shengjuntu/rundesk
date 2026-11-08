package app

import (
	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
	"strings"
	"testing"
)

func TestKunFixturePlanAndEvidence(t *testing.T) {
	m := kunMCPManager(t)
	i, _ := m.Instance()
	w := m.Workspaces()[0]
	task := skillCase("isolated", "OK")
	task.WorkspaceFixture = &p.WorkspaceFixture{Files: []p.FixtureFile{{Path: "seed", Content: "FIXED"}}}
	set, err := m.CreateKunTaskSet(KunTaskSetInput{Title: "fixture", Cases: []KunTaskCase{task}})
	if err != nil {
		t.Fatal(err)
	}
	task.WorkspaceFixture.Files[0].Content = "CHANGED"
	if set.Hash != taskSetHash(set) {
		t.Fatal("caller alias retained")
	}
	view, err := m.CreateKunCaseRun(KunCaseRunInput{TaskSetID: set.ID, ExpectedTaskSetHash: set.Hash, CaseKey: "isolated", WorkspaceID: w.ID, InstanceID: i.ID})
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := p.FixtureRoot(w.Path, view.Preview.SessionID)
	if view.Preview.ExecutionWorkspace != expected || !strings.Contains(view.Preview.EffectiveInput, "inside this evaluation directory") || len(m.handles) != 0 {
		t.Fatal(view)
	}
	side := KunCompareSide{Complete: true, SessionID: view.Preview.SessionID, RunID: view.Preview.RunID}
	proof := p.WorkspaceFixtureState{Mode: p.FixtureWorkspaceMode, Root: expected, Hash: p.FixtureHash(set.Cases[0].WorkspaceFixture), FileCount: 1, Bytes: 5}
	ev := p.Event{Type: "kun/run.started", SessionID: side.SessionID, RunID: side.RunID, Data: p.JSON(map[string]any{"workspaceFixture": proof})}
	score := func(event p.Event) (KunEvaluationSide, *p.WorkspaceFixtureState) {
		out := KunEvaluationSide{Total: 2, Passed: 2}
		got := scoreKunWorkspaceFixture(&out, side, []store.Event{{ID: 2, Method: "kun/run.started", Data: p.JSON(event)}}, view.Preview)
		return out, got
	}
	out, got := score(ev)
	if out.Status != "pass" || out.Total != 3 || got == nil {
		t.Fatal(out, got)
	}
	ev.RunID = "other"
	out, got = score(ev)
	if out.Unknown != 1 || out.Score != nil || got != nil {
		t.Fatal("other run evidence accepted")
	}
	ev.RunID = side.RunID
	proof.Hash = "wrong"
	ev.Data = p.JSON(map[string]any{"workspaceFixture": proof})
	out, got = score(ev)
	if out.Unknown != 1 || got != nil {
		t.Fatal("wrong fixture accepted")
	}
}
