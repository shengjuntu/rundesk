package store

import "encoding/json"

type Record struct {
	Kind, ID string
	Value    any
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
