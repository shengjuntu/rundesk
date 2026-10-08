package store

import (
	"context"
	"encoding/json"
)

// WalkUsage projects only counters and lifecycle identifiers, never prompts or tool content.
// One SELECT provides a consistent SQLite snapshot of retained sessions and events.
func (s *Store) WalkUsage(ctx context.Context, visit func(Event, json.RawMessage) error) error {
	rows, e := s.db.QueryContext(ctx, `SELECT e.id,e.session,e.time,e.direction,e.method,
 json_object('runId',json_extract(e.data,'$.runId'),'status',json_extract(e.data,'$.status'),
 'threadId',json_extract(e.data,'$.params.threadId'),'turnId',coalesce(json_extract(e.data,'$.params.turnId'),json_extract(e.data,'$.params.turn.id')),
 'total',json_extract(e.data,'$.params.tokenUsage.total')),
 json_object('id',o.id,'workspaceId',json_extract(o.data,'$.workspaceId'),'instanceId',json_extract(o.data,'$.instanceId'),'source',json_extract(o.data,'$.source'))
 FROM events e JOIN objects o ON o.kind='session' AND o.id=e.session
 WHERE e.method IN ('run/input','run/state','thread/start','thread/resume','turn/started','thread/tokenUsage/updated') ORDER BY e.id`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var ev Event
		var session, data []byte
		if e = rows.Scan(&ev.ID, &ev.SessionID, &ev.Time, &ev.Direction, &ev.Method, &data, &session); e != nil {
			return e
		}
		ev.Data = data
		if e = visit(ev, session); e != nil {
			return e
		}
	}
	return rows.Err()
}
