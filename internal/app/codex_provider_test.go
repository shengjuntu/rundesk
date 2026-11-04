package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func providerRevision(n int) *int { return &n }
func providerInput(rev int) CodexProviderInput {
	return CodexProviderInput{BaseURL: "http://host.docker.internal:18000/v1", Model: "local-model", Revision: providerRevision(rev)}
}
func TestCodexProviderDefaultRevisionAndSharedLocalHome(t *testing.T) {
	m := testManager(t)
	in := providerInput(0)
	p, err := m.SaveDefaultCodexProvider(in)
	if err != nil || !p.Configured || p.Revision != 1 {
		t.Fatal(p, err)
	}
	if _, err = m.SaveDefaultCodexProvider(in); err == nil {
		t.Fatal("stale default accepted")
	}
	if err = m.ValidateDefaultCodexProvider(0); err == nil {
		t.Fatal("stale launch default accepted")
	}
	if err = m.ValidateDefaultCodexProvider(p.Revision); err != nil {
		t.Fatal(err)
	}
	i, err := m.CreateInstance(InstancePatch{Name: "Local"})
	if err != nil {
		t.Fatal(err)
	}
	w := m.Workspaces()[0]
	other, err := m.CreateWorkspace("Other", "")
	if err != nil {
		t.Fatal(err)
	}
	original := "# keep values\n[mcp_servers.local]\ncommand = 'tool'\n[skills]\ncustom = true\n"
	if err = os.WriteFile(filepath.Join(i.CodexHome, "config.toml"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := m.SaveCodexProvider(i.ID, w.ID, in)
	if err != nil || v.State != "configured" {
		t.Fatal(v, err)
	}
	seen, err := m.CodexProvider(i.ID, other.ID)
	if err != nil || seen.Model != in.Model || seen.Revision != v.Revision || seen.WorkspaceID != other.ID {
		t.Fatal("local home binding fragmented", seen, err)
	}
	if m.codexProviderModel(i.ID, other.ID) != in.Model {
		t.Fatal("local model fallback differs by workspace")
	}
	raw, err := os.ReadFile(filepath.Join(i.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = toml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config["mcp_servers"].(map[string]any)["local"].(map[string]any)["command"] != "tool" || config["skills"].(map[string]any)["custom"] != true {
		t.Fatal("unknown settings lost", config)
	}
	if _, err = m.SaveCodexProvider(i.ID, other.ID, in); err == nil {
		t.Fatal("shared local revision lost")
	}
}
func TestCodexProviderPendingSeedCredentialScopeAndRotation(t *testing.T) {
	m, _, i, w := dockerTestApp(t)
	t.Setenv("LOCAL_MODEL_KEY", "private-test-marker")
	in := providerInput(0)
	in.APIKeyEnv = "LOCAL_MODEL_KEY"
	p, err := m.SaveDefaultCodexProvider(in)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ApplyDefaultCodexProvider(i.ID, w.ID, p.Revision); err != nil {
		t.Fatal(err)
	}
	view, err := m.CodexProvider(i.ID, w.ID)
	if err != nil || view.State != "pending" {
		t.Fatal(view, err)
	}
	env, err := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(env.CodexHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("provider prevented image seed", err)
	}
	path, cleanup, err := m.codexProviderEnvFile(env)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "LOCAL_MODEL_KEY=private-test-marker\n" {
		t.Fatal("selected credential not isolated", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential file permissions")
	}
	cleanup()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temporary credential not removed")
	}
	if err = os.WriteFile(filepath.Join(env.CodexHome, "config.toml"), []byte("[mcp_servers.media]\ncommand = 'video-tool'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.recordCodexProviderDeployment(env); err != nil {
		t.Fatal(err)
	}
	if err = m.applySeededCodexProvider(env); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
	if !strings.Contains(string(raw), "video-tool") || strings.Contains(string(raw), "private-test-marker") {
		t.Fatal("seed capability lost or secret persisted")
	}
	view, _ = m.CodexProvider(i.ID, w.ID)
	if view.State != "configured" {
		t.Fatal(view)
	}
	// The image seed hook must not rewrite a manually edited configured HOME.
	sentinel := append(raw, []byte("\n# manual comment\n")...)
	os.WriteFile(filepath.Join(env.CodexHome, "config.toml"), sentinel, 0600)
	if err = m.applySeededCodexProvider(env); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
	if string(unchanged) != string(sentinel) {
		t.Fatal("configured home overwritten at start")
	}
	other, err := m.CreateWorkspace("Unrelated", "")
	if err != nil {
		t.Fatal(err)
	}
	absent, _ := m.CodexProvider(i.ID, other.ID)
	if absent.Configured {
		t.Fatal("credential inherited by unrelated workspace")
	}
	m.environmentMu.Lock()
	m.environments[env.ID].ContainerID = "running-container"
	m.environmentMu.Unlock()
	t.Setenv("LOCAL_MODEL_KEY", "rotated-private-marker")
	in.Revision = providerRevision(view.Revision)
	view, err = m.SaveCodexProvider(i.ID, w.ID, in)
	if err != nil || view.State != "recreate_required" {
		t.Fatal(view, err)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "private-marker") {
		t.Fatal("API credential leak")
	}
	current, _ := os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
	if string(current) != string(sentinel) {
		t.Fatal("pending rotation changed active config")
	}
	// Launch retries retain their frozen default even after global edits.
	if err = m.ApplyDefaultCodexProvider(i.ID, w.ID, p.Revision); err == nil {
		t.Fatal("explicit environment config overwritten by default retry")
	}
}
func TestCodexProviderAtomicInvalidConfigAndAuthOverride(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	os.WriteFile(path, []byte("not [valid"), 0600)
	p := CodexProviderConfig{BaseURL: "http://localhost:8000/v1", Model: "model"}
	if err := writeCodexProviderConfig(home, p); err == nil {
		t.Fatal("invalid TOML overwritten")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "not [valid" {
		t.Fatal("failed edit mutated config")
	}
	provider := map[string]any{"requires_openai_auth": false, "env_key": "MODEL_KEY"}
	if !codexProviderAuthMatches(provider, "MODEL_KEY") {
		t.Fatal("valid explicit auth rejected")
	}
	provider["env_key"] = "OTHER_KEY"
	if codexProviderAuthMatches(provider, "MODEL_KEY") {
		t.Fatal("project credential override ignored")
	}
	provider["env_key"] = "MODEL_KEY"
	provider["http_headers"] = map[string]any{"Authorization": "private"}
	if codexProviderAuthMatches(provider, "MODEL_KEY") {
		t.Fatal("header override ignored")
	}
}
func TestCodexProviderResponsesProbeRequiresToolRoundTrip(t *testing.T) {
	for _, mode := range []string{"ready", "chat-only", "no-tool", "no-final", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v1/responses" {
					t.Error("wrong protocol", r.URL.Path)
				}
				if mode == "chat-only" {
					w.WriteHeader(404)
					return
				}
				if mode == "unauthorized" {
					w.WriteHeader(401)
					fmt.Fprint(w, "private-secret")
					return
				}
				var in map[string]any
				json.NewDecoder(r.Body).Decode(&in)
				if r.Header.Get("Authorization") != "Bearer private-secret" {
					t.Error("credential missing")
				}
				if calls == 1 {
					if mode == "no-tool" {
						fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`)
						return
					}
					fmt.Fprint(w, `{"output":[{"type":"function_call","name":"rundesk_provider_ping","call_id":"call-1","arguments":"{\"value\":\"OK\"}"}]}`)
				} else {
					if in["tool_choice"] != "none" {
						t.Error("tool round trip missing")
					}
					if mode == "no-final" {
						fmt.Fprint(w, `{"output":[]}`)
					} else {
						fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`)
					}
				}
			}))
			defer server.Close()
			client := setupProbeClient(time.Second)
			defer client.CloseIdleConnections()
			r := probeCodexResponses(context.Background(), client, CodexProviderConfig{BaseURL: server.URL + "/v1", Model: "test"}, "private-secret", "host")
			if (r.Status == "ready") != (mode == "ready") || r.ToolsOK != (mode == "ready") {
				t.Fatal(r)
			}
			if strings.Contains(r.Error, "private-secret") {
				t.Fatal("probe error leaked key")
			}
		})
	}
}

func TestCodexProviderProjectSnapshotRetainsSelectedCredential(t *testing.T) {
	m, _, i, w := dockerTestApp(t)
	t.Setenv("MODEL_KEY", "snapshot-private-key")
	in := providerInput(0)
	in.APIKeyEnv = "MODEL_KEY"
	if _, err := m.SaveCodexProvider(i.ID, w.ID, in); err != nil {
		t.Fatal(err)
	}
	other, err := m.CreateWorkspace("project", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.inheritProjectCodexProvider(i.ID, w.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	view, err := m.CodexProvider(i.ID, other.ID)
	if err != nil || view.APIKeyEnv != "MODEL_KEY" || view.State != "pending" || m.codexProviderModel(i.ID, other.ID) != in.Model {
		t.Fatal(view, err)
	}
	env, err := m.EnsureApplicationEnvironment(i.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := m.codexProviderEnvFile(env)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := os.ReadFile(path)
	if string(raw) != "MODEL_KEY=snapshot-private-key\n" {
		t.Fatal("project selected credential not available")
	}
	// Updating source does not mutate the already captured project profile.
	source, _ := m.CodexProvider(i.ID, w.ID)
	in.Model = "source-changed"
	in.Revision = providerRevision(source.Revision)
	if _, err = m.SaveCodexProvider(i.ID, w.ID, in); err != nil {
		t.Fatal(err)
	}
	frozen, _ := m.CodexProvider(i.ID, other.ID)
	if frozen.Model == in.Model {
		t.Fatal("project snapshot changed retroactively")
	}
}
