package kun

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

// Modules return data. Only Engine commits state, calls providers or dispatches tools.
// Inputs are owned copies; a module cannot mutate the engine's live state.
type Memory interface {
	Version() p.ModuleVersion
	Build(p.State) (ContextBundle, error)
}
type Planning interface {
	Version() p.ModuleVersion
	Plan(p.State) p.ModuleState
}
type Capability interface {
	Version() p.ModuleVersion
	Build(p.State) (*toolCatalog, error)
}
type Action interface {
	Version() p.ModuleVersion
	Validate(p.ToolCall, *toolCatalog) (toolIntent, error)
}
type LoopPolicy interface {
	Version() p.ModuleVersion
	Next(p.State) string
}
type Provider interface {
	Complete(context.Context, p.State, string) (completion, error)
}
type openAIProvider struct{}

func (openAIProvider) Complete(ctx context.Context, s p.State, key string) (completion, error) {
	return modelCall(ctx, s, key)
}

type ContextBundle struct {
	Messages []p.Message
	Tools    []json.RawMessage
	Bytes    int
}
type toolIntent struct {
	Name       string
	SchemaHash string
	MCP        *p.MCPTool
	schema     *argumentSchema
}
type toolCatalog struct {
	Definitions []json.RawMessage
	Tools       map[string]toolIntent
}
type fixedMemory struct{}
type noPlan struct{}
type fixedCapability struct{}
type schemaAction struct{}
type toolLoop struct{}

func moduleVersion(id string) p.ModuleVersion {
	return p.ModuleVersion{ID: id, Version: "1.0.0", StateSchemaVersion: 1}
}
func (fixedMemory) Version() p.ModuleVersion     { return moduleVersion("full-history-v1") }
func (noPlan) Version() p.ModuleVersion          { return moduleVersion("no-explicit-plan-v1") }
func (fixedCapability) Version() p.ModuleVersion { return moduleVersion("fixed-catalog-v1") }
func (schemaAction) Version() p.ModuleVersion    { return moduleVersion("schema-action-v1") }
func (toolLoop) Version() p.ModuleVersion        { return moduleVersion("tool-loop-v1") }
func (fixedMemory) Build(s p.State) (ContextBundle, error) {
	b := ContextBundle{Messages: s.Messages, Tools: s.ToolDefinitions}
	b.Bytes = len(p.JSON(requestBody(s)))
	if b.Bytes > 2<<20 {
		return b, fmt.Errorf("context exceeds 2 MiB; start a new session")
	}
	return b, nil
}
func (m noPlan) Plan(s p.State) p.ModuleState {
	return p.ModuleState{Implementation: m.Version(), Phase: s.Phase, Data: p.JSON(map[string]any{"status": "not_requested", "plan": nil, "reason": "The fixed tool loop does not request a separate planning model call."})}
}
func (toolLoop) Next(s p.State) string {
	if len(s.Pending) > 0 {
		return "before_tool"
	}
	for _, c := range s.Queued {
		if c.Operation == "steer" {
			return "before_model"
		}
	}
	if len(s.Messages) > 0 && s.Messages[len(s.Messages)-1].Role == "assistant" {
		return "complete"
	}
	return "before_model"
}
func (fixedCapability) Build(s p.State) (*toolCatalog, error) {
	c := &toolCatalog{Tools: map[string]toolIntent{}}
	for _, raw := range s.ToolDefinitions {
		var def struct {
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &def); err != nil {
			return nil, err
		}
		if def.Function.Name == "" {
			return nil, fmt.Errorf("missing tool name")
		}
		if _, exists := c.Tools[def.Function.Name]; exists {
			return nil, fmt.Errorf("duplicate tool definition %s", def.Function.Name)
		}
		schema, err := compileArguments(def.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("tool %s schema: %w", def.Function.Name, err)
		}
		intent := toolIntent{Name: def.Function.Name, SchemaHash: fingerprint(json.RawMessage(def.Function.Parameters)), schema: schema}
		for _, mcpTool := range s.MCPTools {
			if mcpTool.Alias == intent.Name {
				v := mcpTool
				intent.MCP = &v
				break
			}
		}
		c.Tools[intent.Name] = intent
		c.Definitions = append(c.Definitions, raw)
	}
	return c, nil
}
func (schemaAction) Validate(call p.ToolCall, catalog *toolCatalog) (toolIntent, error) {
	if catalog == nil {
		return toolIntent{}, fmt.Errorf("tool catalog unavailable")
	}
	intent, found := catalog.Tools[call.Function.Name]
	if !found {
		return toolIntent{}, fmt.Errorf("tool %s is not in this run's authorized catalog", call.Function.Name)
	}
	if err := intent.schema.Validate([]byte(call.Function.Arguments)); err != nil {
		return intent, fmt.Errorf("invalid tool arguments: %w", err)
	}
	return intent, nil
}

type modules struct {
	memory     Memory
	planning   Planning
	capability Capability
	action     Action
	policy     LoopPolicy
	provider   Provider
}

func defaultModules() modules {
	return modules{fixedMemory{}, noPlan{}, fixedCapability{}, schemaAction{}, toolLoop{}, openAIProvider{}}
}
func (m modules) harness() p.Harness {
	v := m.policy.Version()
	return p.Harness{ID: v.ID, Version: v.Version, Revision: 1, Modules: map[string]p.ModuleVersion{"memory": m.memory.Version(), "planning": m.planning.Version(), "action": m.action.Version(), "capability": m.capability.Version()}}
}
func (e *Engine) moduleState(name, phase string, data any) {
	e.state.Modules[name] = p.ModuleState{Implementation: e.state.Harness.Modules[name], Phase: phase, Data: p.JSON(data)}
}

type toolResult struct {
	Output  string
	Raw     json.RawMessage
	IsError bool
}

// This is the single side-effect gateway. Validation, approval, budget reservation,
// and the durable dispatched record must precede it in Engine.
func (e *Engine) executeAction(ctx context.Context, intent toolIntent, call p.ToolCall) (toolResult, error) {
	if intent.MCP != nil {
		out, raw, bad, err := e.callMCP(ctx, *intent.MCP, call)
		return toolResult{out, raw, bad}, err
	}
	out, err := executeTool(e.workspace, e.state.Config.AllowWrite, call)
	if err != nil {
		return toolResult{Output: err.Error(), IsError: true}, nil
	}
	return toolResult{Output: out}, nil
}
