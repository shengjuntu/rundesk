package kun

import (
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestInspectEvidenceAndContextSurviveRestart(t *testing.T) {
	var calls atomic.Int32
	var input json.RawMessage
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		final(w, "final response absent from sent input")
	}))
	defer provider.Close()
	path := filepath.Join(t.TempDir(), "inspect.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := startRequest(t, provider.URL)
	if _, err = e.Start(in); err != nil {
		t.Fatal(err)
	}
	waitKun(t, e, "completed")
	var started, completed p.Event
	for _, event := range allKunEvents(t, e) {
		if event.Type == "kun/model.started" {
			started = event
		}
		if event.Type == "kun/model.completed" {
			completed = event
		}
	}
	e.Close()
	e, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	before := e.State()
	events := allKunEvents(t, e)
	actual, err := e.Query(p.DebugQuery{Kind: "context", Sequence: started.Sequence})
	if err != nil {
		t.Fatal(err)
	}
	var context struct {
		Capture  string
		Request  json.RawMessage
		Evidence p.Event
	}
	if err = json.Unmarshal(actual.Data, &context); err != nil {
		t.Fatal(err)
	}
	var sent, captured any
	json.Unmarshal(input, &sent)
	json.Unmarshal(context.Request, &captured)
	if context.Capture != "model_request" || context.Evidence.Sequence != started.Sequence || !reflect.DeepEqual(sent, captured) || strings.Contains(string(context.Request), "final response absent") {
		t.Fatal(string(actual.Data))
	}
	state, err := e.Query(p.DebugQuery{Kind: "context", Sequence: completed.Sequence})
	if err != nil || !strings.Contains(string(state.Data), `"capture":"state_context"`) || !strings.Contains(string(state.Data), "final response absent") {
		t.Fatal(string(state.Data), err)
	}
	for _, seq := range []int64{started.Sequence, completed.Sequence} {
		result, err := e.Query(p.DebugQuery{Kind: "evidence", Sequence: seq})
		if err != nil {
			t.Fatal(err)
		}
		var evidence p.Event
		json.Unmarshal(result.Data, &evidence)
		if evidence.Sequence != seq || evidence.RunID != before.RunID || evidence.SessionID != before.SessionID {
			t.Fatal(evidence)
		}
	}
	result, err := e.Query(p.DebugQuery{Kind: "diff", FromSequence: started.Sequence, Sequence: completed.Sequence})
	if err != nil {
		t.Fatal(err)
	}
	var diff snapshotDiff
	json.Unmarshal(result.Data, &diff)
	if diff.From.Sequence != started.Sequence || diff.To.Sequence != completed.Sequence || len(diff.Changes) == 0 || diff.Truncated {
		t.Fatal(diff)
	}
	for _, q := range []p.DebugQuery{
		{Kind: "diff"}, {Kind: "diff", FromSequence: 1}, {Kind: "diff", Sequence: 1},
		{Kind: "diff", FromSequence: -1, Sequence: 1}, {Kind: "run", FromSequence: 1},
		{Kind: "evidence"}, {Kind: "evidence", Sequence: 999999},
		{Kind: "diff", FromSequence: 999999, Sequence: completed.Sequence},
	} {
		if _, err := e.Query(q); err == nil {
			t.Fatal("invalid query accepted", q)
		}
	}
	if calls.Load() != 1 || !reflect.DeepEqual(before, e.State()) || !reflect.DeepEqual(events, allKunEvents(t, e)) {
		t.Fatal("inspection changed execution or journal")
	}
}

func TestInspectDiffStructurePrecisionRedactionAndBounds(t *testing.T) {
	makeSnapshot := func(raw string) p.Snapshot {
		return p.Snapshot{Sequence: 1, State: p.State{SessionID: "session", RunID: "run", Modules: map[string]p.ModuleState{"fixture": {Data: json.RawMessage(raw)}}}}
	}
	from := makeSnapshot(`{"a/b~c":9007199254740992,"removed":null,"password":"first-secret","http_headers":{"Authorization":"secret"},"list":["before",null]}`)
	to := makeSnapshot(`{"a/b~c":9007199254740993,"added":null,"password":"second-secret","http_headers":{"Authorization":"changed-secret"},"list":["after"]}`)
	diff, err := diffSnapshots(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Changes) != 5 || diff.Truncated {
		t.Fatal(diff)
	}
	changes := map[string]snapshotChange{}
	for _, c := range diff.Changes {
		changes[c.Path] = c
	}
	c := changes["/modules/fixture/data/a~1b~0c"]
	if c.Before.Preview != "9007199254740992" || c.After.Preview != "9007199254740993" {
		t.Fatal(c)
	}
	if changes["/modules/fixture/data/added"].Change != "add" || changes["/modules/fixture/data/removed"].Change != "remove" || changes["/modules/fixture/data/list/1"].Before.Type != "null" {
		t.Fatal(changes)
	}
	if strings.Contains(string(p.JSON(diff)), "secret") || strings.Contains(string(p.JSON(diff)), "password") {
		t.Fatal("diff leaks redacted fields")
	}
	reverse, err := diffSnapshots(to, from)
	if err != nil {
		t.Fatal(err)
	}
	foundReverse := false
	for _, change := range reverse.Changes {
		if change.Path == "/modules/fixture/data/added" && change.Change == "remove" {
			foundReverse = true
		}
	}
	if !foundReverse {
		t.Fatal("reverse comparison lost removal")
	}
	same, err := diffSnapshots(from, from)
	if err != nil || len(same.Changes) != 0 || same.Truncated {
		t.Fatal(same, err)
	}
	to.State.SessionID = "other"
	if _, err := diffSnapshots(from, to); err == nil {
		t.Fatal("cross-session diff allowed")
	}
	to.State.SessionID = "session"
	to.State.RunID = "next-run"
	crossRun, err := diffSnapshots(from, to)
	if err != nil || crossRun.From.RunID == crossRun.To.RunID {
		t.Fatal(crossRun, err)
	}
	values := map[string]any{}
	for n := 0; n < 400; n++ {
		values[fmt.Sprintf("key%03d", n)] = strings.Repeat("界", 1000)
	}
	bounded, err := diffSnapshots(makeSnapshot(`{}`), makeSnapshot(string(p.JSON(values))))
	if err != nil || len(bounded.Changes) != diffLimit || !bounded.Truncated {
		t.Fatal(len(bounded.Changes), bounded.Truncated, err)
	}
	for _, c := range bounded.Changes {
		if !c.After.Truncated || len([]rune(c.After.Preview)) != diffPreviewLimit || !utf8.ValidString(c.After.Preview) {
			t.Fatal(c)
		}
	}
	// An added object is redacted before its preview is stringified.
	added, err := diffSnapshots(makeSnapshot(`{}`), makeSnapshot(`{"new":{"api_key":"hidden","visible":"ok"}}`))
	if err != nil || strings.Contains(string(p.JSON(added)), "hidden") || !strings.Contains(string(p.JSON(added)), "[redacted]") {
		t.Fatal(added, err)
	}
	longKey := strings.Repeat("界", 3000)
	long, err := diffSnapshots(makeSnapshot(`{}`), makeSnapshot(string(p.JSON(map[string]int{longKey: 1}))))
	if err != nil || !long.Changes[0].PathTruncated || len([]rune(long.Changes[0].Path)) > 512 {
		t.Fatal("unbounded path", err)
	}
	large := make([]int, diffNodeLimit+10)
	a := makeSnapshot(string(p.JSON(map[string]any{"list": large})))
	large[len(large)-1] = 1
	b := makeSnapshot(string(p.JSON(map[string]any{"list": large})))
	nodes, err := diffSnapshots(a, b)
	if err != nil || !nodes.Truncated {
		t.Fatal("node traversal cap not reported", err)
	}
}
