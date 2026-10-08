package store

import (
	"context"
	"encoding/json"
)

func (s *Store) KunForkPreviews(ctx context.Context, offset, limit int) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT json_remove(data,'$.instruction','$.live') FROM objects WHERE kind='kun_fork_preview' ORDER BY rowid DESC LIMIT ? OFFSET ?`, limit+1, offset)
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
