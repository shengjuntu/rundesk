package kunproto

import "encoding/json"

// Fast IR is data, never an executable script. All branches move forward.
type FastValue struct {
	Ref     string          `json:"ref,omitempty"`
	Literal json.RawMessage `json:"literal,omitempty"`
}
type FastInstruction struct {
	Op      string               `json:"op"`
	Store   string               `json:"store,omitempty"`
	Tool    string               `json:"tool,omitempty"`
	Args    map[string]FastValue `json:"args,omitempty"`
	Value   FastValue            `json:"value,omitempty"`
	Pointer string               `json:"pointer,omitempty"`
	Schema  json.RawMessage      `json:"schema,omitempty"`
	Equals  json.RawMessage      `json:"equals,omitempty"`
	Then    int                  `json:"then,omitempty"`
	Else    int                  `json:"else,omitempty"`
}
type FastGuard struct {
	ContextRevision string `json:"contextRevision"`
	HarnessHash     string `json:"harnessHash"`
	EngineVersion   string `json:"engineVersion"`
	ConfigHash      string `json:"configHash"`
	SkillsHash      string `json:"skillsHash"`
	MCPHash         string `json:"mcpHash"`
	Workspace       string `json:"workspace"`
	CatalogHash     string `json:"catalogHash"`
	ApprovalPolicy  string `json:"approvalPolicy"`
}
type FastProgram struct {
	Schema      int               `json:"schema"`
	Domain      string            `json:"domain"`
	InputSchema json.RawMessage   `json:"inputSchema"`
	Guard       FastGuard         `json:"guard"`
	Steps       []FastInstruction `json:"steps"`
}
type FastStart struct {
	CandidateID   string          `json:"candidateId"`
	CandidateHash string          `json:"candidateHash"`
	Program       FastProgram     `json:"program"`
	Input         json.RawMessage `json:"input"`
}
type FastResult struct {
	CallID string `json:"callId"`
	Output string `json:"output"`
}
type FastState struct {
	FastStart
	Status       string                     `json:"status"`
	Reason       string                     `json:"reason,omitempty"`
	PC           int                        `json:"pc"`
	Values       map[string]json.RawMessage `json:"values"`
	Evidence     map[string]bool            `json:"evidence"`
	PendingStore string                     `json:"pendingStore,omitempty"`
	Completed    map[string]FastResult      `json:"completed"`
}
