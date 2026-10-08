package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

const buildUploadLimit = 32 << 20
const buildContextLimit = 128 << 20
const buildLogLimit = 4 << 20

type ImageBuild struct {
	ID             string `json:"id"`
	InstanceID     string `json:"instanceId"`
	Name           string `json:"name"`
	Dockerfile     string `json:"dockerfile"`
	Reference      string `json:"reference"`
	Status         string `json:"status"`
	Created        string `json:"created"`
	Updated        string `json:"updated"`
	Started        string `json:"started,omitempty"`
	Finished       string `json:"finished,omitempty"`
	ContextSHA256  string `json:"contextSha256"`
	ContextBytes   int64  `json:"contextBytes"`
	Files          int    `json:"files"`
	TimeoutMinutes int    `json:"timeoutMinutes"`
	NoCache        bool   `json:"noCache"`
	ImageID        string `json:"imageId,omitempty"`
	ImageVersionID string `json:"imageVersionId,omitempty"`
	Error          string `json:"error,omitempty"`
	LogTruncated   bool   `json:"logTruncated"`
}

func buildActive(s string) bool              { return s == "queued" || s == "running" || s == "canceling" }
func (m *Manager) buildDir(id string) string { return filepath.Join(m.Data, "image-builds", id) }
func (m *Manager) buildsLocked(iid string) ([]ImageBuild, error) {
	rows, e := m.Store.List("image-build")
	out := []ImageBuild{}
	if e != nil {
		return out, e
	}
	for _, b := range rows {
		var v ImageBuild
		if e = json.Unmarshal(b, &v); e != nil {
			return out, e
		}
		if iid == "" || v.InstanceID == iid {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}
func (m *Manager) Builds(iid string) ([]ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	if _, e := m.Instance(iid); e != nil {
		return nil, e
	}
	return m.buildsLocked(iid)
}
func (m *Manager) buildLocked(iid, id string) (ImageBuild, error) {
	var v ImageBuild
	e := m.Store.Get("image-build", id, &v)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && v.InstanceID != iid) {
		return v, failure(404, "build_not_found", "构建任务不存在")
	}
	return v, e
}
func (m *Manager) Build(iid, id string) (ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	return m.buildLocked(iid, id)
}
func (m *Manager) initBuilds() error {
	m.buildCancels = map[string]context.CancelFunc{}
	rows, e := m.buildsLocked("")
	if e != nil {
		return e
	}
	for _, v := range rows {
		if buildActive(v.Status) {
			v.Status = "interrupted"
			v.Error = "RunDesk 重启，构建状态未确认；不会自动重跑。Docker 缓存或镜像可能已生成，请检查后重新提交。"
			v.Finished = store.Now()
			v.Updated = v.Finished
			if e = m.Store.Put("image-build", v.ID, v); e != nil {
				return e
			}
			_ = os.RemoveAll(filepath.Join(m.buildDir(v.ID), "context"))
		}
	}
	// Uploads are staged outside persistent job directories. No server is sharing this data directory.
	entries, _ := os.ReadDir(filepath.Join(m.Data, "image-builds"))
	for _, x := range entries {
		if strings.HasPrefix(x.Name(), "upload-") {
			_ = os.RemoveAll(m.buildDir(x.Name()))
		}
	}
	return nil
}
func safeBuildPath(s string) bool {
	return s != "" && s != "." && !strings.ContainsAny(s, "\\:\x00\r\n") && !strings.HasPrefix(s, "/") && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../") && len(s) <= 512
}
func extractBuildContext(zipPath, dir, dockerfile string) (int, int64, error) {
	z, e := zip.OpenReader(zipPath)
	if e != nil {
		return 0, 0, failure(400, "invalid_build_zip", "构建目录必须为有效 ZIP")
	}
	defer z.Close()
	if len(z.File) > 4000 {
		return 0, 0, failure(413, "build_context_limit", "最多 4000 个文件或目录")
	}
	seen := map[string]bool{}
	count := 0
	var total int64
	for _, f := range z.File {
		n := strings.TrimSuffix(f.Name, "/")
		if !safeBuildPath(n) || seen[strings.ToLower(n)] || f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return 0, 0, failure(400, "unsafe_build_zip", "ZIP 含不安全路径、链接、特殊文件或重复路径")
		}
		seen[strings.ToLower(n)] = true
		dest := filepath.Join(dir, filepath.FromSlash(n))
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(dest, 0755); e != nil {
				return 0, 0, e
			}
			continue
		}
		if f.UncompressedSize64 > buildContextLimit || total+int64(f.UncompressedSize64) > buildContextLimit {
			return 0, 0, failure(413, "build_context_limit", "解压后最多 128 MiB")
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
			return 0, 0, e
		}
		r, e := f.Open()
		if e != nil {
			return 0, 0, e
		}
		mode := os.FileMode(0644)
		if f.Mode()&0111 != 0 {
			mode = 0755
		}
		w, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			r.Close()
			return 0, 0, failure(400, "invalid_build_zip", "ZIP 路径冲突")
		}
		size, copyErr := io.Copy(w, io.LimitReader(r, buildContextLimit-total+1))
		closeErr := w.Close()
		r.Close()
		if copyErr != nil {
			return 0, 0, failure(400, "invalid_build_zip", "ZIP 校验失败")
		}
		if closeErr != nil {
			return 0, 0, closeErr
		}
		total += size
		if total > buildContextLimit {
			return 0, 0, failure(413, "build_context_limit", "解压后最多 128 MiB")
		}
		count++
	}
	info, e := os.Stat(filepath.Join(dir, filepath.FromSlash(dockerfile)))
	if e != nil || !info.Mode().IsRegular() {
		return 0, 0, failure(400, "dockerfile_missing", "找不到指定 Dockerfile；路径相对于 ZIP 根目录")
	}
	return count, total, nil
}
func (m *Manager) CreateBuild(iid, name, dockerfile string, timeout int, noCache bool, src io.Reader) (ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	var v ImageBuild
	if runtime.GOOS != "linux" {
		return v, failure(400, "docker_platform_unsupported", "镜像构建仅支持 Linux 本机 Docker")
	}
	i, e := m.Instance(iid)
	if e != nil {
		return v, e
	}
	if i.ID == DefaultInstance {
		return v, failure(409, "not_docker_application", "请在专用应用中构建镜像")
	}
	name = strings.TrimSpace(name)
	if len(name) > 120 {
		return v, failure(400, "invalid_build_name", "构建名称最多 120 字节")
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if !safeBuildPath(dockerfile) {
		return v, failure(400, "invalid_dockerfile", "Dockerfile 必须是构建目录内的相对路径")
	}
	if timeout == 0 {
		timeout = 30
	}
	if timeout < 1 || timeout > 120 {
		return v, failure(400, "invalid_build_timeout", "超时范围为 1–120 分钟")
	}
	rows, e := m.buildsLocked("")
	if e != nil {
		return v, e
	}
	pending, total := 0, 0
	for _, x := range rows {
		if x.InstanceID == iid {
			total++
		}
		if x.Status == "draft" || buildActive(x.Status) {
			pending++
		}
	}
	if pending >= 20 || total >= 200 {
		return v, failure(409, "build_limit", "最多 20 个未完成构建，每应用最多 200 条记录；请先清理旧记录")
	}
	base := filepath.Join(m.Data, "image-builds")
	if e = os.MkdirAll(base, 0700); e != nil {
		return v, e
	}
	tmp, e := os.MkdirTemp(base, "upload-")
	if e != nil {
		return v, e
	}
	defer os.RemoveAll(tmp)
	f, e := os.Create(filepath.Join(tmp, "context.zip"))
	if e != nil {
		return v, e
	}
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, hash), io.LimitReader(src, buildUploadLimit+1))
	ce := f.Close()
	if e != nil {
		return v, e
	}
	if ce != nil {
		return v, ce
	}
	if n > buildUploadLimit {
		return v, failure(413, "build_upload_limit", "ZIP 最多 32 MiB")
	}
	files, size, e := extractBuildContext(filepath.Join(tmp, "context.zip"), filepath.Join(tmp, "context"), dockerfile)
	if e != nil {
		return v, e
	}
	if e = os.Remove(filepath.Join(tmp, "context.zip")); e != nil {
		return v, e
	}
	id := store.ID()
	if name == "" {
		name = "构建 " + id[:8]
	}
	v = ImageBuild{ID: id, InstanceID: iid, Name: name, Dockerfile: dockerfile, Reference: "rundesk-build/" + iid + ":" + id, Status: "draft", Created: store.Now(), Updated: store.Now(), ContextSHA256: hex.EncodeToString(hash.Sum(nil)), ContextBytes: size, Files: files, TimeoutMinutes: timeout, NoCache: noCache}
	if e = os.Rename(tmp, m.buildDir(id)); e != nil {
		return v, e
	}
	if e = m.Store.Put("image-build", id, v); e != nil {
		_ = os.RemoveAll(m.buildDir(id))
	}
	return v, e
}
func (m *Manager) StartBuild(iid, id string) (ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	v, e := m.buildLocked(iid, id)
	if e != nil {
		return v, e
	}
	if v.Status != "draft" {
		return v, nil
	}
	i, e := m.Instance(iid)
	if e != nil {
		return v, e
	}
	if i.ID == DefaultInstance {
		return v, failure(409, "not_docker_application", "请在专用应用中构建镜像")
	}
	v.Status = "queued"
	v.Updated = store.Now()
	return v, m.Store.Put("image-build", id, v)
}
func (m *Manager) CancelBuild(iid, id string) (ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	v, e := m.buildLocked(iid, id)
	if e != nil {
		return v, e
	}
	switch v.Status {
	case "draft", "queued":
		v.Status = "canceled"
		v.Finished = store.Now()
	case "running":
		v.Status = "canceling"
	case "canceling":
		return v, nil
	default:
		return v, nil
	}
	v.Updated = store.Now()
	if e = m.Store.Put("image-build", id, v); e != nil {
		return v, e
	}
	if cancel := m.buildCancels[id]; cancel != nil {
		cancel()
	} else {
		_ = os.RemoveAll(filepath.Join(m.buildDir(id), "context"))
	}
	return v, nil
}
func (m *Manager) DeleteBuild(iid, id string) error {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	v, e := m.buildLocked(iid, id)
	if e != nil {
		return e
	}
	if buildActive(v.Status) {
		return failure(409, "build_active", "请先取消构建并等待结束")
	}
	if e = os.RemoveAll(m.buildDir(id)); e != nil {
		return e
	}
	return m.Store.Delete("image-build", id)
}

type buildLogWriter struct {
	mu        sync.Mutex
	f         *os.File
	size      int
	truncated bool
}

func (w *buildLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := buildLogLimit - w.size
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	if len(p) > 0 {
		written, e := w.f.Write(p)
		w.size += written
		if e != nil {
			return written, e
		}
	}
	return n, nil
}
func (m *Manager) buildLoop() {
	defer m.wg.Done()
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.runNextBuild()
		}
	}
}
func (m *Manager) runNextBuild() {
	m.buildMu.Lock()
	rows, e := m.buildsLocked("")
	if e != nil {
		m.buildMu.Unlock()
		return
	}
	var v ImageBuild
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Status == "queued" {
			v = rows[i]
			break
		}
	}
	if v.ID == "" || m.ctx.Err() != nil {
		m.buildMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, time.Duration(v.TimeoutMinutes)*time.Minute)
	m.buildCancels[v.ID] = cancel
	v.Status = "running"
	v.Started = store.Now()
	v.Updated = v.Started
	if e = m.Store.Put("image-build", v.ID, v); e != nil {
		delete(m.buildCancels, v.ID)
		cancel()
		m.buildMu.Unlock()
		return
	}
	m.buildMu.Unlock()
	imageID, truncated, runErr := m.executeBuild(ctx, v)
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	defer cancel()
	delete(m.buildCancels, v.ID)
	latest, e := m.buildLocked(v.InstanceID, v.ID)
	if e != nil {
		log.Printf("build %s completion persistence: %v", v.ID, e)
		return
	}
	v = latest
	v.ImageID = imageID
	v.LogTruncated = truncated
	v.Finished = store.Now()
	v.Updated = v.Finished
	switch {
	case m.ctx.Err() != nil:
		v.Status = "interrupted"
		v.Error = "服务关闭，构建状态未确认；不会自动重跑。"
	case v.Status == "canceling":
		v.Status = "canceled"
		v.Error = "已取消客户端构建；Docker 端可能保留缓存或已生成镜像。"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		v.Status = "failed"
		v.Error = "构建超时；Docker 端可能保留缓存，请检查后重新提交。"
	case runErr != nil:
		v.Status = "failed"
		v.Error = diagnosticText(runErr.Error())
	default:
		// Register by immutable image ID, never by a tag another process can move.
		version, err := m.RegisterImage(v.InstanceID, imageID)
		if err != nil {
			v.Status = "failed"
			v.Error = "镜像已生成但登记失败：" + diagnosticText(err.Error())
		} else {
			v.Status = "succeeded"
			v.ImageVersionID = version.ID
		}
	}
	if e = m.Store.Put("image-build", v.ID, v); e != nil {
		log.Printf("build %s completion persistence: %v", v.ID, e)
		return
	}
	_ = os.RemoveAll(filepath.Join(m.buildDir(v.ID), "context"))
}
func (m *Manager) executeBuild(ctx context.Context, v ImageBuild) (string, bool, error) {
	endpoint := os.Getenv("RUNDESK_DOCKER_HOST")
	if endpoint != "" && (!strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\n\r\x00")) {
		return "", false, fmt.Errorf("仅支持本机 Docker Unix socket")
	}
	i, e := m.Instance(v.InstanceID)
	if e != nil {
		return "", false, e
	}
	if i.Execution.normalized().Mode != "docker" {
		return "", false, fmt.Errorf("应用已切换为本机执行，构建未启动")
	}
	dir := m.buildDir(v.ID)
	f, e := os.OpenFile(filepath.Join(dir, "build.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return "", false, e
	}
	defer f.Close()
	out := &buildLogWriter{f: f}
	iidfile := filepath.Join(dir, "image-id")
	_ = os.Remove(iidfile)
	args := []string{"build", "--builder", "default", "--load", "--progress=plain", "--platform", "linux/" + runtime.GOARCH, "--iidfile", iidfile, "--tag", v.Reference, "--file", filepath.Join(dir, "context", filepath.FromSlash(v.Dockerfile))}
	if v.NoCache {
		args = append(args, "--no-cache")
	}
	args = append(args, filepath.Join(dir, "context"))
	cmd := m.docker.Command(args...)
	cmd.Env = append(cmd.Env, "DOCKER_BUILDKIT=1")
	cmd.Stdout = out
	cmd.Stderr = out
	prepareBuildCommand(cmd)
	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}
	if e = cmd.Start(); e != nil {
		return "", false, fmt.Errorf("无法启动 Docker 构建：%w", e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case e = <-done:
	case <-ctx.Done():
		interruptBuildCommand(cmd)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			killBuildCommand(cmd)
			<-done
		}
		e = ctx.Err()
	}
	if e != nil {
		return "", out.truncated, fmt.Errorf("Docker 构建失败，请查看日志：%w", e)
	}
	b, e := os.ReadFile(iidfile)
	if e != nil {
		return "", out.truncated, fmt.Errorf("Docker 未返回镜像身份：%w", e)
	}
	id := strings.TrimSpace(string(b))
	if len(id) != 71 || !strings.HasPrefix(id, "sha256:") {
		return "", out.truncated, fmt.Errorf("Docker 返回无效镜像身份")
	}
	if _, e = hex.DecodeString(id[7:]); e != nil {
		return "", out.truncated, e
	}
	return id, out.truncated, nil
}
func (m *Manager) BuildLog(iid, id string, after int64) (any, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	v, e := m.buildLocked(iid, id)
	if e != nil {
		return nil, e
	}
	if after < 0 || after > buildLogLimit {
		return nil, failure(400, "invalid_log_offset", "日志偏移无效")
	}
	f, e := os.Open(filepath.Join(m.buildDir(id), "build.log"))
	if os.IsNotExist(e) {
		return map[string]any{"text": "", "next": 0, "status": v.Status, "truncated": v.LogTruncated}, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if _, e = f.Seek(after, io.SeekStart); e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(f, 64<<10))
	return map[string]any{"text": strings.ToValidUTF8(string(b), "�"), "next": after + int64(len(b)), "status": v.Status, "truncated": v.LogTruncated}, e
}
func (s *Server) buildRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/instances/{iid}/builds", func(w http.ResponseWriter, r *http.Request) { v, e := m.Builds(r.PathValue("iid")); respond(w, v, e) })
	mux.HandleFunc("POST /api/instances/{iid}/builds", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, buildUploadLimit+(1<<20))
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			respond(w, nil, failure(400, "invalid_build_upload", "请上传 ZIP，最大 32 MiB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, _, e := r.FormFile("file")
		if e != nil {
			respond(w, nil, failure(400, "build_file_required", "请选择构建目录 ZIP"))
			return
		}
		defer f.Close()
		timeout := 0
		if x := r.FormValue("timeoutMinutes"); x != "" {
			timeout, e = strconv.Atoi(x)
			if e != nil {
				respond(w, nil, failure(400, "invalid_build_timeout", "超时必须为整数分钟"))
				return
			}
		}
		v, e := m.CreateBuild(r.PathValue("iid"), r.FormValue("name"), r.FormValue("dockerfile"), timeout, r.FormValue("noCache") == "true", f)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/instances/{iid}/builds/{bid}", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.Build(r.PathValue("iid"), r.PathValue("bid"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/instances/{iid}/builds/{bid}/start", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.StartBuild(r.PathValue("iid"), r.PathValue("bid"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/instances/{iid}/builds/{bid}/cancel", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.CancelBuild(r.PathValue("iid"), r.PathValue("bid"))
		respond(w, v, e)
	})
	mux.HandleFunc("DELETE /api/instances/{iid}/builds/{bid}", func(w http.ResponseWriter, r *http.Request) {
		e := m.DeleteBuild(r.PathValue("iid"), r.PathValue("bid"))
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("GET /api/instances/{iid}/builds/{bid}/log", func(w http.ResponseWriter, r *http.Request) {
		var after int64
		var e error
		if x := r.URL.Query().Get("after"); x != "" {
			after, e = strconv.ParseInt(x, 10, 64)
		}
		if e != nil {
			respond(w, nil, failure(400, "invalid_log_offset", "日志偏移无效"))
			return
		}
		v, e := m.BuildLog(r.PathValue("iid"), r.PathValue("bid"), after)
		respond(w, v, e)
	})
}
