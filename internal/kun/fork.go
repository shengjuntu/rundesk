package kun

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

// Normalize object key order/whitespace, preserve exact JSON numbers, and reject
// duplicate keys. Different numeric spellings conservatively remain distinct.
func replayArguments(raw string) (string, error) {
	if len(raw) > 256<<10 {
		return "", fmt.Errorf("arguments too large")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 64 {
			return nil, fmt.Errorf("arguments too deep")
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t {
		case json.Delim('{'):
			v := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid argument key")
				}
				if _, exists := v[name]; exists {
					return nil, fmt.Errorf("duplicate argument key")
				}
				item, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				v[name] = item
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("invalid object")
			}
			return v, nil
		case json.Delim('['):
			v := []any{}
			for d.More() {
				item, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				v = append(v, item)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("invalid array")
			}
			return v, nil
		default:
			if _, bad := t.(json.Delim); bad {
				return nil, fmt.Errorf("invalid value")
			}
			return t, nil
		}
	}
	v, err := read(0)
	if err != nil {
		return "", err
	}
	if _, ok := v.(map[string]any); !ok {
		return "", fmt.Errorf("arguments must be an object")
	}
	if _, err = d.Token(); err != io.EOF {
		return "", fmt.Errorf("trailing arguments")
	}
	return fingerprint(v), nil
}
func replayIdentity(s p.State, intent toolIntent) string {
	return fingerprint(struct {
		Engine, Environment, Catalog, Name, Schema string
		MCP                                        *p.MCPTool
	}{p.EngineVersion, fingerprint(s.Manifest), catalogFingerprint(s), intent.Name, intent.SchemaHash, intent.MCP})
}
func (e *Engine) forkSourceLocked() error {
	if e.closed || e.state.SessionID == "" || e.state.RunID == "" || p.Active(e.state.Status) || e.state.Status == "completing" {
		return fmt.Errorf("fork source must be a stopped run")
	}
	if e.done != nil {
		select {
		case <-e.done:
		default:
			return fmt.Errorf("source is still closing")
		}
	}
	if e.state.Diagnostic != nil || e.state.Fork != nil {
		return fmt.Errorf("fork source must be an ordinary Kun run")
	}
	for _, status := range e.state.Actions {
		if status == "dispatched" || status == "outcome_unknown" {
			return fmt.Errorf("source has an unknown action outcome")
		}
	}
	return nil
}
func (e *Engine) ForkPoints(offset, limit int) (p.ForkPoints, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := p.ForkPoints{Items: []p.ForkPoint{}}
	if offset < 0 || offset > 1000000 || limit < 1 || limit > 50 {
		return out, fmt.Errorf("invalid fork point page")
	}
	if err := e.forkSourceLocked(); err != nil {
		return out, err
	}
	out.Selection = p.ForkSelection{SourceRunID: e.state.RunID, ExpectedRevision: e.state.Revision, WorkerEpoch: e.epoch}
	if err := e.j.db.QueryRow("SELECT coalesce(max(seq),0) FROM events").Scan(&out.Selection.Through); err != nil {
		return out, err
	}
	rows, err := e.j.db.Query(`SELECT seq, json_extract(snapshot,'$.phase'), json_extract(snapshot,'$.step'), coalesce(json_array_length(snapshot,'$.pending'),0) FROM events WHERE json_extract(snapshot,'$.runId')=? AND json_extract(snapshot,'$.manifest.engineVersion')=? AND json_extract(snapshot,'$.modules.capability.phase')='ready' AND json_extract(snapshot,'$.phase') IN ('before_model','after_model','before_tool','after_tool','approval') AND coalesce(json_array_length(snapshot,'$.queuedControls'),0)=0 AND NOT EXISTS (SELECT 1 FROM json_each(snapshot,'$.actions') WHERE value IN ('dispatched','outcome_unknown')) ORDER BY seq DESC LIMIT ? OFFSET ?`, e.state.RunID, p.EngineVersion, limit+1, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item p.ForkPoint
		if err = rows.Scan(&item.Sequence, &item.Phase, &item.Step, &item.Pending); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.HasMore = len(out.Items) > limit
	if out.HasMore {
		out.Items = out.Items[:limit]
	}
	out.NextOffset = offset + len(out.Items)
	return out, nil
}
func (e *Engine) ExportFork(in p.ForkExport) (p.ForkBundle, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out p.ForkBundle
	mode := in.Mode
	if mode == "" {
		mode = "hybrid"
	}
	if mode != "hybrid" && mode != "live" {
		return out, fmt.Errorf("unsupported fork mode")
	}
	if err := e.forkSourceLocked(); err != nil {
		return out, err
	}
	q := in.Selection
	if q.SourceRunID != e.state.RunID || q.WorkerEpoch != e.epoch || q.ExpectedRevision != e.state.Revision || q.Sequence < 1 || q.Through < q.Sequence {
		return out, fmt.Errorf("stale or invalid fork selection")
	}
	var last, size int64
	if err := e.j.db.QueryRow("SELECT coalesce(max(seq),0) FROM events").Scan(&last); err != nil {
		return out, err
	}
	if q.Through != last {
		return out, fmt.Errorf("fork recording cursor changed")
	}
	if err := e.j.db.QueryRow("SELECT length(snapshot) FROM events WHERE seq=?", q.Sequence).Scan(&size); err != nil {
		return out, err
	}
	if size > 2<<20 {
		return out, fmt.Errorf("fork snapshot exceeds 2 MiB")
	}
	snap, err := e.j.snapshot(q.Sequence)
	if err != nil {
		return out, err
	}
	s := snap.State
	if s.SessionID != e.state.SessionID || s.RunID != q.SourceRunID || !checkpointSafe(s) || s.Manifest.EngineVersion != p.EngineVersion || !compatibleHarness(s) {
		return out, fmt.Errorf("not a compatible safe fork boundary")
	}
	out = p.ForkBundle{Schema: p.ForkSchema, Mode: mode, Selection: q, State: s, CatalogHash: catalogFingerprint(s), EnvironmentHash: fingerprint(s.Manifest), Records: []p.ReplayRecord{}}
	if mode == "live" {
		out.ContentHash = p.ForkHash(out)
		if len(p.JSON(out)) > p.MaxForkBytes {
			return out, fmt.Errorf("fork bundle exceeds 4 MiB")
		}
		return out, nil
	}
	catalog, err := e.modules.capability.Build(s)
	if err != nil {
		return out, err
	}
	var count, total, largest int64
	const filter = ` FROM events WHERE seq>? AND seq<=? AND json_extract(data,'$.runId')=? AND json_extract(data,'$.type')='kun/tool.completed'`
	if err = e.j.db.QueryRow(`SELECT count(*),coalesce(sum(length(data)),0),coalesce(max(length(data)),0)`+filter, q.Sequence, q.Through, q.SourceRunID).Scan(&count, &total, &largest); err != nil {
		return out, err
	}
	if count > p.MaxReplayRecords || total > 2<<20 || largest > 1<<20 {
		return out, fmt.Errorf("replay exceeds 128 records, 2 MiB total or 1 MiB per record")
	}
	rows, err := e.j.db.Query(`SELECT seq,data`+filter+` ORDER BY seq`, q.Sequence, q.Through, q.SourceRunID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var raw []byte
		var ev p.Event
		if err = rows.Scan(&seq, &raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &ev); err != nil {
			return out, err
		}
		var data struct {
			Call    p.ToolCall `json:"call"`
			Output  *string    `json:"output"`
			IsError bool       `json:"isError"`
			Status  string     `json:"status"`
		}
		if err = json.Unmarshal(ev.Data, &data); err != nil {
			return out, err
		}
		if ev.SessionID != s.SessionID || data.Output == nil || (data.Status != "succeeded" && data.Status != "failed") || data.IsError != (data.Status == "failed") {
			return out, fmt.Errorf("recording contains a non-replayable outcome at %d", seq)
		}
		intent, err := e.modules.action.Validate(data.Call, catalog)
		if err != nil {
			return out, err
		}
		args, err := replayArguments(data.Call.Function.Arguments)
		if err != nil {
			return out, err
		}
		out.Records = append(out.Records, p.ReplayRecord{Sequence: seq, Tool: intent.Name, SchemaHash: intent.SchemaHash, ArgumentsHash: args, IdentityHash: replayIdentity(s, intent), Output: *data.Output, IsError: data.IsError, Status: data.Status})
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.ContentHash = p.ForkHash(out)
	if len(p.JSON(out)) > p.MaxForkBytes {
		return out, fmt.Errorf("fork bundle exceeds 4 MiB")
	}
	return out, nil
}
func (e *Engine) forkLocked(in p.Start, hash string) (p.State, error) {
	f := in.Fork
	b := f.Bundle
	s := clone(b.State)
	o := f.Origin
	live := o.Mode == "live"
	if e.state.SessionID != "" || in.Resume != nil || in.Diagnostic != nil || (!live && (len(in.MCP) > 0 || len(in.Skills) > 0)) {
		return p.State{}, fmt.Errorf("fork requires an empty session; Hybrid prohibits live tool configuration")
	}
	if b.Schema != p.ForkSchema || b.Mode != o.Mode || (b.Mode != "hybrid" && b.Mode != "live") || b.ContentHash != p.ForkHash(b) || len(p.JSON(b)) > p.MaxForkBytes || len(b.Records) > p.MaxReplayRecords || (live && len(b.Records) > 0) || !checkpointSafe(s) || s.Manifest.EngineVersion != p.EngineVersion || !compatibleHarness(s) || s.Manifest.ConfigHash != fingerprint(s.Config) || b.CatalogHash != catalogFingerprint(s) || b.EnvironmentHash != fingerprint(s.Manifest) {
		return p.State{}, fmt.Errorf("invalid or incompatible fork recording")
	}
	if o.PreviewID == "" || o.SessionID != s.SessionID || o.SessionID == in.SessionID || o.RunID != s.RunID || o.BundleHash != b.ContentHash || o.Sequence != b.Selection.Sequence || o.Through != b.Selection.Through || o.RunID != b.Selection.SourceRunID || fingerprint(in.Config) != fingerprint(s.Config) || in.ApprovalPolicy != s.ApprovalPolicy || utf8.RuneCountInString(f.Instruction) > 16000 {
		return p.State{}, fmt.Errorf("fork source or configuration mismatch")
	}
	if live {
		if !f.ConfirmLive {
			return p.State{}, fmt.Errorf("Live fork requires explicit execution confirmation")
		}
		manifest := e.manifest(in)
		manifest.HarnessHash = fingerprint(s.Harness)
		if fingerprint(manifest) != fingerprint(s.Manifest) {
			return p.State{}, fmt.Errorf("Live fork workspace, skills, authorization or runtime configuration changed")
		}
		next := "model"
		if len(s.Pending) > 0 {
			next = "tool"
		} else if f.Instruction == "" && e.modules.policy.Next(s) == "complete" {
			next = "complete"
		}
		if reason := budgetReason(s, next); reason != "" {
			return p.State{}, budgetError(reason)
		}
	}
	last := b.Selection.Sequence
	for _, r := range b.Records {
		if r.Sequence <= last || r.Sequence > b.Selection.Through || r.Tool == "" || r.SchemaHash == "" || r.ArgumentsHash == "" || r.IdentityHash == "" || (r.Status != "succeeded" && r.Status != "failed") || r.IsError != (r.Status == "failed") {
			return p.State{}, fmt.Errorf("invalid replay tape")
		}
		last = r.Sequence
	}
	previous := e.state
	s.Fork = &p.ForkState{Origin: o, InheritedStep: s.Step, InheritedBudget: s.Budget, ReplayTotal: len(b.Records)}
	s.SessionID, s.RunID, s.Revision, s.Status, s.Error = in.SessionID, in.RunID, 1, "running", ""
	s.ResumedFrom = nil
	s.Approval = nil // A source decision never authorizes a new external call.
	s.Queued = nil
	s.Debug.Pause = nil
	if s.Actions == nil {
		s.Actions = map[string]string{}
	}
	if s.Debug.Hits == nil {
		s.Debug.Hits = map[string]int{}
	}
	preamble := "This is a RunDesk Hybrid experiment. Tool results are recorded simulations, not current observations or executed actions. Report that distinction. A replay miss stops the run; never claim a simulated write happened."
	if live {
		s.Phase = "restoring"
		s.ToolDefinitions = nil
		for _, definition := range toolDefinitions(in.Config.AllowWrite) {
			s.ToolDefinitions = append(s.ToolDefinitions, p.JSON(definition))
		}
		s.MCPTools = nil
		s.MCP = nil
		for _, spec := range in.MCP {
			transport := "stdio"
			if spec.URL != "" {
				transport = "streamable-http"
			}
			s.MCP = append(s.MCP, p.MCPStatus{Name: spec.Name, Revision: spec.Revision, Transport: transport, Status: "pending"})
		}
		capability := s.Modules["capability"]
		capability.Phase = "pending"
		capability.Data = p.JSON(map[string]any{})
		s.Modules["capability"] = capability
		preamble = "This is a RunDesk Live fork. Earlier context and tool results are historical observations, not the current external world. No files or external systems were rolled back. Future tools execute against the CURRENT workspace and services, using current authorization; effects from the source may be repeated. Pending tool arguments are unchanged. Save new deliverables under outputs/" + in.SessionID + "/ unless the task explicitly addresses an existing file. Report new results separately from inherited observations."
	} else {
		for n := range s.MCP {
			s.MCP[n].Status = "recorded"
			s.MCP[n].Error = ""
		}
	}
	if s.Harness.ID == "plan-act-v1" && s.Modules["planning"].Phase == "ready" {
		var plan planData
		_ = json.Unmarshal(s.Modules["planning"].Data, &plan)
		plan.MessageCount++
		module := s.Modules["planning"]
		module.Data = p.JSON(plan)
		s.Modules["planning"] = module
	}
	s.Messages = append([]p.Message{{Role: "system", Content: preamble}}, s.Messages...)
	if strings.TrimSpace(f.Instruction) != "" {
		s.Queued = []p.Control{{RequestID: in.RunID + "-fork-instruction", RunID: in.RunID, ExpectedRevision: 1, Operation: "steer", Text: f.Instruction}}
	}
	e.state = s
	e.forkTape = nil
	if !live {
		e.forkTape = &b
	}
	e.expectedCatalog = b.CatalogHash
	if _, err := e.j.commit(s, "kun/run.started", map[string]any{"fork": s.Fork, "instruction": f.Instruction, "liveConfirmed": live && f.ConfirmLive}, in.RunID, hash, nil); err != nil {
		e.state = previous
		e.forkTape = nil
		e.expectedCatalog = ""
		return p.State{}, err
	}
	e.launchLocked(in)
	return clone(e.state), nil
}

// Called after schema validation, with the engine lock held, before any live
// approval, dispatch or gateway. Only the next position is eligible for reuse.
func (e *Engine) replayTool(call p.ToolCall, intent toolIntent) error {
	f := e.state.Fork
	miss := func(reason string) error {
		e.state.Budget.StopReason = "replay_miss"
		if err := e.record("kun/replay.miss", map[string]any{"mode": "hybrid", "reason": reason, "position": f.ReplayCursor, "tool": call.Function.Name, "executed": false}); err != nil {
			return err
		}
		return fmt.Errorf("replay_miss: %s at tool position %d", reason, f.ReplayCursor+1)
	}
	if e.forkTape == nil || f.ReplayCursor >= len(e.forkTape.Records) {
		return miss("recording_exhausted")
	}
	r := e.forkTape.Records[f.ReplayCursor]
	args, err := replayArguments(call.Function.Arguments)
	if err != nil {
		return miss("invalid_arguments")
	}
	if intent.Name != r.Tool || intent.SchemaHash != r.SchemaHash || args != r.ArgumentsHash || replayIdentity(e.state, intent) != r.IdentityHash {
		return miss("recording_key_mismatch")
	}
	e.state.Budget.ToolCalls++
	evidence := &p.ReplayEvidence{Mode: "recorded", SourceSequence: r.Sequence, Position: f.ReplayCursor, BundleHash: e.forkTape.ContentHash, RecordedStatus: r.Status, Executed: false}
	f.ReplayCursor++
	return e.finishTool(call, intent, toolResult{Output: r.Output, IsError: r.IsError, Replay: evidence}, "replayed", 0)
}

func forkExecutionMode(s p.State) string {
	if s.Fork != nil {
		return s.Fork.Origin.Mode
	}
	return "ordinary"
}
