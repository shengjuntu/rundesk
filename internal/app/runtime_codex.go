package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/resource"
)

func (b codexBackend) Run(run RuntimeRun) {
	s, w, h, in, recovery := run.Session, run.Workspace, run.Handle, run.Input, run.Recovery
	fail := func(e error) {
		h.mu.Lock()
		c := h.client
		h.client = nil
		h.thread = ""
		canceled := h.canceled || b.state.Context().Err() != nil
		h.mu.Unlock()
		if c != nil {
			c.Close()
			<-c.Done()
		}
		b.transport.ObservationDisconnected(s.ID)
		b.transport.ExpireApprovals(s.ID, h)
		if canceled {
			b.state.Finish(s.ID, "interrupted", "")
		} else {
			b.state.Finish(s.ID, "failed", e.Error())
		}
	}
	workspace, e := b.resources.Resolve(s, resource.RoleWorkspace)
	if e != nil {
		fail(e)
		return
	}
	w.Path = workspace.Path
	outputs, e := b.resources.Resolve(s, resource.RoleOutputs)
	if e != nil {
		fail(e)
		return
	}
	if e := b.transport.CheckBuildAssistantExecution(s); e != nil {
		fail(e)
		return
	}
	permissions, e := b.policy.Permissions(s)
	if e != nil {
		fail(e)
		return
	}
	if s.TraceOrigin != nil {
		permissions.Sandbox = "read-only"
		permissions.NetworkAccess = nil
	}
	h.mu.Lock()
	old := h.client
	changed := old != nil && (h.permissionsKey != "" && h.permissionsKey != permissions.key() || h.thread != "" && h.observationEnabled != s.ObserveHTTP)
	if changed {
		h.client = nil
		h.thread = ""
	}
	h.mu.Unlock()
	if changed {
		old.Close()
		<-old.Done()
	}
	c, e := b.transport.Connect(s.ID, w, h, s.InstanceID)
	if e != nil {
		fail(e)
		return
	}
	ctx, cancel := context.WithTimeout(b.state.Context(), 90*time.Second)
	defer cancel()
	h.mu.Lock()
	canceled := h.canceled
	h.mu.Unlock()
	if canceled {
		b.state.Finish(s.ID, "interrupted", "")
		return
	}
	// A fresh process must resume the native thread before it can accept a turn.
	h.mu.Lock()
	ready := h.thread != "" && h.thread == s.ThreadID
	h.mu.Unlock()
	var raw json.RawMessage
	if !ready {
		params := permissions.threadParams(w.Path)
		if e := b.transport.ConfigureBuildAssistant(s, w, params); e != nil {
			fail(e)
			return
		}
		captureConfigured := b.transport.ConfigureHTTPObservation(ctx, c, s, w, params)
		if s.TraceOrigin != nil {
			if e := b.transport.ConfigureTraceAnalysis(s, params); e != nil {
				fail(e)
				return
			}
		}
		if s.Model != "" {
			params["model"] = s.Model
		}
		method := "thread/start"
		if s.ThreadID != "" {
			method = "thread/resume"
			params["threadId"] = s.ThreadID
			params["excludeTurns"] = true
		}
		raw, e = c.Call(ctx, method, params)
		if e != nil {
			fail(fmt.Errorf("Codex %s: %w", method, e))
			return
		}
		var res struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if e = json.Unmarshal(raw, &res); e != nil || res.Thread.ID == "" {
			fail(errors.New("thread/start/resume 未返回 thread.id"))
			return
		}
		s.ThreadID = res.Thread.ID
		if e = b.state.Update(s.ID, func(v *Session) { v.ThreadID = s.ThreadID }); e != nil {
			fail(e)
			return
		}
		h.mu.Lock()
		h.thread = s.ThreadID
		h.permissionsKey = permissions.key()
		h.observationEnabled = s.ObserveHTTP
		h.mu.Unlock()
		if captureConfigured {
			b.transport.ObservationApplied(s.ID, s.ObserveHTTP)
		}
		b.transport.RecordEffective(s.ID, raw, permissions)
	}
	// Reload queues updated MCP configuration for the next turn on this thread.
	if _, e = c.Call(ctx, "config/mcpServer/reload", map[string]any{}); e != nil {
		fail(fmt.Errorf("MCP reload: %w", e))
		return
	}
	root, e := os.OpenRoot(w.Path)
	if e != nil {
		fail(e)
		return
	}
	e = root.MkdirAll(outputs.RelativePath, 0700)
	root.Close()
	if e != nil {
		fail(e)
		return
	}
	text := in.Text
	text += recoveryInstructions(recovery)
	if s.TraceOrigin != nil {
		text += traceAnalysisInstructions(s.TraceOrigin)
	}
	if w.Notes != "" {
		text += "\n\n<project-notes revision=\"" + fmt.Sprint(w.Revision) + "\">\n" + w.Notes + "\n</project-notes>"
	}
	text += "\n\n[Application context] Save deliverable files in " + outputs.Path + ". Uploaded files, if any, are listed below."
	input := []any{}
	for _, f := range in.Files {
		path := filepath.Join(w.Path, filepath.FromSlash(f))
		text += "\nUploaded file: " + path
		ext := strings.ToLower(filepath.Ext(f))
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			input = append(input, map[string]string{"type": "localImage", "path": path})
		}
	}
	input = append(input, map[string]string{"type": "text", "text": text})
	for _, sk := range in.Skills {
		input = append(input, map[string]string{"type": "skill", "name": sk.Name, "path": sk.Path})
	}
	h.mu.Lock()
	canceled = h.canceled
	h.mu.Unlock()
	if canceled {
		b.state.Finish(s.ID, "interrupted", "")
		return
	}
	turnParams := map[string]any{"threadId": s.ThreadID, "input": input}
	// A continued turn can select another available model even while the native
	// thread connection remains open. Thread-level setup alone would leave that
	// turn using the earlier model.
	if s.Model != "" {
		turnParams["model"] = s.Model
	}
	raw, e = c.Call(ctx, "turn/start", turnParams)
	if e != nil {
		fail(fmt.Errorf("Codex turn/start: %w", e))
		return
	}
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if e = json.Unmarshal(raw, &result); e != nil || result.Turn.ID == "" {
		fail(errors.New("Codex turn/start 未返回有效 turn.id；提交结果无法确认，未自动重发，请核对原生线程"))
		return
	}
	_ = b.state.Update(s.ID, func(v *Session) {
		if v.TurnID == "" {
			v.TurnID = result.Turn.ID
		}
		if v.Status == "starting" {
			v.Status = "running"
		}
	})
	h.mu.Lock()
	canceled = h.canceled
	h.mu.Unlock()
	if canceled {
		b.state.Go(func() { b.interrupt(s.ID, h, s.RunID) })
	}
}

// Stop aborts an in-flight turn submission before waiting for the run lock.
// A request tied to an old run is still checked by interrupt before any native
// turn is canceled.
func (b codexBackend) Stop(stop RuntimeStop) {
	s, h := stop.Session, stop.Handle
	current, err := b.state.Session(s.ID)
	if err != nil || current.RunID != s.RunID || !active(current.Status) {
		return
	}
	h.mu.Lock()
	if s.Status == "starting" && s.TurnID == "" && h.client != nil {
		h.client.Close()
	}
	h.mu.Unlock()
	b.state.Go(func() { b.interrupt(s.ID, h, s.RunID) })
}

func (b codexBackend) interrupt(id string, h *handle, expectedRunIDs ...string) {
	h.op.Lock()
	defer h.op.Unlock()
	s, e := b.state.Session(id)
	if e != nil || !active(s.Status) || (len(expectedRunIDs) > 0 && s.RunID != expectedRunIDs[0]) {
		return
	}
	h.mu.Lock()
	c := h.client
	h.mu.Unlock()
	if c == nil || s.TurnID == "" {
		b.state.Finish(id, "interrupted", "")
		return
	}
	ctx, cancel := context.WithTimeout(b.state.Context(), 10*time.Second)
	defer cancel()
	_, e = c.Call(ctx, "turn/interrupt", map[string]string{"threadId": s.ThreadID, "turnId": s.TurnID})
	if e != nil {
		c.Close()
		b.state.Finish(id, "interrupted", e.Error())
	}
	b.transport.ExpireApprovals(id, h)
}
