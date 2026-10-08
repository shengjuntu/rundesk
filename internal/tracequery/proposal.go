package tracequery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// A proposal is a historical suggestion, never an executable control command.
// Citations are checked for identity and scope, not for the truth of the claim.
type ProposalInput struct {
	Kind        string  `json:"kind"`
	Reason      string  `json:"reason"`
	Text        string  `json:"text"`
	EvidenceIDs []int64 `json:"evidenceIds"`
}
type Proposal struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	Through   int64  `json:"through"`
	ProposalInput
	Note string `json:"note"`
}

func proposalSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "reason", "text", "evidenceIds"}, "properties": map[string]any{
		"kind":        map[string]any{"type": "string", "enum": []string{"inspect", "steer", "configuration"}},
		"reason":      map[string]any{"type": "string", "minLength": 1, "maxLength": 2000},
		"text":        map[string]any{"type": "string", "minLength": 1, "maxLength": 4000},
		"evidenceIds": map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "uniqueItems": true, "items": map[string]any{"type": "integer", "minimum": 1}},
	}}
}
func (r *Reader) propose(ctx context.Context, runID string, p *ProposalInput) (Proposal, error) {
	out := Proposal{}
	if p == nil || runID == "" || utf8.RuneCountInString(runID) > 256 {
		return out, errors.New("runId and proposal required")
	}
	if p.Kind != "inspect" && p.Kind != "steer" && p.Kind != "configuration" {
		return out, errors.New("unknown suggestion kind")
	}
	if strings.TrimSpace(p.Reason) == "" || strings.TrimSpace(p.Text) == "" || utf8.RuneCountInString(p.Reason) > 2000 || utf8.RuneCountInString(p.Text) > 4000 || len(p.EvidenceIDs) < 1 || len(p.EvidenceIDs) > 16 {
		return out, errors.New("suggestion text or evidence exceeds limits")
	}
	owned := map[int64]bool{}
	for _, run := range r.runs {
		if run.ID == runID {
			for _, id := range run.EventIDs {
				owned[id] = true
			}
		}
	}
	for _, step := range r.steps {
		if step.RunID == runID {
			for _, id := range step.EventIDs {
				owned[id] = true
			}
		}
	}
	seen := map[int64]bool{}
	for _, id := range p.EvidenceIDs {
		if id <= 0 || id > r.Through || !owned[id] || seen[id] {
			return out, errors.New("citation must be a distinct retained lifecycle event of the selected run")
		}
		var exists int
		if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM events WHERE session=? AND id=? AND id<=?", r.SessionID, id, r.Through).Scan(&exists); err != nil {
			return out, err
		}
		if exists != 1 {
			return out, errors.New("citation is unavailable")
		}
		seen[id] = true
	}
	out = Proposal{Status: "suggestion_only", SessionID: r.SessionID, RunID: runID, Through: r.Through, ProposalInput: *p, Note: "Historical evidence references verified; model claims are not validated. No changes applied. Recheck current state and use authorized controls separately."}
	b, _ := json.Marshal(out)
	out.ID = fmt.Sprintf("proposal-%x", sha256.Sum256(b))
	return out, nil
}

// Both the native trace MCP and Kun diagnostic tools use the same strict adapter.
func ParseArgs(name string, raw json.RawMessage) (Args, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var schema map[string]any
	for _, item := range Tools() {
		tool := item.(map[string]any)
		if tool["name"] == name {
			schema = tool["inputSchema"].(map[string]any)
			break
		}
	}
	if schema == nil {
		return Args{}, errors.New("unknown read-only trace tool")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Args{}, errors.New("arguments must be an object")
	}
	props := schema["properties"].(map[string]any)
	for key, value := range fields {
		if _, ok := props[key]; !ok || string(value) == "null" {
			return Args{}, errors.New("unknown, inapplicable or null argument")
		}
	}
	for _, key := range schema["required"].([]string) {
		if _, ok := fields[key]; !ok {
			return Args{}, errors.New("missing required argument")
		}
	}
	var a Args
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return Args{}, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Args{}, errors.New("one argument object required")
	}
	return a, nil
}
func (r *Reader) CallJSONContext(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	a, err := ParseArgs(name, raw)
	if err != nil {
		return nil, err
	}
	return r.CallContext(ctx, name, a)
}
