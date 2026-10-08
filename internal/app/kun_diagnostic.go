package app

import (
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"os"
	"path/filepath"
)

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
	cfg.PauseBeforeModel = false
	cfg.Debug = p.DebugPolicy{}
	cfg.SystemPrompt = "" // No task-specific instructions or project notes in diagnostics.
	request := p.Start{SessionID: s.ID, RunID: s.RunID, Workspace: workspace, Input: in.Text + traceAnalysisInstructions(origin), Config: cfg, APIKey: key, ApprovalPolicy: "never", Diagnostic: &p.DiagnosticSource{DiagnosticScope: p.DiagnosticScope{SessionID: origin.SessionID, RunID: origin.RunID, Through: origin.Through}, SnapshotPath: snapshot}}
	return request, nil
}
