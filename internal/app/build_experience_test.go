package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func experienceInstallFixture(t *testing.T, m *Manager) BuildExperienceInput {
	t.Helper()
	failed := buildExperienceInstallEvidence{ID: store.ID(), Status: "failed", Platform: runtime.GOOS + "/" + runtime.GOARCH}
	failed.Input.Version = "0.157.1"
	failed.Result.Error = "403 Forbidden downloading pinned package"
	failed.Result.Log = "registry request returned 403; password=hidden-password"
	success := buildExperienceInstallEvidence{ID: store.ID(), Status: "succeeded", Platform: failed.Platform}
	success.Input.Version, success.Result.Version = failed.Input.Version, failed.Input.Version
	success.Result.OK, success.Result.Method = true, "native"
	success.Result.Registry, success.Result.Integrity = "https://registry.npmjs.org", "sha512-fixture-integrity"
	for _, v := range []buildExperienceInstallEvidence{failed, success} {
		if err := m.Store.Put("codex-install", v.ID, v); err != nil {
			t.Fatal(err)
		}
	}
	return BuildExperienceInput{Kind: "codex-install", FailedID: failed.ID, SuccessID: success.ID, Summary: "Pinned package download was blocked", Changes: "Retried the same version using the configured registry"}
}

func TestBuildExperienceInstallVerificationAndImmutableVersions(t *testing.T) {
	m := testManager(t)
	in := experienceInstallFixture(t, m)
	first, err := m.RecordBuildExperience(in)
	if err != nil || first.Status != "verified" || !first.Validation.InstallVersionVerified || first.Version != 1 || !first.CurrentApplicable {
		t.Fatal(first, err)
	}
	encoded, _ := json.Marshal(first)
	if strings.Contains(string(encoded), "hidden-password") {
		t.Fatal("credential copied into reusable experience")
	}
	retry, err := m.RecordBuildExperience(in)
	if err != nil || retry.ID != first.ID || retry.Version != 1 {
		t.Fatal("retry created a new experience", retry, err)
	}
	in.Changes += "; download integrity and binary version both checked"
	second, err := m.RecordBuildExperience(in)
	if err != nil || second.ID == first.ID || second.FamilyID != first.FamilyID || second.Version != 2 || second.PreviousID != first.ID {
		t.Fatal(second, err)
	}
	rows, err := m.BuildExperiences("codex-install", "0.157.1")
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	var original BuildExperience
	if err = m.Store.Get("build-experience", first.ID, &original); err != nil || original.Changes != first.Changes || original.Version != 1 {
		t.Fatal("old evidence was overwritten", original, err)
	}
}

func TestBuildExperienceInstallScopeAndActualSuccess(t *testing.T) {
	for _, scenario := range []string{"different-version", "different-platform", "missing-platform", "missing-integrity", "no-version-check", "running-source"} {
		t.Run(scenario, func(t *testing.T) {
			m := testManager(t)
			in := experienceInstallFixture(t, m)
			var success buildExperienceInstallEvidence
			if err := m.Store.Get("codex-install", in.SuccessID, &success); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "different-version":
				success.Input.Version, success.Result.Version = "0.158.0", "0.158.0"
			case "different-platform":
				success.Platform = "another/arch"
			case "missing-platform":
				success.Platform = ""
			case "missing-integrity":
				success.Result.Integrity = ""
			case "no-version-check":
				success.Result.OK = false
			case "running-source":
				var failed buildExperienceInstallEvidence
				_ = m.Store.Get("codex-install", in.FailedID, &failed)
				failed.Status = "running"
				_ = m.Store.Put("codex-install", failed.ID, failed)
			}
			if err := m.Store.Put("codex-install", success.ID, success); err != nil {
				t.Fatal(err)
			}
			v, err := m.RecordBuildExperience(in)
			if scenario == "no-version-check" || scenario == "running-source" {
				if err == nil {
					t.Fatal("unverified task was accepted", v)
				}
			} else if err != nil || v.Status != "candidate" || v.CurrentApplicable || len(v.Warnings) == 0 {
				t.Fatal(v, err)
			}
		})
	}
}

func experienceImageFixture(t *testing.T, m *Manager) (BuildExperienceInput, ImageBuild, ImageBuild) {
	t.Helper()
	smoke := &RecipeSmokeSpec{Command: []string{"tool", "--offline-check"}, TimeoutSeconds: 30, ExpectedFiles: []string{"/tmp/result.json"}}
	failed := ImageBuild{ID: store.ID(), Scope: "recipe", Source: "build", Status: "failed", Dockerfile: "Dockerfile", PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH, CodexVersion: "0.157.1", RecipeID: "builtin-news2douyin", RecipeRevision: 1, RecipeSHA: strings.Repeat("a", 64), ProfileID: "news2douyin", RecipeSmoke: smoke, Error: "wheel archive contained a duplicate file", ContextSHA256: strings.Repeat("b", 64)}
	success := failed
	success.ID, success.Status, success.Error = store.ID(), "succeeded", ""
	success.ImageID, success.ContextSHA256 = "sha256:"+strings.Repeat("c", 64), strings.Repeat("d", 64)
	success.RebuildOf = failed.ID
	for _, v := range []ImageBuild{failed, success} {
		if err := m.Store.Put("image-build", v.ID, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(m.buildDir(failed.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.buildDir(failed.ID), "build.log"), []byte("ValueError: repeated force-include: agent_reach/guides/setup-exa.md\nAuthorization: Bearer private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	in := BuildExperienceInput{Kind: "image-build", FailedID: failed.ID, SuccessID: success.ID, Summary: "Upstream wheel packaging included resources twice", Changes: "Removed only the duplicate force-include entries for the pinned upstream commit"}
	for _, kind := range []string{"offline", "smoke"} {
		check := ImageBuild{ID: store.ID(), Scope: "image-test", Status: "succeeded", TestKind: kind, SourceBuildID: success.ID, ImageID: success.ImageID, ContextSHA256: success.ContextSHA256, RecipeSHA: success.RecipeSHA, RecipeSmoke: success.RecipeSmoke, PlatformOS: success.PlatformOS, PlatformArch: success.PlatformArch}
		if err := m.Store.Put("image-build", check.ID, check); err != nil {
			t.Fatal(err)
		}
		in.VerificationIDs = append(in.VerificationIDs, check.ID)
	}
	return in, failed, success
}

func TestBuildExperienceImageRequiresIndependentFrozenChecks(t *testing.T) {
	m := testManager(t)
	in, _, _ := experienceImageFixture(t, m)
	v, err := m.RecordBuildExperience(in)
	if err != nil || v.Status != "verified" || !v.Validation.Offline || !v.Validation.Smoke {
		t.Fatal(v, err)
	}
	if strings.Contains(v.Failure.LogExcerpt, "private-value") {
		t.Fatal("credential leaked in reusable log")
	}
	in.VerificationIDs[0], in.VerificationIDs[1] = in.VerificationIDs[1], in.VerificationIDs[0]
	retry, err := m.RecordBuildExperience(in)
	if err != nil || retry.ID != v.ID {
		t.Fatal("test argument order duplicated experience", retry, err)
	}
	in.VerificationIDs = nil
	candidate, err := m.RecordBuildExperience(in)
	if err != nil || candidate.Status != "candidate" || candidate.Version != 2 || candidate.PreviousID != v.ID {
		t.Fatal("build success without tool checks was promoted", candidate, err)
	}
}

func TestBuildExperienceRejectsOtherBuildChecksForSameImage(t *testing.T) {
	m := testManager(t)
	in, _, success := experienceImageFixture(t, m)
	var check ImageBuild
	_ = m.Store.Get("image-build", in.VerificationIDs[0], &check)
	check.SourceBuildID = store.ID() // Same image may come from several contexts.
	_ = m.Store.Put("image-build", check.ID, check)
	if v, err := m.RecordBuildExperience(in); err == nil {
		t.Fatal("foreign build's check was accepted", success, v)
	}
}

func TestBuildExperienceChangedAcceptanceAndLineageRemainCandidates(t *testing.T) {
	for _, scenario := range []string{"weakened-smoke", "unrelated-recipe", "different-platform", "different-version", "missing-platform"} {
		t.Run(scenario, func(t *testing.T) {
			m := testManager(t)
			in, _, success := experienceImageFixture(t, m)
			switch scenario {
			case "weakened-smoke":
				success.RecipeSmoke = &RecipeSmokeSpec{Command: []string{"true"}, TimeoutSeconds: 30}
			case "unrelated-recipe":
				success.ProfileID, success.RecipeID, success.RebuildOf = "unrelated", "other", ""
			case "different-platform":
				success.PlatformArch = "other"
			case "different-version":
				success.CodexVersion = "0.158.0"
			case "missing-platform":
				success.PlatformOS = ""
			}
			_ = m.Store.Put("image-build", success.ID, success)
			for _, id := range in.VerificationIDs {
				var check ImageBuild
				_ = m.Store.Get("image-build", id, &check)
				check.PlatformOS, check.PlatformArch, check.RecipeSmoke = success.PlatformOS, success.PlatformArch, success.RecipeSmoke
				_ = m.Store.Put("image-build", id, check)
			}
			v, err := m.RecordBuildExperience(in)
			if err != nil || v.Status != "candidate" || len(v.Warnings) == 0 {
				t.Fatal(v, err)
			}
		})
	}
}

func TestBuildExperienceCustomRepairLineageAndHostApplicability(t *testing.T) {
	m := testManager(t)
	in, failed, success := experienceImageFixture(t, m)
	success.ProfileID, success.RecipeID = "", store.ID()
	_ = m.Store.Put("image-build", success.ID, success)
	v, err := m.RecordBuildExperience(in)
	if err != nil || v.Status != "verified" || v.Scope.ProfileID != failed.ProfileID {
		t.Fatal("explicit repair chain should preserve source scope", v, err)
	}
	v.ID, v.Scope.OS = store.ID(), "another-os"
	_ = m.Store.Put("build-experience", v.ID, v)
	rows, err := m.BuildExperiences("image-build", "another-os")
	if err != nil || len(rows) != 1 || rows[0].CurrentApplicable {
		t.Fatal("foreign platform suggested as current", rows, err)
	}
}

func TestBuildExperienceRoutesRejectClaimedValidation(t *testing.T) {
	m := testManager(t)
	in := experienceInstallFixture(t, m)
	h := http.NewServeMux()
	(&Server{Manager: m}).setupExperienceRoutes(h)
	data, _ := json.Marshal(in)
	data = append(data[:len(data)-1], []byte(",\"passed\":true}")...)
	r := httptest.NewRequest("POST", "/api/setup/experiences", strings.NewReader(string(data)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unrecognized pass flag accepted", w.Code, w.Body.String())
	}
}

func experienceWriteContext(t *testing.T, m *Manager, v *ImageBuild, files map[string]string) {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for name, contents := range files {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data.Bytes())
	v.ContextSHA256 = hex.EncodeToString(sum[:])
	if err := os.MkdirAll(m.buildDir(v.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.buildDir(v.ID), "context.zip"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.Put("image-build", v.ID, v); err != nil {
		t.Fatal(err)
	}
}

func TestBuildExperienceFrozenPatchAndAutomaticLearning(t *testing.T) {
	m := testManager(t)
	in, failed, success := experienceImageFixture(t, m)
	experienceWriteContext(t, m, &failed, map[string]string{"Dockerfile": "FROM python:3.12\nRUN pip install https://example.invalid/archive.zip\n", "requirements.txt": "tool @ https://example.invalid/archive.zip", "obsolete.txt": "old"})
	experienceWriteContext(t, m, &success, map[string]string{"Dockerfile": "FROM python:3.12\nCOPY vendor /opt/vendor\nRUN pip install /opt/vendor/tool\n", "requirements.txt": "/opt/vendor/tool", "vendor/tool/pyproject.toml": "[project]\nname='tool'"})
	for _, id := range in.VerificationIDs {
		var test ImageBuild
		_ = m.Store.Get("image-build", id, &test)
		test.ContextSHA256 = success.ContextSHA256
		if test.TestKind == "smoke" {
			test.Status = "running"
		}
		_ = m.Store.Put("image-build", id, test)
	}
	candidate, err := m.AutoRecordBuildRepair(success.ID)
	if err != nil || candidate.Status != "candidate" || candidate.Patch == nil || !candidate.Patch.Available || candidate.Patch.ChangedFiles != 4 || !strings.Contains(candidate.Patch.DockerfileDiff, "+COPY vendor") {
		t.Fatal(candidate, err)
	}
	var smokeID string
	for _, id := range in.VerificationIDs {
		var test ImageBuild
		_ = m.Store.Get("image-build", id, &test)
		if test.TestKind == "smoke" {
			test.Status, smokeID = "succeeded", test.ID
			_ = m.Store.Put("image-build", id, test)
		}
	}
	verified, err := m.AutoRecordBuildRepair(smokeID)
	if err != nil || verified.Status != "verified" || verified.Version != 2 || verified.PreviousID != candidate.ID || len(verified.Patch.Files) != 4 {
		t.Fatal(verified, err)
	}
	for _, file := range verified.Patch.Files {
		if file.Path == "vendor/tool/pyproject.toml" && (file.Change != "added" || file.BeforeSHA256 != "" || len(file.AfterSHA256) != 64) {
			t.Fatal(file)
		}
	}
	if _, err := m.AutoRecordBuildRepair(smokeID); err != nil {
		t.Fatal(err)
	}
	rows, _ := m.BuildExperiences("image-build", "")
	if len(rows) != 2 {
		t.Fatal("automatic polling duplicated immutable versions", rows)
	}
	if err := os.WriteFile(filepath.Join(m.buildDir(success.ID), "context.zip"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	patch := m.experienceContextPatch(failed, success)
	if patch.Available || patch.Error == "" {
		t.Fatal("modified frozen bytes were used as repair evidence", patch)
	}
}
