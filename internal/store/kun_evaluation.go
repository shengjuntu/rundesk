package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// Lists project only bounded metadata, never whole rules or comparison reports.
func (s *Store) KunEvaluationList(ctx context.Context, kind, key string, offset, limit int) ([]json.RawMessage, error) {
	var query string
	switch kind {
	case "rubrics":
		query = `SELECT json_object('id',id,'title',json_extract(data,'$.title'),'hash',json_extract(data,'$.hash'),'createdAt',json_extract(data,'$.createdAt'),'ruleCount',json_extract(data,'$.ruleCount'),'baselineHash',json_extract(data,'$.baselineHash')) FROM objects WHERE kind='kun_rubric' AND json_extract(data,'$.baselineHash')=? ORDER BY rowid DESC LIMIT ? OFFSET ?`
	case "evaluations":
		query = `SELECT json_object('id',id,'hash',json_extract(data,'$.hash'),'createdAt',json_extract(data,'$.createdAt'),'title',json_extract(data,'$.rubric.title'),'leftStatus',json_extract(data,'$.left.status'),'rightStatus',json_extract(data,'$.right.status'),'against',json_extract(data,'$.comparison.against')) FROM objects WHERE kind='kun_evaluation' AND json_extract(data,'$.previewId')=? ORDER BY rowid DESC LIMIT ? OFFSET ?`
	default:
		return nil, fmt.Errorf("unsupported evaluation list")
	}
	if offset < 0 || offset > 20971520 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid evaluation pagination")
	}
	rows, err := s.db.QueryContext(ctx, query, key, limit+1, offset)
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
