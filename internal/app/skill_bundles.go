package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"github.com/shengjuntu/rundesk/internal/store"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const skillBundleLimit = 32 << 20
const skillBundleFiles = 1000

type skillAsset struct {
	Path string
	Data []byte
	Mode fs.FileMode
}
type SkillFile struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Executable bool   `json:"executable"`
}
type SkillBundle struct {
	Name       string      `json:"name"`
	Files      []SkillFile `json:"files"`
	Size       int64       `json:"size"`
	BackupPath string      `json:"backupPath,omitempty"`
}

func bundleError(message string) error { return failure(400, "invalid_skill_bundle", message) }
func validSkillPath(p string) bool {
	if p == "" || len(p) > 1024 || strings.ContainsAny(p, "\\:\x00\r\n") || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9' {
			return false
		}
	}
	return true
}
func normalizeSkillAssets(files []skillAsset) ([]skillAsset, error) {
	if len(files) == 0 || len(files) > skillBundleFiles {
		return nil, bundleError("技能目录必须包含 1–1000 个文件")
	}
	// Accept SKILL.md at root, or one wrapping directory. Never flatten paths.
	prefix := ""
	rootMD := false
	for _, f := range files {
		if f.Path == "SKILL.md" {
			rootMD = true
		}
	}
	if !rootMD {
		for _, f := range files {
			if strings.Count(f.Path, "/") == 1 && strings.HasSuffix(f.Path, "/SKILL.md") {
				if prefix != "" {
					return nil, bundleError("每次只能导入一个完整技能")
				}
				prefix = strings.TrimSuffix(f.Path, "SKILL.md")
			}
		}
		if prefix == "" {
			return nil, bundleError("目录根部需要 SKILL.md；ZIP 也可包含一层技能文件夹")
		}
	}
	seen := map[string]bool{}
	total := 0
	for n := range files {
		f := &files[n]
		if !validSkillPath(f.Path) {
			return nil, bundleError("技能包包含无效或越界路径")
		}
		if prefix != "" {
			if !strings.HasPrefix(f.Path, prefix) {
				return nil, bundleError("ZIP 中存在技能目录之外的文件")
			}
			f.Path = strings.TrimPrefix(f.Path, prefix)
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return nil, bundleError("技能包包含重复或大小写冲突的路径")
		}
		seen[key] = true
		total += len(f.Data)
		if total > skillBundleLimit {
			return nil, failure(413, "skill_bundle_too_large", "完整技能解压后最多 32 MiB")
		}
		if f.Mode&os.ModeSymlink != 0 || !f.Mode.IsRegular() {
			return nil, bundleError("技能包只支持普通文件，不支持符号链接或特殊文件")
		}
		if f.Path == "SKILL.md" {
			if len(f.Data) > 256<<10 || !utf8.Valid(f.Data) || !strings.HasPrefix(strings.ReplaceAll(string(f.Data), "\r\n", "\n"), "---\n") {
				return nil, bundleError("SKILL.md 需要 YAML frontmatter，最多 256 KiB")
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
func readBundleZIP(data []byte) ([]skillAsset, error) {
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return nil, bundleError("无法读取 ZIP 文件")
	}
	files := []skillAsset{}
	total := 0
	if len(z.File) > skillBundleFiles*2 {
		return nil, bundleError("ZIP 条目过多")
	}
	for _, f := range z.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, bundleError("ZIP 中不允许符号链接")
		}
		if !validSkillPath(strings.TrimSuffix(f.Name, "/")) {
			return nil, bundleError("ZIP 中包含无效路径")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return nil, bundleError("ZIP 中不允许特殊文件")
		}
		if f.UncompressedSize64 > uint64(skillBundleLimit-total) {
			return nil, failure(413, "skill_bundle_too_large", "技能包解压后最多 32 MiB")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(r, int64(skillBundleLimit-total+1)))
		r.Close()
		if e != nil {
			return nil, e
		}
		total += len(b)
		if total > skillBundleLimit {
			return nil, failure(413, "skill_bundle_too_large", "技能包解压后最多 32 MiB")
		}
		files = append(files, skillAsset{f.Name, b, f.Mode().Perm()})
	}
	return normalizeSkillAssets(files)
}
func readBundleDirectory(r *multipart.Reader) ([]skillAsset, error) {
	files := []skillAsset{}
	total := 0
	for {
		p, e := r.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(files) >= skillBundleFiles {
			p.Close()
			return nil, bundleError("技能目录最多 1000 个文件")
		}
		name := p.FormName()
		if !validSkillPath(name) {
			p.Close()
			return nil, bundleError("文件夹包含无效相对路径")
		}
		b, e := io.ReadAll(io.LimitReader(p, int64(skillBundleLimit-total+1)))
		p.Close()
		if e != nil {
			return nil, e
		}
		total += len(b)
		if total > skillBundleLimit {
			return nil, failure(413, "skill_bundle_too_large", "技能目录最多 32 MiB")
		}
		mode := fs.FileMode(0600)
		if bytes.HasPrefix(b, []byte("#!")) {
			mode = 0700
		}
		files = append(files, skillAsset{name, b, mode})
	}
	return normalizeSkillAssets(files)
}
func (m *Manager) ImportSkillBundle(wid, name string, files []skillAsset, replace bool, opts ...string) (SkillBundle, error) {
	if !slug.MatchString(name) {
		return SkillBundle{}, bundleError("技能目录名仅支持字母、数字、下划线和连字符，最多 64 字符")
	}
	files, e := normalizeSkillAssets(files)
	if e != nil {
		return SkillBundle{}, e
	}
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	rootPath, dir, e := m.skillRoot(wid, opts)
	if e != nil {
		return SkillBundle{}, e
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return SkillBundle{}, e
	}
	defer root.Close()
	dest := dir + "/" + name
	exists := false
	if info, e := root.Lstat(dest); e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return SkillBundle{}, bundleError("目标必须是普通技能目录")
		}
		exists = true
		if !replace {
			return SkillBundle{}, failure(409, "skill_exists", "同名技能已存在，请选择替换；原目录会完整备份")
		}
	} else if !errors.Is(e, fs.ErrNotExist) {
		return SkillBundle{}, e
	}
	stage := ".rundesk/skill-staging/" + store.ID()
	if e = root.MkdirAll(stage, 0700); e != nil {
		return SkillBundle{}, e
	}
	defer root.RemoveAll(stage)
	for _, f := range files {
		p := stage + "/" + f.Path
		if e = root.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return SkillBundle{}, e
		}
		mode := fs.FileMode(0600)
		if f.Mode&0111 != 0 {
			mode = 0700
		}
		out, e := root.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			return SkillBundle{}, e
		}
		_, e = out.Write(f.Data)
		if e == nil {
			e = out.Sync()
		}
		ce := out.Close()
		if e != nil {
			return SkillBundle{}, e
		}
		if ce != nil {
			return SkillBundle{}, ce
		}
	}
	if e = root.MkdirAll(dir, 0700); e != nil {
		return SkillBundle{}, e
	}
	backup := ""
	if exists {
		backup = ".rundesk/skill-backups/" + name + "-" + store.ID()
		if e = root.MkdirAll(path.Dir(backup), 0700); e != nil {
			return SkillBundle{}, e
		}
		if e = root.Rename(dest, backup); e != nil {
			return SkillBundle{}, e
		}
	}
	if e = root.Rename(stage, dest); e != nil {
		if backup != "" {
			if rollback := root.Rename(backup, dest); rollback != nil {
				return SkillBundle{}, &apiError{Status: 500, Code: "skill_restore_required", Message: "安装失败且自动恢复失败，原技能已保存在 " + backup, Cause: errors.Join(e, rollback)}
			}
		}
		return SkillBundle{}, e
	}
	result := bundleManifest(name, files)
	result.BackupPath = backup
	return result, nil
}
func bundleManifest(name string, assets []skillAsset) SkillBundle {
	b := SkillBundle{Name: name, Files: []SkillFile{}}
	for _, f := range assets {
		b.Files = append(b.Files, SkillFile{f.Path, int64(len(f.Data)), f.Mode&0111 != 0})
		b.Size += int64(len(f.Data))
	}
	return b
}

// Read under a root handle, rejecting links. Callers hold skillMu to synchronize imports and edits.
func (m *Manager) skillAssets(wid, name string, opts ...string) ([]skillAsset, error) {
	if !slug.MatchString(name) {
		return nil, bundleError("无效技能目录名")
	}
	rootPath, dir, e := m.skillRoot(wid, opts)
	if e != nil {
		return nil, e
	}
	parent, e := os.OpenRoot(rootPath)
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	info, e := parent.Lstat(dir + "/" + name)
	if errors.Is(e, fs.ErrNotExist) {
		return nil, failure(404, "skill_not_found", "技能目录不存在")
	}
	if e != nil {
		return nil, e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, bundleError("只支持普通技能目录")
	}
	root, e := parent.OpenRoot(dir + "/" + name)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	out := []skillAsset{}
	total := 0
	e = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == "." {
			return nil
		}
		if !validSkillPath(p) || d.Type()&os.ModeSymlink != 0 {
			return bundleError("目录包含链接或无效路径，不能完整读取")
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return bundleError("目录包含特殊文件")
		}
		if len(out) >= skillBundleFiles || info.Size() > int64(skillBundleLimit-total) {
			return failure(413, "skill_bundle_too_large", "技能目录超出 1000 文件或 32 MiB 限制")
		}
		f, e := root.Open(p)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, int64(skillBundleLimit-total+1)))
		f.Close()
		if e != nil {
			return e
		}
		total += len(b)
		if total > skillBundleLimit {
			return failure(413, "skill_bundle_too_large", "技能目录超过 32 MiB")
		}
		out = append(out, skillAsset{p, b, info.Mode().Perm()})
		return nil
	})
	return out, e
}
func (m *Manager) SkillBundle(wid, name string, opts ...string) (SkillBundle, error) {
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	files, e := m.skillAssets(wid, name, opts...)
	return bundleManifest(name, files), e
}
func (m *Manager) ExportSkillBundle(wid, name string, opts ...string) ([]byte, error) {
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	files, e := m.skillAssets(wid, name, opts...)
	if e != nil {
		return nil, e
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: name + "/" + f.Path, Method: zip.Deflate}
		h.SetMode(f.Mode)
		w, e := z.CreateHeader(h)
		if e != nil {
			return nil, e
		}
		if _, e = w.Write(f.Data); e != nil {
			return nil, e
		}
	}
	if e = z.Close(); e != nil {
		return nil, e
	}
	return buf.Bytes(), nil
}
func (m *Manager) PreviewSkillFile(wid, name, file string, opts ...string) (any, error) {
	if !validSkillPath(file) {
		return nil, bundleError("无效文件路径")
	}
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	files, e := m.skillAssets(wid, name, opts...)
	if e != nil {
		return nil, e
	}
	for _, f := range files {
		if f.Path == file {
			v := map[string]any{"path": file, "size": len(f.Data), "binary": !utf8.Valid(f.Data) || bytes.ContainsRune(f.Data, 0)}
			if v["binary"] == false {
				b := f.Data
				if len(b) > 256<<10 {
					b = b[:256<<10]
					for !utf8.Valid(b) {
						b = b[:len(b)-1]
					}
					v["truncated"] = true
				}
				v["text"] = string(b)
			}
			return v, nil
		}
	}
	return nil, failure(404, "skill_file_not_found", "文件不存在")
}
func (m *Manager) RemoveSkillBundle(wid, name string, opts ...string) (string, error) {
	if !slug.MatchString(name) {
		return "", bundleError("无效技能目录名")
	}
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	rootPath, dir, e := m.skillRoot(wid, opts)
	if e != nil {
		return "", e
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return "", e
	}
	defer root.Close()
	src := dir + "/" + name
	info, e := root.Lstat(src)
	if e != nil {
		return "", e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", bundleError("只移除普通技能目录")
	}
	dest := ".rundesk/skill-backups/" + name + "-" + store.ID()
	if e = root.MkdirAll(path.Dir(dest), 0700); e != nil {
		return "", e
	}
	if e = root.Rename(src, dest); e != nil {
		return "", e
	}
	return dest, nil
}
func (s *Server) skillBundleRoutes(mux *http.ServeMux) {
	opts := func(r *http.Request) []string {
		scope := r.URL.Query().Get("scope")
		if scope == "" {
			scope = "instance"
		}
		return []string{r.URL.Query().Get("instanceId"), scope}
	}
	mux.HandleFunc("POST /api/workspaces/{wid}/skill-bundles", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, skillBundleLimit+2<<20)
		typ, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil {
			respond(w, nil, bundleError("请上传 ZIP 或完整文件夹"))
			return
		}
		var files []skillAsset
		if typ == "application/zip" {
			var b []byte
			b, e = io.ReadAll(r.Body)
			if e == nil {
				files, e = readBundleZIP(b)
			}
		} else if typ == "multipart/form-data" && params["boundary"] != "" {
			files, e = readBundleDirectory(multipart.NewReader(r.Body, params["boundary"]))
		} else {
			e = bundleError("请使用 application/zip 或 multipart/form-data")
		}
		if e != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(e, &tooLarge) {
				e = failure(413, "skill_bundle_too_large", "技能上传超过大小限制")
			}
			respond(w, nil, e)
			return
		}
		v, e := s.Manager.ImportSkillBundle(r.PathValue("wid"), r.URL.Query().Get("name"), files, r.URL.Query().Get("replace") == "1", opts(r)...)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/skill-bundles/{name}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.SkillBundle(r.PathValue("wid"), r.PathValue("name"), opts(r)...)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/skill-bundles/{name}/file", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.PreviewSkillFile(r.PathValue("wid"), r.PathValue("name"), r.URL.Query().Get("path"), opts(r)...)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/workspaces/{wid}/skill-bundles/{name}/export", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.ExportSkillBundle(r.PathValue("wid"), r.PathValue("name"), opts(r)...)
		if e != nil {
			respond(w, nil, e)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("name")+`.zip"`)
		w.Write(v)
	})
	mux.HandleFunc("DELETE /api/workspaces/{wid}/skill-bundles/{name}", func(w http.ResponseWriter, r *http.Request) {
		backup, e := s.Manager.RemoveSkillBundle(r.PathValue("wid"), r.PathValue("name"), opts(r)...)
		respond(w, map[string]string{"backupPath": backup}, e)
	})
}
