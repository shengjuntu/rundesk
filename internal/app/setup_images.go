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
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/examples/profiletemplates"
)

type RuntimeImageInput struct {
	ProfileID    string `json:"profileId"`
	CodexVersion string `json:"codexVersion"`
	Source       string `json:"source"` // build, pull, or local
	Reference    string `json:"reference"`
}

var profileRelease = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// Expand the exact same contexts as examples/profiles.py without requiring
// Python/npm on the RunDesk host. Only a validated Codex version is substituted.
func runtimeImageContext(profile, version string) ([]byte, error) {
	if !profileRelease.MatchString(version) || len(version) > 64 || version == "0.0.0" {
		return nil, failure(400, "invalid_version", "请填写已选定的 Codex 精确版本 X.Y.Z")
	}
	switch profile {
	case "news2douyin", "people-search", "video-build":
	default:
		return nil, failure(400, "invalid_profile", "请选择内置运行环境")
	}
	data, err := profiletemplates.Files.ReadFile(profile + ".zip")
	if err != nil {
		return nil, err
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
			b = []byte(strings.ReplaceAll(string(b), "0.0.0", version))
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
	}
	if in.Source != "build" && in.Source != "pull" && in.Source != "local" {
		return ImageBuild{}, failure(400, "invalid_image_source", "镜像来源应为 build、pull 或 local")
	}
	in.Reference = strings.TrimSpace(in.Reference)
	if !imageReference.MatchString(in.Reference) || (in.Source == "build" && (strings.Contains(in.Reference, "@") || strings.HasPrefix(in.Reference, "sha256:"))) {
		return ImageBuild{}, failure(400, "invalid_image_reference", "请填写镜像标签；构建目标不能使用 Digest")
	}
	data, err := runtimeImageContext(in.ProfileID, in.CodexVersion)
	if err != nil {
		return ImageBuild{}, err
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

func runtimeImageMatches(v ImageBuild, image imageInspection) bool {
	labels := image.Config.Labels
	reach := "true"
	if v.ProfileID == "video-build" {
		reach = "false"
	}
	return labels["io.rundesk.profile"] == v.ProfileID &&
		labels["org.opencontainers.image.version"] == "1.1.0" &&
		labels["io.rundesk.agent-reach"] == reach &&
		labels["io.rundesk.codex.version"] == v.CodexVersion
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
	if !runtimeImageMatches(v, image) {
		return fmt.Errorf("镜像与所选配置或 Codex 版本不匹配；需要 RunDesk profile 1.1.0 的完整工具镜像")
	}
	fmt.Fprintln(out, "检查只读根目录、运行用户、Codex、工具版本、Skills 与 Exa 配置；不调用模型或公网服务。")
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	name := "rundesk-init-check-" + v.ID
	// Mirror managed execution restrictions. The UID is deliberately unprivileged.
	defer m.dockerRun(15*time.Second, "rm", "--force", name)
	err = m.runtimeDockerCommand(ctx, out, "run", "--name", name, "--rm", "--pull=never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", "1000:1000", "--memory", "512m", "--pids-limit", "128", "--tmpfs", "/tmp:rw,nosuid,nodev,size=128m,mode=1777", "--env", "HOME=/tmp", "--env", "CODEX_HOME=/tmp/codex", "--entrypoint", "python3", image.ID, "/opt/rundesk-profile/tools/profile_doctor.py", "--json")
	if err != nil {
		return fmt.Errorf("镜像已获取，但离线工具检查未通过，请查看日志：%w", err)
	}
	fmt.Fprintln(out, "镜像准备完成。模型认证、平台 Cookie、外部 MCP 和真实研究仍需在应用环境中检查。")
	return nil
}

func (s *Server) setupImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": []map[string]any{
			{"id": "news2douyin", "name": "新闻研究 · Exa + Agent-Reach", "reference": "rundesk-news2douyin:exa-agent-reach"},
			{"id": "people-search", "name": "公开人物研究 · Exa + Agent-Reach", "reference": "rundesk-people-search:exa-agent-reach"},
			{"id": "video-build", "name": "视频制作 · FFmpeg", "reference": "rundesk-video-build:local"},
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
