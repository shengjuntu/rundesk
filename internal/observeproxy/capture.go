package observeproxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxChunks = 4096

type capture struct {
	mu       sync.Mutex
	proxy    *Proxy
	started  time.Time
	expected int64
	body     Body
	frozen   bool
	// Sources are read only after their body reader observes EOF, when net/http
	// has finished populating trailers. Snapshotting a live source map races the
	// transport for early upstream responses.
	trailerSource http.Header
	trailers      map[string][]string
}

func (p *Proxy) newCapture(started time.Time, expected int64) *capture {
	return &capture{proxy: p, started: started, expected: expected, body: Body{Complete: expected == 0}}
}

func (c *capture) add(b []byte) {
	if len(b) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.frozen {
		return
	}
	offset := c.body.Size
	c.body.Size += int64(len(b))
	n := len(b)
	// Retained bytes must always remain a contiguous prefix. Reclaiming global
	// budget after a dropped segment must never splice later bytes onto it.
	if c.body.Truncated {
		n = 0
	}
	if remaining := c.proxy.maxBody - int64(len(c.body.Bytes)); int64(n) > remaining {
		n = int(remaining)
	}
	n = c.proxy.reserve(n)
	if n > 0 {
		c.body.Bytes = append(c.body.Bytes, b[:n]...)
		if len(c.body.Chunks) < maxChunks {
			c.body.Chunks = append(c.body.Chunks, Chunk{Offset: offset, Size: n, ElapsedMicros: time.Since(c.started).Microseconds()})
		} else {
			c.body.ChunkMetadataTruncated = true
		}
	}
	if n != len(b) {
		c.body.Truncated = true
	}
	if c.expected >= 0 && c.body.Size >= c.expected {
		c.body.Complete = true
	}
}

func (c *capture) ended(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.frozen {
		return
	}
	if errors.Is(err, io.EOF) {
		c.body.Complete = c.expected < 0 || c.body.Size == c.expected
		if !c.body.Complete {
			c.body.Error = io.ErrUnexpectedEOF.Error()
		}
		c.trailers, _ = redactHeaders(c.trailerSource)
	} else if err != nil {
		// Transport can close the request reader after receiving an early
		// response while its writer makes one final probe read. If all declared
		// bytes were already observed, that probe does not make bytes missing.
		if c.expected >= 0 && c.body.Size == c.expected {
			return
		}
		c.body.Error = err.Error()
		c.body.Complete = false
	}
}

func (c *capture) capturedTrailers() map[string][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.trailers
}

func (c *capture) snapshot() Body {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = true
	return c.body
}

type readBody struct {
	io.ReadCloser
	capture *capture
}

func (b *readBody) Read(buf []byte) (int, error) {
	n, err := b.ReadCloser.Read(buf)
	b.capture.add(buf[:n])
	if err != nil {
		b.capture.ended(err)
	}
	return n, err
}

type duplexBody struct {
	io.ReadWriteCloser
	upstream, client *capture
	proxy            *Proxy
	once             sync.Once
	mu               sync.Mutex
	closed           bool
	active           sync.WaitGroup
}

func (b *duplexBody) Read(buf []byte) (int, error) {
	if !b.begin() {
		return 0, net.ErrClosed
	}
	defer b.active.Done()
	n, err := b.ReadWriteCloser.Read(buf)
	b.client.add(buf[:n])
	if err != nil {
		b.client.ended(err)
	}
	return n, err
}

func (b *duplexBody) Write(buf []byte) (int, error) {
	if !b.begin() {
		return 0, net.ErrClosed
	}
	defer b.active.Done()
	n, err := b.ReadWriteCloser.Write(buf)
	b.upstream.add(buf[:n])
	if err != nil {
		b.upstream.ended(err)
	}
	return n, err
}

func (b *duplexBody) CloseWrite() error {
	// Preserve half-close support exposed by net/http's upgraded body. Without
	// it, ReverseProxy would close the connection as soon as the caller finishes
	// writing and could discard the server's final response.
	closer, ok := b.ReadWriteCloser.(interface{ CloseWrite() error })
	if !ok {
		return fmt.Errorf("upgraded connection does not support CloseWrite")
	}
	err := closer.CloseWrite()
	if err == nil {
		b.upstream.ended(io.EOF)
	}
	return err
}

func (b *duplexBody) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	err := b.ReadWriteCloser.Close()
	b.active.Wait()
	b.once.Do(func() {
		b.proxy.duplexMu.Lock()
		delete(b.proxy.duplex, b)
		b.proxy.duplexMu.Unlock()
	})
	return err
}

func (b *duplexBody) begin() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	b.active.Add(1)
	return true
}

func redactHeaders(h http.Header) (map[string][]string, []string) {
	copy := make(map[string][]string, len(h))
	var redacted []string
	for name, values := range h {
		key := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
		if strings.Contains(key, "authorization") || strings.Contains(key, "apikey") || strings.Contains(key, "token") || strings.Contains(key, "secret") || key == "cookie" || key == "setcookie" || key == "proxyauthenticate" {
			copy[name] = []string{"[redacted]"}
			redacted = append(redacted, name)
		} else {
			copy[name] = append([]string(nil), values...)
		}
	}
	sort.Strings(redacted)
	return copy, redacted
}

func recordBytes(r Record) int64 {
	n := int64(len(r.Request.Body.Bytes) + len(r.Response.Body.Bytes))
	if r.WebSocket != nil {
		n += int64(len(r.WebSocket.ToUpstream.Bytes) + len(r.WebSocket.ToClient.Bytes))
	}
	return n
}

func bodyIncomplete(b Body) bool {
	return !b.Complete || b.Truncated || b.ChunkMetadataTruncated || b.Error != ""
}
