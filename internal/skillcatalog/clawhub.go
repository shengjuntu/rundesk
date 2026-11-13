package skillcatalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	clawOwnerPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)
	clawSlugPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	clawVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.+-]+)?$`)
	clawRepoOwner      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	clawRepoName       = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
)

const clawMetadataLimit int64 = 2 << 20

func parseClawHubURL(raw string) (owner, slug string, err error) {
	if len(raw) > 2048 {
		return "", "", fail("invalid_url", "ClawHub 技能网址过长")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "clawhub.ai" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fail("invalid_url", "请使用不带参数的 HTTPS ClawHub 技能网址")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 3 && parts[1] == "skills" {
		owner, slug = parts[0], parts[2]
	} else if len(parts) == 2 {
		owner, slug = parts[0], parts[1]
	} else {
		return "", "", fail("invalid_url", "ClawHub 网址需要明确发布者与技能名")
	}
	if !clawOwnerPattern.MatchString(owner) || !clawSlugPattern.MatchString(slug) {
		return "", "", fail("invalid_url", "ClawHub 发布者或技能名无效")
	}
	return owner, slug, nil
}

func clawHandoff(body []byte) error {
	var handoff struct {
		SourceRef string `json:"sourceRef"`
		Repo      string `json:"repo"`
		Commit    string `json:"commit"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(body, &handoff); err != nil {
		return fail("invalid_response", "ClawHub 返回了无效 JSON")
	}
	if handoff.SourceRef != "public-github" {
		return nil
	}
	parts := strings.Split(handoff.Repo, "/")
	if len(parts) != 2 || !clawRepoOwner.MatchString(parts[0]) || !clawRepoName.MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." || !isSHA(handoff.Commit) || (handoff.Path != "" && !validPath(handoff.Path)) {
		return fail("invalid_response", "ClawHub 返回了无效的固定版本 GitHub 来源")
	}
	// Ignore archiveUrl: registry metadata cannot redirect a bearer token or
	// provide an arbitrary network destination. The caller can present this
	// explicitly validated immutable GitHub source to the user.
	githubURL := "https://github.com/" + handoff.Repo + "/tree/" + strings.ToLower(handoff.Commit)
	if handoff.Path != "" {
		githubURL += "/" + (&url.URL{Path: handoff.Path}).EscapedPath()
	}
	return &Error{Code: "source_handoff", Message: "该条目源码托管于 GitHub，请继续获取固定版本源码并检查", Details: map[string]any{"githubUrl": githubURL, "revision": strings.ToLower(handoff.Commit), "source": "github"}}
}

func (c *Client) clawJSON(ctx context.Context, endpoint, token string, dst any) error {
	body, _, err := c.request(ctx, endpoint, token, clawMetadataLimit)
	if err != nil {
		return err
	}
	if err = clawHandoff(body); err != nil {
		return err
	}
	if err = json.Unmarshal(body, dst); err != nil {
		return fail("invalid_response", "ClawHub 返回的元数据格式无效")
	}
	return nil
}

type clawManifestFile struct {
	Path   string          `json:"path"`
	Size   *int64          `json:"size"`
	SHA256 string          `json:"sha256"`
	Type   string          `json:"type"`
	Mode   json.RawMessage `json:"mode"`
}

type clawVersion struct {
	Version    string          `json:"version"`
	Files      json.RawMessage `json:"files"`
	Truncated  bool            `json:"truncated"`
	Incomplete bool            `json:"incomplete"`
	NextCursor string          `json:"nextCursor"`
}

func clawManifest(raw json.RawMessage) ([]clawManifestFile, error) {
	var entries []clawManifestFile
	if string(raw) == "null" || json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return nil, fail("incomplete_manifest", "ClawHub 没有返回完整文件清单")
	}
	if len(entries) > MaxFiles {
		return nil, fail("too_many_files", "技能超过 200 文件限制")
	}
	seen, total, hasSkill := map[string]bool{}, int64(0), false
	for _, entry := range entries {
		key := strings.ToLower(entry.Path)
		if !validPath(entry.Path) || seen[key] {
			return nil, fail("unsafe_path", "技能文件清单含无效或重复路径")
		}
		seen[key] = true
		mode := strings.Trim(string(entry.Mode), `"`)
		if (entry.Type != "" && entry.Type != "file") || mode == "120000" || mode == "160000" {
			return nil, fail("unsupported_entry", "技能文件清单含链接或特殊条目")
		}
		digest, err := hex.DecodeString(entry.SHA256)
		if entry.Size == nil || *entry.Size < 0 || err != nil || len(digest) != sha256.Size {
			return nil, fail("incomplete_manifest", "每个文件都需要精确大小和 SHA256")
		}
		if *entry.Size > MaxFileBytes {
			return nil, fail("file_too_large", "技能单文件超过 2 MiB 限制")
		}
		total += *entry.Size
		if total > MaxTotalBytes {
			return nil, fail("skill_too_large", "技能超过 20 MiB 总大小限制")
		}
		hasSkill = hasSkill || entry.Path == "SKILL.md"
	}
	if !hasSkill {
		return nil, fail("no_skill", "技能根目录缺少 SKILL.md")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func clawReadZIP(body []byte) ([]Asset, error) {
	if int64(len(body)) > MaxTotalBytes {
		return nil, fail("skill_too_large", "技能 ZIP 超过 20 MiB 限制")
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(z.File) > MaxFiles*2 {
		return nil, fail("invalid_archive", "技能 ZIP 无效或条目过多")
	}
	assets, seen, total := []Asset{}, map[string]bool{}, int64(0)
	hasSkill := false
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !validPath(name) || f.Mode()&fs.ModeSymlink != 0 {
			return nil, fail("invalid_archive", "技能 ZIP 含危险路径或链接")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] || !f.Mode().IsRegular() || f.Flags&1 != 0 {
			return nil, fail("invalid_archive", "技能 ZIP 含重复路径、特殊文件或加密内容")
		}
		seen[key] = true
		if len(assets) >= MaxFiles || f.UncompressedSize64 > uint64(MaxFileBytes) || f.UncompressedSize64 > uint64(MaxTotalBytes-total) {
			return nil, fail("skill_too_large", "技能 ZIP 解压后超过文件或大小限制")
		}
		r, err := f.Open()
		if err != nil {
			return nil, fail("invalid_archive", "无法读取技能 ZIP 文件")
		}
		data, readErr := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || int64(len(data)) > MaxFileBytes || int64(len(data)) != int64(f.UncompressedSize64) {
			return nil, fail("invalid_archive", "技能 ZIP 内容损坏或实际大小超限")
		}
		total += int64(len(data))
		if total > MaxTotalBytes {
			return nil, fail("skill_too_large", "技能 ZIP 解压后超过 20 MiB")
		}
		mode := fs.FileMode(0600)
		if f.Mode()&0111 != 0 || bytes.HasPrefix(data, []byte("#!")) {
			mode = 0700
		}
		assets = append(assets, Asset{Path: name, Data: data, Mode: mode})
		hasSkill = hasSkill || name == "SKILL.md"
	}
	if !hasSkill {
		return nil, fail("no_skill", "技能 ZIP 根目录缺少 SKILL.md")
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	return assets, nil
}

func (c *Client) clawDownload(ctx context.Context, owner, slug, version, token string) ([]Asset, error) {
	query := url.Values{"owner": {owner}, "slug": {slug}}
	if version != "" {
		query.Set("version", version)
	}
	body, headers, err := c.request(ctx, "https://clawhub.ai/api/v1/download?"+query.Encode(), token, MaxTotalBytes)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(headers.Get("Content-Type")), "json") || bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		if err = clawHandoff(body); err != nil {
			return nil, err
		}
		return nil, fail("unsupported_download", "ClawHub 返回的是描述信息，而不是完整技能包")
	}
	if version == "" {
		return nil, fail("unpinned_version", "无法在没有固定版本时获取完整技能包")
	}
	return clawReadZIP(body)
}

func (c *Client) fetchClawHub(ctx context.Context, rawURL string) (Bundle, error) {
	owner, slug, err := parseClawHubURL(rawURL)
	if err != nil {
		return Bundle{}, err
	}
	auth, err := token("CLAWHUB_TOKEN")
	if err != nil {
		return Bundle{}, err
	}
	base := "https://clawhub.ai/api/v1/skills/" + url.PathEscape(slug)
	ownerQuery := "?" + url.Values{"owner": {owner}}.Encode()
	var detail struct {
		Owner struct {
			Handle string `json:"handle"`
		} `json:"owner"`
		Skill struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"displayName"`
		} `json:"skill"`
		LatestVersion *struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
	}
	if err = c.clawJSON(ctx, base+ownerQuery, auth, &detail); err != nil {
		return Bundle{}, err
	}
	if !strings.EqualFold(detail.Owner.Handle, owner) || detail.Skill.Slug != slug {
		return Bundle{}, fail("identity_mismatch", "ClawHub 返回的技能或发布者与请求不符")
	}
	version := ""
	if detail.LatestVersion != nil {
		version = detail.LatestVersion.Version
	}
	if version == "" {
		_, err = c.clawDownload(ctx, owner, slug, "", auth)
		if err != nil {
			return Bundle{}, err
		}
		return Bundle{}, fail("unpinned_version", "ClawHub 条目没有固定发布版本")
	}
	if len(version) > 128 || !clawVersionPattern.MatchString(version) {
		return Bundle{}, fail("unpinned_version", "ClawHub 没有提供有效的固定发布版本")
	}
	var envelope struct {
		Version *clawVersion `json:"version"`
		Skill   *struct {
			Slug string `json:"slug"`
		} `json:"skill"`
		Truncated  bool   `json:"truncated"`
		Incomplete bool   `json:"incomplete"`
		NextCursor string `json:"nextCursor"`
	}
	err = c.clawJSON(ctx, base+"/versions/"+url.PathEscape(version)+ownerQuery, auth, &envelope)
	var assets []Asset
	var sourceErr *Error
	if err != nil {
		if !errors.As(err, &sourceErr) || sourceErr.Code != "not_found" {
			return Bundle{}, err
		}
		assets, err = c.clawDownload(ctx, owner, slug, version, auth)
	} else {
		if envelope.Version == nil || envelope.Version.Version != version || (envelope.Skill != nil && envelope.Skill.Slug != slug) {
			return Bundle{}, fail("integrity_error", "ClawHub 返回了不同的技能版本")
		}
		v := envelope.Version
		if envelope.Truncated || envelope.Incomplete || envelope.NextCursor != "" || v.Truncated || v.Incomplete || v.NextCursor != "" {
			return Bundle{}, fail("incomplete_manifest", "ClawHub 文件清单不完整")
		}
		if len(v.Files) == 0 {
			assets, err = c.clawDownload(ctx, owner, slug, version, auth)
		} else {
			var manifest []clawManifestFile
			manifest, err = clawManifest(v.Files)
			if err == nil {
				assets = make([]Asset, 0, len(manifest))
				for _, file := range manifest {
					params := url.Values{"owner": {owner}, "version": {version}, "path": {file.Path}}
					var data []byte
					data, _, err = c.request(ctx, base+"/file?"+params.Encode(), auth, MaxFileBytes)
					if err != nil {
						break
					}
					digest := sha256.Sum256(data)
					if int64(len(data)) != *file.Size || !strings.EqualFold(hex.EncodeToString(digest[:]), file.SHA256) {
						err = fail("integrity_error", "下载文件与固定版本的 SHA256 或大小不符")
						break
					}
					mode := fs.FileMode(0600)
					if strings.Trim(string(file.Mode), `"`) == "100755" || bytes.HasPrefix(data, []byte("#!")) {
						mode = 0700
					}
					assets = append(assets, Asset{Path: file.Path, Data: data, Mode: mode})
				}
			}
		}
	}
	if err != nil {
		return Bundle{}, err
	}
	name := detail.Skill.DisplayName
	if name == "" {
		name = slug
	}
	return Bundle{Name: name, SourceURL: "https://clawhub.ai/" + owner + "/skills/" + slug, Revision: version, Assets: assets}, nil
}
