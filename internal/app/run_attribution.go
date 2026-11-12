package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/shengjuntu/rundesk/internal/resource"
	"github.com/shengjuntu/rundesk/internal/store"
)

// RunExecution is recorded at admission. The session may later use a different
// environment; historical consumers must not resolve these fields from it.
type RunExecution struct {
	SchemaVersion           int    `json:"schemaVersion,omitempty"`
	SessionID               string `json:"sessionId"`
	RunID                   string `json:"runId"`
	TaskID                  string `json:"taskId,omitempty"`
	WorkspaceID             string `json:"workspaceId,omitempty"`
	InstanceID              string `json:"instanceId,omitempty"`
	InstanceRevision        *int   `json:"instanceRevision,omitempty"`
	Engine                  string `json:"engine,omitempty"`
	RequestedModel          string `json:"requestedModel,omitempty"`
	ExecutionMode           string `json:"executionMode,omitempty"`
	EnvironmentID           string `json:"environmentId,omitempty"`
	ParticipationID         string `json:"participationId,omitempty"`
	ConfigurationRevisionID string `json:"configurationRevisionId,omitempty"`
}

func admittedRunExecution(s Session, taskID, participationID string, revision ConfigurationRevision) RunExecution {
	return RunExecution{
		SchemaVersion: 1, SessionID: s.ID, RunID: s.RunID, TaskID: taskID,
		WorkspaceID: s.WorkspaceID, InstanceID: s.InstanceID, InstanceRevision: &revision.InstanceRevision,
		Engine: revision.Engine, RequestedModel: s.Model,
		ExecutionMode: revision.Environment.Spec.Mode, EnvironmentID: revision.Environment.ID,
		ParticipationID: participationID, ConfigurationRevisionID: revision.ID,
	}
}

// Both identifiers come from a native acknowledgment, never an optimistic
// session update or the current connection's mutable thread state.
type RunNativeEvidence struct {
	NativeSessionID string `json:"nativeSessionId"`
	NativeRunID     string `json:"nativeRunId"`
	SourceEventID   int64  `json:"sourceEventId"`
	Source          string `json:"source"`
}

type RunAttribution struct {
	RunExecution
	Basis         string                `json:"basis"`
	InputEventID  int64                 `json:"inputEventId"`
	AdmittedAt    string                `json:"admittedAt"`
	Native        *RunNativeEvidence    `json:"native,omitempty"`
	Participation *RunParticipationInfo `json:"participation,omitempty"`
}

func (m *Manager) runAttribution(ctx context.Context, sid, rid string, through *int64) (RunAttribution, error) {
	var out RunAttribution
	input, native, err := m.Store.RunHistoryEvidence(ctx, sid, rid, through)
	if errors.Is(err, sql.ErrNoRows) {
		return out, failure(404, "run_not_found", "在当前会话的所选记录范围内未找到这次执行")
	}
	if errors.Is(err, store.ErrTraceCursor) {
		return out, failure(400, "invalid_trace_cursor", "轨迹游标不在该会话的记录范围内")
	}
	if err != nil {
		return out, err
	}
	var admitted struct {
		Execution  *RunExecution  `json:"execution"`
		InstanceID string         `json:"instanceId"`
		Revision   *int           `json:"instanceRevision"`
		Engine     string         `json:"declaredEngine"`
		TaskID     string         `json:"taskId"`
		Model      string         `json:"model"`
		Scope      resource.Scope `json:"scope"`
	}
	if err := json.Unmarshal(input.Data, &admitted); err != nil {
		return out, err
	}
	out.InputEventID, out.AdmittedAt = input.ID, input.Time
	if admitted.Execution != nil {
		if admitted.Execution.SchemaVersion != 1 || admitted.Execution.SessionID != sid || admitted.Execution.RunID != rid {
			return out, failure(409, "run_evidence_mismatch", "执行记录的版本或归属与请求不符")
		}
		out.RunExecution = *admitted.Execution
		out.Basis = "declared_at_admission"
		segment, err := m.participationForRun(out.ParticipationID, participationScope{
			SessionID: sid, InstanceID: out.InstanceID, WorkspaceID: out.WorkspaceID, Engine: out.Engine,
			ExecutionMode: out.ExecutionMode, EnvironmentID: out.EnvironmentID,
		})
		if err != nil {
			return out, err
		}
		out.Participation = &segment
	} else {
		out.RunExecution = RunExecution{SessionID: sid, RunID: rid, InstanceID: admitted.InstanceID, InstanceRevision: admitted.Revision, TaskID: admitted.TaskID, RequestedModel: admitted.Model}
		if admitted.Scope.SessionID == sid {
			out.WorkspaceID = admitted.Scope.WorkspaceID
			out.Engine = admitted.Engine
			if out.InstanceID == "" {
				out.InstanceID = admitted.Scope.InstanceID
			}
		}
		out.Basis = "legacy_evidence"
	}
	if native != nil {
		engine := "codex"
		if native.Method == "kun/run.started" {
			engine = "kun"
		}
		if out.Engine != "" && out.Engine != engine {
			return out, failure(409, "run_evidence_mismatch", "原生回执与执行引擎不符")
		}
		association := RunNativeEvidence{SourceEventID: native.ID, Source: native.Method}
		if err := json.Unmarshal(native.Data, &association); err != nil {
			return out, err
		}
		out.Engine, out.Native = engine, &association
	}
	return out, nil
}

// This branch uses the same authorization as the session configuration route.
// Reading historical evidence must not create an environment or probe an engine.
func (s *Server) historicalRunConfiguration(w http.ResponseWriter, r *http.Request, sid string) {
	query := r.URL.Query()
	rid := query.Get("runId")
	if rid == "" || len(rid) > 256 || strings.TrimSpace(rid) != rid {
		writeErr(w, 400, failure(400, "invalid_run_id", "执行编号无效"))
		return
	}
	var through *int64
	if query.Has("through") {
		cursor, err := strconv.ParseInt(query.Get("through"), 10, 64)
		if err != nil || cursor < 0 {
			writeErr(w, 400, failure(400, "invalid_trace_cursor", "轨迹游标无效"))
			return
		}
		through = &cursor
	}
	run, err := s.Manager.runAttribution(r.Context(), sid, rid, through)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var revision any
	if run.ConfigurationRevisionID != "" {
		revision, err = s.Manager.configurationRevisionForScope(run.ConfigurationRevisionID, run.InstanceID, run.WorkspaceID)
	}
	respond(w, map[string]any{"run": run, "configurationRevision": revision}, err)
}
