package app

import (
	"errors"
	kc "github.com/shengjuntu/rundesk/internal/adapters/kun"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"strconv"
)

func (s *Server) kunDebugRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/kun/query", func(w http.ResponseWriter, r *http.Request) {
		q := p.DebugQuery{Kind: r.URL.Query().Get("kind")}
		if value := r.URL.Query().Get("sequence"); value != "" {
			seq, err := strconv.ParseInt(value, 10, 64)
			if err != nil || seq < 0 {
				respond(w, nil, failure(400, "invalid_debug_query", "无效快照序号"))
				return
			}
			q.Sequence = seq
		}
		if err := q.Validate(); err != nil {
			respond(w, nil, failure(400, "invalid_debug_query", err.Error()))
			return
		}
		c, err := s.Manager.kunClient(r.PathValue("sid"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		var result p.DebugResult
		err = c.Call(r.Context(), "query", q, &result)
		var remote *kc.RemoteError
		if errors.As(err, &remote) {
			err = failure(409, "kun_query_rejected", remote.Error())
		}
		respond(w, redact(result), err)
	})
}
