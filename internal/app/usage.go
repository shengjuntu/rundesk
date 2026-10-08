package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"sort"
	"time"
)

var usageFields = []string{"inputTokens", "outputTokens", "totalTokens", "cachedInputTokens", "reasoningOutputTokens"}

type UsageRow struct {
	AppID        string            `json:"appId"`
	InstanceID   string            `json:"instanceId"`
	WorkspaceID  string            `json:"workspaceId"`
	Tokens       map[string]*int64 `json:"tokens"`
	Runs         int               `json:"runs"`
	ReportedRuns int               `json:"reportedRuns"`
	UnknownRuns  int               `json:"unknownRuns"`
	Statuses     map[string]int    `json:"statuses"`
	TokenEvents  int               `json:"tokenEvents"`
	Issues       map[string]int    `json:"issues"`
}
type UsageReport struct {
	Rows           []*UsageRow `json:"rows"`
	From           string      `json:"from"`
	To             string      `json:"to"`
	GeneratedAt    string      `json:"generatedAt"`
	ThroughEventID int64       `json:"throughEventId"`
	Basis          string      `json:"basis"`
	Demo           bool        `json:"demo"`
}
type usageWindow struct {
	from, to  time.Time
	app, work string
}
type usageCounters struct {
	values    map[string]int64
	owner     string
	anchored  bool
	snapshots int
}
type usageSession struct {
	current string
	fresh   bool
	turns   map[string]string
}
type usageRun struct {
	row                *UsageRow
	included, reported bool
	status             string
}

func usageGroup(s Session) string {
	return s.InstanceID + "\x00" + s.WorkspaceID + "\x00" + s.Source.AppID
}
func (m *Manager) Usage(ctx context.Context, f usageWindow) (UsageReport, error) {
	report := UsageReport{Rows: []*UsageRow{}, From: f.from.Format(time.RFC3339Nano), To: f.to.Format(time.RFC3339Nano), GeneratedAt: store.Now(), Basis: "retained-journal-observed-deltas", Demo: m.Demo}
	groups := map[string]*UsageRow{}
	sessions := map[string]*usageSession{}
	streams := map[string]*usageCounters{}
	runs := map[string]*usageRun{}
	count := 0
	inWindow := func(t time.Time) bool { return !t.Before(f.from) && t.Before(f.to) }
	err := m.Store.WalkUsage(ctx, func(ev store.Event, raw json.RawMessage) error {
		count++
		if count > 200000 {
			return failure(422, "usage_scan_limit", "Usage scan exceeds 200000 lifecycle events; no partial total is returned.")
		}
		report.ThroughEventID = ev.ID
		var s Session
		if e := json.Unmarshal(raw, &s); e != nil {
			return e
		}
		if s.InstanceID == "" {
			s.InstanceID = DefaultInstance
		}
		var p struct {
			RunID    string                     `json:"runId"`
			Status   string                     `json:"status"`
			ThreadID string                     `json:"threadId"`
			TurnID   string                     `json:"turnId"`
			Total    map[string]json.RawMessage `json:"total"`
		}
		if e := json.Unmarshal(ev.Data, &p); e != nil {
			return e
		}
		at, e := time.Parse(time.RFC3339Nano, ev.Time)
		if e != nil {
			return e
		}
		key := usageGroup(s)
		row := groups[key]
		if row == nil {
			row = &UsageRow{AppID: s.Source.AppID, InstanceID: s.InstanceID, WorkspaceID: s.WorkspaceID, Tokens: map[string]*int64{}, Statuses: map[string]int{}, Issues: map[string]int{}}
			for _, field := range usageFields {
				row.Tokens[field] = nil
			}
			groups[key] = row
		}
		ss := sessions[s.ID]
		if ss == nil {
			ss = &usageSession{turns: map[string]string{}}
			sessions[s.ID] = ss
		}
		issue := func(name string) {
			if inWindow(at) {
				row.Issues[name]++
			}
		}
		switch ev.Method {
		case "run/input":
			if ev.Direction != "internal" || p.RunID == "" {
				return nil
			}
			ss.current = s.ID + "/" + p.RunID
			if runs[ss.current] == nil {
				runs[ss.current] = &usageRun{row: row, included: inWindow(at), status: "unfinished"}
			}
		case "run/state":
			if ev.Direction != "internal" {
				return nil
			}
			if r := runs[s.ID+"/"+p.RunID]; r != nil {
				if at.Before(f.to) {
					r.status = p.Status
				}
			}
		case "thread/start":
			if ev.Direction == "out" {
				ss.fresh = true
			}
		case "thread/resume":
			if ev.Direction == "out" {
				ss.fresh = false
			}
		case "turn/started":
			if ev.Direction == "in" && p.TurnID != "" {
				ss.turns[p.TurnID] = ss.current
			}
		case "thread/tokenUsage/updated":
			if ev.Direction != "in" {
				return nil
			}
			if inWindow(at) {
				row.TokenEvents++
			}
			if p.ThreadID == "" || len(p.Total) == 0 {
				issue("missing_snapshot")
				return nil
			}
			// Native thread IDs are scoped by configuration identity. Never sum repeated snapshots.
			streamKey := s.InstanceID + "/" + p.ThreadID
			c := streams[streamKey]
			if c == nil {
				c = &usageCounters{values: map[string]int64{}, owner: key, anchored: ss.fresh}
				streams[streamKey] = c
				if !c.anchored {
					issue("unknown_baseline")
				}
			}
			foreign := c.owner != key
			if foreign {
				issue("ambiguous_owner")
			}
			firstSnapshot := c.snapshots == 0
			c.snapshots++
			known := false
			for _, field := range usageFields {
				raw, ok := p.Total[field]
				if !ok || string(raw) == "null" {
					if field == "totalTokens" {
						issue("missing_total")
					}
					continue
				}
				var n int64
				if json.Unmarshal(raw, &n) != nil || n < 0 || n > 9007199254740991 {
					issue("invalid_counter")
					continue
				}
				old, seen := c.values[field]
				if seen && n < old {
					issue("counter_regression")
					continue
				}
				c.values[field] = n
				if foreign {
					continue
				}
				if !seen && !(firstSnapshot && c.anchored) {
					if c.anchored {
						issue("missing_field_baseline")
					}
					continue
				}
				delta := n - old
				if inWindow(at) {
					if row.Tokens[field] == nil {
						v := int64(0)
						row.Tokens[field] = &v
					}
					if *row.Tokens[field] > 9007199254740991-delta {
						return fmt.Errorf("usage counter overflow")
					}
					*row.Tokens[field] += delta
				}
				if field == "totalTokens" {
					known = true
				}
			}
			runKey := ss.current
			if p.TurnID != "" {
				if mapped := ss.turns[p.TurnID]; mapped != "" {
					runKey = mapped
				} else {
					runKey = ""
					issue("unmatched_turn")
				}
			}
			if r := runs[runKey]; r != nil && known && at.Before(f.to) {
				r.reported = true
			}
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	for _, run := range runs {
		if !run.included {
			continue
		}
		run.row.Runs++
		run.row.Statuses[run.status]++
		if run.reported {
			run.row.ReportedRuns++
		} else {
			run.row.UnknownRuns++
		}
	}
	for _, row := range groups {
		if (f.app != "" && row.AppID != f.app) || (f.work != "" && row.WorkspaceID != f.work) {
			continue
		}
		if row.Runs == 0 && row.TokenEvents == 0 {
			continue
		}
		report.Rows = append(report.Rows, row)
	}
	sort.Slice(report.Rows, func(a, b int) bool {
		x, y := report.Rows[a], report.Rows[b]
		return x.AppID+"/"+x.InstanceID+"/"+x.WorkspaceID < y.AppID+"/"+y.InstanceID+"/"+y.WorkspaceID
	})
	return report, nil
}
func (s *Server) usageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/usage", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		to := time.Now().UTC()
		from := to.AddDate(0, 0, -30)
		var e error
		if q.Get("to") != "" {
			to, e = time.Parse(time.RFC3339Nano, q.Get("to"))
			if e != nil {
				writeErr(w, 400, failure(400, "invalid_usage_range", "to requires RFC3339"))
				return
			}
		}
		if q.Get("from") != "" {
			from, e = time.Parse(time.RFC3339Nano, q.Get("from"))
			if e != nil {
				writeErr(w, 400, failure(400, "invalid_usage_range", "from requires RFC3339"))
				return
			}
		} else {
			from = to.AddDate(0, 0, -30)
		}
		if !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
			writeErr(w, 400, failure(400, "invalid_usage_range", "Range must be positive and at most 366 days"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, e := s.Manager.Usage(ctx, usageWindow{from: from, to: to, app: q.Get("appId"), work: q.Get("workspaceId")})
		respond(w, v, e)
	})
}
