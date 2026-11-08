package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/your-org/rundesk/internal/store"
)

// Plans are immutable declarations, not acceptance evidence. The host binds
// each plan to one idempotent build request and derives QA from frozen records.
type BuildMaintenancePlan struct {
	RequestID  string   `json:"requestId"`
	Goal       string   `json:"goal"`
	Hypothesis string   `json:"hypothesis"`
	Preserve   []string `json:"preserve"`
	Checks     []string `json:"checks"`
}

type buildMaintenancePlanRecord struct {
	BuildMaintenancePlan
	RunID   string `json:"runId"`
	Created string `json:"created"`
	BuildID string `json:"buildId,omitempty"`
}

type BuildMaintenanceAttempt struct {
	Plan               BuildMaintenancePlan `json:"plan"`
	BuildID            string               `json:"buildId,omitempty"`
	Status             string               `json:"status"`
	ImageID            string               `json:"imageId,omitempty"`
	ContextSHA256      string               `json:"contextSha256,omitempty"`
	VerificationIDs    []string             `json:"verificationIds"`
	Offline            bool                 `json:"offline"`
	Smoke              bool                 `json:"smoke"`
	VerificationFailed bool                 `json:"verificationFailed"`
	Error              string               `json:"error,omitempty"`
}

type BuildMaintenanceView struct {
	Phase           string                    `json:"phase"`
	MaxAttempts     int                       `json:"maxAttempts"`
	Attempts        []BuildMaintenanceAttempt `json:"attempts"`
	VerifiedBuildID string                    `json:"verifiedBuildId,omitempty"`
	ExperienceID    string                    `json:"experienceId,omitempty"`
	StopReason      string                    `json:"stopReason,omitempty"`
}

func maintenancePlanKey(run, request string) string { return assistantScopedRequest(run, request) }

func (m *Manager) maintenancePlans(run string) ([]buildMaintenancePlanRecord, error) {
	rows, err := m.Store.List("build-maintenance-plan")
	if err != nil {
		return nil, err
	}
	result := []buildMaintenancePlanRecord{}
	for _, raw := range rows {
		var p buildMaintenancePlanRecord
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.RunID == run {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created < result[j].Created })
	return result, nil
}

// Caller holds buildAssistantMu. A lost response reuses the same immutable plan.
func (m *Manager) recordMaintenancePlan(cap buildAssistantCapability, plan BuildMaintenancePlan) (buildMaintenancePlanRecord, error) {
	var zero buildMaintenancePlanRecord
	if err := validateBuildCenterRequestID(plan.RequestID); err != nil {
		return zero, err
	}
	plan.Goal, plan.Hypothesis = strings.TrimSpace(plan.Goal), strings.TrimSpace(plan.Hypothesis)
	if plan.Goal == "" || plan.Hypothesis == "" || len(plan.Goal) > 2000 || len(plan.Hypothesis) > 4000 || len(plan.Preserve) == 0 || len(plan.Preserve) > 12 || len(plan.Checks) == 0 || len(plan.Checks) > 12 {
		return zero, failure(400, "maintenance_plan_invalid", "计划需要一个目标、诊断假设、要保留的能力和可检查的验收条件")
	}
	for _, rows := range [][]string{plan.Preserve, plan.Checks} {
		for _, item := range rows {
			if strings.TrimSpace(item) == "" || len(item) > 1000 {
				return zero, failure(400, "maintenance_plan_invalid", "计划条目不能为空或超过 1000 字节")
			}
		}
	}
	key := maintenancePlanKey(cap.RunID, plan.RequestID)
	var existing buildMaintenancePlanRecord
	if err := m.Store.Get("build-maintenance-plan", key, &existing); err == nil {
		if !reflect.DeepEqual(existing.BuildMaintenancePlan, plan) {
			return zero, failure(409, "request_id_conflict", "同一构建请求已经有不同计划，请为新尝试使用新编号")
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	view, err := m.buildMaintenanceView(cap.RunID, "")
	if err != nil {
		return zero, err
	}
	if view.VerifiedBuildID != "" {
		return zero, failure(409, "maintenance_already_verified", "本任务已有通过验证的候选，请保存经验并结束")
	}
	for _, a := range view.Attempts {
		if a.BuildID == "" || buildActive(a.Status) {
			return zero, failure(409, "maintenance_attempt_pending", "请先完成当前计划和构建，再决定下一项改进")
		}
		if a.Status == "succeeded" && !a.VerificationFailed {
			return zero, failure(409, "maintenance_verification_pending", "请先验证当前候选；只有独立检查失败后才开始下一次修改")
		}
	}
	if len(view.Attempts) >= 3 {
		return zero, failure(409, "build_repair_budget", "已达到三次改进上限，请保留证据并报告阻塞")
	}
	p := buildMaintenancePlanRecord{BuildMaintenancePlan: plan, RunID: cap.RunID, Created: store.Now()}
	return p, m.Store.Put("build-maintenance-plan", key, p)
}

func maintenanceTestMatches(test, build ImageBuild) bool {
	return test.Scope == "image-test" && test.OwnerRunID == build.OwnerRunID && test.SourceBuildID == build.ID && test.ImageID == build.ImageID && test.ContextSHA256 == build.ContextSHA256 && test.RecipeSHA == build.RecipeSHA && test.PlatformOS == build.PlatformOS && test.PlatformArch == build.PlatformArch && reflect.DeepEqual(test.CodexRuntime, build.CodexRuntime)
}

func (m *Manager) buildMaintenanceView(run, taskStatus string) (BuildMaintenanceView, error) {
	v := BuildMaintenanceView{Phase: "planning", MaxAttempts: 3, Attempts: []BuildMaintenanceAttempt{}}
	plans, err := m.maintenancePlans(run)
	if err != nil {
		return v, err
	}
	builds, err := m.BuildCenterBuilds()
	if err != nil {
		return v, err
	}
	for _, p := range plans {
		a := BuildMaintenanceAttempt{Plan: p.BuildMaintenancePlan, BuildID: p.BuildID, Status: "planned", VerificationIDs: []string{}}
		// The build request receipt survives a crash between build admission and
		// plan projection. Reconstruct the link, never start a second build.
		if a.BuildID == "" {
			var receipt buildCenterRequest
			if m.Store.Get("build-center-request", maintenancePlanKey(run, p.RequestID), &receipt) == nil {
				a.BuildID = receipt.ResultID
			}
		}
		for _, b := range builds {
			if b.ID != a.BuildID || b.OwnerRunID != run || b.Scope == "image-test" {
				continue
			}
			a.Status, a.ImageID, a.ContextSHA256, a.Error = b.Status, b.ImageID, b.ContextSHA256, sanitizeBuildAssistantText(b.Error, "")
			for _, test := range builds {
				if !maintenanceTestMatches(test, b) {
					continue
				}
				a.VerificationIDs = append(a.VerificationIDs, test.ID)
				if test.Status == "failed" || test.Status == "interrupted" || test.Status == "canceled" {
					a.VerificationFailed = true
				}
				if test.Status != "succeeded" {
					continue
				}
				if test.TestKind == "offline" {
					a.Offline = true
				}
				if test.TestKind == "smoke" && b.RecipeSmoke != nil && len(b.RecipeSmoke.Command) > 0 && reflect.DeepEqual(test.RecipeSmoke, b.RecipeSmoke) {
					a.Smoke = true
				}
			}
			if buildActive(b.Status) {
				v.Phase = "building"
			} else if b.Status == "succeeded" {
				v.Phase = "verifying"
			} else {
				v.Phase = "replanning"
			}
			if b.Status == "succeeded" && validBuildCenterImageID(b.ImageID) && a.Offline && a.Smoke {
				preserved := true
				if b.RebuildOf != "" {
					source, e := m.BuildCenterBuild(b.RebuildOf)
					preserved = e == nil && (source.RecipeSmoke == nil || reflect.DeepEqual(source.RecipeSmoke, b.RecipeSmoke))
				}
				if preserved {
					v.VerifiedBuildID = b.ID
				}
			}
		}
		v.Attempts = append(v.Attempts, a)
	}
	if v.VerifiedBuildID != "" {
		v.Phase = "verified"
		rows, err := m.BuildExperiences("image-build", "")
		if err != nil {
			return v, err
		}
		for _, x := range rows {
			if x.Success.ID == v.VerifiedBuildID && x.Status == "verified" {
				v.ExperienceID = x.ID
				break
			}
		}
	} else if taskStatus != "" && !taskPending(taskStatus) {
		v.Phase = "blocked"
		v.StopReason = "维护任务已结束，但尚无同时通过 offline 和 smoke 验证的候选"
	}
	return v, nil
}

func (m *Manager) requireMaintenancePlan(cap buildAssistantCapability, request string) (*buildMaintenancePlanRecord, error) {
	var run buildAssistantRecord
	if err := m.Store.Get("build-assistant", cap.RunID, &run); err != nil {
		return nil, err
	}
	if run.ProtocolVersion == 0 {
		return nil, nil
	} // Existing runs keep their contract.
	view, err := m.buildMaintenanceView(cap.RunID, "")
	if err != nil {
		return nil, err
	}
	var replay buildCenterRequest
	if m.Store.Get("build-center-request", maintenancePlanKey(cap.RunID, request), &replay) != nil && view.VerifiedBuildID != "" {
		return nil, failure(409, "maintenance_already_verified", "已验证成功，请保存经验并结束，不再继续构建")
	}
	var p buildMaintenancePlanRecord
	if err = m.Store.Get("build-maintenance-plan", maintenancePlanKey(cap.RunID, request), &p); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, failure(409, "maintenance_plan_required", "构建前请用 build_plan_record 保存目标、假设、保留能力与验收条件，并使用相同 requestId")
		}
		return nil, err
	}
	return &p, nil
}
