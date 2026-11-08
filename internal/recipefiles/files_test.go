package recipefiles

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRecipe(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "example")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"Dockerfile": "FROM scratch\n", "profile.lock.json": `{"profile":{"id":"example","version":"1.0.0"}}`, "SHA256SUMS": "stale on-disk checksums are regenerated"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RUNDESK_RECIPES_DIR", root)
	return directory
}

func zipFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range archive.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(content)
	}
	return files
}

func TestRecipeChangesAreReadWithoutRestart(t *testing.T) {
	directory := testRecipe(t)
	before, err := Files.ReadFile("example.zip")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Files.ReadFile("example.zip")
	if err != nil || !bytes.Equal(before, again) {
		t.Fatal("same files did not yield a deterministic snapshot", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "Dockerfile"), []byte("FROM busybox\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := Files.ReadFile("example.zip")
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("source edits were not reloaded", err)
	}
	oldFiles, newFiles := zipFiles(t, before), zipFiles(t, after)
	if oldFiles["Dockerfile"] != "FROM scratch\n" || newFiles["Dockerfile"] != "FROM busybox\n" || oldFiles["SHA256SUMS"] == newFiles["SHA256SUMS"] || strings.Contains(newFiles["SHA256SUMS"], "stale") {
		t.Fatal("snapshot was changed or its checksum was not regenerated")
	}
}

func TestExplicitRecipeDirectoryDoesNotFallBack(t *testing.T) {
	t.Setenv("RUNDESK_RECIPES_DIR", filepath.Join(t.TempDir(), "missing"))
	if _, err := Files.ReadFile("news2douyin.zip"); err == nil || !strings.Contains(err.Error(), "RUNDESK_RECIPES_DIR") {
		t.Fatal("invalid explicit recipe source silently fell back", err)
	}
}

func TestRecipePathAndLinksAreRejected(t *testing.T) {
	directory := testRecipe(t)
	for _, name := range []string{"../example.zip", "example/../example.zip", "example\\test.zip", "example", ".zip"} {
		if _, err := Files.ReadFile(name); err == nil {
			t.Fatal("invalid recipe path accepted", name)
		}
	}
	if err := os.Symlink(filepath.Join(directory, "Dockerfile"), filepath.Join(directory, "linked")); err != nil {
		t.Skip("platform does not permit symlink fixtures")
	}
	if _, err := Files.ReadFile("example.zip"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatal("recipe link accepted", err)
	}
}

func TestCanonicalDescriptorProducesOnlyFrozenLock(t *testing.T) {
	directory := testRecipe(t)
	legacy := filepath.Join(directory, "profile.lock.json")
	descriptor := filepath.Join(directory, "recipe.json")
	original, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(legacy, descriptor); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "variants"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "variants", "Dockerfile.lite"), []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	metadata, err := Metadata("example")
	if err != nil || !bytes.Equal(metadata, original) {
		t.Fatal("catalog did not read the canonical descriptor", err)
	}
	before, err := Files.ReadFile("example.zip")
	if err != nil {
		t.Fatal(err)
	}
	files := zipFiles(t, before)
	if files["profile.lock.json"] != string(original) || files["recipe.json"] != "" || files["variants/Dockerfile.lite"] != "" {
		t.Fatal("snapshot leaked authoring inputs or did not generate the frozen lock", files)
	}
	if strings.Contains(files["SHA256SUMS"], "recipe.json") || strings.Contains(files["SHA256SUMS"], "variants/") || !strings.Contains(files["SHA256SUMS"], "profile.lock.json") {
		t.Fatal("checksums describe authoring files instead of the frozen context")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("taking a snapshot wrote a generated lock into the editable source", err)
	}
	updated := []byte(`{"profile":{"id":"example","version":"2.0.0"}}`)
	if err := os.WriteFile(descriptor, updated, 0644); err != nil {
		t.Fatal(err)
	}
	after, err := Files.ReadFile("example.zip")
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("canonical descriptor edit was not read without restart", err)
	}
	if zipFiles(t, before)["profile.lock.json"] != string(original) || zipFiles(t, after)["profile.lock.json"] != string(updated) {
		t.Fatal("canonical edits changed an earlier frozen snapshot")
	}
}

func TestDescriptorAmbiguityAndMetadataLimitsAreRejected(t *testing.T) {
	directory := testRecipe(t)
	if metadata, err := Metadata("example"); err != nil || len(metadata) == 0 {
		t.Fatal("legacy external metadata stopped working", err)
	}
	descriptor := filepath.Join(directory, "recipe.json")
	if err := os.WriteFile(descriptor, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Metadata("example"); err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatal("metadata readers silently selected an ambiguous source", err)
	}
	if _, err := Files.ReadFile("example.zip"); err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatal("snapshot silently selected an ambiguous source", err)
	}
	if err := os.Remove(filepath.Join(directory, "profile.lock.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descriptor, bytes.Repeat([]byte("x"), (256<<10)+1), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Metadata("example"); err == nil {
		t.Fatal("unbounded canonical metadata accepted")
	}
	if _, err := Files.ReadFile("example.zip"); err == nil {
		t.Fatal("unbounded canonical metadata accepted into a snapshot")
	}
}

func TestAuthoringVariantLinksAreRejectedEvenWhenNotPacked(t *testing.T) {
	directory := testRecipe(t)
	if err := os.Mkdir(filepath.Join(directory, "variants"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "Dockerfile"), filepath.Join(directory, "variants", "linked")); err != nil {
		t.Skip("platform does not permit symlink fixtures")
	}
	if _, err := Files.ReadFile("example.zip"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatal("ignored authoring variants weakened source path validation", err)
	}
}

func TestCanonicalUpstreamProjectionIsFrozenAndUnambiguous(t *testing.T) {
	directory := testRecipe(t)
	if err := os.Remove(filepath.Join(directory, "profile.lock.json")); err != nil {
		t.Fatal(err)
	}
	descriptor := []byte(`{"profile":{"id":"example","version":"1.0.0"},"upstream":{"commit":"reviewed","sourceArchive":"vendor/source.tar.gz"}}`)
	if err := os.WriteFile(filepath.Join(directory, "recipe.json"), descriptor, 0644); err != nil {
		t.Fatal(err)
	}
	data, err := Files.ReadFile("example.zip")
	if err != nil {
		t.Fatal(err)
	}
	files := zipFiles(t, data)
	expected := "{\n  \"commit\": \"reviewed\",\n  \"sourceArchive\": \"vendor/source.tar.gz\"\n}\n"
	if files["upstream/upstream.json"] != expected || files["profile.lock.json"] != string(descriptor) {
		t.Fatal("upstream declaration did not produce a deterministic frozen runtime file", files)
	}
	if _, err := os.Stat(filepath.Join(directory, "upstream")); !os.IsNotExist(err) {
		t.Fatal("reading a recipe materialized generated metadata in the source tree", err)
	}
	if err := os.Mkdir(filepath.Join(directory, "upstream"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "upstream", "upstream.json"), []byte(`{"commit":"other"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Files.ReadFile("example.zip"); err == nil || !strings.Contains(err.Error(), "only in recipe.json") {
		t.Fatal("a second editable upstream declaration was silently accepted", err)
	}
}
