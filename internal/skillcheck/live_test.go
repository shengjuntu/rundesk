package skillcheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in protocol verification only. The supplied suite must be an explicitly
// trusted fixture. Validate does not execute Agent/test/judge commands; its CLI
// loader may resolve YAML paths, so do not point this host test at a third-party
// untrusted suite. Production untrusted suites use the application's container.
func TestLiveOfficialValidationAndResultProtocol(t *testing.T) {
	binary, dir, reportFile := os.Getenv("RUNDESK_TEST_SKILLUP_BIN"), os.Getenv("RUNDESK_TEST_SKILLUP_FIXTURE_DIR"), os.Getenv("RUNDESK_TEST_SKILLUP_REPORT")
	if binary == "" || dir == "" || reportFile == "" {
		t.Skip("set all RUNDESK_TEST_SKILLUP_{BIN,FIXTURE_DIR,REPORT} variables for a controlled official CLI fixture")
	}
	t.Setenv("CUSTOM_AGENT_BIN", filepath.Join(dir, "agent.sh"))
	validation, err := Validate(context.Background(), dir, binary)
	if err != nil || validation.Status != "success" || validation.Cases <= 0 {
		t.Fatalf("real validate: %+v %v", validation, err)
	}
	data, err := os.ReadFile(reportFile)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ParseReport(data)
	if err != nil || len(report.Cases) == 0 {
		t.Fatalf("real report: %+v %v", report, err)
	}
	if report.SuiteDigest != "" || Compare(report, report).Verdict != "incomplete" {
		t.Fatal("external protocol result self-attested a passing regression")
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatal(err)
	}
	t.Logf("real CLI validated %d cases; report=%s/%s, model identity known=%v, input identity present=%v", validation.Cases, report.EngineName, report.Status, report.IdentityComplete, report.Cases[0].InputID != "")
}
