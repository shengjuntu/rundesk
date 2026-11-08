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

func (s *Store) KunForkPreviews(ctx context.Context, offset, limit int) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT json_remove(data,'$.instruction','$.live','$.hypothesis.output','$.hypothesis.reason') FROM objects WHERE kind='kun_fork_preview' ORDER BY rowid DESC LIMIT ? OFFSET ?`, limit+1, offset)
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
