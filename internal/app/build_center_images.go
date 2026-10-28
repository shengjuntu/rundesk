package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

// This catalog is deliberately an index of RunDesk's records. Listing it does
// not enumerate the host's image library or start containers.
type BuildCenterImageSource struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status,omitempty"`
	ProfileID      string `json:"profileId,omitempty"`
	RecipeID       string `json:"recipeId,omitempty"`
	RecipeRevision int    `json:"recipeRevision,omitempty"`
}
type BuildCenterImageRegistration struct {
	InstanceID string `json:"instanceId"`
	VersionID  string `json:"versionId"`
}
type BuildCenterImageSelection struct {
	InstanceID string `json:"instanceId"`
	Name       string `json:"name"`
}
type BuildCenterImage struct {
	Key           string                         `json:"key"`
	ImageID       string                         `json:"imageId"`
	References    []string                       `json:"references"`
	Sources       []BuildCenterImageSource       `json:"sources"`
	Registrations []BuildCenterImageRegistration `json:"registrations"`
	SelectedBy    []BuildCenterImageSelection    `json:"selectedBy"`
	Environments  []Environment                  `json:"environments"`
	Availability  string                         `json:"availability"`
	CheckedAt     string                         `json:"checkedAt,omitempty"`
	Error         string                         `json:"error,omitempty"`
	LastTest      *ImageBuild                    `json:"lastTest,omitempty"`
}
type BuildCenterImageDeleteInput struct {
	ImageID   string `json:"imageId"`
	RequestID string `json:"requestId"`
}
type BuildCenterImageDeleteResult struct {
	ImageID           string `json:"imageId"`
	Deleted           bool   `json:"deleted"`
	RegistrationCount int    `json:"registrationCount"`
}
type BuildCenterImageLaunchInput struct {
	Name        string `json:"name"`
	InstanceID  string `json:"instanceId,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	RequestID   string `json:"requestId"`
}
type BuildCenterImageLaunchResult struct {
	Instance    Instance    `json:"instance"`
	Workspace   Workspace   `json:"workspace"`
	Environment Environment `json:"environment"`
}
type BuildCenterImageTestInput struct {
	Kind          string `json:"kind"`
	RequestID     string `json:"requestId"`
	OwnerRunID    string `json:"-"`
	SourceBuildID string `json:"-"`
}

type buildCenterImageCheck struct {
	ImageID, Availability, CheckedAt, Error string
}
type buildCenterArchivedImageSource struct {
	ImageID   string                 `json:"imageId"`
	Reference string                 `json:"reference,omitempty"`
	Source    BuildCenterImageSource `json:"source"`
}
type buildCenterImageOperation struct {
	Fingerprint string          `json:"fingerprint"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorStatus int             `json:"errorStatus,omitempty"`
	ErrorCode   string          `json:"errorCode,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// Mutation admission is serialized in addition to the existing execution and
// build locks. Durable receipts make retries reads, including after a restart.
var buildCenterImageMu sync.Mutex
var buildCenterImageKeyPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func BuildCenterImageKey(imageID string) string {
	sum := sha256.Sum256([]byte(imageID))
	return hex.EncodeToString(sum[:12])
}

func validBuildCenterImageID(id string) bool {
	if !strings.HasPrefix(id, "sha256:") || len(id) <= len("sha256:") || len(id) > 71 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "sha256:"))
	return err == nil
}

// Called before deleting build history. The immutable artifact remains managed
// without retaining logs, context paths, smoke commands, or free-form names.
// No Docker command or Manager lock is needed; DeleteBuild holds buildMu.
func (m *Manager) preserveBuildCenterImageSource(v ImageBuild) error {
	if v.Scope == "image-test" || !validBuildCenterImageID(v.ImageID) {
		return nil
	}
	archived := buildCenterArchivedImageSource{ImageID: v.ImageID, Source: BuildCenterImageSource{Kind: "archived-build", ID: v.ID, Name: "已归档构建", Status: v.Status, ProfileID: v.ProfileID, RecipeID: v.RecipeID, RecipeRevision: v.RecipeRevision}}
	if imageReference.MatchString(v.Reference) {
		archived.Reference = diagnosticText(v.Reference)
	}
	return m.Store.Put("build-center-image-source", v.ID, archived)
}

func (m *Manager) buildCenterImageRows() ([]BuildCenterImage, error) {
	versions, err := m.Store.List("image-version")
	if err != nil {
		return nil, err
	}
	builds, err := m.buildsLocked("") // Store snapshot only; caller never needs buildMu for reads.
	if err != nil {
		return nil, err
	}
	byID := map[string]*BuildCenterImage{}
	row := func(id string) *BuildCenterImage {
		v := byID[id]
		if v == nil {
			v = &BuildCenterImage{Key: BuildCenterImageKey(id), ImageID: id, References: []string{}, Sources: []BuildCenterImageSource{}, Registrations: []BuildCenterImageRegistration{}, SelectedBy: []BuildCenterImageSelection{}, Environments: []Environment{}, Availability: "unverified"}
			byID[id] = v
		}
		return v
	}
	ref := func(v *BuildCenterImage, s string) {
		if s == "" {
			return
		}
		for _, previous := range v.References {
			if previous == s {
				return
			}
		}
		v.References = append(v.References, s)
	}
	for _, raw := range versions {
		var registered ImageVersion
		if err = json.Unmarshal(raw, &registered); err != nil {
			return nil, err
		}
		if registered.ImageID == "" {
			continue
		}
		v := row(registered.ImageID)
		ref(v, registered.Reference)
		v.Registrations = append(v.Registrations, BuildCenterImageRegistration{InstanceID: registered.InstanceID, VersionID: registered.ID})
		v.Sources = append(v.Sources, BuildCenterImageSource{Kind: "registration", ID: registered.ID, Name: registered.Reference})
		if registered.CheckedAt > v.CheckedAt {
			v.CheckedAt, v.Availability, v.Error = registered.CheckedAt, registered.Availability, registered.Error
		}
	}
	for _, b := range builds {
		if b.Scope == "image-test" || !validBuildCenterImageID(b.ImageID) {
			continue
		}
		v := row(b.ImageID)
		ref(v, b.Reference)
		v.Sources = append(v.Sources, BuildCenterImageSource{Kind: "build", ID: b.ID, Name: b.Name, Status: b.Status, ProfileID: b.ProfileID, RecipeID: b.RecipeID, RecipeRevision: b.RecipeRevision})
		if v.Availability == "unverified" && v.Error == "" && b.Status == "failed" {
			v.Error = diagnosticText(b.Error)
		}
	}
	archivedSources, err := m.Store.List("build-center-image-source")
	if err != nil {
		return nil, err
	}
	liveBuilds := map[string]bool{}
	for _, b := range builds {
		liveBuilds[b.ID] = true
	}
	for _, raw := range archivedSources {
		var archived buildCenterArchivedImageSource
		if err = json.Unmarshal(raw, &archived); err != nil {
			return nil, err
		}
		if liveBuilds[archived.Source.ID] || !validBuildCenterImageID(archived.ImageID) {
			continue
		}
		v := row(archived.ImageID)
		ref(v, archived.Reference)
		v.Sources = append(v.Sources, archived.Source)
	}
	for _, i := range m.Instances() {
		if v := byID[i.Execution.ImageID]; v != nil && i.Execution.normalized().Mode == "docker" {
			v.SelectedBy = append(v.SelectedBy, BuildCenterImageSelection{InstanceID: i.ID, Name: i.Name})
		}
	}
	for _, environment := range m.Environments("") {
		id := environment.ImageID
		if id == "" {
			id = environment.Spec.ImageID
		}
		if v := byID[id]; v != nil {
			// The physical HOME is not needed by the catalog consumer.
			environment.CodexHome = ""
			v.Environments = append(v.Environments, environment)
		}
	}
	for _, b := range builds { // builds are newest first
		if b.Scope == "image-test" {
			if v := byID[b.ImageID]; v != nil && v.LastTest == nil {
				copy := b
				v.LastTest = &copy
			}
		}
	}
	out := make([]BuildCenterImage, 0, len(byID))
	for _, v := range byID {
		var checked buildCenterImageCheck
		if err = m.Store.Get("build-center-image-check", v.Key, &checked); err == nil {
			if checked.ImageID == v.ImageID && checked.CheckedAt >= v.CheckedAt {
				v.Availability, v.CheckedAt, v.Error = checked.Availability, checked.CheckedAt, checked.Error
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		sort.Strings(v.References)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *Manager) BuildCenterImages() (map[string]any, error) {
	m.executionMu.RLock()
	defer m.executionMu.RUnlock()
	items, err := m.buildCenterImageRows()
	return map[string]any{"items": items}, err
}

func (m *Manager) buildCenterImage(key string) (BuildCenterImage, error) {
	if !buildCenterImageKeyPattern.MatchString(key) {
		return BuildCenterImage{}, failure(400, "invalid_image_key", "镜像编号无效")
	}
	rows, err := m.buildCenterImageRows()
	if err != nil {
		return BuildCenterImage{}, err
	}
	for _, v := range rows {
		if v.Key == key {
			return v, nil
		}
	}
	return BuildCenterImage{}, failure(404, "build_center_image_not_found", "没有 RunDesk 登记或构建此镜像的记录")
}

func (m *Manager) BuildCenterImage(key string) (BuildCenterImage, error) {
	m.executionMu.RLock()
	defer m.executionMu.RUnlock()
	return m.buildCenterImage(key)
}

func (m *Manager) CheckBuildCenterImage(key string) (BuildCenterImage, error) {
	buildCenterImageMu.Lock()
	defer buildCenterImageMu.Unlock()
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	v, err := m.buildCenterImage(key)
	if err != nil {
		return v, err
	}
	checked := buildCenterImageCheck{ImageID: v.ImageID, Availability: "available", CheckedAt: store.Now()}
	if _, err = m.inspectImage(v.ImageID); err != nil {
		checked.Availability, checked.Error = "unavailable", diagnosticText(err.Error())
	}
	if err = m.Store.Put("build-center-image-check", key, checked); err != nil {
		return v, err
	}
	v.Availability, v.CheckedAt, v.Error = checked.Availability, checked.CheckedAt, checked.Error
	return v, nil
}

func (m *Manager) beginImageOperation(requestID, action, key string, input any, result any) (bool, error) {
	if !buildCenterImageKeyPattern.MatchString(requestID) {
		return false, failure(400, "invalid_request_id", "requestId 必须为 24 位小写十六进制编号")
	}
	// Internal helper scope is part of admission identity even though clients
	// cannot supply these ownership fields in JSON.
	var internalScope any
	if test, ok := input.(BuildCenterImageTestInput); ok {
		internalScope = []string{test.OwnerRunID, test.SourceBuildID}
	}
	b, err := json.Marshal([]any{action, key, input, internalScope})
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(b)
	fingerprint := hex.EncodeToString(sum[:])
	var previous buildCenterImageOperation
	err = m.Store.Get("build-center-image-operation", requestID, &previous)
	if err == nil {
		if previous.Fingerprint != fingerprint {
			return false, failure(409, "request_id_conflict", "此 requestId 已用于其他镜像操作或参数")
		}
		if len(previous.Result) > 0 {
			if err = json.Unmarshal(previous.Result, result); err != nil {
				return true, err
			}
		}
		if previous.Status == "succeeded" {
			return true, nil
		}
		if previous.Status == "failed" {
			return true, failure(previous.ErrorStatus, previous.ErrorCode, previous.Error)
		}
		message := "此操作已开始但结果未确认；不会重复创建或删除，请检查现有资源"
		if launch, ok := result.(*BuildCenterImageLaunchResult); ok {
			message += buildCenterLaunchResourceNote(*launch)
		}
		return true, failure(409, "image_operation_unconfirmed", message)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return false, m.Store.Put("build-center-image-operation", requestID, buildCenterImageOperation{Fingerprint: fingerprint, Status: "pending"})
}

func (m *Manager) finishImageOperation(requestID string, result any, operationErr error) error {
	var receipt buildCenterImageOperation
	if err := m.Store.Get("build-center-image-operation", requestID, &receipt); err != nil {
		return err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	receipt.Result = b // Keep partial resource identities on failure too.
	if operationErr == nil {
		receipt.Status = "succeeded"
	} else {
		receipt.Status, receipt.ErrorStatus, receipt.ErrorCode, receipt.Error = "failed", 500, "image_operation_failed", diagnosticText(operationErr.Error())
		var typed *apiError
		if errors.As(operationErr, &typed) {
			receipt.ErrorStatus, receipt.ErrorCode = typed.Status, typed.Code
		}
	}
	if err := m.Store.Put("build-center-image-operation", requestID, receipt); err != nil {
		return err
	}
	return operationErr
}

func buildCenterLaunchResourceNote(result BuildCenterImageLaunchResult) string {
	ids := []string{}
	if result.Instance.ID != "" {
		ids = append(ids, "instanceId="+result.Instance.ID)
	}
	if result.Workspace.ID != "" {
		ids = append(ids, "workspaceId="+result.Workspace.ID)
	}
	if result.Environment.ID != "" {
		ids = append(ids, "environmentId="+result.Environment.ID)
	}
	if len(ids) == 0 {
		return ""
	}
	return "；已保留资源：" + strings.Join(ids, ", ") + "。请检查这些资源，避免用新 requestId 重复创建。"
}

func buildCenterAgentName(v BuildCenterImage, instances []Instance) string {
	base := "Docker Agent"
	for _, ref := range v.References {
		if strings.HasPrefix(ref, "sha256:") {
			continue
		}
		base = filepath.Base(ref)
		if pos := strings.LastIndex(base, ":"); pos >= 0 {
			base = base[:pos]
		}
		break
	}
	for _, source := range v.Sources {
		if source.ProfileID != "" {
			base = source.ProfileID
			break
		}
		if source.RecipeID != "" {
			base = strings.TrimPrefix(source.RecipeID, "builtin-")
			break
		}
		if base == "Docker Agent" && strings.TrimSpace(source.Name) != "" && !strings.HasPrefix(source.Name, "sha256:") {
			base = strings.TrimSpace(source.Name)
		}
	}
	if len([]rune(base)) > 48 {
		base = string([]rune(base)[:48])
	}
	used := map[string]bool{}
	for _, instance := range instances {
		used[strings.ToLower(instance.Name)] = true
	}
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s %d", base, n)
		}
		if !used[strings.ToLower(name)] {
			return name
		}
	}
}

func (m *Manager) checkpointImageLaunch(requestID string, result BuildCenterImageLaunchResult) error {
	var receipt buildCenterImageOperation
	if err := m.Store.Get("build-center-image-operation", requestID, &receipt); err != nil {
		return err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	receipt.Result = b
	return m.Store.Put("build-center-image-operation", requestID, receipt)
}

// Selected tags from older RunDesk versions have no pinned ID. Resolve them at
// deletion admission; an unavailable/ambiguous reference cannot mean "unused".
func (m *Manager) executionReferencesImage(spec ExecutionSpec, actual, target string) (bool, error) {
	if actual == target || spec.ImageID == target {
		return true, nil
	}
	if spec.normalized().Mode != "docker" || actual != "" || spec.ImageID != "" {
		return false, nil
	}
	image, err := m.inspectImage(spec.Image)
	if err != nil {
		return false, failure(409, "image_usage_unknown", "无法确认未固定镜像的实例或环境引用；请先检查其运行环境")
	}
	return image.ID == target, nil
}

func (m *Manager) DeleteBuildCenterImage(key string, input BuildCenterImageDeleteInput) (result BuildCenterImageDeleteResult, err error) {
	buildCenterImageMu.Lock()
	defer buildCenterImageMu.Unlock()
	if done, e := m.beginImageOperation(input.RequestID, "delete", key, input, &result); done || e != nil {
		return result, e
	}
	defer func() { err = m.finishImageOperation(input.RequestID, result, err) }()
	// Match build completion's buildMu -> executionMu order.
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	m.executionMu.Lock()
	defer m.executionMu.Unlock()
	v, err := m.buildCenterImage(key)
	if err != nil {
		return result, err
	}
	if input.ImageID != v.ImageID {
		return result, failure(409, "image_identity_conflict", "镜像身份已变化，请刷新后重试")
	}
	if _, err = m.inspectImage(v.ImageID); err != nil {
		return result, failure(409, "image_delete_unconfirmed", "无法确认待删镜像仍存在；此操作未删除登记或其他资源")
	}
	for _, i := range m.Instances() {
		used, e := m.executionReferencesImage(i.Execution, "", v.ImageID)
		if e != nil {
			return result, e
		}
		if used {
			return result, failure(409, "image_in_use", "镜像仍被实例选用，请先明确切换该实例的镜像")
		}
	}
	for _, environment := range m.Environments("") {
		used, e := m.executionReferencesImage(environment.Spec, environment.ImageID, v.ImageID)
		if e != nil {
			return result, e
		}
		if used {
			return result, failure(409, "image_in_use", "镜像仍被已登记环境引用，请先显式移除或更新该环境")
		}
	}
	builds, err := m.buildsLocked("")
	if err != nil {
		return result, err
	}
	for _, b := range builds {
		if !buildActive(b.Status) {
			continue
		}
		referenced := b.ImageID == v.ImageID || b.Reference == v.ImageID
		for _, reference := range v.References {
			referenced = referenced || b.Reference == reference
		}
		if referenced {
			return result, failure(409, "image_in_use", "镜像或其登记标签仍有排队或运行中的构建、检查任务")
		}
	}
	// Docker knows about stopped and foreign containers that RunDesk did not
	// create. Never rely solely on our environment registry for a destructive call.
	inspectionContext, cancelInspection := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancelInspection()
	containers, err := m.docker.Run(inspectionContext, "container", "ls", "-a", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return result, failure(409, "image_usage_unknown", "无法查询 Docker 容器引用，未执行删除")
	}
	ids := strings.Fields(string(containers))
	if len(containers) >= 256<<10 || len(ids) > 5000 {
		return result, failure(409, "image_usage_unknown", "容器引用过多，无法完整确认，未执行删除")
	}
	for _, id := range ids {
		raw, e := m.docker.Run(inspectionContext, "container", "inspect", id)
		var inspected []containerInspection
		if e != nil || json.Unmarshal(raw, &inspected) != nil || len(inspected) != 1 || inspected[0].ID != id || inspected[0].Image == "" {
			return result, failure(409, "image_usage_unknown", "有容器状态无法确认，未执行删除")
		}
		if inspected[0].Image == v.ImageID {
			return result, failure(409, "image_in_use", "Docker 中仍有容器引用此镜像（包含停止或非 RunDesk 容器）")
		}
	}
	// Delete by immutable identity only. Docker refuses shared tags/in-use images
	// without force. A moved historical tag can never delete its new target.
	if _, err = m.dockerRun(30*time.Second, "image", "rm", "--no-prune", v.ImageID); err != nil {
		return result, &apiError{Status: 409, Code: "image_delete_refused", Message: "Docker 未接受非强制删除；可能仍有多个标签或其他引用。登记、项目与 HOME 均保留。", Cause: err}
	}
	checked := buildCenterImageCheck{ImageID: v.ImageID, Availability: "deleted", CheckedAt: store.Now()}
	records := []store.Record{{Kind: "build-center-image-check", ID: key, Value: checked}}
	for _, registration := range v.Registrations {
		var registered ImageVersion
		if err = m.Store.Get("image-version", registration.VersionID, &registered); err != nil {
			return result, err
		}
		registered.Availability, registered.CheckedAt, registered.Error = "deleted", checked.CheckedAt, ""
		records = append(records, store.Record{Kind: "image-version", ID: registered.ID, Value: registered})
	}
	result = BuildCenterImageDeleteResult{ImageID: v.ImageID, Deleted: true, RegistrationCount: len(v.Registrations)}
	err = m.Store.PutMany(records...)
	return result, err
}

// Caller holds applicationMu. App and preset reservations are never borrowed by
// the global build center, even if that app currently has no active task.
func (m *Manager) checkBuildCenterLaunchReservations(iid, wid string) error {
	if wid != "" {
		if _, reserved, e := m.projectEnvironmentForWorkspace(wid); e != nil {
			return e
		} else if reserved {
			return failure(409, "application_binding_conflict", "项目工作区已由应用管理，不能从镜像中心重新占用")
		}
	}
	apps, err := m.applicationRecords()
	if err != nil {
		return err
	}
	for _, app := range apps {
		if iid != "" && app.InstanceID == iid || wid != "" && app.WorkspaceID == wid {
			return failure(409, "application_binding_conflict", "此实例或工作区已保留给应用，请在应用中启动")
		}
	}
	presets, err := m.allApplicationPresets()
	if err != nil {
		return err
	}
	for _, preset := range presets {
		if iid != "" && preset.InstanceID == iid || wid != "" && preset.WorkspaceID == wid {
			return failure(409, "application_binding_conflict", "此实例或工作区已保留给应用运行环境")
		}
	}
	for _, s := range m.Sessions() {
		if s.InstanceID != iid && s.WorkspaceID != wid {
			continue
		}
		if s.Source.Kind == "application" {
			return failure(409, "application_binding_conflict", "此实例或工作区已用于应用任务")
		}
		if active(s.Status) {
			return failure(409, "environment_busy", "此实例或工作区仍有活动任务")
		}
	}
	for _, environment := range m.Environments("") {
		if (environment.InstanceID == iid || environment.WorkspaceID == wid) && environment.ActiveConnections > 0 {
			return failure(409, "environment_busy", "此实例或工作区仍有活动连接")
		}
	}
	return nil
}

func (m *Manager) LaunchBuildCenterImage(key string, input BuildCenterImageLaunchInput) (result BuildCenterImageLaunchResult, err error) {
	buildCenterImageMu.Lock()
	defer buildCenterImageMu.Unlock()
	if done, e := m.beginImageOperation(input.RequestID, "launch", key, input, &result); done || e != nil {
		return result, e
	}
	defer func() {
		if err != nil && result.Instance.ID != "" {
			note := buildCenterLaunchResourceNote(result)
			var typed *apiError
			if errors.As(err, &typed) {
				err = &apiError{Status: typed.Status, Code: typed.Code, Message: diagnosticText(typed.Message + note), Cause: err}
			} else {
				err = &apiError{Status: 500, Code: "image_launch_failed", Message: diagnosticText(err.Error() + note), Cause: err}
			}
		}
		err = m.finishImageOperation(input.RequestID, result, err)
	}()
	name := strings.TrimSpace(input.Name)
	if len(name) > 160 || input.InstanceID == DefaultInstance {
		return result, failure(400, "invalid_image_launch", "Agent 名称过长，或试图复用默认助手")
	}
	v, err := m.BuildCenterImage(key)
	if err != nil {
		return result, err
	}
	if _, err = m.inspectImage(v.ImageID); err != nil {
		return result, err
	}
	validatedSource := false
	for _, source := range v.Sources {
		validatedSource = validatedSource || source.Kind == "registration" || source.Status == "succeeded"
	}
	if !validatedSource {
		return result, failure(409, "image_build_unverified", "此镜像只来自未成功验证的构建，可检查或清理；请先完成成功构建或明确登记后再启动")
	}
	if name == "" {
		name = buildCenterAgentName(v, m.Instances())
	}
	// Validate user-selected resources before creating anything.
	m.applicationMu.Lock()
	err = m.checkBuildCenterLaunchReservations(input.InstanceID, input.WorkspaceID)
	m.applicationMu.Unlock()
	if err != nil {
		return result, err
	}
	if input.InstanceID != "" {
		result.Instance, err = m.Instance(input.InstanceID)
		if err != nil {
			return result, err
		}
		if runtimeKind(result.Instance.AgentRuntime.Kind) != "codex" || result.Instance.Execution.normalized().Mode != "docker" || result.Instance.Execution.ImageID != v.ImageID {
			return result, failure(409, "image_launch_mismatch", "复用实例必须已固定为同一 Codex Docker 镜像；不会覆盖已有配置")
		}
	}
	if input.WorkspaceID != "" {
		result.Workspace, err = m.Workspace(input.WorkspaceID)
		if err != nil {
			return result, err
		}
		if err = m.validateDockerWorkspace(result.Workspace); err != nil {
			return result, err
		}
	}
	if result.Instance.ID == "" {
		result.Instance, err = m.CreateInstance(InstancePatch{Name: name})
		if err != nil {
			return result, err
		}
		if err = m.checkpointImageLaunch(input.RequestID, result); err != nil {
			return result, err
		}
		registered, e := m.RegisterImage(result.Instance.ID, v.ImageID)
		if e != nil {
			return result, e
		}
		result.Instance, err = m.SetExecution(result.Instance.ID, result.Instance.Revision, ExecutionSpec{Mode: "docker", Image: registered.Reference, ImageID: registered.ImageID, ImageVersionID: registered.ID})
		if err != nil {
			return result, err
		}
	}
	if result.Workspace.ID == "" {
		// CreateWorkspace's empty-path mode is confined to data/workspaces/<id>.
		result.Workspace, err = m.CreateWorkspace(name, "")
		if err != nil {
			return result, err
		}
		if err = m.checkpointImageLaunch(input.RequestID, result); err != nil {
			return result, err
		}
	}
	// Keep the application reservation lock through environment preparation so a
	// concurrent app registration cannot lend us an already-reserved identity.
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	if err = m.checkBuildCenterLaunchReservations(result.Instance.ID, result.Workspace.ID); err != nil {
		return result, err
	}
	result.Environment, err = m.EnsureApplicationEnvironment(result.Instance.ID, result.Workspace.ID)
	if err != nil {
		return result, err
	}
	if err = m.checkpointImageLaunch(input.RequestID, result); err != nil {
		return result, err
	}
	if result.Environment.ImageID != "" && result.Environment.ImageID != v.ImageID || result.Environment.Spec.ImageID != v.ImageID {
		return result, failure(409, "image_launch_mismatch", "已有环境使用其他镜像；不会重建或覆盖其 HOME")
	}
	result.Environment, err = m.EnvironmentAction(result.Environment.ID, "start", result.Environment.Revision)
	return result, err
}

func (m *Manager) StartBuildCenterImageTest(key string, input BuildCenterImageTestInput) (result ImageBuild, err error) {
	buildCenterImageMu.Lock()
	defer buildCenterImageMu.Unlock()
	if done, e := m.beginImageOperation(input.RequestID, "test", key, input, &result); done || e != nil {
		return result, e
	}
	defer func() { err = m.finishImageOperation(input.RequestID, result, err) }()
	if input.Kind != "offline" && input.Kind != "smoke" {
		return result, failure(400, "invalid_image_test", "测试类型必须为 offline 或 smoke")
	}
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	m.executionMu.RLock()
	defer m.executionMu.RUnlock()
	v, err := m.buildCenterImage(key)
	if err != nil {
		return result, err
	}
	if _, err = m.inspectImage(v.ImageID); err != nil {
		return result, err
	}
	builds, err := m.buildsLocked("")
	if err != nil {
		return result, err
	}
	var source ImageBuild
	for _, b := range builds {
		if b.ImageID != v.ImageID || b.Scope == "image-test" || buildActive(b.Status) {
			continue
		}
		if (input.OwnerRunID != "" || input.SourceBuildID != "") && b.Status != "succeeded" {
			continue
		}
		if input.OwnerRunID != "" && b.OwnerRunID != input.OwnerRunID || input.SourceBuildID != "" && b.ID != input.SourceBuildID {
			continue
		}
		if input.Kind == "offline" || input.Kind == "smoke" && b.RecipeSmoke != nil {
			source = b
			break
		}
	}
	if source.ID == "" && (input.Kind == "smoke" || input.OwnerRunID != "" || input.SourceBuildID != "") {
		return result, failure(409, "image_test_unavailable", "此镜像没有符合权限的成功构建或已冻结 smoke 配方；镜像可用性检查不代表制作测试通过")
	}
	activeCount := 0
	for _, b := range builds {
		if buildActive(b.Status) {
			activeCount++
		}
	}
	if activeCount >= 32 {
		return result, failure(429, "build_queue_full", "构建与检查队列已满")
	}
	id, now := store.ID(), store.Now()
	result = ImageBuild{ID: id, InstanceID: DefaultInstance, Scope: "image-test", Source: "local", TestKind: input.Kind, ImageID: v.ImageID, Name: input.Kind + " · " + v.ImageID, Reference: v.ImageID, Status: "queued", Created: now, Updated: now, TimeoutMinutes: 10, CodexVersion: source.CodexVersion, RecipeID: source.RecipeID, RecipeRevision: source.RecipeRevision, RecipeSHA: source.RecipeSHA, RecipeSmoke: source.RecipeSmoke, OwnerRunID: input.OwnerRunID}
	result.SourceBuildID, result.ContextSHA256 = source.ID, source.ContextSHA256
	result.PlatformOS, result.PlatformArch = source.PlatformOS, source.PlatformArch
	result.RuntimeIdentity = source.RuntimeIdentity
	result.CodexRuntime = source.CodexRuntime
	if source.Scope == "setup" {
		result.ProfileID = source.ProfileID
	}
	if err = os.MkdirAll(m.buildDir(id), 0700); err != nil {
		return result, err
	}
	err = m.Store.Put("image-build", id, result)
	return result, err
}

func (m *Manager) BuildCenterImageTest(id string) (ImageBuild, error) {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	v, err := m.buildLocked(DefaultInstance, id)
	if err == nil && v.Scope != "image-test" {
		err = failure(404, "image_test_not_found", "镜像检查任务不存在")
	}
	return v, err
}
func (m *Manager) BuildCenterImageTestLog(id string, after int64) (any, error) {
	if _, err := m.BuildCenterImageTest(id); err != nil {
		return nil, err
	}
	return m.BuildLog(DefaultInstance, id, after)
}

func (m *Manager) executeBuildCenterTest(ctx context.Context, v ImageBuild) (string, bool, error) {
	f, err := os.OpenFile(filepath.Join(m.buildDir(v.ID), "build.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return v.ImageID, false, err
	}
	defer f.Close()
	bounded := &buildLogWriter{f: f}
	out := newBuildLogRedactor(bounded)
	fmt.Fprintln(out, "离线镜像检查；不使用模型、服务凭据或宿主目录。通过仅表示本次声明的检查成功。")
	switch v.TestKind {
	case "offline":
		if v.ProfileID != "" {
			err = m.verifyRuntimeImage(ctx, v, v.ImageID, out)
		} else {
			err = m.runBuildCenterSmoke(ctx, v.ImageID, RecipeSmokeSpec{}, out)
		}
	case "smoke":
		if v.RecipeSmoke == nil {
			err = fmt.Errorf("检查任务缺少冻结的 smoke 配方")
		} else {
			err = m.runBuildCenterSmoke(ctx, v.ImageID, *v.RecipeSmoke, out)
		}
	default:
		err = fmt.Errorf("未知镜像检查类型")
	}
	if flushErr := out.Flush(); err == nil {
		err = flushErr
	}
	return v.ImageID, bounded.truncated || out.Truncated(), err
}

// A smoke spec is frozen with its build. Commands run as argv, never as shell
// interpolation; expected files are checked in the same temporary container.
// The only writable filesystem is an anonymous tmpfs discarded on completion.
func (m *Manager) runBuildCenterSmoke(parent context.Context, imageID string, spec RecipeSmokeSpec, out io.Writer) error {
	if _, err := m.inspectImage(imageID); err != nil {
		return err
	}
	seconds := spec.TimeoutSeconds
	if seconds <= 0 {
		seconds = 180
	}
	if seconds > 600 || len(spec.Command) > 64 || len(spec.ExpectedFiles) > 64 {
		return fmt.Errorf("无效 smoke 配方")
	}
	for _, arg := range spec.Command {
		if len(arg) > 8192 || strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("无效 smoke 命令参数")
		}
	}
	for _, path := range spec.ExpectedFiles {
		if path == "" || len(path) > 512 || strings.ContainsAny(path, "\x00\r\n") || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("smoke 产物检查必须使用容器内的规范绝对路径")
		}
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(seconds)*time.Second)
	defer cancel()
	name := "rundesk-image-smoke-" + store.ID()
	defer func() {
		// Shutdown cancels the build context; cleanup still gets its own bounded
		// opportunity to remove only our uniquely named temporary probe.
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = m.docker.Run(cleanup, "rm", "--force", name)
	}()
	fmt.Fprintln(out, "在隔离容器中检查 Codex App Server initialize 与配方命令；不发起模型轮次。")
	err = m.runtimeDockerCommand(ctx, out, "run", "--name", name, "--rm", "--pull=never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", "1000:1000", "--cpus", "2", "--memory", "2048m", "--memory-swap", "2048m", "--pids-limit", "256", "--tmpfs", "/tmp:rw,nosuid,nodev,size=768m,mode=1777", "--workdir", "/tmp", "--env", "HOME=/tmp", "--env", "CODEX_HOME=/tmp/codex", "--entrypoint", "python3", imageID, "-c", buildCenterSmokeProgram, string(data))
	if err != nil {
		return fmt.Errorf("隔离 smoke 检查失败，请查看日志：%w", err)
	}
	fmt.Fprintln(out, "声明的离线 smoke 检查成功；外部 MCP、模型推理、真实媒体生成未测试。")
	return nil
}

const buildCenterSmokeProgram = `import json,os,select,subprocess,sys,time
spec=json.loads(sys.argv[1])
os.makedirs(os.environ.get('CODEX_HOME','/tmp/codex'),exist_ok=True)
subprocess.run(['codex','--version'],check=True,timeout=20)
p=subprocess.Popen(['codex','app-server'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=sys.stderr)
try:
 p.stdin.write((json.dumps({'id':1,'method':'initialize','params':{'clientInfo':{'name':'rundesk-image-smoke','version':'1'}}})+'\n').encode());p.stdin.flush()
 end=time.monotonic()+20
 ok=False
 while time.monotonic()<end:
  ready,_,_=select.select([p.stdout],[],[],max(0,end-time.monotonic()))
  if not ready: break
  line=p.stdout.readline()
  if not line: break
  message=json.loads(line)
  if message.get('id')==1:
   if 'error' in message or not isinstance(message.get('result'),dict): raise RuntimeError('Codex initialize rejected')
   ok=True;break
 if not ok: raise RuntimeError('Codex initialize did not return a result')
 print('Codex app-server initialize passed; no model turn',flush=True)
finally:
 p.terminate()
 try: p.wait(timeout=3)
 except subprocess.TimeoutExpired: p.kill();p.wait()
if spec.get('command'): subprocess.run(spec['command'],check=True,timeout=min(spec.get('timeoutSeconds',180) or 180,600))
for path in spec.get('expectedFiles',[]):
 if not os.path.isfile(path) or os.path.getsize(path)==0: raise RuntimeError('missing or empty smoke output: '+path)
print('Declared smoke command and output checks passed',flush=True)
`
