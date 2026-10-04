// Package rpc implements the Codex App Server's bidirectional JSONL protocol.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("RPC %d: %s", e.Code, e.Message) }

type Client struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	mu      sync.Mutex
	writeMu sync.Mutex
	pending map[string]chan Message
	seq     atomic.Uint64
	done    chan struct{}
	once    sync.Once
	onEvent func(Message)
	trace   func(string, Message)
}

// Callbacks execute on the reader goroutine: they must not call Call synchronously.
func Start(cmd *exec.Cmd, event func(Message), trace func(string, Message)) (*Client, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, in: in, pending: map[string]chan Message{}, done: make(chan struct{}), onEvent: event, trace: trace}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 4096), 1024*1024)
		for s.Scan() {
			if trace != nil {
				b, _ := json.Marshal(map[string]string{"text": s.Text()})
				trace("stderr", Message{Method: "process/stderr", Params: b})
			}
		}
	}()
	go func() {
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 65536), 32*1024*1024)
		for s.Scan() {
			var m Message
			if err := json.Unmarshal(s.Bytes(), &m); err != nil {
				if trace != nil {
					b, _ := json.Marshal(map[string]string{"error": err.Error()})
					trace("in", Message{Method: "protocol/invalidJSON", Params: b})
				}
				continue
			}
			if trace != nil {
				trace("in", m)
			}
			if m.Method != "" {
				if event != nil {
					event(m)
				}
				continue
			}
			c.mu.Lock()
			ch := c.pending[string(m.ID)]
			delete(c.pending, string(m.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
		// A broken/oversized frame must not leave a child running forever.
		if s.Err() != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		<-stderrDone
		c.finish()
	}()
	return c, nil
}
func (c *Client) finish()               { c.once.Do(func() { close(c.done); _ = c.in.Close() }) }
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Close() {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.in.Close()
}
func (c *Client) Send(m Message) error {
	select {
	case <-c.done:
		return errors.New("app server is closed")
	default:
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_, err = c.in.Write(append(b, '\n'))
	c.writeMu.Unlock()
	if err == nil && c.trace != nil {
		c.trace("out", m)
	}
	return err
}
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("%d", c.seq.Add(1))
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err = c.Send(Message{ID: json.RawMessage(id), Method: method, Params: p}); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("Codex App Server exited")
	}
}
func (c *Client) Reply(id json.RawMessage, result any) error {
	b, e := json.Marshal(result)
	if e != nil {
		return e
	}
	return c.Send(Message{ID: id, Result: b})
}
func (c *Client) Initialize(ctx context.Context) error {
	_, err := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "rundesk", "title": "RunDesk", "version": "0.8.2"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err != nil {
		return err
	}
	return c.Send(Message{Method: "initialized"})
}
