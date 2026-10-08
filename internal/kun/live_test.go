package kun

import (
	"encoding/json"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func liveBundle(t *testing.T, e *Engine, choose func(p.State) bool) p.ForkBundle {
	t.Helper()
	points, err := e.ForkPoints(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	for _, point := range points.Items {
		snap, err := e.Snapshot(point.Sequence)
		if err != nil {
			t.Fatal(err)
		}
		if choose(snap.State) {
			selection.Sequence = point.Sequence
			break
		}
	}
	if selection.Sequence == 0 {
		t.Fatal("no matching boundary", points)
	}
	b, err := e.ExportFork(p.ForkExport{Selection: selection, Mode: "live"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Mode != "live" || len(b.Records) != 0 {
		t.Fatal("Live imported a replay tape")
	}
	return b
}
func liveRequest(in p.Start, b p.ForkBundle, sid string) p.Start {
	in.SessionID = sid
	in.RunID = sid + "-run"
	in.Resume = nil
	in.Fork = &p.ForkStart{ConfirmLive: true, Bundle: b, Origin: p.ForkOrigin{PreviewID: "preview-" + sid, Mode: "live", SessionID: b.State.SessionID, RunID: b.State.RunID, Sequence: b.Selection.Sequence, Through: b.Selection.Through, BundleHash: b.ContentHash}}
	return in
}

func TestLiveForkCurrentFilesAndSourceJournalIsolation(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Messages []p.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		models.Add(1)
		live := strings.Contains(q.Messages[0].Content, "RunDesk Live")
		if q.Messages[len(q.Messages)-1].Role == "tool" {
			if live && !strings.Contains(string(p.JSON(q.Messages)), "CURRENT WORLD") {
				t.Error("Live reused historical result")
			}
			planReply(w, "done")
			return
		}
		name := "input.txt"
		if live {
			name = "new-path.txt"
		}
		modelTools(w, []p.ToolCall{{ID: "read", Type: "function", Function: p.Function{Name: "read_file", Arguments: `{"path":"` + name + `"}`}}, {ID: "write", Type: "function", Function: p.Function{Name: "write_file", Arguments: `{"path":"proof.txt","content":"fresh execution"}`}}}, map[string]int{"total_tokens": 5})
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	_ = os.WriteFile(filepath.Join(in.Workspace, "input.txt"), []byte("old input"), 0600)
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	<-e.done
	before := fingerprint(e.State())
	bundle := liveBundle(t, e, func(s p.State) bool { return s.Phase == "before_model" && s.Step == 0 })
	_ = os.WriteFile(filepath.Join(in.Workspace, "new-path.txt"), []byte("CURRENT WORLD"), 0600)
	_ = os.WriteFile(filepath.Join(in.Workspace, "proof.txt"), []byte("do not restore snapshot"), 0600)
	for _, name := range []string{"unconfirmed", "mode-change", "workspace", "config", "context-revision"} {
		t.Run(name, func(t *testing.T) {
			branch := newTestEngine(t)
			request := liveRequest(in, bundle, name)
			switch name {
			case "unconfirmed":
				request.Fork.ConfirmLive = false
			case "mode-change":
				request.Fork.Origin.Mode = "hybrid"
			case "workspace":
				request.Workspace = t.TempDir()
			case "config":
				request.Config.AllowWrite = false
			case "context-revision":
				request.ContextRevision = "changed"
			}
			if _, err := branch.Start(request); err == nil {
				t.Fatal("unsafe start accepted")
			}
			if branch.State().SessionID != "" || models.Load() != 2 {
				t.Fatal("rejected start executed")
			}
		})
	}
	branch := newTestEngine(t)
	request := liveRequest(in, bundle, "live")
	if _, err := branch.Start(request); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, branch, "completed")
	<-branch.done
	if s.Fork.Origin.Mode != "live" || s.Fork.ReplayCursor != 0 || s.Budget.ToolCalls != 2 || models.Load() != 4 {
		t.Fatal(s, models.Load())
	}
	if raw, _ := os.ReadFile(filepath.Join(in.Workspace, "proof.txt")); string(raw) != "fresh execution" {
		t.Fatal("Live did not write current workspace")
	}
	if before != fingerprint(e.State()) {
		t.Fatal("source state changed")
	}
	started := 0
	for _, ev := range allKunEvents(t, branch) {
		if ev.Type == "kun/tool.started" {
			started++
		}
		if ev.Type == "kun/tool.completed" && (!strings.Contains(string(ev.Data), `"executionMode":"live"`) || strings.Contains(string(ev.Data), `"replay"`)) {
			t.Fatal("wrong execution evidence", string(ev.Data))
		}
	}
	if started != 2 {
		t.Fatal(started)
	}
	if _, err := branch.Start(request); err != nil || models.Load() != 4 {
		t.Fatal("duplicate executed", err)
	}
	request.Fork = nil
	request.RunID = "ordinary"
	if _, err := branch.Start(request); err == nil {
		t.Fatal("Live became ordinary")
	}
	if check, err := branch.Checkpoint(); err != nil || check.Eligible || check.Reason != "live_use_new_fork" {
		t.Fatal(check, err)
	}
}

func TestLiveForkReauthorizesMCPAndRejectsCatalogDrift(t *testing.T) {
	var calls, models, connections atomic.Int32
	var drift atomic.Bool
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var q struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if q.ID == nil {
			w.WriteHeader(202)
			return
		}
		var value any
		switch q.Method {
		case "initialize":
			connections.Add(1)
			value = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			description := "original"
			if drift.Load() {
				description = "changed"
			}
			value = map[string]any{"tools": []any{map[string]any{"name": "lookup weather", "description": description, "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			calls.Add(1)
			value = map[string]any{"content": []any{map[string]string{"type": "text", "text": "fresh MCP result"}}}
		default:
			t.Error("unexpected MCP method", q.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": value})
	}))
	defer mcp.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	source := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(mcp.URL)
	spec.Headers = map[string]string{"Authorization": "Bearer first-credential"}
	in.MCP = []p.MCPServer{spec}
	if _, err := source.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, source, "paused")
	checkpointControl(t, source, "approve")
	waitKun(t, source, "completed")
	<-source.done
	bundle := liveBundle(t, source, func(s p.State) bool {
		return s.Phase == "approval" && s.Approval != nil && s.Approval.Decision == "approve"
	})
	for _, decision := range []string{"reject", "approve"} {
		t.Run(decision, func(t *testing.T) {
			prior := calls.Load()
			branch := newTestEngine(t)
			request := liveRequest(in, bundle, decision)
			if _, err := branch.Start(request); err != nil {
				t.Fatal(err)
			}
			s := waitKun(t, branch, "paused")
			if s.Approval == nil || s.Approval.Decision != "" || calls.Load() != prior {
				t.Fatal("source approval reused", s)
			}
			checkpointControl(t, branch, decision)
			s = waitKun(t, branch, "completed")
			want := prior
			if decision == "approve" {
				want++
			}
			if calls.Load() != want || s.Fork.ReplayCursor != 0 {
				t.Fatal(s, calls.Load(), want)
			}
		})
	}
	branch := newTestEngine(t)
	bad := liveRequest(in, bundle, "credential-change")
	bad.MCP = append([]p.MCPServer(nil), in.MCP...)
	bad.MCP[0].Headers = map[string]string{"Authorization": "Bearer new-credential"}
	before := connections.Load()
	if _, err := branch.Start(bad); err == nil || connections.Load() != before {
		t.Fatal("credential drift connected", err)
	}
	drift.Store(true)
	branch = newTestEngine(t)
	beforeCalls, beforeModels := calls.Load(), models.Load()
	if _, err := branch.Start(liveRequest(in, bundle, "catalog-change")); err != nil {
		t.Fatal(err)
	}
	s := waitTerminal(t, branch)
	if s.Status != "failed" || !strings.Contains(s.Error, "catalog changed") || calls.Load() != beforeCalls || models.Load() != beforeModels || s.Approval != nil {
		t.Fatal("catalog drift executed", s)
	}
}

func TestLiveForkRejectsInheritedExhaustedBudget(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models.Add(1)
		modelTools(w, []p.ToolCall{writeCall("write", "never.txt")}, map[string]int{"total_tokens": 7})
	}))
	defer server.Close()
	source := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.Budget.MaxTotalTokens = 7
	if _, err := source.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitTerminal(t, source)
	<-source.done
	if s.Budget.StopReason != "reported_token_threshold" {
		t.Fatal(s)
	}
	b := liveBundle(t, source, func(s p.State) bool { return s.Phase == "after_model" && len(s.Pending) == 1 })
	branch := newTestEngine(t)
	if _, err := branch.Start(liveRequest(in, b, "budget")); err == nil || !strings.Contains(err.Error(), "reported_token_threshold") {
		t.Fatal(err)
	}
	if models.Load() != 1 || branch.State().SessionID != "" {
		t.Fatal("budget was reset")
	}
	if _, err := os.Stat(filepath.Join(in.Workspace, "never.txt")); !os.IsNotExist(err) {
		t.Fatal("budget allowed a write")
	}
}
