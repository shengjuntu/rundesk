package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func HTTP(endpoint string, headers http.Header) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	h := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("MCP redirects are disabled") }}
	headers = headers.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	session, version := "", ""
	request := func(ctx context.Context, method string, body []byte) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header = headers.Clone()
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		if version != "" {
			req.Header.Set("MCP-Protocol-Version", version)
		}
		return h.Do(req)
	}
	c := &Client{}
	c.close = func() {
		defer transport.CloseIdleConnections()
		if session == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if res, err := request(ctx, "DELETE", nil); err == nil {
			res.Body.Close()
		}
	}
	c.exchange = func(ctx context.Context, msg map[string]any) (json.RawMessage, error) {
		defer func() {
			if ctx.Err() == nil || msg["method"] == "initialize" || msg["id"] == nil {
				return
			}
			cancelCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			notice, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": msg["id"], "reason": "Kun request cancelled"}})
			if reply, e := request(cancelCtx, "POST", notice); e == nil {
				reply.Body.Close()
			}
		}()
		b, _ := json.Marshal(msg)
		res, err := request(ctx, "POST", b)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode > 299 {
			return nil, fmt.Errorf("MCP HTTP %d; request is not retried", res.StatusCode)
		}
		id, ok := msg["id"]
		if !ok {
			return nil, nil
		}
		reader := io.LimitReader(res.Body, Limit+1)
		var raw json.RawMessage
		consume := func(data []byte) (bool, error) {
			value, match, e := Result(data, id)
			if reply, ok := serverResponse(e); ok {
				encoded, _ := json.Marshal(reply)
				rr, err := request(ctx, "POST", encoded)
				if err != nil {
					return false, err
				}
				rr.Body.Close()
				if rr.StatusCode < 200 || rr.StatusCode > 299 {
					return false, fmt.Errorf("MCP server rejected client response")
				}
				return false, nil
			}
			if match {
				raw = value
			}
			return match, e
		}
		if strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
			found := errors.New("matching MCP response")
			total := 0
			err = ConsumeSSEStream(reader, ConsumeSSEOptions{MaxEventBytes: Limit, OnEvent: func(event SSEEvent) error {
				total += len(event.Data)
				if total > Limit {
					return fmt.Errorf("MCP response exceeds 2 MiB")
				}
				if strings.TrimSpace(event.Data) == "" {
					return nil
				}
				match, e := consume([]byte(event.Data))
				if e != nil {
					return e
				}
				if match {
					return found
				}
				return nil
			}})
			if err == found {
				err = nil
			} else if err == nil {
				err = fmt.Errorf("MCP stream ended without a matching response; request is not retried")
			}
		} else {
			b, err = io.ReadAll(reader)
			if err == nil && len(b) > Limit {
				err = fmt.Errorf("MCP response exceeds 2 MiB")
			}
			if err == nil {
				var match bool
				match, err = consume(b)
				if err == nil && !match {
					err = fmt.Errorf("unmatched MCP response")
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if msg["method"] == "initialize" {
			session = res.Header.Get("Mcp-Session-Id")
			if len(session) > 4096 {
				return nil, fmt.Errorf("invalid MCP session ID")
			}
			for _, c := range session {
				if c < 0x21 || c > 0x7e {
					return nil, fmt.Errorf("invalid MCP session ID")
				}
			}
			var v struct {
				Protocol string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(raw, &v)
			version = v.Protocol
		}
		return raw, nil
	}
	return c
}
