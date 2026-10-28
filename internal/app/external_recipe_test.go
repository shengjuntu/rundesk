package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestExternalRecipeChangesDoNotChangeFrozenImageIdentity(t *testing.T) {
	context, err := runtimeImageContext("news2douyin", "0.157.1")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	directory := filepath.Join(root, "news2douyin")
	archive, err := zip.NewReader(bytes.NewReader(context), int64(len(context)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, filepath.FromSlash(entry.Name))
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	build := ImageBuild{Scope: "setup", ProfileID: "news2douyin", CodexVersion: "0.157.1", Dockerfile: "Dockerfile"}
	if err := freezeRuntimeImageIdentity(&build, directory); err != nil {
		t.Fatal(err)
	}
	if build.RuntimeIdentity == nil || build.RuntimeIdentity.ProfileVersion != "1.1.2" {
		t.Fatal("runtime identity was not frozen", build)
	}
	t.Setenv("RUNDESK_RECIPES_DIR", root)
	lockPath := filepath.Join(directory, "profile.lock.json")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	lock["profile"].(map[string]any)["version"] = "9.9.9"
	updated, _ := json.Marshal(lock)
	if err := os.WriteFile(lockPath, updated, 0644); err != nil {
		t.Fatal(err)
	}
	var image imageInspection
	image.Config.Labels = map[string]string{"org.opencontainers.image.version": "1.1.2", "io.rundesk.profile": "news2douyin", "io.rundesk.codex.version": "0.157.1", "io.rundesk.agent-reach": "true"}
	if !runtimeImageMatches(build, image) {
		t.Fatal("editing the external recipe changed a submitted build's identity")
	}
	if runtimeProfileVersion("news2douyin") != "9.9.9" {
		t.Fatal("new recipe metadata remained cached")
	}
	// A previously edited on-disk default must not override the UI's selection.
	dockerPath := filepath.Join(directory, "Dockerfile")
	docker, err := os.ReadFile(dockerPath)
	if err != nil {
		t.Fatal(err)
	}
	docker = []byte(strings.ReplaceAll(string(docker), "0.157.1", "8.8.8"))
	if err := os.WriteFile(dockerPath, docker, 0644); err != nil {
		t.Fatal(err)
	}
	selected, err := runtimeImageContext("news2douyin", "0.157.1")
	if err != nil {
		t.Fatal(err)
	}
	selectedArchive, _ := zip.NewReader(bytes.NewReader(selected), int64(len(selected)))
	for _, entry := range selectedArchive.File {
		if entry.Name != "Dockerfile" {
			continue
		}
		reader, _ := entry.Open()
		content, _ := io.ReadAll(reader)
		reader.Close()
		if !bytes.Contains(content, []byte("ARG CODEX_VERSION=0.157.1")) || bytes.Contains(content, []byte("8.8.8")) {
			t.Fatal("UI version was not bound to editable recipe", string(content))
		}
	}
}

func TestRecipeFilesHTTPPreviewAndCompleteExport(t *testing.T) {
	m := quietRecipeManager(t)
	mux := http.NewServeMux()
	server := &Server{Manager: m}
	server.buildCenterRecipeRoutes(mux)
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	for _, query := range []string{"", "?codexVersion=0.157.1"} {
		response := request("/api/build-center/recipes/builtin-news2douyin/dockerfile" + query)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		var view struct {
			Dockerfile string `json:"dockerfile"`
			Path       string `json:"path"`
			Template   bool   `json:"template"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || view.Path != "Dockerfile" || view.Template != (query == "") || !strings.Contains(view.Dockerfile, "COPY vendor/") {
			t.Fatal("invalid preview", view, err)
		}
	}
	if response := request("/api/build-center/recipes/builtin-news2douyin/context.zip"); response.Code != 400 {
		t.Fatal("exported unresolved placeholder version", response.Code)
	}
	exported := request("/api/build-center/recipes/builtin-news2douyin/context.zip?codexVersion=0.157.1")
	if exported.Code != 200 || exported.Header().Get("Content-Type") != "application/zip" || !strings.Contains(exported.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(exported.Code, exported.Header(), exported.Body.String())
	}
	recipe, err := m.BuildCenterRecipe("builtin-news2douyin")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := m.ImportBuildRecipe(recipe.BuildRecipeManifest, bytes.NewReader(exported.Body.Bytes()), store.ID(), "")
	if err != nil {
		t.Fatal("export cannot be imported without repairing its files", err)
	}
	response := request("/api/build-center/recipes/" + custom.ID + "/context.zip")
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), exported.Body.Bytes()) {
		t.Fatal("custom export changed its frozen context", response.Code)
	}
	if response := request("/api/build-center/recipes/" + custom.ID + "/context.zip?codexVersion=0.157.1"); response.Code != 400 {
		t.Fatal("custom frozen version was overridable", response.Code)
	}
}
