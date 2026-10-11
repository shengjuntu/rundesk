package app

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func skillCase(key, content string) KunTaskCase {
	return KunTaskCase{Key: key, Title: key, Input: string(p.JSON(map[string]string{"domain": "file-copy", "path": key + ".txt", "content": content})), Rules: []KunReplyRule{{Kind: "reply_contains", Value: content}}}
}
func TestKunSkillEndToEndIndependentGateAndApplicationScope(t *testing.T) {
	m := kunMCPManager(t)
	m.Kun = buildKunTestBinary(t)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Messages []p.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var input map[string]string
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				_ = json.Unmarshal([]byte(strings.Split(msg.Content, "\nSave deliverables")[0]), &input)
				break
			}
		}
		tools := 0
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				tools++
			}
		}
		reply := p.Message{Role: "assistant", Content: input["content"]}
		finish := "stop"
		if tools == 0 {
			reply.Content = ""
			reply.ToolCalls = []p.ToolCall{{ID: "source-write", Type: "function", Function: p.Function{Name: "write_file", Arguments: string(p.JSON(map[string]string{"path": input["path"], "content": input["content"]}))}}, {ID: "source-read", Type: "function", Function: p.Function{Name: "read_file", Arguments: string(p.JSON(map[string]string{"path": input["path"]}))}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": reply, "finish_reason": finish}}, "usage": map[string]int{"total_tokens": 9}})
	}))
	defer provider.Close()
	i, err := m.CreateInstance(InstancePatch{Name: "K4 test application"})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Model: "fixture", Endpoint: provider.URL + "/v1", AllowWrite: true, MaxSteps: 5})
	if err != nil {
		t.Fatal(err)
	}
	w := m.Workspaces()[0]
	train, err := m.CreateKunTaskSet(KunTaskSetInput{Title: "source", Cases: []KunTaskCase{skillCase("train", "TRAIN")}})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := m.CreateKunTaskSet(KunTaskSetInput{Title: "holdout", Cases: []KunTaskCase{skillCase("valid-a", "ALPHA"), skillCase("valid-b", "BETA")}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := m.CreateKunCaseRun(KunCaseRunInput{TaskSetID: train.ID, ExpectedTaskSetHash: train.Hash, CaseKey: "train", WorkspaceID: w.ID, InstanceID: i.ID})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || len(m.handles) != 0 || preview.Session != nil {
		t.Fatal("preview started runtime")
	}
	if _, err = m.Session(preview.Preview.SessionID); err == nil {
		t.Fatal("preview persisted session")
	}
	if _, err = m.StartKunCaseRun(preview.Preview.ID, preview.Preview.Hash, false); err == nil {
		t.Fatal("unconfirmed started")
	}
	var wg sync.WaitGroup
	for n := 0; n < 5; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := m.StartKunCaseRun(preview.Preview.ID, preview.Preview.Hash, true)
			if err != nil || s.RunID != preview.Preview.RunID {
				t.Errorf("start mismatch %v", err)
			}
		}()
	}
	wg.Wait()
	waitState(t, m, preview.Preview.SessionID, "completed")
	if calls.Load() != 2 {
		t.Fatal("duplicate case execution", calls.Load())
	}
	if _, err = m.Start(preview.Preview.SessionID, Input{Text: "changed"}); err == nil {
		t.Fatal("case input changed")
	}
	score, err := m.KunCaseScore(m.ctx, preview.Preview.ID)
	if err != nil || score.Evaluation.Status != "pass" {
		t.Fatal(score, err)
	}
	input := KunSkillInput{Title: "Copy file", Domain: "file-copy", Applicability: "JSON file-copy tasks in this workspace", FailureCases: []string{"schema or catalog changes fall back"}, SourcePlanIDs: []string{preview.Preview.ID}, ValidationTaskSetID: valid.ID, ExpectedValidationHash: valid.Hash, InputSchema: json.RawMessage(`{"type":"object","required":["domain","path","content"],"properties":{"domain":{"type":"string"},"path":{"type":"string"},"content":{"type":"string"}},"additionalProperties":false}`)}
	candidate, err := m.CreateKunSkill(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Program.Steps) != 3 || candidate.Program.Steps[0].Args["content"].Ref != "input#/content" {
		t.Fatal("not parameterized", candidate.Program)
	}
	app, err := m.RegisterApplication("skill-app", ApplicationInput{Name: "Skill app", InstanceID: i.ID, WorkspaceID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := m.CreateKunSkillValidation(candidate.ID, candidate.Hash)
	if err != nil {
		t.Fatal(err)
	}
	incomplete, err := m.EvaluateKunSkill(batch.ID, batch.Hash)
	if err != nil || incomplete.Status != "unknown" || incomplete.Score != nil {
		t.Fatal(incomplete, err)
	}
	if _, err = m.PromoteKunSkill(candidate.ID, candidate.Hash, incomplete.ID, incomplete.Hash, app.AppID, app.Revision); err == nil {
		t.Fatal("incomplete validation enabled")
	}
	for _, plan := range batch.Plans {
		if _, err = m.StartKunCaseRun(plan.ID, plan.Hash, true); err != nil {
			t.Fatal(err)
		}
		waitState(t, m, plan.SessionID, "completed")
	}
	report, err := m.EvaluateKunSkill(batch.ID, batch.Hash)
	if err != nil || report.Status != "pass" || report.Passed != 2 || report.Score == nil || *report.Score != 100 {
		t.Fatalf("report %+v, %v", report, err)
	}
	if calls.Load() != 2 {
		t.Fatal("fast path used models", calls.Load())
	}
	for _, c := range report.Cases {
		if !c.FastCompleted || c.Summary.ModelCalls != 0 || len(c.Summary.Tools) != 2 {
			t.Fatal(c)
		}
	}
	if _, err = m.PromoteKunSkill(candidate.ID, candidate.Hash, report.ID, report.Hash, app.AppID, app.Revision+1); err == nil {
		t.Fatal("stale app promoted")
	}
	if _, err = m.PromoteKunSkill(candidate.ID, candidate.Hash, report.ID, report.Hash, app.AppID, app.Revision); err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSessionWithSource(w.ID, "production", "", i.ID, SessionSource{Kind: "application", AppID: app.AppID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(session.ID, Input{Text: skillCase("production", "GAMMA").Input, FastSkillID: candidate.ID}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, session.ID, "completed")
	if calls.Load() != 2 {
		t.Fatal("promoted fast path did not run", calls.Load())
	}
	human, err := m.CreateSession(w.ID, "human", "")
	if err != nil {
		t.Fatal(err)
	}
	ww, _ := m.Workspace(w.ID)
	if _, err = m.kunStartRequest(human, ww, Input{Text: skillCase("human", "no").Input, FastSkillID: candidate.ID}); err == nil {
		t.Fatal("human bypassed application capability")
	}
	overlap, _ := m.CreateKunTaskSet(KunTaskSetInput{Title: "overlap", Cases: []KunTaskCase{skillCase("train", "TRAIN"), skillCase("another", "OK")}})
	input.ValidationTaskSetID, input.ExpectedValidationHash = overlap.ID, overlap.Hash
	if _, err = m.CreateKunSkill(input); err == nil {
		t.Fatal("holdout leaked source")
	}
	if err = m.DeleteSession(preview.Preview.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.StartKunCaseRun(preview.Preview.ID, preview.Preview.Hash, true); err == nil {
		t.Fatal("deleted case resurrected")
	}
}
func TestKunCaseAndSkillAPIAuthorization(t *testing.T) {
	m, user, code, _, viewer, source, _ := userFixture(t)
	_, _, key := setupKey(t, m, "skill-key", source.WorkspaceID, "read", "run", "approvals")
	h := NewHandler(m, userTestAdmin, true)
	routes := []struct{ method, path string }{{"POST", "/kun-skills"}, {"GET", "/kun-skills/id"}, {"POST", "/kun-skills/id/promote"}, {"POST", "/kun-skills/id/disable"}, {"POST", "/kun-skills/id/validations"}, {"GET", "/kun-skill-validations/id"}, {"POST", "/kun-skill-validations/id/start"}, {"POST", "/kun-skill-validations/id/evaluate"}, {"GET", "/kun-skill-reports/id"}, {"GET", "/kun-case-runs/id/score"}, {"POST", "/kun-case-runs"}, {"GET", "/kun-case-runs"}, {"GET", "/kun-case-runs/id"}, {"POST", "/kun-case-runs/id/start"}}
	for _, token := range []string{code, viewer, key} {
		for _, prefix := range []string{"/api", "/api/v1", "/api/v1/member/" + user.Grants[0].ID} {
			for _, route := range routes {
				r := appRequest(h, route.method, prefix+route.path, `{}`, token, "denied-skill")
				if r.Code != 403 {
					t.Fatal(route, prefix, r.Code, r.Body.String())
				}
			}
		}
	}
}
