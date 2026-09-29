package app

import "strings"

// ReloadInstance closes idle connections only. Active turns and approvals survive.
func (m *Manager) ReloadInstance(id string) (any, error) {
	if _, e := m.Instance(id); e != nil {
		return nil, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	closed, busy := 0, 0
	for key, h := range m.handles {
		session := m.sessions[key]
		if !(session != nil && session.InstanceID == id) && !strings.HasPrefix(key, "config-"+id+"-") {
			continue
		}
		// Acquire op before mu, matching connect/run. Never wait while holding m.mu.
		if !h.op.TryLock() {
			busy++
			continue
		}
		if !h.mu.TryLock() {
			busy++
			h.op.Unlock()
			continue
		}
		if h.instanceID == id && h.client != nil {
			if s := m.sessions[key]; s != nil && active(s.Status) {
				busy++
			} else {
				c := h.client
				h.client = nil
				h.thread = ""
				c.Close()
				closed++
			}
		}
		h.mu.Unlock()
		h.op.Unlock()
	}
	return map[string]any{"closedConnections": closed, "busyConnections": busy, "note": "空闲连接将在下次使用时重新启动；运行中的任务保留。"}, nil
}
