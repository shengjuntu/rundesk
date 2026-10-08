package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/shengjuntu/rundesk/internal/tracequery"
	"net/http"
	"net/url"
	"path/filepath"

	kc "github.com/shengjuntu/rundesk/internal/adapters/kun"
	d "github.com/shengjuntu/rundesk/internal/debugapi"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/redaction"
)

// DebugService is read-only. HTTP authentication/ownership gates run before it.
// The MCP proxy uses those same routes; it has no database or worker access.
type DebugService struct{ manager *Manager }

func (s DebugService) Capabilities(sid string) (d.Capabilities, error) {
	session, err := s.manager.Session(sid)
	if err != nil {
		return d.Capabilities{}, err
	}
	backend := runtimeKind(session.RuntimeKind)
	out := d.Capabilities{SessionID: sid, Backend: backend, ReadOnly: true, Queries: map[string]d.Capability{}, HostEventCursor: "host event id; retain through from the first page, including zero", WorkerSequence: "Kun sequence only; never a host event id"}
	online := false
	if backend == "kun" {
		if c, e := s.manager.kunClient(sid); e == nil {
			select {
			case <-c.Done():
			default:
				online = true
			}
		}
	}
	for _, kind := range d.Kinds {
		v := d.Capability{Supported: true, Available: true}
		switch kind {
		case "overview", "events", "event", "runs", "steps", "step", "issues", "statistics":
		case "run":
			if backend != "kun" {
				v.Reason = "host session overview only; no internal Codex state revision or snapshot"
				break
			}
			fallthrough
		default:
			if backend != "kun" {
				v = d.Capability{Reason: "Codex adapter does not expose this internal debug capability"}
			} else if !online {
				v.Available = false
				v.Reason = "Kun worker is offline; inspection will not start or recover it"
			}
		}
		out.Queries[kind] = v
	}
	return out, nil
}

func (s DebugService) Query(ctx context.Context, sid string, q d.Query) (d.Result, error) {
	out := d.Result{SessionID: sid, Kind: q.Kind}
	if err := q.Validate(); err != nil {
		return out, failure(400, "invalid_debug_query", err.Error())
	}
	session, err := s.manager.Session(sid)
	if err != nil {
		return out, err
	}
	out.Backend = runtimeKind(session.RuntimeKind)
	if d.Projection(q.Kind) {
		page, e := s.manager.Store.DebugEvents(sid, 0, q.Through, 1)
		if e != nil {
			return out, failure(400, "invalid_debug_cursor", e.Error())
		}
		reader, e := tracequery.OpenContext(ctx, filepath.Join(s.manager.Data, "state.db"), sid, page.Through)
		if e != nil {
			return out, e
		}
		defer reader.Close()
		methods := map[string]string{"runs": "trace_list_runs", "steps": "trace_find_steps", "step": "trace_get_step", "issues": "trace_find_issues", "statistics": "trace_statistics"}
		value, e := reader.CallContext(ctx, methods[q.Kind], tracequery.Args{RunID: q.RunID, Type: q.Type, Status: q.Status, Query: q.Search, StepID: q.StepID, Offset: q.Offset, Limit: q.Limit})
		if e != nil {
			if errors.Is(e, tracequery.ErrProjectionLimit) {
				return out, failure(413, "debug_projection_too_large", e.Error())
			}
			return out, failure(409, "debug_projection_rejected", e.Error())
		}
		out.Source = "host_projection"
		out.Through = &page.Through
		out.RunID = q.RunID
		out.Data = value
		return out, nil
	}

	if q.Kind == "overview" || q.Kind == "run" && out.Backend != "kun" {
		if q.Sequence != 0 {
			return out, failure(409, "debug_unsupported", "Codex 没有内部状态快照；sequence 不适用于宿主运行概况")
		}
		out.Source = "host_session"
		out.RunID = session.RunID
		out.Data = session
		return out, nil
	}
	switch q.Kind {
	case "events":
		limit := q.Limit
		if limit == 0 {
			limit = 50
		}
		page, e := s.manager.Store.DebugEvents(sid, q.After, q.Through, limit)
		if e != nil {
			return out, failure(400, "invalid_debug_cursor", e.Error())
		}
		out.Source = "host_journal"
		out.Data = page
		return out, nil
	case "event":
		if q.Through != nil {
			if _, e := s.manager.Store.DebugEvents(sid, 0, q.Through, 1); e != nil {
				return out, failure(400, "invalid_debug_cursor", e.Error())
			}
			if q.EventID > *q.Through {
				return out, failure(404, "debug_event_not_found", "事件不在选定宿主历史范围内")
			}
			out.Through = q.Through
		}
		n, e := s.manager.Store.DebugEventSize(sid, q.EventID)
		if errors.Is(e, sql.ErrNoRows) {
			return out, failure(404, "debug_event_not_found", "此会话没有该宿主事件")
		}
		if e != nil {
			return out, e
		}
		if n > 8<<20 {
			return out, failure(413, "debug_event_too_large", "单个事件超过只读调试解析上限 8 MiB")
		}
		event, e := s.manager.Store.Event(sid, q.EventID)
		if e != nil {
			return out, e
		}
		data, e := debugJSON(event)
		if e != nil {
			return out, e
		}
		runes := []rune(string(data))
		offset := q.Offset
		if offset > len(runes) {
			return out, failure(400, "invalid_debug_offset", "偏移超出已脱敏事件长度")
		}
		limit := q.Limit
		if limit == 0 {
			limit = 4000
		}
		end := offset + limit
		if end > len(runes) {
			end = len(runes)
		}
		out.Source = "host_journal"
		out.Data = map[string]any{"eventId": q.EventID, "text": string(runes[offset:end]), "offset": offset, "nextOffset": end, "totalCharacters": len(runes), "hasMore": end < len(runes), "encoding": "redacted JSON; Unicode code point offsets"}
		return out, nil
	}
	if out.Backend != "kun" {
		return out, failure(409, "debug_unsupported", "Codex 尚未暴露此内部调试能力；请查询 capabilities、run 或宿主 events")
	}
	out.Source = "kun_worker"
	if q.Kind == "snapshot" {
		c, e := s.manager.kunClient(sid)
		if e != nil {
			return out, e
		}
		var snap p.Snapshot
		e = c.Call(ctx, "snapshot", map[string]int64{"sequence": q.Sequence}, &snap)
		if e != nil {
			return out, debugWorkerError(e)
		}
		out.Sequence = &q.Sequence
		out.Revision = &snap.State.Revision
		out.RunID = snap.State.RunID
		out.Data = snap
		return out, nil
	}
	result, e := s.manager.kunDebugQuery(ctx, sid, p.DebugQuery{Kind: q.Kind, Sequence: q.Sequence, FromSequence: q.FromSequence})
	if e != nil {
		return out, e
	}
	out.RunID = result.RunID
	out.Revision = &result.Revision
	out.Sequence = &result.Sequence
	out.Data = result.Data
	return out, nil
}

func (m *Manager) kunDebugQuery(ctx context.Context, sid string, q p.DebugQuery) (p.DebugResult, error) {
	var result p.DebugResult
	if e := q.Validate(); e != nil {
		return result, failure(400, "invalid_debug_query", e.Error())
	}
	c, e := m.kunClient(sid)
	if e != nil {
		return result, e
	}
	e = c.Call(ctx, "query", q, &result)
	return result, debugWorkerError(e)
}
func debugWorkerError(err error) error {
	var remote *kc.RemoteError
	if errors.As(err, &remote) {
		return failure(409, "kun_query_rejected", remote.Error())
	}
	return err
}

// Preserve JSON integer precision and redact before serialization/chunking.
func debugJSON(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err = decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return json.Marshal(redaction.Fields(decoded))
}
func debugRespond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		respond(w, nil, err)
		return
	}
	b, err := debugJSON(value)
	if err == nil && len(b) > d.MaxResponseBytes {
		err = failure(413, "debug_response_too_large", "调试响应超过 4 MiB；请缩小查询范围或分块读取宿主事件")
	}
	respond(w, json.RawMessage(b), err)
}
func (s *Server) debugRoutes(mux *http.ServeMux) {
	service := DebugService{manager: s.Manager}
	mux.HandleFunc("GET /api/sessions/{sid}/debug/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			respond(w, nil, failure(400, "invalid_debug_query", "能力查询不接受参数"))
			return
		}
		v, e := service.Capabilities(r.PathValue("sid"))
		debugRespond(w, v, e)
	})
	mux.HandleFunc("GET /api/sessions/{sid}/debug/query", func(w http.ResponseWriter, r *http.Request) {
		values, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			respond(w, nil, failure(400, "invalid_debug_query", "无效查询参数"))
			return
		}
		q, e := d.Parse(values)
		if e != nil {
			respond(w, nil, failure(400, "invalid_debug_query", e.Error()))
			return
		}
		v, e := service.Query(r.Context(), r.PathValue("sid"), q)
		debugRespond(w, v, e)
	})
}
