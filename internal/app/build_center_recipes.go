package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/shengjuntu/rundesk/examples/profiletemplates"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Recipes describe build inputs and requirements. Plugins remain native Codex
// plugin references; a recipe is not another plugin package or workflow format.
type RecipePluginRequirement struct {
	ID          string `json:"id"`
	Marketplace string `json:"marketplace,omitempty"`
	Version     string `json:"version,omitempty"`
}

type RecipeSmokeSpec struct {
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	ExpectedFiles  []string `json:"expectedFiles"`
}

type BuildRecipeManifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Name          string                    `json:"name"`
	Version       string                    `json:"version"`
	Description   string                    `json:"description"`
	Dockerfile    string                    `json:"dockerfile"`
	Plugins       []RecipePluginRequirement `json:"plugins"`
	Smoke         RecipeSmokeSpec           `json:"smoke"`
}

type BuildRecipe struct {
	BuildRecipeManifest
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Revision      int    `json:"revision"`
	ProfileID     string `json:"profileId,omitempty"`
	ContextSHA256 string `json:"contextSha256"`
	RecipeSHA     string `json:"recipeSha"`
	Created       string `json:"created"`
	Updated       string `json:"updated"`
	Archived      bool   `json:"archived"`
	OwnerRunID    string `json:"ownerRunId,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
	RequestSHA    string `json:"requestSha,omitempty"`
}

type RecipeBuildInput struct {
	CodexRuntime *BuildRuntimeOptions `json:"codexRuntime,omitempty"`
	RecipeSHA      string              `json:"recipeSha,omitempty"`
	BuildSources   *BuildSourceOptions `json:"buildSources,omitempty"`
	RecipeRevision int                 `json:"recipeRevision"`
	CodexVersion   string              `json:"codexVersion,omitempty"`
	Reference      string              `json:"reference,omitempty"`
	RequestID      string              `json:"requestId"`
	NoCache        bool                `json:"noCache,omitempty"`
	OwnerRunID     string              `json:"-"`
	RepairOf       string              `json:"-"`
}

type RebuildCenterBuildInput struct {
	RequestID string `json:"requestId"`
	NoCache   bool   `json:"noCache,omitempty"`
}

// Serializes catalog mutations and idempotency checks without creating a second
// build scheduler. Docker work continues to use Manager.buildMu/buildLoop.
var buildRecipeMu sync.Mutex

var buildCenterID = regexp.MustCompile(`^[a-f0-9]{24}$`)
var recipeVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
var recipePluginID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
var recipeSecretLiteral = regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----`)

type buildCenterRequest struct {
	Operation string `json:"operation"`
	SHA256    string `json:"sha256"`
	ResultID  string `json:"resultId"`
}

func validateBuildCenterRequestID(id string) error {
	if !buildCenterID.MatchString(id) {
		return failure(400, "invalid_request_id", "requestId 必须为 24 位小写十六进制；重试时沿用同一值")
	}
	return nil
}

func normalizeRecipeManifest(in BuildRecipeManifest) (BuildRecipeManifest, error) {
	in.Name, in.Version = strings.TrimSpace(in.Name), strings.TrimSpace(in.Version)
	in.Description, in.Dockerfile = strings.TrimSpace(in.Description), strings.TrimSpace(in.Dockerfile)
	if in.SchemaVersion != 1 {
		return in, failure(400, "invalid_recipe_schema", "仅支持 schemaVersion 1 的配方")
	}
	if in.Name == "" || len(in.Name) > 120 || strings.ContainsAny(in.Name, "\x00\r\n") || !recipeVersion.MatchString(in.Version) {
		return in, failure(400, "invalid_recipe", "配方名称必填且最多 120 字节，version 必须为 1–64 位版本标识")
	}
	if len(in.Description) > 4000 || strings.ContainsRune(in.Description, '\x00') {
		return in, failure(400, "invalid_recipe", "配方说明最多 4000 字节")
	}
	if in.Dockerfile == "" {
		in.Dockerfile = "Dockerfile"
	}
	if !safeBuildPath(in.Dockerfile) {
		return in, failure(400, "invalid_dockerfile", "Dockerfile 必须是构建目录内的相对路径")
	}
	if len(in.Plugins) > 32 {
		return in, failure(400, "invalid_recipe_plugins", "最多 32 个原生插件引用")
	}
	if in.Plugins == nil {
		in.Plugins = []RecipePluginRequirement{}
	}
	seen := map[string]bool{}
	for _, p := range in.Plugins {
		if !recipePluginID.MatchString(p.ID) || len(p.Marketplace) > 512 || strings.ContainsAny(p.Marketplace, "\x00\r\n") || (p.Version != "" && !recipeVersion.MatchString(p.Version)) || seen[p.ID+"@"+p.Marketplace] {
			return in, failure(400, "invalid_recipe_plugins", "插件仅接受不重复的原生 ID、来源与可选版本描述")
		}
		seen[p.ID+"@"+p.Marketplace] = true
	}
	s := &in.Smoke
	if s.TimeoutSeconds == 0 {
		s.TimeoutSeconds = 60
	}
	if s.TimeoutSeconds < 1 || s.TimeoutSeconds > 600 || len(s.Command) > 64 || len(s.ExpectedFiles) > 64 {
		return in, failure(400, "invalid_recipe_smoke", "检查命令/文件最多 64 项，超时范围 1–600 秒")
	}
	if s.Command == nil {
		s.Command = []string{}
	}
	if s.ExpectedFiles == nil {
		s.ExpectedFiles = []string{}
	}
	for _, arg := range s.Command {
		if arg == "" || len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return in, failure(400, "invalid_recipe_smoke", "检查命令参数无效")
		}
	}
	for _, name := range s.ExpectedFiles {
		p := strings.TrimPrefix(name, "/")
		if !strings.HasPrefix(name, "/") || !safeBuildPath(p) || strings.HasPrefix(name, "//") {
			return in, failure(400, "invalid_recipe_smoke", "检查文件必须为容器内的规范绝对路径")
		}
	}
	encoded, _ := json.Marshal(in)
	if recipeSecretLiteral.Match(encoded) || diagnosticURLAuth.Match(encoded) {
		return in, failure(400, "recipe_secret_file", "配方不得包含访问令牌、私钥或 URL 凭据；请使用运行时配置")
	}
	return in, nil
}

func recipeDigest(in BuildRecipeManifest, contextSHA string) string {
	b, _ := json.Marshal(struct {
		Manifest   BuildRecipeManifest `json:"manifest"`
		ContextSHA string              `json:"contextSha256"`
	}{in, contextSHA})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func builtinBuildRecipes() ([]BuildRecipe, error) {
	items := []BuildRecipe{}
	for _, id := range []string{"news2douyin", "people-search", "video-build", "hyperframes", "video-use", "openmontage"} {
		b, err := profiletemplates.Files.ReadFile(id + ".zip")
		if err != nil {
			return nil, err
		}
		archive, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return nil, err
		}
		var metadata struct {
			Profile struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Version     string `json:"version"`
			} `json:"profile"`
		}
		for _, f := range archive.File {
			if f.Name != "profile.lock.json" {
				continue
			}
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			e = json.NewDecoder(io.LimitReader(r, 256<<10)).Decode(&metadata)
			r.Close()
			if e != nil {
				return nil, e
			}
			break
		}
		manifest, err := normalizeRecipeManifest(BuildRecipeManifest{SchemaVersion: 1, Name: metadata.Profile.Title, Version: metadata.Profile.Version, Description: metadata.Profile.Description, Dockerfile: "Dockerfile"})
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		contextSHA := hex.EncodeToString(sum[:])
		items = append(items, BuildRecipe{BuildRecipeManifest: manifest, ID: "builtin-" + id, Kind: "builtin", ProfileID: id, Revision: 1, ContextSHA256: contextSHA, RecipeSHA: recipeDigest(manifest, contextSHA)})
	}
	return items, nil
}

func (m *Manager) BuildCenterRecipes() ([]BuildRecipe, error) {
	items, err := builtinBuildRecipes()
	if err != nil {
		return nil, err
	}
	rows, err := m.Store.List("build-recipe")
	if err != nil {
		return nil, err
	}
	custom := []BuildRecipe{}
	for _, row := range rows {
		var r BuildRecipe
		if err = json.Unmarshal(row, &r); err != nil {
			return nil, err
		}
		custom = append(custom, r)
	}
	sort.Slice(custom, func(i, j int) bool { return custom[i].Created > custom[j].Created })
	return append(items, custom...), nil
}

func (m *Manager) BuildCenterRecipe(id string) (BuildRecipe, error) {
	if strings.HasPrefix(id, "builtin-") {
		items, err := builtinBuildRecipes()
		if err != nil {
			return BuildRecipe{}, err
		}
		for _, r := range items {
			if r.ID == id {
				return r, nil
			}
		}
		return BuildRecipe{}, failure(404, "recipe_not_found", "构建配方不存在")
	}
	var r BuildRecipe
	if !strings.HasPrefix(id, "custom-") || !buildCenterID.MatchString(strings.TrimPrefix(id, "custom-")) {
		return r, failure(404, "recipe_not_found", "构建配方不存在")
	}
	err := m.Store.Get("build-recipe", id, &r)
	if errors.Is(err, sql.ErrNoRows) {
		return r, failure(404, "recipe_not_found", "构建配方不存在")
	}
	return r, err
}

func (m *Manager) recipeSnapshotDir(id string, revision int) string {
	return filepath.Join(m.Data, "build-recipes", id, "revisions", strconv.Itoa(revision))
}

func secretRecipePath(name string) bool {
	base := strings.ToLower(path.Base(name))
	for _, suffix := range []string{".example", ".sample", ".template"} {
		if strings.HasSuffix(base, suffix) {
			return false
		}
	}
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if part == ".git" || part == ".ssh" || part == ".aws" {
			return true
		}
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") {
		return true
	}
	switch base {
	case ".npmrc", ".netrc", "auth.json", "credentials", "credentials.json", "credentials.yaml", "credentials.yml", "token", "token.txt", "token.json", "tokens.json", "secrets.json", "secrets.yaml", "secrets.yml", "id_rsa", "id_ed25519", "id_ecdsa":
		return true
	}
	return false
}

func validateRecipeSecrets(dir string) error {
	return filepath.WalkDir(dir, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		if secretRecipePath(filepath.ToSlash(rel)) {
			return failure(400, "recipe_secret_file", "构建目录含凭据、私钥或令牌文件；请使用运行时配置，并从 ZIP 中移除")
		}
		if d.IsDir() {
			return nil
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		// Keep import memory bounded even for a very large individual source file.
		var tail []byte
		buf := make([]byte, 32<<10)
		for {
			n, readErr := f.Read(buf)
			chunk := append(tail, buf[:n]...)
			if recipeSecretLiteral.Match(chunk) {
				f.Close()
				return failure(400, "recipe_secret_file", "构建目录含明文访问令牌或私钥；请移除后重试")
			}
			if len(chunk) > 256 {
				chunk = chunk[len(chunk)-256:]
			}
			tail = append(tail[:0], chunk...)
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				f.Close()
				return readErr
			}
		}
		return f.Close()
	})
}

func (m *Manager) buildCenterReplay(operation, requestID, digest string) (string, error) {
	var req buildCenterRequest
	err := m.Store.Get("build-center-request", requestID, &req)
	if errors.Is(err, sql.ErrNoRows) {
		// The result itself carries the request hash. This recovers idempotency if
		// the process stopped between storing a result and its request index.
		kind := "image-build"
		if operation == "import" {
			kind = "build-recipe"
		}
		rows, listErr := m.Store.List(kind)
		if listErr != nil {
			return "", listErr
		}
		for _, row := range rows {
			var item struct {
				ID         string `json:"id"`
				RequestID  string `json:"requestId"`
				RequestSHA string `json:"requestSha"`
				RebuildOf  string `json:"rebuildOf"`
			}
			if listErr = json.Unmarshal(row, &item); listErr != nil {
				return "", listErr
			}
			if item.RequestID != requestID {
				continue
			}
			if item.RequestSHA != digest || (operation == "rebuild") != (item.RebuildOf != "") {
				return "", failure(409, "request_id_conflict", "requestId 已用于不同请求；新请求应使用新值")
			}
			return item.ID, nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if req.Operation != operation || req.SHA256 != digest {
		return "", failure(409, "request_id_conflict", "requestId 已用于不同请求；新请求应使用新值")
	}
	return req.ResultID, nil
}

// ownerRunID is injected by the scoped build assistant, never read from an HTTP
// manifest. The immutable snapshot contains the manifest and original ZIP bytes.
func (m *Manager) ImportBuildRecipe(manifest BuildRecipeManifest, src io.Reader, requestID, ownerRunID string) (BuildRecipe, error) {
	var r BuildRecipe
	if err := validateBuildCenterRequestID(requestID); err != nil {
		return r, err
	}
	manifest, err := normalizeRecipeManifest(manifest)
	if err != nil {
		return r, err
	}
	base := filepath.Join(m.Data, "build-recipes")
	if err = os.MkdirAll(base, 0700); err != nil {
		return r, err
	}
	tmp, err := os.MkdirTemp(base, "import-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(tmp)
	f, err := os.OpenFile(filepath.Join(tmp, "context.zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return r, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, buildUploadLimit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return r, copyErr
	}
	if closeErr != nil {
		return r, closeErr
	}
	if n > buildUploadLimit {
		return r, failure(413, "build_upload_limit", "ZIP 最多 32 MiB")
	}
	if _, _, err = extractBuildContext(filepath.Join(tmp, "context.zip"), filepath.Join(tmp, "context"), manifest.Dockerfile); err != nil {
		return r, err
	}
	if err = validateRecipeSecrets(filepath.Join(tmp, "context")); err != nil {
		return r, err
	}
	contextSHA := hex.EncodeToString(h.Sum(nil))
	digest := recipeDigest(manifest, contextSHA)
	requestSHA := recipeRequestDigest(struct{ SHA, Owner string }{digest, ownerRunID})
	buildRecipeMu.Lock()
	defer buildRecipeMu.Unlock()
	if id, err := m.buildCenterReplay("import", requestID, requestSHA); err != nil {
		return r, err
	} else if id != "" {
		return m.BuildCenterRecipe(id)
	}
	rows, err := m.Store.List("build-recipe")
	if err != nil {
		return r, err
	}
	if len(rows) >= 200 {
		return r, failure(409, "recipe_limit", "最多保存 200 个自定义配方及其历史")
	}
	id := "custom-" + store.ID()
	now := store.Now()
	r = BuildRecipe{BuildRecipeManifest: manifest, ID: id, Kind: "custom", Revision: 1, ContextSHA256: contextSHA, RecipeSHA: digest, Created: now, Updated: now, OwnerRunID: ownerRunID, RequestID: requestID, RequestSHA: requestSHA}
	if err = os.RemoveAll(filepath.Join(tmp, "context")); err != nil {
		return r, err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, err
	}
	if err = os.WriteFile(filepath.Join(tmp, "manifest.json"), append(b, '\n'), 0400); err != nil {
		return r, err
	}
	if err = os.Chmod(filepath.Join(tmp, "context.zip"), 0400); err != nil {
		return r, err
	}
	dest := m.recipeSnapshotDir(id, 1)
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return r, err
	}
	if err = os.Rename(tmp, dest); err != nil {
		return r, err
	}
	if err = m.Store.Put("build-recipe", id, r); err != nil {
		_ = os.RemoveAll(filepath.Join(base, id))
		return r, err
	}
	err = m.Store.Put("build-center-request", requestID, buildCenterRequest{Operation: "import", SHA256: requestSHA, ResultID: id})
	return r, err
}

func (m *Manager) ArchiveBuildRecipe(id string, revision int) (BuildRecipe, error) {
	buildRecipeMu.Lock()
	defer buildRecipeMu.Unlock()
	r, err := m.BuildCenterRecipe(id)
	if err != nil {
		return r, err
	}
	if r.Kind == "builtin" {
		return r, failure(409, "builtin_recipe_readonly", "内置配方为只读；可另行导入自定义配方")
	}
	if revision != r.Revision {
		return r, failure(409, "recipe_revision_conflict", "配方版本已变化，请刷新后重试")
	}
	if r.Archived {
		return r, nil
	}
	r.Archived, r.Updated = true, store.Now()
	return r, m.Store.Put("build-recipe", id, r)
}

func recipeRequestDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func verifiedFrozenContext(filename, expectedSHA string) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, failure(409, "build_context_unavailable", "冻结构建目录不可用；请重新导入配方或重新准备内置环境")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, buildUploadLimit+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if len(b) > buildUploadLimit || hex.EncodeToString(sum[:]) != expectedSHA {
		return nil, failure(409, "build_context_changed", "冻结构建目录校验失败，拒绝使用已变更的源文件")
	}
	return b, nil
}

func (m *Manager) StartRecipeBuild(id string, in RecipeBuildInput) (ImageBuild, error) {
	var v ImageBuild
	if err := validateBuildCenterRequestID(in.RequestID); err != nil {
		return v, err
	}
	buildRecipeMu.Lock()
	defer buildRecipeMu.Unlock()
	var rawBuiltin []byte
	var r BuildRecipe
	var err error
	if strings.HasPrefix(id, "builtin-") {
		profile := strings.TrimPrefix(id, "builtin-")
		rawBuiltin, err = builtinRecipeSourceContext(profile)
		if err == nil {
			r, err = builtinRecipeFromSource(profile, rawBuiltin)
		}
	} else {
		r, err = m.BuildCenterRecipe(id)
	}
	if err != nil {
		return v, err
	}
	if in.OwnerRunID != "" && r.Kind == "custom" && r.OwnerRunID != in.OwnerRunID {
		return v, failure(404, "recipe_not_found", "构建配方不存在")
	}
	if in.RecipeRevision != r.Revision {
		return v, failure(409, "recipe_revision_conflict", "配方版本已变化，请刷新后重试")
	}
	// The owner participates in idempotency, so a scoped assistant cannot replay
	// an administrator's request ID and obtain another run's result.
	digest := recipeRequestDigest(struct {
		ID    string
		Input RecipeBuildInput
		Owner string
	}{id, in, in.OwnerRunID})
	if bid, err := m.buildCenterReplay("build", in.RequestID, digest); err != nil {
		return v, err
	} else if bid != "" {
		return m.BuildCenterBuild(bid)
	}
	if in.RecipeSHA != "" && in.RecipeSHA != r.RecipeSHA {
		return v, failure(409, "recipe_snapshot_changed", "配方文件已变化，请刷新后重新提交")
	}
	if r.Archived {
		return v, failure(409, "recipe_archived", "配方已归档，不能发起新构建；历史记录仍可查看和重建")
	}
	if in.Reference != "" && (!imageReference.MatchString(in.Reference) || strings.Contains(in.Reference, "@") || strings.HasPrefix(in.Reference, "sha256:")) {
		return v, failure(400, "invalid_image_reference", "构建目标必须为有效镜像标签")
	}
	var data []byte
	var setup *RuntimeImageInput
	if r.Kind == "builtin" {
		data, err = expandRuntimeImageContext(rawBuiltin, in.CodexVersion)
		if err != nil {
			return v, err
		}
		if in.Reference == "" {
			in.Reference = "rundesk-" + r.ProfileID + ":" + in.CodexVersion
		}
		setup = &RuntimeImageInput{ProfileID: r.ProfileID, CodexVersion: in.CodexVersion, Source: "build", Reference: in.Reference}
	} else {
		if in.CodexVersion != "" {
			return v, failure(400, "custom_recipe_frozen", "自定义配方按已导入 Dockerfile 构建；Codex 版本应在配方中固定")
		}
		data, err = verifiedFrozenContext(filepath.Join(m.recipeSnapshotDir(r.ID, r.Revision), "context.zip"), r.ContextSHA256)
		if err != nil {
			return v, err
		}
	}
	if recipeDigest(r.BuildRecipeManifest, r.ContextSHA256) != r.RecipeSHA {
		return v, failure(409, "recipe_snapshot_changed", "配方快照校验失败，请重新导入")
	}
	var repairSource *ImageBuild
	if in.RepairOf != "" {
		source, err := m.BuildCenterBuild(in.RepairOf)
		if err != nil {
			return v, err
		}
		if buildActive(source.Status) || source.Scope == "image-test" || source.Status == "succeeded" {
			return v, failure(400, "invalid_repair_source", "修复来源必须是已结束的失败构建")
		}
		repairSource = &source
		if source.ProfileID != "" && source.CodexVersion != "" {
			setup = &RuntimeImageInput{ProfileID: source.ProfileID, CodexVersion: source.CodexVersion, Source: "build", Reference: in.Reference}
		}
	}
	sources, err := m.resolveBuildSources(in.BuildSources, data, r.Dockerfile)
	if err != nil {
		return v, err
	}
	if in.BuildSources == nil && repairSource != nil && repairSource.BuildSources != nil {
		encoded, _ := json.Marshal(repairSource.BuildSources)
		if err = json.Unmarshal(encoded, &sources); err != nil {
			return v, err
		}
		if _, err = buildSourceArguments(sources); err != nil {
			return v, err
		}
		text, err := recipeDockerfile(data, r.Dockerfile)
		if err != nil {
			return v, err
		}
		caps := sourceCapabilitiesFor(text, sources.Definitions)
		supported := map[string]bool{}
		for _, c := range caps.Types {
			for _, name := range c.Arguments {
				supported[name] = true
			}
		}
		for name := range sources.Arguments {
			if !supported[name] {
				return v, failure(409, "repair_source_arguments_changed", "修复配方移除了原下载源参数，请显式选择新的源配置")
			}
		}
	}
	var runtimeSnapshot *BuildRuntimeSnapshot
	if in.CodexRuntime == nil && repairSource != nil && repairSource.CodexRuntime != nil {
		copy := *repairSource.CodexRuntime
		runtimeSnapshot = &copy
		if !runtimeRecipeSupported(data, r.Dockerfile) || runtimeRecipeVersion(data, r.Dockerfile) != copy.Version {
			return v, failure(409, "repair_runtime_changed", "修复配方移除了原运行时接口或改变了 Codex 版本，请显式选择新运行时")
		}
		if sources != nil && copy.Mode == "base" {
			if base := sources.Arguments["RD_BASE_IMAGE"]; base != "" && base != copy.Reference && base != copy.ImageID {
				return v, failure(409, "codex_runtime_source_conflict", "修复构建的基础镜像与冻结运行时不一致，请显式选择新运行时")
			}
		}
	} else {
		runtimeSnapshot, err = m.resolveBuildRuntime(in.CodexRuntime, data, r.Dockerfile, in.CodexVersion, sources, r.Kind == "builtin")
		if err != nil { return v, err }
	}
	data, err = freezeBuildSources(data, sources)
	if err != nil {
		return v, err
	}
	data, err = freezeBuildRuntime(data, runtimeSnapshot)
	if err != nil { return v, err }
	snapshot := &ImageBuild{CodexRuntime: runtimeSnapshot, RecipeID: r.ID, RecipeRevision: r.Revision, RecipeSHA: r.RecipeSHA, RecipeSmoke: &r.Smoke, OwnerRunID: in.OwnerRunID, RequestID: in.RequestID, RequestSHA: digest, Reference: in.Reference, BuildSources: sources}
	snapshot.RebuildOf = in.RepairOf
	name := r.Name
	if suffix := " · " + r.Version; len(name)+len(suffix) <= 120 {
		name += suffix
	}
	v, err = m.createBuildWithRecipe(DefaultInstance, name, r.Dockerfile, 60, in.NoCache, bytes.NewReader(data), setup, snapshot)
	if err != nil {
		return v, err
	}
	err = m.Store.Put("build-center-request", in.RequestID, buildCenterRequest{Operation: "build", SHA256: digest, ResultID: v.ID})
	return v, err
}

func (m *Manager) BuildCenterBuilds() ([]ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	return m.buildsLocked("")
}

func (m *Manager) BuildCenterBuild(id string) (ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	var v ImageBuild
	if !buildCenterID.MatchString(id) {
		return v, failure(404, "build_not_found", "构建任务不存在")
	}
	err := m.Store.Get("image-build", id, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return v, failure(404, "build_not_found", "构建任务不存在")
	}
	return v, err
}

func (m *Manager) RebuildCenterBuild(id string, in RebuildCenterBuildInput) (ImageBuild, error) {
	var v ImageBuild
	if err := validateBuildCenterRequestID(in.RequestID); err != nil {
		return v, err
	}
	buildRecipeMu.Lock()
	defer buildRecipeMu.Unlock()
	digest := recipeRequestDigest(struct {
		ID    string
		Input RebuildCenterBuildInput
	}{id, in})
	if bid, err := m.buildCenterReplay("rebuild", in.RequestID, digest); err != nil {
		return v, err
	} else if bid != "" {
		return m.BuildCenterBuild(bid)
	}
	old, err := m.BuildCenterBuild(id)
	if err != nil {
		return v, err
	}
	if old.Scope == "image-test" {
		return v, failure(409, "not_image_build", "检查记录不能重建，请对原始构建发起重建")
	}
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(id), "context.zip"), old.ContextSHA256)
	if err != nil {
		return v, err
	}
	var setup *RuntimeImageInput
	if old.Scope == "setup" {
		source := "build"
		if old.Source == "pull" || old.Source == "local" {
			// A download retry reuses the frozen acquisition request. Building
			// under a digest reference would be invalid and changes its meaning.
			source = old.Source
		}
		setup = &RuntimeImageInput{ProfileID: old.ProfileID, CodexVersion: old.CodexVersion, Source: source, Reference: old.Reference}
	}
	frozen := old
	frozen.RequestID, frozen.RequestSHA, frozen.RebuildOf = in.RequestID, digest, old.ID
	// Rebuild is an explicit administrator action. Its authorization must not
	// depend on the temporary assistant capability that created the old job.
	frozen.OwnerRunID = ""
	snapshot := &frozen
	v, err = m.createBuildWithRecipe(old.InstanceID, old.Name, old.Dockerfile, old.TimeoutMinutes, in.NoCache, bytes.NewReader(data), setup, snapshot)
	if err != nil {
		return v, err
	}
	if v.Status == "draft" {
		v, err = m.StartBuild(v.InstanceID, v.ID)
		if err != nil {
			return v, err
		}
	}
	err = m.Store.Put("build-center-request", in.RequestID, buildCenterRequest{Operation: "rebuild", SHA256: digest, ResultID: v.ID})
	return v, err
}

func (s *Server) buildCenterRecipeRoutes(mux *http.ServeMux) {
	s.buildSourceRoutes(mux)
	s.buildRecipeFileRoutes(mux)
	s.buildRuntimeRoutes(mux)
	m := s.Manager
	mux.HandleFunc("GET /api/build-center/recipes", func(w http.ResponseWriter, r *http.Request) {
		items, err := m.BuildCenterRecipes()
		respond(w, map[string]any{"items": items}, err)
	})
	mux.HandleFunc("POST /api/build-center/recipes", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, buildUploadLimit+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
			respond(w, nil, failure(400, "invalid_recipe_upload", "请上传 manifest JSON 与构建目录 ZIP，ZIP 最大 32 MiB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		var in BuildRecipeManifest
		decoder := json.NewDecoder(strings.NewReader(r.FormValue("manifest")))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			respond(w, nil, failure(400, "invalid_recipe_manifest", "manifest 必须为有效的 schemaVersion 1 配方 JSON"))
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			respond(w, nil, failure(400, "invalid_recipe_manifest", "manifest 只接受单个 JSON 对象"))
			return
		}
		file, _, err := r.FormFile("context")
		if err != nil {
			respond(w, nil, failure(400, "build_file_required", "请选择 context 构建目录 ZIP"))
			return
		}
		defer file.Close()
		v, err := m.ImportBuildRecipe(in, file, r.FormValue("requestId"), "")
		respond(w, v, err)
	})
	mux.HandleFunc("GET /api/build-center/recipes/{rid}", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.BuildCenterRecipe(r.PathValue("rid"))
		respond(w, v, err)
	})
	mux.HandleFunc("DELETE /api/build-center/recipes/{rid}", func(w http.ResponseWriter, r *http.Request) {
		rev, err := strconv.Atoi(r.URL.Query().Get("revision"))
		if err != nil {
			respond(w, nil, failure(400, "invalid_recipe_revision", "revision 必须为整数"))
			return
		}
		v, err := m.ArchiveBuildRecipe(r.PathValue("rid"), rev)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /api/build-center/recipes/{rid}/build", func(w http.ResponseWriter, r *http.Request) {
		var in RecipeBuildInput
		if !decode(w, r, &in) {
			return
		}
		v, err := m.StartRecipeBuild(r.PathValue("rid"), in)
		respond(w, v, err)
	})
	mux.HandleFunc("GET /api/build-center/builds", func(w http.ResponseWriter, r *http.Request) {
		items, err := m.BuildCenterBuilds()
		respond(w, map[string]any{"items": items}, err)
	})
	mux.HandleFunc("GET /api/build-center/builds/{bid}", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.BuildCenterBuild(r.PathValue("bid"))
		respond(w, v, err)
	})
	mux.HandleFunc("POST /api/build-center/builds/{bid}/rebuild", func(w http.ResponseWriter, r *http.Request) {
		var in RebuildCenterBuildInput
		if !decode(w, r, &in) {
			return
		}
		v, err := m.RebuildCenterBuild(r.PathValue("bid"), in)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /api/build-center/builds/{bid}/cancel", func(w http.ResponseWriter, r *http.Request) {
		old, err := m.BuildCenterBuild(r.PathValue("bid"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		v, err := m.CancelBuild(old.InstanceID, old.ID)
		respond(w, v, err)
	})
	mux.HandleFunc("GET /api/build-center/builds/{bid}/log", func(w http.ResponseWriter, r *http.Request) {
		after := int64(0)
		var err error
		if value := r.URL.Query().Get("after"); value != "" {
			after, err = strconv.ParseInt(value, 10, 64)
		}
		if err != nil {
			respond(w, nil, failure(400, "invalid_log_offset", "日志偏移无效"))
			return
		}
		old, err := m.BuildCenterBuild(r.PathValue("bid"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		v, err := m.BuildLog(old.InstanceID, old.ID, after)
		respond(w, v, err)
	})
}
