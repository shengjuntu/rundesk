package app

import (
	"context"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
	d "github.com/shengjuntu/rundesk/internal/debugapi"
)

func TestExternalKunDebugCapabilitiesAreObserved(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	m, err := New(t.TempDir(), "missing-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Kun = buildKunLegacyContractFixture(t)
	i, _ := m.Instance()
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "unused"}); err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession(m.Workspaces()[0].ID, "external capabilities", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(s.ID, Input{Text: "core task"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, "completed")
	service := DebugService{manager: m}
	caps, err := service.Capabilities(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"context", "snapshot", "diff", "run"} {
		if caps.Queries[kind].Supported || caps.Queries[kind].Available || caps.Queries[kind].Reason == "" {
			t.Fatalf("invented %s capability: %+v", kind, caps.Queries[kind])
		}
	}
	for _, kind := range []string{"events", "runs", "statistics", "llm_requests", "observations", "activity"} {
		if !caps.Queries[kind].Supported || !caps.Queries[kind].Available {
			t.Fatalf("host query %s unavailable: %+v", kind, caps.Queries[kind])
		}
	}
	if _, err = service.Query(context.Background(), s.ID, d.Query{Kind: "context"}); err == nil {
		t.Fatal("unsupported internal query was dispatched")
	}
}
