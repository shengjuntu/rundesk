package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func buildAssistantFixture(t *testing.T) (*Manager, BuildAssistantRun, string, buildAssistantCapability) {
	t.Helper()
	m := testManager(t)
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	run, err := m.CreateBuildAssistantRun(BuildAssistantInput{Prompt: "Make a reusable research app", RequestID: store.ID()}, "http://127.0.0.1:3210/api/build-center/mcp")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(m.buildAssistantSecretPath(run.ID))
	if err != nil {
		t.Fatal(err)
	}
	cap, err := m.buildAssistantCapability(string(secret))
	if err != nil {
		t.Fatal(err)
	}
	return m, run, string(secret), cap
}
func buildAssistantMCPRequest(s *Server, token, method string, params any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	r := httptest.NewRequest("POST", "http://localhost/api/build-center/mcp", bytes.NewReader(data))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.buildAssistantMCP(w, r)
	return w
}
func TestBuildAssistantAdmissionConcurrentRetryAndNoExistingMutation(t *testing.T) {
	m := testManager(t)
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	parent := m.Workspaces()[0]
	beforeInstances := m.Instances()
	beforeWorkspaces := len(m.Workspaces())
	beforeTasks := len(m.Tasks())
	in := BuildAssistantInput{Prompt: "Create a pinned text search app", WorkspaceID: parent.ID, RequestID: store.ID()}
	var wg sync.WaitGroup
	ids := make(chan BuildAssistantRun, 8)
	failures := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := m.CreateBuildAssistantRun(in, "http://127.0.0.1:3210/api/build-center/mcp")
			if err != nil {
				failures <- err
				return
			}
			ids <- run
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first BuildAssistantRun
	for run := range ids {
		if first.ID == "" {
			first = run
		}
		if first.ID != run.ID || first.TaskID != run.TaskID || first.SessionID != run.SessionID {
			t.Fatal("retry duplicated work", first, run)
		}
	}
	if len(m.Tasks()) != beforeTasks+1 || len(m.Workspaces()) != beforeWorkspaces+1 {
		t.Fatal("duplicate task/workspace")
	}
	if first.WorkspaceID == parent.ID || first.ParentWorkspaceID != parent.ID || !first.SkillBootstrap || first.NativePluginInstalled {
		t.Fatal("incorrect bootstrap state", first)
	}
	w, _ := m.Workspace(first.WorkspaceID)
	if _, err := os.Stat(filepath.Join(w.Path, ".agents", "skills", "add-app-docker-image", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.Path, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("persistent config unexpectedly written")
	}
	secret, _ := os.ReadFile(m.buildAssistantSecretPath(first.ID))
	encoded, _ := json.Marshal(first)
	task, _ := m.Task(first.TaskID)
	taskJSON, _ := json.Marshal(task)
	if bytes.Contains(encoded, secret) || bytes.Contains(taskJSON, secret) {
		t.Fatal("capability leaked in response or task prompt")
	}
	after, _ := json.Marshal(m.Instances())
	before, _ := json.Marshal(beforeInstances)
	if !bytes.Equal(after, before) {
		t.Fatal("existing instance was changed")
	}
	in.Prompt = "different requirement"
	if _, err := m.CreateBuildAssistantRun(in, "http://127.0.0.1:3210/api/build-center/mcp"); err == nil {
		t.Fatal("same ID accepted different prompt")
	}
}
func TestBuildAssistantPartialAdmissionReusesWorkspace(t *testing.T) {
	m := testManager(t)
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	in := BuildAssistantInput{Prompt: "Build from a project", RequestID: store.ID()}
	id := buildAssistantDigest("assistant:" + in.RequestID)[:24]
	// Simulate a crash between the durable request receipt/workspace checkpoint
	// and the atomic task/session/run admission.
	normalized := in
	normalized.InstanceID = DefaultInstance
	if err := m.Store.Put("build-assistant-admission", id, map[string]string{"fingerprint": buildAssistantDigest(normalized)}); err != nil {
		t.Fatal(err)
	}
	workspace, err := m.ensureBuildAssistantWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	count := len(m.Workspaces())
	run, err := m.CreateBuildAssistantRun(in, "http://localhost:3210/api/build-center/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if run.WorkspaceID != workspace.ID || len(m.Workspaces()) != count || len(m.Tasks()) != 1 {
		t.Fatal("partial retry duplicated workspace or task", run)
	}
}
func TestBuildAssistantCapabilityExpiryRevocationAndExactBinding(t *testing.T) {
	t.Run("revocation invalidates admitted copy", func(t *testing.T) {
		m, run, token, cap := buildAssistantFixture(t)
		if _, err := m.RevokeBuildAssistantRun(run.ID); err != nil {
			t.Fatal(err)
		}
		if err := m.checkBuildAssistantCapability(cap); err == nil {
			t.Fatal("stale admitted grant survived revoke")
		}
		if _, err := m.buildAssistantCapability(token); err == nil {
			t.Fatal("revoked token accepted")
		}
		if _, err := os.Stat(m.buildAssistantSecretPath(run.ID)); !os.IsNotExist(err) {
			t.Fatal("revoked secret file retained")
		}
	})
	t.Run("expiry", func(t *testing.T) {
		m, _, token, cap := buildAssistantFixture(t)
		cap.ExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
		if err := m.Store.Put("build-assistant-capability", keyHash(token), cap); err != nil {
			t.Fatal(err)
		}
		if _, err := m.buildAssistantCapability(token); err == nil {
			t.Fatal("expired token accepted")
		}
	})
	t.Run("workspace changed", func(t *testing.T) {
		m, run, token, _ := buildAssistantFixture(t)
		m.mu.Lock()
		m.workspaces[run.WorkspaceID].Path = t.TempDir()
		m.mu.Unlock()
		if _, err := m.buildAssistantCapability(token); err == nil {
			t.Fatal("moved workspace accepted")
		}
	})
	t.Run("session changed", func(t *testing.T) {
		m, run, token, _ := buildAssistantFixture(t)
		other, _ := m.CreateWorkspace("other", "")
		if err := m.update(run.SessionID, func(s *Session) { s.WorkspaceID = other.ID }); err != nil {
			t.Fatal(err)
		}
		if _, err := m.buildAssistantCapability(token); err == nil {
			t.Fatal("foreign session workspace accepted")
		}
	})
	t.Run("task completed", func(t *testing.T) {
		m, run, token, cap := buildAssistantFixture(t)
		m.queueMu.Lock()
		task := m.tasks[run.TaskID]
		task.Status = "completed"
		m.tasks[task.ID] = task
		m.queueMu.Unlock()
		if _, err := m.buildAssistantCapability(token); err == nil {
			t.Fatal("completed task retained token")
		}
		s, _ := m.Session(run.SessionID)
		if err := m.checkBuildAssistantExecution(s); err == nil {
			t.Fatal("completed assistant allowed another turn")
		}
		if err := m.checkBuildAssistantBuildExecution(ImageBuild{OwnerRunID: cap.RunID}); err == nil {
			t.Fatal("completed task allowed queued Docker work")
		}
	})
	t.Run("session completed before queue reconciliation", func(t *testing.T) {
		m, run, token, _ := buildAssistantFixture(t)
		if err := m.update(run.SessionID, func(s *Session) { s.Status = "completed" }); err != nil {
			t.Fatal(err)
		}
		if _, err := m.buildAssistantCapability(token); err == nil {
			t.Fatal("terminal session retained capability while task was pending")
		}
		current, err := m.BuildAssistantRun(run.ID)
		if err != nil || current.RevokedAt == "" {
			t.Fatal("run query did not report revocation", current, err)
		}
	})
}
func TestBuildAssistantNativeOverrideAndNarrowMCP(t *testing.T) {
	m, run, token, _ := buildAssistantFixture(t)
	s, _ := m.Session(run.SessionID)
	w, _ := m.Workspace(run.WorkspaceID)
	params := map[string]any{"sandbox": "danger-full-access", "config": map[string]any{"model_reasoning_effort": "high"}}
	if err := m.configureBuildAssistant(s, w, params); err != nil {
		t.Fatal(err)
	}
	config := params["config"].(map[string]any)
	server := config["mcp_servers.rundesk_build_center"].(map[string]any)
	if params["sandbox"] != "workspace-write" || server["http_headers"].(map[string]string)["Authorization"] != "Bearer "+token || config["model_reasoning_effort"] != "high" {
		t.Fatal("wrong native configuration")
	}
	masked, _ := json.Marshal(redact(params))
	if bytes.Contains(masked, []byte(token)) {
		t.Fatal("native trace leaked capability")
	}
	handler := &Server{Manager: m}
	if response := buildAssistantMCPRequest(handler, "", "tools/list", map[string]any{}); response.Code != 401 {
		t.Fatal("unauthenticated MCP accepted", response.Code)
	}
	if response := buildAssistantMCPRequest(handler, "administrator-token", "tools/list", map[string]any{}); response.Code != 401 {
		t.Fatal("administrator token accepted as task capability")
	}
	response := buildAssistantMCPRequest(handler, token, "tools/list", map[string]any{})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, bad := range []string{"image_delete", "instance_update", "plugin_install", "config_read", "build_rebuild"} {
		if _, err := parseBuildAssistantArgs(bad, []byte(`{}`)); err == nil {
			t.Fatal("unauthorized tool", bad)
		}
	}
	if _, err := parseBuildAssistantArgs("build_start", []byte(`{"recipeId":"builtin-video-build","recipeRevision":1,"requestId":"123456789012345678901234","reference":"existing:tag"}`)); err == nil {
		t.Fatal("arbitrary image tag accepted")
	}
	full := NewHandler(m, "admin-private-token", true)
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	request := httptest.NewRequest("POST", "http://localhost/api/build-center/mcp", bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer "+token)
	reply := httptest.NewRecorder()
	full.ServeHTTP(reply, request)
	if reply.Code != 200 {
		t.Fatal("main HTTP gate did not admit scoped MCP", reply.Code, reply.Body.String())
	}
	foreign := httptest.NewRequest("GET", "http://localhost/api/build-center/recipes", nil)
	foreign.Header.Set("Authorization", "Bearer "+token)
	reply = httptest.NewRecorder()
	full.ServeHTTP(reply, foreign)
	if reply.Code == 200 {
		t.Fatal("capability escalated to administrator routes")
	}
}
func assistantRecipeArtifacts(t *testing.T, m *Manager, cap buildAssistantCapability) (BuildRecipeManifest, []byte) {
	t.Helper()
	manifest := BuildRecipeManifest{SchemaVersion: 1, Name: "Fixture app", Version: "1.0.0", Dockerfile: "Dockerfile", Smoke: RecipeSmokeSpec{Command: []string{"true"}, TimeoutSeconds: 5}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(cap.WorkspacePath, "artifacts", "recipe.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var zipped bytes.Buffer
	writer := zip.NewWriter(&zipped)
	file, _ := writer.Create("Dockerfile")
	_, _ = file.Write([]byte("FROM scratch\n"))
	_ = writer.Close()
	if err := os.WriteFile(filepath.Join(cap.WorkspacePath, "artifacts", "context.zip"), zipped.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return manifest, zipped.Bytes()
}
func TestBuildAssistantOwnArtifactsRecipeIdempotencyAndIsolation(t *testing.T) {
	m, _, _, cap := buildAssistantFixture(t)
	assistantRecipeArtifacts(t, m, cap)
	args := buildAssistantToolArgs{ManifestPath: "artifacts/recipe.json", ContextPath: "artifacts/context.zip", RequestID: store.ID()}
	result, err := m.callBuildAssistantTool(cap, "build_recipe_import", args)
	if err != nil {
		t.Fatal(err)
	}
	recipe := result.(BuildRecipe)
	retry, err := m.callBuildAssistantTool(cap, "build_recipe_import", args)
	if err != nil || retry.(BuildRecipe).ID != recipe.ID || recipe.OwnerRunID != cap.RunID {
		t.Fatal("import not task-scoped and idempotent", retry, err)
	}
	foreign := cap
	foreign.RunID = store.ID()
	if _, err := m.assistantRecipe(foreign, recipe.ID); err == nil {
		t.Fatal("foreign recipe read accepted")
	}
	for _, path := range []string{"../state.db", "artifacts/../../state.db", "/etc/passwd"} {
		if _, err := m.openAssistantArtifact(cap, path, 256<<10); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("outside"), 0600)
	if err := os.Symlink(outside, filepath.Join(cap.WorkspacePath, "artifacts", "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.openAssistantArtifact(cap, "artifacts/link.txt", 256<<10); err == nil {
		t.Fatal("external symlink read accepted")
	}
	if _, err := m.callBuildAssistantTool(cap, "build_artifact_read", buildAssistantToolArgs{Path: "artifacts/context.zip"}); err == nil {
		t.Fatal("binary artifact accepted as text")
	}
	foreignBuild := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, OwnerRunID: store.ID(), Scope: "recipe", Status: "draft"}
	_ = m.Store.Put("image-build", foreignBuild.ID, foreignBuild)
	for _, tool := range []string{"build_get", "build_log", "build_cancel", "build_image_check", "build_image_test"} {
		if _, err := m.callBuildAssistantTool(cap, tool, buildAssistantToolArgs{BuildID: foreignBuild.ID, RequestID: store.ID(), Kind: "smoke"}); err == nil {
			t.Fatal("foreign build operation accepted", tool)
		}
	}
	result, err = m.callBuildAssistantTool(cap, "build_start", buildAssistantToolArgs{RecipeID: recipe.ID, RecipeRevision: recipe.Revision, RequestID: store.ID()})
	if err != nil {
		t.Fatal(err)
	}
	b := result.(ImageBuild)
	if b.OwnerRunID != cap.RunID || !strings.HasPrefix(b.Reference, "rundesk-assistant/"+cap.RunID+":") {
		t.Fatal("build reused a shared image reference", b)
	}
	_, _ = m.CancelBuild(b.InstanceID, b.ID)
}
func TestBuildAssistantSanitizedLogsBeforePagination(t *testing.T) {
	m, _, token, cap := buildAssistantFixture(t)
	b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, OwnerRunID: cap.RunID, Scope: "recipe", Status: "running"}
	_ = m.Store.Put("image-build", b.ID, b)
	_ = os.MkdirAll(m.buildDir(b.ID), 0700)
	raw := fmt.Sprintf("API KEY=private-first-secret\nhttps://name:private-url-secret@example.com/log\nAuthorization: Bearer %s\nnormal output\nTOKEN=unfinished-secret", token)
	_ = os.WriteFile(filepath.Join(m.buildDir(b.ID), "build.log"), []byte(raw), 0600)
	text := ""
	offset := 0
	for n := 0; n < 100; n++ {
		value, err := m.callBuildAssistantTool(cap, "build_log", buildAssistantToolArgs{BuildID: b.ID, Offset: offset, Limit: 7})
		if err != nil {
			t.Fatal(err)
		}
		page := value.(map[string]any)
		text += page["text"].(string)
		offset = page["nextOffset"].(int)
		if !page["hasMore"].(bool) {
			break
		}
	}
	for _, secret := range []string{"private-first-secret", "private-url-secret", token, "unfinished-secret"} {
		if strings.Contains(text, secret) {
			t.Fatal("log credential exposed", secret)
		}
	}
	if !strings.Contains(text, "normal output") || !strings.Contains(text, "[redacted]") {
		t.Fatal("sanitized logs lost safe evidence", text)
	}
	jsonSecret := `{"api_key":"private-json-secret","Cookie":"session=private-cookie-secret"}`
	sanitized := sanitizeBuildAssistantText(jsonSecret, "")
	if strings.Contains(sanitized, "private-json-secret") || strings.Contains(sanitized, "private-cookie-secret") {
		t.Fatal("quoted text artifact credential exposed", sanitized)
	}
}
func TestBuildAssistantDockerCallbackRequiresReachability(t *testing.T) {
	t.Setenv("RUNDESK_BUILD_MCP_ORIGIN", "")
	server := &Server{}
	i := Instance{Execution: ExecutionSpec{Mode: "docker", Network: "bridge"}}
	req := httptest.NewRequest("POST", "http://localhost:3210/api/build-center/assistant-runs", nil)
	if _, err := server.buildAssistantCallback(req, i); err == nil {
		t.Fatal("implicit Docker loopback accepted")
	}
	t.Setenv("RUNDESK_BUILD_MCP_ORIGIN", "http://127.0.0.1:3210")
	if _, err := server.buildAssistantCallback(req, i); err == nil {
		t.Fatal("explicit Docker loopback accepted")
	}
	t.Setenv("RUNDESK_BUILD_MCP_ORIGIN", "http://host.docker.internal:3210")
	endpoint, err := server.buildAssistantCallback(req, i)
	if err != nil || endpoint != "http://host.docker.internal:3210/api/build-center/mcp" {
		t.Fatal(endpoint, err)
	}
	i.Execution.Network = "none"
	if _, err := server.buildAssistantCallback(req, i); err == nil {
		t.Fatal("network-disabled instance accepted")
	}
}

func TestBuildAssistantRevokedQueuedBuildNeverStartsDocker(t *testing.T) {
	m, run, _, cap := buildAssistantFixture(t)
	driver := &buildTestDocker{fakeDocker: &fakeDocker{}, mode: "success"}
	m.docker = driver
	if _, err := m.RevokeBuildAssistantRun(run.ID); err != nil {
		t.Fatal(err)
	}
	b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, OwnerRunID: cap.RunID, Scope: "recipe", Status: "queued", Created: store.Now(), Updated: store.Now()}
	if err := m.Store.Put("image-build", b.ID, b); err != nil {
		t.Fatal(err)
	}
	m.runNextBuild()
	current, err := m.BuildCenterBuild(b.ID)
	if err != nil || current.Status != "canceled" || current.Finished == "" {
		t.Fatal("revoked queued build was not canceled", current, err)
	}
	driver.lock.Lock()
	starts := driver.starts
	driver.lock.Unlock()
	if starts != 0 {
		t.Fatal("revoked build launched Docker", starts)
	}
}
