package app

import (
	"encoding/json"
	"errors"
)

// Instance permissions override thread settings only, never the user's config file.
type Permissions struct {
	Sandbox        string `json:"sandbox"`
	ApprovalPolicy string `json:"approvalPolicy"`
	Reviewer       string `json:"reviewer"`
	NetworkAccess  *bool  `json:"networkAccess,omitempty"`
}

func (p Permissions) normalized() Permissions {
	if p.Sandbox == "" {
		p.Sandbox = "workspace-write"
	}
	if p.ApprovalPolicy == "" {
		p.ApprovalPolicy = "on-request"
	}
	if p.Reviewer == "" {
		p.Reviewer = "user"
	}
	return p
}
func (p Permissions) validate() error {
	p = p.normalized()
	if p.Sandbox != "workspace-write" && p.Sandbox != "read-only" && p.Sandbox != "danger-full-access" {
		return errors.New("不支持的沙箱模式")
	}
	if p.ApprovalPolicy != "on-request" && p.ApprovalPolicy != "never" {
		return errors.New("审批策略必须为 on-request 或 never")
	}
	if p.Reviewer != "user" && p.Reviewer != "auto_review" {
		return errors.New("审批处理方必须为 user 或 auto_review")
	}
	if p.NetworkAccess != nil && p.Sandbox != "workspace-write" {
		return errors.New("网络覆盖设置只适用于 workspace-write")
	}
	return nil
}
func (p Permissions) key() string { b, _ := json.Marshal(p.normalized()); return string(b) }
func (p Permissions) threadParams(cwd string) map[string]any {
	p = p.normalized()
	m := map[string]any{"cwd": cwd, "sandbox": p.Sandbox, "approvalPolicy": p.ApprovalPolicy, "approvalsReviewer": p.Reviewer}
	if p.NetworkAccess != nil {
		m["config"] = map[string]any{"sandbox_workspace_write.network_access": *p.NetworkAccess}
	}
	return m
}
