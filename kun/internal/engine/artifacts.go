package kun

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"
)

const maxArtifactBytes = 1 << 20

type artifactObservation struct {
	data         []byte
	size         *int64
	hash, reason string
}

// Root prevents traversal (including symlinks) outside the workspace. Nonblock
// prevents a substituted FIFO from hanging capture; only regular files qualify.
// Every unique path is read once; no content is retained in the journal.
func observeArtifact(root *os.Root, path string) artifactObservation {
	f, err := root.OpenFile(filepath.FromSlash(path), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return artifactObservation{reason: "missing"}
		}
		return artifactObservation{reason: "unavailable"}
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return artifactObservation{reason: "unavailable"}
	}
	if !before.Mode().IsRegular() {
		return artifactObservation{reason: "not_regular"}
	}
	size := before.Size()
	out := artifactObservation{size: &size}
	if size > maxArtifactBytes {
		out.reason = "too_large"
		return out
	}
	data, err := io.ReadAll(io.LimitReader(f, maxArtifactBytes+1))
	if err != nil {
		out.reason = "unavailable"
		return out
	}
	if len(data) > maxArtifactBytes {
		out.reason = "too_large"
		return out
	}
	after, err := f.Stat()
	current, pathErr := root.Stat(filepath.FromSlash(path))
	if err != nil || pathErr != nil || !os.SameFile(before, current) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || int64(len(data)) != size {
		out.reason = "changed_during_capture"
		return out
	}
	out.data = data
	out.hash = fmt.Sprintf("%x", sha256.Sum256(data))
	return out
}
func captureArtifacts(workspace string, rules []p.ArtifactRule) *p.ArtifactEvidence {
	if len(rules) == 0 {
		return nil
	}
	out := &p.ArtifactEvidence{RulesHash: p.ArtifactRulesHash(rules), Results: []p.ArtifactResult{}}
	root, err := os.OpenRoot(workspace)
	if err == nil {
		defer root.Close()
	}
	seen := map[string]artifactObservation{}
	for i, r := range rules {
		v, ok := seen[r.Path]
		if !ok {
			v.reason = "workspace_unavailable"
			if err == nil {
				v = observeArtifact(root, r.Path)
			}
			seen[r.Path] = v
		}
		result := p.ArtifactResult{Index: i, Path: r.Path, Kind: r.Kind, Status: "unknown", Reason: v.reason, Bytes: v.size, SHA256: v.hash}
		switch v.reason {
		case "missing", "not_regular":
			result.Status = "fail"
		case "":
			pass := false
			switch r.Kind {
			case "file_exists":
				pass = true
			case "file_sha256":
				pass = v.hash == r.Value
			case "file_contains", "file_equals":
				if !utf8.Valid(v.data) {
					result.Reason = "invalid_utf8"
					break
				}
				if r.Kind == "file_contains" {
					pass = bytes.Contains(v.data, []byte(r.Value))
				} else {
					pass = bytes.Equal(v.data, []byte(r.Value))
				}
			}
			if result.Reason == "" {
				result.Status = "fail"
				result.Reason = "not_matched"
				if pass {
					result.Status = "pass"
					result.Reason = "matched"
				}
			}
		}
		out.Results = append(out.Results, result)
	}
	return out
}
