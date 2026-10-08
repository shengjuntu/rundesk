// Package mcp implements bounded MCP tool connections for Kun and RunDesk diagnostics.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

const Limit = 2 << 20

type Client struct {
	mu       sync.Mutex
	seq      uint64
	exchange func(context.Context, map[string]any) (json.RawMessage, error)
	close    func()
	closed   bool
}

func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("MCP connection is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.seq++
	return c.exchange(ctx, map[string]any{"jsonrpc": "2.0", "id": c.seq, "method": method, "params": params})
}
func (c *Client) Initialize(ctx context.Context, name, version string) (json.RawMessage, error) {
	raw, err := c.Call(ctx, "initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": name, "version": version}})
	if err != nil {
		return nil, err
	}
	var v struct {
		Protocol string `json:"protocolVersion"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	switch v.Protocol {
	case "2025-11-25", "2025-06-18", "2025-03-26":
	default:
		return nil, fmt.Errorf("unsupported MCP version: %s", v.Protocol)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.exchange(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return raw, err
}
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		c.close()
	}
}

type ServerRequest struct {
	ID     json.RawMessage
	Method string
}

func (e *ServerRequest) Error() string { return "unsupported MCP server request: " + e.Method }
func Result(raw []byte, id any) (json.RawMessage, bool, error) {
	var v struct {
		Version string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, err
	}
	if v.Version != "2.0" {
		return nil, false, fmt.Errorf("invalid MCP JSON-RPC version")
	}
	if v.Method != "" {
		if len(v.ID) > 0 {
			return nil, false, &ServerRequest{v.ID, v.Method}
		}
		return nil, false, nil
	}
	want, _ := json.Marshal(id)
	if !bytes.Equal(v.ID, want) {
		return nil, false, nil
	}
	if len(v.Error) > 0 && string(v.Error) != "null" {
		return nil, true, fmt.Errorf("MCP JSON-RPC error: %s", v.Error)
	}
	if len(v.Result) == 0 || bytes.Equal(v.Result, []byte("null")) {
		return nil, true, fmt.Errorf("missing MCP result")
	}
	return v.Result, true, nil
}
func serverResponse(err error) (map[string]any, bool) {
	var req *ServerRequest
	if !errors.As(err, &req) {
		return nil, false
	}
	v := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if req.Method == "ping" {
		v["result"] = map[string]any{}
	} else {
		v["error"] = map[string]any{"code": -32601, "message": "Kun does not support " + req.Method}
	}
	return v, true
}
