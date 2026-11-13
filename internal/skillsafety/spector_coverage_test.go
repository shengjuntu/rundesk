package skillsafety

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSpectorNeverPromotesEmptyOrContradictoryCoverage(t *testing.T) {
	for name, ledger := range map[string]map[string]any{
		"zero files":                           {"is_complete": true, "files_seen": 0, "files_scanned": 0},
		"missing inspected count":              {"is_complete": true},
		"negative inspected count":             {"is_complete": true, "fully_inspected_files": -1},
		"negative ignored aggregate":           {"is_complete": true, "files_skipped": -1, "fully_inspected_files": 1},
		"partial hidden by zero aggregate":     {"is_complete": true, "files_skipped": 0, "fully_inspected_files": 1, "partially_inspected_files": 1},
		"uninspected hidden by zero aggregate": {"is_complete": true, "files_skipped": 0, "fully_inspected_files": 1, "entirely_uninspected_files": 1},
		"conflicting inspected counters":       {"is_complete": true, "files_scanned": 1, "fully_inspected_files": 2},
		"seen count smaller than inspected":    {"is_complete": true, "files_seen": 1, "fully_inspected_files": 2},
		"impossible percentage":                {"is_complete": true, "fully_inspected_files": 1, "coverage_percent": 101},
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(spectorJSON("SAFE", true, true), &value); err != nil {
				t.Fatal(err)
			}
			value["analysis_completeness"] = ledger
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			report, err := parseSpectorReport(data, 0, false)
			if err != nil || report.Status != "incomplete" || report.Recommendation != "incomplete" {
				t.Fatalf("contradictory coverage became a passing report: %+v %v", report, err)
			}
		})
	}
}

func TestSpectorCrossChecksReportedFilesAgainstActualDirectory(t *testing.T) {
	for _, test := range []struct {
		name       string
		paths      []string
		count      int
		wantStatus string
	}{
		{"matching relative files", []string{"SKILL.md", "helper.py"}, 2, "completed"},
		{"missing file", []string{"SKILL.md"}, 1, "incomplete"},
		{"count claims complete but file omitted", []string{"SKILL.md"}, 2, "incomplete"},
		{"duplicate paths", []string{"SKILL.md", "SKILL.md"}, 2, "incomplete"},
		{"escaping file", []string{"SKILL.md", "../helper.py"}, 2, "incomplete"},
		{"wrong second file", []string{"SKILL.md", "unrelated.py"}, 2, "incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"SKILL.md", "helper.py"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("static fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var value map[string]any
			_ = json.Unmarshal(spectorJSON("SAFE", true, true), &value)
			value["analysis_completeness"] = map[string]any{"is_complete": true, "fully_inspected_files": test.count}
			components := []any{}
			for _, name := range test.paths {
				components = append(components, map[string]any{"path": name})
			}
			value["components"] = components
			data, _ := json.Marshal(value)
			command := writeScanner(t, "printf '%s' "+shellQuote(string(data)))
			report, err := RunSkillSpector(context.Background(), dir, command, false)
			if err != nil || report.Status != test.wantStatus {
				t.Fatalf("report=%+v error=%v", report, err)
			}
		})
	}
}

func TestSpectorScopeExclusionsKeepTheActualFileAndReason(t *testing.T) {
	// This shape follows the real 2.13.4 binary fixture: the percentage only
	// covers selected components, while the scope ledger names omitted bytes.
	value := map[string]any{
		"execution_successful": true,
		"risk_assessment":      map[string]any{"recommendation": "SAFE"},
		"components":           []any{map[string]any{"path": "SKILL.md"}, map[string]any{"path": "assets/opaque.bin"}},
		"issues":               []any{},
		"analysis_completeness": map[string]any{
			"is_complete": true, "status": "complete", "coverage_percent": 100,
			"fully_inspected_files": 1, "partially_inspected_files": 0, "entirely_uninspected_files": 0,
			"ledger_exceptions": []any{map[string]any{"path": "assets/opaque.bin", "reason_code": "excluded_executable_content", "message": "Executable content was inventoried but excluded from content analysis."}},
			"scope_exclusions":  []any{map[string]any{"path": "assets/opaque.bin", "reason_code": "binary_content", "message": "Binary content is unsupported by this analyzer."}},
		},
	}
	data, _ := json.Marshal(value)
	report, err := parseSpectorReport(data, 0, false)
	if err != nil || report.Status != "incomplete" || report.Coverage.FilesSkipped != 1 || len(report.Coverage.Skipped) != 2 {
		t.Fatalf("report=%+v error=%v", report, err)
	}
	for _, gap := range report.Coverage.Skipped {
		if gap.File != "assets/opaque.bin" || gap.Reason == "" {
			t.Fatalf("lost source coverage evidence: %+v", gap)
		}
	}
}

func TestSpectorRefusesEmptyLinkedAndOversizeSourcesBeforeLaunching(t *testing.T) {
	command := writeScanner(t, "exit 99")
	for _, kind := range []string{"empty", "symlink", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			switch kind {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("private fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "SKILL.md")); err != nil {
					t.Skip(err)
				}
			case "oversize":
				file, err := os.Create(filepath.Join(dir, "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(MaxTotalBytes + 1)
				_ = file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			report, err := RunSkillSpector(context.Background(), dir, command, false)
			if err == nil || report.Status != "error" || report.Recommendation != "incomplete" {
				t.Fatalf("unsafe source accepted: %+v %v", report, err)
			}
		})
	}
}

func TestSpectorRejectsSameSizeSourceMutationDuringScan(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(name, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	command := writeScanner(t, "printf '%s' 'after!' > "+shellQuote(name)+"\nprintf '%s' "+shellQuote(string(spectorJSON("SAFE", true, true))))
	report, err := RunSkillSpector(context.Background(), dir, command, false)
	if err != nil || report.Status != "incomplete" || report.Recommendation != "incomplete" {
		t.Fatalf("same-size modified bytes retained a successful report: %+v %v", report, err)
	}
}
