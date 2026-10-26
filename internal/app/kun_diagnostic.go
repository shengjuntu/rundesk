package app

import (
	p "github.com/your-org/kun/sdk/protocol"
	"os"
	"path/filepath"
	"strconv"
)

// The host owns the snapshot format. Kun only receives a scoped, read-only MCP
// provider; its database schema is not part of the engine's diagnostic contract.
var kunDiagnosticProviderExecutable = os.Executable

var kunDiagnosticTools = []string{
	"trace_list_runs", "trace_find_steps", "trace_get_step", "trace_read_event",
	"trace_find_issues", "trace_propose", "trace_list_llm_requests",
	"trace_read_llm_request", "trace_statistics", "trace_list_observations", "trace_list_activity",
}

func (m *Manager) kunDiagnosticRequest(s Session, w Workspace, in Input, cfg p.Config, key string) (p.Start, error) {
	if len(in.Files) > 0 || len(in.Skills) > 0 || in.KunResume != nil {
		return p.Start{}, failure(400, "diagnostic_input_only", "诊断会话只接收问题文本")
	}
	origin := s.TraceOrigin
	source, err := m.Session(origin.SessionID)
	if err != nil {
		return p.Start{}, err
	}
	if source.WorkspaceID != s.WorkspaceID || source.InstanceID != s.InstanceID {
		return p.Start{}, failure(409, "diagnostic_source_changed", "来源归属已改变")
	}
	snapshot := filepath.Join(m.Data, "kun", "sessions", s.ID, "trace-source.db")
	if err = m.Store.ExportTraceSnapshot(snapshot, origin.SessionID, origin.Through); err != nil {
		return p.Start{}, err
	}
	workspace := filepath.Join(m.Data, "kun", "sessions", s.ID, "diagnostic-workspace")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		return p.Start{}, err
	}
	cfg.AllowWrite = false
	cfg.AllowBash = false
	cfg.PauseBeforeModel = false
	cfg.Debug = p.DebugPolicy{}
	cfg.SystemPrompt = "" // No task-specific instructions or project notes in diagnostics.
	executable, err := kunDiagnosticProviderExecutable()
	if err != nil {
		return p.Start{}, err
	}
	provider := &p.MCPServer{
		Name: "rundesk_trace", Revision: forkDigest(origin), Command: executable,
		Args:           []string{"__trace_mcp", snapshot, origin.SessionID, strconv.FormatInt(origin.Through, 10)},
		StartupTimeout: 15, ToolTimeout: 30, Allowlist: true,
		EnabledTools: append([]string(nil), kunDiagnosticTools...),
	}
	request := p.Start{SessionID: s.ID, RunID: s.RunID, Workspace: workspace, Input: in.Text + traceAnalysisInstructions(origin), Config: cfg, APIKey: key, ApprovalPolicy: "never", Diagnostic: &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: origin.SessionID, RunID: origin.RunID, Through: origin.Through}, Provider: provider}}
	return request, nil
}
