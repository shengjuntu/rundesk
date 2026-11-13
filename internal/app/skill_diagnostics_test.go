package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/skillcheck"
	"github.com/shengjuntu/rundesk/internal/store"
)

func diagnosticQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func diagnosticScanner(t *testing.T, version, report string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX trusted scanner fixture")
	}
	path := filepath.Join(t.TempDir(), "trusted-scanner")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then printf '%s' " + diagnosticQuote("SkillSpector v"+version) + "; else printf '%s' " + diagnosticQuote(report) + "; fi\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func diagnosticSpectorReport(t *testing.T, version string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"metadata": map[string]any{"skillspector_version": version}, "execution_successful": true, "risk_assessment": map[string]any{"recommendation": "SAFE"}, "analysis_completeness": map[string]any{"is_complete": true, "fully_inspected_files": 1}, "components": []any{map[string]any{"path": "SKILL.md"}}, "issues": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestSkillDiagnosticRequiresBothVersionAndProtocolEvidence(t *testing.T) {
	for _, checks := range [][]SkillDiagnosticCheck{nil, {{ID: "docker", Status: "ok"}}, {{ID: "version", Status: "ok"}}, {{ID: "protocol", Status: "ok"}}} {
		if skillDiagnosticOverall(checks) == "ready" {
			t.Fatal("partial evidence marked ready", checks)
		}
	}
	if skillDiagnosticOverall([]SkillDiagnosticCheck{{ID: "version", Status: "ok"}, {ID: "protocol", Status: "ok"}, {ID: "model", Status: "unknown"}}) != "incomplete" {
		t.Fatal("model unknown was ignored")
	}
}

func TestSkillDiagnosticRejectsInvalidInstanceBeforeAdmissionOrPersistence(t *testing.T) {
	m := testManager(t)
	for _, instance := range []string{"not-existing", "not/existing", strings.Repeat("x", 65)} {
		_, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup", InstanceID: instance})
		if err == nil {
			t.Fatal("invalid instance accepted", instance)
		}
	}
	if m.skillChecks != nil {
		t.Fatal("invalid instance initialized execution admission")
	}
	recent, err := m.SkillDiagnostics()
	if err != nil || len(recent) != 0 {
		t.Fatal("invalid instance created diagnostic records", recent, err)
	}
	result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup"})
	if err != nil || result.InstanceID != DefaultInstance || result.Overall != "unavailable" {
		t.Fatal("empty instance was not normalized to existing default", result, err)
	}
	recent, err = m.SkillDiagnostics()
	if err != nil || len(recent) != 1 || recent[0].InstanceID != DefaultInstance {
		t.Fatal("default diagnostic was not stored canonically", recent, err)
	}
}
func TestSkillDiagnosticSpectorVersionAndCanaryAreSeparateFromSkillScan(t *testing.T) {
	for _, tc := range []struct{ name, version, report, want string }{
		{"ready", "2.13.4", diagnosticSpectorReport(t, "2.13.4"), "ready"},
		{"unsupported major", "3.0.0", diagnosticSpectorReport(t, "3.0.0"), "incompatible"},
		{"mismatched report version", "2.13.4", diagnosticSpectorReport(t, "2.13.3"), "failed"},
		{"invalid report", "2.13.4", "not JSON", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testManager(t)
			exe := diagnosticScanner(t, tc.version, tc.report)
			t.Setenv("RUNDESK_SKILLSPECTOR_BIN", exe)
			result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"})
			if err != nil || result.Overall != tc.want || result.CheckedAt == "" {
				t.Fatal(result, err)
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), exe) || strings.Contains(string(data), m.Data) {
				t.Fatal("diagnostic leaked executable/source path")
			}
			packs, err := m.SkillPacks("")
			if err != nil || len(packs) != 0 {
				t.Fatal("tool diagnostic created a user skill", packs, err)
			}
			recent, err := m.SkillDiagnostics()
			if err != nil || len(recent) != 1 || recent[0].ID != result.ID {
				t.Fatal(recent, err)
			}
		})
	}
	m := testManager(t)
	t.Setenv("RUNDESK_SKILLSPECTOR_BIN", filepath.Join(t.TempDir(), "missing"))
	result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"})
	if err != nil || result.Overall != "unavailable" {
		t.Fatal(result, err)
	}
}

type diagnosticDocker struct {
	*skillCheckDockerFixture
	version          bool
	versionBanner    string
	validationBanner string
	creates          int
	firstCleanup     chan struct{}
	releaseCleanup   chan struct{}
}

func (f *diagnosticDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "image":
		return json.Marshal([]any{map[string]any{"Id": "sha256:" + strings.Repeat("d", 64), "Os": "linux", "Architecture": runtime.GOARCH, "Config": map[string]any{"Volumes": map[string]any{}}}})
	case "create":
		f.mu.Lock()
		f.version = args[len(args)-1] == "--version"
		f.creates++
		for i := range args {
			if args[i] == "--label" && i+1 < len(args) {
				f.owner = strings.TrimPrefix(args[i+1], "io.rundesk.skillcheck=")
			}
		}
		f.mu.Unlock()
	case "start":
		f.mu.Lock()
		version := f.version
		f.mu.Unlock()
		if version {
			return []byte(f.versionBanner), nil
		}
		return []byte(f.validationBanner), nil
	case "rm":
		if f.firstCleanup != nil {
			select {
			case <-f.firstCleanup:
			default:
				close(f.firstCleanup)
			}
			select {
			case <-f.releaseCleanup:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return f.skillCheckDockerFixture.Run(ctx, args...)
}
func diagnosticEnvironment(t *testing.T, m *Manager, f *diagnosticDocker) Instance {
	t.Helper()
	i, err := m.CreateInstance(InstancePatch{Name: "Diagnostic environment"})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.instances[i.ID].Execution = ExecutionSpec{Mode: "docker", Image: "rundesk-tools:fixture"}
	i = *m.instances[i.ID]
	m.mu.Unlock()
	m.docker = f
	return i
}
func newDiagnosticDocker() *diagnosticDocker {
	return &diagnosticDocker{skillCheckDockerFixture: &skillCheckDockerFixture{containerID: strings.Repeat("c", 64)}, versionBanner: "skill-up version 0.15.0\n", validationBanner: "✓ eval.yaml is valid (loaded 1 case(s))\n"}
}
func TestSkillDiagnosticSkillUpSharesIsolationAndLeavesModelUnknown(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker policy")
	}
	m := testManager(t)
	f := newDiagnosticDocker()
	i := diagnosticEnvironment(t, m, f)
	result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup", InstanceID: i.ID})
	if err != nil || result.Overall != "incomplete" || result.ImageID == "" || result.CleanupJobID != "" {
		t.Fatal(result, err)
	}
	if f.creates != 2 {
		t.Fatal("version and canary were not both checked", f.creates)
	}
	for _, c := range result.Checks {
		if (c.ID == "version" || c.ID == "protocol") && c.Status != "ok" {
			t.Fatal(c)
		}
		if c.ID == "model" && c.Status != "unknown" {
			t.Fatal(c)
		}
	}
	f.mu.Lock()
	calls := append([][]string(nil), f.calls...)
	f.mu.Unlock()
	for _, args := range calls {
		if args[0] == "create" {
			joined := strings.Join(args, "\x00")
			for _, flag := range []string{"--network\x00none", "--read-only", "--log-driver\x00none", "--user\x0065532:65532"} {
				if !strings.Contains(joined, flag) {
					t.Fatal("diagnostic weakened container policy", args)
				}
			}
			if strings.Contains(joined, "\x00run\x00") || strings.Contains(joined, "CODEX_HOME") {
				t.Fatal("diagnostic started an agent or inherited credentials", args)
			}
		}
	}
	rows, err := m.Store.List("skill-check-job")
	if err != nil || len(rows) != 0 {
		t.Fatal("successful canary occupied ordinary test history", len(rows), err)
	}
}

func seedSkillDiagnosticOrdinaryHistory(t *testing.T, m *Manager, pack SkillPack, i Instance, count int) {
	t.Helper()
	records := make([]store.Record, 0, count)
	for n := 0; n < count; n++ {
		id := store.ID()
		// Even an ordinary environment with the diagnostic display name is not
		// an internal canary: its pack identity must keep it in ordinary history.
		job := SkillCheckJob{ID: id, PackID: pack.ID, Digest: pack.Digest, Action: "validate", InstanceID: i.ID, EnvironmentName: skillDiagnosticEnvironmentName, ContainerName: "rundesk-skillcheck-" + id, Status: "completed", CreatedAt: store.Now()}
		records = append(records, store.Record{Kind: "skill-check-job", ID: id, Value: job})
	}
	if err := m.Store.PutMany(records...); err != nil {
		t.Fatal(err)
	}
}

func TestSkillDiagnosticRepeatedCanariesDoNotConsumeOrdinaryHistory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker policy")
	}
	m := testManager(t)
	f := newDiagnosticDocker()
	i := diagnosticEnvironment(t, m, f)
	pack := skillCheckSuite(t, m, "history-boundary")
	seedSkillDiagnosticOrdinaryHistory(t, m, pack, i, 999)
	for n := 0; n < 3; n++ {
		result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup", InstanceID: i.ID})
		if err != nil || result.Overall != "incomplete" {
			t.Fatal(result, err)
		}
		rows, err := m.Store.List("skill-check-job")
		if err != nil || len(rows) != 999 {
			t.Fatal("canary changed existing ordinary history", len(rows), err)
		}
	}
	req := SkillCheckRequest{Digest: pack.Digest, Action: "validate", InstanceID: i.ID, AcknowledgeExecution: true}
	job, err := m.StartSkillCheck(pack.ID, req)
	if err != nil {
		t.Fatal("canary consumed the final ordinary history slot", err)
	}
	skillCheckWait(t, m, job.ID)
	_, err = m.StartSkillCheck(pack.ID, req)
	appCode(t, err, "skill_check_history_full")
}

func TestSkillDiagnosticCleanupRetryDoesNotBlockFinalOrdinaryHistorySlot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker policy")
	}
	m := testManager(t)
	f := newDiagnosticDocker()
	f.cleanupErr = errors.New("fixture cleanup unavailable")
	i := diagnosticEnvironment(t, m, f)
	pack := skillCheckSuite(t, m, "cleanup-history-boundary")
	seedSkillDiagnosticOrdinaryHistory(t, m, pack, i, 999)
	result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup", InstanceID: i.ID})
	if err != nil || result.CleanupJobID == "" {
		t.Fatal(result, err)
	}
	req := SkillCheckRequest{Digest: pack.Digest, Action: "validate", InstanceID: i.ID, AcknowledgeExecution: true}
	_, err = m.StartSkillCheck(pack.ID, req)
	appCode(t, err, "skill_check_busy")
	f.cleanupErr = nil
	if job, err := m.CancelSkillCheck(result.CleanupJobID); err != nil || job.CleanupPending || job.CleanupError != "" {
		t.Fatal(job, err)
	}
	// A user may start a normal test immediately, without another diagnosis.
	job, err := m.StartSkillCheck(pack.ID, req)
	if err != nil {
		t.Fatal("cleaned canary occupied ordinary history", err)
	}
	skillCheckWait(t, m, job.ID)
	// The next explicit diagnosis retires the old recovery row and persists
	// resolved cleanup evidence before deleting its temporary job.
	t.Setenv("RUNDESK_SKILLSPECTOR_BIN", filepath.Join(t.TempDir(), "missing"))
	if _, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"}); err != nil {
		t.Fatal(err)
	}
	var gone SkillCheckJob
	if err := m.Store.Get("skill-check-job", result.CleanupJobID, &gone); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("resolved canary job survived retirement", gone, err)
	}
	var latest SkillDiagnostic
	if err := m.Store.Get("skill-diagnostic", diagnosticKey("skillup", i.ID), &latest); err != nil || latest.CleanupJobID != "" {
		t.Fatal("cleanup evidence was not retained", latest, err)
	}
	rows, err := m.Store.List("skill-check-job")
	if err != nil || len(rows) != 1000 {
		t.Fatal("ordinary history was removed", len(rows), err)
	}
}

func TestSkillDiagnosticRestartRecoveryRetiresOnlyConfirmedCanary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker policy")
	}
	m := testManager(t)
	f := newDiagnosticDocker()
	i := diagnosticEnvironment(t, m, f)
	id := store.ID()
	f.owner = id
	job := SkillCheckJob{ID: id, Action: "validate", InstanceID: i.ID, EnvironmentName: skillDiagnosticEnvironmentName, ContainerName: "rundesk-skillcheck-" + id, Status: "running", StartedAt: store.Now(), CleanupPending: true}
	latest := SkillDiagnostic{ID: id, Target: "skillup", InstanceID: i.ID, CleanupJobID: id, Overall: "failed", Checks: []SkillDiagnosticCheck{{ID: "version", Status: "ok"}, {ID: "protocol", Status: "ok"}, {ID: "cleanup", Status: "failed"}, {ID: "model", Status: "unknown"}}}
	if err := m.Store.PutMany(store.Record{Kind: "skill-check-job", ID: id, Value: job}, store.Record{Kind: "skill-diagnostic", ID: diagnosticKey("skillup", i.ID), Value: latest}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNDESK_SKILLSPECTOR_BIN", filepath.Join(t.TempDir(), "missing"))
	if _, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.Get("skill-check-job", id, &job); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("recovered canary remained ordinary history", job, err)
	}
	latest = SkillDiagnostic{}
	if err := m.Store.Get("skill-diagnostic", diagnosticKey("skillup", i.ID), &latest); err != nil || latest.CleanupJobID != "" || latest.Overall != "incomplete" {
		t.Fatal("restart cleanup evidence was lost", latest, err)
	}
}
func TestSkillDiagnosticCancelledBetweenStagesDoesNotCreateAnotherContainer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker policy")
	}
	m := testManager(t)
	f := newDiagnosticDocker()
	f.firstCleanup = make(chan struct{})
	f.releaseCleanup = make(chan struct{})
	i := diagnosticEnvironment(t, m, f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan SkillDiagnostic, 1)
	go func() {
		r, _ := m.DiagnoseSkillTools(ctx, SkillDiagnosticRequest{Target: "skillup", InstanceID: i.ID})
		done <- r
	}()
	select {
	case <-f.firstCleanup:
	case <-time.After(3 * time.Second):
		t.Fatal("version cleanup did not start")
	}
	cancel()
	close(f.releaseCleanup)
	select {
	case r := <-done:
		if r.Overall != "failed" {
			t.Fatal(r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled diagnostic did not finish")
	}
	if f.creates != 1 {
		t.Fatal("cancelled diagnostic created a second container", f.creates)
	}
}
func TestSkillDiagnosticCleanupFailureRetainsExecutionBarrierAndReadIsPassive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker policy")
	}
	m := testManager(t)
	f := newDiagnosticDocker()
	f.cleanupErr = errors.New("fixture cleanup unavailable")
	i := diagnosticEnvironment(t, m, f)
	result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup", InstanceID: i.ID})
	if err != nil || result.CleanupJobID == "" || result.Overall != "failed" {
		t.Fatal(result, err)
	}
	_, err = m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillup", InstanceID: i.ID})
	appCode(t, err, "skill_diagnostic_busy")
	before := len(f.calls)
	recent, err := m.SkillDiagnostics()
	if err != nil || len(recent) != 1 || len(f.calls) != before {
		t.Fatal("GET diagnostic performed a probe", recent, err)
	}
	f.cleanupErr = nil
	job, err := m.CancelSkillCheck(result.CleanupJobID)
	if err != nil || job.CleanupPending || job.CleanupError != "" {
		t.Fatal(job, err)
	}
	recent, err = m.SkillDiagnostics()
	if err != nil || recent[0].CleanupJobID != "" {
		t.Fatal(recent, err)
	}
}
func TestSkillDiagnosticContextAndManagerCloseCancelTrustedProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX scanner fixture")
	}
	marker := filepath.Join(t.TempDir(), "started")
	exe := filepath.Join(t.TempDir(), "scanner")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf started > "+diagnosticQuote(marker)+"\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNDESK_SKILLSPECTOR_BIN", exe)
	m := testManager(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("probe did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Manager.Close failed to cancel/wait diagnostic")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("diagnostic survived close")
	}
}
func TestSkillDiagnosticCanaryMatchesRealSkillUpValidation(t *testing.T) {
	exe := os.Getenv("RUNDESK_TEST_SKILLUP_BIN")
	if exe == "" {
		t.Skip("set trusted official CLI for opt-in canary validation")
	}
	source, err := makeSkillDiagnosticCanary()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(source))
	result, err := skillcheck.Validate(context.Background(), source, exe)
	if err != nil || result.Status != "success" || result.Cases != 1 {
		t.Fatal(result, err)
	}
}
func TestSkillDiagnosticLiveSkillSpector(t *testing.T) {
	exe := os.Getenv("RUNDESK_TEST_SKILLSPECTOR_BIN")
	if exe == "" {
		t.Skip("set trusted official scanner for opt-in canary validation")
	}
	t.Setenv("RUNDESK_SKILLSPECTOR_BIN", exe)
	m := testManager(t)
	result, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"})
	if err != nil || result.Overall != "ready" {
		t.Fatal(result, err)
	}
}
func TestSkillDiagnosticReadDoesNotInitializeOrProbeDocker(t *testing.T) {
	m := testManager(t)
	f := newDiagnosticDocker()
	m.docker = f
	result := SkillDiagnostic{ID: store.ID(), Target: "skillup", Overall: "failed", CheckedAt: store.Now(), Checks: []SkillDiagnosticCheck{{ID: "docker", Status: "failed"}}}
	if err := m.Store.Put("skill-diagnostic", "fixture", result); err != nil {
		t.Fatal(err)
	}
	got, err := m.SkillDiagnostics()
	if err != nil || len(got) != 1 || len(f.calls) != 0 || m.skillChecks != nil {
		t.Fatal(got, err)
	}
}

func TestSkillDiagnosticRoutesAreAdminOnlyAndGETRemainsPassive(t *testing.T) {
	m, _, memberToken, _, _, shared, _ := userFixture(t)
	_, appToken, err := m.CreateApplicationKey("news-members", KeyInput{Name: "No diagnostic administration", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, method := range []string{"GET", "POST"} {
			for _, token := range []string{memberToken, appToken} {
				response := appRequest(h, method, prefix+"/skill-center/diagnostics", `{"target":"skillspector"}`, token, "")
				if response.Code != 403 {
					t.Fatal("non-admin reached diagnostic route", method, prefix, response.Code, response.Body.String())
				}
			}
		}
	}
	f := newDiagnosticDocker()
	m.docker = f
	response := appRequest(h, "GET", "/api/v1/skill-center/diagnostics", "", userTestAdmin, "")
	if response.Code != 200 || len(f.calls) != 0 || m.skillChecks != nil {
		t.Fatal("passive GET initialized/probed execution", response.Code, response.Body.String(), f.calls)
	}
	response = appRequest(h, "POST", "/api/v1/skill-center/diagnostics", `{"target":"skillspector","command":"/bin/sh"}`, userTestAdmin, "")
	if response.Code != 400 {
		t.Fatal("diagnostic accepted an arbitrary executable field", response.Code, response.Body.String())
	}
}

func TestSkillDiagnosticDoesNotProbeWhileAnotherExecutionOwnsTheSlot(t *testing.T) {
	m := testManager(t)
	m.skillCheckMu.Lock()
	if err := m.initSkillChecksLocked(); err != nil {
		m.skillCheckMu.Unlock()
		t.Fatal(err)
	}
	id := store.ID()
	m.skillChecks.cancels[id] = func() {}
	m.skillCheckMu.Unlock()
	_, err := m.DiagnoseSkillTools(context.Background(), SkillDiagnosticRequest{Target: "skillspector"})
	appCode(t, err, "skill_diagnostic_busy")
	recent, err := m.SkillDiagnostics()
	if err != nil || len(recent) != 0 {
		t.Fatal("rejected diagnostic fabricated a result", recent, err)
	}
	m.skillCheckMu.Lock()
	delete(m.skillChecks.cancels, id)
	m.skillCheckMu.Unlock()
}
