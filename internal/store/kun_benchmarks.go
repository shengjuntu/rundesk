package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// A batch and all of its private execution plans become visible together.
// INSERT deliberately rejects existing identities instead of overwriting them.
func (s *Store) CreateKunBenchmark(ctx context.Context, id string, batch any, plans map[string]json.RawMessage) error {
	if len(plans) < 8 || len(plans) > 100 {
		return fmt.Errorf("invalid benchmark size")
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, data := range plans {
		if !json.Valid(data) {
			return fmt.Errorf("invalid benchmark plan")
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO objects VALUES('kun_case_plan',?,?)", key, data); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO objects VALUES('kun_benchmark',?,?)", id, raw); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) KunBenchmarkList(ctx context.Context, kind, parent string, offset, limit int) ([]json.RawMessage, error) {
	if (kind != "kun_benchmark" && kind != "kun_benchmark_report") || offset < 0 || offset > 20971520 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid benchmark list")
	}
	field := "candidateId"
	if kind == "kun_benchmark_report" {
		field = "benchmarkId"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT json_object('id',id,'hash',json_extract(data,'$.hash'),'title',json_extract(data,'$.title'),'createdAt',json_extract(data,'$.createdAt'),'candidateId',json_extract(data,'$.candidateId'),'benchmarkId',json_extract(data,'$.benchmarkId')) FROM objects WHERE kind=? AND json_extract(data,'$.`+field+`')=? ORDER BY rowid DESC LIMIT ? OFFSET ?`, kind, parent, limit+1, offset)
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
