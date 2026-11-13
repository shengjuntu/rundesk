package skillcatalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
)

type clawTestTransport func(*http.Request) (*http.Response, error)

func (f clawTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clawTestJSON(value any) []byte { b, _ := json.Marshal(value); return b }

func clawTestResponse(status int, contentType string, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(body))}
}

func clawTestDetail() map[string]any {
	return map[string]any{"owner": map[string]any{"handle": "alice"}, "skill": map[string]any{"slug": "demo", "displayName": "Demo"}, "latestVersion": map[string]any{"version": "1.2.3"}}
}

func clawTestManifest(files map[string][]byte) map[string]any {
	entries := []map[string]any{}
	for path, data := range files {
		digest := sha256.Sum256(data)
		entries = append(entries, map[string]any{"path": path, "size": len(data), "sha256": hex.EncodeToString(digest[:])})
	}
	return map[string]any{"skill": map[string]any{"slug": "demo"}, "version": map[string]any{"version": "1.2.3", "files": entries}}
}

func clawTestErrorCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("expected code %s, got %v", code, err)
	}
	return e
}

func TestClawFetchCompleteVersionAndHashes(t *testing.T) {
	t.Setenv("CLAWHUB_TOKEN", "")
	files := map[string][]byte{"SKILL.md": []byte("---\nname: demo\n---\nText"), "scripts/check.sh": []byte("#!/bin/sh\nprintf ok")}
	calls := []string{}
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "clawhub.ai" || r.URL.Query().Get("owner") != "alice" {
			t.Errorf("unqualified request %s", r.URL)
		}
		calls = append(calls, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/skills/demo":
			return clawTestResponse(200, "application/json", clawTestJSON(clawTestDetail())), nil
		case "/api/v1/skills/demo/versions/1.2.3":
			return clawTestResponse(200, "application/json", clawTestJSON(clawTestManifest(files))), nil
		case "/api/v1/skills/demo/file":
			if r.URL.Query().Get("version") != "1.2.3" {
				t.Errorf("unlocked version")
			}
			return clawTestResponse(200, "text/plain", files[r.URL.Query().Get("path")]), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})}}
	bundle, err := client.fetchClawHub(context.Background(), "https://clawhub.ai/alice/skills/demo")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Revision != "1.2.3" || bundle.Name != "Demo" || len(bundle.Assets) != 2 || len(calls) != 4 {
		t.Fatalf("incomplete bundle %+v", bundle)
	}
	if bundle.Assets[1].Mode&0111 == 0 {
		t.Fatal("script executable metadata not preserved")
	}
}

func TestClawRejectsInvalidURLBeforeNetwork(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(*http.Request) (*http.Response, error) { t.Fatal("network called"); return nil, nil })}}
	for _, raw := range []string{"http://clawhub.ai/alice/demo", "https://clawhub.ai:443/alice/demo", "https://clawhub.ai@evil.com/alice/demo", "https://clawhub.ai/alice/skills/../demo", "https://clawhub.ai/alice/demo?owner=bob"} {
		_, err := client.fetchClawHub(context.Background(), raw)
		clawTestErrorCode(t, err, "invalid_url")
	}
}

func TestClawRejectsMismatchedPublisher(t *testing.T) {
	detail := clawTestDetail()
	detail["owner"] = map[string]any{"handle": "bob"}
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(*http.Request) (*http.Response, error) {
		return clawTestResponse(200, "application/json", clawTestJSON(detail)), nil
	})}}
	_, err := client.fetchClawHub(context.Background(), "https://clawhub.ai/alice/demo")
	clawTestErrorCode(t, err, "identity_mismatch")
}

func TestClawHashMismatchNeverReturnsPartialBundle(t *testing.T) {
	files := map[string][]byte{"SKILL.md": []byte("valid")}
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/file"):
			return clawTestResponse(200, "text/plain", []byte("wrong")), nil
		case strings.Contains(r.URL.Path, "/versions/"):
			return clawTestResponse(200, "application/json", clawTestJSON(clawTestManifest(files))), nil
		default:
			return clawTestResponse(200, "application/json", clawTestJSON(clawTestDetail())), nil
		}
	})}}
	bundle, err := client.fetchClawHub(context.Background(), "https://clawhub.ai/alice/demo")
	clawTestErrorCode(t, err, "integrity_error")
	if len(bundle.Assets) != 0 {
		t.Fatal("returned partial files")
	}
}

func TestClawManifestRejectsUnsafeEntriesAndBudgets(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name    string
		entries []map[string]any
		code    string
	}{
		{"traversal", []map[string]any{{"path": "../SKILL.md", "size": 1, "sha256": digest}}, "unsafe_path"},
		{"symlink", []map[string]any{{"path": "SKILL.md", "size": 1, "sha256": digest, "mode": "120000"}}, "unsupported_entry"},
		{"large", []map[string]any{{"path": "SKILL.md", "size": MaxFileBytes + 1, "sha256": digest}}, "file_too_large"},
		{"missinghash", []map[string]any{{"path": "SKILL.md", "size": 1}}, "incomplete_manifest"},
		{"missingroot", []map[string]any{{"path": "nested/SKILL.md", "size": 1, "sha256": digest}}, "no_skill"},
		{"duplicatecase", []map[string]any{{"path": "SKILL.md", "size": 1, "sha256": digest}, {"path": "skill.md", "size": 1, "sha256": digest}}, "unsafe_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := clawManifest(clawTestJSON(tc.entries))
			clawTestErrorCode(t, err, tc.code)
		})
	}
}

func clawTestZIP(t *testing.T, name string, data []byte, mode fs.FileMode) []byte {
	t.Helper()
	var output bytes.Buffer
	z := zip.NewWriter(&output)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	f, err := z.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestClawFullZIPFallback(t *testing.T) {
	archive := clawTestZIP(t, "SKILL.md", []byte("content"), 0600)
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/api/v1/download":
			if r.URL.Query().Get("owner") != "alice" || r.URL.Query().Get("version") != "1.2.3" {
				t.Errorf("unlocked ZIP")
			}
			return clawTestResponse(200, "application/zip", archive), nil
		case strings.Contains(r.URL.Path, "/versions/"):
			return clawTestResponse(404, "text/plain", []byte("not found")), nil
		default:
			return clawTestResponse(200, "application/json", clawTestJSON(clawTestDetail())), nil
		}
	})}}
	bundle, err := client.fetchClawHub(context.Background(), "https://clawhub.ai/alice/demo")
	if err != nil || len(bundle.Assets) != 1 {
		t.Fatalf("%+v %v", bundle, err)
	}
}

func TestClawZIPRejectsPathsAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode fs.FileMode
	}{{"../SKILL.md", 0600}, {"SKILL.md", fs.ModeSymlink | 0777}, {"/SKILL.md", 0600}} {
		_, err := clawReadZIP(clawTestZIP(t, tc.name, []byte("content"), tc.mode))
		clawTestErrorCode(t, err, "invalid_archive")
	}
}

func TestClawGitHubHandoffIsExplicitAndImmutable(t *testing.T) {
	t.Setenv("CLAWHUB_TOKEN", "claw-secret")
	commit := strings.Repeat("a", 40)
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "clawhub.ai" {
			t.Fatal("followed handoff")
		}
		return clawTestResponse(200, "application/json", clawTestJSON(map[string]any{"sourceRef": "public-github", "repo": "alice/repo", "commit": commit, "path": "skills/demo", "archiveUrl": "https://evil.invalid/private"})), nil
	})}}
	_, err := client.fetchClawHub(context.Background(), "https://clawhub.ai/alice/demo")
	e := clawTestErrorCode(t, err, "source_handoff")
	if e.Details["githubUrl"] != "https://github.com/alice/repo/tree/"+commit+"/skills/demo" {
		t.Fatal(e.Details)
	}
	if strings.Contains(e.Error(), "claw-secret") {
		t.Fatal("credential leaked")
	}
	err = clawHandoff(clawTestJSON(map[string]any{"sourceRef": "public-github", "repo": "alice/repo", "commit": "main", "path": ""}))
	clawTestErrorCode(t, err, "invalid_response")
}

func TestClawTruncatedManifestNeverFallsBack(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: clawTestTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/versions/") {
			value := clawTestManifest(map[string][]byte{"SKILL.md": []byte("x")})
			value["truncated"] = true
			return clawTestResponse(200, "application/json", clawTestJSON(value)), nil
		}
		if r.URL.Path == "/api/v1/download" {
			t.Fatal("fallback on incomplete manifest")
		}
		return clawTestResponse(200, "application/json", clawTestJSON(clawTestDetail())), nil
	})}}
	_, err := client.fetchClawHub(context.Background(), "https://clawhub.ai/alice/demo")
	clawTestErrorCode(t, err, "incomplete_manifest")
}
