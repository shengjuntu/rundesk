package experiment

import (
	"encoding/json"
	"strings"
	"testing"
)

func testPack() Bundle {
	tool := func(output string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"status": "succeeded", "output": output, "call": map[string]any{"id": "c", "function": map[string]any{"name": "read_file"}}}})
		return b
	}
	return Bundle{Schema: Schema, Source: Source{SessionID: "s", RunID: "r", Through: 9, Backend: "kun"}, Events: []Event{{ID: 1, Method: "kun/model.started", Data: json.RawMessage(`{}`)}, {ID: 3, Method: "kun/tool.completed", Data: tool("first")}, {ID: 5, Method: "kun/model.completed", Data: json.RawMessage(`{}`)}, {ID: 7, Method: "kun/tool.completed", Data: tool("second")}, {ID: 9, Method: "kun/run.finished", Data: json.RawMessage(`{"data":{"status":"completed"}}`)}}}
}
func TestRecordingForkInvalidationRestoreAndDiff(t *testing.T) {
	pack := testPack()
	root := NewRoot("root", "baseline", "now", pack)
	saved := Hash(root)
	sourceHash := Hash(pack)
	child, err := Fork(root, pack, "child", "edited", "later", root.ContentHash, Change{EventID: 3, Operation: "replace", Text: "alternative", Reason: "test a different return"})
	if err != nil {
		t.Fatal(err)
	}
	views, counts := Views(child, pack)
	if counts != (Counts{Recorded: 1, Edited: 1, Stale: 3}) || views[4].Classification != "stale" || !views[4].DownstreamUnverified {
		t.Fatal(views, counts)
	}
	if Hash(root) != saved || Hash(pack) != sourceHash {
		t.Fatal("original mutated")
	}
	later, err := Fork(child, pack, "later", "another hypothesis", "next", child.ContentHash, Change{EventID: 7, Operation: "replace", Text: "", Reason: "empty result"})
	if err != nil {
		t.Fatal(err)
	}
	views, counts = Views(later, pack)
	if counts != (Counts{Recorded: 1, Edited: 2, Stale: 2}) || !views[3].DownstreamUnverified {
		t.Fatal("downstream edited result must stay unverified", views, counts)
	}
	restored, err := Fork(later, pack, "restore", "restore first", "next", later.ContentHash, Change{EventID: 3, Operation: "restore", Reason: "discard first hypothesis"})
	if err != nil {
		t.Fatal(err)
	}
	_, counts = Views(restored, pack)
	if counts != (Counts{Recorded: 3, Edited: 1, Stale: 1}) {
		t.Fatal(counts)
	}
	full, err := Fork(restored, pack, "full", "restore all", "next", restored.ContentHash, Change{EventID: 7, Operation: "restore", Reason: "return to original"})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Diff(root, full, pack)
	if err != nil || len(diff) != 0 {
		t.Fatal(diff, err)
	}
	diff, err = Diff(root, child, pack)
	if err != nil || len(diff) != 4 || diff[0].EventID != 3 || diff[3].After.Classification != "stale" {
		t.Fatal(diff, err)
	}
	for n := 0; n < 5; n++ {
		again, _ := Diff(root, child, pack)
		if Hash(again) != Hash(diff) {
			t.Fatal("offline replay not deterministic")
		}
	}
	other := NewRoot("other", "other", "now", pack)
	if _, err = Diff(other, child, pack); err == nil {
		t.Fatal("cross-root diff accepted")
	}
}
func TestRecordingForkBoundsAndIdentity(t *testing.T) {
	pack := testPack()
	root := NewRoot("root", "root", "now", pack)
	for _, change := range []Change{{EventID: 2, Operation: "replace", Text: "x", Reason: "foreign"}, {EventID: 5, Operation: "replace", Text: "x", Reason: "not tool output"}, {EventID: 3, Operation: "live", Reason: "not offline"}, {EventID: 3, Operation: "restore", Reason: "not edited"}, {EventID: 3, Operation: "replace", Text: "first", Reason: "no change"}, {EventID: 3, Operation: "replace", Text: strings.Repeat("中", MaxText+1), Reason: "too long"}, {EventID: 3, Operation: "replace", Text: "x", Reason: " "}} {
		if _, err := Fork(root, pack, "bad", "bad", "now", root.ContentHash, change); err == nil {
			t.Fatal("accepted", change.Operation, change.EventID)
		}
	}
	change := Change{EventID: 3, Operation: "replace", Text: "x", Reason: "new"}
	if _, err := Fork(root, pack, "bad", "bad", "now", "wrong-hash", change); err == nil {
		t.Fatal("wrong parent hash accepted")
	}
	damaged := pack
	damaged.Source.RunID = "foreign"
	if Verify(root, damaged) == nil {
		t.Fatal("recording mutation undetected")
	}
	deep := root
	deep.Depth = MaxDepth
	deep = Seal(deep)
	if _, err := Fork(deep, pack, "bad", "bad", "now", deep.ContentHash, change); err == nil {
		t.Fatal("depth overflow")
	}
}
