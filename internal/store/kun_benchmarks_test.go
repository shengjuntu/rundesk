package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestKunBenchmarkAtomicPlanCreation(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Put("kun_case_plan", "collision", map[string]string{"original": "kept"}); e != nil {
		t.Fatal(e)
	}
	plans := map[string]json.RawMessage{"collision": json.RawMessage(`{"new":true}`)}
	for n := 0; n < 7; n++ {
		plans[ID()] = json.RawMessage(`{"new":true}`)
	}
	if e = s.CreateKunBenchmark(context.Background(), "batch", map[string]string{"id": "batch"}, plans); e == nil {
		t.Fatal("overwrote existing identity")
	}
	rows, e := s.List("kun_case_plan")
	if e != nil || len(rows) != 1 || string(rows[0]) != `{"original":"kept"}` {
		t.Fatal("partial plans escaped rollback", rows, e)
	}
	var batch any
	if e = s.Get("kun_benchmark", "batch", &batch); e == nil {
		t.Fatal("partial benchmark persisted")
	}
}
