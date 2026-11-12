package store

import (
	"bytes"
	"encoding/json"
	"errors"
)

type Record struct {
	Kind, ID string
	Value    any
}

// PutImmutable creates a content-addressed object once. Repeated admissions
// reuse its row. A different value under the same identity is a storage error.
func (s *Store) PutImmutable(kind, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := s.db.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(kind,id) DO NOTHING", kind, id, data)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 1 {
		return err
	}
	var previous []byte
	if err := s.db.QueryRow("SELECT data FROM objects WHERE kind=? AND id=?", kind, id).Scan(&previous); err != nil {
		return err
	}
	if !bytes.Equal(previous, data) {
		return errors.New("immutable object identity has different content")
	}
	return nil
}

func (s *Store) PutMany(records ...Record) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, r := range records {
		b, e := json.Marshal(r.Value)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", r.Kind, r.ID, b); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Delete(kind, id string) error {
	_, e := s.db.Exec("DELETE FROM objects WHERE kind=? AND id=?", kind, id)
	return e
}
