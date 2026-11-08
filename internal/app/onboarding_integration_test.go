package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

func TestOnboardingFirstRunSurvivesEarlySessionAndCompletionPreservesBackend(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.Codex = "not-installed-codex"
	work := m.Workspaces()[0]
	if _, err := m.CreateSession(work.ID, "early session", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := m.Instance()
	h := NewHandler(m, "", true)
	r := v1Request(h, "GET", "/setup", "", "")
	if r.Code != 200 || object(t, r)["firstRun"] != true {
		t.Fatal(r.Code, r.Body.String())
	}
	draft := false
	saved, err := m.completeSetup(SetupCompleteInput{Revision: 0, WorkspaceID: work.ID, Completed: &draft})
	if err != nil || saved.Completed || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	saved, err = m.completeSetup(SetupCompleteInput{Revision: 1, WorkspaceID: work.ID})
	if err != nil || !saved.Completed || saved.Revision != 2 {
		t.Fatal(saved, err)
	}
	after, _ := m.Instance()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("completion changed default instance")
	}
	if _, err = m.completeSetup(SetupCompleteInput{Revision: 1}); err == nil {
		t.Fatal("stale completion accepted")
	}
	r = v1Request(h, "GET", "/setup", "", "")
	if r.Code != 200 || object(t, r)["firstRun"] != false {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestKunBuildAssistantFrozenRepairAndNativeToolConfiguration(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	instance, err := m.CreateInstance(InstancePatch{Name: "Kun build fixture"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err = m.SetAgentRuntime(instance.ID, instance.Revision, p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:8000/v1", Model: "fixture", AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	data := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\n", "tools/smoke.sh": "#!/bin/sh\ntrue\n"})
	source := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "recipe", Status: "failed", Dockerfile: "Dockerfile", ContextSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH, RecipeSmoke: &RecipeSmokeSpec{Command: []string{"sh", "tools/smoke.sh"}, TimeoutSeconds: 30, ExpectedFiles: []string{}}, Error: "fixture package error"}
	if err = os.MkdirAll(m.buildDir(source.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(m.buildDir(source.ID), "context.zip"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Store.Put("image-build", source.ID, source); err != nil {
		t.Fatal(err)
	}
	input := BuildAssistantInput{Prompt: "repair package", InstanceID: instance.ID, SourceBuildID: source.ID, RequestID: store.ID()}
	run, err := m.CreateBuildAssistantRun(input, "http://127.0.0.1:3210/api/build-center/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if run.RuntimeKind != "kun" || run.SourceBuildID != source.ID {
		t.Fatal(run)
	}
	session, _ := m.Session(run.SessionID)
	workspace, _ := m.Workspace(run.WorkspaceID)
	secret, err := os.ReadFile(m.buildAssistantSecretPath(run.ID))
	if err != nil {
		t.Fatal(err)
	}
	cap, err := m.buildAssistantCapability(string(secret))
	if err != nil {
		t.Fatal(err)
	}
	request, err := m.kunStartRequest(session, workspace, Input{Text: "repair"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Config.AllowBash || !request.Config.AllowWrite || len(request.MCP) != 1 || len(request.Skills) != 1 {
		t.Fatal(request.Config, len(request.MCP), len(request.Skills))
	}
	server := request.MCP[0]
	if err = server.Validate(); err != nil {
		t.Fatal(err)
	}
	if server.Headers["Authorization"] != "Bearer "+string(secret) || !server.Allowlist || !strings.Contains(request.Skills[0].Content, "build_context_pack") {
		t.Fatal("missing scoped capability or skill")
	}
	if strings.Contains(request.Input, string(secret)) {
		t.Fatal("capability in model prompt")
	}
	if _, err = m.assistantBuild(cap, source.ID); err != nil {
		t.Fatal("source not readable", err)
	}
	if _, err = m.callBuildAssistantTool(cap, "build_cancel", buildAssistantToolArgs{BuildID: source.ID}); err == nil {
		t.Fatal("source build mutable")
	}
	path := filepath.Join(workspace.Path, "artifacts", "context", "Dockerfile")
	if err = os.WriteFile(path, []byte("FROM scratch\nLABEL repaired=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.callBuildAssistantTool(cap, "build_context_pack", buildAssistantToolArgs{ContextDirectory: "artifacts/context", ContextPath: "artifacts/context.zip"}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(filepath.Join(workspace.Path, "artifacts", "context.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	found := false
	for _, file := range archive.File {
		if file.Name == "Dockerfile" {
			f, _ := file.Open()
			content, _ := io.ReadAll(f)
			f.Close()
			found = strings.Contains(string(content), "repaired=true")
		}
	}
	if !found {
		t.Fatal("patched context absent")
	}
	frozen, _ := os.ReadFile(filepath.Join(m.buildDir(source.ID), "context.zip"))
	if !bytes.Equal(frozen, data) {
		t.Fatal("original context changed")
	}
	manifest := BuildRecipeManifest{}
	encoded, _ := os.ReadFile(filepath.Join(workspace.Path, "artifacts", "recipe.json"))
	if err = json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Smoke.Command = []string{"true"}
	encoded, _ = json.Marshal(manifest)
	os.WriteFile(filepath.Join(workspace.Path, "artifacts", "recipe.json"), encoded, 0600)
	if _, err = m.callBuildAssistantTool(cap, "build_recipe_import", buildAssistantToolArgs{ManifestPath: "artifacts/recipe.json", ContextPath: "artifacts/context.zip", RequestID: store.ID()}); err == nil {
		t.Fatal("weakened smoke accepted")
	}
	if runtime.GOOS != "windows" {
		if err = os.Symlink(t.TempDir(), filepath.Join(workspace.Path, "artifacts", "context", "outside")); err != nil {
			t.Fatal(err)
		}
		if _, err = m.assistantPackContext(cap, "artifacts/context", "artifacts/symlink.zip"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	if _, err = m.assistantPackContext(cap, "../outside", "artifacts/no.zip"); err == nil {
		t.Fatal("traversal accepted")
	}
	m.mu.Lock()
	m.instances[instance.ID].Permissions.Sandbox = "read-only"
	m.mu.Unlock()
	if _, err = m.kunStartRequest(session, workspace, Input{Text: "repair"}); err == nil {
		t.Fatal("changed permissions were overridden")
	}
}

func TestBuildRepairPreservesFrozenSourcesAndRechecksProfileIdentity(t *testing.T) {
	m := quietRecipeManager(t)
	recipe, err := m.BuildCenterRecipe("builtin-news2douyin")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	original, err := m.StartRecipeBuild(recipe.ID, RecipeBuildInput{RecipeRevision: recipe.Revision, CodexVersion: "0.157.1", RequestID: store.ID(), BuildSources: &BuildSourceOptions{Group: "china", CatalogRevision: catalog.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	original.Status = "failed"
	original.Error = "fixture packaging failure"
	if err = m.Store.Put("image-build", original.ID, original); err != nil {
		t.Fatal(err)
	}
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(original.ID), "context.zip"), original.ContextSHA256)
	if err != nil {
		t.Fatal(err)
	}
	manifest := recipe.BuildRecipeManifest
	manifest.Name = "Repair frozen news profile"
	imported, err := m.ImportBuildRecipe(manifest, bytes.NewReader(data), store.ID(), "fixture-repair")
	if err != nil {
		t.Fatal(err)
	}
	// Current defaults may change after failure; the repair keeps the selected
	// historical addresses unless the agent explicitly chooses new sources.
	catalog.DefaultGroup = "international"
	if _, err = m.SaveBuildSources(catalog); err != nil {
		t.Fatal(err)
	}
	repaired, err := m.StartRecipeBuild(imported.ID, RecipeBuildInput{RecipeRevision: imported.Revision, OwnerRunID: "fixture-repair", RepairOf: original.ID, RequestID: store.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if repaired.RebuildOf != original.ID || repaired.CodexVersion != original.CodexVersion || repaired.ProfileID != original.ProfileID || repaired.RuntimeIdentity == nil || !reflect.DeepEqual(repaired.RuntimeIdentity, original.RuntimeIdentity) || !reflect.DeepEqual(repaired.BuildSources, original.BuildSources) {
		t.Fatalf("repair lost frozen identity or source configuration: %+v", repaired)
	}
}
