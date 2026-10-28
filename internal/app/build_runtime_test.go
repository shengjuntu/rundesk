package app

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeBuildRuntimeFreezesInstalledArchiveAndRejectsMutation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux managed Docker runtime")
	}
	m := testManager(t)
	target, _, _, err := codexPlatform("linux", runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	payload := codexFixtureArchive(t, codexArchiveFixture{name: "package/vendor/" + target + "/bin/codex", body: "test binary", mode: 0755})
	sum := sha512.Sum512(payload)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	dir := filepath.Join(m.Data, "tools", "codex", "fixture")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "codex-package.tgz")
	if err = os.WriteFile(archive, payload, 0600); err != nil {
		t.Fatal(err)
	}
	job := CodexInstallJob{ID: "0123456789abcdef01234567", Status: "succeeded", Platform: "linux/" + runtime.GOARCH}
	job.Input.Version = "0.157.1"
	job.Result.Version = "0.157.1"
	job.Result.OK = true
	job.Result.Method = "native"
	job.Result.Integrity = integrity
	job.Result.Codex = filepath.Join(dir, "bin", "codex")
	if err = m.Store.Put("codex-install", job.ID, job); err != nil {
		t.Fatal(err)
	}
	found, path, err := m.nativeBuildRuntime("0.157.1")
	if err != nil || found == nil || path != archive {
		t.Fatal(found, path, err)
	}
	snapshot, err := m.freezeNativeRuntime(*found, path)
	if err != nil {
		t.Fatal(err)
	}
	sha := sha256.Sum256(payload)
	if snapshot.AssetSHA256 != fmt.Sprintf("%x", sha) || snapshot.AssetBytes != int64(len(payload)) {
		t.Fatal(snapshot)
	}
	if err = os.WriteFile(archive, []byte("changed origin"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = runtimeArchiveDigest(m.runtimeArchivePath(snapshot.AssetSHA256), snapshot.Integrity); err != nil {
		t.Fatal("frozen copy lost", err)
	}
	if err = os.Chmod(m.runtimeArchivePath(snapshot.AssetSHA256), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(m.runtimeArchivePath(snapshot.AssetSHA256), []byte("tampered frozen copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = runtimeArchiveDigest(m.runtimeArchivePath(snapshot.AssetSHA256), snapshot.Integrity); err == nil {
		t.Fatal("tampered asset accepted")
	}
}

func TestRuntimeSnapshotBoundToFrozenRecipe(t *testing.T) {
	data, err := runtimeImageContext("news2douyin", "0.157.1")
	if err != nil {
		t.Fatal(err)
	}
	if !runtimeRecipeSupported(data, "Dockerfile") {
		t.Fatal("builtin lacks runtime helper")
	}
	snapshot := &BuildRuntimeSnapshot{RequestedMode: "auto", Mode: "npm", Version: "0.157.1", Platform: "linux/" + runtime.GOARCH}
	bound, err := freezeBuildRuntime(data, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(bound, []byte("build-runtime.lock.json")) {
		t.Fatal("snapshot absent")
	}
	m := testManager(t)
	id := "0123456789abcdef01234567"
	if err = os.MkdirAll(m.buildDir(id), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(m.buildDir(id), "context.zip"), bound, 0600); err != nil {
		t.Fatal(err)
	}
	sha := sha256.Sum256(bound)
	build := ImageBuild{ID: id, ContextSHA256: fmt.Sprintf("%x", sha), CodexRuntime: snapshot}
	if err = m.validateFrozenBuildRuntime(build); err != nil {
		t.Fatal(err)
	}
	changed := *snapshot
	changed.Mode = "native"
	build.CodexRuntime = &changed
	if err = m.validateFrozenBuildRuntime(build); err == nil || !strings.Contains(err.Error(), "冻结") {
		t.Fatal("changed snapshot accepted", err)
	}
}
