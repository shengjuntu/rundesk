package store

import (
	"path/filepath"
	"testing"
)

func TestKunProjectionDedupGapAndCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	record := Record{Kind: "session", ID: "s", Value: map[string]string{"status": "running"}}
	ok, err := s.ImportKun("s", 1, Now(), "kun/run.started", map[string]int{"sequence": 1}, record)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = s.ImportKun("s", 1, Now(), "kun/run.started", nil, Record{Kind: "session", ID: "s", Value: map[string]string{"status": "bad"}})
	if err != nil || ok {
		t.Fatal("duplicate applied", ok, err)
	}
	if _, err = s.ImportKun("s", 3, Now(), "kun/run.finished", nil); err == nil {
		t.Fatal("gap accepted")
	}
	cursor, err := s.KunCursor("s")
	if err != nil || cursor != 1 {
		t.Fatal(cursor, err)
	}
	var stored map[string]string
	if err = s.Get("session", "s", &stored); err != nil || stored["status"] != "running" {
		t.Fatal(stored, err)
	}
}
