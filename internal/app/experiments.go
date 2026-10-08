package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	x "github.com/shengjuntu/rundesk/internal/experiment"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

type ExperimentInput struct {
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	Through   *int64 `json:"through,omitempty"`
	Title     string `json:"title"`
}
type ExperimentForkInput struct {
	Title              string   `json:"title"`
	ExpectedParentHash string   `json:"expectedParentHash"`
	Change             x.Change `json:"change"`
}
type ExperimentSummary struct {
	ID          string   `json:"id"`
	RootID      string   `json:"rootId"`
	ParentID    string   `json:"parentId,omitempty"`
	ContentHash string   `json:"contentHash"`
	Source      x.Source `json:"source"`
	Title       string   `json:"title"`
	CreatedAt   string   `json:"createdAt"`
	Mode        string   `json:"mode"`
	Depth       int      `json:"depth"`
	EventCount  int      `json:"eventCount"`
	PatchCount  int      `json:"patchCount"`
}

func experimentSummary(b x.Branch) ExperimentSummary {
	return ExperimentSummary{ID: b.ID, RootID: b.RootID, ParentID: b.ParentID, ContentHash: b.ContentHash, Source: b.Source, Title: b.Title, CreatedAt: b.CreatedAt, Mode: b.Mode, Depth: b.Depth, EventCount: b.EventCount, PatchCount: len(b.Patches)}
}
func experimentTitle(title string) error {
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > 120 {
		return failure(400, "invalid_experiment_title", "实验标题应为 1–120 个字符")
	}
	return nil
}
func (m *Manager) CreateExperiment(ctx context.Context, in ExperimentInput) (x.Branch, error) {
	var branch x.Branch
	if err := experimentTitle(in.Title); err != nil {
		return branch, err
	}
	if in.SessionID == "" || in.RunID == "" {
		return branch, failure(400, "invalid_experiment_source", "请选择一个来源轮次")
	}
	source, err := m.Session(in.SessionID)
	if err != nil {
		return branch, err
	}
	if source.RuntimeKind != "kun" {
		return branch, failure(409, "experiment_backend_unsupported", "首版离线记录实验仅支持 Kun；Codex 仍可只读检查")
	}
	page, err := m.Store.DebugEvents(source.ID, 0, in.Through, 1)
	if err != nil || page.Through == 0 {
		return branch, failure(400, "invalid_experiment_cursor", "来源上界无效或没有保留记录")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	events, err := m.Store.ExperimentEvents(ctx, source.ID, in.RunID, page.Through)
	if err != nil {
		return branch, failure(409, "experiment_capture_rejected", err.Error())
	}
	// The source title is display metadata repeated in lineage/list responses.
	// Bound it separately from the immutable event payload and identity fields.
	title := []rune(source.Title)
	pack := x.Bundle{Schema: x.Schema, Source: x.Source{SessionID: source.ID, RunID: in.RunID, Through: page.Through, WorkspaceID: source.WorkspaceID, InstanceID: source.InstanceID, Title: string(title[:min(len(title), 120)]), Backend: "kun"}, Events: []x.Event{}}
	workerEvents := 0
	for _, ev := range events {
		if strings.HasPrefix(ev.Method, "kun/") {
			var worker p.Event
			if json.Unmarshal(ev.Data, &worker) != nil || worker.SessionID != source.ID || worker.RunID != in.RunID || worker.Type != ev.Method || worker.Sequence < 1 {
				return branch, failure(409, "experiment_identity_missing", "Kun 记录缺少一致的会话、轮次或事件标识")
			}
			workerEvents++
		}
		raw, err := debugJSON(ev.Data)
		if err != nil {
			return branch, err
		}
		pack.Events = append(pack.Events, x.Event{ID: ev.ID, Time: ev.Time, Method: ev.Method, Data: raw})
	}
	if workerEvents == 0 {
		return branch, failure(409, "experiment_no_worker_records", "该范围没有 Kun worker 记录")
	}
	if len(p.JSON(pack)) > 20<<20 {
		return branch, failure(413, "experiment_too_large", "已脱敏记录包超过 20 MiB")
	}
	branch = x.NewRoot(store.ID(), in.Title, store.Now(), pack)
	err = m.Store.PutMany(store.Record{Kind: "experiment_bundle", ID: branch.ID, Value: pack}, store.Record{Kind: "experiment", ID: branch.ID, Value: branch})
	return branch, err
}
func (m *Manager) experiment(id string) (x.Branch, x.Bundle, error) {
	var b x.Branch
	var pack x.Bundle
	if err := m.Store.Get("experiment", id, &b); err != nil {
		return b, pack, failure(404, "experiment_not_found", "实验分支不存在")
	}
	if err := m.Store.Get("experiment_bundle", b.RootID, &pack); err != nil {
		return b, pack, failure(409, "experiment_recording_missing", "实验记录包缺失")
	}
	if err := x.Verify(b, pack); err != nil {
		return b, pack, failure(409, "experiment_integrity", err.Error())
	}
	return b, pack, nil
}
func (m *Manager) ForkExperiment(id string, in ExperimentForkInput) (x.Branch, error) {
	if err := experimentTitle(in.Title); err != nil {
		return x.Branch{}, err
	}
	parent, pack, err := m.experiment(id)
	if err != nil {
		return x.Branch{}, err
	}
	branch, err := x.Fork(parent, pack, store.ID(), in.Title, store.Now(), in.ExpectedParentHash, in.Change)
	if err != nil {
		return branch, failure(409, "experiment_branch_rejected", err.Error())
	}
	err = m.Store.Put("experiment", branch.ID, branch)
	return branch, err
}
func (m *Manager) experimentLineage(b x.Branch) ([]ExperimentSummary, error) {
	out := []ExperimentSummary{}
	seen := map[string]bool{}
	for {
		if seen[b.ID] || len(out) > x.MaxDepth {
			return nil, failure(409, "experiment_lineage_invalid", "实验谱系不完整或存在循环")
		}
		seen[b.ID] = true
		out = append(out, experimentSummary(b))
		if b.ParentID == "" {
			break
		}
		var parent x.Branch
		if err := m.Store.Get("experiment", b.ParentID, &parent); err != nil || parent.RootID != b.RootID || parent.ContentHash != b.ParentHash || x.Seal(parent).ContentHash != parent.ContentHash {
			return nil, failure(409, "experiment_lineage_invalid", "父分支缺失或指纹不匹配")
		}
		b = parent
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
func experimentQuery(r *http.Request, allowed ...string) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, failure(400, "invalid_experiment_query", "无效查询")
	}
	for key, values := range q {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
			}
		}
		if !found || len(values) != 1 {
			return nil, failure(400, "invalid_experiment_query", "未知或重复查询参数")
		}
	}
	return q, nil
}
func experimentPage(q url.Values, max int) (int, int, error) {
	offset, limit := 0, min(20, max)
	var err error
	if q.Has("offset") {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			return 0, 0, err
		}
	}
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			return 0, 0, err
		}
	}
	if offset < 0 || offset > 20<<20 || limit < 1 || limit > max {
		return 0, 0, fmt.Errorf("invalid page bounds")
	}
	return offset, limit, nil
}
func experimentPageError(err error) error {
	return failure(400, "invalid_experiment_page", err.Error())
}
func (s *Server) experimentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/experiments", func(w http.ResponseWriter, r *http.Request) {
		var in ExperimentInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.CreateExperiment(r.Context(), in)
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/experiments", func(w http.ResponseWriter, r *http.Request) {
		q, e := experimentQuery(r, "sessionId", "offset", "limit")
		if e != nil {
			respond(w, nil, e)
			return
		}
		offset, limit, e := experimentPage(q, 50)
		if e != nil {
			respond(w, nil, experimentPageError(e))
			return
		}
		rows, e := s.Manager.Store.Experiments(r.Context(), q.Get("sessionId"), offset, limit)
		if e != nil {
			respond(w, nil, e)
			return
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		items := []ExperimentSummary{}
		for _, raw := range rows {
			var b x.Branch
			if e = json.Unmarshal(raw, &b); e != nil {
				respond(w, nil, e)
				return
			}
			items = append(items, experimentSummary(b))
		}
		debugRespond(w, map[string]any{"items": items, "nextOffset": offset + len(items), "hasMore": more}, nil)
	})
	mux.HandleFunc("GET /api/experiments/{eid}", func(w http.ResponseWriter, r *http.Request) {
		if _, e := experimentQuery(r); e != nil {
			respond(w, nil, e)
			return
		}
		b, pack, e := s.Manager.experiment(r.PathValue("eid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		lineage, e := s.Manager.experimentLineage(b)
		_, counts := x.Views(b, pack)
		debugRespond(w, map[string]any{"branch": b, "counts": counts, "lineage": lineage, "invalidation": "all later host events are conservatively stale; no dependency graph or reexecution"}, e)
	})
	mux.HandleFunc("POST /api/experiments/{eid}/branches", func(w http.ResponseWriter, r *http.Request) {
		var in ExperimentForkInput
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Manager.ForkExperiment(r.PathValue("eid"), in)
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/experiments/{eid}/events", func(w http.ResponseWriter, r *http.Request) {
		q, e := experimentQuery(r, "offset", "limit")
		if e != nil {
			respond(w, nil, e)
			return
		}
		offset, limit, e := experimentPage(q, 50)
		if e != nil {
			respond(w, nil, experimentPageError(e))
			return
		}
		b, pack, e := s.Manager.experiment(r.PathValue("eid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		views, counts := x.Views(b, pack)
		if offset > len(views) {
			offset = len(views)
		}
		end := min(offset+limit, len(views))
		debugRespond(w, map[string]any{"items": views[offset:end], "total": len(views), "nextOffset": end, "hasMore": end < len(views), "counts": counts}, nil)
	})
	mux.HandleFunc("GET /api/experiments/{eid}/events/{eventId}", func(w http.ResponseWriter, r *http.Request) {
		q, e := experimentQuery(r, "offset", "limit")
		if e != nil {
			respond(w, nil, e)
			return
		}
		offset, limit, e := experimentPage(q, 16000)
		if !q.Has("limit") {
			limit = 4000
		}
		if e != nil {
			respond(w, nil, experimentPageError(e))
			return
		}
		id, e := strconv.ParseInt(r.PathValue("eventId"), 10, 64)
		if e != nil || id < 1 {
			respond(w, nil, failure(400, "invalid_experiment_event", "无效宿主事件 ID"))
			return
		}
		b, pack, e := s.Manager.experiment(r.PathValue("eid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		views, _ := x.Views(b, pack)
		for i, event := range pack.Events {
			if event.ID != id {
				continue
			}
			text := []rune(string(event.Data))
			if offset > len(text) {
				respond(w, nil, failure(400, "invalid_experiment_offset", "偏移超出记录长度"))
				return
			}
			end := min(offset+limit, len(text))
			debugRespond(w, map[string]any{"event": views[i], "originalChunk": string(text[offset:end]), "offset": offset, "nextOffset": end, "totalCharacters": len(text), "hasMore": end < len(text), "note": "original recording is unchanged; replacement is an unvalidated hypothetical tool output"}, nil)
			return
		}
		respond(w, nil, failure(404, "experiment_event_not_found", "事件不在此固定轮次记录包中"))
	})
	mux.HandleFunc("GET /api/experiments/{eid}/diff", func(w http.ResponseWriter, r *http.Request) {
		q, e := experimentQuery(r, "against", "offset", "limit")
		if e != nil {
			respond(w, nil, e)
			return
		}
		offset, limit, e := experimentPage(q, 16)
		if e != nil {
			respond(w, nil, experimentPageError(e))
			return
		}
		b, pack, e := s.Manager.experiment(r.PathValue("eid"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		against := q.Get("against")
		if against == "" {
			against = b.ParentID
			if against == "" {
				against = b.RootID
			}
		}
		var other x.Branch
		if e = s.Manager.Store.Get("experiment", against, &other); e != nil {
			respond(w, nil, failure(404, "experiment_not_found", "对比分支不存在"))
			return
		}
		changes, e := x.Diff(other, b, pack)
		if e != nil {
			respond(w, nil, failure(409, "experiment_compare_rejected", e.Error()))
			return
		}
		if offset > len(changes) {
			offset = len(changes)
		}
		end := min(offset+limit, len(changes))
		debugRespond(w, map[string]any{"before": experimentSummary(other), "after": experimentSummary(b), "items": changes[offset:end], "total": len(changes), "nextOffset": end, "hasMore": end < len(changes)}, nil)
	})
}
