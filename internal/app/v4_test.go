package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/rpc"
)

func TestApprovalOfferedDecisionsHTTPAndAudit(t *testing.T) {
	m := testManager(t)
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	_, err := m.Start(s.ID, Input{Text: "审批"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "waiting")
	a := m.Approvals(s.ID)[0]
	if len(a.Decisions) != 3 {
		t.Fatal(a.Decisions)
	}
	h := NewHandler(m, "", true)
	post := func(decision any) int {
		body, _ := json.Marshal(map[string]any{"decision": decision})
		req := httptest.NewRequest(http.MethodPost, "http://localhost/api/sessions/"+s.ID+"/approvals/"+a.ID, bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	for _, invalid := range []any{"acceptForSession", "decline", map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"/bin/bash"}}}} {
		if code := post(invalid); code != 409 {
			t.Fatalf("unoffered decision %v accepted (%d)", invalid, code)
		}
	}
	if len(m.Approvals(s.ID)) != 1 {
		t.Fatal("invalid reply cleared pending approval")
	}
	rule := a.Decisions[1]
	if code := post(rule); code != 200 {
		t.Fatalf("offered object rejected: %d", code)
	}
	waitState(t, m, s.ID, "completed")
	if code := post(rule); code != 409 {
		t.Fatal("duplicate reply accepted", code)
	}
	var stored Approval
	if err := m.Store.Get("approval", a.ID, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Decision == nil || stored.ResolvedAt == "" || stored.Status != "resolved" {
		t.Fatal(stored)
	}
}

func TestApprovalScopesAndLegacy(t *testing.T) {
	msg := rpc.Message{Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"command":"echo ok"}`)}
	if _, err := approvalResult(msg, "acceptForSession", nil, nil, ""); err == nil {
		t.Fatal("invented session grant")
	}
	msg.Params = json.RawMessage(`{"availableDecisions":[]}`)
	if _, err := approvalResult(msg, "accept", nil, nil, ""); err == nil {
		t.Fatal("ignored empty offered choices")
	}
	msg.Params = json.RawMessage(`{"availableDecisions":["acceptForSession","cancel"]}`)
	if _, err := approvalResult(msg, "acceptForSession", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	msg = rpc.Message{Method: "item/permissions/requestApproval", Params: json.RawMessage(`{"permissions":{"network":{"enabled":true}}}`)}
	result, err := approvalResult(msg, "accept", nil, nil, "session")
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["scope"] != "session" {
		t.Fatal(result)
	}
	if _, err := approvalResult(msg, "accept", nil, nil, "forever"); err == nil {
		t.Fatal("unbounded grant")
	}
	if _, err := approvalResult(msg, "anything", nil, nil, "turn"); err == nil {
		t.Fatal("invalid permission choice")
	}
	msg.Method = "mcpServer/elicitation/request"
	result, err = approvalResult(msg, "decline", nil, "must not send form content", "")
	if err != nil || result.(map[string]any)["content"] != nil {
		t.Fatal(result, err)
	}
}

func TestPermissionsApplyNextTurnAndRuntimePersists(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, _ := m.CreateSession(m.Workspaces()[0].ID, "", "")
	_, err = m.Start(s.ID, Input{Text: "审批 告警"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "waiting")
	before, _ := m.Runtime(s.ID)
	if before.Effective["model"] != "demo-fixture" || before.Requested.Sandbox != "workspace-write" {
		t.Fatal(before)
	}
	if len(before.Notices) != 1 || before.Notices[0].Count != 2 {
		t.Fatal(before.Notices)
	}
	i, _ := m.Instance()
	p := Permissions{Sandbox: "read-only", ApprovalPolicy: "never", Reviewer: "user"}
	if _, err = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Revision: i.Revision, Permissions: &p}); err != nil {
		t.Fatal(err)
	}
	still, _ := m.Runtime(s.ID)
	if still.ConnectionID != before.ConnectionID || still.Requested.Sandbox != "workspace-write" {
		t.Fatal("changed active turn policy")
	}
	a := m.Approvals(s.ID)[0]
	if err = m.Approve(s.ID, a.ID, "accept", nil, nil); err != nil {
		t.Fatal(err)
	}
	one := waitState(t, m, s.ID, "completed")
	_, err = m.Start(s.ID, Input{Text: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	two := waitState(t, m, s.ID, "completed")
	after, _ := m.Runtime(s.ID)
	if one.ThreadID != two.ThreadID || after.ConnectionID == before.ConnectionID || after.Requested.Sandbox != "read-only" || after.Effective["approvalPolicy"] != "never" {
		t.Fatal("next-turn policy mismatch", after)
	}
	// Old clients omitting permissions must not reset the instance's policy.
	i, _ = m.Instance()
	i, err = m.PatchInstance(i.ID, InstancePatch{Name: "renamed", Revision: i.Revision})
	if err != nil || i.Permissions.Sandbox != "read-only" {
		t.Fatal(i, err)
	}
	m.Close()
	reloaded, err := New(dir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	snapshot, _ := reloaded.Runtime(s.ID)
	if snapshot.Live || snapshot.Effective["approvalPolicy"] != "never" {
		t.Fatal("lost historical snapshot", snapshot)
	}
	if err = reloaded.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := reloaded.InstanceRuntime(i.ID)
	for _, r := range rows {
		if r.ID == s.ID {
			t.Fatal("deleted session left runtime data")
		}
	}
}

func TestSandboxDiagnosisDetectsBothCLIFormatsAndDoesNotBypass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, tc := range []struct {
		name, help, output string
		code               int
		want               string
	}{
		{"flat", "Usage: codex sandbox [OPTIONS] [COMMAND]...", "rundesk-sandbox-ok", 0, "ok"},
		{"platform", "Usage: codex sandbox <COMMAND>\nCommands:\n  linux  Linux sandbox", "rundesk-sandbox-ok", 0, "ok"},
		{"denied", "Usage: codex sandbox [COMMAND]...", "bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted", 1, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testManager(t)
			m.Demo = false
			path := filepath.Join(t.TempDir(), "codex-fixture")
			log := filepath.Join(t.TempDir(), "calls")
			t.Setenv("RUNDESK_PROBE_LOG", log)
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RUNDESK_PROBE_LOG\"\ncase \"$*\" in *--help*) cat <<'HELP'\n" + tc.help + "\nHELP\nexit 0;; esac\nprintf '%s\\n' '" + tc.output + "'\nexit " + string(rune('0'+tc.code)) + "\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			m.Codex = path
			i, _ := m.Instance()
			check := m.sandboxCheck(m.Workspaces()[0], i)
			if check.Status != tc.want || check.ExitCode == nil || *check.ExitCode != tc.code {
				t.Fatal(check)
			}
			if tc.name == "denied" && (check.Hint == "" || !strings.Contains(check.Output, "RTM_NEWADDR")) {
				t.Fatal(check)
			}
			calls, _ := os.ReadFile(log)
			if strings.Count(string(calls), "\n") != 2 || strings.Contains(string(calls), "danger-full-access") {
				t.Fatal("unexpected fallback", string(calls))
			}
			if tc.name == "platform" && !strings.Contains(string(calls), "sandbox linux --") {
				t.Fatal(string(calls))
			}
			if tc.name == "flat" && strings.Contains(string(calls), "sandbox linux") {
				t.Fatal(string(calls))
			}
		})
	}
}
