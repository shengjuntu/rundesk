package kun

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
)

// The durable 0.16.0 JSON/journal format is deliberately unchanged. Rewrite the
// admitted version in every stored snapshot, then close and reopen the actual
// journal; this exercises the same restoration path used on an upgraded host.
func legacyJournal(t *testing.T, path string) {
	t.Helper()
	j, err := openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.db.Close()
	for _, statement := range []string{
		`UPDATE state SET data=json_set(data,'$.manifest.engineVersion','0.16.0')`,
		`UPDATE events SET snapshot=json_set(snapshot,'$.manifest.engineVersion','0.16.0')`,
		`UPDATE checkpoints SET snapshot=json_set(snapshot,'$.manifest.engineVersion','0.16.0')`,
	} {
		if _, err = j.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacy016CheckpointRestoresWithExactEnvironment(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { final(w, "restored legacy journal") }))
	defer model.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, model.URL)
	in.ApprovalPolicy = "on-request"
	in.Config.PauseBeforeModel = true
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	e.Close()
	legacyJournal(t, path)
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.State().Manifest.EngineVersion != "0.16.0" {
		t.Fatal("legacy admission was overwritten")
	}
	check, err := e.CheckpointFor(in)
	if err != nil || !check.Eligible {
		t.Fatal(check, err)
	}
	in.RunID = "legacy-resumed"
	in.Resume = &check.Selection
	changed := in
	changed.Config.MaxSteps++
	if _, err = e.Start(changed); err == nil {
		t.Fatal("legacy version bypassed config guard")
	}
	changed = in
	changed.APIKey = "current resolved key"
	changed.ContextRevision += "-changed"
	if _, err = e.Start(changed); err == nil {
		t.Fatal("legacy version bypassed context guard")
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	checkpointControl(t, e, "resume")
	result := waitKun(t, e, "completed")
	if result.ResumedFrom == nil || result.Manifest.EngineVersion != "0.16.0" {
		t.Fatal(result)
	}
}

func TestLegacy016ForkExportsAndReplaysWithExactIdentity(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []p.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		for _, m := range req.Messages {
			if m.Role == "tool" {
				final(w, "done")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		msg := p.Message{Role: "assistant", ToolCalls: []p.ToolCall{{ID: "read", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"input.txt"}`}}}}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "tool_calls"}}})
	}))
	defer model.Close()
	path := filepath.Join(t.TempDir(), "source.db")
	source, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, model.URL)
	os.WriteFile(filepath.Join(in.Workspace, "input.txt"), []byte("historical observation"), 0600)
	if _, err = source.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, source, "completed")
	source.Close()
	legacyJournal(t, path)
	source, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	points, err := source.ForkPoints(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	selected := points.Selection
	for _, point := range points.Items {
		if point.Phase == "after_model" && point.Pending == 1 {
			selected.Sequence = point.Sequence
			break
		}
	}
	if selected.Sequence == 0 {
		t.Fatal("legacy fork point omitted", points)
	}
	bundle, err := source.ExportFork(p.ForkExport{Selection: selected})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.State.Manifest.EngineVersion != "0.16.0" || len(bundle.Records) != 1 {
		t.Fatal(bundle)
	}
	branch := newTestEngine(t)
	in.SessionID = "legacy-fork"
	in.RunID = "legacy-fork-run"
	in.Workspace = t.TempDir()
	in.Fork = &p.ForkStart{Origin: p.ForkOrigin{PreviewID: "legacy-preview", SessionID: bundle.State.SessionID, RunID: bundle.State.RunID, Sequence: selected.Sequence, Through: selected.Through, BundleHash: bundle.ContentHash, Mode: "hybrid"}, Bundle: bundle}
	if _, err = branch.Start(in); err != nil {
		t.Fatal(err)
	}
	result := waitKun(t, branch, "completed")
	if result.Fork == nil || result.Fork.ReplayCursor != 1 || result.Manifest.EngineVersion != "0.16.0" {
		t.Fatal(result)
	}
	if _, err = os.Stat(filepath.Join(in.Workspace, "input.txt")); !os.IsNotExist(err) {
		t.Fatal("hybrid replay accessed fresh workspace")
	}
}
