package kun

import (
	"encoding/json"
	p "github.com/your-org/kun/sdk/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func fastFixture(e *Engine, in p.Start) p.FastProgram {
	in.Config = in.Config.Normalized()
	s := p.State{Config: in.Config, Manifest: e.manifest(in), Harness: e.nextHarness(in.Config), ApprovalPolicy: "on-request"}
	for _, d := range toolDefinitions(in.Config.AllowWrite, in.Config.AllowBash) {
		s.ToolDefinitions = append(s.ToolDefinitions, p.JSON(d))
	}
	return p.FastProgram{Schema: 1, Domain: "file-copy", InputSchema: p.JSON(map[string]any{"type": "object", "required": []string{"domain", "path", "content"}, "properties": map[string]any{"domain": map[string]string{"type": "string"}, "path": map[string]string{"type": "string"}, "content": map[string]string{"type": "string"}}, "additionalProperties": false}), Guard: FastGuardFor(s), Steps: []p.FastInstruction{
		{Op: "tool", Tool: "write_file", Store: "written", Args: map[string]p.FastValue{"path": {Ref: "input#/path"}, "content": {Ref: "input#/content"}}},
		{Op: "tool", Tool: "read_file", Store: "read", Args: map[string]p.FastValue{"path": {Ref: "input#/path"}}},
		{Op: "finish", Value: p.FastValue{Ref: "read"}},
	}}
}
func fastInput(in p.Start, program p.FastProgram) p.Start {
	in.Input = `{"domain":"file-copy","path":"result.txt","content":"new evidence"}`
	in.Fast = &p.FastStart{CandidateID: "candidate", CandidateHash: "hash", Program: program, Input: json.RawMessage(in.Input)}
	return in
}
func TestFastPathNoModelAndFreshToolEvidence(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { models.Add(1); final(w, "unexpected") }))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	in = fastInput(in, fastFixture(e, in))
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	if s.Fast.Status != "completed" || s.Step != 0 || s.Budget.ToolCalls != 2 || models.Load() != 0 || !strings.Contains(s.Messages[len(s.Messages)-1].Content, "new evidence") {
		t.Fatalf("bad fast result %+v", s)
	}
	if len(s.Actions) != 2 || s.Fast.PC != 3 {
		t.Fatal(s.Actions, s.Fast.PC)
	}
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	if e.State().Budget.ToolCalls != 2 {
		t.Fatal("replayed duplicate run")
	}
}
func TestFastFallbackKeepsLedgerAndDeduplicatesWrite(t *testing.T) {
	var models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []p.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if models.Add(1) == 1 {
			seen := false
			for _, m := range req.Messages {
				if m.Role == "tool" && m.ToolCallID != "" {
					seen = true
				}
			}
			if !seen {
				t.Error("fallback lost completed tool result")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", ToolCalls: []p.ToolCall{{ID: "dynamic-new-id", Type: "function", Function: p.Function{Name: "write_file", Arguments: `{"content":"new evidence","path":"result.txt"}`}}}}, "finish_reason": "tool_calls"}}})
		} else {
			final(w, "recovered")
		}
	}))
	defer server.Close()
	e := newTestEngine(t)
	in := startRequest(t, server.URL)
	program := fastFixture(e, in)
	program.Steps = append(program.Steps[:1], p.FastInstruction{Op: "assert", Value: p.FastValue{Ref: "written"}, Schema: json.RawMessage(`{"const":"impossible assertion"}`)}, p.FastInstruction{Op: "finish", Value: p.FastValue{Ref: "written"}})
	in = fastInput(in, program)
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	if s.Fast.Status != "fallback" || s.Fast.PC != 1 || models.Load() != 2 || s.Budget.ToolCalls != 1 || s.Actions["dynamic-new-id"] != "reused" {
		t.Fatalf("unsafe fallback %+v", s)
	}
	b, err := os.ReadFile(filepath.Join(in.Workspace, "result.txt"))
	if err != nil || string(b) != "new evidence" {
		t.Fatal(string(b), err)
	}
	check, err := e.Checkpoint()
	if err != nil || check.Eligible {
		t.Fatal("fast run allowed checkpoint replay", check, err)
	}
}
func TestFastGuardsAndIRRejectUnsafePrograms(t *testing.T) {
	for _, mutate := range []func(*p.FastProgram){
		func(v *p.FastProgram) { v.Guard.EngineVersion = "stale" }, func(v *p.FastProgram) { v.Guard.EngineVersion = "0.16.0" }, func(v *p.FastProgram) { v.Guard.CatalogHash = "changed" }, func(v *p.FastProgram) { v.Guard.ConfigHash = "changed" }, func(v *p.FastProgram) { v.Guard.SkillsHash = "changed" }, func(v *p.FastProgram) { v.Guard.MCPHash = "changed" }, func(v *p.FastProgram) { v.Guard.Workspace = "changed" }, func(v *p.FastProgram) { v.Guard.ApprovalPolicy = "never" }, func(v *p.FastProgram) { v.Domain = "wrong" }, func(v *p.FastProgram) { v.InputSchema = json.RawMessage(`{"type":"object","required":["absent"]}`) },
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { final(w, "dynamic") }))
		e := newTestEngine(t)
		in := startRequest(t, server.URL)
		pgr := fastFixture(e, in)
		mutate(&pgr)
		in = fastInput(in, pgr)
		if _, err := e.Start(in); err != nil {
			t.Fatal(err)
		}
		s := waitKun(t, e, "completed")
		server.Close()
		if s.Fast.Status != "fallback" || s.Budget.ToolCalls != 0 || s.Step != 1 {
			t.Fatal("guard bypass", s.Fast)
		}
	}
	e := newTestEngine(t)
	in := startRequest(t, "http://127.0.0.1:1")
	for _, mutate := range []func(*p.FastProgram){func(v *p.FastProgram) { v.Steps[0].Op = "python" }, func(v *p.FastProgram) {
		v.Steps[0] = p.FastInstruction{Op: "branch", Value: p.FastValue{Ref: "input"}, Equals: json.RawMessage(`null`), Then: 0, Else: 1}
	}, func(v *p.FastProgram) { v.Steps[2].Value = p.FastValue{Literal: json.RawMessage(`"cached answer"`)} }, func(v *p.FastProgram) { v.InputSchema = json.RawMessage(`{"$ref":"https://example.com"}`) }} {
		v := fastFixture(e, in)
		mutate(&v)
		if ValidateFastProgram(v) == nil {
			t.Fatal("invalid IR accepted", v)
		}
	}
}

func TestFastMCPStillRequiresDurableApproval(t *testing.T) {
	var external, models atomic.Int32
	server := mcpFixture(t, &external, "", nil)
	defer server.Close()
	model := mcpModel(t, &models, nil)
	defer model.Close()
	source := newTestEngine(t)
	in := startRequest(t, model.URL)
	spec := mcpSpec(server.URL)
	spec.Allowlist = true
	spec.EnabledTools = []string{"lookup weather"}
	in.MCP = []p.MCPServer{spec}
	if _, err := source.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, source, "paused")
	if _, err := source.Control(p.Control{RequestID: "approve-source", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "approve", CallID: s.Approval.CallID}); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, source, "completed")
	fast := newTestEngine(t)
	in.SessionID = "fast-mcp"
	in.RunID = "fast-run"
	in.Input = `{"domain":"weather","city":"Shanghai"}`
	in.Fast = &p.FastStart{CandidateID: "weather-candidate", CandidateHash: "hash", Input: json.RawMessage(in.Input), Program: p.FastProgram{Schema: 1, Domain: "weather", InputSchema: json.RawMessage(`{"type":"object","required":["domain","city"]}`), Guard: FastGuardFor(s), Steps: []p.FastInstruction{{Op: "tool", Tool: s.MCPTools[0].Alias, Store: "weather", Args: map[string]p.FastValue{"city": {Ref: "input#/city"}}}, {Op: "finish", Value: p.FastValue{Ref: "weather"}}}}}
	if _, err := fast.Start(in); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, fast, "paused")
	if s.Approval == nil || external.Load() != 1 || models.Load() != 2 {
		t.Fatal("fast bypassed approval", s.Fast)
	}
	if _, err := fast.Control(p.Control{RequestID: "cannot-skip-fast-approval", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "resume"}); err == nil {
		t.Fatal("resume granted permission")
	}
	if _, err := fast.Control(p.Control{RequestID: "approve-fast-tool", RunID: s.RunID, ExpectedRevision: s.Revision, Operation: "approve", CallID: s.Approval.CallID}); err != nil {
		t.Fatal(err)
	}
	s = waitKun(t, fast, "completed")
	if s.Fast.Status != "completed" || external.Load() != 2 || models.Load() != 2 || s.Budget.ToolCalls != 1 {
		t.Fatal(s.Fast, external.Load(), models.Load())
	}
}
func TestFastExtractionBranchAndEvidenceGuard(t *testing.T) {
	e := newTestEngine(t)
	in := startRequest(t, "http://127.0.0.1:1")
	program := fastFixture(e, in)
	program.Steps = []p.FastInstruction{program.Steps[0], program.Steps[1], {Op: "extract", Value: p.FastValue{Ref: "read"}, Pointer: "/answer", Store: "answer"}, {Op: "assert", Value: p.FastValue{Ref: "answer"}, Schema: json.RawMessage(`{"type":"string","minLength":1}`)}, {Op: "branch", Value: p.FastValue{Ref: "input#/path"}, Equals: json.RawMessage(`"result.txt"`), Then: 5, Else: 6}, {Op: "finish", Value: p.FastValue{Ref: "answer"}}, {Op: "finish", Value: p.FastValue{Ref: "read"}}}
	in = fastInput(in, program)
	in.Input = `{"domain":"file-copy","path":"result.txt","content":"{\"answer\":\"fresh JSON result\"}"}`
	in.Fast.Input = json.RawMessage(in.Input)
	if _, err := e.Start(in); err != nil {
		t.Fatal(err)
	}
	s := waitKun(t, e, "completed")
	if s.Messages[len(s.Messages)-1].Content != "fresh JSON result" || s.Fast.PC != 6 || !s.Fast.Evidence["answer"] {
		t.Fatal(s.Fast)
	}
}
