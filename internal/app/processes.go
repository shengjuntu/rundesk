package app

import (
	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"os"
	"runtime"
	"sort"
)

type ProcessEntry struct {
	rpc.ProcessInfo
	ConnectionID  string `json:"connectionId"`
	InstanceID    string `json:"instanceId"`
	WorkspaceID   string `json:"workspaceId"`
	SessionID     string `json:"sessionId,omitempty"`
	Kind          string `json:"kind"`
	ExecutionMode string `json:"executionMode"`
}

func (m *Manager) Processes() []ProcessEntry {
	m.mu.Lock()
	hs := map[string]*handle{}
	sessionIDs := map[string]bool{}
	for id, h := range m.handles {
		hs[id] = h
		sessionIDs[id] = m.sessions[id] != nil
	}
	m.mu.Unlock()
	out := []ProcessEntry{}
	for id, h := range hs {
		h.mu.Lock()
		c := h.client
		k := h.kun
		h.mu.Unlock()
		if k != nil {
			select {
			case <-k.Done():
			default:
				session, _ := m.Session(id)
				out = append(out, ProcessEntry{ProcessInfo: rpc.ProcessInfo{PID: k.PID(), CleanupMode: "managed-stdio"}, SessionID: id, InstanceID: session.InstanceID, WorkspaceID: session.WorkspaceID, Kind: "kun", ExecutionMode: "local"})
			}
		}
		if c == nil {
			continue
		}
		info := c.ProcessInfo()
		if info.Exited {
			continue
		}
		var v RuntimeStatus
		_ = m.Store.Get("runtime", id, &v)
		entry := ProcessEntry{ProcessInfo: info, ConnectionID: v.ConnectionID, InstanceID: v.InstanceID, WorkspaceID: v.WorkspaceID, Kind: "configuration", ExecutionMode: v.ExecutionMode}
		if sessionIDs[id] {
			entry.Kind = "session"
			entry.SessionID = id
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].PID < out[b].PID })
	return out
}
func (s *Server) processRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/processes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"serverPID": os.Getpid(), "platform": runtime.GOOS, "cleanupMode": rpc.CleanupMode, "scope": "app-server-connections", "observedAt": store.Now(), "items": s.Manager.Processes()})
	})
}
