package skillsafety

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func spectorJSON(recommendation string, complete, successful any) []byte {
	value := map[string]any{
		"risk_assessment":       map[string]any{"recommendation": recommendation},
		"metadata":              map[string]any{"skillspector_version": "test-version"},
		"analysis_completeness": map[string]any{},
		"issues":                []any{},
		"components":            []any{map[string]any{"path": "SKILL.md"}},
	}
	if complete != nil {
		value["analysis_completeness"] = map[string]any{"is_complete": complete}
	}
	if successful != nil {
		value["execution_successful"] = successful
	}
	data, _ := json.Marshal(value)
	return data
}

func TestSpectorInterpretationPreservesRiskAndCompleteness(t *testing.T) {
	tests := []struct {
		name                           string
		recommendation                 string
		complete, successful           any
		exit                           int
		wantStatus, wantRecommendation string
		wantError                      bool
	}{
		{"complete clean", "SAFE", true, true, 0, "completed", "no_findings", false},
		{"caution is not clean", "CAUTION", true, true, 0, "completed", "review", false},
		{"high risk is completed", "DO_NOT_INSTALL", true, true, 1, "completed", "review", false},
		{"exit one no silent clean", "SAFE", true, true, 1, "completed", "review", false},
		{"incomplete flag", "SAFE", false, true, 1, "incomplete", "incomplete", false},
		{"missing completeness", "SAFE", nil, true, 0, "incomplete", "incomplete", false},
		{"missing execution", "SAFE", true, nil, 0, "incomplete", "incomplete", false},
		{"failed execution", "SAFE", true, false, 0, "error", "incomplete", true},
		{"cli technical error", "SAFE", true, true, 2, "error", "incomplete", true},
		{"unknown risk", "MAYBE", true, true, 0, "error", "incomplete", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report, err := parseSpectorReport(spectorJSON(test.recommendation, test.complete, test.successful), test.exit, false)
			if report.Status != test.wantStatus || report.Recommendation != test.wantRecommendation || (err != nil) != test.wantError {
				t.Fatalf("report=%+v error=%v", report, err)
			}
			if len(report.Raw) != 0 {
				t.Fatal("raw upstream content must not be exposed")
			}
		})
	}
}

func TestSpectorEvidenceRedactionAndIssueMapping(t *testing.T) {
	var value map[string]any
	_ = json.Unmarshal(spectorJSON("SAFE", true, true), &value)
	value["issues"] = []any{map[string]any{
		"id": "one", "rule_id": "SC_TEST", "severity": "HIGH", "category": "credentials",
		"message": "Reading credential", "evidence": "token = 'github_pat_1234567890ABCDEFGHIJKLMN'", "remediation": "Remove token",
		"location": map[string]any{"file": "scripts/run.py", "start_line": 8},
	}}
	data, _ := json.Marshal(value)
	report, err := parseSpectorReport(data, 0, false)
	if err != nil || report.Recommendation != "review" || len(report.Findings) != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	finding := report.Findings[0]
	if finding.File != "scripts/run.py" || finding.Line != 8 || finding.RuleID != "SC_TEST" || finding.Severity != "high" {
		t.Fatalf("finding=%+v", finding)
	}
	if strings.Contains(finding.Evidence, "github_pat_") || !strings.Contains(finding.Evidence, "REDACTED") {
		t.Fatal("raw token in report evidence")
	}
}

func TestSpectorCurrentUpstreamFindingAndLedgerFields(t *testing.T) {
	var value map[string]any
	_ = json.Unmarshal(spectorJSON("CAUTION", true, true), &value)
	value["analysis_completeness"] = map[string]any{"is_complete": true, "status": "complete", "fully_inspected_files": 2, "partially_inspected_files": 0, "entirely_uninspected_files": 0, "coverage_percent": 100}
	value["issues"] = []any{map[string]any{
		"id": "SC001", "finding_id": "stable-id", "severity": "MEDIUM",
		"explanation": "Actual upstream explanation", "code_snippet": "cat ~/.ssh/id_rsa", "evidence": map[string]any{},
		"location": map[string]any{"file": "SKILL.md", "start_line": 12},
	}}
	data, _ := json.Marshal(value)
	report, err := parseSpectorReport(data, 0, false)
	if err != nil || report.Status != "completed" || report.Coverage.FilesSeen != 2 || report.Coverage.FilesScanned != 2 {
		t.Fatalf("%+v %v", report, err)
	}
	finding := report.Findings[0]
	if finding.ID != "stable-id" || finding.RuleID != "SC001" || finding.Reason != "Actual upstream explanation" || finding.Evidence != "cat ~/.ssh/id_rsa" {
		t.Fatalf("%+v", finding)
	}
	value["analysis_completeness"] = map[string]any{"is_complete": false, "fully_inspected_files": 1, "partially_inspected_files": 1, "entirely_uninspected_files": 1}
	data, _ = json.Marshal(value)
	report, _ = parseSpectorReport(data, 1, false)
	if report.Status != "incomplete" || report.Coverage.FilesSkipped != 2 || report.Coverage.FilesSeen != 3 {
		t.Fatalf("%+v", report)
	}
}

func TestSpectorInvalidJSONDoesNotBecomePass(t *testing.T) {
	for _, data := range []string{"", "logs before {}", "null", "{}", "{broken", "{} trailing"} {
		report, err := parseSpectorReport([]byte(data), 0, false)
		if err == nil || report.Status != "error" || report.Recommendation != "incomplete" {
			t.Fatalf("input=%q report=%+v err=%v", data, report, err)
		}
	}
}

func TestSpectorContradictoryAndLLMCompleteness(t *testing.T) {
	var value map[string]any
	_ = json.Unmarshal(spectorJSON("SAFE", true, true), &value)
	value["analysis_completeness"] = map[string]any{"is_complete": true, "status": "partial"}
	data, _ := json.Marshal(value)
	report, _ := parseSpectorReport(data, 0, false)
	if report.Status != "incomplete" {
		t.Fatal("partial status cannot be overridden by is_complete")
	}
	value["analysis_completeness"] = map[string]any{"status": "complete"}
	value["metadata"] = map[string]any{"llm_requested": true, "llm_available": false, "llm_error": "provider absent"}
	data, _ = json.Marshal(value)
	report, _ = parseSpectorReport(data, 0, true)
	if report.Status != "incomplete" {
		t.Fatal("requested but unavailable model analysis is incomplete")
	}
	value["metadata"] = map[string]any{"llm_requested": false, "llm_available": true}
	data, _ = json.Marshal(value)
	report, _ = parseSpectorReport(data, 0, true)
	if report.Status != "incomplete" {
		t.Fatal("requested model mode was not actually selected")
	}
	value["metadata"] = map[string]any{"llm_requested": true, "llm_available": true}
	data, _ = json.Marshal(value)
	report, _ = parseSpectorReport(data, 0, true)
	if report.Status != "completed" {
		t.Fatal("explicit complete requested model mode rejected")
	}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func writeScanner(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable fixture")
	}
	path := filepath.Join(t.TempDir(), "fake-scanner")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSpectorRunsVectorArgumentsAndCapturesJSON(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "skill ; shell syntax")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	argsPath := filepath.Join(t.TempDir(), "arguments")
	data := string(spectorJSON("SAFE", true, true))
	command := writeScanner(t, "printf '%s\\n' \"$@\" > "+shellQuote(argsPath)+"\nprintf '%s' "+shellQuote(data))
	report, err := RunSkillSpector(context.Background(), directory, command, false)
	if err != nil || report.Status != "completed" || report.EngineVersion != "test-version" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	arguments, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "scan\n" + directory + "\n--format\njson\n--fail-on-incomplete\n--no-llm\n"
	if string(arguments) != want {
		t.Fatalf("arguments=%q want=%q", arguments, want)
	}
	if !Probe(command).Installed {
		t.Fatal("fake executable should be installed")
	}
}

func TestSpectorUninstalledAndInvalidDirectory(t *testing.T) {
	report, err := RunSkillSpector(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "missing"), false)
	if err == nil || report.Status != "unavailable" || Probe("/nonexistent/rundesk-skillspector").Installed {
		t.Fatalf("report=%+v error=%v", report, err)
	}
	command := writeScanner(t, "exit 0")
	report, err = RunSkillSpector(context.Background(), filepath.Join(t.TempDir(), "missing"), command, false)
	if err == nil || report.Status != "error" {
		t.Fatalf("report=%+v error=%v", report, err)
	}
}

func TestSpectorCancellationIsNotComplete(t *testing.T) {
	command := writeScanner(t, "exec sleep 10")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	report, err := RunSkillSpector(ctx, t.TempDir(), command, false)
	if err == nil || report.Status != "incomplete" || report.Recommendation != "incomplete" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestCappedOutputDrainsWithoutUnboundedRetention(t *testing.T) {
	output := &cappedOutput{limit: 4}
	if n, err := output.Write([]byte("1234567890")); n != 10 || err != nil {
		t.Fatal(n, err)
	}
	value, exceeded := output.snapshot()
	if string(value) != "1234" || !exceeded {
		t.Fatal(string(value), exceeded)
	}
	if n, err := output.Write([]byte("more")); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	value, _ = output.snapshot()
	if len(value) != 4 {
		t.Fatal("buffer grew beyond limit")
	}
}
