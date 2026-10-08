// Package mcptest retains the one-shot diagnostic API over the shared MCP transport.
package mcptest

import (
	"context"
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/mcp"
	"net/http"
	"os/exec"
)

const Limit = mcp.Limit

type Client struct {
	client *mcp.Client
	ctx    context.Context
	Close  func()
}

func wrap(ctx context.Context, c *mcp.Client) *Client {
	return &Client{client: c, ctx: ctx, Close: c.Close}
}
func (c *Client) Call(method string, params any) (json.RawMessage, error) {
	return c.client.Call(c.ctx, method, params)
}
func (c *Client) Initialize() error {
	_, err := c.client.Initialize(c.ctx, "rundesk-mcp-test", "0.31.0")
	return err
}
func Stdio(ctx context.Context, cmd *exec.Cmd) (*Client, error) {
	c, e := mcp.Stdio(ctx, cmd)
	if e != nil {
		return nil, e
	}
	return wrap(ctx, c), nil
}
func HTTP(ctx context.Context, url string, headers http.Header) *Client {
	return wrap(ctx, mcp.HTTP(url, headers))
}
func result(raw []byte, id any) (json.RawMessage, bool, error) { return mcp.Result(raw, id) }
