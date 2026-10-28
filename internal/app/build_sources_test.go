package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func sourceTestContext(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, map[string]string{"Dockerfile": "ARG RD_BASE_IMAGE=scratch\nFROM ${RD_BASE_IMAGE}\nARG RD_NPM_REGISTRY=https://registry.npmjs.org/\nARG RD_PIP_INDEX_URL=https://pypi.org/simple/\nRUN printf '%s' \"$RD_NPM_REGISTRY $RD_PIP_INDEX_URL\"\n"})
}

func TestBuildSourceCatalogExternalFileAndHTTP(t *testing.T) {
	m := quietRecipeManager(t)
	catalog, err := m.BuildSources()
	if err != nil || len(catalog.Revision) != 64 || len(catalog.Groups) != 2 || len(catalog.Types) != 8 {
		t.Fatal(catalog, err)
	}
	path := filepath.Join(m.Data, "config", "build-sources.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	originalRevision := catalog.Revision
	catalog.DefaultGroup = "china"
	catalog.Groups[1].Name = "中国镜像（本机配置）"
	updated, err := m.SaveBuildSources(catalog)
	if err != nil || updated.DefaultGroup != "china" || updated.Revision == originalRevision {
		t.Fatal(updated, err)
	}
	if _, err := m.SaveBuildSources(catalog); err == nil {
		t.Fatal("stale catalog update accepted")
	}
	// Direct file edits are read immediately; no restart or compiled default wins.
	updated.Groups[1].Name = "文件修改已生效"
	data, _ := json.Marshal(updated)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	updated, err = m.BuildSources()
	if err != nil || updated.Groups[1].Name != "文件修改已生效" {
		t.Fatal(updated, err)
	}
	mux := http.NewServeMux()
	(&Server{Manager: m}).buildCenterRecipeRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/build-center/sources", nil))
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"defaultGroup":"china"`)) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/build-center/sources", strings.NewReader(`{"schemaVersion":1,"revision":"old","groups":[]}`)))
	if rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.BuildSources(); err == nil {
		t.Fatal("malformed external file silently fell back")
	}
}

func TestBuildSourcesRejectCredentialsAndUnsupportedParameters(t *testing.T) {
	m := quietRecipeManager(t)
	for _, value := range []string{"https://user:pass@example.com/simple", "https://example.com/?token=secret", "https://example.com/#token", "https://example.com/\nRUN bad", "file:///etc/passwd", "https://example.com/|bad"} {
		_, err := m.resolveBuildSources(&BuildSourceOptions{Group: "international", Overrides: map[string]map[string]string{"pip": {"indexUrl": value}}}, sourceTestContext(t), "Dockerfile")
		if err == nil {
			t.Fatal("unsafe address accepted", value)
		}
	}
	for _, dockerfile := range []string{"FROM scratch\nRUN pip install package\n", "FROM scratch\nARG RD_PIP_INDEX_URL\nRUN echo no-use\n", "FROM scratch\nARG RD_PIP_INDEX_URL\n# RUN pip --index-url $RD_PIP_INDEX_URL\n", "FROM scratch\nARG RD_PIP_INDEX_URL\nRUN echo $RD_PIP_INDEX_URL_NOT_THE_ARGUMENT\n"} {
		_, err := m.resolveBuildSources(&BuildSourceOptions{Group: "international", Overrides: map[string]map[string]string{"pip": {"indexUrl": "https://mirror.example/simple"}}}, buildZip(t, map[string]string{"Dockerfile": dockerfile}), "Dockerfile")
		if err == nil {
			t.Fatal("unsupported override appeared to succeed", dockerfile)
		}
	}
	snapshot, err := m.resolveBuildSources(&BuildSourceOptions{Group: "china", Overrides: map[string]map[string]string{"docker": {"baseImage": ""}}}, sourceTestContext(t), "Dockerfile")
	if err != nil || snapshot.Arguments["RD_NPM_REGISTRY"] != "https://registry.npmmirror.com/" {
		t.Fatal(snapshot, err)
	}
	if _, ok := snapshot.Arguments["RD_BASE_IMAGE"]; ok {
		t.Fatal("empty base image was passed as a build argument")
	}
	_, err = m.resolveBuildSources(&BuildSourceOptions{Group: "international", Overrides: map[string]map[string]string{"docker": {"baseImage": "user:pass@registry.example"}}}, sourceTestContext(t), "Dockerfile")
	if err == nil {
		t.Fatal("image userinfo accepted")
	}
}

func TestBuildSourceCatalogConcurrentUpdates(t *testing.T) {
	m := quietRecipeManager(t)
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(catalog)
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, name := range []string{"first writer", "second writer"} {
		workers.Add(1)
		go func(name string) {
			defer workers.Done()
			var local BuildSourceCatalog
			_ = json.Unmarshal(encoded, &local)
			local.Groups[0].Name = name
			_, err := m.SaveBuildSources(local)
			results <- err
		}(name)
	}
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if typed, ok := err.(*apiError); ok && typed.Code == "source_catalog_conflict" {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
}

func TestBuildSourceCustomTypesAndFrozenDefinition(t *testing.T) {
	m := quietRecipeManager(t)
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Types = append(catalog.Types, BuildSourceType{ID: "go", Name: "Go Modules", DetectPattern: `(?i)\bgo\s`, Fields: []BuildSourceField{{ID: "proxy", Label: "GOPROXY", Argument: "RD_GO_PROXY", Kind: "url"}}})
	catalog.Groups[0].Sources = append(catalog.Groups[0].Sources, BuildSourceEntry{ID: "official-go", Name: "Go module proxy", Type: "go", Values: map[string]string{"proxy": "https://proxy.golang.org"}})
	catalog, err = m.SaveBuildSources(catalog)
	if err != nil {
		t.Fatal(err)
	}
	data := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\nARG RD_GO_PROXY\nRUN go env -w GOPROXY=\"$RD_GO_PROXY\"\n"})
	snapshot, err := m.resolveBuildSources(&BuildSourceOptions{Group: "international"}, data, "Dockerfile")
	if err != nil || snapshot.Arguments["RD_GO_PROXY"] != "https://proxy.golang.org" {
		t.Fatal(snapshot, err)
	}
	before, err := buildSourceArguments(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Later configuration changes cannot reinterpret a historical RD_* mapping.
	catalog.Types[len(catalog.Types)-1].Fields[0].Argument = "RD_NEW_GO_PROXY"
	if _, err := m.SaveBuildSources(catalog); err != nil {
		t.Fatal(err)
	}
	after, err := buildSourceArguments(snapshot)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(before, after, err)
	}
	if !strings.Contains(strings.Join(after, " "), "RD_GO_PROXY=https://proxy.golang.org") {
		t.Fatal(after)
	}
	bad := *snapshot
	bad.Arguments = map[string]string{"HTTP_PROXY": "https://example.com"}
	if _, err := buildSourceArguments(&bad); err == nil {
		t.Fatal("unmapped frozen argument accepted")
	}
}

type sourceCaptureDocker struct {
	*runtimeImageDocker
	arguments []string
}

func (d *sourceCaptureDocker) Command(args ...string) *exec.Cmd {
	if len(args) > 0 && args[0] == "build" {
		d.arguments = append([]string{}, args...)
	}
	return d.runtimeImageDocker.Command(args...)
}

func TestBuildSourcesFrozenRebuildAndDockerArguments(t *testing.T) {
	m := quietRecipeManager(t)
	manifest := recipeTestManifest()
	manifest.Smoke = RecipeSmokeSpec{}
	recipe, err := m.ImportBuildRecipe(manifest, bytes.NewReader(sourceTestContext(t)), store.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	options := &BuildSourceOptions{Group: "china"}
	input := RecipeBuildInput{RecipeRevision: 1, RecipeSHA: recipe.RecipeSHA, RequestID: store.ID(), BuildSources: options}
	build, err := m.StartRecipeBuild(recipe.ID, input)
	if err != nil || build.BuildSources == nil {
		t.Fatal(build, err)
	}
	again, err := m.StartRecipeBuild(recipe.ID, input)
	if err != nil || again.ID != build.ID {
		t.Fatal(again, err)
	}
	frozen, err := os.ReadFile(filepath.Join(m.buildDir(build.ID), "context.zip"))
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(frozen), int64(len(frozen)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range z.File {
		if file.Name == "build-sources.lock.json" {
			r, _ := file.Open()
			content, _ := io.ReadAll(r)
			r.Close()
			found = bytes.Contains(content, []byte("registry.npmmirror.com"))
		}
	}
	if !found {
		t.Fatal("effective sources missing from context snapshot")
	}
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	for gi := range catalog.Groups {
		for si := range catalog.Groups[gi].Sources {
			entry := &catalog.Groups[gi].Sources[si]
			if entry.ID == "npmmirror" {
				entry.Values["registry"] = "https://changed.example/"
			}
		}
	}
	if _, err := m.SaveBuildSources(catalog); err != nil {
		t.Fatal(err)
	}
	rebuild, err := m.RebuildCenterBuild(build.ID, RebuildCenterBuildInput{RequestID: store.ID()})
	if err != nil || rebuild.ContextSHA256 != build.ContextSHA256 || !reflect.DeepEqual(rebuild.BuildSources, build.BuildSources) {
		t.Fatal(rebuild, err)
	}
	newBuild, err := m.StartRecipeBuild(recipe.ID, RecipeBuildInput{RecipeRevision: 1, RequestID: store.ID(), BuildSources: options})
	if err != nil || newBuild.ContextSHA256 == build.ContextSHA256 || newBuild.BuildSources.Arguments["RD_NPM_REGISTRY"] != "https://changed.example/" {
		t.Fatal(newBuild, err)
	}
	driver := &sourceCaptureDocker{runtimeImageDocker: &runtimeImageDocker{fakeDocker: newFakeDocker()}}
	m.docker = driver
	if _, _, err := m.executeBuild(context.Background(), rebuild); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(driver.arguments, " ")
	if !strings.Contains(args, "--build-arg RD_NPM_REGISTRY=https://registry.npmmirror.com/") || strings.Contains(args, "changed.example") || strings.Contains(args, "RD_BASE_IMAGE=") {
		t.Fatal(args)
	}
}

func TestBuildSourceCapabilityEndpointAndStaleRecipe(t *testing.T) {
	m := quietRecipeManager(t)
	recipe, err := m.ImportBuildRecipe(recipeTestManifest(), bytes.NewReader(sourceTestContext(t)), store.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{Manager: m}).buildCenterRecipeRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/build-center/recipes/"+recipe.ID+"/sources", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "RD_NPM_REGISTRY") {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	if _, err := m.StartRecipeBuild(recipe.ID, RecipeBuildInput{RecipeRevision: 1, RecipeSHA: strings.Repeat("a", 64), RequestID: store.ID()}); err == nil {
		t.Fatal("stale recipe hash accepted")
	}
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.resolveBuildSources(&BuildSourceOptions{Group: "china", CatalogRevision: catalog.Revision + "bad"}, sourceTestContext(t), "Dockerfile"); err == nil {
		t.Fatal("stale source catalog accepted")
	}
}
