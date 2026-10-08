// Package kun provides the RunDesk-side client for a separate Kun process.
package kun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	mu      sync.Mutex
	pending map[string]chan p.Envelope
	write   sync.Mutex
	counter atomic.Uint64
	done    chan struct{}
	closed  sync.Once
}

func Launch(ctx context.Context, executable, data string) (*Client, error) {
	cmd := exec.Command(executable, "--data", data)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, input: input, pending: map[string]chan p.Envelope{}, done: make(chan struct{})}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 65536), p.MaxMessage)
		for scanner.Scan() {
			var msg p.Envelope
			if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.Version != p.Version {
				break
			}
			c.mu.Lock()
			ch := c.pending[msg.ID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		close(c.done)
	}()
	var hello struct {
		ProtocolVersion int    `json:"protocolVersion"`
		Engine          string `json:"engine"`
	}
	helloCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = c.Call(helloCtx, "hello", nil, &hello); err != nil || hello.ProtocolVersion != p.Version || hello.Engine != "kun" {
		c.Close()
		if err == nil {
			err = fmt.Errorf("Kun handshake mismatch")
		}
		return nil, err
	}
	return c, nil
}

type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	id := fmt.Sprint(c.counter.Add(1))
	reply := make(chan p.Envelope, 1)
	c.mu.Lock()
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	msg := p.Envelope{Version: p.Version, ID: id, Method: method, Params: p.JSON(params)}
	written := make(chan error, 1)
	go func() { c.write.Lock(); defer c.write.Unlock(); written <- json.NewEncoder(c.input).Encode(msg) }()
	select {
	case err := <-written:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		_ = c.input.Close()
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("Kun worker exited")
	}
	select {
	case response := <-reply:
		if response.Error != "" {
			return &RemoteError{Message: response.Error}
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(response.Result, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("Kun worker exited")
	}
}
func (c *Client) PID() int              { return c.cmd.Process.Pid }
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Close() {
	c.closed.Do(func() {
		_ = c.input.Close()
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			_ = c.cmd.Process.Kill()
			<-c.done
		}
	})
}
