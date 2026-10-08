// Package debugapi defines the read-only host debug contract shared by HTTP
// clients and the stdio MCP proxy. Host event IDs are not Kun sequences.
package debugapi

import (
	"fmt"
	"net/url"
	"strconv"
	"unicode/utf8"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

const MaxResponseBytes = 4 << 20

var Kinds = []string{"overview", "run", "events", "event", "context", "tools", "budget", "modules", "breakpoints", "actions", "snapshot", "evidence", "diff", "runs", "steps", "step", "issues", "statistics"}

type Query struct {
	Kind         string `json:"kind"`
	Sequence     int64  `json:"sequence,omitempty"`
	FromSequence int64  `json:"fromSequence,omitempty"`
	After        int64  `json:"after,omitempty"`
	Through      *int64 `json:"through,omitempty"`
	EventID      int64  `json:"eventId,omitempty"`
	Offset       int    `json:"offset,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	RunID        string `json:"runId,omitempty"`
	Type         string `json:"type,omitempty"`
	Status       string `json:"status,omitempty"`
	Search       string `json:"query,omitempty"`
	StepID       string `json:"stepId,omitempty"`
}

// Fields lists the only accepted fields per query, including explicitly zero
// values. This prevents misspelled or inapplicable selectors from being ignored.
func Fields(kind string) []string {
	switch kind {
	case "overview":
		return []string{}
	case "events":
		return []string{"after", "through", "limit"}
	case "event":
		return []string{"eventId", "through", "offset", "limit"}
	case "runs":
		return []string{"through", "offset", "limit"}
	case "steps", "issues":
		return []string{"through", "runId", "type", "status", "query", "offset", "limit"}
	case "step":
		return []string{"through", "runId", "stepId"}
	case "statistics":
		return []string{"through", "runId", "type", "status", "query"}
	case "diff":
		return []string{"sequence", "fromSequence"}
	case "run", "context", "tools", "budget", "modules", "breakpoints", "actions", "snapshot", "evidence":
		return []string{"sequence"}
	}
	return nil
}

func (q Query) Validate() error {
	if Fields(q.Kind) == nil {
		return fmt.Errorf("unsupported debug query")
	}
	if q.Sequence < 0 || q.FromSequence < 0 || q.After < 0 || q.EventID < 0 || q.Offset < 0 || q.Limit < 0 || q.Through != nil && *q.Through < 0 {
		return fmt.Errorf("negative debug selector")
	}
	allowed := map[string]bool{}
	for _, field := range Fields(q.Kind) {
		allowed[field] = true
	}
	present := map[string]bool{"sequence": q.Sequence != 0, "fromSequence": q.FromSequence != 0, "after": q.After != 0, "through": q.Through != nil, "eventId": q.EventID != 0, "offset": q.Offset != 0, "limit": q.Limit != 0, "runId": q.RunID != "", "type": q.Type != "", "status": q.Status != "", "query": q.Search != "", "stepId": q.StepID != ""}
	for field, yes := range present {
		if yes && !allowed[field] {
			return fmt.Errorf("inapplicable debug selector")
		}
	}
	for field, value := range map[string]string{"runId": q.RunID, "type": q.Type, "status": q.Status, "query": q.Search, "stepId": q.StepID} {
		limit := 256
		if field == "query" {
			limit = 1000
		}
		if utf8.RuneCountInString(value) > limit {
			return fmt.Errorf("debug text selector too long")
		}
	}
	switch q.Kind {
	case "runs", "steps", "step", "issues", "statistics":
		if q.Limit > 50 {
			return fmt.Errorf("at most 50 projected records per page")
		}
		if q.Kind == "step" && q.StepID == "" {
			return fmt.Errorf("stepId required")
		}
		return nil
	case "overview", "events", "event":
		if q.Sequence != 0 || q.FromSequence != 0 {
			return fmt.Errorf("host queries do not accept Kun sequences")
		}
		if q.Kind == "events" && (q.Limit > 200 || q.Through != nil && q.After > *q.Through) {
			return fmt.Errorf("invalid event page")
		}
		if q.Kind == "event" && (q.EventID == 0 || q.Limit > 16000) {
			return fmt.Errorf("event requires a positive host eventId and at most 16000 characters")
		}
		return nil
	case "snapshot":
		if q.Sequence <= 0 || q.FromSequence != 0 {
			return fmt.Errorf("snapshot requires a fixed positive Kun sequence")
		}
		return nil
	default:
		return (p.DebugQuery{Kind: q.Kind, Sequence: q.Sequence, FromSequence: q.FromSequence}).Validate()
	}
}

func Parse(values url.Values) (Query, error) {
	q := Query{Kind: values.Get("kind")}
	allowed := map[string]bool{"kind": true}
	for _, name := range Fields(q.Kind) {
		allowed[name] = true
	}
	for name, items := range values {
		if !allowed[name] || len(items) != 1 {
			return q, fmt.Errorf("unknown, repeated or inapplicable debug parameter")
		}
		if name == "kind" {
			continue
		}
		switch name {
		case "runId":
			q.RunID = items[0]
			continue
		case "type":
			q.Type = items[0]
			continue
		case "status":
			q.Status = items[0]
			continue
		case "query":
			q.Search = items[0]
			continue
		case "stepId":
			q.StepID = items[0]
			continue
		}
		n, err := strconv.ParseInt(items[0], 10, 64)
		if err != nil || n < 0 || name == "limit" && n == 0 {
			return q, fmt.Errorf("invalid debug integer")
		}
		switch name {
		case "sequence":
			q.Sequence = n
		case "fromSequence":
			q.FromSequence = n
		case "after":
			q.After = n
		case "through":
			q.Through = &n
		case "eventId":
			q.EventID = n
		case "offset", "limit":
			if n > 1<<31-1 {
				return q, fmt.Errorf("debug integer too large")
			}
			if name == "offset" {
				q.Offset = int(n)
			} else {
				q.Limit = int(n)
			}
		}
	}
	return q, q.Validate()
}

type Capability struct {
	Supported bool   `json:"supported"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type Capabilities struct {
	SessionID       string                `json:"sessionId"`
	Backend         string                `json:"backend"`
	ReadOnly        bool                  `json:"readOnly"`
	Queries         map[string]Capability `json:"queries"`
	HostEventCursor string                `json:"hostEventCursor"`
	WorkerSequence  string                `json:"workerSequence"`
}
type Result struct {
	SessionID string `json:"sessionId"`
	Backend   string `json:"backend"`
	Source    string `json:"source"`
	Through   *int64 `json:"through,omitempty"`
	Kind      string `json:"kind"`
	RunID     string `json:"runId,omitempty"`
	Revision  *int64 `json:"revision,omitempty"`
	Sequence  *int64 `json:"sequence,omitempty"`
	Data      any    `json:"data"`
}

func Projection(kind string) bool {
	return kind == "runs" || kind == "steps" || kind == "step" || kind == "issues" || kind == "statistics"
}
