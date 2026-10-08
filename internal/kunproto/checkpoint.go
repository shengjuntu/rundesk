package kunproto

// A selection is bound to one worker lifetime and the latest terminal revision.
// It cannot request historical rollback or replace the run's input/configuration.
type CheckpointSelection struct {
	SourceRunID      string `json:"sourceRunId"`
	Sequence         int64  `json:"sequence"`
	ExpectedRevision int64  `json:"expectedStateRevision"`
	WorkerEpoch      string `json:"workerEpoch"`
}
type RunManifest struct {
	EngineVersion   string `json:"engineVersion"`
	Workspace       string `json:"workspace"`
	ConfigHash      string `json:"configHash"`
	MCPHash         string `json:"mcpHash"`
	SkillsHash      string `json:"skillsHash"`
	HarnessHash     string `json:"harnessHash"`
	ContextRevision string `json:"contextRevision"`
}
type CheckpointCheck struct {
	Eligible  bool                `json:"eligible"`
	Reason    string              `json:"reason"`
	Selection CheckpointSelection `json:"selection"`
	Phase     string              `json:"phase,omitempty"`
	Step      int                 `json:"step"`
	Pending   int                 `json:"pending"`
	Budget    BudgetUsage         `json:"budget"`
	Skills    []Skill             `json:"-"`
}
