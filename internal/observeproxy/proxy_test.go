package observeproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testProxy(t *testing.T, config Config) (*Proxy, <-chan Record) {
	t.Helper()
	records := make(chan Record, 32)
	if config.Record == nil {
		config.Record = func(r Record) error { records <- r; return nil }
	}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return p, records
}

func receive(t *testing.T, records <-chan Record) Record {
	t.Helper()
	select {
	case r := <-records:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("no observation received")
		return Record{}
	}
}

func register(t *testing.T, p *Proxy, upstream string) string {
	t.Helper()
	base, _, err := p.Register(upstream, func() Labels {
		return Labels{SessionID: "session-1", RunID: "run-1", ThreadID: "thread-1", TurnID: "turn-1"}
	})
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func TestHTTPPreservesBodyEncodedPathQueryAndHeaders(t *testing.T) {
	requestBody := []byte(" {\"number\":9007199254740993,\"model\":\"keep-me\",\"temperature\":0,\"empty\":null}\n")
	responseBody := []byte("\x00{ \"large\":9007199254740995 }\xff")
	var upstreamBody []byte
	var upstreamURI, upstreamAuth, upstreamCookie, upstreamForwarded string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		upstreamURI = r.RequestURI
		upstreamAuth, upstreamCookie = r.Header.Get("Authorization"), r.Header.Get("Cookie")
		upstreamForwarded = r.Header.Get("X-Forwarded-For")
		if r.Header.Get("Accept-Encoding") != "" {
			t.Error("proxy added compression negotiation")
		}
		w.Header().Set("X-Request-ID", "req-original")
		w.Header().Set("Set-Cookie", "secret=response")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	base := register(t, p, upstream.URL+"/v1")
	req, _ := http.NewRequest(http.MethodPost, base+"/keep%2Fencoded//path?key=a%2Bb&key=&bad=%zz&semi=a;b", bytes.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer original-token")
	req.Header.Set("Cookie", "secret=request")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(body, responseBody) || !bytes.Equal(upstreamBody, requestBody) {
		t.Fatalf("body changed: request=%q response=%q", upstreamBody, body)
	}
	if upstreamURI != "/v1/keep%2Fencoded//path?key=a%2Bb&key=&bad=%zz&semi=a;b" {
		t.Fatalf("path or query changed: %s", upstreamURI)
	}
	if upstreamAuth != "Bearer original-token" || upstreamCookie != "secret=request" || upstreamForwarded != "" {
		t.Fatalf("forwarded headers changed: auth=%q cookie=%q forwarded=%q", upstreamAuth, upstreamCookie, upstreamForwarded)
	}
	if resp.StatusCode != 422 || resp.Header.Get("Set-Cookie") != "secret=response" {
		t.Fatal("response metadata changed")
	}
	r := receive(t, records)
	if r.Incomplete || r.Association != "request-start" || r.Labels.RunID != "run-1" {
		t.Fatalf("capture metadata: %+v", r)
	}
	if !bytes.Equal(r.Request.Body.Bytes, requestBody) || !bytes.Equal(r.Response.Body.Bytes, responseBody) {
		t.Fatal("stored raw bodies changed")
	}
	if r.Request.Headers["Authorization"][0] != "[redacted]" || r.Request.Headers["Cookie"][0] != "[redacted]" || r.Response.Headers["Set-Cookie"][0] != "[redacted]" {
		t.Fatal("stored credentials not redacted")
	}
	if r.Response.Status != 422 || r.Response.Headers["X-Request-Id"][0] != "req-original" {
		t.Fatal("stored status/request id lost")
	}
}

func TestSSEDeliveredBeforeUpstreamCompletes(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"first\"}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	base := register(t, p, upstream.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/responses", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "event: response.output_text.delta\n" {
		t.Fatalf("stream buffered until completion: %q %v", line, err)
	}
	close(release)
	rest, err := io.ReadAll(reader)
	_ = resp.Body.Close()
	if err != nil || !strings.HasSuffix(string(rest), "data: [DONE]\n\n") {
		t.Fatalf("stream end changed: %q %v", rest, err)
	}
	r := receive(t, records)
	if !r.Response.Body.Complete || r.Incomplete || len(r.Response.Body.Chunks) < 2 {
		t.Fatalf("stream capture incomplete: %+v", r)
	}
}

func TestCancellationStopsUpstreamAndMarksIncomplete(t *testing.T) {
	canceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	base := register(t, p, upstream.URL)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/responses", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bufio.NewReader(resp.Body).ReadString('\n')
	cancel()
	_ = resp.Body.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("upstream not canceled")
	}
	r := receive(t, records)
	if !r.Incomplete || r.Response.Body.Complete {
		t.Fatal("canceled stream presented as complete")
	}
}

func TestNoReplayAfterUpstreamDisconnect(t *testing.T) {
	var count atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	base := register(t, p, upstream.URL)
	resp, err := http.Get(base + "/responses")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if count.Load() != 1 || resp.StatusCode != 502 {
		t.Fatalf("request replayed or error hidden: count=%d status=%d", count.Load(), resp.StatusCode)
	}
	r := receive(t, records)
	if !r.Incomplete || r.Error == "" || r.Response.Status != 0 {
		t.Fatalf("transport failure metadata incorrect: %+v", r)
	}
}

func TestBoundsCaptureWithoutTruncatingForwardedBody(t *testing.T) {
	body := bytes.Repeat([]byte("abc"), 100)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(w, r.Body) }))
	defer upstream.Close()
	p, records := testProxy(t, Config{MaxBodyBytes: 32, QueueBytes: 48})
	base := register(t, p, upstream.URL)
	resp, err := http.Post(base+"/responses", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(got, body) {
		t.Fatal("capture bound truncated application data")
	}
	r := receive(t, records)
	if !r.Incomplete || !r.Request.Body.Truncated || !r.Response.Body.Truncated || recordBytes(r) > 48 || r.Request.Body.Size != 300 || r.Response.Body.Size != 300 {
		t.Fatalf("bad truncation metadata: %+v", r)
	}
}

func TestSlowRecorderNeverBlocksForwardingAndReportsLoss(t *testing.T) {
	blocked := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(blocked) }) }
	var recordsMu sync.Mutex
	var records []Record
	p, _ := testProxy(t, Config{QueueSize: 1, Record: func(r Record) error {
		once.Do(func() { close(entered); <-blocked })
		recordsMu.Lock()
		records = append(records, r)
		recordsMu.Unlock()
		return nil
	}})
	defer release()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "okay") }))
	defer upstream.Close()
	base := register(t, p, upstream.URL)
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.Get(base + "/responses")
		if err != nil {
			t.Fatal("recorder stalled forwarding:", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if i == 0 {
			<-entered
		}
	}
	if p.Stats().DroppedRecords == 0 {
		t.Fatal("queue overflow not reported")
	}
	release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	recordsMu.Lock()
	defer recordsMu.Unlock()
	var observedLoss bool
	for _, r := range records {
		observedLoss = observedLoss || (r.Association == "observation-loss" && r.Incomplete && r.Labels.SessionID == "session-1")
	}
	if !observedLoss {
		t.Fatal("queue overflow has no scoped persistent loss marker")
	}
}

func TestQueueLossRecord(t *testing.T) {
	p, records := testProxy(t, Config{})
	p.lossMu.Lock()
	p.losses[Labels{SessionID: "session-lost"}] = lossInfo{QueueCount: 2}
	p.lossMu.Unlock()
	p.writeLosses()
	r := receive(t, records)
	if r.Association != "observation-loss" || !r.Incomplete || r.CaptureLoss == "" || r.Labels.SessionID != "session-lost" {
		t.Fatalf("loss evidence missing: %+v", r)
	}
}

func TestRecorderErrorIsVisibleWithoutChangingResponse(t *testing.T) {
	p, _ := testProxy(t, Config{Record: func(Record) error { return errors.New("disk unavailable") }})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "okay") }))
	defer upstream.Close()
	base := register(t, p, upstream.URL)
	resp, err := http.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if string(body) != "okay" || p.Stats().RecordErrors < 1 || p.Stats().DroppedRecords != 1 || p.Stats().LastError != "disk unavailable" {
		t.Fatalf("record failure not isolated: body=%q stats=%+v", body, p.Stats())
	}
}

func TestRetainedBodyRemainsPrefixAfterBudgetRecovers(t *testing.T) {
	p, _ := testProxy(t, Config{MaxBodyBytes: 32, QueueBytes: 8})
	held := p.newCapture(time.Now(), -1)
	held.add([]byte("123456"))
	actual := p.newCapture(time.Now(), -1)
	actual.add([]byte("abcdef")) // Only "ab" fits; four bytes are now missing.
	p.release(int64(len(held.snapshot().Bytes)))
	actual.add([]byte("ghij")) // Budget recovered, but filling the gap is impossible.
	got := actual.snapshot()
	defer p.release(int64(len(got.Bytes)))
	if string(got.Bytes) != "ab" || got.Size != 10 || !got.Truncated {
		t.Fatalf("capture spliced discontiguous bytes: %+v", got)
	}
}

func TestBodyCompletionDistinguishesLateReadErrorFromMissingBytes(t *testing.T) {
	p, _ := testProxy(t, Config{})
	complete := p.newCapture(time.Now(), 3)
	complete.add([]byte("abc"))
	complete.ended(http.ErrBodyReadAfterClose)
	got := complete.snapshot()
	defer p.release(int64(len(got.Bytes)))
	if !got.Complete || got.Error != "" {
		t.Fatalf("late probe error hid complete bytes: %+v", got)
	}
	short := p.newCapture(time.Now(), 4)
	short.add([]byte("abc"))
	short.ended(io.EOF)
	got = short.snapshot()
	defer p.release(int64(len(got.Bytes)))
	if got.Complete || got.Error == "" {
		t.Fatalf("premature EOF reported complete: %+v", got)
	}
}

func TestRecorderRecoveryPersistsScopedLossWithoutReplayingRequest(t *testing.T) {
	var writes, calls atomic.Int32
	records := make(chan Record, 1)
	p, _ := testProxy(t, Config{Record: func(r Record) error {
		if writes.Add(1) <= 2 {
			return errors.New("disk temporarily unavailable")
		}
		records <- r
		return nil
	}})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "okay")
	}))
	defer upstream.Close()
	base := register(t, p, upstream.URL)
	resp, err := http.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	r := receive(t, records)
	if r.Association != "observation-loss" || r.Labels.SessionID != "session-1" || !strings.Contains(r.CaptureLoss, "recorder failure=1") {
		t.Fatalf("recovered loss marker wrong: %+v", r)
	}
	if calls.Load() != 1 || p.Stats().DroppedRecords != 1 || p.Stats().RecordErrors != 2 {
		t.Fatalf("failure replayed request or inflated loss count: calls=%d stats=%+v", calls.Load(), p.Stats())
	}
}

func TestCapabilityRouteAndRevocation(t *testing.T) {
	p, _ := testProxy(t, Config{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("revoked route reached upstream") }))
	defer upstream.Close()
	base, revoke, err := p.Register(upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	revoke()
	revoke()
	for _, path := range []string{base, p.base + "/http://example.com", p.base + routePrefix + "unknown/responses"} {
		resp, err := http.Get(path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("unregistered destination accepted: %d", resp.StatusCode)
		}
	}
	for _, invalid := range []string{"file:///etc/passwd", "https://u:p@example.com", "https://example.com?key=secret", "https://example.com#fragment"} {
		if _, _, err := p.Register(invalid, nil); err == nil {
			t.Fatalf("unsafe upstream accepted: %s", invalid)
		}
	}
}

func TestWebSocketFrameBytesAndConnectionAssociation(t *testing.T) {
	clientFrame := []byte{0x81, 0x84, 1, 2, 3, 4, 'p' ^ 1, 'i' ^ 2, 'n' ^ 3, 'g' ^ 4}
	serverFrame := []byte{0x81, 4, 'p', 'o', 'n', 'g'}
	upstreamReceived := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		_, _ = fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
		_ = buf.Flush()
		got := make([]byte, len(clientFrame))
		_, _ = io.ReadFull(buf, got)
		upstreamReceived <- got
		_, _ = conn.Write(serverFrame)
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	base := register(t, p, upstream.URL+"/v1")
	u, _ := url.Parse(base + "/responses")
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nAuthorization: Bearer ws-secret\r\n\r\n", u.Path, u.Host)
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("upgrade failed: %+v %v", resp, err)
	}
	_, _ = conn.Write(clientFrame)
	got := make([]byte, len(serverFrame))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, serverFrame) || !bytes.Equal(<-upstreamReceived, clientFrame) {
		t.Fatal("WebSocket frames changed")
	}
	_ = conn.Close()
	r := receive(t, records)
	if r.WebSocket == nil || !bytes.Equal(r.WebSocket.ToUpstream.Bytes, clientFrame) || !bytes.Equal(r.WebSocket.ToClient.Bytes, serverFrame) {
		t.Fatalf("WebSocket frame capture differs: %+v", r.WebSocket)
	}
	if r.Association != "websocket-connection" || r.Labels.RunID != "" || r.Labels.TurnID != "" || r.Labels.ThreadID != "" || r.ConnectionStartLabels.RunID != "run-1" {
		t.Fatalf("connection falsely attributed to a run: %+v", r)
	}
}

func TestWebSocketHalfClosePreservesFinalUpstreamBytes(t *testing.T) {
	clientFrame := websocketFrame(1, true, true, []byte("finished sending"))
	serverFrame := websocketFrame(1, true, false, []byte("last server response"))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = buf.Flush()
		got, _ := io.ReadAll(buf)
		if !bytes.Equal(got, clientFrame) {
			t.Error("upstream did not receive all client bytes before half-close")
		}
		_, _ = conn.Write(serverFrame)
	}))
	defer upstream.Close()
	p, records := testProxy(t, Config{})
	u, _ := url.Parse(register(t, p, upstream.URL))
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n", u.Path, u.Host)
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("upgrade failed: %+v %v", resp, err)
	}
	_, _ = conn.Write(clientFrame)
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, serverFrame) {
		t.Fatalf("half-close lost final server bytes: %q %v", got, err)
	}
	r := receive(t, records)
	if r.WebSocket == nil || !bytes.Equal(r.WebSocket.ToClient.Bytes, serverFrame) || !bytes.Equal(r.WebSocket.ToUpstream.Bytes, clientFrame) {
		t.Fatal("half-close capture lost bytes")
	}
}
