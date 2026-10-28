package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Public image entries describe published content, not a registry search result.
// A source is always pinned by manifest digest; no credentials belong here.
type PublicImageSource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Reference string `json:"reference"`
}
type PublicImageEntry struct {
	ID             string              `json:"id"`
	ProfileID      string              `json:"profileId"`
	ProfileVersion string              `json:"profileVersion"`
	CodexVersion   string              `json:"codexVersion"`
	Platform       string              `json:"platform"`
	Sources        []PublicImageSource `json:"sources"`
}
type PublicImageCatalog struct {
	SchemaVersion int                `json:"schemaVersion"`
	Revision      string             `json:"revision,omitempty"`
	Items         []PublicImageEntry `json:"items"`
}
type PublicImageSnapshot struct {
	ID              string `json:"id"`
	SourceID        string `json:"sourceId"`
	Reference       string `json:"reference"`
	Platform        string `json:"platform"`
	ProfileVersion  string `json:"profileVersion"`
	CatalogRevision string `json:"catalogRevision"`
}

var publicImagesMu sync.Mutex
var publicImageReference = regexp.MustCompile(`^([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]{1,5})?)/[a-z0-9]+(?:[._-]+[a-z0-9]+)*(?:/[a-z0-9]+(?:[._-]+[a-z0-9]+)*)*@sha256:[a-f0-9]{64}$`)
var publicImagePlatform = regexp.MustCompile(`^linux/(?:amd64|arm64|arm|386|ppc64le|s390x|riscv64)$`)

// Docker Hub may omit its host and the official-image library namespace in
// RepoDigests. Normalize those spellings without conflating other registries.
func canonicalImageDigestReference(reference string) string {
	parts := strings.SplitN(reference, "/", 2)
	if len(parts) == 1 {
		return "docker.io/library/" + reference
	}
	host, repository := parts[0], parts[1]
	if host == "docker.io" || host == "index.docker.io" || host == "registry-1.docker.io" {
		if !strings.Contains(repository, "/") {
			repository = "library/" + repository
		}
		return "docker.io/" + repository
	}
	if !strings.ContainsAny(host, ".:") && host != "localhost" {
		return "docker.io/" + reference
	}
	return reference
}

func knownRuntimeProfile(id string) bool {
	switch id {
	case "news2douyin", "people-search", "video-build", "hyperframes", "video-use", "openmontage":
		return true
	}
	return false
}

func normalizePublicImages(in PublicImageCatalog) (PublicImageCatalog, error) {
	if in.SchemaVersion != 1 || len(in.Items) > 128 {
		return in, failure(400, "invalid_public_images", "公共镜像列表须为 schemaVersion 1，最多 128 个版本")
	}
	if in.Items == nil {
		in.Items = []PublicImageEntry{}
	}
	ids := map[string]bool{}
	for _, entry := range in.Items {
		if !sourceIdentifier.MatchString(entry.ID) || ids[entry.ID] || !knownRuntimeProfile(entry.ProfileID) || !profileRelease.MatchString(entry.ProfileVersion) || len(entry.ProfileVersion) > 64 || !profileRelease.MatchString(entry.CodexVersion) || len(entry.CodexVersion) > 64 || entry.CodexVersion == "0.0.0" || !publicImagePlatform.MatchString(entry.Platform) || len(entry.Sources) < 1 || len(entry.Sources) > 8 {
			return in, failure(400, "invalid_public_images", "公共镜像需要唯一标识、受支持的预设、精确版本、Linux 平台及 1–8 个下载源")
		}
		ids[entry.ID] = true
		sources, references, digest := map[string]bool{}, map[string]bool{}, ""
		for _, source := range entry.Sources {
			match := publicImageReference.FindStringSubmatch(source.Reference)
			if !sourceIdentifier.MatchString(source.ID) || sources[source.ID] || strings.TrimSpace(source.Name) == "" || len(source.Name) > 160 || strings.ContainsAny(source.Name, "\x00\r\n\t") || len(source.Reference) > 255 || len(match) != 2 || (!strings.ContainsAny(match[1], ".:") && match[1] != "localhost") || references[source.Reference] {
				return in, failure(400, "invalid_public_image_source", "镜像源须有唯一标识和名称，地址须为完整 registry/repository@sha256:Digest，不接受凭据或可变标签")
			}
			value := source.Reference[strings.LastIndex(source.Reference, "@")+1:]
			if digest != "" && digest != value {
				return in, failure(400, "public_image_mirror_mismatch", "同一镜像版本的所有下载源必须指向相同的 SHA-256 Digest")
			}
			digest, sources[source.ID], references[source.Reference] = value, true, true
		}
	}
	return in, nil
}

func publicImageDefaultFile() (string, error) {
	paths := []string{}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "config", "public-images.json"))
	}
	if dir, err := os.Getwd(); err == nil {
		for level := 0; level < 4; level++ {
			paths = append(paths, filepath.Join(dir, "config", "public-images.json"))
			dir = filepath.Dir(dir)
		}
	}
	for _, path := range paths {
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", failure(409, "public_image_catalog_missing", "找不到外置 config/public-images.json，请部署配置目录或指定 RUNDESK_PUBLIC_IMAGES")
}

func (m *Manager) publicImageCatalogFile() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("RUNDESK_PUBLIC_IMAGES")); configured != "" {
		return configured, nil
	}
	target := filepath.Join(m.Data, "config", "public-images.json")
	if _, err := os.Stat(target); err == nil {
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	source, err := publicImageDefaultFile()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	if err = writePublicImageCatalog(target, data); err != nil {
		return "", err
	}
	return target, nil
}

func writePublicImageCatalog(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".public-images-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (m *Manager) readPublicImages() (PublicImageCatalog, string, error) {
	var catalog PublicImageCatalog
	path, err := m.publicImageCatalogFile()
	if err != nil {
		return catalog, "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return catalog, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return catalog, "", failure(400, "invalid_public_images", "公共镜像列表不可读或超过 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&catalog) != nil {
		return catalog, "", failure(400, "invalid_public_images", "外置公共镜像 JSON 格式无效")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return catalog, "", failure(400, "invalid_public_images", "公共镜像列表须为单个 JSON 对象")
	}
	catalog, err = normalizePublicImages(catalog)
	if err != nil {
		return catalog, "", err
	}
	sum := sha256.Sum256(data)
	catalog.Revision = hex.EncodeToString(sum[:])
	return catalog, path, nil
}

func (m *Manager) PublicImages() (PublicImageCatalog, error) {
	publicImagesMu.Lock()
	defer publicImagesMu.Unlock()
	catalog, _, err := m.readPublicImages()
	return catalog, err
}

func (m *Manager) SavePublicImages(in PublicImageCatalog) (PublicImageCatalog, error) {
	publicImagesMu.Lock()
	defer publicImagesMu.Unlock()
	current, path, err := m.readPublicImages()
	if err != nil {
		return in, err
	}
	if in.Revision != current.Revision {
		return in, failure(409, "public_image_catalog_conflict", "公共镜像列表已变化，请刷新后重新保存")
	}
	if in.SchemaVersion == 0 {
		in.SchemaVersion = 1
	}
	in, err = normalizePublicImages(in)
	if err != nil {
		return in, err
	}
	in.Revision = ""
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return in, err
	}
	if err = writePublicImageCatalog(path, append(data, '\n')); err != nil {
		return in, err
	}
	in, _, err = m.readPublicImages()
	return in, err
}

func (m *Manager) resolvePublicImage(in *RuntimeImageInput) error {
	if in.PublicImageID == "" {
		if in.PublicSourceID != "" || in.CatalogRevision != "" {
			return failure(400, "invalid_public_image_selection", "公共镜像源和版本须与 publicImageId 一起提供")
		}
		return nil
	}
	if in.Source != "pull" || in.PublicSourceID == "" || in.CatalogRevision == "" {
		return failure(400, "invalid_public_image_selection", "公共镜像须选择 pull 来源、下载源和当前列表版本")
	}
	catalog, err := m.PublicImages()
	if err != nil {
		return err
	}
	if catalog.Revision != in.CatalogRevision {
		return failure(409, "public_image_catalog_conflict", "公共镜像列表已变化，请刷新后重新选择")
	}
	for _, entry := range catalog.Items {
		if entry.ID != in.PublicImageID {
			continue
		}
		if entry.Platform != "linux/"+runtime.GOARCH {
			return failure(409, "public_image_platform_mismatch", "所选公共镜像不支持当前 Docker 平台")
		}
		if in.ProfileID != "" && in.ProfileID != entry.ProfileID || in.CodexVersion != "" && in.CodexVersion != entry.CodexVersion {
			return failure(409, "public_image_selection_mismatch", "所选公共镜像与预设或 Codex 版本不一致")
		}
		for _, source := range entry.Sources {
			if source.ID != in.PublicSourceID {
				continue
			}
			if in.Reference != "" && in.Reference != source.Reference {
				return failure(409, "public_image_selection_mismatch", "镜像地址与所选公共镜像源不一致")
			}
			in.ProfileID, in.CodexVersion, in.Reference = entry.ProfileID, entry.CodexVersion, source.Reference
			in.publicImage = &PublicImageSnapshot{ID: entry.ID, SourceID: source.ID, Reference: source.Reference, Platform: entry.Platform, ProfileVersion: entry.ProfileVersion, CatalogRevision: catalog.Revision}
			return nil
		}
		return failure(404, "public_image_source_not_found", "所选公共镜像下载源不存在")
	}
	return failure(404, "public_image_not_found", "所选公共镜像版本不存在")
}

type runtimeImageRequest struct {
	InputSHA string `json:"inputSHA"`
	BuildID  string `json:"buildId"`
}

func runtimeImageRequestKey(requestID string) string {
	sum := sha256.Sum256([]byte(requestID))
	return hex.EncodeToString(sum[:])
}

// Called with buildMu held. Receipts survive completed/removed job records, so
// retrying a lost submission cannot silently download or build a second image.
func (m *Manager) replayRuntimeImageLocked(in *RuntimeImageInput) (ImageBuild, bool, error) {
	if in.RequestID == "" {
		return ImageBuild{}, false, nil
	}
	var receipt runtimeImageRequest
	err := m.Store.Get("runtime-image-request", runtimeImageRequestKey(in.RequestID), &receipt)
	if err == nil {
		if receipt.InputSHA != in.requestSHA {
			return ImageBuild{}, false, failure(409, "runtime_image_request_conflict", "此准备请求编号已用于不同的配置")
		}
		job, err := m.buildLocked(DefaultInstance, receipt.BuildID)
		if err != nil {
			return ImageBuild{}, false, failure(409, "runtime_image_request_gone", "此准备请求已经受理，记录已清理；如需再次操作请使用新请求编号")
		}
		return job, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ImageBuild{}, false, err
	}
	// Recover the narrow crash window between saving a queued job and its receipt.
	rows, err := m.buildsLocked(DefaultInstance)
	if err != nil {
		return ImageBuild{}, false, err
	}
	for _, row := range rows {
		if row.Scope != "setup" || row.RecipeID != "" || row.RebuildOf != "" || row.RequestID != in.RequestID {
			continue
		}
		if row.RequestSHA != in.requestSHA {
			return ImageBuild{}, false, failure(409, "runtime_image_request_conflict", "此准备请求编号已用于不同的配置")
		}
		return row, true, m.Store.Put("runtime-image-request", runtimeImageRequestKey(in.RequestID), runtimeImageRequest{InputSHA: in.requestSHA, BuildID: row.ID})
	}
	return ImageBuild{}, false, nil
}

func (s *Server) publicImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/public-images", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Manager.PublicImages()
		respond(w, v, err)
	})
	mux.HandleFunc("PUT /api/setup/public-images", func(w http.ResponseWriter, r *http.Request) {
		var in PublicImageCatalog
		if !decode(w, r, &in) {
			return
		}
		v, err := s.Manager.SavePublicImages(in)
		respond(w, v, err)
	})
}
