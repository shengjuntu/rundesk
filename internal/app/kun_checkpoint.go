package app

import (
	"context"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"time"
)

type kunResumeInput struct {
	Selection       p.CheckpointSelection
	SubmittingKeyID string
}

// This may reopen an idle worker to read its journal; it never starts a run/MCP/model.
func (m *Manager) KunCheckpoint(sid string) (p.CheckpointCheck, error) {
	s, err := m.Session(sid)
	if err != nil {
		return p.CheckpointCheck{}, err
	}
	if s.RuntimeKind != "kun" {
		return p.CheckpointCheck{}, failure(400, "not_kun", "该会话不使用 Kun")
	}
	if err = m.checkSessionExecution(s); err != nil {
		return p.CheckpointCheck{}, err
	}
	h, err := m.getHandle(sid)
	if err != nil {
		return p.CheckpointCheck{}, err
	}
	h.op.Lock()
	defer h.op.Unlock()
	h.admission.Lock()
	defer h.admission.Unlock()
	s, err = m.Session(sid)
	if err != nil {
		return p.CheckpointCheck{}, err
	}
	if active(s.Status) {
		return p.CheckpointCheck{}, failure(409, "session_busy", "请先结束当前运行")
	}
	if s.RunID == "" {
		return p.CheckpointCheck{Reason: "no_run"}, nil
	}
	if err = m.reserveProcess(sid, h); err != nil {
		return p.CheckpointCheck{}, err
	}
	defer m.unreserveProcess(sid)
	if _, err = m.ensureKunWorker(s, h); err != nil {
		return p.CheckpointCheck{}, err
	}
	w, err := m.Workspace(s.WorkspaceID)
	if err != nil {
		return p.CheckpointCheck{}, err
	}
	check, _, err := m.checkKunCheckpoint(s, w, h)
	return check, err
}
func (m *Manager) checkKunCheckpoint(s Session, w Workspace, h *handle) (p.CheckpointCheck, []Skill, error) {
	h.mu.Lock()
	c := h.kun
	h.mu.Unlock()
	if c == nil {
		return p.CheckpointCheck{}, nil, failure(409, "kun_offline", "请先检查恢复条件")
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	var state p.State
	if err := c.Call(ctx, "state", nil, &state); err != nil {
		return p.CheckpointCheck{}, nil, err
	}
	if state.RunID != s.RunID {
		return p.CheckpointCheck{}, nil, failure(409, "kun_run_conflict", "会话与 worker 的运行编号不同，不能恢复")
	}
	skills := make([]Skill, 0, len(state.Skills))
	for _, sk := range state.Skills {
		skills = append(skills, Skill{Name: sk.Name, Path: sk.Path})
	}
	start, err := m.kunStartRequest(s, w, Input{Text: "检查 Kun 恢复条件", Skills: skills})
	if err != nil {
		return p.CheckpointCheck{}, nil, err
	}
	var check p.CheckpointCheck
	err = c.Call(ctx, "checkpoint", start, &check)
	return check, skills, err
}
func (m *Manager) validateKunResume(s Session, w Workspace, h *handle, in Input) ([]Skill, error) {
	check, skills, err := m.checkKunCheckpoint(s, w, h)
	if err != nil {
		return nil, err
	}
	if !check.Eligible {
		return nil, failure(409, "kun_resume_blocked", "检查点不能恢复："+check.Reason)
	}
	if check.Selection != in.KunResume.Selection {
		return nil, failure(409, "kun_checkpoint_stale", "恢复条件已变化，请重新检查")
	}
	return skills, nil
}
func (s *Server) kunCheckpointRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/kun/checkpoint", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.Manager.KunCheckpoint(r.PathValue("sid"))
		respond(w, value, err)
	})
	mux.HandleFunc("POST /api/sessions/{sid}/kun/resume", func(w http.ResponseWriter, r *http.Request) {
		var selection p.CheckpointSelection
		if !decode(w, r, &selection) {
			return
		}
		if selection.Sequence < 1 || selection.SourceRunID == "" || selection.ExpectedRevision < 1 || selection.WorkerEpoch == "" {
			respond(w, nil, failure(400, "invalid_checkpoint", "请先检查恢复条件"))
			return
		}
		in := Input{Text: fmt.Sprintf("从 Kun 检查点 #%d 继续", selection.Sequence), LibraryOwner: personalOwner(r), KunResume: &kunResumeInput{Selection: selection, SubmittingKeyID: submittingKey(r)}}
		result, err := s.Manager.start(r.PathValue("sid"), in, nil)
		respond(w, result, err)
	})
}
