package skillcheck

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/skillsafety"
)

// The source shape is pinned to alibaba/skill-up v0.15.0:
// internal/report/reporter.go, internal/judge/judge.go and expect.go. Prompt,
// response, turn results, arbitrary configuration fields and artifact paths are
// intentionally absent from these types and can never reach a normalized result.
type sourceReport struct {
	SkillName              string              `json:"skill_name"`
	SchemaVersion          string              `json:"schema_version"`
	EngineName             string              `json:"engine_name"`
	ModelName              string              `json:"model_name"`
	RequestedConfiguration *AgentConfiguration `json:"requested_configuration"`
	AppliedConfiguration   *AgentConfiguration `json:"applied_configuration"`
	ObservedConfiguration  *AgentConfiguration `json:"observed_configuration"`
	StartTime              string              `json:"start_time"`
	EndTime                string              `json:"end_time"`
	CaseResults            []sourceCase        `json:"case_results"`
}

type sourceCase struct {
	CaseID          string          `json:"case_id"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	DurationMS      int64           `json:"duration_ms"`
	Error           string          `json:"error"`
	Configuration   string          `json:"configuration"`
	Grading         *sourceGrading  `json:"grading"`
	Gates           *sourceGates    `json:"gates"`
	Judges          []sourceOutcome `json:"judge_results"`
	Prompt          *string         `json:"prompt"`
	Turns           []sourceTurn    `json:"turn_results"`
	ObservedModel   string          `json:"observed_model"`
	ObservedVersion string          `json:"observed_version"`
}

type sourceTurn struct {
	Number  int     `json:"turn_number"`
	Content *string `json:"content"`
	Source  string  `json:"source"`
}

type sourceGrading struct {
	Status      string `json:"status"`
	SkipReason  string `json:"skip_reason"`
	ErrorReason string `json:"error_reason"`
	Assertions  []struct {
		Text     string `json:"text"`
		Passed   *bool  `json:"passed"`
		Skipped  bool   `json:"skipped"`
		Evidence string `json:"evidence"`
	} `json:"assertion_results"`
}

type sourceGates struct {
	Passed   *bool `json:"passed"`
	Failures []struct {
		Rule   string `json:"rule"`
		Detail string `json:"detail"`
	} `json:"failures"`
	Rules []string `json:"configured_rules"`
}

type sourceOutcome struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Status     string         `json:"status"`
	Error      string         `json:"error"`
	SkipReason string         `json:"skip_reason"`
	Result     *sourceGrading `json:"result"`
}

func clean(value string) string { return skillsafety.Redact(value) }

func validID(value string) bool {
	if value == "" || len(value) > 512 || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func configuration(value *AgentConfiguration) *AgentConfiguration {
	if value == nil {
		return nil
	}
	return &AgentConfiguration{clean(value.Role), clean(value.Engine), clean(value.Protocol), clean(value.Provider), clean(value.Model), clean(value.Version)}
}

func knownIdentity(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unknown", "unset", "<unset>":
		return false
	default:
		return true
	}
}

func identity(value sourceReport) (string, bool) {
	// Only the documented credential-free fields participate. Hash the original
	// values before display redaction so two different secrets cannot collapse
	// to an apparently equal configuration, and never store those originals.
	identity := struct {
		Schema, Engine, Model        string
		Requested, Applied, Observed *AgentConfiguration
	}{value.SchemaVersion, value.EngineName, value.ModelName, value.RequestedConfiguration, value.AppliedConfiguration, value.ObservedConfiguration}
	encoded, _ := json.Marshal(identity)
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:]), knownIdentity(value.SchemaVersion) && knownIdentity(value.EngineName) && knownIdentity(value.ModelName)
}

// ParseReport accepts only a bounded result.json object. Imported status remains
// declared evidence, not proof that a skill, engine or dataset was independently
// executed. Missing, skipped or contradictory outcomes cannot become pass.
func ParseReport(data []byte) (Report, error) {
	result := Report{Status: "incomplete", Cases: []Case{}, Warnings: []string{}}
	if len(data) == 0 || len(data) > MaxReportBytes || !utf8.Valid(data) {
		return result, failure("report_size", "Skill-Up report is empty or exceeds 4 MiB")
	}
	if err := boundedJSON(data); err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var source sourceReport
	if err := decoder.Decode(&source); err != nil {
		return result, failure("invalid_report", "Skill-Up report must be a valid result.json object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, failure("invalid_report", "Skill-Up report must contain exactly one JSON object")
	}
	if source.SkillName == "" || len(source.CaseResults) == 0 || len(source.CaseResults) > MaxCases {
		return result, failure("invalid_report", "Skill-Up report requires a skill name and 1–1000 case results")
	}
	result.SkillName, result.SchemaVersion = clean(source.SkillName), clean(source.SchemaVersion)
	result.EngineName, result.ModelName = clean(source.EngineName), clean(source.ModelName)
	result.RequestedConfiguration = configuration(source.RequestedConfiguration)
	result.AppliedConfiguration = configuration(source.AppliedConfiguration)
	result.ObservedConfiguration = configuration(source.ObservedConfiguration)
	result.ConfigurationID, result.IdentityComplete = identity(source)
	if !result.IdentityComplete {
		result.Warnings = append(result.Warnings, "Engine, model or schema identity is missing; regression comparability is unknown.")
	}
	if source.RequestedConfiguration == nil && source.AppliedConfiguration == nil {
		result.Warnings = append(result.Warnings, "Configuration snapshots are absent; identity covers only the declared schema, engine and model.")
	}
	metadataComplete := source.SchemaVersion == "v1alpha1"
	if !metadataComplete {
		result.Warnings = append(result.Warnings, "Unsupported or missing Skill-Up schema; outcomes are retained without a passing verdict.")
	}
	start, startErr := time.Parse(time.RFC3339Nano, source.StartTime)
	end, endErr := time.Parse(time.RFC3339Nano, source.EndTime)
	if startErr == nil && endErr == nil && !end.Before(start) {
		result.StartedAt, result.FinishedAt = start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)
	} else {
		result.Warnings = append(result.Warnings, "Run timestamps are missing or inconsistent.")
	}
	keys := map[string]bool{}
	for _, entry := range source.CaseResults {
		if !validID(entry.CaseID) {
			return result, failure("invalid_case_id", "Skill-Up report contains an invalid or unbounded case ID")
		}
		variant := entry.Configuration
		if variant == "" {
			variant = "default"
		}
		out := Case{CaseID: clean(entry.CaseID), Title: clean(entry.Title), Variant: clean(variant), Status: entry.Status, DurationMS: entry.DurationMS, Error: clean(entry.Error), Assertions: []Assertion{}}
		key := caseKey(out.CaseID, out.Variant)
		if keys[key] {
			return result, failure("duplicate_case", "Skill-Up report repeats a case ID and configuration variant")
		}
		keys[key] = true
		if !knownStatus(out.Status) || variant != "default" && variant != "with_skill" && variant != "without_skill" || entry.DurationMS < 0 || entry.DurationMS > 7*24*60*60*1000 {
			out.Status = "UNKNOWN"
			result.Warnings = append(result.Warnings, "A case contains an unknown status, variant or invalid duration.")
		}
		if out.DurationMS < 0 || out.DurationMS > 7*24*60*60*1000 {
			out.DurationMS = 0
		}
		contradiction := false
		appendGrading(&out, entry.Grading, "grading", &contradiction)
		if entry.Gates != nil {
			gate := entry.Gates
			if gate.Passed == nil || len(gate.Failures) > 0 && *gate.Passed {
				contradiction = true
			}
			for _, rule := range gate.Rules {
				passed := true
				evidence := []string{}
				for _, failed := range gate.Failures {
					if failed.Rule == rule {
						passed = false
						evidence = append(evidence, failed.Detail)
					}
				}
				out.Assertions = append(out.Assertions, Assertion{Text: clean(rule), Passed: passed, Evidence: clean(strings.Join(evidence, "; ")), Source: "gates"})
			}
			if gate.Passed != nil && !*gate.Passed && out.Status == "PASS" {
				contradiction = true
			}
		}
		for _, judge := range entry.Judges {
			if !knownStatus(judge.Status) || out.Status == "PASS" && (judge.Status != "PASS" || judge.Error != "") {
				contradiction = true
			}
			appendGrading(&out, judge.Result, "judge:"+clean(judge.ID), &contradiction)
		}
		if len(out.Assertions) > 1000 {
			return result, failure("assertion_limit", "Skill-Up report has too many assertions in one case")
		}
		out.InputID = inputIdentity(entry)
		if entry.ObservedModel != "" || entry.ObservedVersion != "" {
			data, _ := json.Marshal(struct{ Model, Version string }{entry.ObservedModel, entry.ObservedVersion})
			hash := sha256.Sum256(data)
			out.RuntimeID = "sha256:" + hex.EncodeToString(hash[:])
		}
		if out.Status == "PASS" {
			checked := 0
			for _, assertion := range out.Assertions {
				if assertion.Skipped {
					continue
				}
				checked++
				if !assertion.Passed {
					contradiction = true
				}
			}
			if checked == 0 || contradiction || entry.Error != "" {
				out.Status = "UNKNOWN"
				result.Warnings = append(result.Warnings, "A declared PASS has no completed assertions or contradicts its error/judge evidence.")
			}
		}
		result.Cases = append(result.Cases, out)
	}
	result.Summary, metadataComplete = summarize(result.Cases, metadataComplete)
	result.Status = verdict(result.Summary, metadataComplete)
	// Avoid one warning per case becoming an unbounded secondary report.
	result.Warnings = uniqueWarnings(result.Warnings)
	return result, nil
}

// Bound JSON structure before typed decoding, preventing a small encoded array
// of millions of empty objects from causing a much larger typed allocation.
// Duplicate keys are also rejected instead of silently accepting the last one.
func boundedJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	tokens := 0
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 || tokens > 50000 {
			return failure("report_structure", "Skill-Up report exceeds its structural budget")
		}
		token, err := decoder.Token()
		if err != nil {
			return failure("invalid_report", "Skill-Up report contains invalid JSON")
		}
		tokens++
		delim, structured := token.(json.Delim)
		if !structured {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				key, ok := token.(string)
				if err != nil || !ok || keys[key] {
					return failure("invalid_report", "Skill-Up report contains an invalid or duplicate JSON field")
				}
				keys[key] = true
				tokens++
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return failure("invalid_report", "Skill-Up report contains invalid JSON delimiters")
		}
		if _, err := decoder.Token(); err != nil {
			return failure("invalid_report", "Skill-Up report contains incomplete JSON")
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return failure("invalid_report", "Skill-Up report must contain exactly one JSON object")
	}
	return nil
}

func appendGrading(out *Case, grading *sourceGrading, source string, contradiction *bool) {
	if grading == nil {
		return
	}
	if !knownStatus(grading.Status) || out.Status == "PASS" && (grading.Status != "PASS" || grading.ErrorReason != "") {
		*contradiction = true
	}
	for _, assertion := range grading.Assertions {
		if assertion.Passed == nil || strings.TrimSpace(assertion.Text) == "" {
			*contradiction = true
		}
		out.Assertions = append(out.Assertions, Assertion{Text: clean(assertion.Text), Passed: assertion.Passed != nil && *assertion.Passed, Skipped: assertion.Skipped, Evidence: clean(assertion.Evidence), Source: source})
	}
}

func inputIdentity(entry sourceCase) string {
	if len(entry.Turns) > 0 {
		for index, turn := range entry.Turns {
			if turn.Number != index+1 || turn.Content == nil || strings.TrimSpace(*turn.Content) == "" {
				return ""
			}
		}
	} else if entry.Prompt == nil || strings.TrimSpace(*entry.Prompt) == "" {
		return ""
	}
	rules := []string{}
	grading := func(value *sourceGrading, source string) {
		if value == nil {
			return
		}
		for _, assertion := range value.Assertions {
			if strings.TrimSpace(assertion.Text) != "" {
				rules = append(rules, source+"\x00"+assertion.Text)
			}
		}
	}
	grading(entry.Grading, "grading")
	if entry.Gates != nil {
		for _, rule := range entry.Gates.Rules {
			if strings.TrimSpace(rule) != "" {
				rules = append(rules, "gates\x00"+rule)
			}
		}
	}
	for _, judge := range entry.Judges {
		grading(judge.Result, "judge:"+judge.ID+":"+judge.Type)
	}
	if len(rules) == 0 {
		return ""
	}
	sort.Strings(rules)
	// The upstream top-level prompt contains only the first turn for a
	// multi-turn run. Hash every actual input turn, excluding all response and
	// outcome fields; changing a later turn must make a pair incomparable.
	var prompt *string
	if len(entry.Turns) == 0 {
		prompt = entry.Prompt
	}
	data, _ := json.Marshal(struct {
		Prompt *string
		Turns  []sourceTurn
		Rules  []string
	}{prompt, entry.Turns, rules})
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func knownStatus(status string) bool {
	return status == "PASS" || status == "FAIL" || status == "ERROR" || status == "SKIP"
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != 71 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func uniqueWarnings(values []string) []string {
	keys := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if !keys[value] {
			keys[value] = true
			out = append(out, value)
		}
	}
	return out
}

func summarize(cases []Case, complete bool) (Summary, bool) {
	primary := map[string]Case{}
	for _, item := range cases {
		previous, exists := primary[item.CaseID]
		if !exists || item.Variant == "with_skill" || previous.Variant == "without_skill" {
			primary[item.CaseID] = item
		}
	}
	result := Summary{Total: len(primary)}
	for _, item := range primary {
		if item.Variant == "without_skill" {
			complete = false
		}
		result.DurationMS += item.DurationMS
		switch item.Status {
		case "PASS":
			result.Pass++
		case "FAIL":
			result.Fail++
		case "ERROR":
			result.Error++
		case "SKIP":
			result.Skip++
		default:
			result.Unknown++
		}
	}
	return result, complete
}

func verdict(summary Summary, complete bool) string {
	if summary.Error > 0 {
		return "error"
	}
	if summary.Fail > 0 {
		return "fail"
	}
	if !complete || summary.Total == 0 || summary.Skip > 0 || summary.Unknown > 0 {
		return "incomplete"
	}
	return "pass"
}

// Compare never calls a tool or assumes missing measurements are passes. It is
// deliberately stricter than Skill-Up's default comparison: engine/model/schema
// or declared configuration changes make every pair incomparable.
func Compare(before, after Report) Comparison {
	result := Comparison{Comparable: true, Verdict: "incomplete", Cases: []CaseChange{}, Warnings: []string{}}
	if !before.IdentityComplete || !after.IdentityComplete || !validDigest(before.ConfigurationID) || before.ConfigurationID != after.ConfigurationID || before.SkillName == "" || before.SkillName != after.SkillName || before.SchemaVersion != "v1alpha1" || after.SchemaVersion != "v1alpha1" {
		result.Comparable = false
		result.Reason = "Skill, engine, model, schema or declared configuration identities differ or are unknown."
	}
	if result.Comparable && (!validDigest(before.SuiteDigest) || before.SuiteDigest != after.SuiteDigest) {
		result.Comparable = false
		result.Reason = "The same frozen test-suite inputs and rule implementations have not been verified. Imported report labels cannot establish this identity."
	}
	old, latest, keys := map[string]Case{}, map[string]Case{}, map[string]bool{}
	for _, item := range before.Cases {
		key := caseKey(item.CaseID, item.Variant)
		old[key], keys[key] = item, true
	}
	for _, item := range after.Cases {
		key := caseKey(item.CaseID, item.Variant)
		latest[key], keys[key] = item, true
	}
	order := make([]string, 0, len(keys))
	for key := range keys {
		order = append(order, key)
	}
	sort.Strings(order)
	for _, key := range order {
		previous, hadBefore := old[key]
		current, hasAfter := latest[key]
		row := CaseChange{CaseID: previous.CaseID, Variant: previous.Variant, BeforeStatus: previous.Status, AfterStatus: current.Status, Change: "unknown"}
		if !hadBefore {
			row.CaseID, row.Variant = current.CaseID, current.Variant
		}
		switch {
		case !result.Comparable:
			row.Change = "unknown"
			row.Reason = result.Reason
		case !hadBefore:
			row.Change = "new"
		case !hasAfter:
			row.Change = "removed"
		case !validDigest(previous.InputID) || !validDigest(current.InputID) || previous.InputID != current.InputID:
			row.Reason = "Declared test input or assertion rule identity differs or is unavailable."
		case previous.RuntimeID != current.RuntimeID:
			row.Reason = "Explicitly observed case model/runtime identity differs or is unavailable on one side."
		case !knownStatus(previous.Status) || !knownStatus(current.Status) || previous.Status == "SKIP" || current.Status == "SKIP":
			row.Change = "unknown"
		case previous.Status == "PASS" && current.Status != "PASS":
			row.Change = "regressed"
		case previous.Status != "PASS" && current.Status == "PASS":
			row.Change = "fixed"
		case previous.Status == current.Status:
			row.Change = "unchanged"
		}
		result.Cases = append(result.Cases, row)
		switch row.Change {
		case "fixed":
			result.Summary.Fixed++
		case "regressed":
			result.Summary.Regressed++
		case "unchanged":
			result.Summary.Unchanged++
		case "new":
			result.Summary.New++
		case "removed":
			result.Summary.Removed++
		default:
			result.Summary.Unknown++
		}
	}
	if !result.Comparable || len(result.Cases) == 0 || result.Summary.New > 0 || result.Summary.Removed > 0 || result.Summary.Unknown > 0 || after.Status == "incomplete" {
		result.Verdict = "incomplete"
	} else if result.Summary.Regressed > 0 || after.Status == "fail" || after.Status == "error" {
		result.Verdict = "fail"
	} else if after.Status == "pass" {
		result.Verdict = "pass"
	}
	if result.Summary.New > 0 || result.Summary.Removed > 0 {
		result.Warnings = append(result.Warnings, "Case coverage changed; new or removed cases are not evidence of a passing regression.")
	}
	return result
}
