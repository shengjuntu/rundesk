package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func downloadTestBuild(t *testing.T, m *Manager, item BuildDownloadSpec) ImageBuild {
	t.Helper()
	encoded, _ := json.Marshal(BuildDownloadManifest{SchemaVersion: 1, Items: []BuildDownloadSpec{item}})
	data := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\nCOPY vendor/ /archive/\n", "build-downloads.json": string(encoded), "vendor/.keep": ""})
	id := fmt.Sprintf("download-%d", time.Now().UnixNano())
	dir := m.buildDir(id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "context.zip"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, size, err := extractBuildContext(filepath.Join(dir, "context.zip"), filepath.Join(dir, "context"), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	return ImageBuild{ID: id, ContextSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), ContextBytes: size}
}

func TestBuildDownloadCacheRehashAndFrozenEvidence(t *testing.T) {
	m := quietRecipeManager(t)
	payload := []byte("reviewed archive bytes")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write(payload) }))
	defer server.Close()
	item := BuildDownloadSpec{ID: "source", URL: server.URL + "/archive", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Destination: "vendor/source.tar.gz"}
	build := downloadTestBuild(t, m, item)
	var log bytes.Buffer
	if err := m.prepareBuildDownloads(context.Background(), build, filepath.Join(m.buildDir(build.ID), "context"), &log); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	stored, err := os.ReadFile(filepath.Join(m.buildDir(build.ID), "context", item.Destination))
	if err != nil || !bytes.Equal(stored, payload) {
		t.Fatal(string(stored), err)
	}
	evidence, _ := os.ReadFile(filepath.Join(m.buildDir(build.ID), "downloads.json"))
	if !bytes.Contains(evidence, []byte(`"cacheHit": false`)) || !bytes.Contains(evidence, []byte(item.SHA256)) {
		t.Fatal(string(evidence))
	}
	// A separate build reuses bytes by digest rather than a mutable URL or tag.
	second := downloadTestBuild(t, m, item)
	if err := m.prepareBuildDownloads(context.Background(), second, filepath.Join(m.buildDir(second.ID), "context"), &log); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("cache was not reused", calls.Load())
	}
	evidence, _ = os.ReadFile(filepath.Join(m.buildDir(second.ID), "downloads.json"))
	if !bytes.Contains(evidence, []byte(`"cacheHit": true`)) {
		t.Fatal(string(evidence))
	}
	// Corrupt cached bytes are never trusted merely because the file is named SHA.
	cache := filepath.Join(m.Data, "build-download-cache", "sha256", item.SHA256+".archive")
	if err := os.WriteFile(cache, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	third := downloadTestBuild(t, m, item)
	if err := m.prepareBuildDownloads(context.Background(), third, filepath.Join(m.buildDir(third.ID), "context"), &log); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("corruption not redownloaded", calls.Load())
	}
	// Changing frozen recipe bytes is detected before another request.
	if err := os.WriteFile(filepath.Join(m.buildDir(third.ID), "context.zip"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.prepareBuildDownloads(context.Background(), third, filepath.Join(m.buildDir(third.ID), "context"), &log); err == nil {
		t.Fatal("changed frozen context accepted")
	}
}

func TestBuildDownloadHashFailureLeavesNoCacheOrInjectedFile(t *testing.T) {
	m := quietRecipeManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "wrong") }))
	defer server.Close()
	item := BuildDownloadSpec{ID: "source", URL: server.URL, SHA256: strings.Repeat("a", 64), Destination: "vendor/source.tar.gz"}
	build := downloadTestBuild(t, m, item)
	err := m.prepareBuildDownloads(context.Background(), build, filepath.Join(m.buildDir(build.ID), "context"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(m.buildDir(build.ID), "context", item.Destination), filepath.Join(m.Data, "build-download-cache", "sha256", item.SHA256+".archive")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unverified bytes persisted", path, err)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(m.Data, "build-download-cache", "sha256", ".archive-*"))
	if len(leftovers) != 0 {
		t.Fatal(leftovers)
	}
}

func TestBuildDownloadCancellationAndSizeLimit(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	result := make(chan error, 1)
	go func() {
		_, err := downloadBuildArchive(ctx, client, server.URL, strings.Repeat("a", 64), filepath.Join(t.TempDir(), "cache"), io.Discard)
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled download succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("download ignored cancellation")
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(buildDownloadMaxBytes+1))
	}))
	defer large.Close()
	if _, err := downloadBuildArchive(context.Background(), client, large.URL, strings.Repeat("a", 64), filepath.Join(t.TempDir(), "cache"), io.Discard); err == nil || !strings.Contains(err.Error(), "128 MiB") {
		t.Fatal(err)
	}
}

func TestBuildDownloadManifestAndCompleteURL(t *testing.T) {
	item := BuildDownloadSpec{ID: "source", URL: "https://codeload.github.com/owner/repo/tar.gz/commit", SHA256: strings.Repeat("a", 64), Destination: "vendor/source.tar.gz", ArchiveBasePath: "/owner/repo/tar.gz/commit"}
	defs := []BuildSourceType{{ID: "github", Name: "GitHub", Fields: []BuildSourceField{{ID: "archiveUrl", Label: "archive", Argument: "RD_GITHUB_ARCHIVE_URL", Kind: "url"}, {ID: "archiveBase", Label: "base", Argument: "RD_GITHUB_ARCHIVE_BASE", Kind: "url"}}}}
	full := "https://proxy.example/https://github.com/owner/repo/archive/commit.tar.gz"
	sources := &BuildSourceSnapshot{Definitions: defs, Values: map[string]map[string]string{"github": {"archiveUrl": full, "archiveBase": "https://unused.example"}}, Arguments: map[string]string{"RD_GITHUB_ARCHIVE_URL": full, "RD_GITHUB_ARCHIVE_BASE": "https://unused.example"}}
	address, err := buildDownloadURL(item, sources)
	if err != nil || address != full {
		t.Fatal(address, err)
	}
	delete(sources.Arguments, "RD_GITHUB_ARCHIVE_URL")
	delete(sources.Values["github"], "archiveUrl")
	address, err = buildDownloadURL(item, sources)
	if err != nil || address != "https://unused.example/owner/repo/tar.gz/commit" {
		t.Fatal(address, err)
	}
	for _, destination := range []string{"../escape", "vendor/../../escape", "Dockerfile", "vendor/source.tar.gz/..", "vendor\\file"} {
		bad := item
		bad.Destination = destination
		encoded, _ := json.Marshal(BuildDownloadManifest{SchemaVersion: 1, Items: []BuildDownloadSpec{bad}})
		if _, err := readBuildDownloadManifest(buildZip(t, map[string]string{"build-downloads.json": string(encoded)})); err == nil {
			t.Fatal("unsafe destination accepted", destination)
		}
	}
}

func TestBuildDownloadPreservesBundledBytesAndRejectsSymlink(t *testing.T) {
	m := quietRecipeManager(t)
	payload := []byte("already bundled")
	item := BuildDownloadSpec{ID: "source", URL: "https://unreachable.invalid/archive", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Destination: "vendor/source.tar.gz"}
	build := downloadTestBuild(t, m, item)
	path := filepath.Join(m.buildDir(build.ID), "context", item.Destination)
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.prepareBuildDownloads(context.Background(), build, filepath.Join(m.buildDir(build.ID), "context"), io.Discard); err != nil {
		t.Fatal(err)
	}
	evidence, _ := os.ReadFile(filepath.Join(m.buildDir(build.ID), "downloads.json"))
	if !bytes.Contains(evidence, []byte(`"bundled": true`)) {
		t.Fatal(string(evidence))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if err := m.prepareBuildDownloads(context.Background(), build, filepath.Join(m.buildDir(build.ID), "context"), io.Discard); err == nil {
		t.Fatal("symlink accepted")
	}
}

func archiveResumePolicy() buildDownloadPolicy {
	return buildDownloadPolicy{Idle: time.Second, RetryBase: time.Millisecond}
}
func seedArchivePartial(t *testing.T, cache, address, expected string, body []byte, total int64, etag string) {
	t.Helper()
	if err := os.WriteFile(cache+".part", body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := buildDownloadWritePartial(cache+".part", buildDownloadPartial{SchemaVersion: 1, URLSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(address))), SHA256: expected, ETag: etag, Total: total}); err != nil {
		t.Fatal(err)
	}
}
func archiveFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected file %s: %v", path, err)
	}
}

func TestBuildDownloadResumesInterruptedResponseWithAndWithoutValidator(t *testing.T) {
	payload := []byte("reviewed source archive contents")
	for _, validator := range []string{`"reviewed-v1"`, ""} {
		t.Run(validator, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("archive byte encoding not pinned")
				}
				w.Header().Set("ETag", validator)
				if calls.Add(1) == 1 {
					if r.Header.Get("Range") != "" {
						t.Error("first request unexpectedly ranged")
					}
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
					w.Write(payload[:7])
					return
				}
				if r.Header.Get("Range") != "bytes=7-" || r.Header.Get("If-Range") != validator {
					t.Error(r.Header)
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 7-%d/%d", len(payload)-1, len(payload)))
				w.WriteHeader(http.StatusPartialContent)
				w.Write(payload[7:])
			}))
			defer server.Close()
			client := buildDownloadHTTPClient()
			defer client.CloseIdleConnections()
			cache := filepath.Join(t.TempDir(), "cache")
			expected := fmt.Sprintf("%x", sha256.Sum256(payload))
			var log bytes.Buffer
			n, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, &log, archiveResumePolicy())
			if err != nil || n != int64(len(payload)) || calls.Load() != 2 {
				t.Fatal(n, err, calls.Load(), log.String())
			}
			body, err := os.ReadFile(cache)
			if err != nil || !bytes.Equal(body, payload) {
				t.Fatal(string(body), err)
			}
			archiveFileAbsent(t, cache+".part")
			archiveFileAbsent(t, cache+".part.json")
			if !strings.Contains(log.String(), "Archive resume") {
				t.Fatal(log.String())
			}
		})
	}
}

func TestBuildDownloadCancellationRetainsPartialForNextInvocation(t *testing.T) {
	payload := []byte("a verified archive restored after cancellation")
	var calls atomic.Int32
	first := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"fixed-source"`)
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			w.Write(payload[:9])
			w.(http.Flusher).Flush()
			close(first)
			<-r.Context().Done()
			return
		}
		if r.Header.Get("Range") != "bytes=9-" || r.Header.Get("If-Range") != `"fixed-source"` {
			t.Error(r.Header)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 9-%d/%d", len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[9:])
	}))
	defer server.Close()
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	cache := filepath.Join(t.TempDir(), "cache")
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := downloadBuildArchiveWithPolicy(ctx, client, server.URL, expected, cache, io.Discard, archiveResumePolicy())
		result <- err
	}()
	<-first
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, err := os.Stat(cache + ".part"); err == nil && st.Size() == 9 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("partial bytes were not written")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation stalled")
	}
	archiveFileAbsent(t, cache)
	if _, err := os.Stat(cache + ".part.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy()); err != nil {
		t.Fatal(err)
	}
	if _, valid := verifiedDownloadFile(cache, expected); !valid {
		t.Fatal("resumed archive was not verified")
	}
}

func TestBuildDownloadFullResponseRestartsInsteadOfAppending(t *testing.T) {
	payload := []byte("current frozen source payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=5-" || r.Header.Get("If-Range") != `"old"` {
			t.Error(r.Header)
		}
		w.Header().Set("ETag", `"new"`)
		w.Write(payload)
	}))
	defer server.Close()
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	cache := filepath.Join(t.TempDir(), "cache")
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	seedArchivePartial(t, cache, server.URL, expected, []byte("wrong"), int64(len(payload)), `"old"`)
	var log bytes.Buffer
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, &log, archiveResumePolicy()); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(cache)
	if !bytes.Equal(body, payload) || !strings.Contains(log.String(), "restarting safely") {
		t.Fatal(string(body), log.String())
	}
}

func TestBuildDownloadRejectsInvalidResumeResponses(t *testing.T) {
	payload := []byte("frozen archive source")
	tests := []struct {
		name, contentRange, etag string
		oversizedBody            bool
	}{
		{"offset", "bytes 6-20/21", `"one"`, false},
		{"total", "bytes 5-20/22", `"one"`, false},
		{"length", "bytes 5-19/21", `"one"`, false},
		{"validator", "bytes 5-20/21", `"two"`, false},
		{"invalid", "bytes */21", `"one"`, false},
		{"limit", fmt.Sprintf("bytes 5-%d/%d", buildDownloadMaxBytes, buildDownloadMaxBytes+1), `"one"`, false},
		{"overrun", "bytes 5-20/21", `"one"`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", tc.etag)
				w.Header().Set("Content-Range", tc.contentRange)
				if tc.oversizedBody {
					w.Header().Set("Transfer-Encoding", "chunked")
				} else {
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)-5))
				}
				w.WriteHeader(http.StatusPartialContent)
				w.Write(payload[5:])
				if tc.oversizedBody {
					w.Write([]byte("extra"))
				}
			}))
			defer server.Close()
			client := buildDownloadHTTPClient()
			defer client.CloseIdleConnections()
			cache := filepath.Join(t.TempDir(), "cache")
			expected := fmt.Sprintf("%x", sha256.Sum256(payload))
			seedArchivePartial(t, cache, server.URL, expected, payload[:5], int64(len(payload)), `"one"`)
			if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy()); err == nil {
				t.Fatal("invalid response accepted")
			}
			archiveFileAbsent(t, cache)
			archiveFileAbsent(t, cache+".part")
			archiveFileAbsent(t, cache+".part.json")
		})
	}
}

func TestBuildDownloadChangedMirrorDoesNotReusePartial(t *testing.T) {
	payload := []byte("reviewed bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" || r.Header.Get("If-Range") != "" {
			t.Error("partial crossed mirror URL", r.Header)
		}
		w.Write(payload)
	}))
	defer server.Close()
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	cache := filepath.Join(t.TempDir(), "cache")
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	seedArchivePartial(t, cache, server.URL+"/previous-mirror", expected, []byte("different"), 0, `"old"`)
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDownloadCompletePartialPromotesWithoutNetwork(t *testing.T) {
	payload := []byte("already received before interruption")
	cache := filepath.Join(t.TempDir(), "cache")
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	seedArchivePartial(t, cache, "https://unreachable.invalid/source", expected, payload, int64(len(payload)), `"done"`)
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	if n, err := downloadBuildArchiveWithPolicy(context.Background(), client, "https://unreachable.invalid/source", expected, cache, io.Discard, archiveResumePolicy()); err != nil || n != int64(len(payload)) {
		t.Fatal(n, err)
	}
}

func TestBuildDownloadHashMismatchAndUnsafePartial(t *testing.T) {
	payload := []byte("incorrect completed archive")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer server.Close()
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	cache := filepath.Join(t.TempDir(), "cache")
	expected := strings.Repeat("a", 64)
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy()); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatal(err)
	}
	archiveFileAbsent(t, cache)
	archiveFileAbsent(t, cache+".part")
	archiveFileAbsent(t, cache+".part.json")
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("preserve"), 0600)
	if err := os.Symlink(outside, cache+".part"); err != nil {
		t.Skip("symlink unavailable")
	}
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy()); err == nil {
		t.Fatal("symlink target accepted")
	}
	body, _ := os.ReadFile(outside)
	if string(body) != "preserve" {
		t.Fatal("outside partial target modified")
	}
}

func TestBuildDownloadBodyIdleDeadlineAndProgressReset(t *testing.T) {
	payload := []byte("slow bytes")
	var stalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if stalled.Load() {
			<-r.Context().Done()
			return
		}
		for _, b := range payload {
			w.Write([]byte{b})
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	policy := buildDownloadPolicy{Idle: 100 * time.Millisecond, RetryBase: time.Millisecond}
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, filepath.Join(t.TempDir(), "slow"), io.Discard, policy); err != nil {
		t.Fatal("slow but active response was canceled", err)
	}
	stalled.Store(true)
	started := time.Now()
	if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, filepath.Join(t.TempDir(), "stalled"), io.Discard, policy); err == nil || !strings.Contains(err.Error(), "stopped receiving") {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("stalled body was not bounded")
	}
}

func TestBuildDownloadProgressIncludesAmountPercentAndSpeed(t *testing.T) {
	var out bytes.Buffer
	p := buildDownloadProgress{out: &out, base: 100, total: 200, last: time.Now().Add(-11 * time.Second), started: time.Now().Add(-time.Second)}
	p.Write(make([]byte, 50))
	text := out.String()
	for _, part := range []string{"MiB", "75.0%", "KiB/s", "verification pending"} {
		if !strings.Contains(text, part) {
			t.Fatal(text)
		}
	}
}

func TestBuildDownloadLastModifiedValidatorAndChangedPartialContent(t *testing.T) {
	payload := []byte("reviewed source archive")
	modified := "Wed, 21 Oct 2015 07:28:00 GMT"
	for _, wrongPrefix := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongPrefix), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=5-" || r.Header.Get("If-Range") != modified {
					t.Error(r.Header)
				}
				w.Header().Set("Last-Modified", modified)
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 5-%d/%d", len(payload)-1, len(payload)))
				w.WriteHeader(http.StatusPartialContent)
				w.Write(payload[5:])
			}))
			defer server.Close()
			client := buildDownloadHTTPClient()
			defer client.CloseIdleConnections()
			cache := filepath.Join(t.TempDir(), "cache")
			expected := fmt.Sprintf("%x", sha256.Sum256(payload))
			prefix := payload[:5]
			if wrongPrefix {
				prefix = []byte("wrong")
			}
			seedArchivePartial(t, cache, server.URL, expected, prefix, int64(len(payload)), "")
			partial := buildDownloadPartial{SchemaVersion: 1, URLSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(server.URL))), SHA256: expected, LastModified: modified, Total: int64(len(payload))}
			if err := buildDownloadWritePartial(cache+".part", partial); err != nil {
				t.Fatal(err)
			}
			_, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy())
			if wrongPrefix {
				if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
					t.Fatal("mixed bytes were accepted", err)
				}
				archiveFileAbsent(t, cache)
				archiveFileAbsent(t, cache+".part")
				archiveFileAbsent(t, cache+".part.json")
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildDownloadUnsatisfiedRangeCannotBlessIncorrectBytes(t *testing.T) {
	payload := []byte("actual reviewed archive")
	for _, header := range []string{"5", "bytes */5", "bytes */6"} {
		t.Run(header, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Range", header)
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			}))
			defer server.Close()
			client := buildDownloadHTTPClient()
			defer client.CloseIdleConnections()
			cache := filepath.Join(t.TempDir(), "cache")
			expected := fmt.Sprintf("%x", sha256.Sum256(payload))
			seedArchivePartial(t, cache, server.URL, expected, payload[:5], 0, "")
			if _, err := downloadBuildArchiveWithPolicy(context.Background(), client, server.URL, expected, cache, io.Discard, archiveResumePolicy()); err == nil {
				t.Fatal("incorrect416promoted partial")
			}
			archiveFileAbsent(t, cache)
			archiveFileAbsent(t, cache+".part")
			archiveFileAbsent(t, cache+".part.json")
		})
	}
}
