package skillcatalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const searchLimit = 12

// Search queries only selected sources. An empty source list makes no network
// requests. A failed provider cannot turn the combined response into a clean
// empty result, and catalog claims never become local security conclusions.
func (c *Client) Search(ctx context.Context, query string, sources []string) (SearchResult, error) {
	query = strings.Join(strings.Fields(query), " ")
	result := SearchResult{Items: []Item{}, Providers: []ProviderStatus{}, Complete: true, Warnings: []string{}}
	if len([]rune(query)) == 0 || len([]rune(query)) > 200 {
		return result, fail("invalid_query", "请输入 1–200 字符的搜索词")
	}
	selected := []string{}
	seen := map[string]bool{}
	for _, source := range sources {
		if source != "github" && source != "clawhub" && source != "skillsmp" && source != "skills.sh" {
			return result, fail("invalid_source", "未知搜索来源")
		}
		if !seen[source] {
			selected = append(selected, source)
			seen[source] = true
		}
	}
	type response struct {
		source   string
		items    []Item
		complete bool
		err      error
	}
	responses := make(chan response, len(selected))
	for _, source := range selected {
		go func(source string) {
			items, complete, err := c.searchOne(ctx, source, query)
			responses <- response{source, items, complete, err}
		}(source)
	}
	bySource := map[string]response{}
	for range selected {
		select {
		case r := <-responses:
			bySource[r.source] = r
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	for _, source := range selected {
		r := bySource[source]
		status := ProviderStatus{Source: source, Status: "ok", Message: "查询完成", Count: len(r.items)}
		if r.err != nil {
			status.Status, status.Message = "error", r.err.Error()
			var e *Error
			if errors.As(r.err, &e) {
				status.Code = e.Code
			}
			result.Complete = false
			result.Warnings = append(result.Warnings, source+"："+status.Message)
		} else {
			result.Items = append(result.Items, r.items...)
			if !r.complete {
				result.Complete = false
				status.Message = "部分条目未能解析，结果不完整"
				result.Warnings = append(result.Warnings, source+"："+status.Message)
			}
		}
		result.Providers = append(result.Providers, status)
	}
	result.Items = mergeItems(result.Items)
	return result, nil
}

func (c *Client) searchOne(ctx context.Context, source, query string) ([]Item, bool, error) {
	var endpoint, auth string
	var err error
	params := url.Values{"q": {query}, "limit": {strconv.Itoa(searchLimit)}}
	switch source {
	case "github":
		auth, err = token("GITHUB_TOKEN")
		params = url.Values{"q": {query + ` "SKILL.md" in:readme`}, "per_page": {strconv.Itoa(searchLimit)}}
		endpoint = "https://api.github.com/search/repositories"
	case "clawhub":
		endpoint = "https://clawhub.ai/api/v1/search"
	case "skillsmp":
		auth, err = token("SKILLSMP_API_KEY")
		endpoint = "https://skillsmp.com/api/v1/skills/search"
		params.Set("page", "1")
	case "skills.sh":
		if len([]rune(query)) < 2 {
			return nil, false, fail("invalid_query", "skills.sh 搜索词至少需要 2 字符")
		}
		auth, err = token("SKILLS_SH_TOKEN")
		endpoint = "https://skills.sh/api/search"
		if auth != "" {
			endpoint = "https://skills.sh/api/v1/skills/search"
		}
	}
	if err != nil {
		return nil, false, err
	}
	var payload map[string]any
	if err = c.getJSON(ctx, endpoint+"?"+params.Encode(), auth, 2<<20, &payload); err != nil {
		return nil, false, err
	}
	if payload == nil {
		return nil, false, fail("invalid_response", "来源响应为空")
	}
	if payload["success"] == false || payload["error"] != nil {
		code := strings.ToUpper(first(stringValue(object(payload["error"])["code"]), stringValue(payload["error"])))
		switch code {
		case "SEARCH_COMPLETION_UNCERTAIN":
			return nil, false, fail("search_completion_uncertain", "市场未确认查询计费状态，请先检查用量；未自动重试")
		case "INVALID_API_KEY", "MISSING_API_KEY", "UNAUTHORIZED":
			return nil, false, fail("authentication_required", "来源访问令牌无效或需要配置")
		case "DAILY_QUOTA_EXCEEDED", "MONTHLY_QUOTA_EXCEEDED", "RATE_LIMITED":
			return nil, false, fail("rate_limited", "来源请求达到配额限制")
		default:
			return nil, false, fail("upstream_error", "来源未能完成搜索")
		}
	}
	var raw any
	complete := true
	switch source {
	case "github":
		raw = payload["items"]
		flag, ok := payload["incomplete_results"].(bool)
		if !ok {
			return nil, false, fail("invalid_response", "GitHub 搜索响应缺少完整度信息")
		}
		complete = !flag
	case "clawhub":
		raw = payload["results"]
	case "skillsmp":
		raw = object(payload["data"])["skills"]
	case "skills.sh":
		if auth != "" {
			raw = payload["data"]
		} else {
			raw = payload["skills"]
		}
	}
	rows, ok := raw.([]any)
	if !ok {
		return nil, false, fail("invalid_response", "来源搜索响应缺少条目列表")
	}
	items := []Item{}
	seen := map[string]bool{}
	for n, row := range rows {
		if n >= searchLimit {
			break
		}
		m, ok := row.(map[string]any)
		if !ok {
			complete = false
			continue
		}
		item, ok := normalizeItem(source, m)
		if !ok {
			complete = false
			continue
		}
		if !seen[item.URL] {
			seen[item.URL] = true
			items = append(items, item)
		}
	}
	return items, complete, nil
}

func marketURL(raw, host string) string {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		raw = "https://" + host + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Host != host && u.Host != "www."+host) || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return ""
	}
	for _, r := range raw {
		if r < 32 || r == 127 {
			return ""
		}
	}
	u.Host = host
	return u.String()
}

func addRepository(item *Item, raw string) {
	owner, repo, tree, err := parseGitHub(raw)
	if err != nil {
		return
	}
	item.RepositoryURL = raw
	item.Repository = owner + "/" + repo
	if len(tree) > 1 && isSHA(tree[0]) {
		item.SkillPath = strings.Join(tree[1:], "/")
	}
	item.ImportURL = raw
	item.CanImport = true
}

func normalizeItem(source string, row map[string]any) (Item, bool) {
	item := Item{Source: source, Kind: "skill", ScanState: "not_scanned", Origins: []Origin{}, Metrics: []Metric{}}
	var metricName string
	var metricValue any
	switch source {
	case "github":
		full := stringValue(row["full_name"])
		if _, _, tree, e := parseGitHub("https://github.com/" + full); e != nil || tree != nil {
			return item, false
		}
		item.Name = first(stringValue(row["name"]), full)
		item.Description = stringValue(row["description"])
		item.URL = "https://github.com/" + full
		item.Kind = "repository"
		addRepository(&item, item.URL)
		item.UpdatedAt = stringValue(row["updated_at"])
		metricName, metricValue = "github_stars", row["stargazers_count"]
	case "clawhub":
		item.Slug = stringValue(row["slug"])
		if !componentRE.MatchString(item.Slug) {
			return item, false
		}
		links := object(row["links"])
		item.URL = marketURL(first(stringValue(row["canonicalUrl"]), stringValue(links["canonical"])), "clawhub.ai")
		owner := first(stringValue(row["ownerHandle"]), stringValue(object(row["owner"])["handle"]))
		if owner == "" && item.URL != "" {
			u, _ := url.Parse(item.URL)
			p := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(p) == 3 && p[1] == "skills" && p[2] == item.Slug {
				owner = p[0]
			}
		}
		if item.URL == "" && componentRE.MatchString(owner) {
			item.URL = "https://clawhub.ai/" + url.PathEscape(owner) + "/skills/" + url.PathEscape(item.Slug)
		}
		if item.URL == "" {
			return item, false
		}
		item.Name = first(stringValue(row["displayName"]), stringValue(row["name"]), item.Slug)
		item.Description = first(stringValue(row["summary"]), stringValue(row["description"]))
		item.UpdatedAt = dateValue(row["updatedAt"])
		addRepository(&item, first(stringValue(object(row["install"])["sourceUrl"]), stringValue(links["source"]), stringValue(row["repositoryUrl"])))
		// Download through the owner-qualified registry record, even if it also
		// lists a GitHub repository; the registry can pin another release.
		item.ImportURL, item.CanImport = item.URL, true
		metricName, metricValue = "downloads", row["downloads"]
	case "skillsmp":
		item.Slug = first(stringValue(row["id"]), stringValue(row["slug"]))
		item.Name = stringValue(row["name"])
		item.Description = stringValue(row["description"])
		item.URL = marketURL(stringValue(row["skillUrl"]), "skillsmp.com")
		item.UpdatedAt = dateValue(row["updatedAt"])
		addRepository(&item, stringValue(row["githubUrl"]))
		metricName, metricValue = "github_stars", row["stars"]
	case "skills.sh":
		identity := stringValue(row["id"])
		parts := strings.Split(identity, "/")
		if len(parts) < 2 || len(parts) > 5 {
			return item, false
		}
		for _, p := range parts {
			if !componentRE.MatchString(p) {
				return item, false
			}
		}
		item.Slug = first(stringValue(row["slug"]), stringValue(row["skillId"]), parts[len(parts)-1])
		item.Name = first(stringValue(row["name"]), item.Slug)
		item.Description = stringValue(row["description"])
		item.URL = marketURL(stringValue(row["url"]), "skills.sh")
		if item.URL == "" {
			item.URL = "https://skills.sh/" + strings.Join(parts, "/")
		}
		item.UpdatedAt = dateValue(firstAny(row["updatedAt"], row["updated_at"]))
		addRepository(&item, stringValue(row["installUrl"]))
		if item.RepositoryURL == "" && (row["sourceType"] == nil || row["sourceType"] == "github") {
			addRepository(&item, "https://github.com/"+stringValue(row["source"]))
		}
		metricName, metricValue = "installs", row["installs"]
	}
	if item.Name == "" || item.URL == "" || source != "github" && item.Slug == "" {
		return item, false
	}
	item.Name = short(item.Name, 300)
	item.Description = short(item.Description, 4000)
	item.Origins = append(item.Origins, Origin{Source: source, URL: item.URL})
	if n, ok := number(metricValue); ok {
		item.Metrics = append(item.Metrics, Metric{Name: metricName, Value: n, Source: source})
	}
	return item, true
}

func firstAny(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func dateValue(v any) string {
	if s, ok := v.(string); ok {
		return short(s, 100)
	}
	if n, ok := number(v); ok && n < 1e15 {
		if n > 1e11 {
			n /= 1000
		}
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	return ""
}

func identity(item Item) string {
	if item.RepositoryURL != "" {
		owner, repo, tree, err := parseGitHub(item.RepositoryURL)
		if err == nil {
			if item.Kind == "repository" {
				return "repository\x00" + strings.ToLower(owner+"/"+repo)
			}
			// Only immutable commits unambiguously identify the ref/path boundary.
			if len(tree) > 1 && isSHA(tree[0]) {
				return "path\x00" + strings.ToLower(owner+"/"+repo) + "\x00" + strings.ToLower(tree[0]) + "\x00" + strings.Join(tree[1:], "/")
			}
		}
	}
	return item.Source + "\x00" + item.URL
}

func mergeItems(items []Item) []Item {
	result := []Item{}
	indices := map[string]int{}
	for _, item := range items {
		key := identity(item)
		if index, ok := indices[key]; ok {
			previous := &result[index]
			for _, origin := range item.Origins {
				found := false
				for _, old := range previous.Origins {
					if old == origin {
						found = true
						break
					}
				}
				if !found {
					previous.Origins = append(previous.Origins, origin)
				}
			}
			for _, metric := range item.Metrics {
				found := false
				for _, old := range previous.Metrics {
					if old == metric {
						found = true
						break
					}
				}
				if !found {
					previous.Metrics = append(previous.Metrics, metric)
				}
			}
			if previous.Description == "" {
				previous.Description = item.Description
			}
			continue
		}
		indices[key] = len(result)
		result = append(result, item)
	}
	return result
}

func sourceError(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return fmt.Sprint(err)
}
