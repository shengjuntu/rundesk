package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

// Metadata is reported by the image producer. We never run the image to infer it
// and never expose Config.Env or unrelated labels (which can contain secrets).
type ImageVersion struct {
	ID           string   `json:"id"`
	InstanceID   string   `json:"instanceId"`
	Reference    string   `json:"reference"`
	ImageID      string   `json:"imageId"`
	RepoDigests  []string `json:"repoDigests"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	Size         int64    `json:"size"`
	Created      string   `json:"created,omitempty"`
	Version      string   `json:"version,omitempty"`
	Source       string   `json:"source,omitempty"`
	Revision     string   `json:"revision,omitempty"`
	BuildCreated string   `json:"buildCreated,omitempty"`
	Dockerfile   string   `json:"dockerfile,omitempty"`
	BuildURL     string   `json:"buildUrl,omitempty"`
	CodexVersion string   `json:"codexVersion,omitempty"`
	Tools        string   `json:"tools,omitempty"`
	Skills       string   `json:"skills,omitempty"`
	MCP          string   `json:"mcp,omitempty"`
	RegisteredAt string   `json:"registeredAt"`
	CheckedAt    string   `json:"checkedAt"`
	Availability string   `json:"availability"`
	Error        string   `json:"error,omitempty"`
}
type imageInspection struct {
	ID           string
	RepoDigests  []string
	Os           string
	Architecture string
	Size         int64
	Created      string
	Config       struct {
		Labels  map[string]string
		Volumes map[string]any
	}
}

func (m *Manager) inspectImage(ref string) (imageInspection, error) {
	var item imageInspection
	out, e := m.dockerRun(10*time.Second, "image", "inspect", ref)
	if e != nil {
		return item, &apiError{Status: 503, Code: "docker_image_unavailable", Message: "无法检查本机镜像 " + ref + "：" + e.Error(), Cause: e}
	}
	var items []imageInspection
	if json.Unmarshal(out, &items) != nil || len(items) != 1 || items[0].ID == "" {
		return item, failure(502, "docker_invalid_image", "无法读取镜像身份")
	}
	item = items[0]
	if item.Os != "linux" || item.Architecture != runtime.GOARCH {
		return item, failure(400, "docker_image_platform", "镜像必须为 Linux 且与 RunDesk 主机架构一致")
	}
	if len(item.Config.Volumes) > 0 {
		return item, failure(400, "docker_image_volumes", "应用镜像不能声明 VOLUME，请由 RunDesk 统一管理宿主机挂载")
	}
	if strings.HasPrefix(ref, "sha256:") && item.ID != ref {
		return item, failure(502, "docker_image_identity", "Docker 返回的镜像身份与请求不一致")
	}
	return item, nil
}
func (m *Manager) imageVersion(iid, id string) (ImageVersion, error) {
	var v ImageVersion
	e := m.Store.Get("image-version", id, &v)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && (v.InstanceID != iid || id == "")) {
		return v, failure(404, "image_version_not_found", "此应用的镜像版本不存在")
	}
	return v, e
}
func (m *Manager) RegisterImage(iid, reference string) (ImageVersion, error) {
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	var v ImageVersion
	if _, e := m.Instance(iid); e != nil {
		return v, e
	}
	if iid == DefaultInstance {
		return v, failure(400, "assistant_requires_local", "通用助手使用本机 Codex，无需镜像")
	}
	reference = strings.TrimSpace(reference)
	if !imageReference.MatchString(reference) {
		return v, failure(400, "invalid_image_reference", "请输入有效的镜像名称、标签或 Digest")
	}
	item, e := m.inspectImage(reference)
	if e != nil {
		return v, e
	}
	sum := sha256.Sum256([]byte(iid + "/" + reference + "/" + item.ID))
	id := hex.EncodeToString(sum[:16])
	if old, e := m.imageVersion(iid, id); e == nil {
		old.CheckedAt = store.Now()
		old.Availability = "available"
		old.Error = ""
		return old, m.Store.Put("image-version", id, old)
	} else {
		var ae *apiError
		if !errors.As(e, &ae) || ae.Code != "image_version_not_found" {
			return v, e
		}
	}
	// Keep a bounded catalog; a request never executes a build or a pull.
	rows, e := m.Store.List("image-version")
	if e != nil {
		return v, e
	}
	count := 0
	for _, b := range rows {
		var x ImageVersion
		if e = json.Unmarshal(b, &x); e != nil {
			return v, e
		}
		if x.InstanceID == iid {
			count++
		}
	}
	if count >= 200 {
		return v, failure(409, "image_catalog_full", "每个应用最多登记 200 个镜像版本")
	}
	label := func(key string) string {
		value := strings.TrimSpace(item.Config.Labels[key])
		if len(value) > 4096 {
			return ""
		}
		return diagnosticText(value)
	}
	digests := item.RepoDigests
	if digests == nil {
		digests = []string{}
	}
	v = ImageVersion{ID: id, InstanceID: iid, Reference: reference, ImageID: item.ID, RepoDigests: digests, OS: item.Os, Architecture: item.Architecture, Size: item.Size, Created: item.Created, Version: label("org.opencontainers.image.version"), Source: label("org.opencontainers.image.source"), Revision: label("org.opencontainers.image.revision"), BuildCreated: label("org.opencontainers.image.created"), Dockerfile: label("io.rundesk.build.dockerfile"), BuildURL: label("io.rundesk.build.url"), CodexVersion: label("io.rundesk.codex.version"), Tools: label("io.rundesk.capabilities.tools"), Skills: label("io.rundesk.capabilities.skills"), MCP: label("io.rundesk.capabilities.mcp"), RegisteredAt: store.Now(), CheckedAt: store.Now(), Availability: "available"}
	return v, m.Store.Put("image-version", id, v)
}
func (m *Manager) CheckImage(iid, id string) (ImageVersion, error) {
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	v, e := m.imageVersion(iid, id)
	if e != nil {
		return v, e
	}
	_, check := m.inspectImage(v.ImageID)
	v.CheckedAt = store.Now()
	v.Availability = "available"
	v.Error = ""
	// A failed check is not proof the image was deleted: daemon may be offline.
	if check != nil {
		v.Availability = "unavailable"
		v.Error = diagnosticText(check.Error())
	}
	if e = m.Store.Put("image-version", id, v); e != nil {
		return v, e
	}
	return v, nil
}

type ImageCatalog struct {
	Versions         []ImageVersion `json:"versions"`
	Execution        ExecutionSpec  `json:"execution"`
	InstanceRevision int            `json:"instanceRevision"`
	Environments     []Environment  `json:"environments"`
	PendingUpdates   int            `json:"pendingUpdates"`
}

func (m *Manager) Images(iid string) (ImageCatalog, error) {
	m.executionMu.RLock()
	defer m.executionMu.RUnlock()
	c := ImageCatalog{Versions: []ImageVersion{}}
	i, e := m.Instance(iid)
	if e != nil {
		return c, e
	}
	c.Execution = i.Execution.normalized()
	c.InstanceRevision = i.Revision
	rows, e := m.Store.List("image-version")
	if e != nil {
		return c, e
	}
	for _, b := range rows {
		var v ImageVersion
		if e = json.Unmarshal(b, &v); e != nil {
			return c, e
		}
		if v.InstanceID == iid {
			c.Versions = append(c.Versions, v)
		}
	}
	sort.Slice(c.Versions, func(a, b int) bool {
		if c.Versions[a].RegisteredAt == c.Versions[b].RegisteredAt {
			return c.Versions[a].ID < c.Versions[b].ID
		}
		return c.Versions[a].RegisteredAt > c.Versions[b].RegisteredAt
	})
	c.Environments = m.Environments(iid)
	for _, v := range c.Environments {
		if environmentNeedsUpdate(v, c.Execution) {
			c.PendingUpdates++
		}
	}
	return c, nil
}
func environmentNeedsUpdate(v Environment, s ExecutionSpec) bool {
	if s.Mode != "docker" {
		return false
	}
	// Provenance alone does not require recreating a container using the same bytes.
	old := v.Spec
	old.ImageID = ""
	old.ImageVersionID = ""
	target := s
	target.ImageID = ""
	target.ImageVersionID = ""
	actual := v.ImageID
	if actual == "" {
		actual = v.Spec.ImageID
	}
	return old != target || (s.ImageID != "" && actual != s.ImageID)
}
func (m *Manager) SelectImage(iid, id string, revision int) (Instance, error) {
	v, e := m.imageVersion(iid, id)
	if e != nil {
		return Instance{}, e
	}
	i, e := m.Instance(iid)
	if e != nil {
		return i, e
	}
	if i.Execution.normalized().Mode != "docker" {
		return i, failure(409, "not_docker_application", "请先在运行环境中启用 Docker")
	}
	if i.Revision != revision {
		return i, failure(409, "revision_conflict", "配置已变化，请刷新后重试")
	}
	// Verify by immutable identity, even if the original tag now points elsewhere.
	v, e = m.CheckImage(iid, id)
	if e != nil {
		return i, e
	}
	if v.Availability != "available" {
		return i, failure(503, "docker_image_unavailable", v.Error)
	}
	spec := i.Execution.normalized()
	spec.Image = v.Reference
	spec.ImageID = v.ImageID
	spec.ImageVersionID = v.ID
	return m.SetExecution(iid, revision, spec)
}
func (s *Server) imageRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/instances/{iid}/images", func(w http.ResponseWriter, r *http.Request) { v, e := m.Images(r.PathValue("iid")); respond(w, v, e) })
	mux.HandleFunc("POST /api/instances/{iid}/images", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Reference string `json:"reference"`
		}
		if !decode(w, r, &p) {
			return
		}
		v, e := m.RegisterImage(r.PathValue("iid"), p.Reference)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/instances/{iid}/images/{vid}/check", func(w http.ResponseWriter, r *http.Request) {
		v, e := m.CheckImage(r.PathValue("iid"), r.PathValue("vid"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/instances/{iid}/images/{vid}/select", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Revision *int `json:"revision"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Revision == nil {
			respond(w, nil, failure(400, "revision_required", "需要应用当前 revision"))
			return
		}
		v, e := m.SelectImage(r.PathValue("iid"), r.PathValue("vid"), *p.Revision)
		respond(w, v, e)
	})
}
