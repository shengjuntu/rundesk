package app

import (
	"database/sql"
	"errors"
	"net/http"
)

// Only the server can set the submitting key. Never accept it from JSON.
func submittingKey(r *http.Request) string {
	if k := principal(r); k != nil {
		return k.ID
	}
	return ""
}

// Empty means a trusted internal/admin submission or a legacy record. We do not
// invent provenance for old jobs. Caller holds keyMu when admitting execution.
func (m *Manager) checkTaskAuthorization(keyID string, spec TaskSpec, scheduled bool) error {
	if keyID == "" {
		return nil
	}
	var record keyRecord
	err := m.Store.Get("application-key", keyID, &record)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return &apiError{Status: 503, Code: "task_authorization_unavailable", Message: "无法复核任务授权，任务未启动", Cause: err}
	}
	k := record.Key
	if err != nil || !k.valid() || !k.scope("run") || !k.owns(spec.InstanceID, spec.WorkspaceID, spec.Source) || (scheduled && !k.scope("schedules")) {
		return failure(403, "task_authorization_revoked", "提交凭据已撤销、过期或不再授权此任务，任务未启动；请使用有效凭据重新提交")
	}
	return nil
}
