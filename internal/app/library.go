package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

const personalFileLimit int64 = 512 << 20

type PersonalFile struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Created   string `json:"created"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId,omitempty"`
	Deleted   bool   `json:"deleted"`
}
type libraryLink struct {
	FileID      string `json:"fileId"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
}
type libraryRun struct {
	Owner     string            `json:"owner"`
	SessionID string            `json:"sessionId"`
	RunID     string            `json:"runId"`
	Before    map[string]string `json:"before"`
	Done      bool              `json:"done"`
}

func personalOwner(r *http.Request) string {
	if u := human(r); u != nil {
		return u.User.ID
	}
	if principal(r) != nil {
		return ""
	}
	return "administrator"
}
func libraryLinkID(wid, p string) string {
	s := sha256.Sum256([]byte(wid + "/" + p))
	return hex.EncodeToString(s[:])
}
func (m *Manager) personalFile(id string) (PersonalFile, error) {
	var f PersonalFile
	e := m.Store.Get("personal-file", id, &f)
	return f, e
}
func (m *Manager) ownedFile(owner, id string) (PersonalFile, error) {
	f, e := m.personalFile(id)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && (owner == "" || f.Owner != owner)) {
		return f, failure(404, "personal_file_not_found", "文件不存在")
	}
	if e != nil {
		return f, e
	}
	if f.Deleted {
		return f, failure(410, "personal_file_deleted", "文件已删除")
	}
	return f, nil
}
func (m *Manager) libraryPath(f PersonalFile) string {
	return filepath.Join(m.Data, "personal-files", f.Owner, f.ID)
}
func (m *Manager) PersonalFiles(owner, q string) ([]PersonalFile, error) {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	rows, e := m.Store.List("personal-file")
	out := []PersonalFile{}
	if e != nil {
		return out, e
	}
	q = strings.ToLower(q)
	for _, b := range rows {
		var f PersonalFile
		if e = json.Unmarshal(b, &f); e != nil {
			return out, e
		}
		if f.Owner == owner && !f.Deleted && strings.Contains(strings.ToLower(f.Name), q) {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}
func cleanPersonalName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || len(name) > 160 {
		return "file" + filepath.Ext(name)
	}
	return name
}
func (m *Manager) savePersonalLocked(owner, name, kind, sid, key string, src io.Reader) (PersonalFile, error) {
	var f PersonalFile
	if key != "" {
		var link libraryLink
		if e := m.Store.Get("library-capture", key, &link); e == nil {
			return m.personalFile(link.FileID)
		} else if !errors.Is(e, sql.ErrNoRows) {
			return f, e
		}
	}
	rows, e := m.Store.List("personal-file")
	if e != nil {
		return f, e
	}
	count := 0
	var used int64
	for _, b := range rows {
		var x PersonalFile
		if e = json.Unmarshal(b, &x); e != nil {
			return f, e
		}
		if x.Owner == owner && !x.Deleted {
			count++
			used += x.Size
		}
	}
	if count >= 10000 || used >= 10<<30 {
		return f, failure(413, "personal_library_full", "个人文件库已满，请删除不需要的文件（最多 10000 个、10 GiB）")
	}
	f = PersonalFile{ID: store.ID(), Owner: owner, Name: cleanPersonalName(name), Created: store.Now(), Kind: kind, SessionID: sid}
	dir := filepath.Dir(m.libraryPath(f))
	if e = os.MkdirAll(dir, 0700); e != nil {
		return f, e
	}
	out, e := os.OpenFile(m.libraryPath(f), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return f, e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(out, h), io.LimitReader(src, personalFileLimit+1))
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if n > personalFileLimit || used+n > 10<<30 {
		e = failure(413, "personal_file_limit", "文件超过 512 MiB 或个人库超过 10 GiB")
	}
	if e != nil {
		os.Remove(m.libraryPath(f))
		return f, e
	}
	f.Size = n
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	if e = m.Store.Put("personal-file", f.ID, f); e != nil {
		os.Remove(m.libraryPath(f))
		return f, e
	}
	if key != "" {
		e = m.Store.Put("library-capture", key, libraryLink{FileID: f.ID})
	}
	return f, e
}
func (m *Manager) saveUploaded(owner, wid, p, name string) (PersonalFile, error) {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	ws, e := m.Workspace(wid)
	if e != nil {
		return PersonalFile{}, e
	}
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		return PersonalFile{}, e
	}
	defer root.Close()
	src, e := root.Open(p)
	if e != nil {
		return PersonalFile{}, e
	}
	defer src.Close()
	f, e := m.savePersonalLocked(owner, name, "upload", "", "", src)
	if e == nil {
		e = m.Store.Put("library-link", libraryLinkID(wid, p), libraryLink{FileID: f.ID, WorkspaceID: wid, Path: p})
	}
	return f, e
}
func (m *Manager) AttachPersonal(owner, id, wid string) (map[string]any, error) {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	f, e := m.ownedFile(owner, id)
	if e != nil {
		return nil, e
	}
	ws, e := m.Workspace(wid)
	if e != nil {
		return nil, e
	}
	src, e := os.Open(m.libraryPath(f))
	if e != nil {
		return nil, e
	}
	defer src.Close()
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	if e = root.MkdirAll("uploads", 0700); e != nil {
		return nil, e
	}
	p := "uploads/" + store.ID() + "-" + f.Name
	out, e := root.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	_, e = io.Copy(out, src)
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		root.Remove(p)
		return nil, e
	}
	e = m.Store.Put("library-link", libraryLinkID(wid, p), libraryLink{FileID: f.ID, WorkspaceID: wid, Path: p})
	if e != nil {
		root.Remove(p)
		return nil, e
	}
	return map[string]any{"name": f.Name, "size": f.Size, "path": p, "libraryFileId": f.ID}, nil
}
func (m *Manager) libraryPathDeleted(wid, p string) error {
	var link libraryLink
	e := m.Store.Get("library-link", libraryLinkID(wid, p), &link)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	f, e := m.personalFile(link.FileID)
	if e != nil {
		return e
	}
	if f.Deleted {
		return failure(410, "personal_file_deleted", "文件已删除")
	}
	return nil
}
func (m *Manager) DeletePersonal(owner, id string) error {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	f, e := m.personalFile(id)
	if errors.Is(e, sql.ErrNoRows) || e == nil && f.Owner != owner {
		return failure(404, "personal_file_not_found", "文件不存在")
	}
	if e != nil {
		return e
	}
	f.Deleted = true
	if e = m.Store.Put("personal-file", id, f); e != nil {
		return e
	}
	if e = os.Remove(m.libraryPath(f)); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}
func (m *Manager) outputHashes(s Session) (map[string]string, error) {
	ws, e := m.Workspace(s.WorkspaceID)
	if e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	out := map[string]string{}
	prefix := "outputs/" + s.ID
	e = fs.WalkDir(root.FS(), prefix, func(p string, d fs.DirEntry, e error) error {
		if errors.Is(e, fs.ErrNotExist) && p == prefix {
			return nil
		}
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if len(out) >= 1000 {
			return failure(413, "personal_output_limit", "单次产物超过 1000 个文件")
		}
		if info.Size() > personalFileLimit {
			return failure(413, "personal_file_limit", "产物超过 512 MiB，未自动入库："+d.Name())
		}
		f, e := root.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, io.LimitReader(f, personalFileLimit+1))
		f.Close()
		if e != nil {
			return e
		}
		out[p] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return out, e
}
func (m *Manager) preparePersonalRun(s Session, run, owner string) error {
	if owner == "" {
		return nil
	}
	before, e := m.outputHashes(s)
	if e != nil {
		return e
	}
	return m.Store.Put("library-run", run, libraryRun{Owner: owner, SessionID: s.ID, RunID: run, Before: before})
}
func (m *Manager) capturePersonalRun(s Session) {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	var r libraryRun
	if e := m.Store.Get("library-run", s.RunID, &r); e != nil || r.Done {
		return
	}
	fail := func(e error) {
		m.event(s.ID, "internal", "library/error", map[string]string{"runId": s.RunID, "error": "文件自动保存失败：" + e.Error()})
	}
	hashes, e := m.outputHashes(s)
	if e != nil {
		fail(e)
		return
	}
	ws, e := m.Workspace(s.WorkspaceID)
	if e != nil {
		fail(e)
		return
	}
	root, e := os.OpenRoot(ws.Path)
	if e != nil {
		fail(e)
		return
	}
	defer root.Close()
	for p, hash := range hashes {
		if r.Before[p] == hash {
			continue
		}
		src, e := root.Open(p)
		if e != nil {
			fail(e)
			return
		}
		key := libraryLinkID(r.Owner, s.RunID+"/"+p+"/"+hash)
		f, e := m.savePersonalLocked(r.Owner, filepath.Base(p), "generated", s.ID, key, src)
		src.Close()
		if e != nil {
			fail(e)
			return
		}
		if e = m.Store.Put("library-link", libraryLinkID(s.WorkspaceID, p), libraryLink{FileID: f.ID, WorkspaceID: s.WorkspaceID, Path: p}); e != nil {
			fail(e)
			return
		}
	}
	r.Done = true
	if e = m.Store.Put("library-run", s.RunID, r); e != nil {
		fail(e)
	}
}
func (m *Manager) capturePendingPersonal() {
	rows, e := m.Store.List("library-run")
	if e != nil {
		return
	}
	for _, b := range rows {
		var r libraryRun
		if json.Unmarshal(b, &r) != nil || r.Done {
			continue
		}
		s, e := m.Session(r.SessionID)
		if e == nil && s.RunID == r.RunID && !active(s.Status) {
			m.capturePersonalRun(s)
		}
	}
}
func (m *Manager) deletedPersonalRefs(owner string) ([]libraryLink, error) {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	rows, e := m.Store.List("library-link")
	out := []libraryLink{}
	if e != nil {
		return out, e
	}
	for _, b := range rows {
		var link libraryLink
		if e = json.Unmarshal(b, &link); e != nil {
			return out, e
		}
		f, e := m.personalFile(link.FileID)
		if e != nil {
			return out, e
		}
		if f.Owner == owner && f.Deleted {
			out = append(out, link)
		}
	}
	return out, nil
}
func (s *Server) libraryRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/library/deleted-references", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.deletedPersonalRefs(personalOwner(r))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/library", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.PersonalFiles(personalOwner(r), r.URL.Query().Get("q"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/library", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 33<<20)
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			respond(w, nil, failure(400, "invalid_upload", "请上传文件，最大 32 MiB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, e := r.FormFile("file")
		if e != nil {
			respond(w, nil, e)
			return
		}
		defer f.Close()
		if h.Size > 32<<20 {
			respond(w, nil, failure(413, "upload_limit", "上传最多 32 MiB"))
			return
		}
		m.libraryMu.Lock()
		v, e := m.savePersonalLocked(personalOwner(r), h.Filename, "upload", "", "", f)
		m.libraryMu.Unlock()
		respond(w, v, e)
	})
	mux.HandleFunc("DELETE /api/library/{fid}", func(w http.ResponseWriter, r *http.Request) {
		e := m.DeletePersonal(personalOwner(r), r.PathValue("fid"))
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("GET /api/library/{fid}/content", func(w http.ResponseWriter, r *http.Request) {
		m.libraryMu.Lock()
		f, e := m.ownedFile(personalOwner(r), r.PathValue("fid"))
		var src *os.File
		if e == nil {
			src, e = os.Open(m.libraryPath(f))
		}
		m.libraryMu.Unlock()
		if e != nil {
			respond(w, nil, e)
			return
		}
		defer src.Close()
		ext := strings.ToLower(filepath.Ext(f.Name))
		typ := mime.TypeByExtension(ext)
		inline := strings.Contains("|.png|.jpg|.jpeg|.gif|.webp|.mp3|.wav|.mp4|.txt|.md|.json|.log|", "|"+ext+"|") && ext != ""
		if r.URL.Query().Get("preview") == "1" && inline {
			if ext == ".txt" || ext == ".md" || ext == ".json" || ext == ".log" {
				typ = "text/plain; charset=utf-8"
			}
			w.Header().Set("Content-Type", typ)
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
		}
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		at, _ := time.Parse(time.RFC3339Nano, f.Created)
		http.ServeContent(w, r, f.Name, at, src)
	})
}
