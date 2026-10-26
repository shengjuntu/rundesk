package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureDiagnosticNonce = "0123456789abcdef01234567"

// The fake Docker driver executes only the exact fixed Python payload locally in
// tests. Production uses Docker CLI and has no such local fallback.
type diagnosticTestDocker struct {
	*fakeDocker
	beforeExec func()
	argv       []string
	probe      string
}

func (d *diagnosticTestDocker) Command(args ...string) *exec.Cmd {
	if len(args) >= 4 && args[len(args)-4] == "python3" && args[len(args)-3] == "-I" && args[len(args)-2] == "-c" {
		d.argv = append([]string{}, args...)
		if d.beforeExec != nil {
			d.beforeExec()
		}
		program := args[len(args)-1]
		if d.probe != "" {
			program = d.probe
		}
		return exec.Command("python3", "-I", "-c", program)
	}
	return d.fakeDocker.Command(args...)
}

func diagnosticFixture(t *testing.T) (*Manager, *diagnosticTestDocker, Instance, Workspace, ApplicationRuntimeDiagnosticsInput) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python required for fixed-probe integration")
	}
	m, docker, instance, workspace := dockerTestApp(t)
	if _, e := m.RegisterApplication("news", ApplicationInput{Name: "News", InstanceID: instance.ID, WorkspaceID: workspace.ID}); e != nil {
		t.Fatal(e)
	}
	if _, e := m.PrepareApplicationRuntime("news", ApplicationRuntimeInput{InstanceID: instance.ID, WorkspaceID: workspace.ID, Probe: true}); e != nil {
		t.Fatal(e)
	}
	session, e := m.CreateSessionWithSource(workspace.ID, "diagnostic fixture", "", instance.ID, SessionSource{Kind: "application", AppID: "news"})
	if e != nil {
		t.Fatal(e)
	}
	path := "outputs/" + session.ID + "/.rundesk-probes/" + fixtureDiagnosticNonce + "/request.json"
	absolute := filepath.Join(workspace.Path, filepath.FromSlash(path))
	if e = os.MkdirAll(filepath.Dir(absolute), 0700); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "nonce": fixtureDiagnosticNonce})
	if e = os.WriteFile(absolute, raw, 0600); e != nil {
		t.Fatal(e)
	}
	driver := &diagnosticTestDocker{fakeDocker: docker}
	m.docker = driver
	in := ApplicationRuntimeDiagnosticsInput{InstanceID: instance.ID, WorkspaceID: workspace.ID, Nonce: fixtureDiagnosticNonce,
		Challenge: &ApplicationDiagnosticChallenge{Path: path, Nonce: fixtureDiagnosticNonce}}
	return m, driver, instance, workspace, in
}

func TestApplicationRuntimeDiagnosticsFixedPythonWorkspaceProof(t *testing.T) {
	m, driver, _, workspace, in := diagnosticFixture(t)
	view, e := m.DiagnoseApplicationRuntime(context.Background(), "news", in)
	if e != nil {
		t.Fatal(e)
	}
	proof := view["challenge"].(*diagnosticChallengeResult)
	if proof.Status != "verified" || view["modelTested"] != false || view["modelVisionVerified"] != false {
		t.Fatal(view)
	}
	if view["probed"] != true || view["accountReady"] != true || view["servers"] == nil {
		t.Fatal("existing client auth/tool discovery was not probed", view)
	}
	request, _ := os.ReadFile(filepath.Join(workspace.Path, filepath.FromSlash(in.Challenge.Path)))
	sum := sha256.Sum256(request)
	if proof.RequestSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("request hash does not cover actual bytes")
	}
	response, e := os.ReadFile(filepath.Join(workspace.Path, filepath.FromSlash(proof.ResponsePath)))
	if e != nil || !bytes.Contains(response, []byte(proof.RequestSHA256)) {
		t.Fatal(string(response), e)
	}
	if len(driver.argv) < 9 || strings.Join(driver.argv[:4], " ") != "exec -i --workdir "+workspace.Path || driver.argv[len(driver.argv)-1] != applicationRuntimeProbe {
		t.Fatal("unexpected non-fixed command", driver.argv)
	}
	encoded, _ := json.Marshal(view)
	for _, secret := range []string{m.Data, workspace.Path, "containerName", "PRIVATE", "http_headers"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("diagnostic leaked private value", secret)
		}
	}
}

func TestApplicationRuntimeDiagnosticsChallengeSecurity(t *testing.T) {
	for _, attack := range []string{"tamper", "file-symlink", "directory-symlink", "hardlink", "response-symlink", "fifo"} {
		t.Run(attack, func(t *testing.T) {
			m, _, _, workspace, in := diagnosticFixture(t)
			path := filepath.Join(workspace.Path, filepath.FromSlash(in.Challenge.Path))
			other := filepath.Join(t.TempDir(), "outside")
			original, _ := os.ReadFile(path)
			if e := os.WriteFile(other, original, 0600); e != nil {
				t.Fatal(e)
			}
			switch attack {
			case "tamper":
				_ = os.WriteFile(path, []byte(`{"schemaVersion":1,"nonce":"different"}`), 0600)
			case "file-symlink":
				_ = os.Remove(path)
				if e := os.Symlink(other, path); e != nil {
					t.Fatal(e)
				}
			case "directory-symlink":
				outdir := t.TempDir()
				_ = os.WriteFile(filepath.Join(outdir, "request.json"), original, 0600)
				_ = os.RemoveAll(filepath.Dir(path))
				if e := os.Symlink(outdir, filepath.Dir(path)); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(path, filepath.Join(filepath.Dir(path), "request-link")); e != nil {
					t.Fatal(e)
				}
			case "response-symlink":
				if e := os.Symlink(other, filepath.Join(filepath.Dir(path), "response.json")); e != nil {
					t.Fatal(e)
				}
			case "fifo":
				_ = os.Remove(path)
				if out, e := exec.Command("mkfifo", path).CombinedOutput(); e != nil {
					t.Fatal(e, string(out))
				}
			}
			view, e := m.DiagnoseApplicationRuntime(context.Background(), "news", in)
			if e != nil || view["challenge"].(*diagnosticChallengeResult).Status != "failed" {
				t.Fatal(view, e)
			}
			unchanged, _ := os.ReadFile(other)
			if !bytes.Equal(unchanged, original) {
				t.Fatal("outside file changed")
			}
		})
	}
}

func TestApplicationRuntimeDiagnosticsOwnershipConfigurationAndNoFallback(t *testing.T) {
	for _, attack := range []string{"binding", "session", "path", "nonce", "owner", "stopped", "local", "pending", "changed-during-probe"} {
		t.Run(attack, func(t *testing.T) {
			m, driver, instance, _, in := diagnosticFixture(t)
			want := "application_binding_changed"
			switch attack {
			case "binding":
				in.InstanceID = "foreign"
			case "session":
				in.Challenge.Path = "outputs/foreign/.rundesk-probes/" + fixtureDiagnosticNonce + "/request.json"
				want = "application_forbidden"
			case "path":
				in.Challenge.Path = "outputs/../.rundesk-probes/" + fixtureDiagnosticNonce + "/request.json"
				want = "invalid_diagnostic_challenge"
			case "nonce":
				in.Challenge.Nonce = strings.Repeat("f", 24)
				want = "invalid_diagnostic_challenge"
			case "owner":
				for _, c := range driver.containers {
					c.Labels["io.rundesk.owner"] = "foreign"
				}
				want = "container_owner_mismatch"
			case "stopped":
				for _, c := range driver.containers {
					c.State = "exited"
				}
				want = "environment_not_running"
			case "local":
				m.mu.Lock()
				m.instances[instance.ID].Execution = ExecutionSpec{Mode: "local"}
				m.mu.Unlock()
				want = "docker_configuration_required"
			case "pending":
				if _, e := m.SetExecution(instance.ID, instance.Revision, ExecutionSpec{Mode: "docker", Image: "new:v2"}); e != nil {
					t.Fatal(e)
				}
				want = "environment_update_required"
			case "changed-during-probe":
				driver.beforeExec = func() {
					if _, e := m.SetExecution(instance.ID, instance.Revision, ExecutionSpec{Mode: "docker", Image: "new:v2"}); e != nil {
						t.Fatal(e)
					}
				}
			}
			_, e := m.DiagnoseApplicationRuntime(context.Background(), "news", in)
			expectDockerCode(t, e, want)
			if attack != "changed-during-probe" && len(driver.argv) != 0 {
				t.Fatal("executed despite rejected binding", driver.argv)
			}
			for _, args := range driver.calls {
				if args[0] == "container" && (args[1] == "rm" || args[1] == "stop") {
					t.Fatal("diagnostics interrupted runtime", args)
				}
			}
		})
	}
}

func TestApplicationRuntimeDiagnosticsScopedAPI(t *testing.T) {
	m, _, instance, workspace, in := diagnosticFixture(t)
	_, token, e := m.CreateApplicationKey("news", KeyInput{Name: "run", WorkspaceIDs: []string{workspace.ID}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	_, readonly, _ := m.CreateApplicationKey("news", KeyInput{Name: "read", WorkspaceIDs: []string{workspace.ID}, Scopes: []string{"read"}})
	handler := NewHandler(m, "admin-secret-01234567890123456789", true)
	body, _ := json.Marshal(in)
	for _, sample := range []struct {
		app, token string
		status     int
	}{{"other", token, 403}, {"news", readonly, 403}, {"news", token, 200}} {
		response := appRequest(handler, "POST", "/api/v1/applications/"+sample.app+"/runtime/diagnostics", string(body), sample.token, "")
		if response.Code != sample.status {
			t.Fatal(sample.app, response.Code, response.Body.String(), instance.ID)
		}
	}
}

func TestApplicationRuntimeDiagnosticsImageMCPActualHTTPAndPrivateAuth(t *testing.T) {
	m, driver, _, workspace, in := diagnosticFixture(t)
	in.Challenge = nil // Native editing still needs actual container-to-MCP visibility.
	in.MCPImageServer = "fixture"
	buffer := &bytes.Buffer{}
	picture := image.NewRGBA(image.Rect(0, 0, 8, 6))
	picture.Set(1, 1, color.RGBA{R: 240, A: 255})
	_ = png.Encode(buffer, picture)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer private-container-token" || r.Header.Get("X-Private") != "private-header-value" {
			t.Error("fixed diagnostic lost actual private credentials")
			w.WriteHeader(401)
			return
		}
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "fixture", "version": "1"}}
		case "notifications/initialized":
			w.WriteHeader(202)
			return
		case "tools/call":
			if request.Params["name"] != "rundesk_diagnostic_image" || request.Params["arguments"].(map[string]any)["nonce"] != in.Nonce {
				t.Error("arbitrary tool called", request.Params)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(buffer.Bytes())}}}
		default:
			t.Error("unexpected MCP method", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer server.Close()
	t.Setenv("DIAGNOSTIC_CONTAINER_TOKEN", "private-container-token")
	config, e := m.readConfig(workspace.ID, in.InstanceID)
	if e != nil {
		t.Fatal(e)
	}
	_, revision := userMCP(config)
	if _, e = m.SaveMCP(workspace.ID, "fixture", revision, map[string]any{"url": server.URL, "bearer_token_env_var": "DIAGNOSTIC_CONTAINER_TOKEN", "http_headers": map[string]any{"X-Private": "private-header-value"}}, false, in.InstanceID); e != nil {
		t.Fatal(e)
	}
	view, e := m.DiagnoseApplicationRuntime(context.Background(), "news", in)
	if e != nil {
		t.Fatal(e)
	}
	proof := view["imageTransport"].(*diagnosticImageResult)
	sum := sha256.Sum256(buffer.Bytes())
	if proof.State != "passed" || proof.ImageSHA256 != hex.EncodeToString(sum[:]) || proof.Width != 8 || proof.Height != 6 || calls.Load() != 3 {
		t.Fatal(proof, calls.Load())
	}
	encoded, _ := json.Marshal(view)
	for _, secret := range []string{server.URL, "private-container-token", "private-header-value", "DIAGNOSTIC_CONTAINER_TOKEN"} {
		if bytes.Contains(encoded, []byte(secret)) || strings.Contains(strings.Join(driver.argv, " "), secret) {
			t.Fatal("credential/endpoint leaked", secret)
		}
	}
}

func TestApplicationRuntimeDiagnosticsCancellation(t *testing.T) {
	m, driver, _, _, in := diagnosticFixture(t)
	driver.probe = "import time; time.sleep(10)"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, e := m.DiagnoseApplicationRuntime(ctx, "news", in)
	expectDockerCode(t, e, "diagnostic_timeout")
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation did not bound fixed process")
	}
}

func TestApplicationRuntimeDiagnosticsDoesNotCreateConfigConnection(t *testing.T) {
	m, driver, _, _, in := diagnosticFixture(t)
	in.Challenge = nil
	in.MCPImageServer = "fixture"
	m.mu.Lock()
	key := "config-" + in.InstanceID + "-" + in.WorkspaceID
	previous := m.handles[key]
	delete(m.handles, key)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.handles[key] = previous
		m.mu.Unlock()
	}()
	count := driver.commands
	view, e := m.DiagnoseApplicationRuntime(context.Background(), "news", in)
	if e != nil || view["probed"] != false || view["accountReady"] != false || view["imageTransport"].(*diagnosticImageResult).Code != "mcp_config_unavailable" || driver.commands != count {
		t.Fatal(view, e, driver.commands, count)
	}
}

func runFixedDiagnosticPython(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	input, _ := json.Marshal(map[string]any{"nonce": fixtureDiagnosticNonce, "mcpConfig": cfg})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", "-I", "-c", applicationRuntimeProbe)
	command.Stdin = bytes.NewReader(input)
	raw, e := command.CombinedOutput()
	if e != nil {
		t.Fatal(e, string(raw))
	}
	var value map[string]any
	if e = json.Unmarshal(raw, &value); e != nil {
		t.Fatal(e, string(raw))
	}
	return value
}

func TestDiagnosticPythonMCPRejectsRedirectSSEAndOversize(t *testing.T) {
	for _, attack := range []string{"redirect", "sse", "oversize", "invalid-image"} {
		t.Run(attack, func(t *testing.T) {
			var downstream atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstream.Add(1)
				_, _ = w.Write([]byte("unexpected credential forwarding"))
			}))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attack == "redirect" {
					http.Redirect(w, r, destination.URL, 307)
					return
				}
				if attack == "sse" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {}\n\n"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if attack == "oversize" {
					_, _ = w.Write(bytes.Repeat([]byte("x"), (2<<20)+1))
					return
				}
				var request map[string]any
				_ = json.NewDecoder(r.Body).Decode(&request)
				if request["method"] == "notifications/initialized" {
					w.WriteHeader(202)
					return
				}
				result := map[string]any{"protocolVersion": "2024-11-05"}
				if request["method"] == "tools/call" {
					result = map[string]any{"content": []any{map[string]string{"type": "image", "mimeType": "image/png", "data": "bm90IGEgcG5n"}}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result})
			}))
			defer server.Close()
			value := runFixedDiagnosticPython(t, map[string]any{"url": server.URL, "http_headers": map[string]any{"Authorization": "private"}})
			proof := value["imageTransport"].(map[string]any)
			if proof["state"] == "passed" || downstream.Load() != 0 {
				t.Fatal("unsafe transport accepted", proof, downstream.Load())
			}
			if attack == "sse" && proof["code"] != "mcp_transport_unsupported" || attack == "redirect" && proof["code"] != "mcp_redirect_rejected" {
				t.Fatal(proof)
			}
		})
	}
}

func TestApplicationRuntimeDiagnosticsHonorsToolConfiguration(t *testing.T) {
	for _, code := range []string{"mcp_server_disabled", "mcp_tool_disabled", "mcp_transport_unsupported"} {
		t.Run(code, func(t *testing.T) {
			m, _, _, workspace, in := diagnosticFixture(t)
			in.Challenge, in.MCPImageServer = nil, "fixture"
			value := map[string]any{"url": "http://127.0.0.1:1/mcp"}
			switch code {
			case "mcp_server_disabled":
				value["enabled"] = false
			case "mcp_tool_disabled":
				value["disabled_tools"] = []any{"rundesk_diagnostic_image"}
			case "mcp_transport_unsupported":
				value = map[string]any{"command": "this-must-never-be-executed", "args": []any{}}
			}
			config, e := m.readConfig(workspace.ID, in.InstanceID)
			if e != nil {
				t.Fatal(e)
			}
			_, revision := userMCP(config)
			if _, e = m.SaveMCP(workspace.ID, "fixture", revision, value, false, in.InstanceID); e != nil {
				t.Fatal(e)
			}
			view, e := m.DiagnoseApplicationRuntime(context.Background(), "news", in)
			if e != nil || view["imageTransport"].(*diagnosticImageResult).Code != code {
				t.Fatal(view, e)
			}
		})
	}
}

func TestDiagnosticPythonActualRuntimeMetadata(t *testing.T) {
	root := t.TempDir()
	profile, vendor := filepath.Join(root, "profile"), filepath.Join(root, "vendor")
	for _, path := range []string{filepath.Join(profile, "tools"), filepath.Join(vendor, "helpers")} {
		if e := os.MkdirAll(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	pin := "43cfc566833548093455c9d4a46bd69913b7e552"
	installed, e := exec.Command("python3", "-I", "-c", "import importlib.metadata; print(importlib.metadata.version('pip'))").Output()
	if e != nil {
		t.Skip("pip metadata required for dependency-lock fixture")
	}
	lock := `{"profile":{"id":"video-build","version":"1.3.0"},"dependencies":{"videoUse":{"python":{"pip":"` + strings.TrimSpace(string(installed)) + `"}}}}`
	for path, text := range map[string]string{
		filepath.Join(profile, "profile.lock.json"):          lock,
		filepath.Join(profile, "tools/video_use_adapter.py"): "print('{\"schemaVersion\":1,\"adapterVersion\":2,\"upstreamCommit\":\"" + pin + "\"}')",
		filepath.Join(vendor, "RUNDESK_PIN.json"):            `{"commit":"` + pin + `"}`,
		filepath.Join(vendor, "LICENSE"):                     "fixture",
		filepath.Join(vendor, "helpers/render.py"):           "# fixture",
	} {
		if e := os.WriteFile(path, []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	program := strings.ReplaceAll(strings.ReplaceAll(applicationRuntimeProbe, "/opt/rundesk-profile", profile), "/opt/video-use", vendor)
	command := exec.Command("python3", "-I", "-c", program)
	command.Stdin = strings.NewReader(`{"nonce":"` + fixtureDiagnosticNonce + `"}`)
	raw, e := command.CombinedOutput()
	if e != nil {
		t.Fatal(e, string(raw))
	}
	var actual struct {
		Runtime diagnosticRuntime `json:"runtime"`
	}
	if e = json.Unmarshal(raw, &actual); e != nil {
		t.Fatal(e, string(raw))
	}
	if actual.Runtime.Status != "ready" || !actual.Runtime.HasVideoUse || actual.Runtime.ProfileVersion != "1.3.0" || actual.Runtime.AdapterContractVersion != 2 || actual.Runtime.VideoUseCommit != pin || actual.Runtime.Checks["sourcePin"] != "ready" || actual.Runtime.Checks["pythonDependencies"] != "ready" || actual.Runtime.PythonVersion == "" || actual.Runtime.FFmpegVersion == "" || actual.Runtime.FFprobeVersion == "" {
		t.Fatal(actual)
	}
	lock = strings.Replace(lock, strings.TrimSpace(string(installed)), "0.0.0-missing", 1)
	if e = os.WriteFile(filepath.Join(profile, "profile.lock.json"), []byte(lock), 0600); e != nil {
		t.Fatal(e)
	}
	command = exec.Command("python3", "-I", "-c", program)
	command.Stdin = strings.NewReader(`{"nonce":"` + fixtureDiagnosticNonce + `"}`)
	raw, e = command.CombinedOutput()
	if e != nil || json.Unmarshal(raw, &actual) != nil || actual.Runtime.Status != "incomplete" || actual.Runtime.Checks["pythonDependencies"] != "invalid" {
		t.Fatal("mismatching installed dependency was accepted", actual, e, string(raw))
	}
}

func TestVideoBuildCurrentAdapterImageLabels(t *testing.T) {
	build := ImageBuild{ProfileID: "video-build", CodexVersion: "0.99.0"}
	var image imageInspection
	image.Config.Labels = map[string]string{"io.rundesk.profile": "video-build", "org.opencontainers.image.version": "1.4.0", "io.rundesk.agent-reach": "false", "io.rundesk.codex.version": "0.99.0", "io.rundesk.hyperframes.version": "0.8.87", "io.rundesk.gsap.version": "3.15.0", "io.rundesk.hyperframes.lock": runtimeHyperframesLockSHA256, "io.rundesk.video-use.enabled": "false"}
	if !runtimeImageMatches(build, image) {
		t.Fatal("current independent HyperFrames image rejected")
	}
	for _, label := range []string{"io.rundesk.hyperframes.version", "io.rundesk.gsap.version", "io.rundesk.hyperframes.lock", "io.rundesk.video-use.enabled"} {
		original := image.Config.Labels[label]
		image.Config.Labels[label] = "missing-fixture"
		if runtimeImageMatches(build, image) {
			t.Fatal("invalid required label accepted", label)
		}
		image.Config.Labels[label] = original
	}
	image.Config.Labels["io.rundesk.video-use.enabled"] = "true"
	if runtimeImageMatches(build, image) {
		t.Fatal("unverified legacy compatibility accepted")
	}
	image.Config.Labels["io.rundesk.video-use.commit"] = "43cfc566833548093455c9d4a46bd69913b7e552"
	image.Config.Labels["io.rundesk.video-use.adapter"] = "2"
	if !runtimeImageMatches(build, image) {
		t.Fatal("current optional legacy compatibility rejected")
	}
	image.Config.Labels["org.opencontainers.image.version"] = "1.3.0"
	if runtimeImageMatches(build, image) {
		t.Fatal("old profile blessed as the new default")
	}
}

// These execute the real fixed Python probe against package, executable and pin
// fixtures. They do not stand in for a Docker, Chromium rendering or model test.
func TestDiagnosticPythonHyperframesMetadataFixtures(t *testing.T) {
	for _, test := range []struct {
		name       string
		legacy     bool
		change     string
		wantStatus string
		wantCheck  string
	}{
		{"default_without_legacy", false, "", "ready", ""},
		{"explicit_legacy_compatibility", true, "", "ready", ""},
		{"missing_hyperframes", false, "hyperframes", "incomplete", "hyperframes"},
		{"mismatched_gsap", false, "gsap", "incomplete", "gsap"},
		{"old_node", false, "node", "incomplete", "node"},
		{"missing_browser", false, "browser", "incomplete", "browser"},
		{"tampered_npm_lock", false, "lock", "incomplete", "hyperframesLock"},
		{"required_legacy_missing", true, "legacy", "incomplete", "sourcePin"},
		{"invalid_selection", false, "selection", "incomplete", "profile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			profile, vendor, hyper, bin := filepath.Join(root, "profile"), filepath.Join(root, "vendor"), filepath.Join(root, "hyperframes"), filepath.Join(root, "bin")
			write := func(path, value string, mode os.FileMode) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(value), mode); err != nil {
					t.Fatal(err)
				}
			}
			pin := "43cfc566833548093455c9d4a46bd69913b7e552"
			lockBytes := `{"lockfileVersion":3,"packages":{}}`
			lock := map[string]any{
				"profile":   map[string]any{"id": "video-build", "version": "1.4.0"},
				"selection": map[string]any{"videoUse": test.legacy},
				"dependencies": map[string]any{
					"hyperframes": map[string]any{"npm": map[string]string{"hyperframes": "0.8.87", "gsap": "3.15.0"}, "npmLockSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(lockBytes)))},
					"videoUse":    map[string]any{"python": map[string]string{"pip": "missing-fixture-version"}},
				},
			}
			if test.legacy {
				installed, err := exec.Command("python3", "-I", "-c", "import importlib.metadata; print(importlib.metadata.version('pip'))").Output()
				if err != nil {
					t.Skip("pip metadata required for legacy fixture")
				}
				lock["dependencies"].(map[string]any)["videoUse"] = map[string]any{"python": map[string]string{"pip": strings.TrimSpace(string(installed))}}
				if test.change != "legacy" {
					write(filepath.Join(vendor, "RUNDESK_PIN.json"), `{"commit":"`+pin+`"}`, 0600)
					write(filepath.Join(vendor, "LICENSE"), "fixture license", 0600)
					write(filepath.Join(vendor, "helpers/render.py"), "# fixture", 0600)
				}
			}
			if test.change == "selection" {
				lock["selection"].(map[string]any)["videoUse"] = "false"
			}
			encoded, _ := json.Marshal(lock)
			write(filepath.Join(profile, "profile.lock.json"), string(encoded), 0600)
			write(filepath.Join(profile, "tools/video_use_adapter.py"), "print('{\"schemaVersion\":1,\"adapterVersion\":2,\"upstreamCommit\":\""+pin+"\"}')", 0600)
			if test.change != "hyperframes" {
				write(filepath.Join(hyper, "node_modules/hyperframes/package.json"), `{"version":"0.8.87"}`, 0600)
			}
			gsap := "3.15.0"
			if test.change == "gsap" {
				gsap = "3.14.0"
			}
			write(filepath.Join(hyper, "node_modules/gsap/package.json"), `{"version":"`+gsap+`"}`, 0600)
			if test.change == "lock" {
				lockBytes += " "
			}
			write(filepath.Join(hyper, "package-lock.json"), lockBytes, 0600)
			node := "v22.1.0"
			if test.change == "node" {
				node = "v20.1.0"
			}
			write(filepath.Join(bin, "node"), "#!/bin/sh\nif [ \"$1\" = --version ]; then echo "+node+"; else echo 0.8.87; fi\n", 0700)
			browser := filepath.Join(bin, "chromium")
			if test.change != "browser" {
				write(browser, "#!/bin/sh\necho 'Chromium 140.0.7339.80 built on Debian GNU/Linux 12'\n", 0700)
			}
			program := strings.NewReplacer("/opt/rundesk-profile", profile, "/opt/video-use", vendor, "/opt/hyperframes", hyper, "/usr/bin/chromium", browser).Replace(applicationRuntimeProbe)
			command := exec.Command("python3", "-I", "-c", program)
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			command.Stdin = strings.NewReader(`{"nonce":"` + fixtureDiagnosticNonce + `"}`)
			raw, err := command.CombinedOutput()
			var actual struct {
				Runtime diagnosticRuntime `json:"runtime"`
			}
			if err != nil || json.Unmarshal(raw, &actual) != nil {
				t.Fatal(err, string(raw))
			}
			value := actual.Runtime
			if value.Status != test.wantStatus || value.ProfileVersion != "1.4.0" || value.AdapterContractVersion != 2 || value.VideoUseCommit != pin {
				t.Fatal(value)
			}
			if test.wantCheck != "" && value.Checks[test.wantCheck] == "ready" {
				t.Fatal("broken required component was accepted", value)
			}
			if !test.legacy && test.change != "selection" && (value.HasVideoUse || value.Checks["sourcePin"] != "not_required" || value.Checks["pythonDependencies"] != "not_required") {
				t.Fatal("default unexpectedly required legacy tools", value)
			}
			if test.wantStatus == "ready" && (value.HyperframesVersion != "0.8.87" || value.GSAPVersion != "3.15.0" || value.NodeVersion != "v22.1.0" || value.BrowserVersion != "140.0.7339.80" || value.HasVideoUse != test.legacy) {
				t.Fatal(value)
			}
		})
	}
}
