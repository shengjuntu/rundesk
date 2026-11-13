// Package skillcheck reads bounded Skill-Up v1alpha1 results and compares their
// declared outcomes. It does not run skills, Agents, judges or test suites.
package skillcheck

import "fmt"

const (
	MaxReportBytes = 4 << 20
	MaxCases       = 1000
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func failure(code, message string) error { return &Error{Code: code, Message: message} }

type AgentConfiguration struct {
	Role     string `json:"role,omitempty"`
	Engine   string `json:"engine,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Version  string `json:"version,omitempty"`
}

type Assertion struct {
	Text     string `json:"text"`
	Passed   bool   `json:"passed"`
	Skipped  bool   `json:"skipped"`
	Evidence string `json:"evidence,omitempty"`
	Source   string `json:"source"`
}

type Case struct {
	CaseID     string      `json:"caseId"`
	Title      string      `json:"title"`
	Variant    string      `json:"variant"`             // default, with_skill, without_skill
	InputID    string      `json:"inputId,omitempty"`   // declared prompt + assertion rule identity
	RuntimeID  string      `json:"runtimeId,omitempty"` // explicit per-case observed model/runtime version, when supplied
	Status     string      `json:"status"`              // PASS, FAIL, ERROR, SKIP, UNKNOWN
	DurationMS int64       `json:"durationMs"`
	Error      string      `json:"error,omitempty"`
	Assertions []Assertion `json:"assertions"`
}

type Summary struct {
	Total      int   `json:"total"`
	Pass       int   `json:"pass"`
	Fail       int   `json:"fail"`
	Error      int   `json:"error"`
	Skip       int   `json:"skip"`
	Unknown    int   `json:"unknown"`
	DurationMS int64 `json:"durationMs"`
}

type Report struct {
	SkillName              string              `json:"skillName"`
	SchemaVersion          string              `json:"schemaVersion"`
	EngineName             string              `json:"engineName"`
	ModelName              string              `json:"modelName"`
	RequestedConfiguration *AgentConfiguration `json:"requestedConfiguration,omitempty"`
	AppliedConfiguration   *AgentConfiguration `json:"appliedConfiguration,omitempty"`
	ObservedConfiguration  *AgentConfiguration `json:"observedConfiguration,omitempty"`
	ConfigurationID        string              `json:"configurationId,omitempty"`
	SuiteDigest            string              `json:"suiteDigest,omitempty"` // trusted caller-set frozen test inputs/rules; never read from imported JSON
	IdentityComplete       bool                `json:"identityComplete"`
	StartedAt              string              `json:"startedAt,omitempty"`
	FinishedAt             string              `json:"finishedAt,omitempty"`
	Status                 string              `json:"status"` // pass, fail, error, incomplete
	Cases                  []Case              `json:"cases"`
	Summary                Summary             `json:"summary"` // one primary result per case ID
	Warnings               []string            `json:"warnings"`
}

type CaseChange struct {
	CaseID       string `json:"caseId"`
	Variant      string `json:"variant"`
	BeforeStatus string `json:"beforeStatus,omitempty"`
	AfterStatus  string `json:"afterStatus,omitempty"`
	Change       string `json:"change"` // fixed, regressed, unchanged, new, removed, unknown
	Reason       string `json:"reason,omitempty"`
}

type ChangeSummary struct {
	Fixed     int `json:"fixed"`
	Regressed int `json:"regressed"`
	Unchanged int `json:"unchanged"`
	New       int `json:"new"`
	Removed   int `json:"removed"`
	Unknown   int `json:"unknown"`
}

type Comparison struct {
	Comparable bool          `json:"comparable"`
	Reason     string        `json:"reason,omitempty"`
	Verdict    string        `json:"verdict"` // pass, fail, incomplete
	Cases      []CaseChange  `json:"cases"`
	Summary    ChangeSummary `json:"summary"`
	Warnings   []string      `json:"warnings"`
}

type Validation struct {
	Status   string   `json:"status"` // success, failed, unavailable
	Cases    int      `json:"cases"`
	Output   string   `json:"output"`
	Warnings []string `json:"warnings"`
}

func caseKey(id, variant string) string { return fmt.Sprintf("%s\x00%s", id, variant) }
