// Package mcptest implements bounded, one-shot MCP diagnostic connections.
// Supported handshake revisions: 2025-11-25, 2025-06-18, 2025-03-26.
package mcptest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const Limit = 2 << 20

type Client struct {
	ctx      context.Context
	exchange func(map[string]any) (json.RawMessage, error)
	Close    func()
	seq      int
}

func (c *Client) Call(method string, params any) (json.RawMessage, error) {
	c.seq++
	return c.exchange(map[string]any{"jsonrpc": "2.0", "id": c.seq, "method": method, "params": params})
}
func result(raw []byte, id any) (json.RawMessage, bool, error) {
	var v struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return nil, false, e
	}
	if v.Method != "" {
		if len(v.ID) > 0 {
			return nil, false, fmt.Errorf("server requests (sampling/elicitation) are unsupported")
		}
		return nil, false, nil
	}
	want, _ := json.Marshal(id)
	if !bytes.Equal(v.ID, want) {
		return nil, false, nil
	}
	if len(v.Error) > 0 && string(v.Error) != "null" {
		return nil, true, fmt.Errorf("MCP error: %s", v.Error)
	}
	if len(v.Result) == 0 {
		return nil, true, fmt.Errorf("missing MCP result")
	}
	return v.Result, true, nil
}
func (c *Client) Initialize() error {
	raw, e := c.Call("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "rundesk-mcp-test", "version": "0.17.0"}})
	if e != nil {
		return e
	}
	var v struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		return e
	}
	switch v.ProtocolVersion {
	case "2025-11-25", "2025-06-18", "2025-03-26":
	default:
		return fmt.Errorf("unsupported negotiated MCP version: %s", v.ProtocolVersion)
	}
	_, e = c.exchange(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return e
}
func Stdio(ctx context.Context, cmd *exec.Cmd) (*Client, error) {
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		return nil, e
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		in.Close()
		out.Close()
		return nil, e
	}
	c := &Client{ctx: ctx}
	c.Close = func() {
		in.Close()
		if cmd.Cancel != nil {
			_ = cmd.Cancel()
		} else {
			_ = cmd.Process.Kill()
		}
		out.Close()
		_ = cmd.Wait()
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), Limit)
	c.exchange = func(msg map[string]any) (json.RawMessage, error) {
		type answer struct {
			raw json.RawMessage
			err error
		}
		done := make(chan answer, 1)
		go func() {
			if e := json.NewEncoder(in).Encode(msg); e != nil {
				done <- answer{nil, e}
				return
			}
			id, ok := msg["id"]
			if !ok {
				done <- answer{}
				return
			}
			total := 0
			for scanner.Scan() {
				total += len(scanner.Bytes())
				if total > Limit {
					done <- answer{nil, fmt.Errorf("MCP response exceeds 2 MiB")}
					return
				}
				r, match, e := result(scanner.Bytes(), id)
				if e != nil || match {
					done <- answer{r, e}
					return
				}
			}
			e := scanner.Err()
			if e == nil {
				e = io.EOF
			}
			done <- answer{nil, e}
		}()
		select {
		case a := <-done:
			return a.raw, a.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c, nil
}
func HTTP(ctx context.Context, endpoint string, headers http.Header) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	h := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("MCP redirects are disabled") }}
	session, version := "", ""
	c := &Client{ctx: ctx}
	c.Close = func() {
		defer transport.CloseIdleConnections()
		if session == "" {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(closeCtx, "DELETE", endpoint, nil)
		if e != nil {
			return
		}
		req.Header = headers.Clone()
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("MCP-Protocol-Version", version)
		if resp, e := h.Do(req); e == nil {
			resp.Body.Close()
		}
	}
	c.exchange = func(msg map[string]any) (json.RawMessage, error) {
		b, _ := json.Marshal(msg)
		req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
		if e != nil {
			return nil, e
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
		resp, e := h.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("MCP HTTP %d", resp.StatusCode)
		}
		id, ok := msg["id"]
		if !ok {
			return nil, nil
		}
		reader := io.LimitReader(resp.Body, Limit+1)
		var raw json.RawMessage
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			scan := bufio.NewScanner(reader)
			scan.Buffer(make([]byte, 4096), Limit)
			data := ""
			total := 0
			matched := false
			for scan.Scan() {
				line := scan.Text()
				total += len(line) + 1
				if total > Limit {
					return nil, fmt.Errorf("MCP response exceeds 2 MiB")
				}
				if line == "" && data != "" {
					var match bool
					raw, match, e = result([]byte(strings.TrimSuffix(data, "\n")), id)
					data = ""
					if e != nil {
						return nil, e
					}
					if match {
						matched = true
						break
					}
				} else if strings.HasPrefix(line, "data:") {
					data += strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ") + "\n"
				}
			}
			if e = scan.Err(); e != nil {
				return nil, e
			}
			if !matched {
				return nil, fmt.Errorf("SSE ended without matching MCP result")
			}
		} else {
			b, e = io.ReadAll(reader)
			if e != nil {
				return nil, e
			}
			if len(b) > Limit {
				return nil, fmt.Errorf("MCP response exceeds 2 MiB")
			}
			var match bool
			raw, match, e = result(b, id)
			if e != nil {
				return nil, e
			}
			if !match {
				return nil, fmt.Errorf("unmatched MCP response")
			}
		}
		if msg["method"] == "initialize" {
			session = resp.Header.Get("Mcp-Session-Id")
			var v struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(raw, &v)
			version = v.ProtocolVersion
		}
		return raw, nil
	}
	return c
}
