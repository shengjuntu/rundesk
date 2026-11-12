package app

import "path/filepath"

func (m *Manager) maintenanceWorkspace(w Workspace) bool {
	for _, name := range []string{"build-assistant-workspaces", "setup-agent-workspace", "setup-repair-workspaces"} {
		if within(filepath.Join(m.Data, name), w.Path) {
			return true
		}
	}
	return false
}

func (m *Manager) requireDailyWorkspace(wid string) error {
	w, err := m.Workspace(wid)
	if err != nil {
		return err
	}
	if m.maintenanceWorkspace(w) {
		return failure(409, "maintenance_workspace_reserved", "此工作区由内部维护任务使用，请选择日常工作区")
	}
	return nil
}

// Daily work is always executed by Codex. Kun remains available through
// host-created maintenance, diagnosis and research sessions, not task input.
func (m *Manager) requireDailyInstance(iid string) error {
	i, err := m.Instance(iid)
	if err != nil {
		return err
	}
	switch runtimeKind(i.AgentRuntime.Kind) {
	case "codex":
		return nil
	case "kun":
		return failure(409, "kun_maintenance_only", "Kun 仅用于内部维护和轨迹分析；日常会话与应用任务请选择 Codex 环境")
	default:
		return failure(409, "backend_unsupported", "此环境的引擎尚未注册，无法开始会话")
	}
}

func (m *Manager) requireDailySession(sid string) error {
	s, err := m.Session(sid)
	if err != nil {
		return err
	}
	if runtimeKind(s.RuntimeKind) == "kun" {
		return failure(409, "kun_maintenance_only", "此 Kun 记录用于维护或历史查看，不能作为日常任务继续；请新建 Codex 会话")
	}
	return m.requireDailyInstance(s.InstanceID)
}

// Only trace question-and-answer sessions accept interactive Kun messages.
// Build/installation runs are controlled by their task-bound tools and queue.
func (m *Manager) allowInteractiveSession(sid string) error {
	s, err := m.Session(sid)
	if err != nil {
		return err
	}
	if runtimeKind(s.RuntimeKind) == "kun" && s.TraceOrigin != nil {
		return nil
	}
	return m.requireDailySession(sid)
}
