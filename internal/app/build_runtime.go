package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Runtime assets are separately frozen build inputs. Native platform archives
// can exceed the recipe ZIP limit; their digest is bound into the small recipe
// context and the archive is revalidated before every extraction or rebuild.
type BuildRuntimeOptions struct {
	Mode      string `json:"mode"`
	Reference string `json:"reference,omitempty"`
}
type BuildRuntimeSnapshot struct {
	RequestedMode string `json:"requestedMode"`
	Mode          string `json:"mode"`
	Version       string `json:"version"`
	Platform      string `json:"platform"`
	Reference     string `json:"reference,omitempty"`
	ImageID       string `json:"imageId,omitempty"`
	InstallID     string `json:"installId,omitempty"`
	AssetSHA256   string `json:"assetSha256,omitempty"`
	AssetBytes    int64  `json:"assetBytes,omitempty"`
	Integrity     string `json:"integrity,omitempty"`
	Reason        string `json:"reason,omitempty"`
}
type BuildRuntimeAvailability struct {
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	NativeAvailable bool   `json:"nativeAvailable"`
	NativeInstallID string `json:"nativeInstallId,omitempty"`
	Reason          string `json:"reason"`
}

const buildRuntimeArchiveLimit int64 = 256 << 20

var buildRuntimeAssetMu sync.Mutex
var runtimeSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

func runtimeRecipeSupported(data []byte, dockerfile string) bool {
	text, err := recipeDockerfile(data, dockerfile)
	return err == nil && regexp.MustCompile(`(?m)^ARG RD_CODEX_MODE(?:=|\s*$)`).MatchString(string(text)) && strings.Contains(string(text), "install-codex.sh") && strings.Contains(string(text), "COPY runtime/")
}
func runtimeRecipeVersion(data []byte, dockerfile string) string {
	text, err := recipeDockerfile(data, dockerfile)
	if err != nil {
		return ""
	}
	match := regexp.MustCompile(`(?m)^ARG CODEX_VERSION=([^\r\n]+)$`).FindStringSubmatch(string(text))
	if len(match) != 2 {
		return ""
	}
	value := strings.Trim(strings.TrimSpace(match[1]), `"'`)
	if !profileRelease.MatchString(value) || value == "0.0.0" {
		return ""
	}
	return value
}
func normalizeBuildRuntimeOptions(in *BuildRuntimeOptions) (BuildRuntimeOptions, error) {
	result := BuildRuntimeOptions{Mode: "auto"}
	if in != nil {
		result = *in
	}
	result.Mode, result.Reference = strings.TrimSpace(result.Mode), strings.TrimSpace(result.Reference)
	if result.Mode == "" {
		result.Mode = "auto"
	}
	switch result.Mode {
	case "auto", "native", "base", "npm":
	default:
		return result, failure(400, "invalid_codex_runtime", "Codex 来源须为 auto、native、base 或 npm")
	}
	if result.Mode == "base" {
		if result.Reference == "" || buildSourceValue("image", result.Reference) != nil {
			return result, failure(400, "invalid_codex_runtime", "请填写已有本机基础镜像的完整引用")
		}
	} else if result.Reference != "" {
		return result, failure(400, "invalid_codex_runtime", "只有 base 模式接受基础镜像引用")
	}
	return result, nil
}

// This only selects an installation whose success was recorded by RunDesk.
// Arbitrary host paths and selected CODEX_HOME content never enter a build.
func (m *Manager) nativeBuildRuntime(version string) (*CodexInstallJob, string, error) {
	if runtime.GOOS != "linux" {
		return nil, "", nil
	}
	jobs, err := m.CodexInstalls()
	if err != nil {
		return nil, "", err
	}
	for _, job := range jobs {
		if job.Status != "succeeded" || !job.Result.OK || job.Result.Method != "native" || job.Input.Version != version || job.Result.Version != version || job.Platform != "linux/"+runtime.GOARCH || job.Result.Integrity == "" {
			continue
		}
		if _, err := codexIntegrity(job.Result.Integrity); err != nil {
			continue
		}
		root := filepath.Join(m.Data, "tools", "codex")
		rel, err := filepath.Rel(root, job.Result.Codex)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 2 || parts[0] == ".." {
			continue
		}
		archive := filepath.Join(root, parts[0], "codex-package.tgz")
		real, err := filepath.EvalSymlinks(archive)
		if err != nil || real != archive {
			continue
		}
		info, err := os.Lstat(archive)
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > buildRuntimeArchiveLimit {
			continue
		}
		copy := job
		return &copy, archive, nil
	}
	return nil, "", nil
}
func (m *Manager) BuildRuntimeOptions(version string) (BuildRuntimeAvailability, error) {
	value := BuildRuntimeAvailability{Version: version, Platform: runtime.GOOS + "/" + runtime.GOARCH, Reason: "未找到匹配的已安装原生归档；自动模式将通过所选 npm 源安装。"}
	if !profileRelease.MatchString(version) || version == "0.0.0" {
		return value, failure(400, "invalid_version", "请填写 Codex 精确版本")
	}
	job, _, err := m.nativeBuildRuntime(version)
	if err != nil {
		return value, err
	}
	if job != nil {
		value.NativeAvailable = true
		value.NativeInstallID = job.ID
		value.Reason = "找到相同版本和平台的成功安装记录；提交构建时重新校验归档完整性。"
	}
	if runtime.GOOS != "linux" {
		value.Reason = "当前 Docker 构建需要 Linux 主机；不会将其他平台的 Codex 二进制复制到 Linux 镜像。"
	}
	return value, nil
}
func (m *Manager) runtimeArchivePath(sha string) string {
	return filepath.Join(m.Data, "build-runtime-cache", sha+".tgz")
}
func runtimeArchiveDigest(filename, integrity string) (string, int64, error) {
	expected, err := codexIntegrity(integrity)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("Codex runtime archive must be a regular file")
	}
	f, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h256, h512 := sha256.New(), sha512.New()
	n, err := io.Copy(io.MultiWriter(h256, h512), io.LimitReader(f, buildRuntimeArchiveLimit+1))
	if err != nil {
		return "", 0, err
	}
	if n == 0 || n > buildRuntimeArchiveLimit {
		return "", 0, fmt.Errorf("Codex runtime archive must be between 1 byte and 256 MiB")
	}
	if subtle.ConstantTimeCompare(expected, h512.Sum(nil)) != 1 {
		return "", 0, failure(409, "codex_runtime_integrity", "Codex 原生归档 SHA-512 校验失败；不会降级为其他安装方式")
	}
	return hex.EncodeToString(h256.Sum(nil)), n, nil
}
func (m *Manager) freezeNativeRuntime(job CodexInstallJob, archive string) (*BuildRuntimeSnapshot, error) {
	buildRuntimeAssetMu.Lock()
	defer buildRuntimeAssetMu.Unlock()
	root := filepath.Join(m.Data, "build-runtime-cache")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	source, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	temp, err := os.CreateTemp(root, "freeze-")
	if err != nil {
		return nil, err
	}
	name := temp.Name()
	defer os.Remove(name)
	_, err = io.Copy(temp, io.LimitReader(source, buildRuntimeArchiveLimit+1))
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	sha, size, err := runtimeArchiveDigest(name, job.Result.Integrity)
	if err != nil {
		return nil, err
	}
	dest := m.runtimeArchivePath(sha)
	if _, err = os.Stat(dest); err == nil {
		existing, n, err := runtimeArchiveDigest(dest, job.Result.Integrity)
		if err != nil || existing != sha || n != size {
			return nil, failure(409, "codex_runtime_cache_changed", "冻结 Codex 归档已损坏；请恢复原归档后重试")
		}
	} else if os.IsNotExist(err) {
		if err = os.Chmod(name, 0400); err == nil {
			err = os.Rename(name, dest)
		}
		if err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	return &BuildRuntimeSnapshot{Mode: "native", Version: job.Result.Version, Platform: job.Platform, InstallID: job.ID, AssetSHA256: sha, AssetBytes: size, Integrity: job.Result.Integrity, Reason: "复用已校验的原生 Codex 安装归档，无需再次下载 npm 平台包。"}, nil
}

func (m *Manager) resolveBuildRuntime(in *BuildRuntimeOptions, data []byte, dockerfile, version string, sources *BuildSourceSnapshot, enabled bool) (*BuildRuntimeSnapshot, error) {
	if in == nil && !enabled {
		return nil, nil
	}
	options, err := normalizeBuildRuntimeOptions(in)
	if err != nil {
		return nil, err
	}
	if !runtimeRecipeSupported(data, dockerfile) {
		if in == nil {
			return nil, nil
		} // Existing external/custom recipes keep their own installation contract.
		return nil, failure(400, "codex_runtime_unsupported", "配方没有 runtime/ 和 install-codex.sh 运行时接口；请先更新配方")
	}
	if version == "" {
		version = runtimeRecipeVersion(data, dockerfile)
	}
	if !profileRelease.MatchString(version) || version == "0.0.0" {
		return nil, failure(400, "invalid_version", "复用运行时需要配方固定 Codex 精确版本")
	}
	if sources != nil && sources.Arguments["RD_CODEX_MODE"] != "" {
		return nil, failure(400, "codex_runtime_source_conflict", "Codex 安装方式由运行时选择控制，不能通过下载源覆盖")
	}
	snapshot := &BuildRuntimeSnapshot{RequestedMode: options.Mode, Mode: options.Mode, Version: version, Platform: "linux/" + runtime.GOARCH}
	if options.Mode == "base" {
		if sources != nil {
			if base := sources.Arguments["RD_BASE_IMAGE"]; base != "" && base != options.Reference {
				return nil, failure(400, "codex_runtime_source_conflict", "下载源中的基础镜像与 Codex 基础运行时不同，请只选择同一个镜像")
			}
		}
		image, err := m.inspectImage(options.Reference)
		if err != nil {
			return nil, err
		}
		if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(image.ID) {
			return nil, failure(502, "docker_image_identity", "基础镜像缺少有效的固定身份")
		}
		snapshot.Reference, snapshot.ImageID = options.Reference, image.ID
		snapshot.Reason = "使用固定的本机基础镜像；构建前离线检查 Codex、Node.js、npm 和 node 用户。"
		return snapshot, nil
	}
	if options.Mode == "native" || options.Mode == "auto" {
		job, archive, err := m.nativeBuildRuntime(version)
		if err != nil {
			return nil, err
		}
		if job != nil {
			value, err := m.freezeNativeRuntime(*job, archive)
			if err != nil {
				return nil, err
			}
			value.RequestedMode = options.Mode
			return value, nil
		}
		if options.Mode == "native" {
			return nil, failure(409, "codex_runtime_unavailable", "没有匹配版本和 Linux 平台的成功原生安装归档；请先在初始化第二步安装，或选择 npm")
		}
	}
	snapshot.Mode = "npm"
	snapshot.Reason = "通过本次选择的 npm 源安装精确版本。"
	if options.Mode == "auto" {
		snapshot.Reason = "没有可复用的匹配原生归档，自动选择 npm 安装；此决定已冻结。"
	}
	return snapshot, nil
}

func freezeBuildRuntime(data []byte, snapshot *BuildRuntimeSnapshot) ([]byte, error) {
	if snapshot == nil {
		return data, nil
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	files := map[string][]byte{}
	modes := map[string]os.FileMode{}
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() || entry.Name == "SHA256SUMS" {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(reader, buildContextLimit+1))
		reader.Close()
		if err != nil {
			return nil, err
		}
		if len(content) > buildContextLimit {
			return nil, fmt.Errorf("recipe file exceeds context limit")
		}
		files[entry.Name], modes[entry.Name] = content, entry.Mode()
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	files["build-runtime.lock.json"], modes["build-runtime.lock.json"] = append(encoded, '\n'), 0644
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(files[name]), name)
	}
	files["SHA256SUMS"], modes["SHA256SUMS"] = []byte(sums.String()), 0644
	names = append(names, "SHA256SUMS")
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(modes[name])
		file, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = file.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
func runtimeSnapshotEqual(a, b *BuildRuntimeSnapshot) bool {
	encodedA, _ := json.Marshal(a)
	encodedB, _ := json.Marshal(b)
	return bytes.Equal(encodedA, encodedB)
}

func (m *Manager) validateFrozenBuildRuntime(v ImageBuild) error {
	snapshot := v.CodexRuntime
	if snapshot == nil {
		return nil
	}
	if snapshot.Platform != "linux/"+runtime.GOARCH || !profileRelease.MatchString(snapshot.Version) || snapshot.Version == "0.0.0" {
		return failure(409, "codex_runtime_snapshot_changed", "冻结运行时的平台或版本无效")
	}
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(v.ID), "context.zip"), v.ContextSHA256)
	if err != nil {
		return err
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, entry := range archive.File {
		if entry.Name == "build-runtime.lock.json" {
			r, err := entry.Open()
			if err != nil {
				return err
			}
			defer r.Close()
			var locked BuildRuntimeSnapshot
			if err = json.NewDecoder(io.LimitReader(r, 64<<10)).Decode(&locked); err != nil {
				return err
			}
			if !runtimeSnapshotEqual(snapshot, &locked) {
				return failure(409, "codex_runtime_snapshot_changed", "运行时记录与冻结配方不一致")
			}
			return nil
		}
	}
	return failure(409, "codex_runtime_snapshot_changed", "冻结配方缺少运行时记录")
}

func (m *Manager) prepareBuildRuntime(ctx context.Context, v ImageBuild, contextDir string, out io.Writer) ([]string, error) {
	snapshot := v.CodexRuntime
	if snapshot == nil {
		return nil, nil
	}
	if err := m.validateFrozenBuildRuntime(v); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	fmt.Fprintf(out, "Codex runtime: %s -> %s (%s, %s). %s\n", snapshot.RequestedMode, snapshot.Mode, snapshot.Version, snapshot.Platform, snapshot.Reason)
	args := []string{"--build-arg", "RD_CODEX_MODE=" + snapshot.Mode}
	switch snapshot.Mode {
	case "npm":
		return args, nil
	case "base":
		if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(snapshot.ImageID) {
			return nil, failure(409, "codex_runtime_snapshot_changed", "冻结基础镜像身份无效")
		}
		if _, err := m.inspectImage(snapshot.ImageID); err != nil {
			return nil, err
		}
		// No network, mounts, host credentials or writable root are available during
		// capability checks. The selected image ID, never its movable tag, is used.
		probe := `set -eu; command -v node; command -v npm; id node; node -e 'if(Number(process.versions.node.split(".")[0])<22)process.exit(1)'; observed=$(codex --version); test "${observed##* }" = "$1"; codex app-server --help >/dev/null`
		check, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if err := m.runtimeDockerCommand(check, out, "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "64", "--memory", "512m", "--cpus", "1", "--user", "0:0", "--entrypoint", "sh", snapshot.ImageID, "-c", probe, "rundesk-runtime-check", snapshot.Version); err != nil {
			return nil, fmt.Errorf("Codex 基础镜像不满足版本/Node 22+/npm/node 用户约定：%w", err)
		}
		return append(args, "--build-arg", "RD_BASE_IMAGE="+snapshot.ImageID), nil
	case "native":
		if !runtimeSHA.MatchString(snapshot.AssetSHA256) {
			return nil, failure(409, "codex_runtime_snapshot_changed", "冻结原生归档身份无效")
		}
		archive := m.runtimeArchivePath(snapshot.AssetSHA256)
		sha, size, err := runtimeArchiveDigest(archive, snapshot.Integrity)
		if err != nil {
			return nil, err
		}
		if sha != snapshot.AssetSHA256 || size != snapshot.AssetBytes {
			return nil, failure(409, "codex_runtime_cache_changed", "冻结 Codex 原生归档已变化")
		}
		payload := filepath.Join(contextDir, "runtime")
		info, err := os.Lstat(payload)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, failure(409, "codex_runtime_payload", "配方须提供普通 runtime/ 目录")
		}
		if _, err = os.Lstat(filepath.Join(payload, "native")); !os.IsNotExist(err) {
			return nil, failure(409, "codex_runtime_payload", "配方 runtime/native 已存在，拒绝覆盖")
		}
		temp, err := os.MkdirTemp(m.buildDir(v.ID), "native-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(temp)
		if err = extractCodexArchive(archive, temp); err != nil {
			return nil, err
		}
		target, _, _, err := codexPlatform("linux", runtime.GOARCH)
		if err != nil {
			return nil, err
		}
		platform := filepath.Join(temp, "vendor", target)
		found := false
		for _, rel := range []string{"bin/codex", "codex/codex"} {
			if info, err := os.Lstat(filepath.Join(platform, filepath.FromSlash(rel))); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
				found = true
			}
		}
		if !found {
			return nil, failure(409, "codex_runtime_payload", "已冻结归档缺少所需 Linux 平台的原生 Codex")
		}
		// Make packaged executable and companion files readable by USER node. The
		// extraction function rejects all links and special files before this walk.
		if err = filepath.WalkDir(temp, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if entry.IsDir() || info.Mode()&0111 != 0 {
				mode = 0755
			}
			return os.Chmod(path, mode)
		}); err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err = os.Rename(platform, filepath.Join(payload, "native")); err != nil {
			return nil, err
		}
		// Retain package-level license and provenance alongside the native payload.
		entries, err := os.ReadDir(temp)
		if err != nil {
			return nil, err
		}
		meta := filepath.Join(payload, "package")
		if err = os.Mkdir(meta, 0755); err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				if err = os.Rename(filepath.Join(temp, entry.Name()), filepath.Join(meta, entry.Name())); err != nil {
					return nil, err
				}
			}
		}
		fmt.Fprintf(out, "Reused verified native archive sha256:%s (%d bytes); no Codex download.\n", sha, size)
		return args, nil
	default:
		return nil, failure(409, "codex_runtime_snapshot_changed", "冻结运行时模式无效")
	}
}
func (s *Server) buildRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/build-center/runtime-options", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.Manager.BuildRuntimeOptions(strings.TrimSpace(r.URL.Query().Get("codexVersion")))
		respond(w, value, err)
	})
}
