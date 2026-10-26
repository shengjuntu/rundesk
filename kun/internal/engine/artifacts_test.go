package kun

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestKunArtifactBoundedObservations(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("ok.txt", []byte("完整产物\n"))
	write("empty", nil)
	write("binary", []byte{0xff})
	write("large", make([]byte, maxArtifactBytes+1))
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("完整产物\n")))
	rules := []p.ArtifactRule{{Path: "ok.txt", Kind: "file_exists"}, {Path: "ok.txt", Kind: "file_contains", Value: "产物"}, {Path: "ok.txt", Kind: "file_equals", Value: "完整产物\n"}, {Path: "ok.txt", Kind: "file_sha256", Value: hash}, {Path: "empty", Kind: "file_equals"}, {Path: "missing", Kind: "file_exists"}, {Path: "large", Kind: "file_exists"}, {Path: "binary", Kind: "file_contains", Value: "x"}}
	evidence := captureArtifacts(dir, rules)
	for i, want := range []string{"pass", "pass", "pass", "pass", "pass", "fail", "unknown", "unknown"} {
		if evidence.Results[i].Status != want {
			t.Fatalf("%d: %+v", i, evidence.Results[i])
		}
	}
	if evidence.Results[5].Reason != "missing" || evidence.Results[6].Reason != "too_large" || evidence.Results[7].Reason != "invalid_utf8" {
		t.Fatal(evidence)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escape")); err == nil {
		result := captureArtifacts(dir, []p.ArtifactRule{{Path: "escape", Kind: "file_exists"}}).Results[0]
		if result.Status != "unknown" || result.SHA256 != "" {
			t.Fatal("escape read", result)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if result := captureArtifacts(dir, []p.ArtifactRule{{Path: "folder", Kind: "file_exists"}}).Results[0]; result.Status != "fail" {
		t.Fatal(result)
	}
}
func TestKunArtifactWorkerJournalSurvivesFileChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { final(w, "done") }))
	defer server.Close()
	db := filepath.Join(t.TempDir(), "worker.db")
	e, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	in := startRequest(t, server.URL)
	in.ArtifactRules = []p.ArtifactRule{{Path: "product.txt", Kind: "file_equals", Value: "original"}}
	if err = os.WriteFile(filepath.Join(in.Workspace, "product.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	var captured p.ArtifactEvidence
	events, err := e.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Type == "kun/run.finished" {
			var data struct {
				Artifacts p.ArtifactEvidence `json:"artifacts"`
			}
			if err = json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatal(err)
			}
			captured = data.Artifacts
		}
	}
	if len(captured.Results) != 1 || captured.Results[0].Status != "pass" {
		t.Fatal(captured)
	}
	if err = os.WriteFile(filepath.Join(in.Workspace, "product.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	e.Close()
	reopened, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replay, err := reopened.Events(0)
	if err != nil || !reflect.DeepEqual(events, replay) {
		t.Fatal("reopening recaptured today's files", err)
	}
}
