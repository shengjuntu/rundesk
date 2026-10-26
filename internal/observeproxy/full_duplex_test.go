package observeproxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// headerGateConn lets the upstream respond after receiving request headers,
// while the transport is still unable to start reading the request body.
// This makes an early response race deterministic without delaying the client
// upload or changing the proxy's request-body reader.
type headerGateConn struct {
	net.Conn
	allowBody <-chan struct{}
	closed    chan struct{}
	first     sync.Once
	closeOnce sync.Once
}

func (c *headerGateConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.first.Do(func() {
		select {
		case <-c.allowBody:
		case <-c.closed:
		}
	})
	return n, err
}

func (c *headerGateConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func TestSSEEarlyResponsePreservesPendingRequestBody(t *testing.T) {
	allowBody := make(chan struct{})
	var releaseOnce sync.Once
	releaseBody := func() { releaseOnce.Do(func() { close(allowBody) }) }
	defer releaseBody()
	upstreamBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).EnableFullDuplex(); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("upstream request body: %v", err)
		}
		upstreamBody <- string(body)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	dial := p.transport.DialContext
	p.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &headerGateConn{Conn: conn, allowBody: allowBody, closed: make(chan struct{})}, nil
	}
	base := register(t, p, upstream.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("early response: %q %v", line, err)
	}
	releaseBody()
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "\ndata: [DONE]\n\n" {
		t.Fatalf("response after upload: %q %v", rest, err)
	}
	if body := <-upstreamBody; body != "{}" {
		t.Fatalf("upstream body changed: %q", body)
	}
	r := receive(t, records)
	if string(r.Request.Body.Bytes) != "{}" || !r.Request.Body.Complete || !r.Response.Body.Complete || r.Incomplete {
		t.Fatalf("early response capture incomplete: %+v", r)
	}
}
