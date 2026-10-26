package app

import (
	p "github.com/your-org/kun/sdk/protocol"
	"strings"
	"testing"
)

func TestKunBashHostPermissionBoundary(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	m, err := New(t.TempDir(), "missing-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	i, _ := m.Instance()
	cfg := p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "fixture", AllowBash: true}
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, cfg); err == nil || !strings.Contains(err.Error(), "Bash") {
		t.Fatal("workspace permission accepted Bash", err)
	}
	perm := Permissions{Sandbox: "danger-full-access", ApprovalPolicy: "on-request", Reviewer: "user"}
	i, err = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Revision: i.Revision, Permissions: &perm})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.SetAgentRuntime(i.ID, i.Revision, cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := m.Workspaces()[0]
	s, err := m.CreateSession(w.ID, "bash permission", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.kunStartRequest(s, w, Input{Text: "test"}); err != nil {
		t.Fatal(err)
	}
	perm.Sandbox = "read-only"
	i, err = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Revision: i.Revision, Permissions: &perm})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.kunStartRequest(s, w, Input{Text: "test"}); err == nil {
		t.Fatal("permission drift permitted Bash")
	}
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, cfg); err == nil {
		t.Fatal("read-only permitted Bash")
	}
}
