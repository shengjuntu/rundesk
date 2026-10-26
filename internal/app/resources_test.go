package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/resource"
)

func TestSessionResourcesKeepOriginalPathsWithoutCopyOrNativeLaunch(t *testing.T) {
	m := testManager(t)
	w, err := m.CreateWorkspace("original", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession(w.ID, "resource view", "")
	if err != nil {
		t.Fatal(err)
	}
	set, err := m.SessionResourceSet(s)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := m.ResourceService().Resolve(s, resource.RoleWorkspace)
	if err != nil || workspace.Path != w.Path {
		t.Fatalf("workspace moved: %+v %v", workspace, err)
	}
	outputs, err := m.ResolveSessionResource(s, resource.RoleOutputs)
	if err != nil || outputs.Path != filepath.Join(w.Path, "outputs", s.ID) || outputs.RelativePath != "outputs/"+s.ID {
		t.Fatalf("output binding moved: %+v %v", outputs, err)
	}
	instance, _ := m.Instance(s.InstanceID)
	state, err := m.ResolveSessionResource(s, resource.RoleRuntimeState)
	if err != nil || state.Path != instance.CodexHome {
		t.Fatalf("native state moved: %+v %v", state, err)
	}
	if _, err := os.Stat(outputs.Path); !os.IsNotExist(err) {
		t.Fatalf("read created output directory: %v", err)
	}
	if m.loaded.Load() != 0 {
		t.Fatal("resource inspection launched a native runtime")
	}
	data, err := json.Marshal(set.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, location := range []string{m.Data, w.Path, instance.CodexHome} {
		// JSON escapes Windows separators, so check its encoded value too.
		encoded, _ := json.Marshal(location)
		if strings.Contains(string(data), location) || strings.Contains(string(data), string(encoded)) {
			t.Fatalf("manifest leaked private path: %s", data)
		}
	}
}

func TestSessionResourcesPolicyRevisionIsCurrentBindingAndImmutable(t *testing.T) {
	m := testManager(t)
	s, err := m.CreateSession(m.Workspaces()[0].ID, "policy", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.SessionResourceSet(s)
	if err != nil {
		t.Fatal(err)
	}
	old, err := before.Binding(resource.RolePolicy)
	if err != nil {
		t.Fatal(err)
	}
	i, _ := m.Instance(s.InstanceID)
	permissions := i.Permissions.normalized()
	network := true
	permissions.NetworkAccess = &network
	if _, err = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, Permissions: &permissions, Revision: i.Revision}); err != nil {
		t.Fatal(err)
	}
	current, err := m.SessionResourceSet(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = current.Describe(old.Ref); err == nil {
		t.Fatal("stale policy version silently resolved")
	}
	policy, err := m.PolicyService().ForSession(s)
	if err != nil {
		t.Fatal(err)
	}
	*policy.Permissions.NetworkAccess = false
	again, err := m.PolicyService().ForSession(s)
	if err != nil || !*again.Permissions.NetworkAccess {
		t.Fatal("policy view mutated canonical permissions")
	}
}

func TestKunResourceConfigurationDoesNotExposeCredentials(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	i, _ := m.Instance()
	const secret = "DO-NOT-EXPOSE-A-REAL-CONFIG-SECRET"
	if err := m.Store.Put("kun-mcp-config", i.ID, kunMCPConfig{Revision: 7, Servers: map[string]any{
		"browser": map[string]any{"command": "secret-command", "env": map[string]any{"TOKEN": secret}},
	}}); err != nil {
		t.Fatal(err)
	}
	s := Session{ID: "kun-session", WorkspaceID: w.ID, InstanceID: i.ID, RuntimeKind: "kun", ExecutionMode: "local"}
	set, err := m.SessionResourceSet(s)
	if err != nil {
		t.Fatal(err)
	}
	state, err := m.ResolveSessionResource(s, resource.RoleRuntimeState)
	if err != nil || state.Path != filepath.Join(m.Data, "kun", "sessions", s.ID) {
		t.Fatalf("Kun source changed: %+v %v", state, err)
	}
	manifest := set.Manifest()
	data, _ := json.Marshal(manifest)
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "secret-command") || strings.Contains(string(data), "TOKEN") {
		t.Fatalf("native configuration leaked: %s", data)
	}
	var mcp, plugins resource.Descriptor
	for _, descriptor := range manifest.Resources {
		if descriptor.Ref.Kind == resource.KindMCP {
			mcp = descriptor
		}
		if descriptor.Ref.Kind == resource.KindPlugins {
			plugins = descriptor
		}
	}
	if mcp.Ref.Version != "7" || len(mcp.Configuration.Names) != 1 || mcp.Configuration.Names[0] != "browser" {
		t.Fatalf("missing MCP declaration: %+v", mcp)
	}
	if plugins.Configuration.State != "unsupported" {
		t.Fatal("Kun was given fabricated plugin support")
	}
	if _, err := os.Stat(state.Path); !os.IsNotExist(err) {
		t.Fatalf("read created worker state: %v", err)
	}
}

func TestDockerResourcesUseRegisteredEnvironmentHome(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	i, _ := m.Instance()
	id := environmentID(i.ID, w.ID)
	home := t.TempDir()
	m.environmentMu.Lock()
	m.environments[id] = &Environment{ID: id, InstanceID: i.ID, WorkspaceID: w.ID, CodexHome: home}
	m.environmentMu.Unlock()
	s := Session{ID: "docker-session", WorkspaceID: w.ID, InstanceID: i.ID, RuntimeKind: "codex", ExecutionMode: "docker", EnvironmentID: id}
	state, err := m.ResolveSessionResource(s, resource.RoleRuntimeState)
	if err != nil || state.Path != home {
		t.Fatalf("Docker resolved instance home instead of environment: %+v %v", state, err)
	}
	s.EnvironmentID = "different-environment"
	if _, err = m.SessionResourceSet(s); err == nil {
		t.Fatal("foreign environment accepted")
	}
}
