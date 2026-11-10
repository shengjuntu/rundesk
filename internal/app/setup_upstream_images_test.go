package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestIndependentUpstreamRuntimeContextsAndExactImageIdentity(t *testing.T) {
	for _, profile := range []string{"hyperframes", "video-use", "openmontage"} {
		t.Run(profile, func(t *testing.T) {
			data, err := runtimeImageContext(profile, "0.144.6")
			if err != nil {
				t.Fatal(err)
			}
			archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			var lock map[string]any
			var bundledSHA string
			for _, file := range archive.File {
				reader, _ := file.Open()
				body, _ := io.ReadAll(reader)
				reader.Close()
				if file.Name == "vendor/source.tar.gz" {
					bundledSHA = fmt.Sprintf("%x", sha256.Sum256(body))
				}
				if file.Name == "profile.lock.json" {
					if json.Unmarshal(body, &lock) != nil || lock["codexVersion"] != "0.144.6" {
						t.Fatal(string(body))
					}
				}
				if file.Name == "Dockerfile" && (!bytes.Contains(body, []byte("ARG CODEX_VERSION=0.144.6")) || bytes.Contains(body, []byte("0.0.0"))) {
					t.Fatal("wrong Codex substitution", string(body))
				}
			}
			if lock == nil {
				t.Fatal("missing source lock")
			}
			identity, ok := independentRuntimeIdentity(profile)
			wantVersion := "1.0.0"
			if profile == "openmontage" {
				wantVersion = "1.1.0"
			}
			if !ok || identity.Version != wantVersion {
				t.Fatal(identity, ok)
			}
			if profile == "openmontage" {
				manifest, err := readBuildDownloadManifest(data)
				if err != nil || manifest != nil || len(data) > buildUploadLimit || bundledSHA != identity.SourceSHA256 {
					t.Fatal("OpenMontage must be a complete importable local-source context", len(data), bundledSHA, manifest, err)
				}
			}
			labels := map[string]string{
				"io.rundesk.profile": profile, "org.opencontainers.image.version": wantVersion, "io.rundesk.agent-reach": "false",
				"io.rundesk.codex.version": "0.144.6", "io.rundesk.upstream.repository": identity.Repository,
				"io.rundesk.upstream.commit": identity.Commit, "io.rundesk.upstream.source.sha256": identity.SourceSHA256,
				"io.rundesk.video-app-media.contract": "1",
			}
			var image imageInspection
			image.Config.Labels = labels
			input := ImageBuild{ProfileID: profile, CodexVersion: "0.144.6"}
			if !runtimeImageMatches(input, image) {
				t.Fatal("matching independent image rejected")
			}
			for _, key := range []string{"io.rundesk.profile", "io.rundesk.upstream.repository", "io.rundesk.upstream.commit", "io.rundesk.upstream.source.sha256", "io.rundesk.video-app-media.contract", "io.rundesk.codex.version"} {
				original := labels[key]
				labels[key] = strings.Repeat("0", 40)
				if runtimeImageMatches(input, image) {
					t.Fatal("mismatched independent image accepted", key)
				}
				labels[key] = original
			}
		})
	}
}
