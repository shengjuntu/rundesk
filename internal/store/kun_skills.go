package store

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *Store) KunSkillList(ctx context.Context, kind, candidate string, offset, limit int) ([]json.RawMessage, error) {
	if (kind != "kun_skill" && kind != "kun_skill_validation") || offset < 0 || offset > 20971520 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid skill list")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT json_object('id',id,'hash',json_extract(data,'$.hash'),'title',json_extract(data,'$.title'),'createdAt',json_extract(data,'$.createdAt'),'candidateId',json_extract(data,'$.candidateId')) FROM objects WHERE kind=? AND (?='' OR json_extract(data,'$.candidateId')=?) ORDER BY rowid DESC LIMIT ? OFFSET ?`, kind, candidate, candidate, limit+1, offset)
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
