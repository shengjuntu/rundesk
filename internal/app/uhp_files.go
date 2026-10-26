package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Uploads are bounded independently of output artifacts. Artifact downloads use
// the existing 512 MiB resource-reader limit and never retain another copy.
const UHPFileLimit int64 = 64 << 20

type uhpFileRecord struct {
	ID           string `json:"id"`
	Owner        string `json:"owner"`
	ContainerID  string `json:"containerId,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	WorkspaceID  string `json:"workspaceId,omitempty"`
	Path         string `json:"path,omitempty"`
	Filename     string `json:"filename"`
	Bytes        int64  `json:"bytes"`
	CreatedAt    int64  `json:"createdAt"`
	SHA256       string `json:"sha256"`
	Deleted      bool   `json:"deleted,omitempty"`
	Availability string `json:"availability,omitempty"`
}

func (f uhpFileRecord) Wire() map[string]any {
	v := map[string]any{"id": f.ID, "object": "file", "filename": f.Filename, "bytes": f.Bytes, "created_at": f.CreatedAt}
	if f.ContainerID != "" {
		v["container_id"] = f.ContainerID
	}
	if f.Availability != "" {
		v["rundesk_availability"] = f.Availability
	}
	return v
}

type uhpFiles struct{ Manager *Manager }

func uhpOpaqueID(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) <= len(prefix) || len(id) > 128 {
		return false
	}
	for _, c := range id[len(prefix):] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func uhpFileMissing() error { return failure(404, "file_not_found", "File not found.") }

func (f uhpFiles) Record(owner, containerID, id string) (uhpFileRecord, error) {
	var record uhpFileRecord
	if owner == "" || !uhpOpaqueID(id, "file_") || (containerID != "" && !uhpOpaqueID(containerID, "cntr_")) {
		return record, uhpFileMissing()
	}
	err := f.Manager.Store.Get("uhp-file", id, &record)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (record.Owner != owner || record.ID != id || record.Deleted || record.ContainerID != containerID)) {
		return uhpFileRecord{}, uhpFileMissing()
	}
	return record, err
}

func (f uhpFiles) uploadPath(record uhpFileRecord) string {
	return filepath.Join(f.Manager.Data, "uhp", "uploads", record.ID)
}

// Upload keeps a single original outside workspaces because the upload API does
// not yet select a harness/workspace. PrepareInput materializes it at most once
// per workspace, where native runtimes are permitted to read attachments.
func (f uhpFiles) Upload(owner, filename string, src io.Reader) (uhpFileRecord, error) {
	if owner == "" {
		return uhpFileRecord{}, failure(401, "invalid_credential", "A valid bearer token is required.")
	}
	f.Manager.libraryMu.Lock()
	defer f.Manager.libraryMu.Unlock()
	r := uhpFileRecord{ID: "file_" + store.ID(), Owner: owner, Filename: cleanPersonalName(filename), CreatedAt: time.Now().Unix()}
	root, err := os.OpenRoot(f.Manager.Data)
	if err != nil {
		return r, err
	}
	defer root.Close()
	if err = root.MkdirAll("uhp/uploads", 0700); err != nil {
		return r, err
	}
	p := "uhp/uploads/" + r.ID
	out, err := root.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return r, err
	}
	r.Bytes, r.SHA256, err = uhpCopyInput(out, src)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Manager.Store.Put("uhp-file", r.ID, r)
	}
	if err != nil {
		_ = root.Remove(p)
	}
	return r, err
}

func uhpCopyInput(dst io.Writer, src io.Reader) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, UHPFileLimit+1))
	if n > UHPFileLimit {
		err = failure(413, "file_too_large", "File exceeds the 64 MiB upload limit.")
	}
	return n, hex.EncodeToString(h.Sum(nil)), err
}

// Inline writes the attachment directly into its workspace; there is no second
// managed upload or snapshot. Only data URLs are supported: arbitrary network
// fetches would cross the caller's resource and credential boundary.
func (f uhpFiles) Inline(owner, workspaceID, filename, dataURL string) (string, error) {
	if owner == "" {
		return "", failure(401, "invalid_credential", "A valid bearer token is required.")
	}
	meta, encoded, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(meta, "data:") || !strings.HasSuffix(meta, ";base64") {
		return "", failure(400, "invalid_input", "Inline files must use a base64 data URL.")
	}
	if int64(len(encoded)) > (UHPFileLimit+2)/3*4 {
		return "", failure(413, "file_too_large", "File exceeds the 64 MiB upload limit.")
	}
	f.Manager.libraryMu.Lock()
	defer f.Manager.libraryMu.Unlock()
	r := uhpFileRecord{ID: "file_" + store.ID(), Owner: owner, Filename: cleanPersonalName(filename), WorkspaceID: workspaceID, CreatedAt: time.Now().Unix()}
	w, err := f.Manager.Workspace(workspaceID)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(w.Path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = root.MkdirAll("uploads", 0700); err != nil {
		return "", err
	}
	r.Path = "uploads/" + r.ID + "-" + r.Filename
	out, err := root.OpenFile(r.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	r.Bytes, r.SHA256, err = uhpCopyInput(out, base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)))
	var badEncoding base64.CorruptInputError
	if errors.As(err, &badEncoding) || errors.Is(err, io.ErrUnexpectedEOF) {
		err = failure(400, "invalid_input", "Inline file data is not valid base64.")
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Manager.Store.Put("uhp-file", r.ID, r)
	}
	if err != nil {
		_ = root.Remove(r.Path)
		return "", err
	}
	return r.Path, nil
}

func (f uhpFiles) inputContent(r uhpFileRecord) (*os.File, error) {
	var src *os.File
	var err error
	if r.WorkspaceID != "" {
		src, err = f.Manager.openArtifactSource(resource.FileSource{WorkspaceID: r.WorkspaceID, Path: r.Path})
	} else {
		root, openErr := os.OpenRoot(filepath.Join(f.Manager.Data, "uhp", "uploads"))
		if openErr != nil {
			return nil, uhpFileMissing()
		}
		defer root.Close()
		info, statErr := root.Lstat(r.ID)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, uhpFileMissing()
		}
		src, err = root.Open(r.ID)
	}
	if err != nil {
		return nil, uhpFileMissing()
	}
	size, hash, err := personalDigest(src)
	if err == nil && (size != r.Bytes || hash != r.SHA256) {
		err = failure(409, "rundesk_file_changed", "File content changed since it was registered.")
	}
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	return src, nil
}

func (f uhpFiles) PrepareInput(owner, workspaceID, fileID string) (string, error) {
	return f.prepareInput(owner, workspaceID, fileID, false)
}

// Image input is classified from its bytes, never from a caller's filename.
// Normalize a materialized filename so the native Codex localImage path uses
// the verified image type even when the uploaded filename was image.bin.
func (f uhpFiles) PrepareImageInput(owner, workspaceID, fileID string) (string, error) {
	return f.prepareInput(owner, workspaceID, fileID, true)
}

func (f uhpFiles) prepareInput(owner, workspaceID, fileID string, image bool) (string, error) {
	f.Manager.libraryMu.Lock()
	defer f.Manager.libraryMu.Unlock()
	r, err := f.Record(owner, "", fileID)
	if err != nil {
		return "", err
	}
	src, err := f.inputContent(r)
	if err != nil {
		return "", err
	}
	defer src.Close()
	ext := ""
	if image {
		ext, err = uhpImageExtension(src)
		if err != nil {
			return "", err
		}
		if _, err = src.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		r.Filename = strings.TrimSuffix(r.Filename, filepath.Ext(r.Filename)) + ext
	}
	if r.WorkspaceID == workspaceID && strings.HasPrefix(r.Path, "uploads/") && (!image || strings.EqualFold(filepath.Ext(r.Path), ext)) {
		return r.Path, nil
	}
	linkID := libraryLinkID(workspaceID, r.ID+ext)
	var attached uhpFileRecord
	if err = f.Manager.Store.Get("uhp-file-link", linkID, &attached); err == nil {
		verified, err := f.inputContent(attached)
		if err != nil {
			return "", err
		}
		_ = verified.Close()
		return attached.Path, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	w, err := f.Manager.Workspace(workspaceID)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(w.Path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = root.MkdirAll("uploads", 0700); err != nil {
		return "", err
	}
	p := "uploads/" + store.ID() + "-" + r.Filename
	out, err := root.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	err = copyPersonalVerified(out, src, PersonalFile{Size: r.Bytes, SHA256: r.SHA256})
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	attached = r
	attached.WorkspaceID, attached.Path = workspaceID, p
	if err == nil {
		err = f.Manager.Store.Put("uhp-file-link", linkID, attached)
	}
	if err != nil {
		_ = root.Remove(p)
		return "", err
	}
	return p, nil
}

func uhpImageExtension(src io.Reader) (string, error) {
	var prefix [512]byte
	n, err := io.ReadFull(src, prefix[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", failure(400, "invalid_input", "Image data could not be read.")
	}
	switch http.DetectContentType(prefix[:n]) {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", failure(422, "rundesk_image_unsupported", "Image input must contain PNG, JPEG, or WebP bytes.")
	}
}

func (f uhpFiles) InlineImage(owner, workspaceID, filename, dataURL string) (string, error) {
	meta, encoded, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(meta, "data:") || !strings.HasSuffix(meta, ";base64") {
		return "", failure(400, "invalid_input", "Inline images must use a base64 data URL.")
	}
	if int64(len(encoded)) > (UHPFileLimit+2)/3*4 {
		return "", failure(413, "file_too_large", "Image exceeds the 64 MiB upload limit.")
	}
	ext, err := uhpImageExtension(base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)))
	if err != nil {
		return "", err
	}
	filename = cleanPersonalName(filename)
	filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ext
	return f.Inline(owner, workspaceID, filename, dataURL)
}

// DiscoverArtifacts indexes live original files from every turn of the session.
// Content identity is part of the ID: if a path is overwritten, a historical
// response cannot accidentally serve the replacement as its earlier artifact.
func (f uhpFiles) DiscoverArtifacts(owner, sessionID, hostSessionID, containerID string) ([]uhpFileRecord, error) {
	out := []uhpFileRecord{}
	if owner == "" || !uhpOpaqueID(containerID, "cntr_") || !uhpOpaqueID(sessionID, "hsess") || !safePath(hostSessionID) || strings.Contains(hostSessionID, "/") {
		return out, uhpFileMissing()
	}
	f.Manager.libraryMu.Lock()
	defer f.Manager.libraryMu.Unlock()
	s, err := f.Manager.Session(hostSessionID)
	if err != nil {
		return out, err
	}
	w, err := f.Manager.Workspace(s.WorkspaceID)
	if err != nil {
		return out, err
	}
	root, err := os.OpenRoot(w.Path)
	if err != nil {
		return out, err
	}
	defer root.Close()
	base := "outputs/" + hostSessionID
	for _, p := range []string{"outputs", base} {
		info, err := root.Lstat(p)
		if os.IsNotExist(err) {
			return f.artifactHistory(owner, sessionID, out)
		}
		if err != nil {
			return out, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return out, failure(409, "rundesk_file_source_invalid", "Artifact directory is unavailable.")
		}
	}
	err = fs.WalkDir(root.FS(), base, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		src, err := f.Manager.openArtifactSource(resource.FileSource{WorkspaceID: s.WorkspaceID, Path: p})
		if err != nil {
			return err
		}
		size, hash, err := personalDigest(src)
		_ = src.Close()
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(owner + "\x00" + sessionID + "\x00" + p + "\x00" + hash))
		id := "file_" + hex.EncodeToString(digest[:24])
		var r uhpFileRecord
		if err = f.Manager.Store.Get("uhp-file", id, &r); errors.Is(err, sql.ErrNoRows) {
			r = uhpFileRecord{ID: id, Owner: owner, ContainerID: containerID, SessionID: sessionID, WorkspaceID: s.WorkspaceID, Path: p, Filename: strings.TrimPrefix(p, base+"/"), Bytes: size, SHA256: hash, CreatedAt: time.Now().Unix()}
			err = f.Manager.Store.Put("uhp-file", id, r)
		}
		if err != nil {
			return err
		}
		if !r.Deleted {
			r.Availability = "available"
			out = append(out, r)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	return f.artifactHistory(owner, sessionID, out)
}

// A session lists its entire file history, including references whose original
// bytes have since changed or vanished. Their availability is explicit rather
// than fabricating a historical download or retaining an unsolicited snapshot.
func (f uhpFiles) artifactHistory(owner, sessionID string, live []uhpFileRecord) ([]uhpFileRecord, error) {
	byID := map[string]uhpFileRecord{}
	byPath := map[string]bool{}
	for _, r := range live {
		byID[r.ID] = r
		byPath[r.Path] = true
	}
	rows, err := f.Manager.Store.List("uhp-file")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var r uhpFileRecord
		if err := json.Unmarshal(row, &r); err != nil {
			return nil, err
		}
		if r.Owner != owner || r.SessionID != sessionID || r.Deleted {
			continue
		}
		if _, ok := byID[r.ID]; !ok {
			r.Availability = "missing"
			if byPath[r.Path] {
				r.Availability = "changed"
			}
			byID[r.ID] = r
		}
	}
	out := make([]uhpFileRecord, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Filename == out[j].Filename {
			return out[i].ID < out[j].ID
		}
		return out[i].Filename < out[j].Filename
	})
	return out, nil
}

// Content returns a verified transient read buffer, never a retained snapshot.
// Callers must also authorize the owning UHP session before invoking this method.
func (f uhpFiles) Content(owner, containerID, id string) (uhpFileRecord, *os.File, func(), error) {
	r, err := f.Record(owner, containerID, id)
	if err != nil {
		return r, nil, nil, err
	}
	if r.ContainerID == "" || r.SessionID == "" || !strings.HasPrefix(r.Path, "outputs/") {
		return r, nil, nil, uhpFileMissing()
	}
	src, cleanup, err := f.Manager.openPersonalDownload(PersonalFile{Storage: resource.Reference, Source: &resource.FileSource{WorkspaceID: r.WorkspaceID, Path: r.Path}, Size: r.Bytes, SHA256: r.SHA256})
	if err != nil {
		var api *apiError
		if errors.As(err, &api) && api.Status == 409 {
			err = failure(409, "rundesk_file_changed", "The original artifact is no longer available in its recorded version.")
		} else {
			err = uhpFileMissing()
		}
	}
	return r, src, cleanup, err
}

// DeleteSession revokes references; the task/session controller owns deletion
// of session resources. It must never remove a shared workspace here.
func (f uhpFiles) DeleteSession(owner, sessionID string) error {
	f.Manager.libraryMu.Lock()
	defer f.Manager.libraryMu.Unlock()
	rows, err := f.Manager.Store.List("uhp-file")
	if err != nil {
		return err
	}
	records := []store.Record{}
	for _, row := range rows {
		var r uhpFileRecord
		if err = json.Unmarshal(row, &r); err != nil {
			return err
		}
		if r.Owner == owner && r.SessionID == sessionID && !r.Deleted {
			r.Deleted = true
			records = append(records, store.Record{Kind: "uhp-file", ID: r.ID, Value: r})
		}
	}
	if len(records) == 0 {
		return nil
	}
	return f.Manager.Store.PutMany(records...)
}

func (f uhpFileRecord) Citation() map[string]any {
	return map[string]any{"type": "container_file_citation", "container_id": f.ContainerID, "file_id": f.ID, "filename": f.Filename, "download_url": "/v1/containers/" + f.ContainerID + "/files/" + f.ID + "/content", "start_index": 0, "end_index": 0}
}

// Download filenames never carry directory components into Content-Disposition.
func (f uhpFileRecord) DownloadName() string { return cleanPersonalName(path.Base(f.Filename)) }
