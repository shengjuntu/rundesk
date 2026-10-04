package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
	"github.com/shengjuntu/rundesk/internal/web"
)

type Server struct {
	Manager      *Manager
	Token        string
	cookie       string
	Local        bool
	PublicOrigin string
}

func NewHandler(m *Manager, token string, local bool, publicOrigin ...string) http.Handler {
	s := &Server{Manager: m, Token: token, cookie: store.ID() + store.ID(), Local: local}
	if len(publicOrigin) > 0 {
		s.PublicOrigin = publicOrigin[0]
	}
	mux := http.NewServeMux()
	s.extraRoutes(mux)
	s.instanceRoutes(mux)
	s.applicationRoutes(mux)
	s.skillBundleRoutes(mux)
	s.runtimeRoutes(mux)
	s.traceRoutes(mux)
	s.integrationRoutes(mux)
	s.messageRoutes(mux)
	s.recoveryRoutes(mux)
	s.queueRoutes(mux)
	s.scheduleRoutes(mux)
	s.keyRoutes(mux)
	s.environmentRoutes(mux)
	s.imageRoutes(mux)
	s.buildRoutes(mux)
	s.libraryRoutes(mux)
	s.collaborationRoutes(mux)
	s.userRoutes(mux)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if p := human(r); p != nil && p.LoginID != "" {
			if e := m.Store.Delete("user-login", p.LoginID); e != nil {
				respond(w, nil, e)
				return
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "rundesk", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/meta", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"name": "RunDesk", "version": Version, "apiVersions": []string{"v1"}, "demo": m.Demo, "protocol": "Codex App Server JSONL", "capabilities": []string{"instances", "sessions", "trace", "events", "approvals", "skills", "mcp", "notes", "files", "api-v1", "idempotency", "configuration-summary", "application-metadata", "reply-feedback", "applications", "skill-bundles", "task-recovery", "native-retry-status", "task-queue", "schedules", "application-credentials", "docker-environments", "image-catalog", "member-project-access", "personal-file-library", "collaboration", "a2a-0.3-jsonrpc", "gitea-blackboard"}})
	})
	mux.HandleFunc("GET /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		if k := principal(r); k != nil {
			items := []map[string]string{}
			for _, v := range m.Workspaces() {
				if k.workspace(v.ID) {
					items = append(items, map[string]string{"id": v.ID, "name": v.Name})
				}
			}
			writeJSON(w, 200, items)
			return
		}
		writeJSON(w, 200, m.Workspaces())
	})
	mux.HandleFunc("POST /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Name string
			Path string
		}
		if !decode(w, r, &v) {
			return
		}
		result, e := m.CreateWorkspace(v.Name, v.Path)
		respond(w, result, e)
	})
	mux.HandleFunc("PUT /api/workspaces/{wid}/notes", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Text     string
			Revision int
		}
		if !decode(w, r, &v) {
			return
		}
		result, e := m.Notes(r.PathValue("wid"), v.Text, v.Revision)
		respond(w, result, e)
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		ss := m.Sessions()
		q := r.URL.Query()
		filtered := ss[:0]
		for _, item := range ss {
			if !sessionVisible(r, item) {
				continue
			}
			if (q.Get("instanceId") == "" || item.InstanceID == q.Get("instanceId")) && (q.Get("workspaceId") == "" || item.WorkspaceID == q.Get("workspaceId")) && (q.Get("appId") == "" || item.Source.AppID == q.Get("appId")) && (q.Get("taskId") == "" || item.Source.TaskID == q.Get("taskId")) {
				filtered = append(filtered, item)
			}
		}
		ss = filtered
		sort.Slice(ss, func(i, j int) bool {
			if ss[i].Pinned != ss[j].Pinned {
				return ss[i].Pinned
			}
			return ss[i].Updated > ss[j].Updated
		})
		writeJSON(w, 200, ss)
	})
	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			WorkspaceID   string `json:"workspaceId"`
			InstanceID    string `json:"instanceId"`
			Title         string
			Model         string
			Source        SessionSource   `json:"source"`
			TraceAnalysis *TraceSelection `json:"traceAnalysis"`
		}
		if !decode(w, r, &v) {
			return
		}
		var result Session
		var e error
		if v.TraceAnalysis != nil {
			if v.WorkspaceID != "" || v.InstanceID != "" || v.Model != "" || v.Title != "" || v.Source.Kind != "" || v.Source.AppID != "" || v.Source.TaskID != "" {
				e = failure(400, "invalid_trace_analysis", "轨迹分析沿用来源的模型与配置，请勿同时指定其他配置")
			} else {
				result, e = m.CreateTraceAnalysis(*v.TraceAnalysis)
			}
		} else {
			result, e = m.CreateSessionWithSource(v.WorkspaceID, v.Title, v.Model, v.InstanceID, v.Source)
		}
		respond(w, result, e)
	})
	mux.HandleFunc("GET /api/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		result, e := m.Session(r.PathValue("sid"))
		respond(w, result, e)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/turns", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if !decode(w, r, &in) {
			return
		}
		in.LibraryOwner = personalOwner(r)
		s, e := m.Start(r.PathValue("sid"), in)
		if e != nil {
			writeErr(w, 409, e)
			return
		}
		writeJSON(w, 202, s)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/steer", func(w http.ResponseWriter, r *http.Request) {
		var in SteerInput
		if !decode(w, r, &in) {
			return
		}
		result, err := m.Steer(r.PathValue("sid"), in)
		if err != nil {
			writeErr(w, 409, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/stop", func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("RunDesk-API-Version") == "v1" {
			var input struct {
				ExpectedRunID string `json:"expectedRunId"`
			}
			if !decode(w, r, &input) {
				return
			}
			if input.ExpectedRunID == "" {
				writeErr(w, 400, failure(400, "expected_run_required", "停止任务需要 expectedRunId"))
				return
			}
			respond(w, map[string]bool{"ok": true}, m.StopRun(r.PathValue("sid"), input.ExpectedRunID))
			return
		}
		respond(w, map[string]bool{"ok": true}, m.Stop(r.PathValue("sid")))
	})
	mux.HandleFunc("GET /api/sessions/{sid}/events", s.events)
	mux.HandleFunc("GET /api/sessions/{sid}/approvals", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, m.Approvals(r.PathValue("sid"))) })
	mux.HandleFunc("POST /api/sessions/{sid}/approvals/{aid}", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Decision any
			Scope    string
			Answers  map[string]any
			Content  any
		}
		if !decode(w, r, &v) {
			return
		}
		e := m.Approve(r.PathValue("sid"), r.PathValue("aid"), v.Decision, v.Answers, v.Content, v.Scope)
		if e != nil {
			writeErr(w, 409, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/skills", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.Skills(r.PathValue("wid"), r.URL.Query().Get("instanceId"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/skills/{name}", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ReadSkill(r.PathValue("wid"), r.PathValue("name"), r.URL.Query().Get("instanceId"), r.URL.Query().Get("scope"))
		respond(w, map[string]string{"content": v}, e)
	})
	mux.HandleFunc("PUT /api/workspaces/{wid}/skills/{name}", func(w http.ResponseWriter, r *http.Request) {
		var v struct{ Content string }
		if !decode(w, r, &v) {
			return
		}
		respond(w, map[string]bool{"saved": true}, m.SaveSkill(r.PathValue("wid"), r.PathValue("name"), v.Content, r.URL.Query().Get("instanceId"), r.URL.Query().Get("scope")))
	})
	mux.HandleFunc("POST /api/workspaces/{wid}/skills/toggle", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Path    string
			Enabled bool
		}
		if !decode(w, r, &v) {
			return
		}
		respond(w, map[string]bool{"saved": true}, m.ToggleSkill(r.PathValue("wid"), v.Path, v.Enabled, r.URL.Query().Get("instanceId")))
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/mcp", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.MCP(r.PathValue("wid"), r.URL.Query().Get("instanceId"))
		respond(w, v, e)
	})
	mux.HandleFunc("PUT /api/workspaces/{wid}/mcp/{name}", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Version string
			Config  map[string]any
			Remove  bool
		}
		if !decode(w, r, &v) {
			return
		}
		result, e := m.SaveMCP(r.PathValue("wid"), r.PathValue("name"), v.Version, v.Config, v.Remove, r.URL.Query().Get("instanceId"))
		respond(w, result, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/models", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.ConfigCall(r.PathValue("wid"), "model/list", map[string]any{"limit": 100}, r.URL.Query().Get("instanceId"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/config", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.readConfig(r.PathValue("wid"), r.URL.Query().Get("instanceId"))
		respond(w, redact(v), e)
	})
	mux.HandleFunc("POST /api/workspaces/{wid}/uploads", s.upload)
	mux.HandleFunc("GET /api/workspaces/{wid}/file", s.file)
	mux.HandleFunc("GET /api/sessions/{sid}/files", s.files)
	mux.HandleFunc("GET /api/sessions/{sid}/export", s.export)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, 404, failure(404, "not_found", "API 路径或方法不存在"))
	})
	mux.Handle("/", http.FileServerFS(web.Files))
	return s.apiBoundary(s.guard(s.userGate(s.applicationGate(s.idempotent(mux)))))
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, e error) {
	writeAPIError(w, status, e)
}
func respond(w http.ResponseWriter, v any, e error) {
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	writeJSON(w, 200, v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		writeErr(w, 400, e)
		return false
	}
	var more any
	if e := d.Decode(&more); e != io.EOF {
		writeErr(w, 400, errors.New("只接受单个 JSON 对象"))
		return false
	}
	return true
}
func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob:; connect-src 'self'; media-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		configuredHost := ""
		if s.PublicOrigin != "" {
			u, _ := url.Parse(s.PublicOrigin)
			configuredHost = u.Host
		}
		if s.Local && r.Host != configuredHost && host != "localhost" && host != "127.0.0.1" && host != "::1" {
			writeErr(w, 403, errors.New("不允许的 Host"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			valid := e == nil && u.Host == r.Host && u.Scheme == scheme
			if s.PublicOrigin != "" {
				valid = origin == s.PublicOrigin && r.Host == configuredHost
			}
			if !valid {
				writeErr(w, 403, errors.New("跨来源请求被拒绝"))
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeErr(w, 403, errors.New("跨站请求被拒绝"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			var err error
			r, err = s.authorize(r)
			if err != nil {
				writeErr(w, 401, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var v struct{ Token string }
	if !decode(w, r, &v) {
		return
	}
	if strings.HasPrefix(v.Token, "rd_user_") {
		cookie, e := s.Manager.loginUser(v.Token)
		if e != nil {
			respond(w, nil, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "rundesk", Value: cookie, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(s.PublicOrigin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if s.Token != "" && !same(v.Token, s.Token) {
		writeErr(w, 401, errors.New("访问令牌不正确"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "rundesk", Value: s.cookie, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(s.PublicOrigin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sid")
	if _, e := s.Manager.Session(id); e != nil {
		writeErr(w, 404, e)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if v, e := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); e == nil && v > after {
		after = v
	}
	if after < 0 {
		after = 0
	}
	filt := filterFromRequest(r)
	if e := filt.Validate(); e != nil {
		writeErr(w, 400, e)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if r.URL.Query().Get("stream") != "1" {
		ev, e := s.Manager.Store.QueryEvents(id, after, limit, filt)
		respond(w, ev, e)
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, errors.New("streaming unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(w, ": connected\n\n")
	f.Flush()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	beat := 0
	for {
		if !s.credentialStillValid(r) {
			return
		}
		if _, e := s.Manager.Session(id); e != nil {
			return
		}
		ev, e := s.Manager.Store.QueryEvents(id, after, 250, filt)
		if e != nil {
			return
		}
		for _, v := range ev {
			b, _ := json.Marshal(v)
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, e = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", v.ID, b); e != nil {
				return
			}
			after = v.ID
		}
		if len(ev) > 0 {
			f.Flush()
			if len(ev) == 250 {
				continue
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.Manager.ctx.Done():
			return
		case <-tick.C:
			beat++
			if beat%50 == 0 {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, e = io.WriteString(w, ": heartbeat\n\n"); e != nil {
					return
				}
				f.Flush()
			}
		}
	}
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	ws, e := s.Manager.Workspace(r.PathValue("wid"))
	if e != nil {
		writeErr(w, 404, e)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 33<<20)
	if e = r.ParseMultipartForm(1 << 20); e != nil {
		writeErr(w, 400, e)
		return
	}
	defer r.MultipartForm.RemoveAll()
	if id := r.FormValue("libraryFileId"); id != "" {
		if personalOwner(r) == "" {
			respond(w, nil, failure(403, "personal_library_required", "个人文件仅供本人使用"))
			return
		}
		v, e := s.Manager.AttachPersonal(personalOwner(r), id, ws.ID)
		respond(w, v, e)
		return
	}
	f, head, e := r.FormFile("file")
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	defer f.Close()
	if head.Size > 32<<20 {
		writeErr(w, 400, errors.New("单文件最多 32 MiB"))
		return
	}
	name := filepath.Base(strings.ReplaceAll(head.Filename, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if len(name) > 160 {
		name = "upload" + filepath.Ext(name)
	}
	path := "uploads/" + store.ID() + "-" + name
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	defer root.Close()
	if e = root.MkdirAll("uploads", 0700); e != nil {
		writeErr(w, 400, e)
		return
	}
	out, e := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	n, e := io.Copy(out, io.LimitReader(f, (32<<20)+1))
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if n > 32<<20 {
		e = errors.New("文件过大")
	}
	if e != nil {
		_ = root.Remove(path)
		writeErr(w, 400, e)
		return
	}
	result := map[string]any{"path": path, "name": name, "size": n}
	if owner := personalOwner(r); owner != "" {
		f, e := s.Manager.saveUploaded(owner, ws.ID, path, name)
		if e != nil {
			_ = root.Remove(path)
			respond(w, nil, e)
			return
		}
		result["libraryFileId"] = f.ID
	}
	writeJSON(w, 201, result)
}
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if !safePath(p) || (!strings.HasPrefix(p, "uploads/") && !strings.HasPrefix(p, "outputs/")) {
		writeErr(w, 403, errors.New("无效文件路径"))
		return
	}
	ws, e := s.Manager.Workspace(r.PathValue("wid"))
	if e != nil {
		writeErr(w, 404, e)
		return
	}
	if e = s.Manager.libraryPathDeleted(ws.ID, p); e != nil {
		respond(w, nil, e)
		return
	}
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	defer root.Close()
	f, e := root.Open(p)
	if e != nil {
		writeErr(w, 404, e)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		writeErr(w, 400, errors.New("只支持普通文件"))
		return
	}
	ext := strings.ToLower(filepath.Ext(p))
	typ := mime.TypeByExtension(ext)
	inline := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".mp3": true, ".wav": true, ".mp4": true, ".txt": true, ".md": true, ".json": true, ".log": true}
	if r.URL.Query().Get("preview") == "1" && inline[ext] {
		if ext == ".md" || ext == ".json" || ext == ".log" || ext == ".txt" {
			typ = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	session, e := s.Manager.Session(r.PathValue("sid"))
	if e != nil {
		writeErr(w, 404, e)
		return
	}
	ws, e := s.Manager.Workspace(session.WorkspaceID)
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		writeErr(w, 400, e)
		return
	}
	defer root.Close()
	out := []map[string]any{}
	prefix := "outputs/" + session.ID
	e = fs.WalkDir(root.FS(), prefix, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == prefix {
			return nil
		}
		if err != nil {
			return err
		}
		if len(out) >= 1000 {
			return fs.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			if i.Mode().IsRegular() {
				out = append(out, map[string]any{"path": path, "name": d.Name(), "size": i.Size(), "modified": i.ModTime(), "deleted": s.Manager.libraryPathDeleted(ws.ID, path) != nil})
			}
		}
		return nil
	})
	respond(w, out, e)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sid")
	session, e := s.Manager.Session(id)
	if e != nil {
		writeErr(w, 404, e)
		return
	}
	if r.URL.Query().Get("format") == "markdown" {
		content, e := s.Manager.Markdown(id)
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="rundesk-conversation.md"`)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = io.WriteString(w, content)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="rundesk-events.jsonl"`)
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	if enc.Encode(map[string]any{"session": session}) != nil {
		return
	}
	var after int64
	for {
		events, e := s.Manager.Store.Events(id, after, 1000)
		if e != nil {
			return
		}
		for _, ev := range events {
			if enc.Encode(ev) != nil {
				return
			}
			after = ev.ID
		}
		if len(events) < 1000 {
			return
		}
	}
}
