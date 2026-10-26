package app

import (
	"bytes"
	"io"
	"regexp"
	"sync"
)

// Buffer one bounded line so a credential split across stdout/stderr writes is
// never persisted before the complete value can be masked. Oversized lines are
// suppressed in full rather than emitting an unreviewable partial credential.
const buildLogRedactionLineLimit = 64 << 10

var buildLogHeaderSecret = regexp.MustCompile(`(?i)((?:authorization|proxy-authorization|cookie|set-cookie)["']?[ \t]*[:=][ \t]*)[^\r\n]*`)
var buildLogAssignedSecret = regexp.MustCompile(`(?i)((?:api[ _-]*key|access[ _-]*token|refresh[ _-]*token|client[ _-]*secret|token|password|passwd|secret)["']?[ \t]*[:=][ \t]*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s&,;]+)`)
var buildLogLiteralSecret = regexp.MustCompile(`(?:sk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{16,}|github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|npm_[A-Za-z0-9]{20,}|rd_(?:app|user|browser)_[A-Za-z0-9._-]{16,}|rd-build-[a-f0-9]{64})`)
var buildLogURLCredentials = regexp.MustCompile(`(?i)(https?://)[^\s/@]+(?::[^\s/@]*)?@`)

func redactBuildLogLine(line []byte) []byte {
	line = diagnosticBearer.ReplaceAll(line, []byte("${1}[redacted]"))
	line = buildLogHeaderSecret.ReplaceAll(line, []byte("${1}[redacted]"))
	line = buildLogAssignedSecret.ReplaceAll(line, []byte("${1}[redacted]"))
	line = buildLogURLCredentials.ReplaceAll(line, []byte("${1}[redacted]@"))
	return buildLogLiteralSecret.ReplaceAll(line, []byte("[redacted]"))
}

type buildLogRedactor struct {
	mu        sync.Mutex
	dst       io.Writer
	pending   []byte
	dropping  bool
	truncated bool
	err       error
}

func newBuildLogRedactor(dst io.Writer) *buildLogRedactor {
	return &buildLogRedactor{dst: dst}
}

func (w *buildLogRedactor) emit(line []byte) {
	if w.err != nil {
		return
	}
	data := redactBuildLogLine(line)
	n, err := w.dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
}

func (w *buildLogRedactor) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	n := len(p)
	for len(p) != 0 {
		end := bytes.IndexByte(p, '\n')
		complete := end >= 0
		if complete {
			end++
		} else {
			end = len(p)
		}
		part := p[:end]
		p = p[end:]
		if !w.dropping {
			if len(w.pending)+len(part) > buildLogRedactionLineLimit {
				w.pending = nil
				w.dropping = true
				w.truncated = true
				w.emit([]byte("[build log line omitted: exceeds redaction limit]\n"))
			} else {
				w.pending = append(w.pending, part...)
			}
		}
		if complete {
			if !w.dropping {
				w.emit(w.pending)
			}
			w.pending = w.pending[:0]
			w.dropping = false
		}
	}
	return n, w.err
}

// Flush must be called once the producer has stopped and before reading its
// final byte/truncation counters. It publishes an unterminated final line.
func (w *buildLogRedactor) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) != 0 {
		w.emit(w.pending)
		w.pending = w.pending[:0]
	}
	return w.err
}

func (w *buildLogRedactor) Truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}
