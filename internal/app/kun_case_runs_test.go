package app

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"strings"
	"testing"
)

func TestKunCasePreviewDriftSecretsAndRestart(t *testing.T) {
	m := kunMCPManager(t)
	w := m.Workspaces()[0]
	i, _ := m.Instance()
	t.Setenv("K4_PREVIEW_TEST_KEY", "preview-secret-must-not-persist")
	cfg := i.AgentRuntime
	cfg.APIKeyEnv = "K4_PREVIEW_TEST_KEY"
	i, err := m.SetAgentRuntime(i.ID, i.Revision, cfg)
	if err != nil {
		t.Fatal(err)
	}
	set, err := m.CreateKunTaskSet(taskSetFixture())
	if err != nil {
		t.Fatal(err)
	}
	in := KunCaseRunInput{TaskSetID: set.ID, ExpectedTaskSetHash: set.Hash, CaseKey: set.Cases[0].Key, WorkspaceID: w.ID, InstanceID: i.ID}
	h := NewHandler(m, userTestAdmin, true)
	r := appRequest(h, "POST", "/api/v1/kun-case-runs", string(p.JSON(in)), userTestAdmin, "case-preview-key")
	var v KunCaseRunView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &v) != nil {
		t.Fatal(r.Code, r.Body.String())
	}
	same := appRequest(h, "POST", "/api/v1/kun-case-runs", string(p.JSON(in)), userTestAdmin, "case-preview-key")
	var repeated KunCaseRunView
	if same.Code != 200 || json.Unmarshal(same.Body.Bytes(), &repeated) != nil || forkDigest(repeated) != forkDigest(v) {
		t.Fatal("idempotent preview changed")
	}
	if strings.Contains(r.Body.String(), "preview-secret-must-not-persist") || strings.Contains(v.Preview.EffectiveInput, set.Cases[1].Input) {
		t.Fatal("unrelated input or credential leaked")
	}
	if _, err = m.StartKunCaseRun(v.Preview.ID, "wrong", true); err == nil {
		t.Fatal("stale hash accepted")
	}
	if _, err = m.Notes(w.ID, "New project instructions", w.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = m.StartKunCaseRun(v.Preview.ID, v.Preview.Hash, true); err == nil {
		t.Fatal("configuration drift accepted")
	}
	if len(m.handles) != 0 {
		t.Fatal("drift started worker")
	}
	if _, err = m.Session(v.Preview.SessionID); err == nil {
		t.Fatal("drift persisted session")
	}
	for _, path := range []string{"/kun-case-runs?limit=51&taskSetId=" + set.ID, "/kun-case-runs?taskSetId=" + set.ID + "&unknown=1", "/kun-case-runs/" + v.Preview.ID + "?unknown=1", "/kun-skills?limit=51", "/kun-skills/id?unknown=1"} {
		r := appRequest(h, "GET", "/api/v1"+path, "", userTestAdmin, "")
		if r.Code != 400 {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	data := m.Data
	m.Close()
	reopened, err := New(data, "missing-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	read, err := reopened.KunCaseRun(v.Preview.ID)
	if err != nil || read.Preview.Hash != v.Preview.Hash || read.Status != "planned" {
		t.Fatal(read, err)
	}
}
