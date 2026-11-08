package app

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/your-org/rundesk/internal/store"
)

// Retained runtime tests create host-owned/legacy records explicitly. They do
// not use the retired public daily-conversation admission for their fixtures.
func createInternalSessionFixture(m *Manager, wid, title, model string, ids ...string) (Session, error) {
	return m.createSession(wid, title, model, instanceID(ids), SessionSource{}, nil)
}

func TestMaintenanceDailyAdmissionAndHistoricalRead(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	i, err := m.CreateInstance(InstancePatch{Name: "Internal Kun"})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	w := m.Workspaces()[0]
	if _, err = m.CreateSession(w.ID, "daily", "", i.ID); err == nil {
		t.Fatal("daily Kun admitted")
	}
	if _, err = m.Enqueue(TaskSpec{InstanceID: i.ID, WorkspaceID: w.ID, Input: Input{Text: "daily"}}); err == nil {
		t.Fatal("queued Kun admitted")
	}
	if _, err = m.RegisterApplication("kun-app", ApplicationInput{Name: "app", InstanceID: i.ID, WorkspaceID: w.ID}); err == nil {
		t.Fatal("Kun application registered")
	}
	legacy, err := createInternalSessionFixture(m, w.ID, "legacy", "", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SubmitTask(TaskSubmission{SessionID: legacy.ID, Spec: TaskSpec{Input: Input{Text: "continue"}}}); err == nil {
		t.Fatal("legacy session continued through task facade")
	}
	h := NewHandler(m, "", true)
	for _, suffix := range []string{"/turns", "/steer"} {
		res := v1Request(h, "POST", "/sessions/"+legacy.ID+suffix, `{"text":"continue"}`, "maintenance-test-"+strings.TrimPrefix(suffix, "/"))
		if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "kun_maintenance_only") {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	res := request(h, "GET", "/api/v1/sessions/"+legacy.ID, "", "", "")
	if res.Code != 200 {
		t.Fatal("history lost", res.Code)
	}
	if len(m.Tasks()) != 0 {
		t.Fatal("rejected tasks left side effects")
	}
	if _, err = m.CreateSession(w.ID, "Codex remains available", "", DefaultInstance); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceCodexTraceUsesIsolatedKun(t *testing.T) {
	m := testManager(t)
	source, err := m.CreateSession(m.Workspaces()[0].ID, "business", "demo-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "source task"}); err != nil {
		t.Fatal(err)
	}
	source = waitState(t, m, source.ID, "completed")
	m.Demo = false
	i, err := m.CreateInstance(InstancePatch{Name: "Maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "maintenance", AllowWrite: true, SystemPrompt: "PRIVATE-MAINTENANCE-NOTES"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := m.CreateWorkspace("Maintenance", filepath.Join(t.TempDir(), "maintenance"))
	if err != nil {
		t.Fatal(err)
	}
	selection := TraceSelection{SessionID: source.ID, RunID: source.RunID}
	if _, err = m.CreateMaintenanceTraceAnalysis(selection); err == nil {
		t.Fatal("missing maintenance config silently fell back")
	}
	if err = m.Store.Put("system", "setup-kun", setupKunConfig{InstanceID: i.ID, WorkspaceID: w.ID}); err != nil {
		t.Fatal(err)
	}
	analysis, err := m.CreateMaintenanceTraceAnalysis(selection)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.RuntimeKind != "kun" || analysis.InstanceID == source.InstanceID || analysis.WorkspaceID == source.WorkspaceID || analysis.TraceOrigin.SourceInstanceID != source.InstanceID {
		t.Fatal(analysis)
	}
	start, err := m.kunStartRequest(analysis, w, Input{Text: "Explain the trace"})
	if err != nil {
		t.Fatal(err)
	}
	if start.Config.AllowWrite || start.Config.AllowBash || start.Workspace == w.Path || start.Diagnostic == nil || start.Diagnostic.SessionID != source.ID {
		t.Fatal("diagnostic authority was not isolated", start)
	}
	encoded, _ := json.Marshal(start)
	if strings.Contains(string(encoded), "PRIVATE-MAINTENANCE-NOTES") {
		t.Fatal("maintenance instructions leaked into read-only diagnosis")
	}
	if err = m.allowInteractiveSession(analysis.ID); err != nil {
		t.Fatal("trace questions blocked", err)
	}
	current, _ := m.Session(source.ID)
	if current.RunID != source.RunID || current.InstanceID != source.InstanceID {
		t.Fatal("source changed")
	}
}

func TestMaintenanceBuildPlanAndIndependentEvidence(t *testing.T) {
	m, run, _, cap := buildAssistantFixture(t)
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", run.ID, &record); err != nil {
		t.Fatal(err)
	}
	record.ProtocolVersion = 1
	if err := m.Store.Put("build-assistant", run.ID, record); err != nil {
		t.Fatal(err)
	}
	requestID := store.ID()
	if _, err := m.requireMaintenancePlan(cap, requestID); err == nil {
		t.Fatal("unplanned candidate admitted")
	}
	plan := BuildMaintenancePlan{RequestID: requestID, Goal: "Fix packaging", Hypothesis: "Duplicate resource inclusion", Preserve: []string{"original smoke contract"}, Checks: []string{"offline and smoke pass"}}
	first, err := m.recordMaintenancePlan(cap, plan)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := m.recordMaintenancePlan(cap, plan)
	if err != nil || retry.Created != first.Created {
		t.Fatal("plan retry duplicated", err)
	}
	changed := plan
	changed.Goal = "different"
	if _, err = m.recordMaintenancePlan(cap, changed); err == nil {
		t.Fatal("plan was mutable")
	}
	if _, err = m.requireMaintenancePlan(cap, requestID); err != nil {
		t.Fatal(err)
	}
	in, _, built := experienceImageFixture(t, m)
	built.OwnerRunID = run.ID
	if err = m.Store.Put("image-build", built.ID, built); err != nil {
		t.Fatal(err)
	}
	// Reconstruct after a crash, when the build receipt exists but the plan's
	// BuildID projection has not been persisted yet.
	if err = m.Store.Put("build-center-request", maintenancePlanKey(run.ID, requestID), buildCenterRequest{ResultID: built.ID}); err != nil {
		t.Fatal(err)
	}
	v, err := m.buildMaintenanceView(run.ID, "completed")
	if err != nil || v.Phase != "blocked" || v.VerifiedBuildID != "" {
		t.Fatal("foreign checks promoted candidate", v, err)
	}
	for _, id := range in.VerificationIDs {
		test, _ := m.BuildCenterBuild(id)
		test.OwnerRunID = run.ID
		if err = m.Store.Put("image-build", id, test); err != nil {
			t.Fatal(err)
		}
	}
	v, err = m.buildMaintenanceView(run.ID, "completed")
	if err != nil || v.Phase != "verified" || v.VerifiedBuildID != built.ID {
		t.Fatal(v, err)
	}
	if _, err = m.RecordBuildExperience(in); err != nil {
		t.Fatal(err)
	}
	v, err = m.buildMaintenanceView(run.ID, "completed")
	if err != nil || v.ExperienceID == "" {
		t.Fatal("verified experience not projected", v, err)
	}
	plan.RequestID = store.ID()
	if _, err = m.recordMaintenancePlan(cap, plan); err == nil {
		t.Fatal("kept evolving after success")
	}
	if _, err = m.requireMaintenancePlan(cap, requestID); err != nil {
		t.Fatal("idempotent build retry blocked", err)
	}
	for _, id := range in.VerificationIDs {
		test, _ := m.BuildCenterBuild(id)
		if test.TestKind == "smoke" {
			test.ContextSHA256 = "different-candidate"
			if err = m.Store.Put("image-build", id, test); err != nil {
				t.Fatal(err)
			}
		}
	}
	v, err = m.buildMaintenanceView(run.ID, "completed")
	if err != nil || v.VerifiedBuildID != "" {
		t.Fatal("mixed candidate evidence accepted", v, err)
	}
}

func TestMaintenancePlanBudgetAndPendingGate(t *testing.T) {
	m, run, _, cap := buildAssistantFixture(t)
	plan := func(id string) BuildMaintenancePlan {
		return BuildMaintenancePlan{RequestID: id, Goal: "Fix dependency", Hypothesis: "Source unavailable", Preserve: []string{"smoke assertions"}, Checks: []string{"offline and smoke"}}
	}
	for n := 0; n < 3; n++ {
		id := store.ID()
		p, err := m.recordMaintenancePlan(cap, plan(id))
		if err != nil {
			t.Fatal(n, err)
		}
		if _, err = m.recordMaintenancePlan(cap, plan(store.ID())); err == nil {
			t.Fatal("parallel plan admitted")
		}
		b := ImageBuild{ID: store.ID(), OwnerRunID: run.ID, Status: "succeeded"}
		if err = m.Store.Put("image-build", b.ID, b); err != nil {
			t.Fatal(err)
		}
		p.BuildID = b.ID
		if err = m.Store.Put("build-maintenance-plan", maintenancePlanKey(run.ID, id), p); err != nil {
			t.Fatal(err)
		}
		if _, err = m.recordMaintenancePlan(cap, plan(store.ID())); err == nil {
			t.Fatal("unchecked candidate bypassed")
		}
		b.Status = "failed"
		if err = m.Store.Put("image-build", b.ID, b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.recordMaintenancePlan(cap, plan(store.ID())); err == nil {
		t.Fatal("fourth attempt admitted")
	}
}

func TestMaintenanceWorkspaceReserved(t *testing.T) {
	m, run, _, _ := buildAssistantFixture(t)
	if _, err := m.CreateSession(run.WorkspaceID, "daily", "", DefaultInstance); err == nil {
		t.Fatal("maintenance directory used by daily session")
	}
	if _, err := m.Enqueue(TaskSpec{InstanceID: DefaultInstance, WorkspaceID: run.WorkspaceID, Input: Input{Text: "daily"}}); err == nil {
		t.Fatal("maintenance directory used by daily task")
	}
	found := false
	for _, w := range m.Workspaces() {
		if w.ID == run.WorkspaceID {
			found = true
			if w.Purpose != "maintenance" {
				t.Fatal(w)
			}
		}
	}
	if !found {
		t.Fatal("maintenance metadata missing")
	}
}

// Keep the public serialized maintenance projections aligned with the shipped
// contract: strict generated clients otherwise reject these optional fields.
func TestMaintenanceOpenAPIProjections(t *testing.T) {
	m := testManager(t)
	spec := object(t, v1Request(NewHandler(m, "", true), "GET", "/openapi.json", "", ""))
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	plan := BuildMaintenancePlan{RequestID: store.ID(), Goal: "Fix", Hypothesis: "Source", Preserve: []string{"smoke"}, Checks: []string{"offline"}}
	attempt := BuildMaintenanceAttempt{Plan: plan, BuildID: "build", Status: "failed", ImageID: "image", ContextSHA256: "context", VerificationIDs: []string{"check"}, Error: "fixture"}
	view := BuildMaintenanceView{Phase: "blocked", MaxAttempts: 3, Attempts: []BuildMaintenanceAttempt{attempt}, VerifiedBuildID: "build", ExperienceID: "experience", StopReason: "fixture"}
	cases := map[string]any{
		"BuildAssistantRun":    BuildAssistantRun{ProtocolVersion: 1, Maintenance: &view},
		"BuildMaintenancePlan": plan, "BuildMaintenanceAttempt": attempt, "BuildMaintenanceView": view,
		"TraceOrigin": TraceOrigin{SourceInstanceID: "source", SourceWorkspaceID: "workspace"},
		"Workspace":   Workspace{Purpose: "maintenance"},
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err = json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			schema, ok := schemas[name].(map[string]any)
			if !ok {
				t.Fatal("missing schema", name)
			}
			props := schema["properties"].(map[string]any)
			for key := range fields {
				if _, ok := props[key]; !ok {
					t.Fatal("undocumented response field", name, key)
				}
			}
		})
	}
}
