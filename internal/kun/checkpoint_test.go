package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func checkpointControl(t *testing.T, e *Engine, op string) {
	t.Helper()
	s := e.State()
	if _, err := e.Control(p.Control{RequestID: fmt.Sprintf("checkpoint-%s-%d-%s", s.RunID, s.Revision, op), RunID: s.RunID, ExpectedRevision: s.Revision, Operation: op, CallID: func() string {
		if s.Approval != nil {
			return s.Approval.CallID
		}
		return ""
	}()}); err != nil {
		t.Fatal(err)
	}
}
func TestCheckpointRestoresPendingWithoutReplayingCompletedTool(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		msg := p.Message{Role: "assistant", Content: "done"}
		if calls.Add(1) == 1 {
			msg.Content = ""
			for n := 1; n <= 2; n++ {
				msg.ToolCalls = append(msg.ToolCalls, p.ToolCall{ID: fmt.Sprint(n), Type: "function", Function: p.Function{Name: "write_file", Arguments: fmt.Sprintf(`{"path":"%d.txt","content":"written"}`, n)}})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 7}})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, server.URL)
	in.Config.PauseBeforeModel = true
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	checkpointControl(t, e, "step")
	waitKun(t, e, "paused")
	checkpointControl(t, e, "step")
	s := waitKun(t, e, "paused")
	if len(s.Pending) != 1 || s.Budget.ToolCalls != 1 {
		t.Fatal(s)
	}
	e.Close()
	if err = os.WriteFile(filepath.Join(in.Workspace, "1.txt"), []byte("do not overwrite"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	check, err := e.Checkpoint()
	if err != nil || !check.Eligible || check.Pending != 1 {
		t.Fatal(check, err)
	}
	in.RunID = "run-resumed"
	in.Resume = &check.Selection
	stale := in
	selection := *in.Resume
	selection.WorkerEpoch = "old-worker"
	stale.Resume = &selection
	if _, err = e.Start(stale); err == nil {
		t.Fatal("stale epoch accepted")
	}
	changed := in
	changed.Config.MaxSteps++
	if _, err = e.Start(changed); err == nil {
		t.Fatal("changed budget accepted")
	}
	changed = in
	changed.Workspace = t.TempDir()
	if _, err = e.Start(changed); err == nil {
		t.Fatal("changed workspace accepted")
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, e, "paused")
	if len(s.Pending) != 1 || s.Step != 1 || s.Budget.ReportedTokens != 7 || s.ResumedFrom == nil {
		t.Fatal(s)
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "paused")
	first, _ := os.ReadFile(filepath.Join(in.Workspace, "1.txt"))
	second, _ := os.ReadFile(filepath.Join(in.Workspace, "2.txt"))
	if string(first) != "do not overwrite" || string(second) != "written" || calls.Load() != 1 || s.Budget.ToolCalls != 2 {
		t.Fatal(string(first), string(second), calls.Load(), s.Budget)
	}
	checkpointControl(t, e, "resume")
	s = waitKun(t, e, "completed")
	if calls.Load() != 2 || s.Step != 2 || s.Budget.ReportedTokens != 14 {
		t.Fatal(s.Budget, s.Step, calls.Load())
	}
	if _, err = e.Start(in); err != nil {
		t.Fatal("idempotent retry failed", err)
	}
	if calls.Load() != 2 {
		t.Fatal("duplicate resume executed")
	}
	check, err = e.Checkpoint()
	if err != nil || check.Eligible {
		t.Fatal("completed run resumable", check, err)
	}
	var used string
	if err = e.j.db.QueryRow("SELECT consumed_by FROM checkpoints WHERE run_id=?", "run-one").Scan(&used); err != nil || used != in.RunID {
		t.Fatal("checkpoint consumption missing", used, err)
	}
}
func TestCheckpointRejectsUncertainAndUnappliedStates(t *testing.T) {
	for _, phase := range []string{"model", "tool", "steer", "legacy", "budget"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			e, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			in := startRequest(t, "http://127.0.0.1:1")
			in.Config.PauseBeforeModel = true
			if _, err = e.Start(in); err != nil {
				t.Fatal(err)
			}
			safe := waitKun(t, e, "paused")
			e.Close()
			j, err := openJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			safe.Actions = map[string]string{}
			safe.Status = "running"
			safe.Revision++
			switch phase {
			case "model":
				safe.Phase = "model"
				safe.Step++
			case "tool":
				safe.Phase = "tool"
				safe.Actions["uncertain"] = "dispatched"
				safe.Pending = []p.ToolCall{{ID: "uncertain"}}
			case "steer":
				safe.Queued = []p.Control{{RequestID: "unapplied-steer", Operation: "steer", Text: "do something else"}}
			case "legacy":
				safe.Manifest = nil
				_, err = j.db.Exec("DELETE FROM checkpoints")
				if err != nil {
					t.Fatal(err)
				}
			case "budget":
				safe.Budget.ToolCalls = safe.Config.Budget.MaxToolCalls
				safe.Pending = []p.ToolCall{{ID: "blocked"}}
				safe.Actions["blocked"] = "prepared"
			}
			if _, err = j.commit(safe, "kun/test.crash", nil, "", "", nil); err != nil {
				t.Fatal(err)
			}
			j.db.Close()
			e, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			check, err := e.Checkpoint()
			if err != nil || check.Eligible {
				t.Fatal("unsafe recovery offered", check, err)
			}
			if phase == "tool" && e.State().Actions["uncertain"] != "outcome_unknown" {
				t.Fatal(e.State())
			}
		})
	}
}
func TestCheckpointMCPReconnectRequiresFreshApproval(t *testing.T) {
	var calls, models atomic.Int32
	server := mcpFixture(t, &calls, "", nil)
	defer server.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.DisabledTools = []string{"delete_item"}
	in.MCP = []p.MCPServer{spec}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "paused")
	if s.Approval == nil {
		t.Fatal("missing approval")
	}
	e.Close()
	// Persist the precise crash window after approval was recorded but before dispatch.
	j, err := openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Status = "running"
	s.Approval.Decision = "approve"
	s.Revision++
	if _, err = j.commit(s, "kun/control.applied", nil, "", "", nil); err != nil {
		t.Fatal(err)
	}
	j.db.Close()
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	check, err := e.Checkpoint()
	if err != nil || !check.Eligible {
		t.Fatal(check, err)
	}
	in.RunID = "mcp-resumed"
	in.Resume = &check.Selection
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, e, "paused")
	if s.Phase != "approval" || s.Approval == nil || s.Approval.Decision != "" || calls.Load() != 0 || models.Load() != 1 {
		t.Fatal(s, calls.Load(), models.Load())
	}
	checkpointControl(t, e, "approve")
	waitKun(t, e, "completed")
	if calls.Load() != 1 || models.Load() != 2 {
		t.Fatal(calls.Load(), models.Load())
	}
}
func TestCheckpointCatalogDriftStopsBeforeCalls(t *testing.T) {
	var calls, models atomic.Int32
	fixture := mcpFixture(t, &calls, "", nil)
	defer fixture.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, model.URL)
	spec := mcpSpec(fixture.URL)
	spec.DisabledTools = []string{"delete_item"}
	in.MCP = []p.MCPServer{spec}
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "paused")
	e.Close()
	// Simulate an older persisted catalog with a changed server-side description.
	j, err := openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = j.db.QueryRow("SELECT snapshot FROM checkpoints WHERE run_id=?", in.RunID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved p.State
	json.Unmarshal(raw, &saved)
	saved.MCPTools[0].Description = "old description"
	if _, err = j.db.Exec("UPDATE checkpoints SET snapshot=? WHERE run_id=?", []byte(p.JSON(saved)), in.RunID); err != nil {
		t.Fatal(err)
	}
	j.db.Close()
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	check, err := e.Checkpoint()
	if err != nil || !check.Eligible {
		t.Fatal(check, err)
	}
	in.RunID = "catalog-resume"
	in.Resume = &check.Selection
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	<-e.done
	if e.State().Status != "failed" || !strings.Contains(e.State().Error, "catalog changed") || calls.Load() != 0 || models.Load() != 1 {
		t.Fatal(e.State(), calls.Load(), models.Load())
	}
}
