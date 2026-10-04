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
	"time"
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

// TransportError is a failure of the local App Server connection, not an HTTP
// response from the model provider. Keep the original cause for diagnostics.
type TransportError struct {
	Op    string
	Cause error
}

func (e *TransportError) Error() string { return fmt.Sprintf("Codex App Server %s: %v", e.Op, e.Cause) }
func (e *TransportError) Unwrap() error { return e.Cause }

type Client struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	mu        sync.Mutex
	writeGate chan struct{}
	closing   atomic.Bool
	exitErr   error
	pending   map[string]chan Message
	seq       atomic.Uint64
	done      chan struct{}
	once      sync.Once
	onEvent   func(Message)
	trace     func(string, Message)
}

// Callbacks execute on the reader goroutine: they must not call Call synchronously.
func Start(cmd *exec.Cmd, event func(Message), trace func(string, Message)) (*Client, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	c := &Client{cmd: cmd, in: in, writeGate: make(chan struct{}, 1), pending: map[string]chan Message{}, done: make(chan struct{}), onEvent: event, trace: trace}
	if err = cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		_ = stderr.Close()
		return nil, err
	}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		drainStderr(stderr, trace)
	}()
	go func() {
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 65536), 32*1024*1024)
		var readErr error
		for s.Scan() {
			var m Message
			if err := json.Unmarshal(s.Bytes(), &m); err != nil {
				if trace != nil {
					b, _ := json.Marshal(map[string]string{"error": err.Error()})
					trace("in", Message{Method: "protocol/invalidJSON", Params: b})
				}
				readErr = &TransportError{Op: "invalid JSON", Cause: err}
				break
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
		if readErr == nil && s.Err() != nil {
			readErr = &TransportError{Op: "read", Cause: s.Err()}
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
		}
		waitErr := cmd.Wait()
		_ = stderr.Close()
		<-stderrDone
		if readErr == nil {
			if waitErr == nil {
				waitErr = io.EOF
			}
			readErr = &TransportError{Op: "exited", Cause: waitErr}
		}
		c.mu.Lock()
		c.exitErr = readErr
		c.mu.Unlock()
		c.finish()
	}()
	return c, nil
}
func (c *Client) finish()               { c.once.Do(func() { close(c.done); _ = c.in.Close() }) }
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Closing() bool         { return c.closing.Load() }
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exitErr != nil {
		return c.exitErr
	}
	return &TransportError{Op: "closed", Cause: io.ErrClosedPipe}
}
func (c *Client) Close() {
	c.closing.Store(true)
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.in.Close()
}
func (c *Client) Send(m Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.send(ctx, m)
}
func (c *Client) send(ctx context.Context, m Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closing.Load() {
		return c.Err()
	}
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	select {
	case c.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
	if err := ctx.Err(); err != nil {
		<-c.writeGate
		return err
	}
	if c.closing.Load() {
		<-c.writeGate
		return c.Err()
	}
	// A context deadline must also bound a blocked pipe write. A partially sent
	// JSON frame makes this connection unusable; close it, never replay the call.
	written := make(chan error, 1)
	go func() { _, e := c.in.Write(append(b, '\n')); <-c.writeGate; written <- e }()
	select {
	case err = <-written:
	case <-ctx.Done():
		c.Close()
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
	if err != nil {
		c.Close()
		return &TransportError{Op: "write", Cause: err}
	}
	if err == nil && c.trace != nil {
		c.trace("out", m)
	}
	return err
}
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	if err = c.send(ctx, Message{ID: json.RawMessage(id), Method: method, Params: p}); err != nil {
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
		return nil, c.Err()
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
	_, err := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "rundesk", "title": "RunDesk", "version": "0.8.4"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err != nil {
		return err
	}
	return c.send(ctx, Message{Method: "initialized"})
}

// Drain every stderr byte, even when a line exceeds the display bound. Scanner
// stops at its token limit and can deadlock a child that keeps writing stderr.
func drainStderr(reader io.Reader, trace func(string, Message)) {
	const limit = 64 << 10
	r := bufio.NewReaderSize(reader, 4096)
	line := make([]byte, 0, 4096)
	truncated := false
	for {
		part, err := r.ReadSlice('\n')
		keep := len(part)
		if keep > limit-len(line) {
			keep = limit - len(line)
			truncated = true
		}
		line = append(line, part[:keep]...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(line) > 0 && trace != nil {
			text := string(line)
			if truncated {
				text += "\n[stderr line truncated]"
			}
			b, _ := json.Marshal(map[string]string{"text": text})
			trace("stderr", Message{Method: "process/stderr", Params: b})
		}
		line = line[:0]
		truncated = false
		if err != nil {
			return
		}
	}
}
