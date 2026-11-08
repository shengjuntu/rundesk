package app

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

type requestDiagnostic struct {
	Method, Path string
	Started      time.Time
}

type diagnosticWriter struct {
	http.ResponseWriter
	diagnostic *requestDiagnostic
	wrote      bool
}

func (w *diagnosticWriter) Unwrap() http.ResponseWriter           { return w.ResponseWriter }
func (w *diagnosticWriter) requestDiagnostic() *requestDiagnostic { return w.diagnostic }
func (w *diagnosticWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *diagnosticWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *diagnosticWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func requestDiagnosticOf(w http.ResponseWriter) *requestDiagnostic {
	if d, ok := w.(interface{ requestDiagnostic() *requestDiagnostic }); ok {
		return d.requestDiagnostic()
	}
	return nil
}

var diagnosticBearer = regexp.MustCompile(`(?i)(bearer\s+)[^\s"',;]+`)
var diagnosticSecret = regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret|authorization|cookie)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&,;]+)`)
var diagnosticURLAuth = regexp.MustCompile(`(?i)(https?://)[^/\s:@]+:[^@\s/]+@`)

func diagnosticText(value string) string {
	value = diagnosticBearer.ReplaceAllString(value, "$1[redacted]")
	value = diagnosticSecret.ReplaceAllString(value, "$1[redacted]")
	value = diagnosticURLAuth.ReplaceAllString(value, "$1[redacted]@")
	if len(value) > 32768 {
		value = string([]rune(value[:32768])) + "\n[diagnostic text truncated]"
	}
	return value
}

func diagnosticValue(value any) any {
	x := redact(value)
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, v := range t {
				key := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(k))
				if sensitiveField(k) || key == "token" || key == "secret" || key == "clientsecret" || key == "cookie" || key == "setcookie" {
					t[k] = "[redacted]"
				} else {
					t[k] = walk(v)
				}
			}
		case []any:
			for i, v := range t {
				t[i] = walk(v)
			}
		case string:
			return diagnosticText(t)
		}
		return v
	}
	return walk(x)
}

func errorDiagnostic(w http.ResponseWriter, status int, code string, err error) map[string]any {
	d := map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano), "origin": "rundesk"}
	if req := requestDiagnosticOf(w); req != nil {
		d["method"], d["path"], d["durationMs"] = req.Method, req.Path, time.Since(req.Started).Milliseconds()
	}
	var native *rpc.Error
	if errors.As(err, &native) {
		d["origin"], d["rpcCode"] = "codex_rpc", native.Code
		if len(native.Data) > 0 {
			d["rpcData"] = diagnosticValue(native.Data)
		}
	}
	var transport *rpc.TransportError
	if errors.As(err, &transport) {
		d["origin"], d["transportOperation"] = "codex_transport", transport.Op
	}
	causes := []string{}
	for e, n := err, 0; e != nil && n < 8; e, n = errors.Unwrap(e), n+1 {
		causes = append(causes, diagnosticText(e.Error()))
	}
	d["causes"] = causes
	if status >= 500 {
		log.Printf("api_error request_id=%q method=%q path=%q status=%d code=%q message=%q", w.Header().Get("X-Request-ID"), d["method"], d["path"], status, code, diagnosticText(err.Error()))
	}
	return d
}

// Recovery is limited to API requests that have not sent a response. A broken
// stream is aborted rather than receiving a JSON error appended to SSE/file data.
func diagnosticBoundary(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		next.ServeHTTP(w, r)
		return
	}
	path := r.URL.Path
	if w.Header().Get("RunDesk-API-Version") == "v1" {
		path = "/api/v1/" + strings.TrimPrefix(path, "/api/")
	}
	dw := &diagnosticWriter{ResponseWriter: w, diagnostic: &requestDiagnostic{Method: r.Method, Path: path, Started: time.Now()}}
	defer func() {
		if value := recover(); value != nil {
			if value == http.ErrAbortHandler || dw.wrote {
				panic(value)
			}
			log.Printf("api_panic request_id=%q method=%q path=%q detail=%q\n%s", dw.Header().Get("X-Request-ID"), r.Method, path, diagnosticText(fmt.Sprint(value)), debug.Stack())
			writeAPIError(dw, 500, failure(500, "internal_panic", "RunDesk 处理请求时发生内部异常；请用请求编号核对服务日志。"))
		}
	}()
	next.ServeHTTP(dw, r)
}
