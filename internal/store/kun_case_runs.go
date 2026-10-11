package store

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *Store) KunCaseRunList(ctx context.Context, setID string, offset, limit int) ([]json.RawMessage, error) {
	if offset < 0 || offset > 20971520 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid case run pagination")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT json_object('id',id,'hash',json_extract(data,'$.preview.hash'),'createdAt',json_extract(data,'$.preview.createdAt'),'caseKey',json_extract(data,'$.preview.case.key'),'caseTitle',json_extract(data,'$.preview.case.title'),'instanceName',json_extract(data,'$.preview.instanceName'),'sessionId',json_extract(data,'$.preview.sessionId'),'runId',json_extract(data,'$.preview.runId')) FROM objects WHERE kind='kun_case_plan' AND json_extract(data,'$.preview.taskSetId')=? ORDER BY rowid DESC LIMIT ? OFFSET ?`, setID, limit+1, offset)
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
