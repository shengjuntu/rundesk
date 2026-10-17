package observeproxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const routePrefix = "/_rundesk_observe/"

type route struct {
	upstream *url.URL
	labels   func() Labels
}

type exchange struct {
	record            Record
	started           time.Time
	request, response *capture
	duplex            *duplexBody
}

type exchangeKey struct{}

type lossInfo struct {
	QueueCount  uint64
	RecordCount uint64
	LastError   string
}

type Proxy struct {
	server    *http.Server
	listener  net.Listener
	transport *http.Transport
	base      string
	maxBody   int64
	maxBytes  int64
	record    func(Record) error
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
	routes    map[string]*route
	closing   bool
	budgetMu  sync.Mutex
	retained  int64
	queue     chan Record
	lossMu    sync.Mutex
	losses    map[Labels]lossInfo
	lastError string
	stop      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	handlers  sync.WaitGroup
	active    atomic.Int64
	ids       atomic.Uint64
	pending   atomic.Int64
	dropped   atomic.Uint64
	errors    atomic.Uint64
	duplexMu  sync.Mutex
	duplex    map[*duplexBody]bool
}

// New starts a dedicated listener. Upstream requests use a fresh HTTP/1.1
// connection so net/http cannot silently replay an idempotent request on a stale
// reused connection. Neither transport compression nor proxy retries are enabled.
func New(config Config) (*Proxy, error) {
	if config.Record == nil {
		return nil, errors.New("observation recorder is required")
	}
	if config.ListenAddr == "" {
		config.ListenAddr = "127.0.0.1:0"
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 16 << 20
	}
	if config.QueueBytes <= 0 {
		config.QueueBytes = 64 << 20
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 128
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = 30 * time.Second
	}
	listener, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{
		listener: listener, maxBody: config.MaxBodyBytes, maxBytes: config.QueueBytes,
		record: config.Record, ctx: ctx, cancel: cancel, routes: map[string]*route{},
		queue: make(chan Record, config.QueueSize), losses: map[Labels]lossInfo{},
		stop: make(chan struct{}), closed: make(chan struct{}), duplex: map[*duplexBody]bool{},
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	p.base = "http://" + net.JoinHostPort(host, port)
	p.transport = &http.Transport{
		// Preserve any configured corporate outbound proxy, without making this
		// observation listener an arbitrary destination proxy.
		Proxy:              http.ProxyFromEnvironment,
		DialContext:        (&net.Dialer{Timeout: config.DialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		DisableCompression: true, DisableKeepAlives: true,
		TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	p.server = &http.Server{Handler: http.HandlerFunc(p.serveHTTP), ReadHeaderTimeout: 15 * time.Second, MaxHeaderBytes: 1 << 20}
	go p.recordLoop()
	go func() {
		if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.setError(err)
		}
	}()
	return p, nil
}

// Register fixes the upstream destination and returns an unguessable capability
// base URL. A request to baseURL + "/responses" is forwarded to upstreamBase +
// "/responses". The original base path remains visible in the returned URL
// because some clients use its suffix to select provider features. Upstream
// path/query bytes are not cleaned or re-encoded.
func (p *Proxy) Register(upstreamBase string, labels func() Labels) (string, func(), error) {
	u, err := url.Parse(upstreamBase)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", nil, errors.New("upstream must be an HTTP(S) base URL without credentials, query, or fragment")
	}
	token, err := randomID()
	if err != nil {
		return "", nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return "", nil, errors.New("observation proxy is closed")
	}
	p.routes[token] = &route{upstream: u, labels: labels}
	var once sync.Once
	revoke := func() { once.Do(func() { p.mu.Lock(); delete(p.routes, token); p.mu.Unlock() }) }
	return p.base + routePrefix + token + u.EscapedPath(), revoke, nil
}

func (p *Proxy) serveHTTP(w http.ResponseWriter, req *http.Request) {
	// Register handlers under the same lock that marks shutdown, so Wait can
	// never race a handler's first Add after the server listener is closed.
	p.mu.RLock()
	if p.closing {
		p.mu.RUnlock()
		http.NotFound(w, req)
		return
	}
	p.handlers.Add(1)
	p.mu.RUnlock()
	defer p.handlers.Done()
	escaped := req.URL.EscapedPath()
	if !strings.HasPrefix(escaped, routePrefix) {
		http.NotFound(w, req)
		return
	}
	token, suffix, _ := strings.Cut(strings.TrimPrefix(escaped, routePrefix), "/")
	p.mu.RLock()
	r := p.routes[token]
	closing := p.closing
	p.mu.RUnlock()
	if r == nil || closing || req.Method == http.MethodConnect {
		http.NotFound(w, req)
		return
	}
	p.active.Add(1)
	defer p.active.Add(-1)
	started := time.Now()
	id, err := randomID()
	if err != nil {
		// Capture identity failure must not turn an otherwise valid upstream
		// request into a failed application request.
		id = fmt.Sprintf("capture-%d-%d", started.UnixNano(), p.ids.Add(1))
		p.setError(err)
	}
	labels := Labels{}
	if r.labels != nil {
		labels = r.labels()
	}
	x := &exchange{record: Record{ID: id, Labels: labels, Association: "request-start", Started: started.UTC().Format(time.RFC3339Nano)}, started: started}
	x.request = p.newCapture(started, req.ContentLength)
	x.request.trailerSource = req.Trailer
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &readBody{ReadCloser: req.Body, capture: x.request}
	}
	ctx, cancel := context.WithCancel(req.Context())
	stopCancel := context.AfterFunc(p.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	req = req.WithContext(context.WithValue(ctx, exchangeKey{}, x))
	defer func() {
		// ReverseProxy uses ErrAbortHandler for a broken stream. Record evidence
		// before allowing net/http to close the downstream connection.
		panicValue := recover()
		if panicValue != nil {
			x.record.Error = "forwarding aborted"
		}
		if ctx.Err() != nil && x.record.Error == "" {
			x.record.Error = ctx.Err().Error()
		}
		p.finish(x)
		if panicValue != nil {
			panic(panicValue)
		}
	}()
	if strings.Contains(strings.TrimPrefix(escaped, routePrefix), "/") {
		suffix = "/" + suffix
	}
	decodedSuffix, err := url.PathUnescape(suffix)
	if err != nil {
		x.record.Error = "invalid URL escape"
		http.Error(w, "invalid URL escape", http.StatusBadRequest)
		return
	}
	reverse := &httputil.ReverseProxy{
		Transport:     p.transport,
		FlushInterval: -1,
		ErrorLog:      log.New(io.Discard, "", 0),
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = r.upstream.Scheme
			pr.Out.URL.Host = r.upstream.Host
			pr.Out.URL.Path = decodedSuffix
			pr.Out.URL.RawPath = suffix
			// Rewrite mode sanitizes unparsable query strings by default. Restore
			// the original bytes: this proxy is not a query-parameter interpreter.
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.URL.ForceQuery = pr.In.URL.ForceQuery
			pr.Out.Host = r.upstream.Host
			// ReverseProxy strips forwarding headers before Rewrite. Restore only
			// those the caller actually supplied; never add another proxy identity.
			for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if values, ok := pr.In.Header[name]; ok {
					pr.Out.Header[name] = append([]string(nil), values...)
				}
			}
			x.record.Request = Message{Method: pr.Out.Method, URL: pr.Out.URL.String(), Host: pr.Out.Host}
			x.record.Request.Headers, x.record.Request.RedactedHeaders = redactHeaders(pr.Out.Header)
		},
		ModifyResponse: func(resp *http.Response) error {
			x.record.Response.Status = resp.StatusCode
			x.record.Response.Headers, x.record.Response.RedactedHeaders = redactHeaders(resp.Header)
			if resp.StatusCode == http.StatusSwitchingProtocols {
				rw, ok := resp.Body.(io.ReadWriteCloser)
				if !ok {
					return errors.New("upgraded response is not duplex")
				}
				x.record.Association = "websocket-connection"
				x.record.ConnectionStartLabels = &labels
				x.record.Labels = Labels{SessionID: labels.SessionID}
				x.duplex = &duplexBody{ReadWriteCloser: rw, upstream: p.newCapture(started, -1), client: p.newCapture(started, -1), proxy: p}
				p.duplexMu.Lock()
				p.duplex[x.duplex] = true
				p.duplexMu.Unlock()
				resp.Body = x.duplex
				x.response = p.newCapture(started, 0)
			} else {
				x.response = p.newCapture(started, resp.ContentLength)
				x.response.trailerSource = resp.Trailer
				resp.Body = &readBody{ReadCloser: resp.Body, capture: x.response}
			}
			return nil
		},
		ErrorHandler: func(out http.ResponseWriter, _ *http.Request, err error) {
			x.record.Error = err.Error()
			// This is a transport failure, not a fabricated upstream response.
			http.Error(out, "upstream transport failed", http.StatusBadGateway)
		},
	}
	reverse.ServeHTTP(w, req)
}

func randomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (p *Proxy) finish(x *exchange) {
	if x.duplex != nil {
		// ReverseProxy schedules its upgraded-body Close asynchronously after
		// ServeHTTP returns. Wait for in-flight reads/writes before freezing the
		// snapshot, so final concurrently forwarded bytes cannot disappear.
		_ = x.duplex.Close()
	}
	r := &x.record
	r.Ended = time.Now().UTC().Format(time.RFC3339Nano)
	r.DurationMS = time.Since(x.started).Milliseconds()
	r.Request.Body = x.request.snapshot()
	r.Request.Trailers = x.request.capturedTrailers()
	if x.response != nil {
		r.Response.Body = x.response.snapshot()
		r.Response.Trailers = x.response.capturedTrailers()
	}
	r.Incomplete = r.Error != "" || bodyIncomplete(r.Request.Body) || bodyIncomplete(r.Response.Body)
	if x.duplex != nil {
		// A transport EOF alone does not establish a complete WebSocket message
		// sequence or a protocol close handshake. Preserve that uncertainty.
		r.WebSocket = &Duplex{Encoding: "websocket-frames", ToUpstream: x.duplex.upstream.snapshot(), ToClient: x.duplex.client.snapshot()}
		r.Incomplete = r.Incomplete || bodyIncomplete(r.WebSocket.ToUpstream) || bodyIncomplete(r.WebSocket.ToClient)
	}
	p.enqueue(*r)
}

func (p *Proxy) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closing = true
		p.routes = map[string]*route{}
		p.mu.Unlock()
		go func() {
			p.cancel()
			_ = p.server.Close()
			p.duplexMu.Lock()
			var sockets []*duplexBody
			for b := range p.duplex {
				sockets = append(sockets, b)
			}
			p.duplexMu.Unlock()
			for _, b := range sockets {
				_ = b.Close()
			}
			p.handlers.Wait()
			p.transport.CloseIdleConnections()
			close(p.stop)
		}()
	})
	select {
	case <-p.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Proxy) Stats() Stats {
	p.budgetMu.Lock()
	retained := p.retained
	p.budgetMu.Unlock()
	p.lossMu.Lock()
	lastError := p.lastError
	p.lossMu.Unlock()
	return Stats{ActiveRequests: p.active.Load(), PendingRecords: p.pending.Load(), RetainedBytes: retained, DroppedRecords: p.dropped.Load(), RecordErrors: p.errors.Load(), LastError: lastError}
}

func (p *Proxy) reserve(n int) int {
	p.budgetMu.Lock()
	defer p.budgetMu.Unlock()
	if int64(n) > p.maxBytes-p.retained {
		n = int(p.maxBytes - p.retained)
	}
	p.retained += int64(n)
	return n
}

func (p *Proxy) release(n int64) {
	p.budgetMu.Lock()
	p.retained -= n
	p.budgetMu.Unlock()
}

func (p *Proxy) setError(err error) {
	p.lossMu.Lock()
	p.lastError = err.Error()
	p.lossMu.Unlock()
}

func (p *Proxy) enqueue(r Record) {
	p.pending.Add(1)
	select {
	case p.queue <- r:
	default:
		p.pending.Add(-1)
		p.release(recordBytes(r))
		p.dropped.Add(1)
		p.addLoss(r.Labels, lossInfo{QueueCount: 1})
	}
}

func (p *Proxy) addLoss(labels Labels, loss lossInfo) {
	p.lossMu.Lock()
	defer p.lossMu.Unlock()
	if _, exists := p.losses[labels]; !exists && len(p.losses) >= 1024 {
		// A bounded global bucket still reports overflow across >1024 sessions.
		labels = Labels{}
	}
	old := p.losses[labels]
	old.QueueCount += loss.QueueCount
	old.RecordCount += loss.RecordCount
	if loss.LastError != "" {
		old.LastError = loss.LastError
	}
	p.losses[labels] = old
}

func (p *Proxy) writeRecord(r Record) error {
	err := func() (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("observation recorder panicked: %v", recovered)
			}
		}()
		return p.record(r)
	}()
	if err != nil {
		p.errors.Add(1)
		p.setError(err)
	}
	return err
}

func (p *Proxy) writeLosses() {
	p.lossMu.Lock()
	losses := p.losses
	p.losses = map[Labels]lossInfo{}
	p.lossMu.Unlock()
	for labels, loss := range losses {
		id, _ := randomID()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		detail := fmt.Sprintf("observation records lost: queue overflow=%d, recorder failure=%d", loss.QueueCount, loss.RecordCount)
		if loss.LastError != "" {
			detail += "; " + loss.LastError
		}
		if err := p.writeRecord(Record{ID: id, Labels: labels, Association: "observation-loss", Started: now, Ended: now, Incomplete: true, CaptureLoss: detail}); err != nil {
			// Retry only the compact loss marker, not the lost record and never
			// the upstream HTTP request. Do not count this marker as another loss.
			p.addLoss(labels, loss)
		}
	}
}

func (p *Proxy) recordLoop() {
	defer close(p.closed)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	consume := func(r Record) {
		if err := p.writeRecord(r); err != nil {
			p.dropped.Add(1)
			p.addLoss(r.Labels, lossInfo{RecordCount: 1, LastError: err.Error()})
		}
		p.release(recordBytes(r))
		p.pending.Add(-1)
	}
	for {
		select {
		case r := <-p.queue:
			consume(r)
		case <-tick.C:
			p.writeLosses()
		case <-p.stop:
			for {
				select {
				case r := <-p.queue:
					consume(r)
				default:
					p.writeLosses()
					return
				}
			}
		}
	}
}
