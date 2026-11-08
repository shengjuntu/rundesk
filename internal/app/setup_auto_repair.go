package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

// An automatic handoff is a durable authorization for exactly one source job,
// one Kun configuration and at most one repair task. It is not a policy that
// scans old failures or recursively repairs the agent's own attempts.
type SetupAutoRepairInput struct {
	Kind      string `json:"kind"`
	SourceID  string `json:"sourceId"`
	Enabled   bool   `json:"enabled"`
	RequestID string `json:"requestId"`
}

type SetupAutoRepair struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	SourceID         string `json:"sourceId"`
	Enabled          bool   `json:"enabled"`
	Status           string `json:"status"`
	KunRevision      int    `json:"kunRevision,omitempty"`
	ExpiresAt        string `json:"expiresAt,omitempty"`
	RefreshAttempted bool   `json:"refreshAttempted"`
	ModelRefreshedAt string `json:"modelRefreshedAt,omitempty"`
	RepairID         string `json:"repairId,omitempty"`
	SessionID        string `json:"sessionId,omitempty"`
	TaskID           string `json:"taskId,omitempty"`
	Error            string `json:"error,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type setupAutoRepairRecord struct {
	SetupAutoRepair
	KunFingerprint      string `json:"kunFingerprint"`
	SourceFingerprint   string `json:"sourceFingerprint"`
	Callback            string `json:"callback"`
	RepairRequestID     string `json:"repairRequestId"`
	GrantID             string `json:"grantId"`
	CredentialHash      string `json:"credentialHash"`
	InstanceFingerprint string `json:"instanceFingerprint"`
	RefreshOwner        string `json:"refreshOwner,omitempty"`
}

type setupAutoRepairReceipt struct {
	Fingerprint string `json:"fingerprint"`
	ID          string `json:"id"`
}

var setupAutoRepairMu sync.Mutex
var setupAutoRevalidations sync.Map

func setupAutoRevalidationKey(m *Manager, record setupAutoRepairRecord) string {
	return m.Data + "/" + record.ID + "/" + record.GrantID
}

func setupAutoExpired(record setupAutoRepairRecord) bool {
	expires, err := time.Parse(time.RFC3339Nano, record.ExpiresAt)
	return err != nil || time.Now().After(expires)
}

func setupAutoRepairID(kind, source string) string {
	return buildAssistantDigest("setup-auto-repair:" + kind + ":" + source)[:24]
}

func (m *Manager) setupAutoRepairSource(kind, id string, registration bool) (string, string, error) {
	switch kind {
	case "codex-install":
		job, err := m.CodexInstall(id)
		if err != nil {
			return "", "", err
		}
		if registration {
			rows, err := m.Store.List("setup-repair")
			if err != nil {
				return "", "", err
			}
			for _, raw := range rows {
				var repair setupRepairRecord
				if err := json.Unmarshal(raw, &repair); err != nil {
					return "", "", err
				}
				for _, attempt := range repair.Attempts {
					if attempt.InstallID == id {
						return "", "", failure(409, "setup_auto_recursive", "Kun 的修复尝试不能再次注册自动修复；请查看原修复任务")
					}
				}
				for _, attemptID := range repair.AttemptIDs {
					if attemptID == id {
						return "", "", failure(409, "setup_auto_recursive", "Kun 的修复尝试不能再次注册自动修复；请查看原修复任务")
					}
				}
			}
		}
		fingerprint := buildAssistantDigest(struct {
			ID, Platform, Created string
			Input                 CodexInstallInput
		}{job.ID, job.Platform, job.CreatedAt, job.Input})
		return job.Status, fingerprint, nil
	case "image-build":
		build, err := m.BuildCenterBuild(id)
		if err != nil {
			return "", "", err
		}
		if build.Scope == "image-test" {
			return "", "", failure(400, "setup_auto_source_invalid", "自动修复需要镜像构建，不能选择镜像检查任务")
		}
		if build.OwnerRunID != "" {
			return "", "", failure(409, "setup_auto_recursive", "构建助手产生的尝试由原任务负责，不能递归注册自动修复")
		}
		fingerprint := buildAssistantDigest(struct {
			ID, InstanceID, Created, Context, Recipe, Profile, Codex string
		}{build.ID, build.InstanceID, build.Created, build.ContextSHA256, build.RecipeSHA, build.ProfileID, build.CodexVersion})
		return build.Status, fingerprint, nil
	}
	return "", "", failure(400, "invalid_setup_auto_repair", "自动修复类型必须是 codex-install 或 image-build")
}

// Caller holds setupAgentMu when binding or exercising an authorization. A
// later settings save cannot race a build repair's task admission.
func (m *Manager) setupAutoRepairKunBinding() (SetupKunStatus, string, error) {
	status, err := m.setupKunStatus()
	if err != nil {
		return status, "", err
	}
	if !status.Ready {
		return status, "", failure(409, "setup_kun_not_ready", "Kun 引擎或模型推理/工具调用验证尚未就绪，或验证已经过期；请重新检查后启用自动修复")
	}
	var saved setupKunConfig
	if err := m.Store.Get("system", "setup-kun", &saved); err != nil {
		return status, "", err
	}
	return status, buildAssistantDigest(saved), nil
}

func (m *Manager) RegisterSetupAutoRepair(in SetupAutoRepairInput, callback string) (SetupAutoRepair, error) {
	if (in.Kind != "codex-install" && in.Kind != "image-build") || !buildCenterID.MatchString(in.SourceID) || !buildCenterID.MatchString(in.RequestID) {
		return SetupAutoRepair{}, failure(400, "invalid_setup_auto_repair", "请选择有效来源任务和唯一 requestId")
	}
	setupAutoRepairMu.Lock()
	defer setupAutoRepairMu.Unlock()
	fingerprint := buildAssistantDigest(in)
	var receipt setupAutoRepairReceipt
	if err := m.Store.Get("setup-auto-repair-request", in.RequestID, &receipt); err == nil {
		if receipt.Fingerprint != fingerprint {
			return SetupAutoRepair{}, failure(409, "request_id_conflict", "requestId 已用于另一项自动修复授权")
		}
		var previous setupAutoRepairRecord
		if err := m.Store.Get("setup-auto-repair", receipt.ID, &previous); err != nil {
			return SetupAutoRepair{}, err
		}
		return previous.SetupAutoRepair, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SetupAutoRepair{}, err
	}
	id := setupAutoRepairID(in.Kind, in.SourceID)
	var record setupAutoRepairRecord
	err := m.Store.Get("setup-auto-repair", id, &record)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SetupAutoRepair{}, err
	}
	if record.ID == "" {
		if !in.Enabled {
			if _, _, err := m.setupAutoRepairSource(in.Kind, in.SourceID, false); err != nil {
				return SetupAutoRepair{}, err
			}
		}
		record = setupAutoRepairRecord{SetupAutoRepair: SetupAutoRepair{ID: id, Kind: in.Kind, SourceID: in.SourceID, CreatedAt: store.Now()}, RepairRequestID: buildAssistantDigest("setup-auto-repair-task:" + in.Kind + ":" + in.SourceID)[:24]}
	}
	if !in.Enabled {
		// Disabling must remain possible after a source was cleaned up or its
		// model configuration stopped working. It does not cancel an admitted
		// repair: the ordinary task cancellation interface handles that.
		record.Enabled, record.Status, record.Error = false, "disabled", ""
	} else {
		_, sourceFingerprint, err := m.setupAutoRepairSource(in.Kind, in.SourceID, true)
		if err != nil {
			return SetupAutoRepair{}, err
		}
		u, err := url.Parse(callback)
		if err != nil || u.Path != "/api/build-center/mcp" {
			return SetupAutoRepair{}, failure(400, "invalid_setup_auto_callback", "自动修复需要合法的构建 MCP 回调")
		}
		origin := *u
		origin.Path = ""
		if _, err := buildAssistantOrigin(origin.String(), false); err != nil {
			return SetupAutoRepair{}, err
		}
		setupAgentMu.Lock()
		status, kunFingerprint, err := m.setupAutoRepairKunBinding()
		credential, credentialErr := setupModelCredential(status.APIKeyEnv)
		instance, instanceErr := m.Instance(status.InstanceID)
		setupAgentMu.Unlock()
		if err != nil {
			return SetupAutoRepair{}, err
		}
		if credentialErr != nil {
			return SetupAutoRepair{}, credentialErr
		}
		if instanceErr != nil {
			return SetupAutoRepair{}, instanceErr
		}
		rows, err := m.Store.List("setup-auto-repair")
		if err != nil {
			return SetupAutoRepair{}, err
		}
		watching := 0
		for _, raw := range rows {
			var existing setupAutoRepairRecord
			if err := json.Unmarshal(raw, &existing); err != nil {
				return SetupAutoRepair{}, err
			}
			if existing.ID != id && existing.Enabled && (existing.Status == "watching" || existing.Status == "revalidating") {
				watching++
			}
		}
		if watching >= 32 {
			return SetupAutoRepair{}, failure(429, "setup_auto_repair_limit", "最多同时监视 32 个初始化任务")
		}
		record.Enabled, record.Error = true, ""
		if record.RepairID != "" {
			record.Status = "submitted"
		} else {
			record.Status = "watching"
			record.KunRevision, record.KunFingerprint = status.Revision, kunFingerprint
			record.SourceFingerprint, record.Callback = sourceFingerprint, callback
			record.CredentialHash, record.InstanceFingerprint = keyHash(credential), setupAgentInstanceFingerprint(instance)
			record.GrantID, record.ExpiresAt = in.RequestID, time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339Nano)
			record.RefreshAttempted, record.ModelRefreshedAt, record.RefreshOwner = false, "", ""
		}
	}
	record.UpdatedAt = store.Now()
	if err := m.Store.PutMany(store.Record{Kind: "setup-auto-repair", ID: id, Value: record}, store.Record{Kind: "setup-auto-repair-request", ID: in.RequestID, Value: setupAutoRepairReceipt{Fingerprint: fingerprint, ID: id}}); err != nil {
		return SetupAutoRepair{}, err
	}
	return record.SetupAutoRepair, nil
}

func (m *Manager) SetupAutoRepairs() ([]SetupAutoRepair, error) {
	rows, err := m.Store.List("setup-auto-repair")
	if err != nil {
		return nil, err
	}
	items := []SetupAutoRepair{}
	for _, raw := range rows {
		var record setupAutoRepairRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		value := record.SetupAutoRepair
		value.Error = buildExperienceText(value.Error, 2048)
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	if len(items) > 200 {
		items = items[:200]
	}
	return items, nil
}

func (m *Manager) setupAutoRepairTask(record setupAutoRepairRecord) (string, string, string, error) {
	if record.Kind == "codex-install" {
		callback := strings.TrimSuffix(record.Callback, "/api/build-center/mcp") + "/api/setup/agent-mcp"
		run, err := m.CreateSetupRepair(SetupRepairInput{Kind: "codex-install", InstallID: record.SourceID, RequestID: record.RepairRequestID, ExpectedSetupRevision: record.KunRevision, ExpectedSetupFingerprint: record.KunFingerprint, ExpectedInstanceFingerprint: record.InstanceFingerprint}, callback)
		return run.ID, run.SessionID, run.TaskID, err
	}
	setupAgentMu.Lock()
	defer setupAgentMu.Unlock()
	status, fingerprint, err := m.setupAutoRepairKunBinding()
	if err != nil {
		return "", "", "", err
	}
	if status.Revision != record.KunRevision || fingerprint != record.KunFingerprint {
		return "", "", "", failure(409, "setup_auto_configuration_changed", "Kun 配置已经改变；请重新明确启用此来源任务的自动修复")
	}
	instance, err := m.Instance(status.InstanceID)
	if err != nil || setupAgentInstanceFingerprint(instance) != record.InstanceFingerprint {
		return "", "", "", failure(409, "setup_auto_configuration_changed", "Kun 实例配置或权限已经改变；请重新明确启用自动修复")
	}
	run, err := m.CreateBuildAssistantRun(BuildAssistantInput{SourceBuildID: record.SourceID, InstanceID: status.InstanceID, WorkspaceID: status.WorkspaceID, RequestID: record.RepairRequestID, Prompt: "诊断并修复此初始化镜像构建。保持原来的工具功能与 smoke 验收条件，读取实际失败日志，按已配置源和固定版本调整独立配方副本，进行真实构建及 offline/smoke 检查，并保存适用范围明确的修复经验。"}, record.Callback)
	return run.ID, run.SessionID, run.TaskID, err
}

// One bounded pass is separately callable for restart/reconciliation tests.
// It only reads explicit watching grants and does not enumerate old failures.
func (m *Manager) checkSetupAutoRepairs() {
	setupAutoRepairMu.Lock()
	defer setupAutoRepairMu.Unlock()
	rows, err := m.Store.List("setup-auto-repair")
	if err != nil {
		return
	}
	processed := 0
	for _, raw := range rows {
		if m.ctx.Err() != nil || processed >= 32 {
			return
		}
		var record setupAutoRepairRecord
		if json.Unmarshal(raw, &record) != nil || !record.Enabled || (record.Status != "watching" && record.Status != "revalidating") {
			continue
		}
		processed++
		if record.Status == "revalidating" {
			if _, active := setupAutoRevalidations.Load(setupAutoRevalidationKey(m, record)); active && record.RefreshOwner == m.requestOwner {
				continue
			}
			record.Status, record.Error, record.UpdatedAt = "blocked", "模型复测在服务重启前未确认；请重新测试并明确启用自动修复", store.Now()
			_ = m.Store.Put("setup-auto-repair", record.ID, record)
			continue
		}
		sourceStatus, fingerprint, sourceErr := m.setupAutoRepairSource(record.Kind, record.SourceID, false)
		if sourceErr != nil {
			record.Status, record.Error = "failed", buildExperienceText(sourceErr.Error(), 2048)
		} else if fingerprint != record.SourceFingerprint {
			record.Status, record.Error = "blocked", "来源任务的输入已经变化；需要重新明确授权"
		} else if sourceStatus == "succeeded" || sourceStatus == "canceled" {
			record.Status, record.Error = "completed", ""
		} else if setupAutoExpired(record) {
			record.Status, record.Error = "blocked", "本次两小时自动修复授权已过期；请重新检查后明确启用"
		} else {
			var saved setupKunConfig
			err := m.Store.Get("system", "setup-kun", &saved)
			if err != nil || saved.Revision != record.KunRevision || buildAssistantDigest(saved) != record.KunFingerprint {
				record.Status, record.Error = "blocked", "Kun 初始化配置已经改变；需要重新测试并明确授权"
			} else if sourceStatus == "failed" || sourceStatus == "interrupted" {
				waiting, err := m.setupAutoRepairRefreshModel(&record, saved)
				if err != nil {
					record.Status, record.Error = "blocked", buildExperienceText(err.Error(), 2048)
				} else if !waiting {
					repairID, sessionID, taskID, err := m.setupAutoRepairTask(record)
					if err != nil {
						record.Status, record.Error = "blocked", buildExperienceText(err.Error(), 2048)
					} else {
						record.Status, record.RepairID, record.SessionID, record.TaskID, record.Error = "submitted", repairID, sessionID, taskID, ""
					}
				}
			}
		}
		if record.Status != "watching" {
			record.UpdatedAt = store.Now()
			_ = m.Store.Put("setup-auto-repair", record.ID, record)
		}
	}
}

// setupAutoRepairRefreshModel is admitted by the explicit two-hour handoff. It
// may refresh a time-expired, previously successful receipt exactly once, with
// the same worker, model, endpoint and credential. It never executes a tool or
// changes a model setting. At most two probes run at once; the monitor remains
// available for revocation and other jobs while their bounded requests run.
func (m *Manager) setupAutoRepairRefreshModel(record *setupAutoRepairRecord, saved setupKunConfig) (bool, error) {
	credential, err := setupModelCredential(saved.APIKeyEnv)
	if err != nil || keyHash(credential) != record.CredentialHash {
		return false, failure(409, "setup_auto_credential_changed", "模型凭据已经改变；需要重新测试并明确授权")
	}
	instance, err := m.Instance(saved.InstanceID)
	if err != nil || setupAgentInstanceFingerprint(instance) != record.InstanceFingerprint {
		return false, failure(409, "setup_auto_configuration_changed", "Kun 实例配置或权限已经改变；需要重新明确授权")
	}
	status, err := m.setupKunStatus()
	if err != nil || !status.Available {
		return false, failure(409, "setup_kun_not_ready", "Kun 引擎不可用或已经改变；请重新检查")
	}
	if status.Ready {
		return false, nil
	}
	var evidence setupModelTestEvidence
	if err := m.Store.Get("setup-model-test", setupModelTestKey(saved.Endpoint, saved.Model, saved.APIKeyEnv), &evidence); err != nil {
		return false, failure(409, "setup_auto_model_test_missing", "模型验证记录缺失；请手工重新测试")
	}
	checked, err := time.Parse(time.RFC3339Nano, evidence.CheckedAt)
	if err != nil || time.Since(checked) <= 30*time.Minute || evidence.Endpoint != saved.Endpoint || evidence.Model != saved.Model || evidence.APIKeyEnv != saved.APIKeyEnv || evidence.CredentialHash != record.CredentialHash || !evidence.InferenceOK || !evidence.ToolsOK {
		return false, failure(409, "setup_auto_model_test_failed", "模型验证不再满足原授权条件；请重新检查，不会自动换模型或凭据")
	}
	if record.RefreshAttempted {
		return false, failure(409, "setup_auto_model_refresh_used", "本次授权已经复测过模型；请重新测试后明确启用")
	}
	active := 0
	setupAutoRevalidations.Range(func(key, _ any) bool {
		if strings.HasPrefix(key.(string), m.Data+"/") {
			active++
		}
		return true
	})
	if active >= 2 {
		return true, nil
	}
	record.Status, record.RefreshAttempted, record.RefreshOwner, record.UpdatedAt = "revalidating", true, m.requestOwner, store.Now()
	if err := m.Store.Put("setup-auto-repair", record.ID, record); err != nil {
		return false, err
	}
	snapshot := *record
	key := setupAutoRevalidationKey(m, snapshot)
	setupAutoRevalidations.Store(key, true)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer setupAutoRevalidations.Delete(key)
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		defer cancel()
		client := setupProbeClient(40 * time.Second)
		defer client.CloseIdleConnections()
		result := setupTestModel(ctx, client, SetupModelTestInput{Endpoint: saved.Endpoint, Model: saved.Model, APIKeyEnv: saved.APIKeyEnv}, credential)
		result.Error = buildExperienceText(result.Error, 2048)
		setupAutoRepairMu.Lock()
		defer setupAutoRepairMu.Unlock()
		var current setupAutoRepairRecord
		if m.Store.Get("setup-auto-repair", snapshot.ID, &current) != nil || !current.Enabled || current.Status != "revalidating" || current.GrantID != snapshot.GrantID {
			return
		}
		current.Status, current.Error, current.UpdatedAt = "blocked", "模型复测未通过："+result.Error, store.Now()
		var actual setupKunConfig
		actualCredential, credentialErr := setupModelCredential(saved.APIKeyEnv)
		instance, instanceErr := m.Instance(saved.InstanceID)
		if m.Store.Get("system", "setup-kun", &actual) != nil || buildAssistantDigest(actual) != snapshot.KunFingerprint || credentialErr != nil || keyHash(actualCredential) != snapshot.CredentialHash || instanceErr != nil || setupAgentInstanceFingerprint(instance) != snapshot.InstanceFingerprint || setupAutoExpired(current) {
			current.Error = "复测期间授权或 Kun 配置/凭据发生变化；请重新明确授权"
		} else if result.InferenceOK && result.ToolsOK && result.RoundTripOK {
			fresh := setupModelTestEvidence{SetupModelTestResult: result, APIKeyEnv: saved.APIKeyEnv, CredentialHash: snapshot.CredentialHash}
			if err := m.Store.Put("setup-model-test", setupModelTestKey(saved.Endpoint, saved.Model, saved.APIKeyEnv), fresh); err != nil {
				current.Error = buildExperienceText(err.Error(), 2048)
			} else {
				current.Status, current.Error, current.ModelRefreshedAt = "watching", "", result.CheckedAt
			}
		}
		_ = m.Store.Put("setup-auto-repair", current.ID, current)
	}()
	return true, nil
}

func (m *Manager) startSetupAutoRepairs() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-tick.C:
				m.checkSetupAutoRepairs()
			}
		}
	}()
}

func (s *Server) setupAutoRepairRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/auto-repairs", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Manager.SetupAutoRepairs()
		respondBuildSource(w, map[string]any{"items": items}, err)
	})
	mux.HandleFunc("POST /api/setup/auto-repairs", func(w http.ResponseWriter, r *http.Request) {
		var in SetupAutoRepairInput
		if !decode(w, r, &in) {
			return
		}
		callback := ""
		if in.Enabled {
			i, err := s.Manager.Instance(setupKunInstanceID)
			if err != nil {
				respondBuildSource(w, nil, failure(409, "setup_kun_not_ready", "请先配置和验证 Kun 初始化助手"))
				return
			}
			callback, err = s.buildAssistantCallback(r, i)
			if err != nil {
				respondBuildSource(w, nil, err)
				return
			}
		}
		value, err := s.Manager.RegisterSetupAutoRepair(in, callback)
		respondBuildSource(w, value, err)
	})
}
