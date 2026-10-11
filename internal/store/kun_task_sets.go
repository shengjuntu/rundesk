package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// List bounded metadata only: cases, inputs and rule bodies are never projected.
func (s *Store) KunTaskSetList(ctx context.Context, offset, limit int) ([]json.RawMessage, error) {
	if offset < 0 || offset > 20971520 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid task set pagination")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT json_object('id',id,'hash',json_extract(data,'$.hash'),'title',json_extract(data,'$.title'),'createdAt',json_extract(data,'$.createdAt'),'caseCount',json_extract(data,'$.caseCount')) FROM objects WHERE kind='kun_task_set' ORDER BY rowid DESC LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}
