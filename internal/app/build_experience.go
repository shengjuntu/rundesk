package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/shengjuntu/rundesk/internal/store"
)

// BuildExperienceInput contains an explanation and references, never a claimed
// score. Only completed RunDesk records determine the validation status. The
// explanation remains an observation, not a proven diagnosis or executable rule.
type BuildExperienceInput struct {
	Kind            string   `json:"kind"`
	FailedID        string   `json:"failedId"`
	SuccessID       string   `json:"successId"`
	VerificationIDs []string `json:"verificationIds,omitempty"`
	Summary         string   `json:"summary"`
	Changes         string   `json:"changes"`
}

type BuildExperienceScope struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Version        string `json:"version"`
	ProfileID      string `json:"profileId,omitempty"`
	ProfileVersion string `json:"profileVersion,omitempty"`
	RecipeID       string `json:"recipeId,omitempty"`
	RecipeRevision int    `json:"recipeRevision,omitempty"`
	RecipeSHA      string `json:"recipeSha,omitempty"`
}

type BuildExperienceEvidence struct {
	ID              string                `json:"id"`
	Status          string                `json:"status"`
	Error           string                `json:"error,omitempty"`
	LogExcerpt      string                `json:"logExcerpt,omitempty"`
	Version         string                `json:"version,omitempty"`
	Method          string                `json:"method,omitempty"`
	Registry        string                `json:"registry,omitempty"`
	Integrity       string                `json:"integrity,omitempty"`
	PackageVersion  string                `json:"packageVersion,omitempty"`
	ImageID         string                `json:"imageId,omitempty"`
	ContextSHA256   string                `json:"contextSha256,omitempty"`
	RecipeSHA       string                `json:"recipeSha,omitempty"`
	Sources         *BuildSourceSnapshot  `json:"sources,omitempty"`
	RuntimeIdentity *RuntimeImageIdentity `json:"runtimeIdentity,omitempty"`
	CodexRuntime    *BuildRuntimeSnapshot `json:"codexRuntime,omitempty"`
}

type BuildExperienceValidation struct {
	InstallVersionVerified bool `json:"installVersionVerified"`
	Offline                bool `json:"offline"`
	Smoke                  bool `json:"smoke"`
}

type BuildExperienceFileChange struct {
	Path         string `json:"path"`
	Change       string `json:"change"`
	BeforeSHA256 string `json:"beforeSha256,omitempty"`
	AfterSHA256  string `json:"afterSha256,omitempty"`
}

type BuildExperiencePatch struct {
	Available      bool                        `json:"available"`
	ChangedFiles   int                         `json:"changedFiles"`
	Files          []BuildExperienceFileChange `json:"files"`
	Truncated      bool                        `json:"truncated"`
	DockerfileDiff string                      `json:"dockerfileDiff,omitempty"`
	Error          string                      `json:"error,omitempty"`
}

// An experience is immutable. A later verification or explanation creates a
// new family version. It never edits SKILL.md, the source catalog or a recipe.
// CurrentApplicable is a read-time host/platform filter, not a general promise
// that the recorded repair applies to another task or dependency version.
type BuildExperience struct {
	Schema             int                       `json:"schema"`
	ID                 string                    `json:"id"`
	FamilyID           string                    `json:"familyId"`
	Version            int                       `json:"version"`
	PreviousID         string                    `json:"previousId,omitempty"`
	Kind               string                    `json:"kind"`
	Status             string                    `json:"status"`
	CreatedAt          string                    `json:"createdAt"`
	FailureFingerprint string                    `json:"failureFingerprint"`
	Summary            string                    `json:"summary"`
	Changes            string                    `json:"changes"`
	Scope              BuildExperienceScope      `json:"scope"`
	Failure            BuildExperienceEvidence   `json:"failure"`
	Success            BuildExperienceEvidence   `json:"success"`
	VerificationIDs    []string                  `json:"verificationIds"`
	Validation         BuildExperienceValidation `json:"validation"`
	Patch              *BuildExperiencePatch     `json:"patch,omitempty"`
	Warnings           []string                  `json:"warnings"`
	CurrentApplicable  bool                      `json:"currentApplicable"`
}

var buildExperienceMu sync.Mutex
var buildExperienceCapability = regexp.MustCompile(`rd-(?:build|setup)-[a-f0-9]{64}`)

func buildExperienceDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func buildExperienceText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	lines := strings.Split(value, "\n")
	for n, line := range lines {
		if len(line) > buildLogRedactionLineLimit {
			lines[n] = "[oversized line omitted]"
		} else {
			lines[n] = string(redactBuildLogLine([]byte(line)))
		}
	}
	value = buildExperienceCapability.ReplaceAllString(diagnosticText(strings.Join(lines, "\n")), "[redacted]")
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit]) + "\n[truncated]"
	}
	return value
}

func experiencePlatform(platform string) (string, string) {
	parts := strings.Split(platform, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// This deliberately reads only the persisted installation evidence needed for
// learning. No process is executed and no live credential or executable path
// is included in a cross-task experience.
type buildExperienceInstallEvidence struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Platform string `json:"platform"`
	Input    struct {
		Version string `json:"version"`
		Method  string `json:"method"`
	} `json:"input"`
	Result struct {
		OK             bool   `json:"ok"`
		Version        string `json:"version"`
		Log            string `json:"log"`
		Error          string `json:"error"`
		Method         string `json:"method"`
		Registry       string `json:"registry"`
		Integrity      string `json:"integrity"`
		PackageVersion string `json:"packageVersion"`
	} `json:"result"`
}

func (m *Manager) experienceInstall(id string) (buildExperienceInstallEvidence, error) {
	var v buildExperienceInstallEvidence
	err := m.Store.Get("codex-install", id, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return v, failure(404, "install_not_found", "安装记录不存在")
	}
	return v, err
}

func installExperienceSnapshot(v buildExperienceInstallEvidence) BuildExperienceEvidence {
	return BuildExperienceEvidence{ID: v.ID, Status: v.Status, Error: buildExperienceText(v.Result.Error, 2048), LogExcerpt: buildExperienceText(v.Result.Log, 4096), Version: v.Input.Version, Method: v.Result.Method, Registry: buildExperienceText(v.Result.Registry, 2048), Integrity: v.Result.Integrity, PackageVersion: v.Result.PackageVersion}
}

func (m *Manager) installExperience(in BuildExperienceInput, out *BuildExperience) error {
	failed, err := m.experienceInstall(in.FailedID)
	if err != nil {
		return err
	}
	success, err := m.experienceInstall(in.SuccessID)
	if err != nil {
		return err
	}
	if failed.Status != "failed" && failed.Status != "interrupted" {
		return failure(409, "experience_failure_required", "来源必须是已失败或已中断的安装")
	}
	if success.Status != "succeeded" || !success.Result.OK {
		return failure(409, "experience_success_required", "修复必须引用已成功并通过版本核对的安装")
	}
	out.Failure, out.Success = installExperienceSnapshot(failed), installExperienceSnapshot(success)
	out.Scope.OS, out.Scope.Arch = experiencePlatform(failed.Platform)
	out.Scope.Version = failed.Input.Version
	if out.Failure.Error == "" && out.Failure.LogExcerpt == "" {
		out.Warnings = append(out.Warnings, "失败记录缺少错误或日志证据；仅保存为候选。")
	}
	if failed.Platform == "" || success.Platform == "" || out.Scope.OS == "" || failed.Platform != success.Platform {
		out.Warnings = append(out.Warnings, "失败与成功安装的平台证据缺失或不同；仅保存为候选。")
	}
	if failed.Input.Version == "" || failed.Input.Version != success.Input.Version || success.Result.Version != success.Input.Version {
		out.Warnings = append(out.Warnings, "失败与成功安装未固定为同一 Codex 版本；不能作为该版本的已验证修复。")
	}
	if success.Result.Method == "native" && success.Result.Integrity == "" {
		out.Warnings = append(out.Warnings, "原生安装缺少下载完整性证据；仅保存为候选。")
	}
	if len(in.VerificationIDs) != 0 {
		return failure(400, "experience_verification_invalid", "安装经验使用安装任务自带版本验证，不接受镜像测试编号")
	}
	out.Validation.InstallVersionVerified = len(out.Warnings) == 0
	return nil
}

func imageExperienceSnapshot(v ImageBuild) BuildExperienceEvidence {
	return BuildExperienceEvidence{ID: v.ID, Status: v.Status, Error: buildExperienceText(v.Error, 2048), Version: v.CodexVersion, ImageID: v.ImageID, ContextSHA256: v.ContextSHA256, RecipeSHA: v.RecipeSHA, Sources: v.BuildSources, RuntimeIdentity: v.RuntimeIdentity, CodexRuntime: v.CodexRuntime}
}

func (m *Manager) experienceBuildLog(id string) string {
	f, err := os.Open(filepath.Join(m.buildDir(id), "build.log"))
	if err != nil {
		return ""
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return ""
	}
	const tail = 16 << 10
	if stat.Size() > tail {
		if _, err = f.Seek(stat.Size()-tail, io.SeekStart); err != nil {
			return ""
		}
	}
	data, _ := io.ReadAll(io.LimitReader(f, tail))
	return buildExperienceText(string(data), 4096)
}

func (m *Manager) experienceBuildLineage(failed, success ImageBuild) bool {
	if failed.ProfileID != "" && failed.ProfileID == success.ProfileID {
		return true
	}
	if failed.RecipeID != "" && failed.RecipeID == success.RecipeID {
		return true
	}
	// A copied/custom repair may legitimately have a new recipe ID. Its source
	// must have been recorded by RunDesk, not supplied as an experience claim.
	seen := map[string]bool{success.ID: true}
	for n := 0; n < 16 && success.RebuildOf != ""; n++ {
		if success.RebuildOf == failed.ID {
			return true
		}
		if seen[success.RebuildOf] {
			return false
		}
		seen[success.RebuildOf] = true
		var err error
		success, err = m.BuildCenterBuild(success.RebuildOf)
		if err != nil {
			return false
		}
	}
	return false
}

func (m *Manager) experienceContextFiles(v ImageBuild) (map[string]string, string, error) {
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(v.ID), "context.zip"), v.ContextSHA256)
	if err != nil {
		return nil, "", err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	if len(z.File) > 4000 {
		return nil, "", fmt.Errorf("frozen context has too many files")
	}
	files, dockerfile, remaining := map[string]string{}, "", int64(buildContextLimit)
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !safeBuildPath(f.Name) || !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(remaining) {
			return nil, "", fmt.Errorf("invalid frozen context entry")
		}
		if _, exists := files[f.Name]; exists {
			return nil, "", fmt.Errorf("duplicate frozen context entry")
		}
		r, err := f.Open()
		if err != nil {
			return nil, "", err
		}
		h := sha256.New()
		var body bytes.Buffer
		var dst io.Writer = h
		if f.Name == v.Dockerfile && f.UncompressedSize64 <= 64<<10 {
			dst = io.MultiWriter(h, &body)
		}
		n, err := io.Copy(dst, io.LimitReader(r, remaining+1))
		_ = r.Close()
		if err != nil || n > remaining {
			return nil, "", fmt.Errorf("frozen context entry exceeds its limit")
		}
		remaining -= n
		files[f.Name] = hex.EncodeToString(h.Sum(nil))
		if f.Name == v.Dockerfile {
			dockerfile = body.String()
		}
	}
	return files, dockerfile, nil
}

// One bounded review hunk is sufficient here; this is evidence for a model or
// reviewer, never a patch that the host applies automatically.
func experienceDockerfileDiff(before, after, oldPath, newPath string) string {
	if before == after || before == "" && after == "" {
		return ""
	}
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", oldPath, newPath, start+1, endA-start, start+1, endB-start)
	for _, line := range a[start:endA] {
		out.WriteString("-" + line + "\n")
	}
	for _, line := range b[start:endB] {
		out.WriteString("+" + line + "\n")
	}
	return buildExperienceText(out.String(), 4096)
}

func (m *Manager) experienceContextPatch(failed, success ImageBuild) *BuildExperiencePatch {
	patch := &BuildExperiencePatch{Files: []BuildExperienceFileChange{}}
	before, oldDockerfile, err := m.experienceContextFiles(failed)
	if err != nil {
		patch.Error = "失败构建的冻结目录不可供比对：" + buildExperienceText(err.Error(), 512)
		return patch
	}
	after, newDockerfile, err := m.experienceContextFiles(success)
	if err != nil {
		patch.Error = "成功构建的冻结目录不可供比对：" + buildExperienceText(err.Error(), 512)
		return patch
	}
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		if before[path] == after[path] {
			continue
		}
		patch.ChangedFiles++
		if len(patch.Files) >= 256 {
			patch.Truncated = true
			continue
		}
		change := "modified"
		if before[path] == "" {
			change = "added"
		} else if after[path] == "" {
			change = "removed"
		}
		patch.Files = append(patch.Files, BuildExperienceFileChange{Path: path, Change: change, BeforeSHA256: before[path], AfterSHA256: after[path]})
	}
	patch.DockerfileDiff = experienceDockerfileDiff(oldDockerfile, newDockerfile, failed.ID+"/"+failed.Dockerfile, success.ID+"/"+success.Dockerfile)
	patch.Available = true
	return patch
}

func (m *Manager) imageExperience(in BuildExperienceInput, out *BuildExperience) error {
	failed, err := m.BuildCenterBuild(in.FailedID)
	if err != nil {
		return err
	}
	success, err := m.BuildCenterBuild(in.SuccessID)
	if err != nil {
		return err
	}
	if failed.Scope == "image-test" || failed.Status != "failed" && failed.Status != "interrupted" {
		return failure(409, "experience_failure_required", "来源必须是已失败或已中断的镜像构建")
	}
	if success.Scope == "image-test" || success.Status != "succeeded" || !validBuildCenterImageID(success.ImageID) {
		return failure(409, "experience_success_required", "修复必须引用已成功并产生不可变镜像的构建")
	}
	out.Failure, out.Success = imageExperienceSnapshot(failed), imageExperienceSnapshot(success)
	out.Failure.LogExcerpt = m.experienceBuildLog(failed.ID)
	out.Patch = m.experienceContextPatch(failed, success)
	out.Scope = BuildExperienceScope{OS: failed.PlatformOS, Arch: failed.PlatformArch, Version: failed.CodexVersion, ProfileID: failed.ProfileID, RecipeID: failed.RecipeID, RecipeRevision: failed.RecipeRevision, RecipeSHA: failed.RecipeSHA}
	if out.Failure.Error == "" && out.Failure.LogExcerpt == "" {
		out.Warnings = append(out.Warnings, "失败记录缺少错误或日志证据；仅保存为候选。")
	}
	if failed.RuntimeIdentity != nil {
		out.Scope.ProfileVersion = failed.RuntimeIdentity.ProfileVersion
	}
	if failed.PlatformOS == "" || failed.PlatformArch == "" || failed.PlatformOS != success.PlatformOS || failed.PlatformArch != success.PlatformArch {
		out.Warnings = append(out.Warnings, "失败与成功构建的平台证据缺失或不同；仅保存为候选。")
	}
	if failed.CodexVersion != success.CodexVersion {
		out.Warnings = append(out.Warnings, "Codex 版本发生变化；仅保存为候选，不能推广为原版本修复。")
	}
	if !m.experienceBuildLineage(failed, success) {
		out.Warnings = append(out.Warnings, "未找到相同预设、配方或明确的修复来源链；仅保存为候选。")
	}
	if failed.RecipeSmoke != nil && !reflect.DeepEqual(failed.RecipeSmoke, success.RecipeSmoke) {
		out.Warnings = append(out.Warnings, "原有 smoke 验收条件发生变化；修复经验需要独立复核。")
	}
	for _, id := range in.VerificationIDs {
		v, err := m.BuildCenterBuild(id)
		if err != nil {
			return err
		}
		if v.Scope != "image-test" || v.SourceBuildID != success.ID || v.ImageID != success.ImageID || v.ContextSHA256 != success.ContextSHA256 || v.RecipeSHA != success.RecipeSHA || v.PlatformOS != success.PlatformOS || v.PlatformArch != success.PlatformArch || !reflect.DeepEqual(v.CodexRuntime, success.CodexRuntime) {
			return failure(409, "experience_verification_mismatch", "测试未绑定修复构建的同一镜像、冻结目录和平台")
		}
		if v.Status != "succeeded" {
			continue
		}
		switch v.TestKind {
		case "offline":
			out.Validation.Offline = true
		case "smoke":
			if success.RecipeSmoke != nil && len(success.RecipeSmoke.Command) != 0 && reflect.DeepEqual(v.RecipeSmoke, success.RecipeSmoke) {
				out.Validation.Smoke = true
			}
		}
	}
	if !out.Validation.Offline || !out.Validation.Smoke {
		out.Warnings = append(out.Warnings, "须有精确绑定本次修复构建的成功 offline 和 smoke 检查，才能标记已验证。")
	}
	return nil
}

func (m *Manager) RecordBuildExperience(in BuildExperienceInput) (BuildExperience, error) {
	var out BuildExperience
	in.Summary, in.Changes = strings.TrimSpace(in.Summary), strings.TrimSpace(in.Changes)
	if in.Kind != "codex-install" && in.Kind != "image-build" || !buildCenterID.MatchString(in.FailedID) || !buildCenterID.MatchString(in.SuccessID) || in.FailedID == in.SuccessID || len(in.Summary) == 0 || len(in.Summary) > 2048 || len(in.Changes) == 0 || len(in.Changes) > 16<<10 || len(in.VerificationIDs) > 8 {
		return out, failure(400, "invalid_experience", "需要有效类型、不同失败/成功编号、简述和修改说明；测试编号最多八项")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range in.VerificationIDs {
		if !buildCenterID.MatchString(id) {
			return out, failure(400, "invalid_experience", "测试编号格式无效")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	in.VerificationIDs = ids
	out = BuildExperience{Schema: 1, Kind: in.Kind, Status: "candidate", Summary: buildExperienceText(in.Summary, 2048), Changes: buildExperienceText(in.Changes, 16<<10), VerificationIDs: ids, Warnings: []string{}}
	var err error
	if in.Kind == "codex-install" {
		err = m.installExperience(in, &out)
	} else {
		err = m.imageExperience(in, &out)
	}
	if err != nil {
		return BuildExperience{}, err
	}
	if len(out.Warnings) == 0 {
		out.Status = "verified"
	}
	// Error/log text is data. Its digest groups evidence; no keyword triggers an
	// automatic source change or turns a one-off 403 into a permanent policy.
	out.FailureFingerprint = buildExperienceDigest(struct{ Error, Log string }{out.Failure.Error, out.Failure.LogExcerpt})
	out.FamilyID = buildExperienceDigest(struct {
		Kind, Fingerprint string
		Scope             BuildExperienceScope
	}{out.Kind, out.FailureFingerprint, out.Scope})[:24]
	out.ID = buildExperienceDigest(out)[:24]
	buildExperienceMu.Lock()
	defer buildExperienceMu.Unlock()
	var previous BuildExperience
	if err = m.Store.Get("build-experience", out.ID, &previous); err == nil {
		previous.CurrentApplicable = previous.Status == "verified" && previous.Scope.OS == runtime.GOOS && previous.Scope.Arch == runtime.GOARCH
		return previous, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return BuildExperience{}, err
	}
	rows, err := m.Store.List("build-experience")
	if err != nil {
		return BuildExperience{}, err
	}
	out.Version = 1
	for _, raw := range rows {
		var v BuildExperience
		if err := json.Unmarshal(raw, &v); err != nil {
			return BuildExperience{}, err
		}
		if v.FamilyID == out.FamilyID && v.Version >= out.Version {
			out.Version, out.PreviousID = v.Version+1, v.ID
		}
	}
	out.CreatedAt = store.Now()
	if err := m.Store.Put("build-experience", out.ID, out); err != nil {
		return BuildExperience{}, err
	}
	out.CurrentApplicable = out.Status == "verified" && out.Scope.OS == runtime.GOOS && out.Scope.Arch == runtime.GOARCH
	return out, nil
}

// BuildExperiences lists observations, not instructions. Callers must also
// compare the exact task version/profile; CurrentApplicable only compares the
// current host platform. query is full-text discovery, not compatibility proof.
func (m *Manager) BuildExperiences(kind, query string) ([]BuildExperience, error) {
	if kind != "" && kind != "codex-install" && kind != "image-build" || len(query) > 512 {
		return nil, failure(400, "invalid_experience_query", "经验类型或检索文本无效")
	}
	rows, err := m.Store.List("build-experience")
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := []BuildExperience{}
	for _, raw := range rows {
		var v BuildExperience
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		if kind != "" && v.Kind != kind {
			continue
		}
		text, _ := json.Marshal(v)
		if query != "" && !strings.Contains(strings.ToLower(string(text)), query) {
			continue
		}
		v.CurrentApplicable = v.Status == "verified" && v.Scope.OS == runtime.GOOS && v.Scope.Arch == runtime.GOARCH
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if len(out) > 200 {
		out = out[:200]
	}
	return out, nil
}

// AutoRecordBuildRepair is called after a build/test terminal record has been
// persisted. The caller must not hold buildMu: evidence reads acquire it. A
// normal successful build without a recorded failed ancestor creates no case.
// The first check may create a candidate; subsequent checks append a verified
// version when both frozen checks pass, without overwriting the candidate.
func (m *Manager) AutoRecordBuildRepair(testOrBuildID string) (BuildExperience, error) {
	v, err := m.BuildCenterBuild(testOrBuildID)
	if err != nil {
		return BuildExperience{}, err
	}
	if v.Scope == "image-test" {
		if v.SourceBuildID == "" {
			return BuildExperience{}, nil
		}
		v, err = m.BuildCenterBuild(v.SourceBuildID)
		if err != nil {
			return BuildExperience{}, err
		}
	}
	if v.Status != "succeeded" || v.RebuildOf == "" {
		return BuildExperience{}, nil
	}
	success := v
	var failed ImageBuild
	seen := map[string]bool{v.ID: true}
	for n := 0; n < 16 && v.RebuildOf != ""; n++ {
		if seen[v.RebuildOf] {
			break
		}
		seen[v.RebuildOf] = true
		v, err = m.BuildCenterBuild(v.RebuildOf)
		if err != nil {
			return BuildExperience{}, err
		}
		if v.Status == "failed" || v.Status == "interrupted" {
			failed = v
			break
		}
	}
	if failed.ID == "" {
		return BuildExperience{}, nil
	}
	rows, err := m.BuildCenterBuilds()
	if err != nil {
		return BuildExperience{}, err
	}
	latest := map[string]ImageBuild{}
	for _, test := range rows {
		if test.Scope != "image-test" || test.SourceBuildID != success.ID || test.TestKind != "offline" && test.TestKind != "smoke" || buildActive(test.Status) {
			continue
		}
		if previous, ok := latest[test.TestKind]; !ok || test.Created > previous.Created {
			latest[test.TestKind] = test
		}
	}
	ids := []string{}
	for _, kind := range []string{"offline", "smoke"} {
		if test, ok := latest[kind]; ok {
			ids = append(ids, test.ID)
		}
	}
	changes := fmt.Sprintf("自动记录可观察差异，不推断根因。冻结目录：%s → %s；配方：%s → %s。", failed.ContextSHA256, success.ContextSHA256, failed.RecipeSHA, success.RecipeSHA)
	if !reflect.DeepEqual(failed.BuildSources, success.BuildSources) {
		arguments := func(v *BuildSourceSnapshot) map[string]string {
			if v == nil {
				return nil
			}
			return v.Arguments
		}
		old, _ := json.Marshal(arguments(failed.BuildSources))
		updated, _ := json.Marshal(arguments(success.BuildSources))
		changes += fmt.Sprintf(" 下载源参数：%s → %s。", old, updated)
	}
	if len(changes) > 12000 {
		changes = strings.ToValidUTF8(changes[:12000], "�") + " [truncated]"
	}
	return m.RecordBuildExperience(BuildExperienceInput{Kind: "image-build", FailedID: failed.ID, SuccessID: success.ID, VerificationIDs: ids, Summary: "自动记录镜像构建修复观察", Changes: changes})
}

func (s *Server) setupExperienceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/experiences", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.BuildExperiences(r.URL.Query().Get("kind"), r.URL.Query().Get("query"))
		respond(w, map[string]any{"items": items}, err)
	})
	mux.HandleFunc("POST /api/setup/experiences", func(w http.ResponseWriter, r *http.Request) {
		var in BuildExperienceInput
		if !decode(w, r, &in) {
			return
		}
		v, err := s.Manager.RecordBuildExperience(in)
		respond(w, v, err)
	})
}
