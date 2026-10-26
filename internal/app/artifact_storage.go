package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Personal artifacts retain their content identity independently of their
// storage policy. References never own workspace bytes. A snapshot is an
// explicit promotion of the same identity, not another catalog entry.
type PersonalArtifacts interface {
	PersonalFiles(owner, query string) ([]PersonalFile, error)
	AttachPersonal(owner, id, workspaceID string) (map[string]any, error)
	SnapshotPersonal(owner, id string) (PersonalFile, error)
	DeletePersonal(owner, id string) error
}

var _ PersonalArtifacts = (*Manager)(nil)

func (m *Manager) personalUsageLocked(owner string) (int64, error) {
	if owner == "" {
		return 0, failure(403, "personal_library_required", "个人文件仅供本人使用")
	}
	rows, err := m.Store.List("personal-file")
	if err != nil {
		return 0, err
	}
	var used int64
	count := 0
	for _, row := range rows {
		var f PersonalFile
		if err := json.Unmarshal(row, &f); err != nil {
			return 0, err
		}
		if f.Owner == owner && !f.Deleted {
			used += f.Size
			count++
		}
	}
	if count >= 10000 || used >= 10<<30 {
		return 0, failure(413, "personal_library_full", "个人文件库已满，请删除不需要的文件（最多 10000 个、10 GiB）")
	}
	return used, nil
}

// openArtifactSource keeps the original workspace as the authority. os.Root
// prevents escapes; rejecting all stable symlink components also prevents
// treating an alias as the identity of a historical artifact.
func (m *Manager) openArtifactSource(source resource.FileSource) (*os.File, error) {
	if !safePath(source.Path) || (!strings.HasPrefix(source.Path, "uploads/") && !strings.HasPrefix(source.Path, "outputs/")) {
		return nil, failure(409, "personal_file_source_invalid", "文件来源路径无效")
	}
	ws, err := m.Workspace(source.WorkspaceID)
	if err != nil {
		return nil, failure(410, "personal_file_source_missing", "文件来源工作区已不可用")
	}
	root, err := os.OpenRoot(ws.Path)
	if err != nil {
		return nil, failure(410, "personal_file_source_missing", "文件来源工作区已不可用")
	}
	defer root.Close()
	parts := strings.Split(source.Path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, failure(410, "personal_file_source_missing", "原始文件已移除或不可读取")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, failure(409, "personal_file_source_invalid", "文件来源不能经过符号链接")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, failure(409, "personal_file_source_invalid", "文件来源必须是普通文件")
		}
	}
	f, err := root.Open(source.Path)
	if err != nil {
		return nil, failure(410, "personal_file_source_missing", "原始文件已移除或不可读取")
	}
	return f, nil
}

func personalDigest(f *os.File) (int64, string, error) {
	before, err := f.Stat()
	if err != nil {
		return 0, "", err
	}
	if !before.Mode().IsRegular() {
		return 0, "", failure(409, "personal_file_source_invalid", "文件来源必须是普通文件")
	}
	if before.Size() > personalFileLimit {
		return 0, "", failure(413, "personal_file_limit", "文件超过 512 MiB")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, personalFileLimit+1))
	if err != nil {
		return 0, "", err
	}
	after, err := f.Stat()
	if err != nil {
		return 0, "", err
	}
	if n > personalFileLimit || before.Size() != n || after.Size() != n || !before.ModTime().Equal(after.ModTime()) {
		return 0, "", failure(409, "personal_file_source_changed", "原始文件正在变化，请在写入结束后重试")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) savePersonalReferenceLocked(owner, name, kind, sid, key string, source resource.FileSource, expectedHash string) (PersonalFile, error) {
	if key != "" {
		var link libraryLink
		if err := m.Store.Get("library-capture", key, &link); err == nil {
			return m.personalFile(link.FileID)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return PersonalFile{}, err
		}
	}
	used, err := m.personalUsageLocked(owner)
	if err != nil {
		return PersonalFile{}, err
	}
	src, err := m.openArtifactSource(source)
	if err != nil {
		return PersonalFile{}, err
	}
	defer src.Close()
	size, hash, err := personalDigest(src)
	if err != nil {
		return PersonalFile{}, err
	}
	if expectedHash != "" && hash != expectedHash {
		return PersonalFile{}, failure(409, "personal_file_source_changed", "收录期间原始文件已变化，未登记错误版本")
	}
	if used+size > 10<<30 {
		return PersonalFile{}, failure(413, "personal_file_limit", "个人库超过 10 GiB")
	}
	f := PersonalFile{ID: store.ID(), Owner: owner, Name: cleanPersonalName(name), Size: size, SHA256: hash, Created: store.Now(), Kind: kind, SessionID: sid, Storage: resource.Reference, Source: &source}
	link := libraryLink{FileID: f.ID, WorkspaceID: source.WorkspaceID, Path: source.Path, Original: true}
	records := []store.Record{
		{Kind: "personal-file", ID: f.ID, Value: f},
		{Kind: "library-link", ID: libraryLinkID(source.WorkspaceID, source.Path), Value: link},
	}
	if key != "" {
		records = append(records, store.Record{Kind: "library-capture", ID: key, Value: libraryLink{FileID: f.ID}})
	}
	return f, m.Store.PutMany(records...)
}

func (m *Manager) openPersonalContent(f PersonalFile) (*os.File, error) {
	var src *os.File
	var err error
	switch f.Storage {
	case resource.Reference:
		if f.Source == nil {
			return nil, failure(409, "personal_file_source_invalid", "文件引用缺少来源")
		}
		src, err = m.openArtifactSource(*f.Source)
	case "", resource.Managed, resource.Snapshot:
		src, err = os.Open(m.libraryPath(f))
		if os.IsNotExist(err) {
			err = failure(410, "personal_file_content_missing", "已保存的文件内容不可用")
		}
	default:
		return nil, failure(409, "personal_file_storage_unsupported", "不支持的文件存储方式")
	}
	if err != nil {
		return nil, err
	}
	size, hash, err := personalDigest(src)
	if err == nil && (size != f.Size || hash != f.SHA256) {
		if f.Storage == resource.Reference {
			err = failure(409, "personal_file_source_changed", "文件内容已与收录版本不同；原始版本未归档，不能当作旧产物读取")
		} else {
			err = failure(409, "personal_file_content_changed", "已保存的文件内容与记录的完整性校验不符")
		}
	}
	if err != nil {
		src.Close()
		return nil, err
	}
	return src, nil
}

// A reference points at mutable workspace bytes. Downloads therefore validate
// an ephemeral read buffer before sending any headers. This buffer is never a
// catalog artifact or retained archive, and is removed when the response ends.
// Keeping only the original descriptor after a hash check would still allow
// in-place writes to change bytes midway through an HTTP response.
func (m *Manager) openPersonalDownload(f PersonalFile) (*os.File, func(), error) {
	if f.Storage != resource.Reference {
		src, err := m.openPersonalContent(f)
		if err != nil {
			return nil, nil, err
		}
		return src, func() { _ = src.Close() }, nil
	}
	if f.Source == nil {
		return nil, nil, failure(409, "personal_file_source_invalid", "文件引用缺少来源")
	}
	src, err := m.openArtifactSource(*f.Source)
	if err != nil {
		return nil, nil, err
	}
	defer src.Close()
	tmp, err := os.CreateTemp("", "rundesk-artifact-*")
	if err != nil {
		return nil, nil, err
	}
	// Unix keeps the open descriptor valid after unlink, including if the host
	// crashes. Windows may retain the name until Close; cleanup covers it.
	_ = os.Remove(tmp.Name())
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	if err = copyPersonalVerified(tmp, src, f); err == nil {
		_, err = tmp.Seek(0, io.SeekStart)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tmp, cleanup, nil
}

// Verify the bytes actually materialized, including changes after the initial
// source check. A failed copy is removed by the caller and never registered.
func copyPersonalVerified(dst io.Writer, src io.Reader, f PersonalFile) error {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, personalFileLimit+1))
	if err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return failure(409, "personal_file_source_changed", "复制期间原始文件已变化，未保存错误版本")
	}
	return nil
}

// SnapshotPersonal preserves the current recorded version explicitly. It is
// safe to retry, retains the catalog ID, and never rewrites the source file.
func (m *Manager) SnapshotPersonal(owner, id string) (PersonalFile, error) {
	m.libraryMu.Lock()
	defer m.libraryMu.Unlock()
	f, err := m.ownedFile(owner, id)
	if err != nil {
		return f, err
	}
	if f.Storage != resource.Reference {
		if f.Storage != resource.Managed && f.Storage != resource.Snapshot {
			return f, failure(409, "personal_file_storage_unsupported", "不支持的文件存储方式")
		}
		src, err := m.openPersonalContent(f)
		if err != nil {
			return f, err
		}
		_ = src.Close()
		return f, nil
	}
	src, err := m.openPersonalContent(f)
	if err != nil {
		return f, err
	}
	defer src.Close()
	dir := filepath.Dir(m.libraryPath(f))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return f, err
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return f, err
	}
	defer os.Remove(tmp.Name())
	err = copyPersonalVerified(tmp, src, f)
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return f, err
	}
	if err := os.Rename(tmp.Name(), m.libraryPath(f)); err != nil {
		return f, err
	}
	f.Storage = resource.Snapshot
	f.SnapshotAt = store.Now()
	if err := m.Store.Put("personal-file", f.ID, f); err != nil {
		_ = os.Remove(m.libraryPath(f))
		return f, err
	}
	return f, nil
}
