package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/skillcatalog"
	"github.com/shengjuntu/rundesk/internal/skillsafety"
)

const skillLocalUploadLimit = skillBundleLimit + (2 << 20)

type SkillInstalledItem struct {
	Name string `json:"name"`
}

type SkillInstalledList struct {
	Items       []SkillInstalledItem `json:"items"`
	WorkspaceID string               `json:"workspaceId"`
	InstanceID  string               `json:"instanceId"`
	Scope       string               `json:"scope"`
	Truncated   bool                 `json:"truncated"`
	Warnings    []string             `json:"warnings"`
}

func localSkillScope(wid, iid, scope string) (string, string, error) {
	if strings.TrimSpace(wid) == "" {
		return "", "", failure(400, "skill_workspace_required", "请选择要读取的工作区")
	}
	if iid == "" {
		iid = DefaultInstance
	}
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "instance" {
		return "", "", failure(400, "invalid_skill_scope", "scope 必须为 project 或 instance")
	}
	return iid, scope, nil
}

// A display name never becomes an upload path. Only a simple, already-safe
// frontmatter name is adopted; complex YAML scalars use a content-based name.
func localSkillName(explicit string, files []skillAsset, digest string) (string, error) {
	if explicit != "" {
		if !slug.MatchString(explicit) {
			return "", failure(400, "invalid_skill_name", "技能名称仅支持字母、数字、下划线和连字符，最多 64 字符")
		}
		return explicit, nil
	}
	for _, file := range files {
		if file.Path != "SKILL.md" {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(file.Data), "\r\n", "\n"), "\n")
		for _, line := range lines[1:] {
			if strings.TrimSpace(line) == "---" {
				break
			}
			if strings.HasPrefix(line, "name:") {
				value := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
				if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
					value = value[1 : len(value)-1]
				}
				if slug.MatchString(value) {
					return value, nil
				}
			}
		}
	}
	return "skill-" + strings.TrimPrefix(digest, "sha256:")[:12], nil
}

func (m *Manager) stageLocalSkill(files []skillAsset, explicitName, source string) (SkillPack, error) {
	files, err := normalizeSkillAssets(files)
	if err != nil {
		return SkillPack{}, err
	}
	digest, err := skillsafety.Digest(safetyFiles(files))
	if err != nil {
		return SkillPack{}, err
	}
	name, err := localSkillName(explicitName, files, digest)
	if err != nil {
		return SkillPack{}, err
	}
	if source == "" {
		source = "local-upload://" + name
	}
	assets := make([]skillcatalog.Asset, 0, len(files))
	for _, file := range files {
		assets = append(assets, skillcatalog.Asset{Path: file.Path, Data: file.Data, Mode: file.Mode})
	}
	return m.StageSkillPack(skillcatalog.Bundle{Name: name, SourceURL: source, Revision: digest, Assets: assets})
}

// Check every selected-scope directory component without resolving a link into
// another installation. No browser-supplied host path enters this operation.
func localSkillDirectory(root *os.Root, dir string) error {
	prefix := ""
	for _, component := range strings.Split(dir, "/") {
		if prefix != "" {
			prefix += "/"
		}
		prefix += component
		info, err := root.Lstat(prefix)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return bundleError("已安装技能的范围目录包含链接或不是普通目录")
		}
	}
	return nil
}

func (m *Manager) InstalledSkillCandidates(wid, iid, scope string) (SkillInstalledList, error) {
	iid, scope, err := localSkillScope(wid, iid, scope)
	result := SkillInstalledList{Items: []SkillInstalledItem{}, WorkspaceID: wid, InstanceID: iid, Scope: scope, Warnings: []string{}}
	if err != nil {
		return result, err
	}
	m.skillMu.Lock()
	defer m.skillMu.Unlock()
	rootPath, dir, err := m.skillRoot(wid, []string{iid, scope})
	if err != nil {
		return result, err
	}
	parent, err := os.OpenRoot(rootPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, nil
		}
		return result, err
	}
	defer parent.Close()
	if err := localSkillDirectory(parent, dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, nil
		}
		return result, err
	}
	root, err := parent.OpenRoot(dir)
	if err != nil {
		return result, err
	}
	defer root.Close()
	listing, err := root.Open(".")
	if err != nil {
		return result, err
	}
	defer listing.Close()
	inspected, skipped := 0, 0
	for {
		entries, readErr := listing.ReadDir(128)
		for _, entry := range entries {
			inspected++
			if inspected > 10000 || len(result.Items) >= 1000 {
				result.Truncated = true
				break
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !slug.MatchString(entry.Name()) {
				skipped++
				continue
			}
			info, err := root.Lstat(entry.Name() + "/SKILL.md")
			if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				skipped++
				continue
			}
			result.Items = append(result.Items, SkillInstalledItem{Name: entry.Name()})
		}
		if result.Truncated || readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
	}
	if skipped > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("跳过 %d 个非完整技能、名称不受支持或包含链接的条目", skipped))
	}
	if result.Truncated {
		result.Warnings = append(result.Warnings, "已安装技能列表达到 1000 项或目录检查上限，当前结果不完整")
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return result, nil
}

func (m *Manager) SnapshotInstalledSkill(wid, iid, scope, name string) (SkillPack, error) {
	iid, scope, err := localSkillScope(wid, iid, scope)
	if err != nil {
		return SkillPack{}, err
	}
	if !slug.MatchString(name) {
		return SkillPack{}, failure(400, "invalid_skill_name", "请选择一个完整的已安装技能目录")
	}
	var files []skillAsset
	func() {
		m.skillMu.Lock()
		defer m.skillMu.Unlock()
		var rootPath, dir string
		rootPath, dir, err = m.skillRoot(wid, []string{iid, scope})
		if err != nil {
			return
		}
		var root *os.Root
		root, err = os.OpenRoot(rootPath)
		if err != nil {
			return
		}
		defer root.Close()
		if err = localSkillDirectory(root, dir); err == nil {
			files, err = m.skillAssets(wid, name, iid, scope)
		}
	}()
	if err != nil {
		return SkillPack{}, err
	}
	source := (&url.URL{Scheme: "rundesk", Host: iid, Path: "/" + wid + "/" + scope + "/" + name}).String()
	return m.stageLocalSkill(files, name, source)
}

func (s *Server) skillCenterLocalRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if principal(r) != nil || human(r) != nil {
				respond(w, nil, failure(403, "skill_center_admin_required", "技能中心由管理员配置"))
				return
			}
			handler(w, r)
		})
	}
	register("POST /api/skill-center/import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, skillLocalUploadLimit)
		typ, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			writeSkillCenterError(w, bundleError("请上传 ZIP 或完整文件夹"))
			return
		}
		var files []skillAsset
		switch {
		case typ == "application/zip":
			var data []byte
			data, err = io.ReadAll(r.Body)
			if err == nil {
				files, err = readBundleZIP(data)
			}
		case typ == "multipart/form-data" && params["boundary"] != "":
			files, err = readBundleDirectory(multipart.NewReader(r.Body, params["boundary"]))
		default:
			err = bundleError("请使用 application/zip 或 multipart/form-data")
		}
		if err != nil {
			var over *http.MaxBytesError
			if errors.As(err, &over) {
				err = failure(413, "skill_bundle_too_large", "技能上传请求最多 34 MiB，解压后最多 32 MiB")
			}
			writeSkillCenterError(w, err)
			return
		}
		pack, err := s.Manager.stageLocalSkill(files, r.URL.Query().Get("name"), "")
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"pack": pack, "scan": pack.Scan})
	})
	register("GET /api/skill-center/installed", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.InstalledSkillCandidates(r.URL.Query().Get("workspaceId"), r.URL.Query().Get("instanceId"), r.URL.Query().Get("scope"))
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, items)
	})
	register("POST /api/skill-center/from-installed", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			WorkspaceID string `json:"workspaceId"`
			InstanceID  string `json:"instanceId"`
			Scope       string `json:"scope"`
			Name        string `json:"name"`
		}
		if !decode(w, r, &input) {
			return
		}
		pack, err := s.Manager.SnapshotInstalledSkill(input.WorkspaceID, input.InstanceID, input.Scope, input.Name)
		if err != nil {
			writeSkillCenterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"pack": pack, "scan": pack.Scan})
	})
}
