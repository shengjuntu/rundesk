package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func recipeTestManifest() BuildRecipeManifest {
	return BuildRecipeManifest{SchemaVersion: 1, Name: "Research tools", Version: "1.0.0", Smoke: RecipeSmokeSpec{Command: []string{"python3", "--version"}, ExpectedFiles: []string{"/opt/tools/README.md"}}}
}

func quietRecipeManager(t *testing.T) *Manager {
	t.Helper()
	m := testManager(t)
	// These tests verify persistence and queue inputs without executing Docker.
	m.cancel()
	m.wg.Wait()
	return m
}

func TestBuildCenterRecipeImportsAndImmutableSnapshots(t *testing.T) {
	m := quietRecipeManager(t)
	items, err := m.BuildCenterRecipes()
	if err != nil || len(items) != 6 {
		t.Fatal(items, err)
	}
	for _, item := range items {
		if item.Kind != "builtin" || item.Revision != 1 || item.ContextSHA256 == "" || item.RecipeSHA == "" {
			t.Fatal(item)
		}
		if _, err = m.ArchiveBuildRecipe(item.ID, 1); err == nil {
			t.Fatal("builtin recipe was writable")
		}
	}
	data := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\n", "tools/README.md": "local tools", ".env.example": "SERVICE_URL="})
	requestID := store.ID()
	r, err := m.ImportBuildRecipe(recipeTestManifest(), bytes.NewReader(data), requestID, "run-1")
	if err != nil || r.Kind != "custom" || r.Revision != 1 || r.OwnerRunID != "run-1" {
		t.Fatal(r, err)
	}
	if len(r.ContextSHA256) != 64 || len(r.RecipeSHA) != 64 || r.Dockerfile != "Dockerfile" || r.Smoke.TimeoutSeconds != 60 {
		t.Fatal(r)
	}
	again, err := m.ImportBuildRecipe(recipeTestManifest(), bytes.NewReader(data), requestID, "run-1")
	if err != nil || again.ID != r.ID {
		t.Fatal("import was not idempotent", again, err)
	}
	changed := recipeTestManifest()
	changed.Name = "Different request"
	if _, err = m.ImportBuildRecipe(changed, bytes.NewReader(data), requestID, "run-1"); err == nil {
		t.Fatal("conflicting request ID accepted")
	}
	manifestBefore, err := os.ReadFile(filepath.Join(m.recipeSnapshotDir(r.ID, 1), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ArchiveBuildRecipe(r.ID, 2); err == nil {
		t.Fatal("stale recipe revision accepted")
	}
	archived, err := m.ArchiveBuildRecipe(r.ID, 1)
	if err != nil || !archived.Archived {
		t.Fatal(archived, err)
	}
	manifestAfter, _ := os.ReadFile(filepath.Join(m.recipeSnapshotDir(r.ID, 1), "manifest.json"))
	if !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatal("archive changed immutable snapshot")
	}
	if _, err = m.StartRecipeBuild(r.ID, RecipeBuildInput{RecipeRevision: 1, RequestID: store.ID()}); err == nil {
		t.Fatal("archived recipe built")
	}
	items, err = m.BuildCenterRecipes()
	if err != nil || len(items) != 7 || !items[6].Archived {
		t.Fatal(items, err)
	}
}

func TestBuildCenterRecipeArchiveSafetyAndSecrets(t *testing.T) {
	m := quietRecipeManager(t)
	for _, files := range []map[string]string{
		{"Dockerfile": "FROM scratch", "../escape": "x"},
		{"Dockerfile": "FROM scratch", "dockerfile": "FROM busybox"},
		{"Dockerfile": "FROM scratch", ".env": "API_KEY=secret"},
		{"Dockerfile": "FROM scratch", "nested/token.json": "secret"},
		{"Dockerfile": "FROM scratch", ".codex/auth.json": "secret"},
		{"Dockerfile": "FROM scratch", "source.txt": "github_pat_" + strings.Repeat("a", 60)},
	} {
		if _, err := m.ImportBuildRecipe(recipeTestManifest(), bytes.NewReader(buildZip(t, files)), store.ID(), ""); err == nil {
			t.Fatal("unsafe context accepted", files)
		}
	}
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	h := &zip.FileHeader{Name: "Dockerfile"}
	h.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(h)
	_, _ = w.Write([]byte("/etc/passwd"))
	_ = z.Close()
	if _, err := m.ImportBuildRecipe(recipeTestManifest(), &data, store.ID(), ""); err == nil {
		t.Fatal("symbolic link accepted")
	}
	bad := recipeTestManifest()
	bad.Smoke.ExpectedFiles = []string{"/../outside"}
	if _, err := m.ImportBuildRecipe(bad, bytes.NewReader(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"})), store.ID(), ""); err == nil {
		t.Fatal("unsafe smoke path accepted")
	}
}

func TestBuildCenterRecipeBuildAndFrozenRebuild(t *testing.T) {
	m := quietRecipeManager(t)
	before, err := m.Instance(DefaultInstance)
	if err != nil {
		t.Fatal(err)
	}
	data := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\n", "opt/tools/README.md": "frozen"})
	r, err := m.ImportBuildRecipe(recipeTestManifest(), bytes.NewReader(data), store.ID(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.StartRecipeBuild(r.ID, RecipeBuildInput{RecipeRevision: 1, RequestID: store.ID(), OwnerRunID: "foreign"}); err == nil {
		t.Fatal("foreign recipe accepted")
	}
	request := RecipeBuildInput{RecipeRevision: 1, RequestID: store.ID(), OwnerRunID: "run-1"}
	b, err := m.StartRecipeBuild(r.ID, request)
	if err != nil || b.Scope != "recipe" || b.InstanceID != DefaultInstance || b.Status != "queued" || b.RecipeID != r.ID || b.RecipeSHA != r.RecipeSHA || b.OwnerRunID != "run-1" {
		t.Fatal(b, err)
	}
	again, err := m.StartRecipeBuild(r.ID, request)
	if err != nil || again.ID != b.ID {
		t.Fatal(again, err)
	}
	// The result row recovers an interrupted write of the request index.
	if err = m.Store.Delete("build-center-request", request.RequestID); err != nil {
		t.Fatal(err)
	}
	again, err = m.StartRecipeBuild(r.ID, request)
	if err != nil || again.ID != b.ID {
		t.Fatal("request recovery failed", again, err)
	}
	if _, err = m.CancelBuild(b.InstanceID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(m.buildDir(b.ID), "context")); !os.IsNotExist(err) {
		t.Fatal("extracted context retained after cancel")
	}
	if _, err = m.ArchiveBuildRecipe(r.ID, 1); err != nil {
		t.Fatal(err)
	}
	// Rebuild comes from the original build snapshot, even after recipe archival.
	rebuildRequest := RebuildCenterBuildInput{RequestID: store.ID(), NoCache: true}
	rebuilt, err := m.RebuildCenterBuild(b.ID, rebuildRequest)
	if err != nil || rebuilt.ID == b.ID || rebuilt.ContextSHA256 != b.ContextSHA256 || rebuilt.RecipeSHA != b.RecipeSHA || rebuilt.RebuildOf != b.ID || !rebuilt.NoCache || rebuilt.OwnerRunID != "" {
		t.Fatal(rebuilt, err)
	}
	replayed, err := m.RebuildCenterBuild(b.ID, rebuildRequest)
	if err != nil || replayed.ID != rebuilt.ID {
		t.Fatal(replayed, err)
	}
	after, _ := m.Instance(DefaultInstance)
	if before.Execution != after.Execution || before.Revision != after.Revision {
		t.Fatal("platform build changed default assistant", before, after)
	}
	if _, err = m.CreateBuild(DefaultInstance, "", "Dockerfile", 1, false, bytes.NewReader(data)); err == nil {
		t.Fatal("ordinary default assistant upload accepted")
	}
	// A frozen source is verified by content hash before reuse.
	if err = os.Chmod(filepath.Join(m.buildDir(b.ID), "context.zip"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(m.buildDir(b.ID), "context.zip"), []byte("changed"), 0400); err != nil {
		t.Fatal(err)
	}
	if _, err = m.RebuildCenterBuild(b.ID, RebuildCenterBuildInput{RequestID: store.ID()}); err == nil {
		t.Fatal("tampered frozen build accepted")
	}
}

func TestBuildCenterRecipeHTTPContract(t *testing.T) {
	m := quietRecipeManager(t)
	mux := http.NewServeMux()
	s := &Server{Manager: m}
	s.buildCenterRecipeRoutes(mux)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	manifest, _ := json.Marshal(recipeTestManifest())
	_ = form.WriteField("manifest", string(manifest))
	_ = form.WriteField("requestId", store.ID())
	part, err := form.CreateFormFile("context", "context.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"}))
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/build-center/recipes", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var recipe BuildRecipe
	if err = json.Unmarshal(rec.Body.Bytes(), &recipe); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/build-center/recipes", "/api/build-center/recipes/" + recipe.ID, "/api/build-center/builds"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if rec.Code != 200 {
			t.Fatal(endpoint, rec.Code, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/build-center/recipes/"+recipe.ID+"/build", strings.NewReader(`{"recipeRevision":1,"requestId":"`+store.ID()+`"}`)))
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var build ImageBuild
	if err = json.Unmarshal(rec.Body.Bytes(), &build); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/build-center/builds/"+build.ID+"/log?after=0", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if b, _ := io.ReadAll(rec.Result().Body); !bytes.Contains(b, []byte(`"next":0`)) {
		t.Fatal(string(b))
	}
}

type recipeQueueDocker struct {
	*fakeDocker
	mu       sync.Mutex
	commands [][]string
}

func (d *recipeQueueDocker) Command(args ...string) *exec.Cmd {
	d.mu.Lock()
	d.commands = append(d.commands, append([]string{}, args...))
	d.mu.Unlock()
	exe, _ := os.Executable()
	if len(args) > 0 && args[0] == "build" {
		cmd := exec.Command(exe, append([]string{"-test.run=^TestBuildProcessHelper$", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "RUNDESK_BUILD_HELPER=success")
		return cmd
	}
	cmd := exec.Command(exe, "-test.run=^TestBuildCenterRecipeSmokeProcess$")
	cmd.Env = append(os.Environ(), "RUNDESK_RECIPE_SMOKE_FIXTURE=1")
	return cmd
}

func TestBuildCenterRecipeSmokeProcess(t *testing.T) {
	if os.Getenv("RUNDESK_RECIPE_SMOKE_FIXTURE") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString("fixture offline initialize passed\n")
	os.Exit(0)
}

func TestBuildCenterRecipeQueueSuccess(t *testing.T) {
	m := testManager(t)
	d := &recipeQueueDocker{fakeDocker: newFakeDocker()}
	m.docker = d
	before, _ := m.Instance(DefaultInstance)
	manifest := BuildRecipeManifest{SchemaVersion: 1, Name: "Local tools", Version: "1.0.0"}
	r, err := m.ImportBuildRecipe(manifest, bytes.NewReader(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"})), store.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.StartRecipeBuild(r.ID, RecipeBuildInput{RecipeRevision: 1, RequestID: store.ID()})
	if err != nil {
		t.Fatal(err)
	}
	b = waitBuild(t, m, DefaultInstance, b.ID, "succeeded", "failed")
	if b.Status != "succeeded" || b.ImageID != "sha256:"+strings.Repeat("a", 64) || b.ImageVersionID != "" || !b.OfflineReady {
		t.Fatal(b)
	}
	after, _ := m.Instance(DefaultInstance)
	if before.Execution != after.Execution || before.Revision != after.Revision {
		t.Fatal("recipe build reconfigured default assistant")
	}
	d.mu.Lock()
	commands := append([][]string{}, d.commands...)
	d.mu.Unlock()
	if len(commands) != 2 || commands[0][0] != "build" || commands[1][0] != "run" {
		t.Fatal(commands)
	}
	run := strings.Join(commands[1], " ")
	for _, required := range []string{"--network none", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges:true", b.ImageID} {
		if !strings.Contains(run, required) {
			t.Fatal("smoke isolation missing", required, commands[1])
		}
	}
	if strings.Contains(run, "--mount") || strings.Contains(run, "--volume") || strings.Contains(run, "docker.sock") {
		t.Fatal("smoke received host access", run)
	}
}
