package skillcatalog

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b)))}
}
func errorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func noTokens(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GITHUB_TOKEN", "SKILLSMP_API_KEY", "SKILLS_SH_TOKEN", "CLAWHUB_TOKEN"} {
		t.Setenv(name, "")
	}
}

func TestSearchAllProvidersAndMetrics(t *testing.T) {
	noTokens(t)
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous search received token")
		}
		switch r.URL.Host {
		case "clawhub.ai":
			return jsonResponse(200, map[string]any{"results": []any{map[string]any{"slug": "pdf", "displayName": "PDF", "ownerHandle": "alice", "downloads": 0}}}), nil
		case "skillsmp.com":
			return jsonResponse(200, map[string]any{"success": true, "data": map[string]any{"skills": []any{map[string]any{"id": "pdf", "name": "PDF", "skillUrl": "https://skillsmp.com/skills/pdf", "githubUrl": "https://github.com/o/r/tree/main/skills/pdf", "stars": 5}}}}), nil
		case "skills.sh":
			if r.URL.Path != "/api/search" {
				t.Error("anonymous endpoint mismatch")
			}
			return jsonResponse(200, map[string]any{"skills": []any{map[string]any{"id": "o/r/pdf", "name": "PDF", "source": "o/r", "installs": 10}}}), nil
		case "api.github.com":
			if !strings.Contains(r.URL.Query().Get("q"), `"SKILL.md" in:readme`) {
				t.Error("missing skill candidate query")
			}
			return jsonResponse(200, map[string]any{"items": []any{map[string]any{"full_name": "o/r", "name": "r", "stargazers_count": 7}}, "incomplete_results": false}), nil
		}
		return nil, fmt.Errorf("unexpected host")
	})}}
	result, err := client.Search(context.Background(), "pdf", []string{"clawhub", "skillsmp", "skills.sh", "github"})
	if err != nil || !result.Complete || len(result.Items) != 4 || len(result.Providers) != 4 {
		t.Fatalf("unexpected result: %#v %v", result, err)
	}
	if result.Items[0].Metrics[0].Value != 0 {
		t.Fatal("zero metric lost")
	}
	for _, item := range result.Items {
		if item.ScanState != "not_scanned" || !item.CanImport {
			t.Fatalf("bad item: %#v", item)
		}
	}
}

func TestSearchFailureIsPartialNotEmptySuccess(t *testing.T) {
	noTokens(t)
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "skillsmp.com" {
			return jsonResponse(429, map[string]any{"error": "RATE_LIMITED"}), nil
		}
		return jsonResponse(200, map[string]any{"skills": []any{map[string]any{"id": "o/r/pdf", "name": "PDF", "source": "o/r"}}}), nil
	})}}
	result, err := client.Search(context.Background(), "pdf", []string{"skillsmp", "skills.sh"})
	if err != nil || result.Complete || len(result.Items) != 1 || result.Providers[0].Code != "rate_limited" || len(result.Warnings) == 0 {
		t.Fatalf("failure hidden: %#v %v", result, err)
	}
}

func TestSearchMalformedRowsRemainIncomplete(t *testing.T) {
	noTokens(t)
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, map[string]any{"results": []any{map[string]any{"slug": "pdf"}}}), nil
	})}}
	r, err := client.Search(context.Background(), "pdf", []string{"clawhub"})
	if err != nil || r.Complete || len(r.Items) != 0 || len(r.Warnings) == 0 {
		t.Fatalf("bad malformed result %#v %v", r, err)
	}
}

func TestSearchCancellationAndLocalOnly(t *testing.T) {
	noTokens(t)
	var calls atomic.Int32
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	r, err := client.Search(context.Background(), "pdf", []string{})
	if err != nil || !r.Complete || calls.Load() != 0 {
		t.Fatal("empty sources used network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.Search(ctx, "pdf", []string{"clawhub"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSearchAuthenticatedSkillsShAndInvalidSources(t *testing.T) {
	noTokens(t)
	t.Setenv("SKILLS_SH_TOKEN", "valid-token")
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/skills/search" || r.Header.Get("Authorization") != "Bearer valid-token" {
			t.Fatal("authenticated contract mismatch")
		}
		return jsonResponse(200, map[string]any{"data": []any{map[string]any{"id": "o/r/pdf", "name": "PDF", "source": "o/r", "sourceType": "github"}}}), nil
	})}}
	if _, err := client.Search(context.Background(), "pdf", []string{"skills.sh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), "pdf", []string{"evil"}); errorCode(err) != "invalid_source" {
		t.Fatalf("unknown source accepted %v", err)
	}
}

func TestConservativeMergeAndNoMarketSecurityClaims(t *testing.T) {
	sha := strings.Repeat("a", 40)
	makeItem := func(source, ref string) Item {
		item, ok := normalizeItem("skillsmp", map[string]any{"id": "pdf", "name": "pdf", "skillUrl": "https://skillsmp.com/skills/" + source, "githubUrl": "https://github.com/o/r/tree/" + ref + "/skills/pdf", "scanState": "safe", "stars": 1})
		if !ok {
			t.Fatal("fixture invalid")
		}
		item.Source = source
		item.Origins[0].Source = source
		return item
	}
	merged := mergeItems([]Item{makeItem("one", sha), makeItem("two", sha)})
	if len(merged) != 1 || len(merged[0].Origins) != 2 || merged[0].ScanState != "not_scanned" {
		t.Fatalf("merge failed %#v", merged)
	}
	if len(mergeItems([]Item{makeItem("one", "main"), makeItem("two", "master")})) != 2 {
		t.Fatal("ambiguous branch paths merged")
	}
	if len(mergeItems([]Item{makeItem("one", sha), makeItem("two", strings.Repeat("b", 40))})) != 2 {
		t.Fatal("different immutable revisions merged")
	}
}

func TestHTTPHostRedirectAndResponseLimits(t *testing.T) {
	var calls int
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.invalid/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	_, _, err := client.request(context.Background(), "https://evil.invalid/api/search", "secret", 100)
	if errorCode(err) != "invalid_host" || calls != 0 {
		t.Fatal("foreign host reached transport")
	}
	_, _, err = client.request(context.Background(), "https://skills.sh/api/search", "secret", 100)
	if errorCode(err) != "redirect_refused" || calls != 1 {
		t.Fatal("redirect followed")
	}
	client.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, map[string]any{"large": strings.Repeat("x", 100)}), nil
	})
	_, _, err = client.request(context.Background(), "https://skills.sh/api/search", "", 10)
	if errorCode(err) != "response_too_large" {
		t.Fatal("response limit ignored")
	}
}

func blobFixture(content string) (string, map[string]any) {
	b := []byte(content)
	sum := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(b))), b...))
	sha := hex.EncodeToString(sum[:])
	return sha, map[string]any{"sha": sha, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b)}
}

func githubFixture(t *testing.T, entries []map[string]any, blobs map[string]map[string]any, modify func(*http.Request) *http.Response) *Client {
	t.Helper()
	noTokens(t)
	return &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if modify != nil {
			if response := modify(r); response != nil {
				return response, nil
			}
		}
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			return jsonResponse(200, map[string]any{"default_branch": "main"}), nil
		case strings.HasPrefix(p, "/repos/o/r/commits/"):
			ref := strings.TrimPrefix(p, "/repos/o/r/commits/")
			if ref != "main" && ref != strings.Repeat("a", 40) {
				return jsonResponse(422, map[string]any{"message": "No commit found for SHA: " + ref}), nil
			}
			return jsonResponse(200, map[string]any{"sha": strings.Repeat("a", 40), "commit": map[string]any{"tree": map[string]any{"sha": strings.Repeat("b", 40)}}}), nil
		case strings.HasPrefix(p, "/repos/o/r/git/trees/"):
			return jsonResponse(200, map[string]any{"truncated": false, "tree": entries}), nil
		case strings.HasPrefix(p, "/repos/o/r/git/blobs/"):
			if v, ok := blobs[strings.TrimPrefix(p, "/repos/o/r/git/blobs/")]; ok {
				return jsonResponse(200, v), nil
			}
		}
		return jsonResponse(404, map[string]any{"message": "Not Found"}), nil
	})}}
}

func TestGitHubFetchImmutableFullBundle(t *testing.T) {
	text := "---\nname: docs-helper\n---\nSummarize supplied documents.\n"
	sha, blob := blobFixture(text)
	client := githubFixture(t, []map[string]any{{"path": "skills/docs/SKILL.md", "type": "blob", "mode": "100644", "sha": sha, "size": len(text)}}, map[string]map[string]any{sha: blob}, nil)
	bundle, err := client.Fetch(context.Background(), "https://github.com/o/r/tree/main/skills/docs")
	if err != nil || bundle.Name != "docs-helper" || bundle.Revision != strings.Repeat("a", 40) || len(bundle.Assets) != 1 || bundle.Assets[0].Path != "SKILL.md" || string(bundle.Assets[0].Data) != text {
		t.Fatalf("bad bundle %#v %v", bundle, err)
	}
	if !strings.Contains(bundle.SourceURL, strings.Repeat("a", 40)) {
		t.Fatal("mutable source identity")
	}
}

func TestGitHubFetchChoicesAndRefusal(t *testing.T) {
	text := "---\nname: helper\n---\ntext"
	sha, blob := blobFixture(text)
	entries := []map[string]any{{"path": "one/SKILL.md", "type": "blob", "mode": "100644", "sha": sha, "size": len(text)}, {"path": "two/SKILL.md", "type": "blob", "mode": "100644", "sha": sha, "size": len(text)}}
	client := githubFixture(t, entries, map[string]map[string]any{sha: blob}, nil)
	_, err := client.Fetch(context.Background(), "https://github.com/o/r")
	var e *Error
	if !errors.As(err, &e) || e.Code != "multiple_skills" || e.Details["total"] != 2 {
		t.Fatalf("choices missing %v", err)
	}
	entries[0]["mode"] = "120000"
	_, err = client.Fetch(context.Background(), "https://github.com/o/r/tree/main/one")
	if errorCode(err) != "unsupported_entry" {
		t.Fatalf("symlink accepted %v", err)
	}
}

func TestGitHubIntegrityTruncationAndLimits(t *testing.T) {
	text := "---\nname: helper\n---\ntext"
	sha, blob := blobFixture(text)
	entries := []map[string]any{{"path": "SKILL.md", "type": "blob", "mode": "100644", "sha": sha, "size": len(text)}}
	client := githubFixture(t, entries, map[string]map[string]any{sha: blob}, func(r *http.Request) *http.Response {
		if strings.Contains(r.URL.Path, "/git/trees/") {
			return jsonResponse(200, map[string]any{"truncated": true, "tree": entries})
		}
		return nil
	})
	_, err := client.Fetch(context.Background(), "https://github.com/o/r")
	if errorCode(err) != "truncated_tree" {
		t.Fatalf("truncated tree accepted %v", err)
	}
	bad := map[string]any{"sha": sha, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("changed"))}
	client = githubFixture(t, entries, map[string]map[string]any{sha: bad}, nil)
	_, err = client.Fetch(context.Background(), "https://github.com/o/r")
	if errorCode(err) != "integrity_error" {
		t.Fatalf("changed blob accepted %v", err)
	}
	entries[0]["size"] = MaxFileBytes + 1
	client = githubFixture(t, entries, map[string]map[string]any{sha: blob}, nil)
	_, err = client.Fetch(context.Background(), "https://github.com/o/r")
	if errorCode(err) != "file_too_large" {
		t.Fatalf("oversized file accepted %v", err)
	}
}

func TestFetchRejectsForeignHostsAndUnsafePaths(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe input reached transport")
		return nil, nil
	})}}
	for _, raw := range []string{"http://github.com/o/r", "https://127.0.0.1/o/r", "https://github.com@evil.invalid/o/r", "https://github.com/o/r/tree/main/../outside", "https://github.com/o/r?token=secret"} {
		if _, err := client.Fetch(context.Background(), raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
