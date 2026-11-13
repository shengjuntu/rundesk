package skillcatalog

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

func parseGitHub(raw string) (owner, repo string, tree []string, err error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 2048 || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", nil, fail("invalid_url", "请输入 HTTPS GitHub 仓库或技能目录网址")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || !ownerRE.MatchString(parts[0]) || !repoRE.MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." {
		return "", "", nil, fail("invalid_url", "GitHub 仓库身份无效")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\:\x00\r\n") {
			return "", "", nil, fail("invalid_url", "GitHub 网址包含无效路径")
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return "", "", nil, fail("invalid_url", "GitHub 网址包含控制字符")
			}
		}
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if repo == "" || repo == "." || repo == ".." {
		return "", "", nil, fail("invalid_url", "GitHub 仓库身份无效")
	}
	if len(parts) == 2 {
		return owner, repo, nil, nil
	}
	if len(parts) < 4 || parts[2] != "tree" || len(parts[3:]) > 16 {
		return "", "", nil, fail("invalid_url", "请使用仓库网址或 /tree/版本/技能目录 网址")
	}
	return owner, repo, parts[3:], nil
}

// Fetch returns a complete, bounded source bundle. Callers still validate its
// manifest and run their own checks before any installation.
func (c *Client) Fetch(ctx context.Context, rawURL string) (Bundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	u, e := url.Parse(rawURL)
	if e != nil || len(rawURL) > 2048 || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Bundle{}, fail("invalid_url", "请输入公开 Skill 的 HTTPS 网址")
	}
	switch u.Host {
	case "github.com":
		return c.fetchGitHub(ctx, rawURL)
	case "clawhub.ai":
		return c.fetchClawHub(ctx, rawURL)
	case "skills.sh", "www.skills.sh":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 3 {
			return Bundle{}, fail("invalid_url", "请输入 skills.sh 的具体技能页面网址")
		}
		for _, p := range parts {
			if !componentRE.MatchString(p) {
				return Bundle{}, fail("invalid_url", "技能页面网址无效")
			}
		}
		// A market slug does not establish the repository directory. The GitHub
		// resolver discovers SKILL.md roots and returns explicit choices.
		return c.fetchGitHub(ctx, "https://github.com/"+strings.Join(parts[:2], "/"))
	default:
		return Bundle{}, fail("unsupported_source", "请从搜索结果选择源码，或填写 GitHub / ClawHub 技能网址")
	}
}

func (c *Client) resolveCommit(ctx context.Context, base, ref, auth string) (string, string, error) {
	var response struct {
		SHA    string `json:"sha"`
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	if err := c.getJSON(ctx, base+"/commits/"+url.PathEscape(ref), auth, maxAPIBytes, &response); err != nil {
		return "", "", err
	}
	if !isSHA(response.SHA) || !isSHA(response.Commit.Tree.SHA) {
		return "", "", fail("invalid_response", "GitHub 返回的提交身份无效")
	}
	return strings.ToLower(response.SHA), strings.ToLower(response.Commit.Tree.SHA), nil
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size"`
}

func (c *Client) fetchGitHub(ctx context.Context, rawURL string) (Bundle, error) {
	owner, repo, treeParts, err := parseGitHub(rawURL)
	if err != nil {
		return Bundle{}, err
	}
	auth, err := token("GITHUB_TOKEN")
	if err != nil {
		return Bundle{}, err
	}
	base := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	var revision, treeSHA, selected string
	explicit := treeParts != nil
	if !explicit {
		var info struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err = c.getJSON(ctx, base, auth, maxAPIBytes, &info); err != nil {
			return Bundle{}, err
		}
		if info.DefaultBranch == "" {
			return Bundle{}, fail("invalid_response", "GitHub 未提供默认分支")
		}
		revision, treeSHA, err = c.resolveCommit(ctx, base, info.DefaultBranch, auth)
	} else if isSHA(treeParts[0]) {
		revision, treeSHA, err = c.resolveCommit(ctx, base, treeParts[0], auth)
		selected = strings.Join(treeParts[1:], "/")
	} else {
		for split := len(treeParts); split > 0; split-- {
			revision, treeSHA, err = c.resolveCommit(ctx, base, strings.Join(treeParts[:split], "/"), auth)
			if err == nil {
				selected = strings.Join(treeParts[split:], "/")
				break
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != "not_found" {
				return Bundle{}, err
			}
		}
	}
	if err != nil {
		return Bundle{}, err
	}
	var tree struct {
		Truncated *bool             `json:"truncated"`
		Tree      []githubTreeEntry `json:"tree"`
	}
	if err = c.getJSON(ctx, base+"/git/trees/"+treeSHA+"?recursive=1", auth, maxAPIBytes, &tree); err != nil {
		return Bundle{}, err
	}
	if tree.Truncated == nil || *tree.Truncated {
		return Bundle{}, fail("truncated_tree", "GitHub 目录清单不完整，请选择较小的仓库")
	}
	if tree.Tree == nil {
		return Bundle{}, fail("invalid_response", "GitHub 目录清单无效")
	}
	candidates := map[string]bool{}
	seen := map[string]bool{}
	for _, entry := range tree.Tree {
		if !validPath(entry.Path) || seen[entry.Path] {
			return Bundle{}, fail("unsafe_path", "仓库包含无效或重复路径")
		}
		seen[entry.Path] = true
		if entry.Type == "blob" && (entry.Path == "SKILL.md" || strings.HasSuffix(entry.Path, "/SKILL.md")) {
			candidates[strings.TrimSuffix(strings.TrimSuffix(entry.Path, "SKILL.md"), "/")] = true
		}
	}
	if !explicit {
		if len(candidates) == 0 {
			return Bundle{}, fail("no_skill", "仓库中没有 SKILL.md")
		}
		if len(candidates) > 1 {
			paths := make([]string, 0, len(candidates))
			for p := range candidates {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			choices := []map[string]string{}
			for n, p := range paths {
				if n >= 200 {
					break
				}
				target := "https://github.com/" + owner + "/" + repo + "/tree/" + revision
				if p != "" {
					target += "/" + escapePath(p)
				}
				choices = append(choices, map[string]string{"name": first(p, repo), "url": target})
			}
			return Bundle{}, &Error{Code: "multiple_skills", Message: "仓库包含多个技能，请选择具体目录", Details: map[string]any{"candidates": choices, "total": len(paths), "truncated": len(paths) > 200}}
		}
		for p := range candidates {
			selected = p
		}
	} else if !candidates[selected] {
		return Bundle{}, fail("no_skill", "所选目录根部必须包含 SKILL.md")
	}
	prefix := ""
	if selected != "" {
		prefix = selected + "/"
	}
	entries := []githubTreeEntry{}
	var total int64
	for _, entry := range tree.Tree {
		if !strings.HasPrefix(entry.Path, prefix) || entry.Type == "tree" {
			continue
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
			return Bundle{}, fail("unsupported_entry", "技能包含符号链接、子模块或不支持的文件")
		}
		if entry.Size == nil || *entry.Size < 0 || !isSHA(entry.SHA) {
			return Bundle{}, fail("invalid_response", "GitHub 文件清单缺少有效大小或摘要")
		}
		if *entry.Size > MaxFileBytes {
			return Bundle{}, fail("file_too_large", "技能单文件超过 2 MiB")
		}
		total += *entry.Size
		if total > MaxTotalBytes {
			return Bundle{}, fail("skill_too_large", "技能总大小超过 20 MiB")
		}
		entries = append(entries, entry)
		if len(entries) > MaxFiles {
			return Bundle{}, fail("too_many_files", "技能文件数量超过 200")
		}
	}
	assets := make([]Asset, 0, len(entries))
	var actual int64
	for _, entry := range entries {
		var blob struct {
			SHA      string `json:"sha"`
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if err = c.getJSON(ctx, base+"/git/blobs/"+entry.SHA, auth, 3<<20, &blob); err != nil {
			return Bundle{}, err
		}
		if blob.Encoding != "base64" || !strings.EqualFold(blob.SHA, entry.SHA) {
			return Bundle{}, fail("invalid_response", "GitHub 文件响应缺少有效内容编码或摘要")
		}
		content, e := base64.StdEncoding.Strict().DecodeString(strings.Join(strings.Fields(blob.Content), ""))
		if e != nil {
			return Bundle{}, fail("invalid_response", "GitHub 文件内容编码无效")
		}
		if int64(len(content)) > MaxFileBytes {
			return Bundle{}, fail("file_too_large", "技能单文件超过 2 MiB")
		}
		hash := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...))
		if int64(len(content)) != *entry.Size || !strings.EqualFold(hex.EncodeToString(hash[:]), entry.SHA) {
			return Bundle{}, fail("integrity_error", "下载内容与固定 Git 版本摘要不匹配")
		}
		actual += int64(len(content))
		if actual > MaxTotalBytes {
			return Bundle{}, fail("skill_too_large", "技能总大小超过 20 MiB")
		}
		mode := fs.FileMode(0600)
		if entry.Mode == "100755" {
			mode = 0700
		}
		assets = append(assets, Asset{Path: strings.TrimPrefix(entry.Path, prefix), Data: content, Mode: mode})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	name := repo
	if selected != "" {
		p := strings.Split(selected, "/")
		name = p[len(p)-1]
	}
	for _, asset := range assets {
		if asset.Path == "SKILL.md" {
			name = skillName(asset.Data, name)
		}
	}
	target := "https://github.com/" + owner + "/" + repo + "/tree/" + revision
	if selected != "" {
		target += "/" + escapePath(selected)
	}
	return Bundle{Name: name, SourceURL: target, Revision: revision, Assets: assets}, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for n := range parts {
		parts[n] = url.PathEscape(parts[n])
	}
	return strings.Join(parts, "/")
}

var nameRE = regexp.MustCompile(`(?m)^name:\s*([^\r\n]+)$`)

func skillName(content []byte, fallback string) string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return fallback
	}
	front, _, _ := strings.Cut(text[4:], "\n---")
	if match := nameRE.FindStringSubmatch(front); len(match) > 1 {
		if name := strings.Trim(strings.TrimSpace(match[1]), "\"'"); name != "" {
			return short(name, 100)
		}
	}
	return fallback
}
