package app

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBuildLogRedactionAcrossEveryChunkBoundary(t *testing.T) {
	lines := []string{
		"Authorization: Bearer chunk-boundary-private-value\n",
		"authorization = Basic cHJpdmF0ZTpzZWNyZXQ=\n",
		"{\"API KEY\": \"chunk-boundary-private-value\"}\n",
		"access_token='chunk-boundary-private-value'\n",
		"fetch https://private-user:private-pass@example.invalid/resource\n",
		"fetch https://private-token@example.invalid/resource\n",
		"npm_abcdefghijklmnopqrstuvwxyz0123456789\n",
		"rd-build-" + strings.Repeat("a", 64) + "\n",
		"rd-setup-" + strings.Repeat("b", 64) + "\n",
		"secret=chunk-boundary-private-value", // No trailing newline.
	}
	for _, line := range lines {
		for split := 0; split <= len(line); split++ {
			var result bytes.Buffer
			w := newBuildLogRedactor(&result)
			if _, err := w.Write([]byte(line[:split])); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(line[split:])); err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			got := result.String()
			for _, secret := range []string{"chunk-boundary-private-value", "cHJpdmF0ZTpzZWNyZXQ=", "private-user", "private-pass", "private-token", "npm_abcdefghijklmnopqrstuvwxyz0123456789"} {
				if strings.Contains(got, secret) {
					t.Fatalf("split %d leaked %q: %q", split, secret, got)
				}
			}
			if strings.Contains(got, "rd-build-") || strings.Contains(got, "rd-setup-") {
				t.Fatalf("split %d leaked task capability: %q", split, got)
			}
			if !strings.Contains(got, "[redacted]") {
				t.Fatalf("split %d was not masked: %q", split, got)
			}
		}
	}
}

func TestBuildLogRedactionBoundsOversizedLineAndContinues(t *testing.T) {
	var result bytes.Buffer
	w := newBuildLogRedactor(&result)
	for range 100 {
		if _, err := w.Write(bytes.Repeat([]byte("x"), 4096)); err != nil {
			t.Fatal(err)
		}
		if len(w.pending) > buildLogRedactionLineLimit {
			t.Fatal("unbounded line buffer")
		}
	}
	if _, err := w.Write([]byte("Authorization: Bearer final-private-value\nordinary build output\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if !w.Truncated() {
		t.Fatal("omitted line was not reported as truncated")
	}
	if got := result.String(); strings.Contains(got, "final-private-value") || !strings.Contains(got, "ordinary build output\n") || strings.Count(got, "line omitted") != 1 {
		t.Fatal(got)
	}
}

type buildLogShortWriter struct{}

func (buildLogShortWriter) Write(p []byte) (int, error) { return 0, nil }

func TestBuildLogRedactorPreservesOrdinaryOutputAndWriteErrors(t *testing.T) {
	var result bytes.Buffer
	w := newBuildLogRedactor(&result)
	for _, chunk := range []string{"Step 1/2\nStep", " 2/2\nDone"} {
		if n, err := w.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatal(n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if result.String() != "Step 1/2\nStep 2/2\nDone" {
		t.Fatal(result.String())
	}
	w = newBuildLogRedactor(buildLogShortWriter{})
	if _, err := w.Write([]byte("done\n")); err != io.ErrShortWrite {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("another\n")); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}
