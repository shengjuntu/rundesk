package kun

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"path/filepath"
	"sync"
	"time"
)

type Engine struct {
	mu             sync.Mutex
	j              *journal
	state          p.State
	workspace, key string
	cancel         context.CancelFunc
	done           chan struct{}
	wake           chan struct{}
	pause, single  bool
	fatal          chan error
	closed         bool
}

func Open(path string) (*Engine, error) {
	j, e := openJournal(path)
	if e != nil {
		return nil, e
	}
	s, e := j.load()
	if e != nil {
		j.db.Close()
		return nil, e
	}
	v := &Engine{j: j, state: s, wake: make(chan struct{}, 1), fatal: make(chan error, 1)}
	if p.Active(s.Status) {
		// Recovery is inspection-only in v0.1. An unfinished write is never retried.
		for id, status := range v.state.Actions {
			if status == "dispatched" {
				v.state.Actions[id] = "outcome_unknown"
			}
		}
		for _, c := range v.state.Pending {
			v.state.Messages = append(v.state.Messages, p.Message{Role: "tool", ToolCallID: c.ID, Content: "Interrupted. Recorded outcome: " + v.state.Actions[c.ID] + ". Do not automatically repeat side effects; inspect the workspace."})
		}
		v.state.Pending = nil
		v.state.Status = "interrupted"
		v.state.Error = "Worker stopped before completion; inspect recorded actions before a new turn."
		if e = v.applyQueued("", true); e != nil {
			j.db.Close()
			return nil, e
		}
		if e = v.record("kun/run.finished", map[string]any{"status": "interrupted", "error": v.state.Error}); e != nil {
			j.db.Close()
			return nil, e
		}
	}
	return v, nil
}
func clone(s p.State) p.State    { var v p.State; _ = json.Unmarshal(p.JSON(s), &v); return v }
func (e *Engine) State() p.State { e.mu.Lock(); defer e.mu.Unlock(); return clone(e.state) }
func (e *Engine) Events(after int64) ([]p.Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.j.events(after, 40)
}
func (e *Engine) Snapshot(seq int64) (p.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.j.snapshot(seq)
}
func (e *Engine) Fatal() <-chan error { return e.fatal }
func (e *Engine) record(kind string, data any) error {
	e.state.Revision++
	_, err := e.j.commit(e.state, kind, data, "", "", nil)
	if err != nil {
		select {
		case e.fatal <- err:
		default:
		}
	}
	return err
}
func fingerprint(v any) string { return fmt.Sprintf("%x", sha256.Sum256(p.JSON(v))) }
func (e *Engine) Start(in p.Start) (p.State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return p.State{}, fmt.Errorf("worker closed")
	}
	in.Config = in.Config.Normalized()
	if err := in.Config.Validate(); err != nil {
		return p.State{}, err
	}
	if in.Config.Kind != "kun" || in.SessionID == "" || in.RunID == "" || in.Input == "" || len(in.Input) > 512*1024 {
		return p.State{}, fmt.Errorf("invalid run request")
	}
	if !filepath.IsAbs(in.Workspace) {
		return p.State{}, fmt.Errorf("workspace must be absolute")
	}
	safe := in
	safe.APIKey = ""
	hash := fingerprint(safe)
	var previous string
	err := e.j.db.QueryRow("SELECT fingerprint FROM runs WHERE id=?", in.RunID).Scan(&previous)
	if err == nil {
		if hash != previous {
			return p.State{}, fmt.Errorf("run ID reused with different input")
		}
		return clone(e.state), nil
	}
	if err != sql.ErrNoRows {
		return p.State{}, err
	}
	if p.Active(e.state.Status) {
		return p.State{}, fmt.Errorf("session already running")
	}
	if e.state.SessionID != "" && e.state.SessionID != in.SessionID {
		return p.State{}, fmt.Errorf("session database belongs to another session")
	}
	history := e.state.Messages
	// Rebuild system context from the effective config; preserve completed conversation messages.
	kept := []p.Message{}
	for _, m := range history {
		if m.Role != "system" {
			kept = append(kept, m)
		}
	}
	prompt := "You are Kun, the RunDesk agent. Use the provided tools when needed. Paths are workspace-relative. Report tool failures honestly.\n" + in.Config.SystemPrompt
	for _, sk := range in.Skills {
		prompt += "\n<skill name=" + sk.Name + " hash=" + sk.Hash + ">\n" + sk.Content + "\n</skill>"
	}
	previousState := clone(e.state)
	e.state = p.State{Schema: 1, SessionID: in.SessionID, RunID: in.RunID, Revision: e.state.Revision + 1, Status: "running", Phase: "before_model", Config: in.Config, Skills: in.Skills, Actions: map[string]string{}, Messages: append([]p.Message{{Role: "system", Content: prompt}}, kept...)}
	e.state.Messages = append(e.state.Messages, p.Message{Role: "user", Content: in.Input})
	if len(p.JSON(e.state)) > 2<<20 {
		e.state = previousState
		return p.State{}, fmt.Errorf("context exceeds 2 MiB; start a new session")
	}
	if _, err = e.j.commit(e.state, "kun/run.started", map[string]any{"config": in.Config, "skills": in.Skills}, ""+in.RunID, hash, nil); err != nil {
		e.state.Status = "failed"
		return p.State{}, err
	}
	e.workspace, e.key = in.Workspace, in.APIKey
	e.pause, e.single = in.Config.PauseBeforeModel, false
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	go e.loop(ctx)
	return clone(e.state), nil
}
func (e *Engine) Control(c p.Control) (p.Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var receipt p.Receipt
	var oldHash string
	var raw []byte
	hash := fingerprint(c)
	if len(c.RequestID) < 8 || len(c.RequestID) > 128 {
		return receipt, fmt.Errorf("invalid requestId")
	}
	err := e.j.db.QueryRow("SELECT fingerprint,receipt FROM commands WHERE id=?", c.RequestID).Scan(&oldHash, &raw)
	if err == nil {
		if hash != oldHash {
			return receipt, fmt.Errorf("requestId conflict")
		}
		err = json.Unmarshal(raw, &receipt)
		return receipt, err
	}
	if err != sql.ErrNoRows {
		return receipt, err
	}
	if c.RunID != e.state.RunID || !p.Active(e.state.Status) {
		return receipt, fmt.Errorf("run is not active")
	}
	if c.ExpectedRevision != e.state.Revision && !(c.ExpectedRevision == -1 && (c.Operation == "cancel" || c.Operation == "steer")) {
		return receipt, fmt.Errorf("state revision conflict; refresh and retry")
	}
	receipt = p.Receipt{RequestID: c.RequestID, Status: "applied", Revision: e.state.Revision + 1}
	switch c.Operation {
	case "pause":
		e.state.Queued = append(e.state.Queued, c)
		e.pause = true
		receipt.Status = "queued"
	case "resume", "step":
		if e.state.Status != "paused" {
			return receipt, fmt.Errorf("run is not paused")
		}
		e.pause = false
		e.single = c.Operation == "step"
		e.state.Status = "running"
	case "cancel":
		e.state.Queued = append(e.state.Queued, c)
		receipt.Status = "queued"
		e.cancel()
	case "steer":
		if c.Text == "" || len(c.Text) > 256*1024 || len(p.JSON(e.state.Queued))+len(c.Text) > 512*1024 {
			return receipt, fmt.Errorf("invalid steer text")
		}
		e.state.Queued = append(e.state.Queued, c)
		receipt.Status = "queued"
	default:
		return receipt, fmt.Errorf("unsupported control operation")
	}
	e.state.Revision++
	if _, err = e.j.commit(e.state, "kun/control."+receipt.Status, map[string]any{"command": c, "receipt": receipt}, c.RequestID, hash, &receipt); err != nil {
		e.cancel()
		return receipt, err
	}
	select {
	case e.wake <- struct{}{}:
	default:
	}
	return receipt, nil
}

// applyQueued runs with e.mu held, including after resuming an existing pause.
func (e *Engine) applyQueued(phase string, terminal bool) error {
	pending := e.state.Queued
	e.state.Queued = nil
	for index, c := range pending {
		if !terminal && (c.Operation == "cancel" || (c.Operation == "steer" && phase != "before_model")) {
			e.state.Queued = append(e.state.Queued, c)
			continue
		}
		if !terminal && c.Operation == "steer" {
			e.state.Messages = append(e.state.Messages, p.Message{Role: "user", Content: c.Text})
		}
		receipt := p.Receipt{RequestID: c.RequestID, Status: "applied", Revision: e.state.Revision + 1}
		if terminal && c.Operation != "cancel" {
			receipt.Status = "rejected"
		}
		e.state.Revision++
		snapshot := clone(e.state)
		snapshot.Queued = append(snapshot.Queued, pending[index+1:]...)
		if _, err := e.j.commit(snapshot, "kun/control."+receipt.Status, map[string]any{"command": c, "receipt": receipt}, c.RequestID, fingerprint(c), &receipt); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) boundary(ctx context.Context, phase string) error {
	e.mu.Lock()
	e.state.Phase = phase
	if err := e.applyQueued(phase, false); err != nil {
		e.mu.Unlock()
		return err
	}
	shouldPause := e.pause || (phase == "before_model" && e.state.Config.PauseBeforeModel)
	if shouldPause {
		e.pause = true
		e.state.Status = "paused"
		if err := e.record("kun/run.paused", map[string]string{"phase": phase}); err != nil {
			e.mu.Unlock()
			return err
		}
	}
	e.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.mu.Lock()
		paused := e.pause
		if !paused {
			err := e.applyQueued(phase, false)
			e.mu.Unlock()
			return err
		}
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.wake:
		}
	}
}
func (e *Engine) loop(ctx context.Context) {
	defer close(e.done)
	var failure error
	defer func() {
		if r := recover(); r != nil {
			failure = fmt.Errorf("Kun internal panic: %v", r)
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		wasCanceled := ctx.Err() != nil
		e.cancel()
		if failure != nil {
			e.state.Status = "failed"
			e.state.Error = failure.Error()
			if wasCanceled {
				e.state.Status = "interrupted"
			}
			for _, call := range e.state.Pending {
				status := e.state.Actions[call.ID]
				if status == "dispatched" {
					status = "outcome_unknown"
				} else if status == "" || status == "prepared" {
					status = "cancelled"
				}
				e.state.Actions[call.ID] = status
				e.state.Messages = append(e.state.Messages, p.Message{Role: "tool", ToolCallID: call.ID, Content: "Run stopped; action outcome: " + status + ". Do not automatically repeat an uncertain write."})
			}
			e.state.Pending = nil
		} else {
			e.state.Status = "completed"
		}
		_ = e.applyQueued("", true)
		_ = e.record("kun/run.finished", map[string]any{"status": e.state.Status, "error": e.state.Error})
	}()
	for {
		if failure = e.boundary(ctx, "before_model"); failure != nil {
			return
		}
		e.mu.Lock()
		if e.state.Step >= e.state.Config.MaxSteps {
			e.mu.Unlock()
			failure = fmt.Errorf("model step budget exhausted")
			return
		}
		if len(p.JSON(e.state)) > 2<<20 {
			e.mu.Unlock()
			failure = fmt.Errorf("context exceeds 2 MiB; start a new session")
			return
		}
		e.state.Step++
		e.state.Phase = "model"
		if failure = e.record("kun/model.started", map[string]any{"step": e.state.Step, "request": requestBody(e.state)}); failure != nil {
			e.mu.Unlock()
			return
		}
		s := clone(e.state)
		key := e.key
		e.mu.Unlock()
		started := time.Now()
		result, err := modelCall(ctx, s, key)
		if err != nil {
			failure = err
			return
		}
		e.mu.Lock()
		for _, call := range result.Message.ToolCalls {
			if _, exists := e.state.Actions[call.ID]; exists {
				e.mu.Unlock()
				failure = fmt.Errorf("model reused a tool call ID")
				return
			}
		}
		e.state.Messages = append(e.state.Messages, result.Message)
		e.state.Pending = result.Message.ToolCalls
		e.state.Phase = "after_model"
		for _, call := range e.state.Pending {
			e.state.Actions[call.ID] = "prepared"
		}
		failure = e.record("kun/model.completed", map[string]any{"step": s.Step, "message": result.Message, "usage": result.Usage, "durationMs": time.Since(started).Milliseconds()})
		if e.single {
			e.pause = true
		}
		hasTools := len(e.state.Pending) > 0
		hasSteer := false
		for _, c := range e.state.Queued {
			if c.Operation == "steer" {
				hasSteer = true
			}
		}
		if !hasTools && !hasSteer {
			e.state.Status = "completing"
		} // Close admission before deciding to finish.
		e.mu.Unlock()
		if failure != nil {
			return
		}
		if !hasTools && !hasSteer {
			return
		}
		for {
			e.mu.Lock()
			empty := len(e.state.Pending) == 0
			e.mu.Unlock()
			if empty {
				break
			}
			if failure = e.boundary(ctx, "before_tool"); failure != nil {
				return
			}
			e.mu.Lock()
			call := e.state.Pending[0]
			e.state.Phase = "tool"
			e.state.Actions[call.ID] = "dispatched"
			failure = e.record("kun/tool.started", map[string]any{"call": call, "step": e.state.Step})
			workspace, allow := e.workspace, e.state.Config.AllowWrite
			e.mu.Unlock()
			if failure != nil {
				return
			}
			started := time.Now()
			output, err := executeTool(workspace, allow, call)
			isError := err != nil
			if isError {
				output = err.Error()
			}
			e.mu.Lock()
			status := "succeeded"
			if isError {
				status = "failed"
			}
			e.state.Actions[call.ID] = status
			e.state.Messages = append(e.state.Messages, p.Message{Role: "tool", ToolCallID: call.ID, Content: output})
			e.state.Pending = e.state.Pending[1:]
			e.state.Phase = "after_tool"
			failure = e.record("kun/tool.completed", map[string]any{"call": call, "output": output, "isError": isError, "durationMs": time.Since(started).Milliseconds(), "step": e.state.Step})
			if e.single {
				e.pause = true
			}
			e.mu.Unlock()
			if failure != nil {
				return
			}
		}
	}
}
func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	if e.cancel != nil {
		e.cancel()
	}
	done := e.done
	e.mu.Unlock()
	if done != nil {
		<-done
	}
	e.j.db.Close()
}
