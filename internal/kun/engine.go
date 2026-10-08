package kun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	epoch           string
	expectedCatalog string
	modules         modules
	catalog         *toolCatalog
	clock           time.Time
	waiting         bool
	mcpSpecs        []p.MCPServer
	connections     map[string]*mcpConnection
	secrets         []string
	exchange        int64
	mu              sync.Mutex
	j               *journal
	state           p.State
	workspace, key  string
	cancel          context.CancelFunc
	done            chan struct{}
	wake            chan struct{}
	pause, single   bool
	fatal           chan error
	closed          bool
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
	epoch := make([]byte, 16)
	if _, e = rand.Read(epoch); e != nil {
		j.db.Close()
		return nil, e
	}
	v := &Engine{epoch: fmt.Sprintf("%x", epoch), modules: defaultModules(), j: j, state: s, wake: make(chan struct{}, 1), fatal: make(chan error, 1)}
	if p.Active(s.Status) {
		// Opening only reconciles records. Explicit recovery requires a safe checkpoint.
		for id, status := range v.state.Actions {
			if status == "dispatched" {
				v.state.Actions[id] = "outcome_unknown"
			} else if status == "prepared" {
				v.state.Actions[id] = "cancelled"
			}
		}
		for _, c := range v.state.Pending {
			v.state.Messages = append(v.state.Messages, p.Message{Role: "tool", ToolCallID: c.ID, Content: "Interrupted. Recorded outcome: " + v.state.Actions[c.ID] + ". Do not automatically repeat side effects; inspect the workspace."})
		}
		v.state.Pending = nil
		v.state.Approval = nil
		for n := range v.state.MCP {
			if v.state.MCP[n].Status != "failed" {
				v.state.MCP[n].Status = "closed"
			}
		}
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
	if !e.clock.IsZero() {
		e.tick(time.Now())
	}
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
	if len(in.MCP) > 16 {
		return p.State{}, fmt.Errorf("at most 16 enabled MCP servers are supported")
	}
	names := map[string]bool{}
	for _, spec := range in.MCP {
		if err := spec.Validate(); err != nil {
			return p.State{}, err
		}
		if names[spec.Name] {
			return p.State{}, fmt.Errorf("duplicate MCP server")
		}
		names[spec.Name] = true
	}
	if in.ApprovalPolicy == "" {
		in.ApprovalPolicy = "on-request"
	}
	if in.ApprovalPolicy != "on-request" && in.ApprovalPolicy != "never" {
		return p.State{}, fmt.Errorf("unsupported MCP approval policy")
	}
	safe := in
	safe.APIKey = ""
	safe.MCP = append([]p.MCPServer(nil), in.MCP...)
	for n := range safe.MCP {
		safe.MCP[n].Environment = nil
		safe.MCP[n].Headers = nil
	}
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
	if p.Active(e.state.Status) || e.state.Status == "completing" {
		return p.State{}, fmt.Errorf("session already running")
	}
	if e.state.SessionID != "" && e.state.SessionID != in.SessionID {
		return p.State{}, fmt.Errorf("session database belongs to another session")
	}
	if e.done != nil {
		select {
		case <-e.done:
		default:
			return p.State{}, fmt.Errorf("previous run is still closing")
		}
	}
	if in.Resume != nil {
		return e.resumeLocked(in, hash)
	}
	e.expectedCatalog = ""
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
	e.state.Harness = e.modules.harness()
	e.state.Manifest = e.manifest(in)
	e.state.Modules = map[string]p.ModuleState{}
	for name, version := range e.state.Harness.Modules {
		e.state.Modules[name] = p.ModuleState{Implementation: version, Phase: "pending", Data: p.JSON(map[string]any{})}
	}
	e.state.ApprovalPolicy = in.ApprovalPolicy
	for _, definition := range toolDefinitions(in.Config.AllowWrite) {
		e.state.ToolDefinitions = append(e.state.ToolDefinitions, p.JSON(definition))
	}
	for _, spec := range in.MCP {
		transport := "stdio"
		if spec.URL != "" {
			transport = "streamable-http"
		}
		e.state.MCP = append(e.state.MCP, p.MCPStatus{Name: spec.Name, Revision: spec.Revision, Transport: transport, Status: "pending"})
	}
	if len(p.JSON(e.state)) > 2<<20 {
		e.state = previousState
		return p.State{}, fmt.Errorf("context exceeds 2 MiB; start a new session")
	}
	if _, err = e.j.commit(e.state, "kun/run.started", map[string]any{"config": in.Config, "skills": in.Skills, "mcp": e.state.MCP, "approvalPolicy": in.ApprovalPolicy}, ""+in.RunID, hash, nil); err != nil {
		e.state.Status = "failed"
		return p.State{}, err
	}
	e.launchLocked(in)
	return clone(e.state), nil
}
func (e *Engine) launchLocked(in p.Start) {
	e.workspace, e.key = in.Workspace, in.APIKey
	_ = json.Unmarshal(p.JSON(in.MCP), &e.mcpSpecs)
	e.connections = map[string]*mcpConnection{}
	e.exchange = 0
	e.secrets = []string{in.APIKey}
	for _, spec := range in.MCP {
		for name, value := range spec.Environment {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "auth") {
				e.secrets = append(e.secrets, value)
			}
		}
		for _, value := range spec.Headers {
			e.secrets = append(e.secrets, value)
			if strings.HasPrefix(value, "Bearer ") {
				e.secrets = append(e.secrets, strings.TrimPrefix(value, "Bearer "))
			}
		}
	}
	sort.Slice(e.secrets, func(i, j int) bool { return len(e.secrets[i]) > len(e.secrets[j]) })
	e.clock, e.waiting = time.Now(), false
	e.pause, e.single = in.Config.PauseBeforeModel, false
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	go e.loop(ctx)
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
		if e.state.Status == "paused" {
			break
		}
		e.state.Queued = append(e.state.Queued, c)
		e.pause = true
		receipt.Status = "queued"
	case "resume", "step":
		if e.state.Approval != nil {
			return receipt, fmt.Errorf("MCP tool approval is required; resume/step cannot authorize it")
		}
		if e.state.Status != "paused" {
			return receipt, fmt.Errorf("run is not paused")
		}
		e.pause = false
		e.single = c.Operation == "step"
		e.state.Status = "running"
	case "approve", "reject":
		a := e.state.Approval
		if a == nil || a.CallID != c.CallID || a.Decision != "" {
			return receipt, fmt.Errorf("MCP approval conflict")
		}
		a.Decision = c.Operation
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
		e.setWaiting(true)
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
			e.setWaiting(false)
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
		wasCanceled := ctx.Err() != nil
		e.cancel()
		e.closeMCP()
		e.mu.Lock()
		defer e.mu.Unlock()
		e.setWaiting(false)
		e.state.Approval = nil
		for n := range e.state.MCP {
			if e.state.MCP[n].Status != "failed" {
				e.state.MCP[n].Status = "closed"
			}
		}
		if failure != nil {
			e.state.Status = "failed"
			e.state.Error = e.scrubText(failure.Error())
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
		_ = e.record("kun/run.finished", map[string]any{"status": e.state.Status, "error": e.state.Error, "budget": e.state.Budget})
		e.clock = time.Time{}
		e.secrets = nil
	}()
	failure = e.discoverMCP(ctx)
	if failure = e.activeFailure(failure); failure != nil {
		return
	}
	if failure = e.prepareCatalog(); failure != nil {
		return
	}
	for {
		e.mu.Lock()
		next := e.modules.policy.Next(clone(e.state))
		if next == "complete" {
			e.state.Status = "completing"
		}
		e.mu.Unlock()
		if next == "complete" {
			return
		}
		if next != "before_tool" {
			if failure = e.boundary(ctx, "before_model"); failure != nil {
				return
			}
			e.mu.Lock()
			if failure = e.checkBudget("model"); failure != nil {
				e.mu.Unlock()
				return
			}
			if failure = e.prepareContext(); failure != nil {
				e.mu.Unlock()
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
			callCtx, callCancel := e.activeContext(ctx)
			result, err := e.modules.provider.Complete(callCtx, s, key)
			callCancel()
			err = e.activeFailure(err)
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
			absorbUsage(&e.state.Budget, result.Usage)
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
			nextPhase := e.modules.policy.Next(clone(e.state))
			if nextPhase == "complete" {
				e.state.Status = "completing"
			} // Close admission before deciding to finish.
			e.mu.Unlock()
			if failure != nil {
				return
			}
			if nextPhase == "complete" {
				return
			}
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
			if failure = e.checkBudget("tool"); failure != nil {
				e.mu.Unlock()
				return
			}
			intent, validationErr := e.modules.action.Validate(call, e.catalog)
			if validationErr != nil {
				failure = e.finishTool(call, intent, toolResult{Output: validationErr.Error(), IsError: true}, "rejected", 0)
				e.mu.Unlock()
				if failure != nil {
					return
				}
				continue
			}
			e.moduleState("action", "validated", map[string]any{"callId": call.ID, "tool": intent.Name, "schemaHash": intent.SchemaHash, "argumentsHash": fingerprint(call.Function.Arguments)})
			failure = e.record("kun/tool.validated", e.state.Modules["action"])
			e.mu.Unlock()
			if failure != nil {
				return
			}
			if intent.MCP != nil {
				var allowed bool
				allowed, failure = e.approveMCP(ctx, *intent.MCP, call)
				if failure != nil {
					return
				}
				if !allowed {
					e.mu.Lock()
					failure = e.finishTool(call, intent, toolResult{Output: "MCP tool was denied by the user or approval policy. No external call was made.", IsError: true}, "declined", 0)
					e.mu.Unlock()
					if failure != nil {
						return
					}
					continue
				}
			}
			e.mu.Lock()
			if failure = ctx.Err(); failure == nil {
				failure = e.checkBudget("tool")
			}
			if failure != nil {
				e.mu.Unlock()
				return
			}
			e.state.Phase = "tool"
			e.state.Actions[call.ID] = "dispatched"
			e.state.Budget.ToolCalls++
			metadata := map[string]string{}
			if intent.MCP != nil {
				metadata = map[string]string{"server": intent.MCP.Server, "tool": intent.MCP.Name}
			}
			failure = e.record("kun/tool.started", map[string]any{"call": call, "step": e.state.Step, "mcp": metadata})
			e.mu.Unlock()
			if failure != nil {
				return
			}
			started := time.Now()
			toolCtx, toolCancel := e.activeContext(ctx)
			out, err := e.executeAction(toolCtx, intent, call)
			toolCancel()
			if err != nil {
				failure = e.activeFailure(fmt.Errorf("tool %s outcome unknown: %w", call.Function.Name, err))
				return
			}
			e.mu.Lock()
			status := "succeeded"
			if out.IsError {
				status = "failed"
			}
			failure = e.finishTool(call, intent, out, status, time.Since(started))
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
