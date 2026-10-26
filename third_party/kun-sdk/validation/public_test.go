package validation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
)

func TestStrictJSONLimitsAndNoSideEffects(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{} {}`, strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66), `1e1025`} {
		if _, err := StrictJSON([]byte(raw)); err == nil {
			t.Fatalf("invalid input accepted: %.80s", raw)
		}
	}
	raw := []byte(`{"n":9007199254740993,"text":"中文"}`)
	before := string(raw)
	value, err := StrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]any)["n"] != json.Number("9007199254740993") || string(raw) != before {
		t.Fatal("decoder changed input or precision")
	}
	schema, err := CompileArguments([]byte(`{"type":"string","minLength":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateValue("中文"); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate([]byte(`"中文"`)); err == nil {
		t.Fatal("tool arguments allowed a scalar")
	}
	if err := schema.ValidateValue("x"); err == nil {
		t.Fatal("value ignored schema assertion")
	}
	if _, err := CompileArguments([]byte(strings.Repeat(" ", 65537))); err == nil {
		t.Fatal("unbounded schema accepted")
	}
	if err := schema.ValidateValue(strings.Repeat("x", (1<<20)+1)); err == nil {
		t.Fatal("unbounded value accepted")
	}
}
func fixtureState() p.State {
	return p.State{
		Manifest:        &p.RunManifest{EngineVersion: p.EngineVersion, ConfigHash: "config", SkillsHash: "skills", MCPHash: "mcp", Workspace: "/work", ContextRevision: "revision"},
		ApprovalPolicy:  "prompt",
		Harness:         p.Harness{},
		ToolDefinitions: []json.RawMessage{json.RawMessage(`{"name":"b","parameters":{}}`), json.RawMessage(`{"name":"a","parameters":{}}`)},
		MCPTools:        []p.MCPTool{{Alias: "second", Name: "b"}, {Alias: "first", Name: "a"}},
	}
}
func TestCatalogAndFastGuardsArePureAndStable(t *testing.T) {
	state := fixtureState()
	before := string(p.JSON(state))
	guard := FastGuardFor(state)
	if guard.EngineVersion != p.EngineVersion || guard.CatalogHash != CatalogFingerprint(state) || guard.ContextRevision != "revision" {
		t.Fatalf("wrong guard: %#v", guard)
	}
	state.ToolDefinitions[0], state.ToolDefinitions[1] = state.ToolDefinitions[1], state.ToolDefinitions[0]
	state.MCPTools[0], state.MCPTools[1] = state.MCPTools[1], state.MCPTools[0]
	if !reflect.DeepEqual(guard, FastGuardFor(state)) {
		t.Fatal("catalog enumeration order changed guard")
	}
	state.ToolDefinitions[0], state.ToolDefinitions[1] = state.ToolDefinitions[1], state.ToolDefinitions[0]
	state.MCPTools[0], state.MCPTools[1] = state.MCPTools[1], state.MCPTools[0]
	if string(p.JSON(state)) != before {
		t.Fatal("validator mutated input")
	}
	if FastGuardFor(p.State{}) != (p.FastGuard{}) {
		t.Fatal("manifest-less state produced a guard")
	}
	state.MCPTools[0].Name = "changed"
	if guard.CatalogHash == CatalogFingerprint(state) {
		t.Fatal("tool change lost")
	}
	if Fingerprint(map[string]int{"a": 1, "b": 2}) != Fingerprint(map[string]int{"b": 2, "a": 1}) {
		t.Fatal("object key order changed fingerprint")
	}
}
func TestFastProgramValidationIsPureAndBounded(t *testing.T) {
	program := p.FastProgram{Schema: 1, Domain: "test", InputSchema: p.JSON(map[string]string{"type": "object"}), Guard: FastGuardFor(fixtureState()), Steps: []p.FastInstruction{
		{Op: "tool", Tool: "read", Store: "result", Args: map[string]p.FastValue{"path": {Literal: p.JSON("a.txt")}}},
		{Op: "finish", Value: p.FastValue{Ref: "result"}},
	}}
	before := string(p.JSON(program))
	if err := ValidateFastProgram(program); err != nil {
		t.Fatal(err)
	}
	if string(p.JSON(program)) != before {
		t.Fatal("validator mutated program")
	}
	for _, update := range []func(*p.FastProgram){
		func(v *p.FastProgram) { v.Schema = 2 },
		func(v *p.FastProgram) { v.Guard.ConfigHash = "" },
		func(v *p.FastProgram) { v.Steps[1].Value = p.FastValue{Literal: p.JSON("static answer")} },
		func(v *p.FastProgram) { v.Steps[1].Value = p.FastValue{Ref: "input"} },
		func(v *p.FastProgram) {
			v.Steps[0] = p.FastInstruction{Op: "branch", Value: p.FastValue{Ref: "input"}, Equals: p.JSON(true), Then: 0, Else: 1}
		},
	} {
		var clone p.FastProgram
		if err := json.Unmarshal([]byte(before), &clone); err != nil {
			t.Fatal(err)
		}
		update(&clone)
		if err := ValidateFastProgram(clone); err == nil {
			t.Fatal("unsafe program accepted")
		}
	}
}
