package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceSnapshotSizeLimitHonorsFrozenScope(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("session", "source", map[string]string{"id": "source"}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Add("source", "in", "run/started", map[string]string{"runId": "run"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec("INSERT INTO events(session,time,direction,method,data) VALUES('source',?,'in','large',zeroblob(?))", Now(), (8<<20)+1)
	if err != nil {
		t.Fatal(err)
	}
	largeID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	frozen := filepath.Join(t.TempDir(), "frozen.db")
	if err = s.ExportTraceSnapshot(frozen, "source", first.ID); err != nil {
		t.Fatalf("future oversized event must not block a frozen snapshot: %v", err)
	}
	db, err := sql.Open("sqlite", frozen)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil || count != 1 {
		t.Fatalf("snapshot events=%d err=%v", count, err)
	}
	rejected := filepath.Join(t.TempDir(), "rejected.db")
	if err = s.ExportTraceSnapshot(rejected, "source", largeID); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("expected single-event size rejection: %v", err)
	}
	if _, err = os.Stat(rejected); !os.IsNotExist(err) {
		t.Fatalf("rejected snapshot must not be published: %v", err)
	}
}

func TestTraceSnapshotRetainsLargeObservationBytes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Put("session", "source", map[string]string{"id": "source"})
	s.Put("session", "other", map[string]string{"id": "other"})
	large, err := s.Add("source", "internal", "llm/exchange", map[string]any{"id": "capture", "labels": map[string]string{"sessionId": "source", "runId": "r"}, "request": map[string]any{"body": map[string]any{"bytes": []byte(strings.Repeat("ab", 5<<20)), "complete": true}}})
	if err != nil {
		t.Fatal(err)
	}
	s.Add("other", "internal", "llm/exchange", map[string]string{"secret": "foreign"})
	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err = s.ExportTraceSnapshot(path, "source", large.ID); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var same bool
	var raw []byte
	if err = db.QueryRow("SELECT data FROM events WHERE id=?", large.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	same = string(raw) == string(large.Data)
	if !same {
		t.Fatal("large model observation changed in snapshot")
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// The attached output must be detached so subsequent exports can succeed.
	if err = s.ExportTraceSnapshot(filepath.Join(t.TempDir(), "again.db"), "source", large.ID); err != nil {
		t.Fatal(err)
	}
}
