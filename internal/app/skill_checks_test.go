package app

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/skillcatalog"
	"github.com/shengjuntu/rundesk/internal/skillcheck"
	"github.com/shengjuntu/rundesk/internal/store"
)

func skillCheckSuite(t *testing.T, m *Manager, revision string) SkillPack {
	t.Helper()
	bundle := skillCenterFixture()
	bundle.Revision = revision
	bundle.Assets[0].Data = append(bundle.Assets[0].Data, []byte("\nVersion "+revision+".\n")...)
	bundle.Assets = append(bundle.Assets, skillcatalog.Asset{Path: "evals/eval.yaml", Data: []byte("schema_version: v1alpha1\nname: research\ncase_dir: cases\n"), Mode: 0600}, skillcatalog.Asset{Path: "evals/cases/one.yaml", Data: []byte("id: one\nprompt: Summarize this fixed document.\n"), Mode: 0600})
	return skillCenterStage(t, m, bundle)
}

func skillCheckReportJSON(t *testing.T, status string) []byte {
	t.Helper()
	value := map[string]any{"skill_name": "research", "schema_version": "v1alpha1", "engine_name": "codex", "model_name": "provider/model", "requested_configuration": map[string]any{"role": "tested_agent", "engine": "codex", "protocol": "cli", "provider": "provider", "model": "model", "version": "fixture-1"}, "applied_configuration": map[string]any{"role": "tested_agent", "engine": "codex", "protocol": "cli", "provider": "provider", "model": "model", "version": "fixture-1"}, "case_results": []any{map[string]any{"case_id": "one", "title": "Fixed document", "status": status, "prompt": "Summarize this fixed document.", "grading": map[string]any{"status": status, "assertion_results": []any{map[string]any{"text": "Contains a source citation", "passed": status == "PASS", "evidence": "safe evidence"}}}}}}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func skillCheckReportArchive(t *testing.T, data []byte) string {
	t.Helper()
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	if err := w.WriteHeader(&tar.Header{Name: "result.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "report.tar")
	if err := os.WriteFile(name, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

type skillCheckControllerDocker struct {
	*skillCheckDockerFixture
	validationOutput string
	cleanupStarted   chan struct{}
	cleanupRelease   chan struct{}
}

func (f *skillCheckControllerDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "image":
		return json.Marshal([]any{map[string]any{"Id": "sha256:" + strings.Repeat("d", 64), "Os": "linux", "Architecture": runtime.GOARCH, "Config": map[string]any{"Volumes": map[string]any{}}}})
	case "version":
		return []byte(`{"Version":"fixture"}`), nil
	case "create":
		for i := range args {
			if args[i] == "--label" && i+1 < len(args) {
				f.mu.Lock()
				f.owner = strings.TrimPrefix(args[i+1], "io.rundesk.skillcheck=")
				f.mu.Unlock()
			}
		}
	case "start":
		if f.validationOutput != "" {
			return []byte(f.validationOutput), nil
		}
	case "rm":
		if f.cleanupStarted != nil {
			select {
			case <-f.cleanupStarted:
			default:
				close(f.cleanupStarted)
			}
			select {
			case <-f.cleanupRelease:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return f.skillCheckDockerFixture.Run(ctx, args...)
}

func skillCheckEnvironment(t *testing.T, m *Manager, fixture *skillCheckControllerDocker) Instance {
	t.Helper()
	i, err := m.CreateInstance(InstancePatch{Name: "Isolated test environment"})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.instances[i.ID].Execution = ExecutionSpec{Mode: "docker", Image: "rundesk-skillup:fixture"}
	i = *m.instances[i.ID]
	m.mu.Unlock()
	m.docker = fixture
	return i
}

func skillCheckWait(t *testing.T, m *Manager, id string) SkillCheckJob {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for {
		job, err := m.SkillCheckJob(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != "queued" && job.Status != "running" {
			return job
		}
		if time.Now().After(until) {
			t.Fatalf("check did not finish: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSkillCheckImportIsUnverifiedAndCannotChangeSafetyGate(t *testing.T) {
	m := testManager(t)
	pack := skillCheckSuite(t, m, "v1")
	before, _ := json.Marshal(pack.Reports)
	_, err := m.ImportSkillCheckReport(pack.ID, pack.Digest, skillCheckReportJSON(t, "PASS"), false)
	appCode(t, err, "skill_check_import_confirmation_required")
	_, err = m.ImportSkillCheckReport(pack.ID, "sha256:"+strings.Repeat("e", 64), skillCheckReportJSON(t, "PASS"), true)
	appCode(t, err, "skill_digest_mismatch")
	record, err := m.ImportSkillCheckReport(pack.ID, pack.Digest, skillCheckReportJSON(t, "PASS"), true)
	if err != nil || record.Source != "external_unverified" || record.Report.Status != "pass" || record.Report.SuiteDigest != "" || len(record.Report.Cases) != 1 {
		t.Fatal(record, err)
	}
	items, err := m.SkillCheckReports(pack.ID)
	if err != nil || len(items) != 1 || !items[0].CasesOmitted || len(items[0].Report.Cases) != 0 || items[0].Report.Summary.Pass != 1 {
		t.Fatal(items, err)
	}
	full, err := m.SkillCheckReport(record.ID)
	if err != nil || full.CasesOmitted || len(full.Report.Cases) != 1 {
		t.Fatal(full, err)
	}
	afterPack, _ := m.SkillPack(pack.ID)
	after, _ := json.Marshal(afterPack.Reports)
	if !bytes.Equal(before, after) {
		t.Fatal("quality import modified the independent security reports")
	}
	result, err := m.CompareSkillChecks(pack.ID, record.ID, record.ID)
	if err != nil || result.(map[string]any)["comparison"].(skillcheck.Comparison).Verdict != "incomplete" {
		t.Fatal("imported labels became a passing regression gate", result, err)
	}
}

func TestSkillCheckComparesDifferentSkillVersionsWithFixedInputs(t *testing.T) {
	m := testManager(t)
	before := skillCheckSuite(t, m, "v1")
	after := skillCheckSuite(t, m, "v2")
	makeRecord := func(pack SkillPack, status, suite, image string) SkillCheckReportRecord {
		r, err := skillcheck.ParseReport(skillCheckReportJSON(t, status))
		if err != nil {
			t.Fatal(err)
		}
		r.SuiteDigest = suite
		record := SkillCheckReportRecord{ID: store.ID(), PackID: pack.ID, Digest: pack.Digest, Source: "docker_run", SuiteDigest: suite, ImageID: image, Report: r, CreatedAt: store.Now()}
		m.skillCheckMu.Lock()
		err = m.saveSkillCheckReportLocked(record)
		m.skillCheckMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	suite, image := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	baseline := makeRecord(before, "FAIL", suite, image)
	candidate := makeRecord(after, "PASS", suite, image)
	got, err := m.CompareSkillChecks(after.ID, baseline.ID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	comparison := got.(map[string]any)["comparison"].(skillcheck.Comparison)
	if !comparison.Comparable || comparison.Verdict != "pass" || comparison.Summary.Fixed != 1 {
		t.Fatal(comparison)
	}
	for _, changed := range []SkillCheckReportRecord{makeRecord(after, "PASS", "sha256:"+strings.Repeat("c", 64), image), makeRecord(after, "PASS", suite, "sha256:"+strings.Repeat("c", 64))} {
		got, err = m.CompareSkillChecks(after.ID, baseline.ID, changed.ID)
		if err != nil {
			t.Fatal(err)
		}
		c := got.(map[string]any)["comparison"].(skillcheck.Comparison)
		if c.Verdict != "incomplete" || c.Comparable || c.Summary.Fixed != 0 || c.Summary.Regressed != 0 || c.Summary.Unknown != 1 || c.Cases[0].Change != "unknown" {
			t.Fatal("changed input/environment passed regression", got, err)
		}
	}
	// A custom judge identifier is data. Giving it a ':rule' suffix must not
	// turn its unverified implementation into a built-in regression gate.
	judge := makeRecord(after, "PASS", suite, image)
	judge.Report.Cases[0].Assertions = append(judge.Report.Cases[0].Assertions, skillcheck.Assertion{Text: "custom check", Passed: true, Source: "judge:custom-script:rule"})
	if err := m.Store.Put("skill-check-report", judge.ID, judge); err != nil {
		t.Fatal(err)
	}
	got, err = m.CompareSkillChecks(after.ID, baseline.ID, judge.ID)
	if err != nil {
		t.Fatal(err)
	}
	custom := got.(map[string]any)["comparison"].(skillcheck.Comparison)
	if custom.Comparable || custom.Verdict != "incomplete" || custom.Summary.Fixed != 0 || custom.Summary.Unknown != 1 {
		t.Fatal("custom judge ID impersonated a built-in rule", custom)
	}
	_, err = m.CompareSkillChecks(before.ID, baseline.ID, candidate.ID)
	appCode(t, err, "skill_check_scope_mismatch")
}

func TestSkillCheckFrozenInputsIncludeAuxiliaryJudgeScripts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker controller")
	}
	m := testManager(t)
	before := skillCheckSuite(t, m, "v1")
	_, assets, err := m.skillPackAssets(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	stage := func(revision, script string) SkillPack {
		bundle := skillcatalog.Bundle{Name: before.Name, SourceURL: before.SourceURL, Revision: revision}
		for _, file := range assets {
			bundle.Assets = append(bundle.Assets, skillcatalog.Asset{Path: file.Path, Data: file.Data, Mode: file.Mode})
		}
		bundle.Assets = append(bundle.Assets, skillcatalog.Asset{Path: "scripts/judge.py", Data: []byte(script), Mode: 0600})
		return skillCenterStage(t, m, bundle)
	}
	first, second := stage("judge-v1", "print('pass')\n"), stage("judge-v2", "print('fail')\n")
	f := &skillCheckControllerDocker{skillCheckDockerFixture: &skillCheckDockerFixture{containerID: strings.Repeat("e", 64)}, validationOutput: "✓ eval.yaml is valid (loaded 1 case(s))\n"}
	i := skillCheckEnvironment(t, m, f)
	jobs := []SkillCheckJob{}
	for _, pack := range []SkillPack{first, second} {
		job, err := m.StartSkillCheck(pack.ID, SkillCheckRequest{Digest: pack.Digest, Action: "validate", InstanceID: i.ID, AcknowledgeExecution: true})
		if err != nil {
			t.Fatal(err)
		}
		job = skillCheckWait(t, m, job.ID)
		if job.Status != "completed" {
			t.Fatal(job)
		}
		jobs = append(jobs, job)
	}
	if jobs[0].SuiteDigest == jobs[1].SuiteDigest {
		t.Fatal("evals-external Judge implementation change retained suite identity")
	}
}

func TestSkillCheckRequiresExplicitDockerExecutionAndFixedSnapshot(t *testing.T) {
	m := testManager(t)
	pack := skillCheckSuite(t, m, "v1")
	in := SkillCheckRequest{Digest: pack.Digest, Action: "run", InstanceID: DefaultInstance}
	_, err := m.StartSkillCheck(pack.ID, in)
	appCode(t, err, "skill_check_execution_confirmation_required")
	in.AcknowledgeExecution = true
	in.Digest = "wrong"
	_, err = m.StartSkillCheck(pack.ID, in)
	appCode(t, err, "skill_digest_mismatch")
	in.Digest = pack.Digest
	_, err = m.StartSkillCheck(pack.ID, in)
	if runtime.GOOS == "linux" {
		appCode(t, err, "skill_check_environment_required")
	} else {
		appCode(t, err, "skill_check_unavailable")
	}
	plain := skillCenterStage(t, m, skillCenterFixture())
	in.Digest = plain.Digest
	_, err = m.StartSkillCheck(plain.ID, in)
	appCode(t, err, "skill_check_suite_missing")
	jobs, err := m.SkillCheckJobs(pack.ID)
	if err != nil || len(jobs) != 0 {
		t.Fatal("rejected requests queued execution", jobs, err)
	}
}

func TestSkillCheckDockerJobsPreserveValidationAndNonzeroExit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker controller")
	}
	for _, tc := range []struct {
		action, output, wantJob, wantReport string
		exit                                int
	}{
		{"validate", "✓ eval.yaml is valid (loaded 1 case(s))\n", "completed", "", 0},
		{"validate", "a process exited without real validation", "failed", "", 0},
		{"run", "", "completed", "pass", 0},
		{"run", "", "completed", "incomplete", 1},
	} {
		t.Run(tc.action+tc.wantJob+tc.wantReport, func(t *testing.T) {
			m := testManager(t)
			pack := skillCheckSuite(t, m, "v1")
			f := &skillCheckControllerDocker{skillCheckDockerFixture: &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), archive: skillCheckReportArchive(t, skillCheckReportJSON(t, "PASS")), startCode: tc.exit}, validationOutput: tc.output}
			i := skillCheckEnvironment(t, m, f)
			job, err := m.StartSkillCheck(pack.ID, SkillCheckRequest{Digest: pack.Digest, Action: tc.action, InstanceID: i.ID, AcknowledgeExecution: true})
			if err != nil {
				t.Fatal(err)
			}
			job = skillCheckWait(t, m, job.ID)
			if job.Status != tc.wantJob || job.ImageID != "sha256:"+strings.Repeat("d", 64) || job.SuiteDigest == "" || job.CleanupError != "" {
				t.Fatal(job)
			}
			if tc.wantReport != "" {
				r, err := m.SkillCheckReport(job.ReportID)
				if err != nil || r.Report.Status != tc.wantReport || r.Source != "docker_run" || r.Report.SuiteDigest != job.SuiteDigest || r.Digest != pack.Digest || r.ImageID != job.ImageID {
					t.Fatal(r, err)
				}
			} else if tc.wantJob == "completed" && (job.Validation == nil || job.Validation.Cases != 1) {
				t.Fatal("validation exited without nonempty case evidence", job)
			}
		})
	}
}

func TestSkillCheckCleanupFailureBlocksExecutionUntilExplicitRetry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker controller")
	}
	m := testManager(t)
	pack := skillCheckSuite(t, m, "v1")
	f := &skillCheckControllerDocker{skillCheckDockerFixture: &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), archive: skillCheckReportArchive(t, skillCheckReportJSON(t, "PASS")), cleanupErr: errors.New("fixture daemon unavailable")}}
	i := skillCheckEnvironment(t, m, f)
	in := SkillCheckRequest{Digest: pack.Digest, Action: "run", InstanceID: i.ID, AcknowledgeExecution: true}
	job, err := m.StartSkillCheck(pack.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	job = skillCheckWait(t, m, job.ID)
	if job.Status != "failed" || job.CleanupError == "" {
		t.Fatal(job)
	}
	r, err := m.SkillCheckReport(job.ReportID)
	if err != nil || r.Report.Status != "incomplete" {
		t.Fatal(r, err)
	}
	_, err = m.StartSkillCheck(pack.ID, in)
	appCode(t, err, "skill_check_running")
	f.mu.Lock()
	f.cleanupErr = nil
	f.mu.Unlock()
	job, err = m.CancelSkillCheck(job.ID)
	if err != nil || job.CleanupError != "" {
		t.Fatal(job, err)
	}
	m.skillCheckMu.Lock()
	remaining := len(m.skillChecks.cancels)
	m.skillCheckMu.Unlock()
	if remaining != 0 {
		t.Fatal("confirmed cleanup kept execution blocked")
	}
}

func TestSkillCheckRestartDoesNotRerunQueuedWork(t *testing.T) {
	m := testManager(t)
	pack := skillCheckSuite(t, m, "v1")
	id := store.ID()
	job := SkillCheckJob{ID: id, PackID: pack.ID, Digest: pack.Digest, Action: "run", ContainerName: "rundesk-skillcheck-" + id, Status: "queued", CreatedAt: store.Now(), ExitCode: -1}
	if err := m.Store.Put("skill-check-job", id, job); err != nil {
		t.Fatal(err)
	}
	got, err := m.SkillCheckJob(id)
	if err != nil || got.Status != "interrupted" || got.ReportID != "" || got.FinishedAt == "" {
		t.Fatal(got, err)
	}
}

func TestSkillCheckCanceledJobStaysPendingUntilContainerCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker controller")
	}
	m := testManager(t)
	pack := skillCheckSuite(t, m, "v1")
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f := &skillCheckControllerDocker{skillCheckDockerFixture: &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), started: make(chan struct{}), block: make(chan struct{})}, cleanupStarted: make(chan struct{}), cleanupRelease: release}
	i := skillCheckEnvironment(t, m, f)
	in := SkillCheckRequest{Digest: pack.Digest, Action: "run", InstanceID: i.ID, AcknowledgeExecution: true}
	job, err := m.StartSkillCheck(pack.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.started:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not start")
	}
	job, err = m.CancelSkillCheck(job.ID)
	if err != nil || job.Status != "canceled" || !job.CleanupPending {
		t.Fatal(job, err)
	}
	select {
	case <-f.cleanupStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not begin cleanup")
	}
	job, err = m.SkillCheckJob(job.ID)
	if err != nil || !job.CleanupPending {
		t.Fatal("canceled state lost cleanup barrier", job, err)
	}
	_, err = m.StartSkillCheck(pack.ID, in)
	appCode(t, err, "skill_check_running")
	close(release)
	until := time.Now().Add(3 * time.Second)
	for {
		job, err = m.SkillCheckJob(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !job.CleanupPending {
			break
		}
		if time.Now().After(until) {
			t.Fatal("cleanup did not release barrier", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.ReportID != "" || job.CleanupError != "" {
		t.Fatal("canceled job acquired a passing report", job)
	}
}

func TestSkillCheckRestartRetainsFailedCleanupBarrier(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Docker controller")
	}
	m := testManager(t)
	pack := skillCheckSuite(t, m, "v1")
	id := store.ID()
	f := &skillCheckControllerDocker{skillCheckDockerFixture: &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), owner: id, cleanupErr: errors.New("daemon still unavailable")}}
	i := skillCheckEnvironment(t, m, f)
	job := SkillCheckJob{ID: id, PackID: pack.ID, Digest: pack.Digest, Action: "run", InstanceID: i.ID, ContainerName: "rundesk-skillcheck-" + id, Status: "failed", StartedAt: store.Now(), CleanupPending: true, CleanupError: "prior cleanup not confirmed", CreatedAt: store.Now(), ExitCode: 1}
	if err := m.Store.Put("skill-check-job", id, job); err != nil {
		t.Fatal(err)
	}
	got, err := m.SkillCheckJob(id)
	if err != nil || got.Status != "interrupted" || !got.CleanupPending || got.CleanupError == "" {
		t.Fatal("restart erased cleanup failure", got, err)
	}
	_, err = m.StartSkillCheck(pack.ID, SkillCheckRequest{Digest: pack.Digest, Action: "run", InstanceID: i.ID, AcknowledgeExecution: true})
	appCode(t, err, "skill_check_running")
	f.mu.Lock()
	f.cleanupErr = nil
	f.mu.Unlock()
	got, err = m.CancelSkillCheck(id)
	if err != nil || got.CleanupPending || got.CleanupError != "" {
		t.Fatal(got, err)
	}
	for _, args := range f.calls {
		if args[0] == "create" || args[0] == "start" {
			t.Fatal("restart reran an old job", args)
		}
	}
}

func TestSkillCheckRoutesRemainAdministratorOnly(t *testing.T) {
	m, user, memberToken, _, _, shared, _ := userFixture(t)
	_, appToken, err := m.CreateApplicationKey("news-members", KeyInput{Name: "No check administration", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	for _, route := range []struct{ method, path string }{
		{"GET", "/check-engines"}, {"GET", "/packs/aaaaaaaaaaaaaaaaaaaaaaaa/checks"}, {"POST", "/packs/aaaaaaaaaaaaaaaaaaaaaaaa/checks"},
		{"GET", "/check-jobs/aaaaaaaaaaaaaaaaaaaaaaaa"}, {"POST", "/check-jobs/aaaaaaaaaaaaaaaaaaaaaaaa/cancel"},
		{"GET", "/check-reports"}, {"GET", "/check-reports/aaaaaaaaaaaaaaaaaaaaaaaa"}, {"GET", "/packs/aaaaaaaaaaaaaaaaaaaaaaaa/check-reports"},
		{"POST", "/packs/aaaaaaaaaaaaaaaaaaaaaaaa/check-reports"}, {"GET", "/packs/aaaaaaaaaaaaaaaaaaaaaaaa/check-compare"},
	} {
		for _, prefix := range []string{"/api", "/api/v1"} {
			for _, token := range []string{memberToken, appToken} {
				r := appRequest(h, route.method, prefix+"/skill-center"+route.path, `{}`, token, "")
				if r.Code != 403 {
					t.Fatal(route, r.Code, r.Body.String())
				}
			}
		}
	}
	r := appRequest(h, "GET", "/api/v1/member/"+user.Grants[0].ID+"/skill-center/check-reports", "", memberToken, "")
	if r.Code != 403 {
		t.Fatal("member proxy escaped check boundary", r.Code)
	}
}
