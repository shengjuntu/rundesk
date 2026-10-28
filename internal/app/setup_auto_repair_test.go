package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func setupAutoFailedInstall(t *testing.T, m *Manager) CodexInstallJob {
	t.Helper()
	v := CodexInstallJob{ID: store.ID(), Status: "failed", Platform: runtime.GOOS + "/" + runtime.GOARCH, Input: CodexInstallInput{Version: "0.157.1", Method: "native", Confirm: true}, Result: CodexInstallResult{Version: "0.157.1", Error: "403 Forbidden"}, CreatedAt: store.Now(), UpdatedAt: store.Now()}
	if err := m.Store.Put("codex-install", v.ID, codexInstallRecord{CodexInstallJob: v}); err != nil {
		t.Fatal(err)
	}
	return v
}

func setupAutoGrant(t *testing.T, m *Manager, kind, source string) (SetupAutoRepairInput, SetupAutoRepair) {
	t.Helper()
	in := SetupAutoRepairInput{Kind: kind, SourceID: source, Enabled: true, RequestID: store.ID()}
	v, err := m.RegisterSetupAutoRepair(in, "http://127.0.0.1:3210/api/build-center/mcp")
	if err != nil || v.Status != "watching" {
		t.Fatal(v, err)
	}
	return in, v
}

func setupAutoRecord(t *testing.T, m *Manager, id string) setupAutoRepairRecord {
	t.Helper()
	var record setupAutoRepairRecord
	if err := m.Store.Get("setup-auto-repair", id, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestSetupAutoRepairOnlyExplicitSourceAndExactlyOneTask(t *testing.T) {
	m, _ := setupAgentFixture(t)
	history := setupAutoFailedInstall(t, m)
	selected := setupAutoFailedInstall(t, m)
	m.checkSetupAutoRepairs()
	if len(m.Tasks()) != 0 {
		t.Fatal("historical failures were automatically claimed")
	}
	in, value := setupAutoGrant(t, m, "codex-install", selected.ID)
	m.checkSetupAutoRepairs()
	created := setupAutoRecord(t, m, value.ID)
	if created.Status != "submitted" || created.RepairID == "" || created.SessionID == "" || len(m.Tasks()) != 1 {
		t.Fatal(created, m.Tasks())
	}
	run, err := m.SetupRepair(created.RepairID)
	if err != nil || run.SourceInstallID != selected.ID || run.SourceInstallID == history.ID {
		t.Fatal(run, err)
	}
	again, err := m.RegisterSetupAutoRepair(in, "http://changed-request-host.invalid/api/build-center/mcp")
	if err != nil || again.ID != value.ID || again.RepairID != created.RepairID {
		t.Fatal("idempotent retry rebound authorization", again, err)
	}
	// Simulate a process stopping after durable task admission but before the
	// monitor's own submitted checkpoint. Reconciliation must reuse that task.
	created.Status, created.RepairID, created.SessionID, created.TaskID = "watching", "", "", ""
	if err := m.Store.Put("setup-auto-repair", created.ID, created); err != nil {
		t.Fatal(err)
	}
	m.checkSetupAutoRepairs()
	after := setupAutoRecord(t, m, value.ID)
	if after.Status != "submitted" || after.RepairID != run.ID || len(m.Tasks()) != 1 {
		t.Fatal("restart reconciliation duplicated a repair", after, m.Tasks())
	}
	conflict := in
	conflict.SourceID = history.ID
	if _, err := m.RegisterSetupAutoRepair(conflict, created.Callback); err == nil {
		t.Fatal("same request ID accepted different authorization")
	}
}

func TestSetupAutoRepairBackgroundHandoffWithoutBrowserPolling(t *testing.T) {
	m, _ := setupAgentFixture(t)
	source := setupAutoFailedInstall(t, m)
	_, value := setupAutoGrant(t, m, "codex-install", source.ID)
	// No call to the monitor or a browser/API poll drives the effect. Manager's
	// independent background loop must admit the task after registration.
	created := waitSetupAutoRepair(t, m, value.ID, "submitted")
	if created.RepairID == "" || len(m.Tasks()) != 1 {
		t.Fatal(created, m.Tasks())
	}
}

func TestSetupAutoRepairDisableAndExplicitReauthorization(t *testing.T) {
	m, _ := setupAgentFixture(t)
	source := setupAutoFailedInstall(t, m)
	in, value := setupAutoGrant(t, m, "codex-install", source.ID)
	in.Enabled, in.RequestID = false, store.ID()
	disabled, err := m.RegisterSetupAutoRepair(in, "")
	if err != nil || disabled.Status != "disabled" || disabled.Enabled {
		t.Fatal(disabled, err)
	}
	m.checkSetupAutoRepairs()
	if len(m.Tasks()) != 0 {
		t.Fatal("disabled handoff submitted a task")
	}
	in.Enabled, in.RequestID = true, store.ID()
	_, err = m.RegisterSetupAutoRepair(in, "http://127.0.0.1:3210/api/build-center/mcp")
	if err != nil {
		t.Fatal(err)
	}
	m.checkSetupAutoRepairs()
	if after := setupAutoRecord(t, m, value.ID); after.Status != "submitted" || len(m.Tasks()) != 1 {
		t.Fatal(after)
	}
}

func TestSetupAutoRepairChangedConfigurationAndExpiredEvidenceBlock(t *testing.T) {
	for _, scenario := range []string{"configuration", "expired-grant", "changed-source"} {
		t.Run(scenario, func(t *testing.T) {
			m, ready := setupAgentFixture(t)
			source := setupAutoFailedInstall(t, m)
			_, value := setupAutoGrant(t, m, "codex-install", source.ID)
			switch scenario {
			case "configuration":
				revision := ready.Revision
				if _, err := m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: ready.Endpoint, Model: ready.Model, KunPath: m.Kun, ExpectedRevision: &revision}); err != nil {
					t.Fatal(err)
				}
			case "expired-grant":
				record := setupAutoRecord(t, m, value.ID)
				record.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
				_ = m.Store.Put("setup-auto-repair", record.ID, record)
			case "changed-source":
				source.Input.Version = "0.158.0"
				_ = m.Store.Put("codex-install", source.ID, codexInstallRecord{CodexInstallJob: source})
			}
			m.checkSetupAutoRepairs()
			after := setupAutoRecord(t, m, value.ID)
			if after.Status != "blocked" || after.Error == "" || len(m.Tasks()) != 0 {
				t.Fatal("stale authorization was exercised", after, m.Tasks())
			}
		})
	}
}

func TestSetupAutoRepairSuccessfulSourceNeedsNoAgentAndAttemptsCannotRecurse(t *testing.T) {
	m, _ := setupAgentFixture(t)
	success := setupAutoFailedInstall(t, m)
	_, value := setupAutoGrant(t, m, "codex-install", success.ID)
	success.Status, success.Result.OK = "succeeded", true
	_ = m.Store.Put("codex-install", success.ID, codexInstallRecord{CodexInstallJob: success})
	m.checkSetupAutoRepairs()
	if after := setupAutoRecord(t, m, value.ID); after.Status != "completed" || len(m.Tasks()) != 0 {
		t.Fatal(after)
	}
	attempt := setupAutoFailedInstall(t, m)
	owner := setupRepairRecord{SetupRepairRun: SetupRepairRun{ID: store.ID(), AttemptIDs: []string{attempt.ID}}, Attempts: map[string]setupRepairAttempt{"retry": {InstallID: attempt.ID}}}
	_ = m.Store.Put("setup-repair", owner.ID, owner)
	if _, err := m.RegisterSetupAutoRepair(SetupAutoRepairInput{Kind: "codex-install", SourceID: attempt.ID, Enabled: true, RequestID: store.ID()}, "http://127.0.0.1:3210/api/build-center/mcp"); err == nil {
		t.Fatal("repair attempt recursively registered itself")
	}
}

func TestSetupAutoRepairImageUsesKunAndFrozenFailure(t *testing.T) {
	m, ready := setupAgentFixture(t)
	failed := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "recipe", Source: "build", Status: "failed", Dockerfile: "Dockerfile", Created: store.Now(), Error: "pip failed", RecipeID: "fixture-recipe", RecipeSmoke: &RecipeSmokeSpec{Command: []string{"tool", "--offline"}, TimeoutSeconds: 20}}
	experienceWriteContext(t, m, &failed, map[string]string{"Dockerfile": "FROM scratch\n"})
	_, value := setupAutoGrant(t, m, "image-build", failed.ID)
	m.checkSetupAutoRepairs()
	after := setupAutoRecord(t, m, value.ID)
	if after.Status != "submitted" || after.SessionID == "" || len(m.Tasks()) != 1 {
		t.Fatal(after, m.Tasks())
	}
	run, err := m.BuildAssistantRun(after.RepairID)
	if err != nil || run.InstanceID != ready.InstanceID || run.RuntimeKind != "kun" || run.SourceBuildID != failed.ID {
		t.Fatal(run, err)
	}
	owned := failed
	owned.ID, owned.OwnerRunID = store.ID(), run.ID
	_ = m.Store.Put("image-build", owned.ID, owned)
	if _, err := m.RegisterSetupAutoRepair(SetupAutoRepairInput{Kind: "image-build", SourceID: owned.ID, Enabled: true, RequestID: store.ID()}, "http://127.0.0.1:3210/api/build-center/mcp"); err == nil {
		t.Fatal("agent-owned build recursively registered itself")
	}
}

func TestSetupAutoRepairHTTPDerivesCallbackAndCanDisableWithoutKun(t *testing.T) {
	m, _ := setupAgentFixture(t)
	source := setupAutoFailedInstall(t, m)
	mux := http.NewServeMux()
	s := &Server{Manager: m, PublicOrigin: "https://rundesk.example.test"}
	s.setupAutoRepairRoutes(mux)
	input := SetupAutoRepairInput{Kind: "codex-install", SourceID: source.ID, Enabled: true, RequestID: store.ID()}
	data, _ := json.Marshal(input)
	r := httptest.NewRequest("POST", "http://localhost/api/setup/auto-repairs", strings.NewReader(string(data)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	record := setupAutoRecord(t, m, setupAutoRepairID(input.Kind, source.ID))
	if record.Callback != "https://rundesk.example.test/api/build-center/mcp" {
		t.Fatal(record)
	}
	_ = m.Store.DeleteObject("system", "setup-kun")
	_ = m.Store.DeleteObject("codex-install", source.ID)
	input.Enabled, input.RequestID = false, store.ID()
	data, _ = json.Marshal(input)
	r = httptest.NewRequest("POST", "http://localhost/api/setup/auto-repairs", strings.NewReader(string(data)))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "disabled") {
		t.Fatal("revocation required missing dependencies", w.Code, w.Body.String())
	}
}

func waitSetupAutoRepair(t *testing.T, m *Manager, id string, statuses ...string) setupAutoRepairRecord {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		record := setupAutoRecord(t, m, id)
		for _, status := range statuses {
			if record.Status == status {
				return record
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("automatic handoff did not reach expected status", setupAutoRecord(t, m, id))
	return setupAutoRepairRecord{}
}

func TestSetupAutoRepairLongBuildRevalidatesModelOnceWithSameCredentials(t *testing.T) {
	for _, scenario := range []string{"ready", "model-failed", "credential-changed", "probe-interrupted", "disabled-during-refresh"} {
		t.Run(scenario, func(t *testing.T) {
			m, old := setupAgentFixture(t)
			var calls atomic.Int32
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Setenv("RUNDESK_AUTO_MODEL_TEST_KEY", "fixture-only-model-secret")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if scenario == "disabled-during-refresh" && call == 1 {
					close(started)
					<-release
				}
				if r.Header.Get("Authorization") != "Bearer fixture-only-model-secret" {
					t.Error("automatic revalidation used another credential")
				}
				if scenario == "model-failed" || scenario == "disabled-during-refresh" {
					http.Error(w, "model unavailable", 503)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Header().Set("Content-Type", "application/json")
				if body["tools"] != nil {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "setup-ping", "type": "function", "function": map[string]any{"name": "rundesk_setup_ping", "arguments": "{\"value\":\"OK\"}"}}}}}}})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "OK"}}}})
				}
			}))
			defer server.Close()
			defer releaseOnce.Do(func() { close(release) })
			endpoint := server.URL + "/v1"
			key := setupModelTestKey(endpoint, "fixture-model", "RUNDESK_AUTO_MODEL_TEST_KEY")
			evidence := setupModelTestEvidence{SetupModelTestResult: SetupModelTestResult{Endpoint: endpoint, Model: "fixture-model", InferenceOK: true, ToolsOK: true, RoundTripOK: true, Status: "ready", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}, APIKeyEnv: "RUNDESK_AUTO_MODEL_TEST_KEY", CredentialHash: keyHash("fixture-only-model-secret")}
			_ = m.Store.Put("setup-model-test", key, evidence)
			revision := old.Revision
			ready, err := m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: endpoint, Model: "fixture-model", APIKeyEnv: evidence.APIKeyEnv, KunPath: m.Kun, ExpectedRevision: &revision})
			if err != nil || !ready.Ready {
				t.Fatal(ready, err)
			}
			source := setupAutoFailedInstall(t, m)
			_, value := setupAutoGrant(t, m, "codex-install", source.ID)
			evidence.CheckedAt = time.Now().Add(-40 * time.Minute).UTC().Format(time.RFC3339Nano)
			_ = m.Store.Put("setup-model-test", key, evidence)
			if scenario == "credential-changed" {
				t.Setenv("RUNDESK_AUTO_MODEL_TEST_KEY", "replacement-not-authorized")
			}
			if scenario == "probe-interrupted" {
				record := setupAutoRecord(t, m, value.ID)
				record.Status, record.RefreshAttempted, record.RefreshOwner = "revalidating", true, "previous-server-process"
				_ = m.Store.Put("setup-auto-repair", record.ID, record)
			}
			m.checkSetupAutoRepairs()
			if scenario == "disabled-during-refresh" {
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatal("background model probe did not start")
				}
				disabled, err := m.RegisterSetupAutoRepair(SetupAutoRepairInput{Kind: "codex-install", SourceID: source.ID, Enabled: false, RequestID: store.ID()}, "")
				if err != nil || disabled.Status != "disabled" {
					t.Fatal("in-flight model request prevented revocation", disabled, err)
				}
				releaseOnce.Do(func() { close(release) })
				until := time.Now().Add(2 * time.Second)
				for time.Now().Before(until) {
					if _, active := setupAutoRevalidations.Load(setupAutoRevalidationKey(m, setupAutoRecord(t, m, value.ID))); !active {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				m.checkSetupAutoRepairs()
				if after := setupAutoRecord(t, m, value.ID); after.Status != "disabled" || len(m.Tasks()) != 0 {
					t.Fatal("probe completion resurrected revoked authorization", after, m.Tasks())
				}
				return
			}
			if scenario == "ready" {
				refreshed := waitSetupAutoRepair(t, m, value.ID, "watching", "submitted")
				if !refreshed.RefreshAttempted || refreshed.ModelRefreshedAt == "" || calls.Load() != 3 {
					t.Fatal("full inference/tool/result round trip was not verified", refreshed, calls.Load())
				}
				m.checkSetupAutoRepairs()
				after := setupAutoRecord(t, m, value.ID)
				if after.Status != "submitted" || len(m.Tasks()) != 1 || calls.Load() != 3 {
					t.Fatal(after, m.Tasks(), calls.Load())
				}
			} else {
				blocked := waitSetupAutoRepair(t, m, value.ID, "blocked")
				if blocked.Error == "" || len(m.Tasks()) != 0 {
					t.Fatal(blocked, m.Tasks())
				}
				if scenario == "credential-changed" || scenario == "probe-interrupted" {
					if calls.Load() != 0 {
						t.Fatal("changed authority or interrupted probe retried itself", calls.Load())
					}
				} else if calls.Load() != 1 {
					t.Fatal("failed model probe did not stop immediately", calls.Load())
				}
				before := calls.Load()
				m.checkSetupAutoRepairs()
				if calls.Load() != before {
					t.Fatal("blocked handoff probed the model again")
				}
			}
		})
	}
}
