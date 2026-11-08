// Package debugmcp exposes a fixed RunDesk session through read-only MCP tools.
// It can only issue GET requests to the shared, authenticated DebugService API.
package debugmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	d "github.com/shengjuntu/rundesk/internal/debugapi"
)

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(origin, session, token string) (*Client, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("debug MCP requires a server origin without credentials, path, query or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("debug MCP requires HTTPS or loopback HTTP")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(session) {
		return nil, fmt.Errorf("invalid fixed session ID")
	}
	if token == "" || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\x00") {
		return nil, fmt.Errorf("debug MCP token environment variable is empty or invalid")
	}
	u.Path = "/api/v1/sessions/" + session + "/debug/"
	return &Client{base: u.String(), token: token, http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Get(ctx context.Context, kind string, values url.Values) (json.RawMessage, error) {
	path := "query"
	if kind == "capabilities" {
		path = "capabilities"
	} else {
		values.Set("kind", kind)
	}
	u := c.base + path
	if len(values) > 0 {
		u += "?" + values.Encode()
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if e != nil {
		return nil, fmt.Errorf("cannot construct debug request")
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Accept", "application/json")
	response, e := c.http.Do(r)
	if e != nil {
		return nil, fmt.Errorf("debug service unavailable or request timed out")
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, d.MaxResponseBytes+1))
	if e != nil {
		return nil, fmt.Errorf("cannot read debug response")
	}
	if len(b) > d.MaxResponseBytes {
		return nil, fmt.Errorf("debug response exceeds 4 MiB; request a narrower query or event chunk")
	}
	if response.StatusCode != http.StatusOK {
		var result struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(b, &result)
		if !regexp.MustCompile(`^[a-z_]{1,64}$`).MatchString(result.Code) {
			result.Code = "debug_request_failed"
		}
		return nil, fmt.Errorf("debug service HTTP %d (%s); check capabilities and read access", response.StatusCode, result.Code)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("debug service returned invalid JSON")
	}
	return json.RawMessage(b), nil
}
