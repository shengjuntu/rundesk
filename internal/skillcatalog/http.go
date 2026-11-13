package skillcatalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	ownerRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	repoRE      = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	componentRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
	shaRE       = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
)

func isSHA(s string) bool { return shaRE.MatchString(s) }

func validPath(p string) bool {
	if p == "" || len(p) > 1024 || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return false
			}
		}
	}
	return true
}

func token(name string) (string, error) {
	t := os.Getenv(name)
	if len(t) > 16384 {
		return "", fail("invalid_token", "访问令牌格式无效")
	}
	for _, r := range t {
		if r < 33 || r > 126 {
			return "", fail("invalid_token", "访问令牌格式无效")
		}
	}
	return t, nil
}

func (c *Client) request(ctx context.Context, endpoint, auth string, max int64) ([]byte, http.Header, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return nil, nil, fail("invalid_url", "来源接口地址无效")
	}
	switch u.Host {
	case "api.github.com", "clawhub.ai", "skillsmp.com", "skills.sh":
	default:
		return nil, nil, fail("invalid_host", "只允许访问已配置的公开 Skill 来源")
	}
	if !strings.HasPrefix(u.Path, "/api/") && u.Host != "api.github.com" {
		return nil, nil, fail("invalid_url", "来源接口路径无效")
	}
	if strings.ContainsAny(auth, "\r\n") {
		return nil, nil, fail("invalid_token", "访问令牌格式无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, fail("invalid_url", "来源接口地址无效")
	}
	req.Header.Set("Accept", "application/json, application/zip, */*")
	req.Header.Set("User-Agent", "RunDesk-SkillCatalog/0.73")
	if u.Host == "api.github.com" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	var hc http.Client
	if c != nil && c.HTTP != nil {
		hc = *c.HTTP
	}
	if hc.Timeout == 0 {
		hc.Timeout = 20 * time.Second
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fail("network_error", "无法连接 "+u.Host+"，请检查网络或稍后重试")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, response.Header, ctx.Err()
		}
		return nil, response.Header, fail("network_error", "来源响应未能读取完成")
	}
	if int64(len(body)) > max {
		return nil, response.Header, fail("response_too_large", "来源响应超过读取上限")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, response.Header, fail("redirect_refused", "来源发生重定向，已停止请求并保留令牌边界")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		details := map[string]any{"status": response.StatusCode}
		for header, key := range map[string]string{"Retry-After": "retryAfter", "X-RateLimit-Reset": "rateReset"} {
			if v := response.Header.Get(header); v != "" {
				details[key] = v
			}
		}
		code, message := "upstream_error", "来源未能完成请求"
		switch response.StatusCode {
		case 400, 422:
			code, message = "invalid_request", "来源拒绝了查询、版本或路径参数"
		case 401:
			code, message = "authentication_required", "来源认证失败或需要配置访问令牌"
		case 403:
			code, message = "forbidden", "来源拒绝访问"
		case 404, 410:
			code, message = "not_found", "来源、版本或文件不存在，或当前无权访问"
		case 409:
			code, message = "ambiguous_slug", "技能名称存在歧义，请使用包含发布者的完整网址"
		}
		lower := strings.ToLower(string(body))
		if response.StatusCode == 429 || response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "" || strings.Contains(lower, "rate limit")) {
			code, message = "rate_limited", "来源请求达到配额限制，请稍后重试"
		}
		if u.Host == "api.github.com" && response.StatusCode == 422 && strings.Contains(u.Path, "/commits/") && strings.Contains(lower, "no commit found for sha:") {
			code, message = "not_found", "GitHub 版本不存在"
		}
		if strings.Contains(lower, "search_completion_uncertain") {
			code, message = "search_completion_uncertain", "来源未能确认查询计费状态，请先检查账户用量；未自动重试"
		}
		return nil, response.Header, &Error{Code: code, Message: message, Details: details}
	}
	return body, response.Header, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint, auth string, max int64, out any) error {
	b, _, err := c.request(ctx, endpoint, auth, max)
	if err != nil {
		return err
	}
	if json.Unmarshal(b, out) != nil {
		return fail("invalid_response", "来源返回了无法解析的 JSON")
	}
	return nil
}

func stringValue(v any) string     { s, _ := v.(string); return s }
func object(v any) map[string]any  { m, _ := v.(map[string]any); return m }
func number(v any) (float64, bool) { n, ok := v.(float64); return n, ok && n >= 0 && n <= 1e20 }
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func short(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}
