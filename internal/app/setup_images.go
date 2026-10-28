package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/examples/profiletemplates"
)

type RuntimeImageInput struct {
	CodexRuntime *BuildRuntimeOptions `json:"codexRuntime,omitempty"`
	runtimeSnapshot *BuildRuntimeSnapshot
	ProfileID       string `json:"profileId"`
	CodexVersion    string `json:"codexVersion"`
	Source          string `json:"source"` // build, pull, or local
	Reference       string `json:"reference"`
	PublicImageID   string `json:"publicImageId,omitempty"`
	PublicSourceID  string `json:"publicSourceId,omitempty"`
	CatalogRevision string `json:"catalogRevision,omitempty"`
	RequestID       string `json:"requestId,omitempty"`
	requestSHA      string
	publicImage     *PublicImageSnapshot
}

var profileRelease = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// Read a fresh external recipe snapshot without requiring Python/npm on the
// RunDesk host. A submitted build freezes the returned context, not this folder.
func runtimeImageContext(profile, version string) ([]byte, error) {
	if !profileRelease.MatchString(version) || len(version) > 64 || version == "0.0.0" {
		return nil, failure(400, "invalid_version", "请填写已选定的 Codex 精确版本 X.Y.Z")
	}
	switch profile {
	case "news2douyin", "people-search", "video-build", "hyperframes", "video-use", "openmontage":
	default:
		return nil, failure(400, "invalid_profile", "请选择内置运行环境")
	}
	data, err := profiletemplates.Files.ReadFile(profile + ".zip")
	if err != nil {
		return nil, err
	}
	return expandRuntimeImageContext(data, version)
}

// Expand a single already-read snapshot. Callers that inspect recipe metadata
// first can use these exact same bytes and avoid a second read of editable files.
func expandRuntimeImageContext(data []byte, version string) ([]byte, error) {
	if !profileRelease.MatchString(version) || len(version) > 64 || version == "0.0.0" {
		return nil, failure(400, "invalid_version", "请填写已选定的 Codex 精确版本 X.Y.Z")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	modes := map[string]os.FileMode{}
	for _, f := range z.File {
		if f.Name == "SHA256SUMS" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		if f.Name == "Dockerfile" {
			// The editable Dockerfile remains ordinary Docker syntax. Bind the
			// selected version even if its on-disk default was edited previously.
			dockerfile := string(b)
			argument := regexp.MustCompile(`(?m)^ARG CODEX_VERSION=[^\r\n]*$`)
			label := regexp.MustCompile(`(?m)^LABEL io\.rundesk\.codex\.version=[^\r\n]*$`)
			if !argument.MatchString(dockerfile) || !label.MatchString(dockerfile) {
				return nil, failure(400, "invalid_recipe_codex", "内置配方须保留 CODEX_VERSION 参数和 Codex 版本标签；任意 Dockerfile 请导入为自定义配方")
			}
			dockerfile = argument.ReplaceAllString(dockerfile, "ARG CODEX_VERSION="+version)
			b = []byte(label.ReplaceAllString(dockerfile, "LABEL io.rundesk.codex.version="+strconv.Quote(version)))
		}
		if f.Name == "profile.lock.json" {
			var lock map[string]any
			if err = json.Unmarshal(b, &lock); err != nil {
				return nil, err
			}
			lock["codexVersion"] = version
			b, err = json.MarshalIndent(lock, "", "  ")
			if err != nil {
				return nil, err
			}
			b = append(b, '\n')
		}
		files[f.Name], modes[f.Name] = b, f.Mode()
	}
	names := make([]string, 0, len(files)+1)
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
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(modes[name])
		f, err := w.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (m *Manager) PrepareRuntimeImage(in RuntimeImageInput) (ImageBuild, error) {
	if in.Source == "" {
		in.Source = "build"
		if in.PublicImageID != "" {
			in.Source = "pull"
		}
	}
	if in.Source != "build" && in.Source != "pull" && in.Source != "local" {
		return ImageBuild{}, failure(400, "invalid_image_source", "镜像来源应为 build、pull 或 local")
	}
	in.Reference = strings.TrimSpace(in.Reference)
	if in.RequestID != "" {
		if !safeRequestID.MatchString(in.RequestID) {
			return ImageBuild{}, failure(400, "invalid_request_id", "请求编号须为 8–128 位字母、数字或安全分隔符")
		}
		data, _ := json.Marshal(in)
		in.requestSHA = fmt.Sprintf("%x", sha256.Sum256(data))
		m.buildMu.Lock()
		previous, found, err := m.replayRuntimeImageLocked(&in)
		m.buildMu.Unlock()
		if err != nil || found {
			return previous, err
		}
	}
	if err := m.resolvePublicImage(&in); err != nil {
		return ImageBuild{}, err
	}
	if !imageReference.MatchString(in.Reference) || (in.Source == "build" && (strings.Contains(in.Reference, "@") || strings.HasPrefix(in.Reference, "sha256:"))) {
		return ImageBuild{}, failure(400, "invalid_image_reference", "请填写镜像标签；构建目标不能使用 Digest")
	}
	data, err := runtimeImageContext(in.ProfileID, in.CodexVersion)
	if err != nil {
		return ImageBuild{}, err
	}
	if in.Source == "build" {
		in.runtimeSnapshot, err = m.resolveBuildRuntime(in.CodexRuntime, data, "Dockerfile", in.CodexVersion, nil, true)
		if err != nil { return ImageBuild{}, err }
		data, err = freezeBuildRuntime(data, in.runtimeSnapshot)
		if err != nil { return ImageBuild{}, err }
	} else if in.CodexRuntime != nil {
		return ImageBuild{}, failure(400, "codex_runtime_acquisition", "下载或使用已有完整镜像时不能选择构建运行时")
	}
	return m.createBuild(DefaultInstance, in.ProfileID+" · "+in.Source+" · "+in.CodexVersion, "Dockerfile", 60, false, bytes.NewReader(data), &in)
}

// All long-running operations execute on the persistent serial build queue.
func (m *Manager) runtimeDockerCommand(ctx context.Context, out io.Writer, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := m.docker.Command(args...)
	cmd.Stdout, cmd.Stderr = out, out
	prepareBuildCommand(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		interruptBuildCommand(cmd)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			killBuildCommand(cmd)
			<-done
		}
		return ctx.Err()
	}
}

const runtimeHyperframesLockSHA256 = "b3f48e9687cd27dea097203e928e6e7fd84344b88122ab2bb546d38b48705de1"

func runtimeImageMatches(v ImageBuild, image imageInspection) bool {
	if v.RuntimeIdentity != nil {
		return frozenRuntimeImageMatches(v, image)
	}
	labels := image.Config.Labels
	reach := "true"
	if v.ProfileID == "video-build" || independentVideoProfile(v.ProfileID) {
		reach = "false"
	}
	videoTools := true
	if v.ProfileID == "video-build" {
		videoTools = labels["io.rundesk.hyperframes.version"] == "0.8.87" &&
			labels["io.rundesk.gsap.version"] == "3.15.0" &&
			labels["io.rundesk.hyperframes.lock"] == runtimeHyperframesLockSHA256 &&
			(labels["io.rundesk.video-use.enabled"] == "false" || labels["io.rundesk.video-use.enabled"] == "true" && labels["io.rundesk.video-use.commit"] == "43cfc566833548093455c9d4a46bd69913b7e552" && labels["io.rundesk.video-use.adapter"] == "2")
	}
	if independentVideoProfile(v.ProfileID) {
		expected, ok := independentRuntimeIdentity(v.ProfileID)
		videoTools = ok && labels["io.rundesk.upstream.repository"] == expected.Repository &&
			labels["io.rundesk.upstream.commit"] == expected.Commit &&
			labels["io.rundesk.upstream.source.sha256"] == expected.SourceSHA256 &&
			labels["io.rundesk.video-app-media.contract"] == "1"
	}
	return videoTools && labels["io.rundesk.profile"] == v.ProfileID &&
		labels["org.opencontainers.image.version"] == runtimeProfileVersion(v.ProfileID) &&
		labels["io.rundesk.agent-reach"] == reach &&
		labels["io.rundesk.codex.version"] == v.CodexVersion
}

func independentVideoProfile(profile string) bool {
	return profile == "hyperframes" || profile == "video-use" || profile == "openmontage"
}

type independentImageIdentity struct {
	Repository   string `json:"repository"`
	Commit       string `json:"commit"`
	SourceSHA256 string `json:"sourceSHA256"`
	Version      string `json:"version"`
}

// Image identity comes from the same external source pin as the build recipe.
// A correctly named image from another upstream revision is not interchangeable.
func independentRuntimeIdentity(profile string) (independentImageIdentity, bool) {
	var lock struct {
		Profile struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"profile"`
		Upstream independentImageIdentity `json:"upstream"`
	}
	err := readRuntimeProfileLock(profile, &lock)
	if err != nil || lock.Profile.ID != profile || !profileRelease.MatchString(lock.Profile.Version) || len(lock.Upstream.Commit) != 40 || len(lock.Upstream.SourceSHA256) != 64 {
		return independentImageIdentity{}, false
	}
	lock.Upstream.Version = lock.Profile.Version
	return lock.Upstream, true
}

func readRuntimeProfileLock(profile string, value any) error {
	directory, err := profiletemplates.ProfileDirectory(profile)
	if err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(directory, "profile.lock.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(io.LimitReader(file, 256<<10)).Decode(value)
}

func runtimeProfileVersion(profile string) string {
	var lock struct {
		Profile struct {
			Version string `json:"version"`
		} `json:"profile"`
	}
	if readRuntimeProfileLock(profile, &lock) != nil {
		return ""
	}
	return lock.Profile.Version
}

func (m *Manager) cachedRuntimeImage(v ImageBuild) string {
	rows, err := m.Builds(DefaultInstance)
	if err != nil {
		return ""
	}
	for _, previous := range rows {
		if previous.Scope != "setup" || previous.Status != "succeeded" || !previous.OfflineReady || previous.ContextSHA256 != v.ContextSHA256 || previous.Reference != v.Reference {
			continue
		}
		image, err := m.inspectImage(v.Reference)
		if err == nil && image.ID == previous.ImageID && runtimeImageMatches(v, image) {
			return image.ID
		}
		return ""
	}
	return ""
}

func (m *Manager) acquireRuntimeImage(ctx context.Context, v ImageBuild, out io.Writer) (string, error) {
	image, err := m.inspectImage(v.Reference)
	if v.Source == "pull" && (err != nil || !runtimeImageMatches(v, image)) {
		fmt.Fprintf(out, "拉取 %s (linux/%s)\n", v.Reference, runtime.GOARCH)
		// Credentials, if needed, come from the service account's Docker config.
		if err = m.runtimeDockerCommand(ctx, out, "pull", "--platform", "linux/"+runtime.GOARCH, v.Reference); err != nil {
			return "", fmt.Errorf("镜像拉取失败，请检查仓库地址、网络、登录权限或磁盘空间：%w", err)
		}
		image, err = m.inspectImage(v.Reference)
	} else if err == nil {
		fmt.Fprintln(out, "复用本机镜像，继续离线工具检查。")
	}
	if err != nil {
		return "", err
	}
	return image.ID, m.verifyRuntimeImage(ctx, v, image.ID, out)
}

func (m *Manager) verifyRuntimeImage(parent context.Context, v ImageBuild, id string, out io.Writer) error {
	image, err := m.inspectImage(id)
	if err != nil {
		return err
	}
	if v.PublicImage != nil {
		matched := false
		for _, digest := range image.RepoDigests {
			if canonicalImageDigestReference(digest) == canonicalImageDigestReference(v.PublicImage.Reference) {
				matched = true
			}
		}
		if !matched {
			return failure(409, "public_image_digest_mismatch", "本机镜像的仓库 Digest 与所选公共版本不一致")
		}
	}
	if !runtimeImageMatches(v, image) {
		return fmt.Errorf("镜像与所选配置或 Codex 版本不匹配；需要 RunDesk %s profile %s 的完整工具镜像", v.ProfileID, runtimeProfileVersion(v.ProfileID))
	}
	fmt.Fprintln(out, "检查只读根目录、运行用户、Codex、工具版本、Skills 与 Exa 配置；不调用模型或公网服务。")
	timeout, memory, temporary, pids := 3*time.Minute, "512m", "/tmp:rw,nosuid,nodev,size=128m,mode=1777", "128"
	if v.ProfileID == "video-build" || independentVideoProfile(v.ProfileID) {
		// The custom builder renders browser frames/audio; independent profiles run their native tool and source checks.
		timeout, memory, temporary, pids = 8*time.Minute, "2048m", "/tmp:rw,nosuid,nodev,size=768m,mode=1777", "256"
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	name := "rundesk-init-check-" + v.ID
	// Mirror managed execution restrictions. The UID is deliberately unprivileged.
	defer m.dockerRun(15*time.Second, "rm", "--force", name)
	err = m.runtimeDockerCommand(ctx, out, "run", "--name", name, "--rm", "--pull=never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", "1000:1000", "--memory", memory, "--pids-limit", pids, "--tmpfs", temporary, "--env", "HOME=/tmp", "--env", "CODEX_HOME=/tmp/codex", "--entrypoint", "python3", image.ID, "/opt/rundesk-profile/tools/profile_doctor.py", "--json")
	if err != nil {
		return fmt.Errorf("镜像已获取，但离线工具检查未通过，请查看日志：%w", err)
	}
	fmt.Fprintln(out, "镜像准备完成。模型认证、平台 Cookie、外部 MCP 和真实研究仍需在应用环境中检查。")
	return nil
}

func (s *Server) setupImageRoutes(mux *http.ServeMux) {
	s.publicImageRoutes(mux)
	mux.HandleFunc("GET /api/setup/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": []map[string]any{
			{"id": "news2douyin", "name": "新闻研究 · Exa + Agent-Reach", "reference": "rundesk-news2douyin:exa-agent-reach"},
			{"id": "people-search", "name": "公开人物研究 · Exa + Agent-Reach", "reference": "rundesk-people-search:exa-agent-reach"},
			{"id": "video-build", "name": "文字驱动视频 · HyperFrames + FFmpeg", "reference": "rundesk-video-build:local"},
			{"id": "hyperframes", "name": "HyperFrames · 上游原生制作", "reference": "rundesk-hyperframes:local"},
			{"id": "video-use", "name": "video-use · 上游原生剪辑", "reference": "rundesk-video-use:local"},
			{"id": "openmontage", "name": "OpenMontage · 上游原生制作", "reference": "rundesk-openmontage:local"},
		}, "platform": "linux/" + runtime.GOARCH, "supported": runtime.GOOS == "linux", "modelTested": false})
	})
	mux.HandleFunc("POST /api/setup/runtime-images", func(w http.ResponseWriter, r *http.Request) {
		var in RuntimeImageInput
		if !decode(w, r, &in) {
			return
		}
		v, err := s.Manager.PrepareRuntimeImage(in)
		respond(w, v, err)
	})
}
