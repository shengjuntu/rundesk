package app

import (
	"reflect"
	"testing"
)

func TestMCPToolApprovalValidation(t *testing.T) {
	for _, mode := range []string{"auto", "prompt", "writes", "approve"} {
		original := map[string]any{"url": "https://example.com/mcp", "tools": map[string]any{"tavily-search": map[string]any{"approval_mode": mode, "output_token_limit": 1024}}}
		got, err := prepareMCP(original, nil)
		if err != nil {
			t.Fatal(mode, err)
		}
		if got["tools"].(map[string]any)["tavily-search"].(map[string]any)["approval_mode"] != mode {
			t.Fatal(got)
		}
	}
	for _, bad := range []any{true, []any{}, map[string]any{"search": "approve"}, map[string]any{"search": map[string]any{"approval_mode": "never"}}, map[string]any{"search": map[string]any{"approval_mode": true}}, map[string]any{"search": map[string]any{"unknown": true}}, map[string]any{"search": map[string]any{"output_token_limit": 1.5}}} {
		if _, err := prepareMCP(map[string]any{"command": "example", "tools": bad}, nil); err == nil {
			t.Fatalf("accepted invalid policy %#v", bad)
		}
	}
}

func TestMCPToolApprovalSavedOnlyInSelectedInstance(t *testing.T) {
	m := testManager(t)
	wid := m.Workspaces()[0].ID
	a, err := m.CreateInstance(InstancePatch{Name: "research"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.CreateInstance(InstancePatch{Name: "video"})
	if err != nil {
		t.Fatal(err)
	}
	policy := map[string]any{"tavily-search": map[string]any{"approval_mode": "approve"}, "other": map[string]any{"approval_mode": "prompt"}}
	if _, err = m.SaveMCP(wid, "tavily", "0", map[string]any{"url": "https://example.com/mcp", "tools": policy}, false, a.ID); err != nil {
		t.Fatal(err)
	}
	cfg, err := m.readConfig(wid, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := userMCP(cfg)
	if !reflect.DeepEqual(servers["tavily"].(map[string]any)["tools"], policy) {
		t.Fatal("policy lost", servers)
	}
	cfg, err = m.readConfig(wid, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	servers, _ = userMCP(cfg)
	if servers["tavily"] != nil {
		t.Fatal("policy leaked to another instance")
	}
}
