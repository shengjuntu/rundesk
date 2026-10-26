package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pluginZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestNativePluginProjectionKeepsEvidenceSeparateAndSanitizes(t *testing.T) {
	c := nativeConfig{Config: map[string]any{"plugins": map[string]any{
		"disabled@example": map[string]any{"enabled": false, "credentials": "config-secret"},
		"enabled@example":  map[string]any{"enabled": true, "env": map[string]any{"TOKEN": "env-secret"}},
		"future@example":   "unknown future format",
	}, "api_key": "unrelated-secret"}}
	before, _ := json.Marshal(c)
	skills := json.RawMessage(`{"data":[{"skills":[{"name":"director","path":"/plugins/director/SKILL.md","enabled":true,"pluginId":"enabled@example","pluginDisplayName":"Director","description":"never-return-description"},{"name":"manual","path":"/manual/SKILL.md","enabled":true}]}]}`)
	mcp := json.RawMessage(`{"data":[{"name":"video","pluginId":"runtime-only@example","runtimeStatus":"connected","url":"https://u:password-secret@host/mcp?token=url-secret","http_headers":{"Authorization":"header-secret"},"tools":{"private":{"description":"tool-secret"}}}]}`)
	plugins, warnings := projectNativePlugins(c, skills, mcp)
	if len(plugins) != 4 || len(warnings) != 0 {
		t.Fatal(plugins, warnings)
	}
	byID := map[string]NativePluginStatus{}
	for _, p := range plugins {
		byID[p.ID] = p
		if p.InventoryState != "unknown" {
			t.Fatal("invented installed state", p)
		}
	}
	if p := byID["enabled@example"]; !p.Configured || p.Enabled == nil || !*p.Enabled || p.Loaded != "unknown" || len(p.Skills) != 1 || p.Skills[0].Origin.PluginID != p.ID {
		t.Fatal(p)
	}
	if p := byID["disabled@example"]; p.Enabled == nil || *p.Enabled || p.Loaded != "unknown" {
		t.Fatal(p)
	}
	if p := byID["future@example"]; !p.Configured || p.Enabled != nil || p.Loaded != "unknown" {
		t.Fatal(p)
	}
	if p := byID["runtime-only@example"]; p.Configured || p.Enabled != nil || p.Loaded != "observed" || len(p.MCPServers) != 1 {
		t.Fatal(p)
	}
	b, _ := json.Marshal(plugins)
	for _, secret := range []string{"config-secret", "env-secret", "unrelated-secret", "password-secret", "url-secret", "header-secret", "tool-secret", "never-return-description"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("leaked", secret, string(b))
		}
	}
	after, _ := json.Marshal(c)
	if !bytes.Equal(before, after) {
		t.Fatal("projection changed native config")
	}
}

func TestNativePluginMCPOriginPreventsDuplicateImportsWithoutBlockingOtherEdits(t *testing.T) {
	status := json.RawMessage(`{"data":[{"name":"video","pluginId":"video@example"}]}`)
	before := map[string]any{"video": map[string]any{"url": "http://old"}, "manual": map[string]any{"url": "http://manual"}}
	appCode(t, nativePluginMCPWriteConflict(before, map[string]any{"video": map[string]any{"url": "http://new"}}, status), "native_plugin_component_read_only")
	appCode(t, nativePluginMCPWriteConflict(nil, map[string]any{"video": map[string]any{"url": "http://new"}}, status), "native_plugin_component_read_only")
	for _, changes := range []map[string]any{
		{},
		{"video": map[string]any{"url": "http://old"}, "manual": map[string]any{"url": "http://new"}},
		{"manual": map[string]any{"url": "http://new"}},
	} {
		if err := nativePluginMCPWriteConflict(before, changes, status); err != nil {
			t.Fatal(err)
		}
	}
	if err := nativePluginMCPWriteConflict(before, map[string]any{"manual": "changed"}, nil); err != nil {
		t.Fatal("unknown attribution must preserve legacy manual flow", err)
	}
}

func TestNativePluginOverviewUsesOnlyStableReadRPC(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	i, _ := m.Instance()
	marker := filepath.Join(i.CodexHome, "config.toml")
	if err := os.WriteFile(marker, []byte("# read-only marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	meta, err := m.NativePlugins(i.ID, "")
	if err != nil || !meta.ReadOnly || meta.Support.NativeInventory || meta.Support.NativeMutation || meta.Support.Code != nativePluginUnsupported || m.loaded.Load() != 0 {
		t.Fatal(meta, err)
	}
	view, err := m.NativePlugins(i.ID, w.ID)
	if err != nil || !view.Support.Configuration || !view.Support.EffectiveComponents {
		t.Fatal(view, err)
	}
	events, err := m.Store.Events("config-"+i.ID+"-"+w.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]int{}
	for _, event := range events {
		if event.Direction == "out" && event.Method != "" {
			methods[event.Method]++
			if strings.HasPrefix(event.Method, "plugin/") || strings.Contains(event.Method, "write") || strings.Contains(event.Method, "install") {
				t.Fatal("unexpected RPC", event.Method)
			}
		}
	}
	for _, method := range []string{"config/read", "skills/list", "mcpServerStatus/list"} {
		if methods[method] != 1 {
			t.Fatal(method, methods)
		}
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "# read-only marker\n" {
		t.Fatal(string(got), err)
	}
}

func TestNativePluginOverviewDoesNotStartDisabledOrMismatchedPreset(t *testing.T) {
	m, docker, _, p := runtimePresetRegistryFixture(t)
	disableRuntimePreset(t, m, p)
	_, err := m.NativePlugins(p.InstanceID, p.WorkspaceID)
	appCode(t, err, "runtime_preset_disabled")
	other, err := m.CreateWorkspace("Other", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.NativePlugins(p.InstanceID, other.ID)
	appCode(t, err, "runtime_preset_binding_conflict")
	if len(docker.containers) != 0 || docker.commands != 0 || m.loaded.Load() != 0 {
		t.Fatal("plugin read started runtime")
	}
}

func TestNativePluginKunIsExplicitlyUnsupportedWithoutFallback(t *testing.T) {
	m := testManager(t)
	m.mu.Lock()
	m.instances[DefaultInstance].AgentRuntime.Kind = "kun"
	m.mu.Unlock()
	view, err := m.NativePlugins(DefaultInstance, m.Workspaces()[0].ID)
	if err != nil || view.Support.Code != "backend_unsupported" || view.Support.Configuration || view.Support.EffectiveComponents || len(view.Plugins) != 0 || m.loaded.Load() != 0 {
		t.Fatal(view, err)
	}
}

func TestNativePluginPortableAndCompatibilityInspection(t *testing.T) {
	for _, tt := range []struct {
		name              string
		files             map[string]string
		format, extension string
	}{
		{"portable", map[string]string{
			"package/plugin.json":               `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"video-director","version":"1.2.0","description":"token=description-secret","extensions":{"com.openai":{"displayName":"Director","api_key":"extension-secret"}},"dependencies":{"ffmpeg":">=6"},"future":{"secret":"unknown-secret"},"hooks":{"command":"echo command-secret"}}`,
			"package/skills/director/SKILL.md":  "---\nname: director\n---\nprivate skill instructions",
			"package/mcp.json":                  `{"mcpServers":{"video":{"type":"streamable-http","url":"https://u:url-password@host/mcp?token=url-secret","http_headers":{"Authorization":"header-secret"}}}}`,
			"package/.codex-plugin/plugin.json": `{"name":"legacy-overlay","description":"overlay-secret"}`,
		}, "agent-plugin", "plugin.json"},
		{"compat", map[string]string{
			".codex-plugin/plugin.json": `{"name":"hyperframes","skills":"./skills/","interface":{"defaultPrompt":["private-prompt"]},"author":{"name":"Author"}}`,
			"skills/director/SKILL.md":  "private skill instructions",
			".mcp.json":                 `{"mcpServers":{"video":{"command":"bash","args":["-c","echo command-secret"],"env":{"TOKEN":"env-secret"}}}}`,
		}, "codex-compat", ".codex-plugin/plugin.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			view, err := InspectPluginPackage(bytes.NewReader(pluginZip(t, tt.files)))
			if err != nil || !view.ReadOnly || view.InstallSupported || view.Format != tt.format || view.Compatibility.OpenAIExtensionSource != tt.extension || view.Compatibility.Kun != "unsupported" || view.Compatibility.StableNativeInstall {
				t.Fatal(view, err)
			}
			if len(view.Components) < 2 {
				t.Fatal(view)
			}
			if tt.name == "portable" && (!reflect.DeepEqual(view.UnknownFields, []string{"future"}) || len(view.Dependencies) != 1 || view.Manifest.Name != "video-director") {
				t.Fatal(view)
			}
			b, _ := json.Marshal(view)
			for _, secret := range []string{"description-secret", "extension-secret", "unknown-secret", "command-secret", "private skill instructions", "url-password", "url-secret", "header-secret", "env-secret", "private-prompt", "overlay-secret"} {
				if strings.Contains(string(b), secret) {
					t.Fatal("leaked", secret, string(b))
				}
			}
		})
	}
}

func TestNativePluginInspectionRejectsUnsafeZIP(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "a\\windows", "C:drive", "a/../b", "a\x00b"} {
		t.Run(name, func(t *testing.T) {
			_, err := InspectPluginPackage(bytes.NewReader(pluginZip(t, map[string]string{"plugin.json": `{"name":"valid"}`, name: "x"})))
			if err == nil {
				t.Fatal("accepted unsafe", name)
			}
		})
	}
	for _, files := range []map[string]string{
		{"plugin.json": `{"name":"a"}`, "PLUGIN.JSON": `{"name":"b"}`},
		{"a/plugin.json": `{"name":"a"}`, "b/plugin.json": `{"name":"b"}`},
		{"plugin.json": `[]`},
		{"plugin.json": `{"version":"1"}`},
		{"plugin.json": `{"name":"a"}`, "mcp.json": `{"secret":"must-not-leak"}`},
	} {
		if _, err := InspectPluginPackage(bytes.NewReader(pluginZip(t, files))); err == nil {
			t.Fatal("accepted invalid package", files)
		}
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "plugin.json"}
	h.SetMode(fs.ModeSymlink | 0777)
	f, err := w.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("outside"))
	_ = w.Close()
	if _, err = InspectPluginPackage(bytes.NewReader(b.Bytes())); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, err = InspectPluginPackage(bytes.NewReader(make([]byte, pluginUploadLimit+1))); err == nil {
		t.Fatal("accepted oversized upload")
	}
	if _, err = InspectPluginPackage(bytes.NewReader(pluginZip(t, map[string]string{"plugin.json": strings.Repeat(" ", pluginManifestLimit+1)}))); err == nil {
		t.Fatal("accepted oversized manifest")
	}
}

func TestNativePluginInspectionIgnoresFilesOutsideWrappedRoot(t *testing.T) {
	view, err := InspectPluginPackage(bytes.NewReader(pluginZip(t, map[string]string{
		"package/plugin.json":              `{"name":"wrapped"}`,
		"package/skills/director/SKILL.md": "inside",
		"skills/outside/SKILL.md":          "outside",
	})))
	if err != nil || len(view.Components) != 1 || view.Components[0].Name != "director" {
		t.Fatal(view, err)
	}
}

func TestNativePluginBuildAssistantPackageCanBeInspectedAsNativePlugin(t *testing.T) {
	root := filepath.Join("..", "..", "plugins", "rundesk-build-tools")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := InspectPluginPackage(bytes.NewReader(pluginZip(t, files)))
	if err != nil || view.Manifest.Name != "rundesk-build-tools" || !view.ReadOnly || view.InstallSupported {
		t.Fatal(view, err)
	}
	found := false
	for _, component := range view.Components {
		if component.Kind == "skill" && component.Name == "add-app-docker-image" {
			found = true
		}
	}
	if !found {
		t.Fatal("native package skill missing", view)
	}
}

func TestNativePluginHTTPInspectAndMutationDoNotChangeConfiguration(t *testing.T) {
	m := testManager(t)
	mux := http.NewServeMux()
	(&Server{Manager: m}).nativePluginRoutes(mux)
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	f, err := multipartWriter.CreateFormFile("file", "package.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(pluginZip(t, map[string]string{"plugin.json": `{"name":"director"}`, "skills/director/SKILL.md": "private skill instructions"}))
	_ = multipartWriter.Close()
	r := httptest.NewRequest("POST", "/api/plugins/inspect", &body)
	r.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, r)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"readOnly":true`) {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, operation := range []string{"install", "uninstall"} {
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("POST", "/api/instances/default/plugins/"+operation, strings.NewReader(`{"api_key":"never-return"}`)))
		if response.Code != 501 || !strings.Contains(response.Body.String(), nativePluginUnsupported) || strings.Contains(response.Body.String(), "never-return") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	i, _ := m.Instance()
	if _, err = os.Stat(filepath.Join(i.CodexHome, "skills", "director")); !os.IsNotExist(err) {
		t.Fatal("package was flattened into manual skills", err)
	}
	if m.loaded.Load() != 0 {
		t.Fatal("inspection or unsupported mutation started native process")
	}
}
