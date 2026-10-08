// Package kunproto is the serializable contract between RunDesk and Kun.
package kunproto

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const Version = 4
const EngineVersion = "0.4.0"
const MaxMessage = 8 << 20

type Envelope struct {
	Version int             `json:"version"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
	Event   *Event          `json:"event,omitempty"`
}
type Config struct {
	Budget           BudgetLimits `json:"budget"`
	Kind             string       `json:"kind"`
	Endpoint         string       `json:"endpoint"`
	Model            string       `json:"model"`
	APIKeyEnv        string       `json:"apiKeyEnv,omitempty"`
	SystemPrompt     string       `json:"systemPrompt,omitempty"`
	MaxSteps         int          `json:"maxSteps"`
	TimeoutSeconds   int          `json:"timeoutSeconds"`
	AllowWrite       bool         `json:"allowWrite"`
	PauseBeforeModel bool         `json:"pauseBeforeModel"`
}

func (c Config) Normalized() Config {
	c.Budget = c.Budget.Normalized()
	if c.Kind == "" {
		c.Kind = "codex"
	}
	if c.MaxSteps == 0 {
		c.MaxSteps = 20
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 120
	}
	return c
}
func (c Config) Validate() error {
	c = c.Normalized()
	if c.Kind != "codex" && c.Kind != "kun" {
		return fmt.Errorf("runtime kind must be codex or kun")
	}
	if c.Kind == "codex" {
		return nil
	}
	if err := c.Budget.Validate(); err != nil {
		return err
	}
	u, e := url.Parse(c.Endpoint)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("Kun endpoint must be an HTTP(S) base URL without credentials or query")
	}
	if strings.TrimSpace(c.Model) == "" || len(c.Model) > 160 {
		return fmt.Errorf("Kun model is required")
	}
	if c.APIKeyEnv != "" && !regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]{0,127}$").MatchString(c.APIKeyEnv) {
		return fmt.Errorf("invalid API key environment variable name")
	}
	if c.MaxSteps < 1 || c.MaxSteps > 100 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 || len(c.SystemPrompt) > 65536 {
		return fmt.Errorf("invalid Kun budget or system prompt")
	}
	return nil
}

type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type Skill struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}
type Start struct {
	Resume          *CheckpointSelection `json:"resume,omitempty"`
	ContextRevision string               `json:"contextRevision,omitempty"`
	MCP             []MCPServer          `json:"mcp,omitempty"`
	ApprovalPolicy  string               `json:"approvalPolicy,omitempty"`
	SessionID       string               `json:"sessionId"`
	RunID           string               `json:"runId"`
	Input           string               `json:"input"`
	Workspace       string               `json:"workspace"`
	Config          Config               `json:"config"`
	APIKey          string               `json:"apiKey,omitempty"`
	Skills          []Skill              `json:"skills,omitempty"`
}
type Control struct {
	CallID           string `json:"callId,omitempty"`
	RequestID        string `json:"requestId"`
	RunID            string `json:"runId"`
	ExpectedRevision int64  `json:"expectedStateRevision"`
	Operation        string `json:"operation"`
	Text             string `json:"text,omitempty"`
}
type Receipt struct {
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	Revision  int64  `json:"revision"`
}
type State struct {
	Manifest        *RunManifest           `json:"manifest,omitempty"`
	ResumedFrom     *CheckpointSelection   `json:"resumedFrom,omitempty"`
	Harness         Harness                `json:"harness"`
	Modules         map[string]ModuleState `json:"modules,omitempty"`
	Budget          BudgetUsage            `json:"budget"`
	ToolDefinitions []json.RawMessage      `json:"toolDefinitions,omitempty"`
	MCP             []MCPStatus            `json:"mcp,omitempty"`
	MCPTools        []MCPTool              `json:"mcpTools,omitempty"`
	Approval        *ToolApproval          `json:"approval,omitempty"`
	ApprovalPolicy  string                 `json:"approvalPolicy,omitempty"`
	Queued          []Control              `json:"queuedControls,omitempty"`
	Schema          int                    `json:"schemaVersion"`
	SessionID       string                 `json:"sessionId"`
	RunID           string                 `json:"runId"`
	Revision        int64                  `json:"revision"`
	Status          string                 `json:"status"`
	Phase           string                 `json:"phase"`
	Step            int                    `json:"step"`
	Messages        []Message              `json:"messages"`
	Pending         []ToolCall             `json:"pending,omitempty"`
	Actions         map[string]string      `json:"actions,omitempty"`
	Config          Config                 `json:"config"`
	Skills          []Skill                `json:"skills,omitempty"`
	Error           string                 `json:"error,omitempty"`
}
type Event struct {
	Sequence  int64           `json:"sequence"`
	SessionID string          `json:"sessionId"`
	RunID     string          `json:"runId"`
	Revision  int64           `json:"revision"`
	Time      string          `json:"time"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}
type Snapshot struct {
	Sequence int64 `json:"sequence"`
	State    State `json:"state"`
}

func Active(status string) bool {
	return status == "running" || status == "paused" || status == "pausing"
}
func JSON(v any) json.RawMessage {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
