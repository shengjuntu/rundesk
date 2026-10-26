// Package kun provides a dependency-light client for a separate Kun process.
package kun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
)

type pendingCall struct {
	reply     chan p.Envelope
	delivered bool
}
type writeCall struct {
	ctx    context.Context
	data   []byte
	result chan error
}

// Client owns one worker. Calls may run concurrently. Canceling a Call stops
// waiting for its response; it does not cancel an already accepted engine action.
// Use the protocol's control method to cancel an engine run.
type Client struct {
	cmd         *exec.Cmd
	input       io.WriteCloser
	mu          sync.Mutex
	pending     map[string]*pendingCall
	counter     uint64
	closing     bool
	terminalErr error
	info        p.Hello
	writes      chan writeCall
	done        chan struct{}
	closeOnce   sync.Once
}

// Launch starts a Kun worker and verifies protocol identity before returning.
// ctx limits startup; it does not own the successfully launched worker's lifetime.
func Launch(ctx context.Context, executable, data string) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "--data", data)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		_ = stderr.Close()
		return nil, err
	}
	c := &Client{cmd: cmd, input: input, pending: map[string]*pendingCall{}, writes: make(chan writeCall), done: make(chan struct{})}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go c.writeLoop()
	go c.readLoop(output)
	var hello p.Hello
	helloCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = c.Call(helloCtx, "hello", nil, &hello); err == nil {
		if hello.ProtocolVersion != p.Version || hello.Engine != "kun" || strings.TrimSpace(hello.EngineVersion) == "" || len(hello.EngineVersion) > 128 {
			err = fmt.Errorf("Kun handshake mismatch: engine=%q protocol=%d engineVersion=%q", hello.Engine, hello.ProtocolVersion, hello.EngineVersion)
		}
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	c.mu.Lock()
	c.info = cloneHello(hello)
	c.mu.Unlock()
	return c, nil
}

func cloneHello(info p.Hello) p.Hello {
	if info.Capabilities != nil {
		caps := make(map[string]bool, len(info.Capabilities))
		for key, value := range info.Capabilities {
			caps[key] = value
		}
		info.Capabilities = caps
	}
	return info
}

// Info returns the worker's actual handshake, with an independent capability map.
func (c *Client) Info() p.Hello { c.mu.Lock(); defer c.mu.Unlock(); return cloneHello(c.info) }

// RequireCapabilities checks optional extensions before the host invokes them.
// Missing extensions do not prevent use of the stable core protocol.
func (c *Client) RequireCapabilities(names ...string) error {
	info := c.Info()
	missing := []string{}
	for _, name := range names {
		if name == "" || !info.Capabilities[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("Kun %s lacks required capabilities: %s", info.EngineVersion, strings.Join(missing, ", "))
	}
	return nil
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.terminalErr == nil {
		c.terminalErr = err
	}
	c.mu.Unlock()
	_ = c.input.Close()
	_ = c.cmd.Process.Kill()
}
func (c *Client) exitError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminalErr != nil {
		return c.terminalErr
	}
	return fmt.Errorf("Kun worker exited")
}
func (c *Client) writeLoop() {
	for {
		select {
		case request := <-c.writes:
			if err := request.ctx.Err(); err != nil {
				request.result <- err
				continue
			}
			n, err := c.input.Write(request.data)
			if err == nil && n != len(request.data) {
				err = io.ErrShortWrite
			}
			request.result <- err
			if err != nil {
				c.fail(fmt.Errorf("Kun request write: %w", err))
				return
			}
		case <-c.done:
			return
		}
	}
}
func (c *Client) readLoop(output io.Reader) {
	scanner := bufio.NewScanner(output)
	// Include framing overhead so an exactly MaxMessage-byte JSON payload fits.
	scanner.Buffer(make([]byte, 65536), p.MaxMessage+2)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) > p.MaxMessage {
			c.fail(fmt.Errorf("Kun response exceeds 8 MiB"))
			break
		}
		msg, err := decodeResponse(raw)
		if err != nil {
			c.fail(err)
			break
		}
		c.mu.Lock()
		request := c.pending[msg.ID]
		issued, parseErr := strconv.ParseUint(msg.ID, 10, 64)
		validID := parseErr == nil && issued != 0 && strconv.FormatUint(issued, 10) == msg.ID && issued <= c.counter
		duplicate := request != nil && request.delivered
		if request != nil && !duplicate {
			request.delivered = true
		}
		c.mu.Unlock()
		if !validID || duplicate {
			c.fail(fmt.Errorf("Kun protocol: unexpected response ID %q", msg.ID))
			break
		}
		// A late response for an already completed/canceled Call must never be
		// delivered to another caller, and does not poison a healthy worker.
		if request != nil {
			request.reply <- msg
		}
	}
	if err := scanner.Err(); err != nil {
		c.fail(fmt.Errorf("Kun response framing: %w", err))
	}
	c.mu.Lock()
	if c.terminalErr == nil {
		c.terminalErr = fmt.Errorf("Kun worker exited")
	}
	c.mu.Unlock()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	close(c.done)
}
func decodeResponse(raw []byte) (p.Envelope, error) {
	var msg p.Envelope
	// Duplicate envelope keys must not let two parsers disagree about an ID or version.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return msg, fmt.Errorf("Kun protocol: malformed response object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return msg, fmt.Errorf("Kun protocol: malformed response: %w", err)
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return msg, fmt.Errorf("Kun protocol: duplicate or invalid response field")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return msg, fmt.Errorf("Kun protocol: malformed response: %w", err)
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return msg, fmt.Errorf("Kun protocol: malformed response object")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return msg, fmt.Errorf("Kun protocol: trailing response input")
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return msg, fmt.Errorf("Kun protocol: malformed response: %w", err)
	}
	if msg.Version != p.Version {
		return msg, fmt.Errorf("Kun protocol mismatch: got %d, require %d", msg.Version, p.Version)
	}
	if msg.ID == "" || msg.Method != "" || len(msg.Params) != 0 || msg.Event != nil || (msg.Error == "" && len(msg.Result) == 0) || (msg.Error != "" && len(msg.Result) != 0) {
		return msg, fmt.Errorf("Kun protocol: invalid response envelope")
	}
	return msg, nil
}

type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }

// Call invokes one JSONL request. Responses are correlated only by request ID.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if method == "" {
		return fmt.Errorf("Kun method is required")
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("Kun parameters: %w", err)
	}
	c.mu.Lock()
	if c.closing || c.terminalErr != nil {
		err := c.terminalErr
		if err == nil {
			err = fmt.Errorf("Kun worker closed")
		}
		c.mu.Unlock()
		return err
	}
	c.counter++
	id := strconv.FormatUint(c.counter, 10)
	request := &pendingCall{reply: make(chan p.Envelope, 1)}
	c.pending[id] = request
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	raw, err := json.Marshal(p.Envelope{Version: p.Version, ID: id, Method: method, Params: rawParams})
	if err != nil {
		return fmt.Errorf("Kun request: %w", err)
	}
	if len(raw) > p.MaxMessage {
		return fmt.Errorf("Kun request exceeds 8 MiB")
	}
	written := make(chan error, 1)
	write := writeCall{ctx: ctx, data: append(raw, '\n'), result: written}
	select {
	case c.writes <- write:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.exitError()
	}
	select {
	case err := <-written:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.exitError()
	}
	select {
	case response := <-request.reply:
		if response.Error != "" {
			return &RemoteError{Message: response.Error}
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("Kun response result: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		// Preserve a response already delivered immediately before a clean exit.
		select {
		case response := <-request.reply:
			if response.Error != "" {
				return &RemoteError{Message: response.Error}
			}
			if result == nil {
				return nil
			}
			if err := json.Unmarshal(response.Result, result); err != nil {
				return fmt.Errorf("Kun response result: %w", err)
			}
			return nil
		default:
			return c.exitError()
		}
	}
}
func (c *Client) PID() int              { return c.cmd.Process.Pid }
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()
		_ = c.input.Close()
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			_ = c.cmd.Process.Kill()
			<-c.done
		}
	})
}
