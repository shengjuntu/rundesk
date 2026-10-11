package kun

import (
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestKunFixtureFreshAndBoundedRoots(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("ORIGINAL"), 0600); err != nil {
		t.Fatal(err)
	}
	in := p.Start{SessionID: "sample-a", Workspace: base, WorkspaceFixture: &p.WorkspaceFixture{Files: []p.FixtureFile{{Path: "seed.txt", Content: "FIXED"}, {Path: "sub/empty.txt"}}}}
	a, err := prepareWorkspaceFixture(in)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(a.Root, "seed.txt")); string(b) != "FIXED" {
		t.Fatal("fixture missing")
	}
	if _, err = prepareWorkspaceFixture(in); err == nil {
		t.Fatal("existing directory reused")
	}
	if err = os.WriteFile(filepath.Join(a.Root, "seed.txt"), []byte("CHANGED"), 0600); err != nil {
		t.Fatal(err)
	}
	in.SessionID = "sample-b"
	b, err := prepareWorkspaceFixture(in)
	if err != nil || b.Root == a.Root {
		t.Fatal(b, err)
	}
	if data, _ := os.ReadFile(filepath.Join(b.Root, "seed.txt")); string(data) != "FIXED" {
		t.Fatal("earlier sample leaked")
	}
	if data, _ := os.ReadFile(filepath.Join(base, "seed.txt")); string(data) != "ORIGINAL" {
		t.Fatal("original changed")
	}
	call := p.ToolCall{Function: p.Function{Name: "read_file", Arguments: `{"path":"../../../seed.txt"}`}}
	if _, err = executeTool(b.Root, false, call); err == nil {
		t.Fatal("fixture escaped")
	}
	in.SessionID = "poisoned"
	root, _ := p.FixtureRoot(base, in.SessionID)
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "keep")
	if err = os.WriteFile(keep, []byte("KEEP"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareWorkspaceFixture(in); err == nil {
		t.Fatal("preexisting root adopted")
	}
	if data, _ := os.ReadFile(keep); string(data) != "KEEP" {
		t.Fatal("preexisting directory reset")
	}
}
func TestKunFixtureWorkerEvidenceRetryAndModeGuard(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); final(w, "done") }))
	defer server.Close()
	db := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	in := startRequest(t, server.URL)
	in.WorkspaceFixture = &p.WorkspaceFixture{Files: []p.FixtureFile{{Path: "seed", Content: "FIXED"}}}
	in.ArtifactRules = []p.ArtifactRule{{Path: "seed", Kind: "file_equals", Value: "FIXED"}}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	state := waitKun(t, e, "completed")
	if state.WorkspaceFixture == nil || state.WorkspaceFixture.Hash != p.FixtureHash(in.WorkspaceFixture) || FastGuardFor(state).WorkspaceMode != p.FixtureWorkspaceMode {
		t.Fatal(state)
	}
	// Mode remains a real guard; a shared-workspace candidate cannot silently match.
	shared := clone(state)
	shared.Manifest.WorkspaceMode = ""
	if fingerprint(FastGuardFor(shared)) == fingerprint(FastGuardFor(state)) {
		t.Fatal("workspace mode ignored")
	}
	seed := filepath.Join(state.WorkspaceFixture.Root, "seed")
	if err = os.WriteFile(seed, []byte("LATER"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal("idempotent retry failed", err)
	}
	if data, _ := os.ReadFile(seed); string(data) != "LATER" {
		t.Fatal("retry reset output")
	}
	in.RunID = "run-two"
	if _, err = e.Start(in); err == nil {
		t.Fatal("single-use worker reused")
	}
	e.Close()
	reopened, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.State(); got.WorkspaceFixture == nil || got.WorkspaceFixture.Hash != state.WorkspaceFixture.Hash {
		t.Fatal("evidence lost")
	}
	if data, _ := os.ReadFile(seed); string(data) != "LATER" {
		t.Fatal("reopen reset output")
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected model calls")
	}
	other := newTestEngine(t)
	bad := startRequest(t, server.URL)
	bad.WorkspaceFixture = &p.WorkspaceFixture{}
	bad.MCP = []p.MCPServer{{Name: "forbidden"}}
	if _, err = other.Start(bad); err == nil || !strings.Contains(err.Error(), "built-in-file-only") {
		t.Fatal("MCP accepted", err)
	}
	root, _ := p.FixtureRoot(bad.Workspace, bad.SessionID)
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("rejected start wrote files")
	}
}
