package skillcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixtureCase(id, status string) map[string]any {
	return map[string]any{"case_id": id, "title": "Case " + id, "status": status, "duration_ms": 10, "prompt": "Summarize this fixed document.", "grading": map[string]any{"status": status, "assertion_results": []any{map[string]any{"text": "Contains a source citation", "passed": status == "PASS", "evidence": "bounded fixture"}}}}
}

func fixtureReport(cases ...map[string]any) map[string]any {
	return map[string]any{"skill_name": "research", "schema_version": "v1alpha1", "engine_name": "codex", "model_name": "provider/model", "requested_configuration": map[string]any{"role": "tested_agent", "engine": "codex", "protocol": "cli", "provider": "provider", "model": "model", "version": "fixture-1"}, "applied_configuration": map[string]any{"role": "tested_agent", "engine": "codex", "protocol": "cli", "provider": "provider", "model": "model", "version": "fixture-1"}, "start_time": "2026-11-12T01:00:00Z", "end_time": "2026-11-12T01:00:01Z", "case_results": cases}
}

func encodeFixture(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parsed(t *testing.T, value any) Report {
	t.Helper()
	report, err := ParseReport(encodeFixture(t, value))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func verified(t *testing.T, value any) Report {
	t.Helper()
	report := parsed(t, value)
	// Only a trusted executor may add an independently frozen suite identity.
	report.SuiteDigest = "sha256:" + strings.Repeat("1", 64)
	return report
}

func TestParsePreservesOutcomeAndDeduplicatesBenchmarkSummary(t *testing.T) {
	with := fixtureCase("one", "PASS")
	with["configuration"] = "with_skill"
	without := fixtureCase("one", "FAIL")
	without["configuration"] = "without_skill"
	report := parsed(t, fixtureReport(without, with))
	if report.Status != "pass" || len(report.Cases) != 2 || report.Summary.Total != 1 || report.Summary.Pass != 1 || report.Summary.Fail != 0 {
		t.Fatalf("report=%+v", report)
	}
	if report.Cases[0].Variant != "without_skill" || report.Cases[1].Variant != "with_skill" || report.Cases[1].InputID == "" {
		t.Fatal(report.Cases)
	}
	for _, status := range []string{"FAIL", "ERROR", "SKIP", "unexpected"} {
		t.Run(status, func(t *testing.T) {
			item := fixtureCase("case", status)
			if status == "ERROR" {
				item["grading"] = nil
				item["error"] = "engine failed"
			}
			got := parsed(t, fixtureReport(item))
			want := map[string]string{"FAIL": "fail", "ERROR": "error", "SKIP": "incomplete", "unexpected": "incomplete"}[status]
			if got.Status != want {
				t.Fatalf("%s => %+v", status, got)
			}
		})
	}
	baseline := fixtureCase("one", "PASS")
	baseline["configuration"] = "without_skill"
	if got := parsed(t, fixtureReport(baseline)); got.Status != "incomplete" {
		t.Fatal("baseline-only report claimed skill evaluation passed")
	}
}

func TestParseDoesNotPublishPromptsResponsesOrCredentials(t *testing.T) {
	const secret = "github_pat_1234567890ABCDEFGHIJKLMN"
	item := fixtureCase("one", "FAIL")
	item["prompt"] = "Prompt-private-" + secret
	item["response"] = "Response-private-" + secret
	item["turn_results"] = []any{map[string]any{"turn_number": 1, "content": "Turn-private-" + secret, "response": "Nested-response-private-" + secret, "source": "configured"}}
	item["error"] = "token = '" + secret + "'"
	item["grading"].(map[string]any)["assertion_results"] = []any{map[string]any{"text": "credential rule", "passed": false, "evidence": "token = '" + secret + "'"}}
	value := fixtureReport(item)
	value["requested_configuration"].(map[string]any)["api_key"] = secret
	value["arbitrary_secret"] = secret
	report := parsed(t, value)
	encoded := string(encodeFixture(t, report))
	for _, needle := range []string{secret, "Prompt-private", "Response-private", "Turn-private", "Nested-response-private", "api_key", "arbitrary_secret"} {
		if strings.Contains(encoded, needle) {
			t.Fatalf("published %q: %s", needle, encoded)
		}
	}
	if report.Cases[0].InputID == "" || !strings.Contains(encoded, "REDACTED") {
		t.Fatal(encoded)
	}
}

func TestParseRejectsMalformedAndBoundlessReports(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty": nil, "oversize": make([]byte, MaxReportBytes+1), "null": []byte("null"), "array": []byte("[]"), "bad json": []byte("{"), "extra object": []byte("{} {}"), "duplicate keys": []byte(`{"skill_name":"first","skill_name":"second","case_results":[]}`),
		"depth": []byte(`{"extra":` + strings.Repeat("[", 80) + `null` + strings.Repeat("]", 80) + `}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReport(data); err == nil {
				t.Fatal("accepted malformed report")
			}
		})
	}
	value := fixtureReport()
	if _, err := ParseReport(encodeFixture(t, value)); err == nil {
		t.Fatal("empty cases passed")
	}
	value = fixtureReport(fixtureCase("one", "PASS"), fixtureCase("one", "PASS"))
	if _, err := ParseReport(encodeFixture(t, value)); err == nil {
		t.Fatal("duplicate case passed")
	}
	value = fixtureReport(fixtureCase("unsafe\x00id", "PASS"))
	if _, err := ParseReport(encodeFixture(t, value)); err == nil {
		t.Fatal("control character identity passed")
	}
	items := make([]map[string]any, MaxCases+1)
	for n := range items {
		items[n] = fixtureCase("one", "PASS")
	}
	if _, err := ParseReport(encodeFixture(t, fixtureReport(items...))); err == nil {
		t.Fatal("case budget passed")
	}
}

func TestDeclaredPassRequiresCompletedConsistentAssertions(t *testing.T) {
	for _, name := range []string{"no grading", "empty assertions", "missing passed", "failed assertion", "skipped assertion", "grading fail", "case error", "empty text", "unmeasured gate", "contradictory gate", "unknown judge", "unsupported schema"} {
		t.Run(name, func(t *testing.T) {
			item := fixtureCase("one", "PASS")
			value := fixtureReport(item)
			grading := item["grading"].(map[string]any)
			switch name {
			case "no grading":
				item["grading"] = nil
			case "empty assertions":
				grading["assertion_results"] = []any{}
			case "missing passed":
				grading["assertion_results"] = []any{map[string]any{"text": "rule"}}
			case "failed assertion":
				grading["assertion_results"] = []any{map[string]any{"text": "rule", "passed": false}}
			case "skipped assertion":
				grading["assertion_results"] = []any{map[string]any{"text": "rule", "passed": true, "skipped": true}}
			case "grading fail":
				grading["status"] = "FAIL"
			case "case error":
				item["error"] = "still failed"
			case "empty text":
				grading["assertion_results"] = []any{map[string]any{"text": "", "passed": true}}
			case "unmeasured gate":
				item["grading"] = nil
				item["gates"] = map[string]any{"passed": true, "configured_rules": []any{}}
			case "contradictory gate":
				item["gates"] = map[string]any{"passed": true, "configured_rules": []any{"rule"}, "failures": []any{map[string]any{"rule": "rule", "detail": "did fail"}}}
			case "unknown judge":
				item["judge_results"] = []any{map[string]any{"id": "custom", "status": "mystery"}}
			case "unsupported schema":
				value["schema_version"] = "next-version"
			}
			report := parsed(t, value)
			if report.Status == "pass" {
				t.Fatalf("unmeasured PASS accepted: %+v", report)
			}
		})
	}
	item := fixtureCase("gate", "PASS")
	item["grading"] = nil
	item["gates"] = map[string]any{"passed": true, "configured_rules": []any{"must_contain"}, "failures": []any{}}
	if report := parsed(t, fixtureReport(item)); report.Status != "pass" || len(report.Cases[0].Assertions) != 1 {
		t.Fatalf("actual measured gate rejected: %+v", report)
	}
}

func TestCompareFixedRegressedRemovedNewAndUnknown(t *testing.T) {
	before := verified(t, fixtureReport(fixtureCase("fixed", "FAIL"), fixtureCase("regressed", "PASS"), fixtureCase("same", "PASS"), fixtureCase("removed", "PASS"), fixtureCase("skipped", "SKIP")))
	after := verified(t, fixtureReport(fixtureCase("fixed", "PASS"), fixtureCase("regressed", "FAIL"), fixtureCase("same", "PASS"), fixtureCase("new", "PASS"), fixtureCase("skipped", "PASS")))
	comparison := Compare(before, after)
	if !comparison.Comparable || comparison.Summary.Fixed != 1 || comparison.Summary.Regressed != 1 || comparison.Summary.Unchanged != 1 || comparison.Summary.Removed != 1 || comparison.Summary.New != 1 || comparison.Summary.Unknown != 1 || comparison.Verdict == "pass" {
		t.Fatalf("%+v", comparison)
	}
	clean := verified(t, fixtureReport(fixtureCase("same", "PASS")))
	if got := Compare(clean, clean); got.Verdict != "pass" || got.Summary.Unchanged != 1 {
		t.Fatalf("clean comparison=%+v", got)
	}
	onlyFixed := Compare(verified(t, fixtureReport(fixtureCase("one", "FAIL"))), verified(t, fixtureReport(fixtureCase("one", "PASS"))))
	if onlyFixed.Verdict != "pass" || onlyFixed.Summary.Fixed != 1 {
		t.Fatal(onlyFixed)
	}
}

func TestCompareRequiresSameInputRulesAndModelConfiguration(t *testing.T) {
	before := verified(t, fixtureReport(fixtureCase("one", "FAIL")))
	for _, name := range []string{"prompt", "rule", "missing prompt", "engine", "model", "configuration", "skill"} {
		t.Run(name, func(t *testing.T) {
			item := fixtureCase("one", "PASS")
			value := fixtureReport(item)
			switch name {
			case "prompt":
				item["prompt"] = "A different document"
			case "rule":
				item["grading"].(map[string]any)["assertion_results"] = []any{map[string]any{"text": "An easier rule", "passed": true}}
			case "missing prompt":
				delete(item, "prompt")
			case "engine":
				value["engine_name"] = "other"
			case "model":
				value["model_name"] = "other-model"
			case "configuration":
				value["applied_configuration"].(map[string]any)["version"] = "different"
			case "skill":
				value["skill_name"] = "other-skill"
			}
			got := Compare(before, verified(t, value))
			if got.Verdict == "pass" || got.Summary.Fixed > 0 || got.Summary.Unknown != 1 {
				t.Fatalf("input/config difference counted as fix: %+v", got)
			}
		})
	}
	without := fixtureCase("one", "FAIL")
	without["configuration"] = "without_skill"
	with := fixtureCase("one", "PASS")
	with["configuration"] = "with_skill"
	if got := Compare(verified(t, fixtureReport(without)), verified(t, fixtureReport(with))); got.Verdict == "pass" || got.Summary.Fixed != 0 || got.Summary.New != 1 || got.Summary.Removed != 1 {
		t.Fatal(got)
	}
}

func TestMultiTurnInputIdentityIncludesLaterInputButNotResponses(t *testing.T) {
	makeReport := func(later, response string) Report {
		item := fixtureCase("one", "PASS")
		item["turn_results"] = []any{map[string]any{"turn_number": 1, "content": "first fixed input", "source": "case", "response": response}, map[string]any{"turn_number": 2, "content": later, "source": "case", "response": response}}
		return parsed(t, fixtureReport(item))
	}
	first, changedResponse, changedInput := makeReport("second fixed input", "old response"), makeReport("second fixed input", "new response"), makeReport("different later input", "old response")
	if first.Cases[0].InputID == "" || first.Cases[0].InputID != changedResponse.Cases[0].InputID {
		t.Fatal("response changed input identity")
	}
	if first.Cases[0].InputID == changedInput.Cases[0].InputID || Compare(first, changedInput).Verdict != "incomplete" {
		t.Fatal("later turn change was ignored")
	}
	encoded := string(encodeFixture(t, first))
	if strings.Contains(encoded, "first fixed input") || strings.Contains(encoded, "second fixed input") || strings.Contains(encoded, "old response") {
		t.Fatal("multiturn private text published")
	}
}

func TestImportedReportCannotSelfAttestFrozenRules(t *testing.T) {
	value := fixtureReport(fixtureCase("one", "PASS"))
	value["suiteDigest"] = "sha256:" + strings.Repeat("1", 64)
	value["suite_digest"] = "sha256:" + strings.Repeat("1", 64)
	report := parsed(t, value)
	if report.SuiteDigest != "" {
		t.Fatal("upstream payload self-attested its test rules")
	}
	comparison := Compare(report, report)
	if comparison.Comparable || comparison.Verdict != "incomplete" || comparison.Summary.Unknown != 1 || comparison.Summary.Fixed != 0 {
		t.Fatal(comparison)
	}
	verifiedBefore, verifiedAfter := verified(t, value), verified(t, value)
	verifiedAfter.SuiteDigest = "sha256:" + strings.Repeat("2", 64)
	if got := Compare(verifiedBefore, verifiedAfter); got.Comparable || got.Verdict != "incomplete" || got.Summary.Unknown != 1 {
		t.Fatal("rule implementation change was comparable", got)
	}
}

func TestCompareKeepsExplicitObservedRuntimeDifferencesUnknown(t *testing.T) {
	beforeCase := fixtureCase("one", "FAIL")
	beforeCase["observed_model"] = "actual-model-a"
	beforeCase["observed_version"] = "runtime-v1"
	afterCase := fixtureCase("one", "PASS")
	afterCase["observed_model"] = "actual-model-b"
	afterCase["observed_version"] = "runtime-v1"
	before, after := verified(t, fixtureReport(beforeCase)), verified(t, fixtureReport(afterCase))
	if got := Compare(before, after); got.Verdict != "incomplete" || got.Summary.Unknown != 1 || got.Summary.Fixed != 0 {
		t.Fatal(got)
	}
	delete(afterCase, "observed_model")
	delete(afterCase, "observed_version")
	if got := Compare(before, verified(t, fixtureReport(afterCase))); got.Verdict != "incomplete" || got.Summary.Fixed != 0 {
		t.Fatal(got)
	}
}

func TestIdentityPlaceholdersCannotEstablishComparability(t *testing.T) {
	for _, field := range []string{"engine_name", "model_name", "schema_version"} {
		for _, placeholder := range []string{"", " ", "\t", "unknown", "UNKNOWN", "unset", "<unset>", " <UNSET> "} {
			t.Run(field+"/"+placeholder, func(t *testing.T) {
				value := fixtureReport(fixtureCase("one", "PASS"))
				value[field] = placeholder
				report := verified(t, value)
				if report.IdentityComplete {
					t.Fatal("placeholder identity marked complete")
				}
				if comparison := Compare(report, report); comparison.Comparable || comparison.Verdict != "incomplete" {
					t.Fatalf("placeholder became passing regression: %+v", comparison)
				}
			})
		}
	}
}
