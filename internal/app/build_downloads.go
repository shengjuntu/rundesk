package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// This declaration is part of the frozen recipe ZIP. The expected hash remains
// authoritative when a mirror or complete archive URL is selected. Downloaded
// bytes are auxiliary build inputs, never a silent mutation of the recipe ZIP.
type BuildDownloadSpec struct {
	ID              string `json:"id"`
	URL             string `json:"url"`
	SHA256          string `json:"sha256"`
	Destination     string `json:"destination"`
	ArchiveBasePath string `json:"archiveBasePath,omitempty"`
}

type BuildDownloadManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Items         []BuildDownloadSpec `json:"items"`
}

type BuildDownloadEvidence struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Destination string `json:"destination"`
	Bytes       int64  `json:"bytes"`
	CacheHit    bool   `json:"cacheHit"`
	Bundled     bool   `json:"bundled,omitempty"`
	VerifiedAt  string `json:"verifiedAt"`
}

const buildDownloadMaxBytes int64 = 128 << 20

var buildDownloadSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Builds already run on a serial queue. A cancellable process-wide gate also
// protects the shared content-addressed cache in tests and parallel managers.
var buildDownloadGate = make(chan struct{}, 1)

func readBuildDownloadManifest(data []byte) (*BuildDownloadManifest, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range z.File {
		if file.Name != "build-downloads.json" {
			continue
		}
		if file.UncompressedSize64 > 64<<10 {
			return nil, fmt.Errorf("build-downloads.json exceeds 64 KiB")
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(r, (64<<10)+1))
		r.Close()
		if err != nil || len(content) > 64<<10 {
			return nil, fmt.Errorf("cannot read bounded build-downloads.json")
		}
		var manifest BuildDownloadManifest
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			return nil, fmt.Errorf("invalid build-downloads.json: %w", err)
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF || manifest.SchemaVersion != 1 || len(manifest.Items) < 1 || len(manifest.Items) > 8 {
			return nil, fmt.Errorf("build-downloads.json requires schemaVersion 1 and 1–8 items")
		}
		ids, paths := map[string]bool{}, map[string]bool{}
		for _, item := range manifest.Items {
			if !sourceIdentifier.MatchString(item.ID) || ids[item.ID] || !buildDownloadSHA.MatchString(item.SHA256) || !safeBuildPath(item.Destination) || !strings.HasPrefix(item.Destination, "vendor/") || paths[strings.ToLower(item.Destination)] {
				return nil, fmt.Errorf("invalid or duplicate build download identity/destination")
			}
			if item.URL == "" || buildSourceValue("url", item.URL) != nil {
				return nil, fmt.Errorf("build download requires a public HTTP(S) URL without credentials")
			}
			if item.ArchiveBasePath != "" && (!strings.HasPrefix(item.ArchiveBasePath, "/") || !safeBuildPath(strings.TrimPrefix(item.ArchiveBasePath, "/")) || buildSourceValue("url", "https://archive.invalid"+item.ArchiveBasePath) != nil) {
				return nil, fmt.Errorf("invalid archiveBasePath")
			}
			ids[item.ID], paths[strings.ToLower(item.Destination)] = true, true
		}
		return &manifest, nil
	}
	return nil, nil
}

func buildDownloadURL(item BuildDownloadSpec, sources *BuildSourceSnapshot) (string, error) {
	address := item.URL
	// Only an explicit archive declaration opts into GitHub source overrides.
	// Other downloads retain their frozen URL even if the recipe uses GitHub.
	if item.ArchiveBasePath != "" && sources != nil {
		if _, err := buildSourceArguments(sources); err != nil {
			return "", err
		}
		if full := sources.Arguments["RD_GITHUB_ARCHIVE_URL"]; full != "" {
			address = full
		} else if base := sources.Arguments["RD_GITHUB_ARCHIVE_BASE"]; base != "" {
			address = strings.TrimRight(base, "/") + item.ArchiveBasePath
		}
	}
	if err := buildSourceValue("url", address); err != nil {
		return "", err
	}
	return address, nil
}

func verifiedDownloadFile(path, expected string) (int64, bool) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > buildDownloadMaxBytes {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, buildDownloadMaxBytes+1))
	return n, err == nil && n <= buildDownloadMaxBytes && hex.EncodeToString(h.Sum(nil)) == expected
}

func buildDownloadHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil {
			return fmt.Errorf("invalid archive redirect")
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("archive redirect would disable HTTPS")
		}
		return nil
	}}
}

// Download atomically. Retry transient transport/status failures only; a digest
// mismatch is an integrity failure and never updates the expected recipe hash.
func downloadBuildArchive(ctx context.Context, client *http.Client, address, expected, cachePath string, out io.Writer) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(time.Duration(attempt-1) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return 0, err
		}
		response, err := client.Do(req)
		if err != nil {
			if attempt < 3 && ctx.Err() == nil {
				fmt.Fprintf(out, "Archive connection failed; retry %d/3.\n", attempt+1)
				continue
			}
			return 0, fmt.Errorf("archive request failed: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			if attempt < 3 && (response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500) {
				fmt.Fprintf(out, "Archive HTTP %d; retry %d/3.\n", response.StatusCode, attempt+1)
				continue
			}
			return 0, fmt.Errorf("archive download returned HTTP %d", response.StatusCode)
		}
		if response.ContentLength > buildDownloadMaxBytes {
			response.Body.Close()
			return 0, fmt.Errorf("archive exceeds 128 MiB limit")
		}
		tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".archive-")
		if err != nil {
			response.Body.Close()
			return 0, err
		}
		h := sha256.New()
		progress := &buildDownloadProgress{out: out, last: time.Now()}
		n, copyErr := io.Copy(io.MultiWriter(tmp, h, progress), io.LimitReader(response.Body, buildDownloadMaxBytes+1))
		response.Body.Close()
		if copyErr == nil {
			copyErr = tmp.Sync()
		}
		if closeErr := tmp.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			os.Remove(tmp.Name())
			if attempt < 3 && ctx.Err() == nil {
				fmt.Fprintf(out, "Archive transfer interrupted; retry %d/3.\n", attempt+1)
				continue
			}
			return 0, copyErr
		}
		if n > buildDownloadMaxBytes || hex.EncodeToString(h.Sum(nil)) != expected {
			os.Remove(tmp.Name())
			return 0, fmt.Errorf("archive SHA256 mismatch or size limit exceeded; expected hash was not changed")
		}
		if err := os.Rename(tmp.Name(), cachePath); err != nil {
			os.Remove(tmp.Name())
			return 0, err
		}
		return n, nil
	}
	return 0, fmt.Errorf("archive download attempts exhausted")
}

type buildDownloadProgress struct {
	out   io.Writer
	bytes int64
	last  time.Time
}

func (p *buildDownloadProgress) Write(data []byte) (int, error) {
	p.bytes += int64(len(data))
	if time.Since(p.last) >= 10*time.Second {
		fmt.Fprintf(p.out, "Archive downloaded %d MiB; verifying SHA256 before use.\n", p.bytes>>20)
		p.last = time.Now()
	}
	return len(data), nil
}

func copyVerifiedBuildArchive(cachePath, contextDir, destination, expected string) error {
	parent := filepath.Dir(filepath.Join(contextDir, filepath.FromSlash(destination)))
	// The extracted context rejects symlinks. Check the relevant directories again
	// before placing auxiliary bytes so local edits cannot escape the context.
	rel, err := filepath.Rel(contextDir, parent)
	if err != nil {
		return err
	}
	current := contextDir
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, name)
		if err := os.Mkdir(current, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		st, err := os.Lstat(current)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive destination has an unsafe parent")
		}
	}
	source, err := os.Open(cachePath)
	if err != nil {
		return err
	}
	defer source.Close()
	tmp, err := os.CreateTemp(parent, ".archive-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(source, buildDownloadMaxBytes+1))
	if closeErr := tmp.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return copyErr
	}
	if n > buildDownloadMaxBytes || hex.EncodeToString(h.Sum(nil)) != expected {
		return fmt.Errorf("cached archive changed while copying")
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(contextDir, filepath.FromSlash(destination)))
}

func (m *Manager) prepareBuildDownloads(ctx context.Context, v ImageBuild, contextDir string, out io.Writer) error {
	if _, err := os.Lstat(filepath.Join(contextDir, "build-downloads.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(v.ID), "context.zip"), v.ContextSHA256)
	if err != nil {
		return err
	}
	manifest, err := readBuildDownloadManifest(data)
	if err != nil || manifest == nil {
		return err
	}
	select {
	case buildDownloadGate <- struct{}{}:
		defer func() { <-buildDownloadGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	cacheDir := filepath.Join(m.Data, "build-download-cache", "sha256")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}
	client := buildDownloadHTTPClient()
	defer client.CloseIdleConnections()
	evidence := struct {
		SchemaVersion int                     `json:"schemaVersion"`
		Items         []BuildDownloadEvidence `json:"items"`
	}{SchemaVersion: 1, Items: []BuildDownloadEvidence{}}
	var total int64
	for _, item := range manifest.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		address, err := buildDownloadURL(item, v.BuildSources)
		if err != nil {
			return err
		}
		cachePath := filepath.Join(cacheDir, item.SHA256+".archive")
		destination := filepath.Join(contextDir, filepath.FromSlash(item.Destination))
		if _, err := os.Lstat(destination); err == nil {
			n, valid := verifiedDownloadFile(destination, item.SHA256)
			if !valid {
				return fmt.Errorf("bundled archive %s does not match its frozen hash; refusing to overwrite a recipe file", item.ID)
			}
			fmt.Fprintf(out, "Archive %s: verified bundled SHA256 %s (%d bytes).\n", item.ID, item.SHA256, n)
			evidence.Items = append(evidence.Items, BuildDownloadEvidence{ID: item.ID, URL: address, SHA256: item.SHA256, Destination: item.Destination, Bytes: n, Bundled: true, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		n, cacheHit := verifiedDownloadFile(cachePath, item.SHA256)
		if cacheHit {
			fmt.Fprintf(out, "Archive %s: verified cached SHA256 %s (%d bytes).\n", item.ID, item.SHA256, n)
		} else {
			if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			fmt.Fprintf(out, "Archive %s: downloading %s (expected SHA256 %s).\n", item.ID, address, item.SHA256)
			n, err = downloadBuildArchive(ctx, client, address, item.SHA256, cachePath, out)
			if err != nil {
				return fmt.Errorf("prepare archive %s: %w", item.ID, err)
			}
		}
		total += n
		if total+v.ContextBytes > buildContextLimit {
			return fmt.Errorf("build context with auxiliary archives exceeds 128 MiB limit")
		}
		if err := copyVerifiedBuildArchive(cachePath, contextDir, item.Destination, item.SHA256); err != nil {
			return err
		}
		evidence.Items = append(evidence.Items, BuildDownloadEvidence{ID: item.ID, URL: address, SHA256: item.SHA256, Destination: item.Destination, Bytes: n, CacheHit: cacheHit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.buildDir(v.ID), "downloads.json"), append(encoded, '\n'), 0600)
}
