package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

const configCallTimeout = 45 * time.Second

// Only the in-flight native response is shared. There is no TTL cache of
// effective configuration or credentials, and every waiter receives its copy.
type configReadKey struct {
	client *rpc.Client
	params string
}
type configReadFlight struct {
	done chan struct{}
	raw  json.RawMessage
	err  error
}

func configReadOnly(method string) bool {
	switch method {
	case "config/read", "skills/list", "mcpServerStatus/list", "account/read", "model/list", "thread/read":
		return true
	}
	return false
}

func lockRPCMutex(ctx context.Context, mu *sync.RWMutex, exclusive bool) error {
	try := mu.TryRLock
	if exclusive {
		try = mu.TryLock
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if try() {
		return nil
	}
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if try() {
				return nil
			}
		}
	}
}

// Explicit writer admission prevents polling readers from starving writes.
// Existing lifecycle operations still use op.Lock/TryLock and see active reads.
func lockConfigCall(ctx context.Context, h *handle, exclusive bool) error {
	if exclusive {
		h.mu.Lock()
		h.configWriters++
		h.mu.Unlock()
		defer func() { h.mu.Lock(); h.configWriters--; h.mu.Unlock() }()
		return lockRPCMutex(ctx, &h.op, true)
	}
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h.mu.Lock()
		writers := h.configWriters
		h.mu.Unlock()
		if writers == 0 && h.op.TryRLock() {
			h.mu.Lock()
			writers = h.configWriters
			h.mu.Unlock()
			if writers == 0 {
				return nil
			}
			h.op.RUnlock()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func configConnectionIdentity(m *Manager, i Instance, w Workspace) string {
	// Revisions/path/execution changes must not silently keep an old process.
	executable := "container:codex"
	if i.Execution.normalized().Mode == "local" {
		executable = m.codexExecutable()
	}
	body, _ := json.Marshal(struct {
		Codex, Home, Path string
		Revision          int
		Execution         ExecutionSpec
	}{executable, i.CodexHome, w.Path, i.Revision, i.Execution.normalized()})
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

// Caller holds op exclusively. Closing configuration connections never closes
// any session connection, which has its own handle and native thread.
func closeConfigClientLocked(h *handle, expected *rpc.Client) {
	h.mu.Lock()
	c := h.client
	if expected != nil && c != expected {
		h.mu.Unlock()
		return
	}
	h.client, h.configRetiring, h.thread, h.configIdentity = nil, nil, "", ""
	h.mu.Unlock()
	if c != nil {
		c.Close()
		<-c.Done()
	}
}

// Fatal transport cannot serve any peer. Detach it before returning even when
// the Done watcher currently owns or is waiting for op. Unlike a healthy read
// timeout, this does not require upgrading the caller's shared lock.
func detachBrokenConfigClient(h *handle, c *rpc.Client) {
	h.mu.Lock()
	if h.client == c {
		h.client, h.configRetiring, h.thread, h.configIdentity = nil, nil, "", ""
	}
	h.mu.Unlock()
	c.Close()
	<-c.Done()
}

func retireConfigClient(h *handle, c *rpc.Client) {
	h.mu.Lock()
	if h.client == c {
		h.configRetiring = c
	}
	h.mu.Unlock()
}
func closeRetiredConfigClient(h *handle) {
	if !h.op.TryLock() {
		return
	}
	defer h.op.Unlock()
	h.mu.Lock()
	retiring := h.configRetiring
	h.mu.Unlock()
	if retiring != nil {
		closeConfigClientLocked(h, retiring)
	}
}

func (m *Manager) ConfigCall(wid, method string, params any, ids ...string) (json.RawMessage, error) {
	return m.configCallContext(m.ctx, wid, method, params, ids...)
}
func (m *Manager) configCallContext(parent context.Context, wid, method string, params any, ids ...string) (json.RawMessage, error) {
	return m.configCallWithTimeout(parent, configCallTimeout, wid, method, params, ids...)
}
func (m *Manager) configCallWithTimeout(parent context.Context, timeout time.Duration, wid, method string, params any, ids ...string) (raw json.RawMessage, err error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	w, err := m.Workspace(wid)
	if err != nil {
		return nil, err
	}
	i, err := m.Instance(ids...)
	if err != nil {
		return nil, err
	}
	if i.AgentRuntime.Kind == "kun" {
		return nil, failure(400, "kun_config_endpoint", "Kun 使用 Agent 引擎配置和独立 MCP 配置接口")
	}
	if runtimeKind(i.AgentRuntime.Kind) != "codex" {
		return nil, failure(400, "configuration_adapter_unsupported", "此引擎没有已注册的 Codex 配置适配器")
	}
	id := "config-" + i.ID + "-" + wid
	h, err := m.getHandle(id)
	if err != nil {
		return nil, err
	}
	identity := configConnectionIdentity(m, i, w)
	defer func() {
		if errors.Is(err, context.DeadlineExceeded) {
			err = &apiError{Status: 504, Code: "codex_config_timeout", Message: "Codex 配置操作超时，请重试读取；若刚提交写入，请先读取确认结果。", Cause: err}
		} else if err != nil {
			err = fmt.Errorf("Codex %s: %w", method, err)
		}
	}()
	if !configReadOnly(method) {
		if err = lockConfigCall(ctx, h, true); err != nil {
			return nil, err
		}
		defer h.op.Unlock()
		m.invalidateConfigSnapshots()
		c, e := m.configConnectLocked(ctx, id, w, i, h, identity)
		if e != nil {
			return nil, e
		}
		raw, err = c.Call(ctx, method, params)
		if configCallRetires(err) {
			closeConfigClientLocked(h, c)
		}
		return raw, err
	}
	for {
		if err = lockConfigCall(ctx, h, false); err != nil {
			return nil, err
		}
		h.mu.Lock()
		c := h.client
		ready := c != nil && h.configRetiring != c && h.configIdentity == identity && !c.Closing()
		if ready {
			select {
			case <-c.Done():
				ready = false
			default:
			}
		}
		if ready {
			h.last = time.Now()
		}
		h.mu.Unlock()
		if ready {
			var owned bool
			raw, err, owned = configReadCall(ctx, h, c, method, params)
			// A canceled follower never owned the native request. A closed
			// browser read is also not evidence that a healthy process failed.
			var transport *rpc.TransportError
			if errors.As(err, &transport) || c.Closing() {
				// A follower can observe a broken shared response before its
				// leader resumes, so all transport-error callers detach it.
				detachBrokenConfigClient(h, c)
			} else if owned && errors.Is(err, context.DeadlineExceeded) {
				retireConfigClient(h, c)
			}
			h.op.RUnlock()
			closeRetiredConfigClient(h)
			return raw, err
		}
		h.op.RUnlock()
		if err = lockConfigCall(ctx, h, true); err != nil {
			return nil, err
		}
		_, err = m.configConnectLocked(ctx, id, w, i, h, identity)
		h.op.Unlock()
		if err != nil {
			return nil, err
		}
	}
}

func configCallRetires(err error) bool {
	var transport *rpc.TransportError
	return errors.As(err, &transport) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
func (m *Manager) configConnectLocked(ctx context.Context, id string, w Workspace, i Instance, h *handle, identity string) (*rpc.Client, error) {
	h.mu.Lock()
	stale := h.client != nil && (h.configRetiring == h.client || h.configIdentity != identity)
	h.mu.Unlock()
	if stale {
		closeConfigClientLocked(h, nil)
	}
	c, err := m.connectContext(ctx, id, w, h, i.ID)
	if err == nil {
		h.mu.Lock()
		h.configIdentity = identity
		h.mu.Unlock()
	}
	return c, err
}
func configReadCall(ctx context.Context, h *handle, c *rpc.Client, method string, params any) (json.RawMessage, error, bool) {
	if method != "config/read" {
		raw, err := c.Call(ctx, method, params)
		return raw, err, true
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err, false
	}
	key := configReadKey{client: c, params: string(encoded)}
	h.mu.Lock()
	if flight := h.configReads[key]; flight != nil {
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err(), false
		case <-flight.done:
			if errors.Is(flight.err, context.Canceled) && ctx.Err() == nil {
				// The first HTTP reader disappeared; a live follower may issue
				// its own safe read after that in-flight result was removed.
				return configReadCall(ctx, h, c, method, params)
			}
			return append(json.RawMessage(nil), flight.raw...), flight.err, false
		}
	}
	flight := &configReadFlight{done: make(chan struct{})}
	if h.configReads == nil {
		h.configReads = make(map[configReadKey]*configReadFlight)
	}
	h.configReads[key] = flight
	h.mu.Unlock()
	raw, err := c.Call(ctx, method, params)
	h.mu.Lock()
	delete(h.configReads, key)
	flight.raw, flight.err = raw, err
	close(flight.done)
	h.mu.Unlock()
	return append(json.RawMessage(nil), raw...), err, true
}
