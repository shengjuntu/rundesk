package kun

import (
	"context"
	"encoding/json"
	p "github.com/your-org/kun/sdk/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func codingCall(name string, args any) p.ToolCall {
	return p.ToolCall{ID: "coding-call", Type: "function", Function: p.Function{Name: name, Arguments: string(p.JSON(args))}}
}
func requireBash(t *testing.T) {
	t.Helper()
	if _, err := resolveBash(""); err != nil {
		t.Skip(err)
	}
}
func runBash(t *testing.T, ctx context.Context, workspace, command string, timeout int) (bashResult, toolResult) {
	t.Helper()
	requireBash(t)
	result, err := executeBash(ctx, workspace, p.Config{AllowBash: true, BashTimeoutSeconds: timeout}, codingCall("bash", map[string]any{"command": command}))
	if err != nil {
		t.Fatal(err)
	}
	var value bashResult
	if err = json.Unmarshal(result.Raw, &value); err != nil {
		t.Fatal(result.Output, err)
	}
	return value, result
}
func TestBashExecutionAndFreshShell(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "sub dir"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", filepath.Join(workspace, "startup"))
	os.WriteFile(filepath.Join(workspace, "startup"), []byte("echo injected; exit 99"), 0600)
	result, raw := runBash(t, context.Background(), workspace, "printf '你好\\n' | cat > 'sub dir/result.txt'; cat 'sub dir/result.txt'; printf problem >&2; exit 7", 5)
	if !raw.IsError || result.ExitCode == nil || *result.ExitCode != 7 || result.Stdout != "你好\n" || result.Stderr != "problem" {
		t.Fatalf("%+v", result)
	}
	runBash(t, context.Background(), workspace, "cd 'sub dir'; export KUN_TRANSIENT=one", 5)
	result, raw = runBash(t, context.Background(), workspace, "test -f 'sub dir/result.txt' && test -z \"${KUN_TRANSIENT:-}\" && printf fresh", 5)
	if raw.IsError || result.Stdout != "fresh" {
		t.Fatalf("%+v", result)
	}
	disabled, _ := executeBash(context.Background(), workspace, p.Config{}, codingCall("bash", map[string]any{"command": "touch forbidden"}))
	missing, _ := executeBash(context.Background(), workspace, p.Config{AllowBash: true, BashPath: filepath.Join(workspace, "missing-shell")}, codingCall("bash", map[string]any{"command": "touch forbidden"}))
	if !disabled.IsError || !missing.IsError {
		t.Fatal("disabled or missing executable accepted")
	}
	if _, err := os.Stat(filepath.Join(workspace, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("unexpected side effect")
	}
}
func TestBashBoundedOutput(t *testing.T) {
	result, raw := runBash(t, context.Background(), t.TempDir(), "for ((i=0;i<10000;i++)); do printf 1234567890; printf abcdefghij >&2; done; printf END; printf ERR >&2", 10)
	if raw.IsError || !result.Truncated || result.StdoutBytes != 100003 || result.StderrBytes != 100003 || len(result.Stdout) != bashOutputLimit || len(result.Stderr) != bashOutputLimit || !strings.HasSuffix(result.Stdout, "END") || !strings.HasSuffix(result.Stderr, "ERR") {
		t.Fatalf("output bounds: %+v", result)
	}
}

func TestBashDisabledAndFixtureCannotExecute(t *testing.T) {
	var count atomic.Int32
	server := bashModel(t, "printf forbidden > marker", &count)
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	if s.Budget.ToolCalls != 0 {
		t.Fatal("disabled Bash dispatched")
	}
	if _, err := os.Stat(filepath.Join(in.Workspace, "marker")); !os.IsNotExist(err) {
		t.Fatal("disabled Bash wrote")
	}
	fixture := newTestEngine(t)
	in.Config.AllowBash = true
	in.WorkspaceFixture = &p.WorkspaceFixture{Files: []p.FixtureFile{}}
	if _, err := fixture.Start(in); err == nil {
		t.Fatal("fixture accepted Bash")
	}
	if len(toolDefinitions(true, true)) != 7 || len(toolDefinitions(false, false)) != 4 {
		t.Fatal("wrong capability catalog")
	}
}

func TestCodingSpecialFilesCannotBlockTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO is POSIX-only")
	}
	requireBash(t)
	dir := t.TempDir()
	_, result := runBash(t, context.Background(), dir, "mkfifo input output", 5)
	if result.IsError {
		t.Fatal(result.Output)
	}
	for _, name := range []string{"read_file", "write_file", "edit_file"} {
		args := map[string]any{"path": "input"}
		if name == "write_file" {
			args["path"] = "output"
			args["content"] = "no"
		}
		if name == "edit_file" {
			args["edits"] = []textEdit{{"a", "b"}}
		}
		done := make(chan error, 1)
		go func() { _, err := executeTool(dir, true, codingCall(name, args)); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("special file accepted")
			}
		case <-time.After(time.Second):
			t.Fatal("special file blocked", name)
		}
	}
}
func TestBashTimeoutAndCancellationCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows process tree validation required separately")
	}
	for _, mode := range []string{"timeout", "cancel", "completed"} {
		t.Run(mode, func(t *testing.T) {
			workspace := t.TempDir()
			ctx := context.Background()
			var cancel context.CancelFunc
			if mode == "cancel" {
				ctx, cancel = context.WithCancel(ctx)
				time.AfterFunc(150*time.Millisecond, cancel)
				defer cancel()
			}
			command := "(sleep 1.5; printf leaked > child-marker) & wait"
			if mode == "completed" {
				command = "(sleep 1.5; printf leaked > child-marker) >/dev/null 2>&1 & printf done"
			}
			result, raw := runBash(t, ctx, workspace, command, 1)
			if mode == "timeout" && (!raw.IsError || !result.TimedOut || result.ExitCode != nil) {
				t.Fatalf("%+v", result)
			}
			if mode == "cancel" && (!raw.IsError || !result.Cancelled) {
				t.Fatalf("%+v", result)
			}
			if mode == "completed" && (raw.IsError || result.Stdout != "done") {
				t.Fatalf("%+v", result)
			}
			time.Sleep(1600 * time.Millisecond)
			if _, err := os.Stat(filepath.Join(workspace, "child-marker")); !os.IsNotExist(err) {
				t.Fatal("child escaped ordinary process group cleanup")
			}
		})
	}
}
func bashModel(t *testing.T, command string, count *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			call := codingCall("bash", map[string]any{"command": command})
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", ToolCalls: []p.ToolCall{call}}, "finish_reason": "tool_calls"}}})
		} else {
			final(w, "done")
		}
	}))
}
func TestBashDurableApprovalAndDenial(t *testing.T) {
	requireBash(t)
	for _, decision := range []string{"approve", "reject", "never"} {
		t.Run(decision, func(t *testing.T) {
			var count atomic.Int32
			server := bashModel(t, "printf once >> marker", &count)
			defer server.Close()
			e := newTestEngine(t)
			in := startRequest(t, server.URL)
			in.Config.AllowBash = true
			if decision == "never" {
				in.ApprovalPolicy = "never"
			}
			if _, err := e.Start(in); err != nil {
				t.Fatal(err)
			}
			if decision != "never" {
				s := waitKun(t, e, "paused")
				if s.Approval == nil || s.Approval.Kind != "builtin" || s.Approval.Tool != "bash" {
					t.Fatal(s.Approval)
				}
				if _, err := os.Stat(filepath.Join(in.Workspace, "marker")); !os.IsNotExist(err) {
					t.Fatal("ran before approval")
				}
				if _, err := e.Control(p.Control{RequestID: "skip", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"}); err == nil {
					t.Fatal("resume bypassed approval")
				}
				if _, err := e.Control(p.Control{RequestID: "decision", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: decision, CallID: s.Approval.CallID}); err != nil {
					t.Fatal(err)
				}
			}
			state := waitKun(t, e, "completed")
			data, err := os.ReadFile(filepath.Join(in.Workspace, "marker"))
			if decision == "approve" {
				if err != nil || string(data) != "once" || state.Actions["coding-call"] != "succeeded" {
					t.Fatal(string(data), err, state.Actions)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("denied tool ran")
			}
		})
	}
}

func TestBashHybridReplaysWithoutExecution(t *testing.T) {
	requireBash(t)
	var count atomic.Int32
	server := bashModel(t, "printf once >> marker", &count)
	defer server.Close()
	source := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.AllowBash = true
	in.Config.BashApproval = "approve"
	if _, err := source.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, source, "completed")
	<-source.done
	points, err := source.ForkPoints(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	selection := points.Selection
	for _, point := range points.Items {
		if point.Phase == "before_model" && point.Step == 0 {
			selection.Sequence = point.Sequence
		}
	}
	bundle, err := source.ExportFork(p.ForkExport{Selection: selection})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Records) != 1 {
		t.Fatal(bundle.Records)
	}
	branch := newTestEngine(t)
	in.SessionID = "hybrid"
	in.RunID = "hybrid-run"
	in.Workspace = t.TempDir()
	count.Store(0)
	in.Fork = &p.ForkStart{Origin: p.ForkOrigin{PreviewID: "preview", SessionID: bundle.State.SessionID, RunID: bundle.State.RunID, Sequence: bundle.Selection.Sequence, Through: bundle.Selection.Through, BundleHash: bundle.ContentHash, Mode: "hybrid"}, Bundle: bundle}
	if _, err = branch.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, branch, "completed")
	if _, err = os.Stat(filepath.Join(in.Workspace, "marker")); !os.IsNotExist(err) {
		t.Fatal("Hybrid dispatched Bash")
	}
	events, _ := branch.Events(0)
	found := false
	for _, event := range events {
		if event.Type == "kun/tool.completed" && strings.Contains(string(event.Data), `"replay"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("missing replay evidence")
	}
}
func TestFastBashFallbackDoesNotRepeatSideEffect(t *testing.T) {
	requireBash(t)
	var count atomic.Int32
	server := bashModel(t, "printf once >> marker", &count)
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in.Config.AllowBash = true
	in.Config.BashApproval = "approve"
	program := fastFixture(e, in)
	program.Steps = []p.FastInstruction{
		{Op: "tool", Tool: "bash", Store: "executed", Args: map[string]p.FastValue{"command": {Literal: json.RawMessage(`"printf once >> marker"`)}}},
		{Op: "assert", Value: p.FastValue{Ref: "executed"}, Schema: json.RawMessage(`{"const":"force fallback"}`)},
		{Op: "finish", Value: p.FastValue{Ref: "executed"}},
	}
	if _, err := e.Start(fastInput(in, program)); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	data, err := os.ReadFile(filepath.Join(in.Workspace, "marker"))
	if err != nil || string(data) != "once" || s.Fast.Status != "fallback" || s.Actions["coding-call"] != "reused" || s.Budget.ToolCalls != 1 || count.Load() != 2 {
		t.Fatal(string(data), err, s.Fast, s.Actions, s.Budget)
	}
}
