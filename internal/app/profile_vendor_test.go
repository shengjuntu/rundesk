package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

// Exercise the actual embedded contexts used by the build center, including
// custom import and its secret/path checks, without pretending to run Docker.
func TestResearchRecipeVendoredContext(t *testing.T) {
	m := quietRecipeManager(t)
	for _, profile := range []string{"news2douyin", "people-search"} {
		t.Run(profile, func(t *testing.T) {
			data, err := runtimeImageContext(profile, "0.157.1")
			if err != nil {
				t.Fatal(err)
			}
			archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			for _, entry := range archive.File {
				r, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				content, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				files[entry.Name] = content
			}
			if !bytes.Contains(files["requirements.txt"], []byte("/opt/rundesk-profile/vendor/agent-reach\n")) || bytes.Contains(files["requirements.txt"], []byte("https://")) {
				t.Fatal("Agent-Reach source still depends on a remote archive")
			}
			docker := string(files["Dockerfile"])
			copyAt, installAt := strings.Index(docker, "COPY vendor/"), strings.Index(docker, "python3 -m pip install")
			if copyAt < 0 || installAt < copyAt || !strings.Contains(docker, "ARG CODEX_VERSION=0.157.1") || !strings.Contains(docker, `io.rundesk.codex.version="0.157.1"`) {
				t.Fatal("invalid vendored install order or Codex version")
			}
			var lock struct {
				CodexVersion string `json:"codexVersion"`
				Profile      struct {
					Version string `json:"version"`
				} `json:"profile"`
			}
			if json.Unmarshal(files["profile.lock.json"], &lock) != nil || lock.CodexVersion != "0.157.1" || lock.Profile.Version != runtimeProfileVersion(profile) || lock.Profile.Version != "1.1.2" {
				t.Fatal("recipe and image identity do not agree")
			}
			for _, name := range []string{"LICENSE", "pyproject.toml", "agent_reach/skill/SKILL.md", "agent_reach/guides/setup-exa.md"} {
				if len(files["vendor/agent-reach/"+name]) == 0 {
					t.Fatal("missing source resource", name)
				}
			}
			if bytes.Contains(files["vendor/agent-reach/pyproject.toml"], []byte("force-include")) {
				t.Fatal("duplicate wheel include was reintroduced")
			}
			for _, manifest := range []string{"SHA256SUMS", "vendor/agent-reach.sha256"} {
				for _, line := range strings.Split(strings.TrimSpace(string(files[manifest])), "\n") {
					parts := strings.SplitN(line, "  ", 2)
					if len(parts) != 2 {
						t.Fatal("invalid hash manifest", manifest)
					}
					name := parts[1]
					if manifest != "SHA256SUMS" {
						name = "vendor/agent-reach/" + name
					}
					content, ok := files[name]
					sum := sha256.Sum256(content)
					if !ok || hex.EncodeToString(sum[:]) != parts[0] {
						t.Fatal("embedded source checksum mismatch", name)
					}
				}
			}
			recipe, err := m.BuildCenterRecipe("builtin-" + profile)
			if err != nil || recipe.Version != "1.1.2" {
				t.Fatal(recipe, err)
			}
			if _, err := m.ImportBuildRecipe(recipe.BuildRecipeManifest, bytes.NewReader(data), store.ID(), ""); err != nil {
				t.Fatal("corrected context cannot be imported as a frozen custom recipe", err)
			}
		})
	}
}
