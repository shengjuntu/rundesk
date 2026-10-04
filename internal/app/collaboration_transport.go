package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var boardName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func collaborationURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("仅支持无内嵌凭据的 HTTP/HTTPS 地址")
	}
	if u.RawQuery != "" {
		return fmt.Errorf("服务地址不能包含查询参数")
	}
	return nil
}

// Connections are administrator-configured. Private/LAN endpoints are intentional;
// redirects are forbidden to avoid forwarding credentials to a different service.
type collaborationHTTPError struct{ Status int }

func (e *collaborationHTTPError) Error() string { return fmt.Sprintf("远程服务 HTTP %d", e.Status) }
func collaborationHTTP(ctx context.Context, method, target, tokenEnv string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if tokenEnv != "" {
		secret := os.Getenv(tokenEnv)
		if secret == "" {
			return fmt.Errorf("未设置凭据环境变量 %s", tokenEnv)
		}
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("远程连接失败或超时")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &collaborationHTTPError{Status: res.StatusCode}
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil || len(b) > 2<<20 {
		return fmt.Errorf("远程响应不可读或超过 2 MiB")
	}
	if out != nil {
		if e = json.Unmarshal(b, out); e != nil {
			return fmt.Errorf("远程服务未返回有效 JSON")
		}
	}
	return nil
}

type a2aPart struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}
type a2aMessage struct {
	Kind      string    `json:"kind"`
	Role      string    `json:"role"`
	MessageID string    `json:"messageId"`
	TaskID    string    `json:"taskId,omitempty"`
	ContextID string    `json:"contextId,omitempty"`
	Parts     []a2aPart `json:"parts"`
}
type a2aTask struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State     string      `json:"state"`
		Message   *a2aMessage `json:"message,omitempty"`
		Timestamp string      `json:"timestamp,omitempty"`
	} `json:"status"`
	Artifacts []json.RawMessage `json:"artifacts,omitempty"`
	History   []a2aMessage      `json:"history,omitempty"`
}

func a2aCall(ctx context.Context, a CollaborationAgent, method string, params any) (a2aTask, error) {
	var t a2aTask
	id := "rundesk"
	var reply struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	e := collaborationHTTP(ctx, "POST", a.Endpoint, a.TokenEnv, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}, &reply)
	if e != nil {
		return t, e
	}
	if reply.Error != nil {
		return t, fmt.Errorf("A2A 错误 %d", reply.Error.Code)
	}
	if reply.JSONRPC != "2.0" || reply.ID != id {
		return t, fmt.Errorf("A2A 响应 ID 或版本不匹配")
	}
	if e = json.Unmarshal(reply.Result, &t); e != nil {
		return t, e
	}
	if t.Kind == "message" {
		var msg a2aMessage
		if e = json.Unmarshal(reply.Result, &msg); e != nil {
			return t, e
		}
		t.ID = msg.MessageID
		t.ContextID = msg.ContextID
		t.Status.State = "completed"
		t.Status.Message = &msg
		t.Kind = "task"
	}
	if t.Kind != "task" || t.ID == "" {
		return t, fmt.Errorf("A2A 返回缺少任务标识")
	}
	switch t.Status.State {
	case "submitted", "working", "input-required", "auth-required", "completed", "canceled", "failed", "rejected":
	default:
		return t, fmt.Errorf("未知 A2A 任务状态")
	}
	return t, nil
}
func applyA2ATask(w *CollaborationWork, t a2aTask) {
	w.RemoteID = t.ID
	w.Error = ""
	w.Status = t.Status.State
	if w.Status == "submitted" {
		w.Status = "working"
	}
	if w.Status == "auth-required" {
		w.Status = "input-required"
	}
	if w.Status == "rejected" {
		w.Status = "failed"
	}
	w.Artifacts = t.Artifacts
	texts := []string{}
	for _, raw := range t.Artifacts {
		var a struct {
			Parts []a2aPart `json:"parts"`
		}
		if json.Unmarshal(raw, &a) == nil {
			for _, p := range a.Parts {
				if p.Kind == "text" {
					texts = append(texts, p.Text)
				}
			}
		}
	}
	if len(texts) == 0 && t.Status.Message != nil {
		for _, p := range t.Status.Message.Parts {
			if p.Kind == "text" {
				texts = append(texts, p.Text)
			}
		}
	}
	if len(texts) == 0 {
		for _, msg := range t.History {
			if msg.Role == "agent" {
				for _, p := range msg.Parts {
					if p.Kind == "text" {
						texts = append(texts, p.Text)
					}
				}
			}
		}
	}
	w.Result = strings.Join(texts, "\n")
	if len(w.Result) > 64000 {
		w.Result = w.Result[:64000]
	}
}

type boardComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

func boardBase(b BlackboardConfig) string {
	return strings.TrimRight(b.URL, "/") + "/api/v1/repos/" + url.PathEscape(b.Owner) + "/" + url.PathEscape(b.Repo)
}
func (m *Manager) boardComments(c *Collaboration) ([]boardComment, error) {
	out := []boardComment{}
	for page := 1; page <= 20; page++ {
		var rows []boardComment
		e := collaborationHTTP(m.ctx, "GET", fmt.Sprintf("%s/issues/%d/comments?limit=50&page=%d", boardBase(c.Board), c.Issue, page), c.Board.TokenEnv, nil, &rows)
		if e != nil {
			return nil, e
		}
		out = append(out, rows...)
		if len(rows) < 50 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("黑板评论超过 1000 条，请拆分协作任务")
}
func (m *Manager) syncBlackboard(c *Collaboration) error {
	if c.Board.URL == "" || (c.Status == "canceled" && c.Issue == 0) {
		return nil
	}
	at, _ := time.Parse(time.RFC3339Nano, c.NextSync)
	if at.After(time.Now()) {
		return nil
	}
	c.NextSync = time.Now().Add(15 * time.Second).UTC().Format(time.RFC3339Nano)
	if os.Getenv(c.Board.TokenEnv) == "" {
		return fmt.Errorf("未设置凭据环境变量 %s", c.Board.TokenEnv)
	}
	base := boardBase(c.Board)
	marker := "<!-- rundesk-collaboration:" + c.ID + " -->"
	if c.Issue == 0 {
		// Before a retry, reconcile the deterministic marker; never blindly recreate.
		if c.BoardAttempted {
			for page := 1; page <= 20; page++ {
				var rows []struct {
					Number  int64  `json:"number"`
					Body    string `json:"body"`
					HTMLURL string `json:"html_url"`
				}
				e := collaborationHTTP(m.ctx, "GET", fmt.Sprintf("%s/issues?state=all&type=issues&limit=50&page=%d", base, page), c.Board.TokenEnv, nil, &rows)
				if e != nil {
					return e
				}
				for _, x := range rows {
					if strings.Contains(x.Body, marker) {
						c.Issue = x.Number
						c.BoardURL = x.HTMLURL
						break
					}
				}
				if c.Issue > 0 {
					break
				}
				if len(rows) < 50 {
					return fmt.Errorf("上次建单结果未确认，请核对 Gitea 后绑定 Issue；不会自动重复创建")
				}
			}
			if c.Issue == 0 {
				return fmt.Errorf("未找到黑板 Issue，请手工绑定")
			}
		} else {
			c.BoardAttempted = true
			if e := m.saveCollaboration(c); e != nil {
				return e
			}
			var v struct {
				Number  int64  `json:"number"`
				HTMLURL string `json:"html_url"`
			}
			title := c.Goal
			if len([]rune(title)) > 80 {
				title = string([]rune(title)[:80])
			}
			if e := collaborationHTTP(m.ctx, "POST", base+"/issues", c.Board.TokenEnv, map[string]any{"title": "[RunDesk] " + title, "body": marker + "\n\n" + c.Goal}, &v); e != nil {
				var he *collaborationHTTPError
				if errors.As(e, &he) && (he.Status == 400 || he.Status == 401 || he.Status == 403 || he.Status == 404 || he.Status == 405 || he.Status == 422 || he.Status == 429) {
					c.BoardAttempted = false
				}
				return e
			}
			if v.Number < 1 {
				return fmt.Errorf("Gitea 未返回 Issue 编号")
			}
			c.Issue = v.Number
			c.BoardURL = v.HTMLURL
			if e := m.saveCollaboration(c); e != nil {
				return e
			}
		}
	}
	rows, e := m.boardComments(c)
	if e != nil {
		return e
	}
	for _, r := range rows {
		if r.ID <= c.CommentCursor {
			continue
		}
		if r.ID > c.CommentCursor {
			c.CommentCursor = r.ID
		}
		if !strings.Contains(r.Body, "<!-- rundesk-entry:") && len(c.Entries) < 500 {
			body := r.Body
			if len(body) > 16000 {
				body = body[:16000]
			}
			c.entry("board-comment", r.User.Login, body)
			c.Entries[len(c.Entries)-1].Mirrored = true
			if c.Status == "running" {
				c.Wake = true
			}
		}
	}
	for i := range c.Entries {
		v := &c.Entries[i]
		if v.Mirrored {
			continue
		}
		tag := "<!-- rundesk-entry:" + v.ID + " -->"
		found := false
		for _, r := range rows {
			if strings.Contains(r.Body, tag) {
				found = true
				break
			}
		}
		if !found {
			if e = collaborationHTTP(m.ctx, "POST", fmt.Sprintf("%s/issues/%d/comments", base, c.Issue), c.Board.TokenEnv, map[string]any{"body": tag + "\n\n**" + v.Kind + " · " + v.Author + "**\n\n" + v.Text}, nil); e != nil {
				return e
			}
		}
		v.Mirrored = true
		if e = m.saveCollaboration(c); e != nil {
			return e
		}
	}
	c.BoardError = ""
	return nil
}
