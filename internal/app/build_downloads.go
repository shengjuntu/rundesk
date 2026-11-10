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
	"strconv"
	"strings"
	"sync/atomic"
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

// Partial bytes are auxiliary cache data, never recipe inputs. The original URL
// and frozen SHA bind a partial to its request even without HTTP validators.
// A strong validator additionally enables If-Range; only the complete SHA can
// authorize promotion into the content-addressed, verified archive cache.
type buildDownloadPartial struct {
	SchemaVersion int    `json:"schemaVersion"`
	URLSHA256     string `json:"urlSha256"`
	SHA256        string `json:"sha256"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"lastModified,omitempty"`
	Total         int64  `json:"total,omitempty"`
}

type buildDownloadPolicy struct {
	Idle      time.Duration
	RetryBase time.Duration
}

func buildDownloadStrongETag(value string) bool {
	if len(value) < 2 || len(value) > 512 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for _, b := range []byte(value[1 : len(value)-1]) {
		if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
			return false
		}
	}
	return true
}
func buildDownloadRemovePartial(part string) {
	_ = os.Remove(part)
	_ = os.Remove(part + ".json")
}
func buildDownloadReadPartial(part, address, expected string) (buildDownloadPartial, int64, error) {
	fresh := buildDownloadPartial{SchemaVersion: 1, URLSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(address))), SHA256: expected}
	info, err := os.Lstat(part)
	if errors.Is(err, os.ErrNotExist) {
		// Removing a stale sidecar never follows a link into another file.
		_ = os.Remove(part + ".json")
		return fresh, 0, nil
	}
	if err != nil {
		return fresh, 0, err
	}
	if !info.Mode().IsRegular() {
		return fresh, 0, fmt.Errorf("archive partial must be a regular file")
	}
	side, err := os.Lstat(part + ".json")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fresh, 0, err
	}
	if err == nil && !side.Mode().IsRegular() {
		return fresh, 0, fmt.Errorf("archive partial metadata must be a regular file")
	}
	if err != nil || side.Size() > 4096 || info.Size() <= 0 || info.Size() > buildDownloadMaxBytes {
		buildDownloadRemovePartial(part)
		return fresh, 0, nil
	}
	body, err := os.ReadFile(part + ".json")
	if err != nil {
		return fresh, 0, err
	}
	var old buildDownloadPartial
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	valid := d.Decode(&old) == nil && d.Decode(new(any)) == io.EOF
	valid = valid && old.SchemaVersion == 1 && old.URLSHA256 == fresh.URLSHA256 && old.SHA256 == expected && old.Total >= 0 && old.Total <= buildDownloadMaxBytes && (old.Total == 0 || info.Size() <= old.Total)
	if old.ETag != "" {
		valid = valid && buildDownloadStrongETag(old.ETag)
	}
	if old.LastModified != "" {
		_, e := http.ParseTime(old.LastModified)
		valid = valid && len(old.LastModified) <= 128 && e == nil
	}
	if !valid {
		buildDownloadRemovePartial(part)
		return fresh, 0, nil
	}
	return old, info.Size(), nil
}

// Verify the opened inode before truncating an existing partial. A changed
// path or symlink cannot redirect a restart into a different local file.
func buildDownloadOpenPartial(part string, offset int64) (*os.File, error) {
	prior, err := os.Lstat(part)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil && !prior.Mode().IsRegular() {
		return nil, fmt.Errorf("archive partial must be a regular file")
	}
	flags := os.O_WRONLY
	if prior == nil {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(part, flags, 0600)
	if err != nil {
		return nil, err
	}
	current, err := file.Stat()
	if err != nil || !current.Mode().IsRegular() || (prior != nil && !os.SameFile(prior, current)) || (offset > 0 && current.Size() != offset) {
		file.Close()
		return nil, fmt.Errorf("archive partial changed before transfer")
	}
	if offset == 0 {
		err = file.Truncate(0)
	}
	if err == nil {
		_, err = file.Seek(offset, io.SeekStart)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func buildDownloadWritePartial(part string, value buildDownloadPartial) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(part), ".partial-metadata-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(append(body, '\n')); err == nil {
		err = file.Sync()
	}
	if e := file.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(name, part+".json")
}
func buildDownloadResponsePartial(old buildDownloadPartial, response *http.Response, total int64) buildDownloadPartial {
	old.ETag, old.LastModified, old.Total = "", "", total
	if etag := response.Header.Get("ETag"); buildDownloadStrongETag(etag) {
		old.ETag = etag
	}
	if modified := response.Header.Get("Last-Modified"); len(modified) <= 128 {
		if _, err := http.ParseTime(modified); err == nil {
			old.LastModified = modified
		}
	}
	return old
}
func buildDownloadValidatorMatches(old buildDownloadPartial, response *http.Response) bool {
	if old.ETag != "" {
		return response.Header.Get("ETag") == old.ETag
	}
	if old.LastModified != "" {
		return response.Header.Get("Last-Modified") == old.LastModified
	}
	return true // Same URL and final frozen SHA remain authoritative.
}
func buildDownloadRange(header string) (start, end, total int64, err error) {
	match := regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`).FindStringSubmatch(header)
	if len(match) != 4 {
		return 0, 0, 0, fmt.Errorf("invalid archive Content-Range")
	}
	start, err = strconv.ParseInt(match[1], 10, 64)
	if err == nil {
		end, err = strconv.ParseInt(match[2], 10, 64)
	}
	if err == nil {
		total, err = strconv.ParseInt(match[3], 10, 64)
	}
	if err != nil || start < 0 || end < start || total <= end || total > buildDownloadMaxBytes {
		return 0, 0, 0, fmt.Errorf("invalid archive Content-Range or 128 MiB size limit")
	}
	return start, end, total, nil
}
func buildDownloadPromote(part, cachePath, expected string) (int64, error) {
	n, ok := verifiedDownloadFile(part, expected)
	if !ok {
		buildDownloadRemovePartial(part)
		return 0, fmt.Errorf("archive SHA256 mismatch or size limit exceeded; expected hash was not changed")
	}
	if err := os.Rename(part, cachePath); err != nil {
		return 0, err
	}
	_ = os.Remove(part + ".json")
	return n, nil
}

// The enclosing build job supplies the overall deadline. Successful, slow
// transfers are not cut off after an arbitrary 15 minutes. Headers still have
// a transport deadline, and a body that stops delivering bytes is canceled.
func downloadBuildArchive(ctx context.Context, client *http.Client, address, expected, cachePath string, out io.Writer) (int64, error) {
	return downloadBuildArchiveWithPolicy(ctx, client, address, expected, cachePath, out, buildDownloadPolicy{Idle: 120 * time.Second, RetryBase: time.Second})
}
func downloadBuildArchiveWithPolicy(ctx context.Context, client *http.Client, address, expected, cachePath string, out io.Writer, policy buildDownloadPolicy) (int64, error) {
	part := cachePath + ".part"
	if !buildDownloadSHA.MatchString(expected) {
		return 0, fmt.Errorf("invalid expected archive SHA256")
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(time.Duration(attempt-1) * policy.RetryBase):
			}
		}
		saved, offset, err := buildDownloadReadPartial(part, address, expected)
		if err != nil {
			return 0, err
		}
		if offset > 0 {
			if _, valid := verifiedDownloadFile(part, expected); valid {
				return buildDownloadPromote(part, cachePath, expected)
			}
			if saved.Total > 0 && saved.Total == offset {
				return buildDownloadPromote(part, cachePath, expected)
			}
			fmt.Fprintf(out, "Archive resume: retaining %.1f MiB (%d bytes); attempt %d/3.\n", float64(offset)/(1<<20), offset, attempt)
		}
		requestCtx, cancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, address, nil)
		if err != nil {
			cancel()
			return 0, err
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if saved.ETag != "" {
				req.Header.Set("If-Range", saved.ETag)
			} else if saved.LastModified != "" {
				req.Header.Set("If-Range", saved.LastModified)
			}
		}
		response, err := client.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if attempt < 3 {
				fmt.Fprintf(out, "Archive connection failed; retry %d/3, saved bytes retained.\n", attempt+1)
				continue
			}
			return 0, fmt.Errorf("archive request failed: %w", err)
		}
		closeResponse := func() { cancel(); response.Body.Close() }
		if response.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
			closeResponse()
			size, parseErr := strconv.ParseInt(strings.TrimPrefix(response.Header.Get("Content-Range"), "bytes */"), 10, 64)
			if strings.HasPrefix(response.Header.Get("Content-Range"), "bytes */") && parseErr == nil && size == offset && size <= buildDownloadMaxBytes {
				return buildDownloadPromote(part, cachePath, expected)
			}
			buildDownloadRemovePartial(part)
			return 0, fmt.Errorf("invalid unsatisfied archive range; partial discarded")
		}
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
			closeResponse()
			if attempt < 3 && (response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500) {
				fmt.Fprintf(out, "Archive HTTP %d; retry %d/3, saved bytes retained.\n", response.StatusCode, attempt+1)
				continue
			}
			return 0, fmt.Errorf("archive download returned HTTP %d", response.StatusCode)
		}
		if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
			closeResponse()
			buildDownloadRemovePartial(part)
			return 0, fmt.Errorf("archive response must preserve identity byte encoding")
		}
		total, segment := response.ContentLength, response.ContentLength
		if total < 0 {
			total = 0
		}
		if response.StatusCode == http.StatusPartialContent {
			start, end, size, rangeErr := buildDownloadRange(response.Header.Get("Content-Range"))
			if rangeErr != nil || offset == 0 || start != offset || (segment >= 0 && segment != end-start+1) || (saved.Total > 0 && saved.Total != size) || !buildDownloadValidatorMatches(saved, response) {
				closeResponse()
				buildDownloadRemovePartial(part)
				return 0, fmt.Errorf("invalid archive resume range/validator; partial discarded")
			}
			total, segment = size, end-start+1
		} else if offset > 0 {
			fmt.Fprintln(out, "Archive server returned a full response; restarting safely from byte zero.")
			offset = 0
		}
		if total > buildDownloadMaxBytes {
			closeResponse()
			buildDownloadRemovePartial(part)
			return 0, fmt.Errorf("archive exceeds 128 MiB limit")
		}
		if offset == 0 {
			saved = buildDownloadResponsePartial(saved, response, total)
		} else {
			saved.Total = total
		}
		file, err := buildDownloadOpenPartial(part, offset)
		if err != nil {
			closeResponse()
			return 0, err
		}
		if err = buildDownloadWritePartial(part, saved); err != nil {
			file.Close()
			closeResponse()
			return 0, err
		}
		var idle atomic.Bool
		timer := time.AfterFunc(policy.Idle, func() { idle.Store(true); cancel() })
		progress := &buildDownloadProgress{out: out, base: offset, total: total, last: time.Now(), started: time.Now(), received: func() { timer.Reset(policy.Idle) }}
		readLimit := buildDownloadMaxBytes - offset + 1
		if segment >= 0 && segment+1 < readLimit {
			readLimit = segment + 1
		}
		n, copyErr := io.Copy(io.MultiWriter(file, progress), io.LimitReader(response.Body, readLimit))
		timer.Stop()
		closeResponse()
		if syncErr := file.Sync(); copyErr == nil {
			copyErr = syncErr
		}
		if closeErr := file.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if offset+n > buildDownloadMaxBytes {
			buildDownloadRemovePartial(part)
			return 0, fmt.Errorf("archive exceeds 128 MiB limit")
		}
		if segment >= 0 && n > segment {
			buildDownloadRemovePartial(part)
			return 0, fmt.Errorf("archive response exceeded its declared byte range")
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if idle.Load() {
			copyErr = fmt.Errorf("archive transfer stopped receiving bytes for %s", policy.Idle)
		}
		if copyErr == nil && ((segment >= 0 && n != segment) || (total > 0 && offset+n != total)) {
			copyErr = io.ErrUnexpectedEOF
		}
		if copyErr != nil {
			var diskError *os.PathError
			if attempt < 3 && !errors.As(copyErr, &diskError) {
				fmt.Fprintf(out, "Archive transfer interrupted after %d bytes; retry %d/3 without discarding progress.\n", offset+n, attempt+1)
				continue
			}
			return 0, copyErr
		}
		n, err = buildDownloadPromote(part, cachePath, expected)
		if err == nil {
			fmt.Fprintf(out, "Archive complete: %.1f MiB; SHA256 verified.\n", float64(n)/(1<<20))
		}
		return n, err
	}
	return 0, fmt.Errorf("archive download attempts exhausted")
}

type buildDownloadProgress struct {
	out      io.Writer
	bytes    int64
	base     int64
	total    int64
	last     time.Time
	started  time.Time
	received func()
}

func (p *buildDownloadProgress) Write(data []byte) (int, error) {
	p.bytes += int64(len(data))
	if len(data) > 0 && p.received != nil {
		p.received()
	}
	if time.Since(p.last) >= 10*time.Second {
		speed := float64(p.bytes) / (1 << 10) / time.Since(p.started).Seconds()
		if p.total > 0 {
			fmt.Fprintf(p.out, "Archive downloaded %.1f/%.1f MiB (%.1f%%), %.1f KiB/s; SHA256 verification pending.\n", float64(p.base+p.bytes)/(1<<20), float64(p.total)/(1<<20), 100*float64(p.base+p.bytes)/float64(p.total), speed)
		} else {
			fmt.Fprintf(p.out, "Archive downloaded %.1f MiB, %.1f KiB/s; SHA256 verification pending.\n", float64(p.base+p.bytes)/(1<<20), speed)
		}
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
