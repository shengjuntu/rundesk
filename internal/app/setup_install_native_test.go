package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

type codexArchiveFixture struct {
	name, body string
	kind       byte
	mode       int64
}

func codexFixtureArchive(t *testing.T, entries ...codexArchiveFixture) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0600
		}
		header := &tar.Header{Name: entry.name, Mode: mode, Typeflag: kind, Size: int64(len(entry.body))}
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			header.Size = 0
			header.Linkname = "../../outside"
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func codexRegistryFixture(t *testing.T, archive []byte, integrity string, aliasVersion string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	_, platform, _, err := codexPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@openai/codex/1.2.3":
			json.NewEncoder(w).Encode(map[string]any{"name": "@openai/codex", "version": "1.2.3", "optionalDependencies": map[string]string{"@openai/codex-" + platform: "npm:@openai/codex@" + aliasVersion + "-" + platform}})
		case "/@openai/codex/1.2.3-" + platform:
			json.NewEncoder(w).Encode(map[string]any{"name": "@openai/codex", "version": "1.2.3-" + platform, "dist": map[string]string{"tarball": server.URL + "/codex.tgz", "integrity": integrity}})
		case "/codex.tgz":
			downloads.Add(1)
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &downloads
}
func codexNativeFixture(t *testing.T, observed string) ([]byte, string) {
	t.Helper()
	target, _, binary, err := codexPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	archive := codexFixtureArchive(t, codexArchiveFixture{name: "package/vendor/" + target + "/bin/" + binary, body: "#!/bin/sh\nprintf 'codex-cli " + observed + "\\n'\n", mode: 0700}, codexArchiveFixture{name: "package/vendor/" + target + "/path/rg", body: "companion tool", mode: 0700})
	sum := sha512.Sum512(archive)
	return archive, "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}
func TestCodexNativeInstallWithoutNodeAndWithIntegrity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture binary is a POSIX script")
	}
	archive, integrity := codexNativeFixture(t, "1.2.3")
	server, downloads := codexRegistryFixture(t, archive, integrity, "1.2.3")
	m := testManager(t)
	m.Demo = false
	t.Setenv("PATH", t.TempDir()) // Native installation never invokes node or npm.
	result := m.installCodexWithOptions(context.Background(), CodexInstallInput{Version: "1.2.3", Confirm: true, Method: "native", Registry: server.URL})
	if !result.OK || result.Integrity != integrity || result.PackageVersion == "1.2.3" || downloads.Load() != 1 {
		t.Fatal(result, downloads.Load())
	}
	if !strings.HasPrefix(result.Codex, filepath.Join(m.Data, "tools", "codex")) {
		t.Fatal(result.Codex)
	}
	target, _, _, _ := codexPlatform(runtime.GOOS, runtime.GOARCH)
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(result.Codex)), "path", "rg")); err != nil {
		t.Fatal("companion tool was not retained", target, err)
	}
	saved, _ := m.setupState()
	if saved.Codex != "" {
		t.Fatal("native installation activated itself")
	}
}
func TestCodexNativeIntegrityAndVersionFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture binary is a POSIX script")
	}
	for _, test := range []struct{ name, version, integrity, alias, error string }{{"integrity", "1.2.3", "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, 64)), "1.2.3", "integrity mismatch"}, {"observed-version", "9.9.9", "", "1.2.3", "differs"}, {"unpinned-alias", "1.2.3", "", "9.9.9", "not pinned"}, {"missing-integrity", "1.2.3", "sha1-nope", "1.2.3", "SHA-512"}} {
		t.Run(test.name, func(t *testing.T) {
			archive, integrity := codexNativeFixture(t, test.version)
			if test.integrity != "" {
				integrity = test.integrity
			}
			server, _ := codexRegistryFixture(t, archive, integrity, test.alias)
			m := testManager(t)
			m.Demo = false
			result := m.installCodexWithOptions(context.Background(), CodexInstallInput{Version: "1.2.3", Confirm: true, Method: "native", Registry: server.URL})
			if result.OK || result.Codex != "" || !strings.Contains(result.Error, test.error) {
				t.Fatal(result)
			}
		})
	}
}
func TestCodexArchiveRejectsTraversalLinksAndDuplicates(t *testing.T) {
	for _, entries := range [][]codexArchiveFixture{{{name: "package/../../outside", body: "bad"}}, {{name: "/package/outside", body: "bad"}}, {{name: "package/vendor/link", kind: tar.TypeSymlink}}, {{name: "package/vendor/link", kind: tar.TypeLink}}, {{name: "package/a", body: "1"}, {name: "package/a", body: "2"}}, {{name: "package/C:stream", body: "bad"}}, {{name: "other/a", body: "bad"}}} {
		archive := codexFixtureArchive(t, entries...)
		dir := t.TempDir()
		file := filepath.Join(dir, "input.tgz")
		if err := os.WriteFile(file, archive, 0600); err != nil {
			t.Fatal(err)
		}
		if err := extractCodexArchive(file, filepath.Join(dir, "output")); err == nil {
			t.Fatal("unsafe archive accepted", entries)
		}
		if _, err := os.Stat(filepath.Join(dir, "outside")); !os.IsNotExist(err) {
			t.Fatal("archive wrote outside destination", err)
		}
	}
}
func TestCodexInstallJobPersistenceAndIdempotency(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture binary is a POSIX script")
	}
	archive, integrity := codexNativeFixture(t, "1.2.3")
	server, downloads := codexRegistryFixture(t, archive, integrity, "1.2.3")
	m := testManager(t)
	m.Demo = false
	input := CodexInstallInput{Version: "1.2.3", Confirm: true, Method: "auto", Registry: server.URL, RequestID: strings.Repeat("a", 24)}
	job, err := m.StartCodexInstall(input)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err = m.CodexInstall(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" || job.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != "succeeded" || !job.Result.OK || job.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatal(job)
	}
	replay, err := m.StartCodexInstall(input)
	if err != nil || replay.ID != job.ID || downloads.Load() != 1 {
		t.Fatal(replay, err, downloads.Load())
	}
	input.Version = "1.2.4"
	if _, err = m.StartCodexInstall(input); err == nil {
		t.Fatal("conflicting requestId accepted")
	}
	jobs, err := m.CodexInstalls()
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	interrupted := CodexInstallJob{ID: store.ID(), Status: "running", Input: input, CreatedAt: store.Now()}
	if err = m.Store.Put("codex-install", interrupted.ID, interrupted); err != nil {
		t.Fatal(err)
	}
	restored, err := m.CodexInstall(interrupted.ID)
	if err != nil || restored.Status != "interrupted" || restored.Result.OK {
		t.Fatal(restored, err)
	}
}
func TestCodexInstallSourcesAndValidation(t *testing.T) {
	m := quietRecipeManager(t)
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	in, err := m.normalizeCodexInstall(CodexInstallInput{Version: "0.157.1", Confirm: true, SourceGroup: "china", CatalogRevision: catalog.Revision})
	if err != nil || !strings.Contains(in.Registry, "npmmirror.com") || in.NPMSourceID == "" {
		t.Fatal(in, err)
	}
	if _, err = m.normalizeCodexInstall(CodexInstallInput{Version: "0.157.1", Confirm: true, SourceGroup: "china", CatalogRevision: "stale"}); err == nil {
		t.Fatal("stale source catalog accepted")
	}
	for _, input := range []CodexInstallInput{{Version: "latest", Confirm: true}, {Version: "1.2.3", Confirm: false}, {Version: "1.2.3", Confirm: true, Method: "sh"}, {Version: "1.2.3", Confirm: true, Registry: "https://user:pass@example.org"}, {Version: "1.2.3", Confirm: true, RequestID: "bad"}, {Version: "1.2.3", Confirm: true, SourceGroup: "missing"}} {
		if _, err = m.normalizeCodexInstall(input); err == nil {
			t.Fatal("invalid input accepted", fmt.Sprint(input))
		}
	}
}
func TestCodexPlatformMappings(t *testing.T) {
	for _, platform := range []struct{ os, arch, target, npm, binary string }{{"linux", "amd64", "x86_64-unknown-linux-musl", "linux-x64", "codex"}, {"linux", "arm64", "aarch64-unknown-linux-musl", "linux-arm64", "codex"}, {"darwin", "arm64", "aarch64-apple-darwin", "darwin-arm64", "codex"}, {"windows", "amd64", "x86_64-pc-windows-msvc", "win32-x64", "codex.exe"}} {
		target, npm, binary, err := codexPlatform(platform.os, platform.arch)
		if err != nil || target != platform.target || npm != platform.npm || binary != platform.binary {
			t.Fatal(platform, target, npm, binary, err)
		}
	}
	if _, _, _, err := codexPlatform("linux", "mips"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

func TestCodexInstallHTTPJobRoute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture binary is a POSIX script")
	}
	archive, integrity := codexNativeFixture(t, "1.2.3")
	server, downloads := codexRegistryFixture(t, archive, integrity, "1.2.3")
	m := testManager(t)
	m.Demo = false
	handler := NewHandler(m, "", true)
	body := fmt.Sprintf(`{"version":"1.2.3","confirm":true,"method":"native","registry":%q}`, server.URL)
	request := v1Request(handler, http.MethodPost, "/setup/codex-installs", body, "native-http-install-01")
	if request.Code != http.StatusAccepted {
		t.Fatal(request.Code, request.Body.String())
	}
	var job CodexInstallJob
	if err := json.Unmarshal(request.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := v1Request(handler, http.MethodGet, "/setup/codex-installs/"+job.ID, "", "")
		if response.Code != http.StatusOK {
			t.Fatal(response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" || job.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != "succeeded" {
		t.Fatal(job)
	}
	replay := v1Request(handler, http.MethodPost, "/setup/codex-installs", body, "native-http-install-01")
	if replay.Code != http.StatusAccepted || replay.Header().Get("Idempotency-Replayed") != "true" || downloads.Load() != 1 {
		t.Fatal(replay.Code, replay.Header(), replay.Body.String(), downloads.Load())
	}
	list := v1Request(handler, http.MethodGet, "/setup/codex-installs", "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), job.ID) {
		t.Fatal(list.Code, list.Body.String())
	}
}
