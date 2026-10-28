package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
	if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dir, "context.zip"), data, 0600); err != nil { t.Fatal(err) }
	_, size, err := extractBuildContext(filepath.Join(dir, "context.zip"), filepath.Join(dir, "context"), "Dockerfile")
	if err != nil { t.Fatal(err) }
	return ImageBuild{ID: id, ContextSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), ContextBytes: size}
}

func TestBuildDownloadCacheRehashAndFrozenEvidence(t *testing.T) {
	m := quietRecipeManager(t)
	payload := []byte("reviewed archive bytes")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write(payload) }))
	defer server.Close()
	item := BuildDownloadSpec{ID: "source", URL: server.URL+"/archive", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Destination: "vendor/source.tar.gz"}
	build := downloadTestBuild(t, m, item)
	var log bytes.Buffer
	if err := m.prepareBuildDownloads(context.Background(), build, filepath.Join(m.buildDir(build.ID), "context"), &log); err != nil { t.Fatal(err) }
	if calls.Load() != 1 { t.Fatal(calls.Load()) }
	stored, err := os.ReadFile(filepath.Join(m.buildDir(build.ID), "context", item.Destination))
	if err != nil || !bytes.Equal(stored, payload) { t.Fatal(string(stored), err) }
	evidence, _ := os.ReadFile(filepath.Join(m.buildDir(build.ID), "downloads.json"))
	if !bytes.Contains(evidence, []byte(`"cacheHit": false`)) || !bytes.Contains(evidence, []byte(item.SHA256)) { t.Fatal(string(evidence)) }
	// A separate build reuses bytes by digest rather than a mutable URL or tag.
	second := downloadTestBuild(t, m, item)
	if err := m.prepareBuildDownloads(context.Background(), second, filepath.Join(m.buildDir(second.ID), "context"), &log); err != nil { t.Fatal(err) }
	if calls.Load() != 1 { t.Fatal("cache was not reused", calls.Load()) }
	evidence, _ = os.ReadFile(filepath.Join(m.buildDir(second.ID), "downloads.json"))
	if !bytes.Contains(evidence, []byte(`"cacheHit": true`)) { t.Fatal(string(evidence)) }
	// Corrupt cached bytes are never trusted merely because the file is named SHA.
	cache := filepath.Join(m.Data, "build-download-cache", "sha256", item.SHA256+".archive")
	if err := os.WriteFile(cache, []byte("corrupt"), 0600); err != nil { t.Fatal(err) }
	third := downloadTestBuild(t, m, item)
	if err := m.prepareBuildDownloads(context.Background(), third, filepath.Join(m.buildDir(third.ID), "context"), &log); err != nil { t.Fatal(err) }
	if calls.Load() != 2 { t.Fatal("corruption not redownloaded", calls.Load()) }
	// Changing frozen recipe bytes is detected before another request.
	if err := os.WriteFile(filepath.Join(m.buildDir(third.ID), "context.zip"), []byte("changed"), 0600); err != nil { t.Fatal(err) }
	if err := m.prepareBuildDownloads(context.Background(), third, filepath.Join(m.buildDir(third.ID), "context"), &log); err == nil { t.Fatal("changed frozen context accepted") }
}

func TestBuildDownloadHashFailureLeavesNoCacheOrInjectedFile(t *testing.T) {
	m := quietRecipeManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "wrong") }))
	defer server.Close()
	item := BuildDownloadSpec{ID: "source", URL: server.URL, SHA256: strings.Repeat("a", 64), Destination: "vendor/source.tar.gz"}
	build := downloadTestBuild(t, m, item)
	err := m.prepareBuildDownloads(context.Background(), build, filepath.Join(m.buildDir(build.ID), "context"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") { t.Fatal(err) }
	for _, path := range []string{filepath.Join(m.buildDir(build.ID), "context", item.Destination), filepath.Join(m.Data,"build-download-cache","sha256",item.SHA256+".archive")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) { t.Fatal("unverified bytes persisted", path, err) }
	}
	leftovers, _ := filepath.Glob(filepath.Join(m.Data,"build-download-cache","sha256",".archive-*"))
	if len(leftovers) != 0 { t.Fatal(leftovers) }
}

func TestBuildDownloadCancellationAndSizeLimit(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	result := make(chan error,1)
	go func(){ _, err := downloadBuildArchive(ctx,client,server.URL,strings.Repeat("a",64),filepath.Join(t.TempDir(),"cache"),io.Discard); result <- err }()
	<-started
	cancel()
	select { case err := <-result: if err == nil { t.Fatal("canceled download succeeded") }; case <-time.After(2*time.Second): t.Fatal("download ignored cancellation") }
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){ w.Header().Set("Content-Length",fmt.Sprint(buildDownloadMaxBytes+1)) }))
	defer large.Close()
	if _,err := downloadBuildArchive(context.Background(),client,large.URL,strings.Repeat("a",64),filepath.Join(t.TempDir(),"cache"),io.Discard); err == nil || !strings.Contains(err.Error(),"128 MiB") { t.Fatal(err) }
}

func TestBuildDownloadManifestAndCompleteURL(t *testing.T) {
	item := BuildDownloadSpec{ID:"source",URL:"https://codeload.github.com/owner/repo/tar.gz/commit",SHA256:strings.Repeat("a",64),Destination:"vendor/source.tar.gz",ArchiveBasePath:"/owner/repo/tar.gz/commit"}
	defs := []BuildSourceType{{ID:"github",Name:"GitHub",Fields:[]BuildSourceField{{ID:"archiveUrl",Label:"archive",Argument:"RD_GITHUB_ARCHIVE_URL",Kind:"url"},{ID:"archiveBase",Label:"base",Argument:"RD_GITHUB_ARCHIVE_BASE",Kind:"url"}}}}
	full := "https://proxy.example/https://github.com/owner/repo/archive/commit.tar.gz"
	sources := &BuildSourceSnapshot{Definitions:defs,Values:map[string]map[string]string{"github":{"archiveUrl":full,"archiveBase":"https://unused.example"}},Arguments:map[string]string{"RD_GITHUB_ARCHIVE_URL":full,"RD_GITHUB_ARCHIVE_BASE":"https://unused.example"}}
	address,err := buildDownloadURL(item,sources)
	if err != nil || address != full { t.Fatal(address,err) }
	delete(sources.Arguments,"RD_GITHUB_ARCHIVE_URL")
	delete(sources.Values["github"],"archiveUrl")
	address,err = buildDownloadURL(item,sources)
	if err != nil || address != "https://unused.example/owner/repo/tar.gz/commit" { t.Fatal(address,err) }
	for _,destination := range []string{"../escape","vendor/../../escape","Dockerfile","vendor/source.tar.gz/..","vendor\\file"} {
		bad := item; bad.Destination=destination
		encoded,_ := json.Marshal(BuildDownloadManifest{SchemaVersion:1,Items:[]BuildDownloadSpec{bad}})
		if _,err := readBuildDownloadManifest(buildZip(t,map[string]string{"build-downloads.json":string(encoded)})); err == nil { t.Fatal("unsafe destination accepted",destination) }
	}
}

func TestBuildDownloadPreservesBundledBytesAndRejectsSymlink(t *testing.T) {
	m := quietRecipeManager(t)
	payload:=[]byte("already bundled")
	item:=BuildDownloadSpec{ID:"source",URL:"https://unreachable.invalid/archive",SHA256:fmt.Sprintf("%x",sha256.Sum256(payload)),Destination:"vendor/source.tar.gz"}
	build:=downloadTestBuild(t,m,item)
	path:=filepath.Join(m.buildDir(build.ID),"context",item.Destination)
	if err:=os.WriteFile(path,payload,0644);err!=nil{t.Fatal(err)}
	if err:=m.prepareBuildDownloads(context.Background(),build,filepath.Join(m.buildDir(build.ID),"context"),io.Discard);err!=nil{t.Fatal(err)}
	evidence,_:=os.ReadFile(filepath.Join(m.buildDir(build.ID),"downloads.json"))
	if !bytes.Contains(evidence,[]byte(`"bundled": true`)){t.Fatal(string(evidence))}
	if err:=os.Remove(path);err!=nil{t.Fatal(err)}
	outside:=filepath.Join(t.TempDir(),"outside")
	if err:=os.WriteFile(outside,payload,0600);err!=nil{t.Fatal(err)}
	if err:=os.Symlink(outside,path);err!=nil{t.Skip("symlinks unavailable",err)}
	if err:=m.prepareBuildDownloads(context.Background(),build,filepath.Join(m.buildDir(build.ID),"context"),io.Discard);err==nil{t.Fatal("symlink accepted")}
}
