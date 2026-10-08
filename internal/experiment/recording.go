// Package experiment implements offline record overlays. It deliberately has no
// provider, worker, tool gateway or execution-state import capability.
package experiment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

const Schema = 1
const MaxDepth = 32
const MaxPatches = 32
const MaxText = 16000
const Mode = "record_edit"

type Source struct {
	SessionID   string `json:"sessionId"`
	RunID       string `json:"runId"`
	Through     int64  `json:"through"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`
	Title       string `json:"title"`
	Backend     string `json:"backend"`
}
type Event struct {
	ID     int64           `json:"id"`
	Time   string          `json:"time"`
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data"`
}
type Bundle struct {
	Schema int     `json:"schema"`
	Source Source  `json:"source"`
	Events []Event `json:"events"`
}
type Patch struct {
	EventID  int64  `json:"eventId"`
	Text     string `json:"text"`
	Reason   string `json:"reason"`
	BranchID string `json:"branchId"`
}
type Change struct {
	EventID   int64  `json:"eventId"`
	Operation string `json:"operation"`
	Text      string `json:"text,omitempty"`
	Reason    string `json:"reason"`
}
type Branch struct {
	Schema      int     `json:"schema"`
	ID          string  `json:"id"`
	RootID      string  `json:"rootId"`
	ParentID    string  `json:"parentId,omitempty"`
	ParentHash  string  `json:"parentHash,omitempty"`
	BundleHash  string  `json:"bundleHash"`
	ContentHash string  `json:"contentHash"`
	Source      Source  `json:"source"`
	Title       string  `json:"title"`
	CreatedAt   string  `json:"createdAt"`
	Mode        string  `json:"mode"`
	Depth       int     `json:"depth"`
	EventCount  int     `json:"eventCount"`
	Patches     []Patch `json:"patches"`
	Change      *Change `json:"change,omitempty"`
}
type View struct {
	ID                   int64  `json:"id"`
	Index                int    `json:"index"`
	Time                 string `json:"time"`
	Method               string `json:"method"`
	Classification       string `json:"classification"`
	DownstreamUnverified bool   `json:"downstreamUnverified"`
	Editable             bool   `json:"editable"`
	ToolName             string `json:"toolName,omitempty"`
	Patch                *Patch `json:"patch,omitempty"`
}
type Counts struct {
	Recorded int `json:"recorded"`
	Edited   int `json:"edited"`
	Stale    int `json:"stale"`
}
type Difference struct {
	EventID int64  `json:"eventId"`
	Method  string `json:"method"`
	Before  View   `json:"before"`
	After   View   `json:"after"`
}

func Hash(v any) string    { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }
func Seal(b Branch) Branch { b.ContentHash = ""; b.ContentHash = Hash(b); return b }
func Verify(b Branch, pack Bundle) error {
	if b.Schema != Schema || pack.Schema != Schema || b.Mode != Mode || b.BundleHash != Hash(pack) || b.EventCount != len(pack.Events) || b.Source != pack.Source || Seal(b).ContentHash != b.ContentHash {
		return fmt.Errorf("experiment recording integrity mismatch")
	}
	return nil
}
func ToolOutput(event Event) (string, string, bool) {
	if event.Method != "kun/tool.completed" {
		return "", "", false
	}
	var e struct {
		Data struct {
			Call   p.ToolCall `json:"call"`
			Output *string    `json:"output"`
			Status string     `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(event.Data, &e) != nil || e.Data.Output == nil || e.Data.Call.ID == "" || e.Data.Call.Function.Name == "" || (e.Data.Status != "succeeded" && e.Data.Status != "failed") {
		return "", "", false
	}
	return *e.Data.Output, e.Data.Call.Function.Name, true
}
func NewRoot(id, title, created string, pack Bundle) Branch {
	return Seal(Branch{Schema: Schema, ID: id, RootID: id, BundleHash: Hash(pack), Source: pack.Source, Title: title, CreatedAt: created, Mode: Mode, EventCount: len(pack.Events), Patches: []Patch{}})
}
func Fork(parent Branch, pack Bundle, id, title, created, expectedHash string, change Change) (Branch, error) {
	if err := Verify(parent, pack); err != nil {
		return Branch{}, err
	}
	if expectedHash != parent.ContentHash {
		return Branch{}, fmt.Errorf("parent hash conflict")
	}
	if parent.Depth >= MaxDepth {
		return Branch{}, fmt.Errorf("branch depth exceeds %d", MaxDepth)
	}
	if strings.TrimSpace(change.Reason) == "" || utf8.RuneCountInString(change.Reason) > 2000 {
		return Branch{}, fmt.Errorf("change reason must have 1–2000 characters")
	}
	index := sort.Search(len(pack.Events), func(i int) bool { return pack.Events[i].ID >= change.EventID })
	if index == len(pack.Events) || pack.Events[index].ID != change.EventID {
		return Branch{}, fmt.Errorf("event is outside the fixed run recording")
	}
	original, _, editable := ToolOutput(pack.Events[index])
	if !editable {
		return Branch{}, fmt.Errorf("only recorded tool success/failure output can be replaced")
	}
	patches := append([]Patch{}, parent.Patches...)
	found := -1
	for i, v := range patches {
		if v.EventID == change.EventID {
			found = i
			break
		}
	}
	switch change.Operation {
	case "replace":
		if utf8.RuneCountInString(change.Text) > MaxText {
			return Branch{}, fmt.Errorf("replacement exceeds %d characters", MaxText)
		}
		if found < 0 && change.Text == original || found >= 0 && change.Text == patches[found].Text && change.Reason == patches[found].Reason {
			return Branch{}, fmt.Errorf("no change to recorded or inherited output")
		}
		patch := Patch{EventID: change.EventID, Text: change.Text, Reason: change.Reason, BranchID: id}
		if found < 0 {
			patches = append(patches, patch)
		} else {
			patches[found] = patch
		}
	case "restore":
		if found < 0 || change.Text != "" {
			return Branch{}, fmt.Errorf("restore requires an inherited replacement and no text")
		}
		patches = append(patches[:found], patches[found+1:]...)
	default:
		return Branch{}, fmt.Errorf("operation must be replace or restore")
	}
	if len(patches) > MaxPatches {
		return Branch{}, fmt.Errorf("at most %d replacements per branch", MaxPatches)
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].EventID < patches[j].EventID })
	return Seal(Branch{Schema: Schema, ID: id, RootID: parent.RootID, ParentID: parent.ID, ParentHash: parent.ContentHash, BundleHash: parent.BundleHash, Source: parent.Source, Title: title, CreatedAt: created, Mode: Mode, Depth: parent.Depth + 1, EventCount: len(pack.Events), Patches: patches, Change: &change}), nil
}
func Views(branch Branch, pack Bundle) ([]View, Counts) {
	views := make([]View, 0, len(pack.Events))
	counts := Counts{}
	patches := map[int64]Patch{}
	var first int64
	for _, p := range branch.Patches {
		patches[p.EventID] = p
		if first == 0 || p.EventID < first {
			first = p.EventID
		}
	}
	for i, event := range pack.Events {
		_, name, editable := ToolOutput(event)
		v := View{ID: event.ID, Index: i, Time: event.Time, Method: event.Method, Classification: "recorded", Editable: editable, ToolName: name, DownstreamUnverified: first > 0 && event.ID > first}
		if patch, ok := patches[event.ID]; ok {
			v.Classification = "edited"
			v.Patch = &patch
			counts.Edited++
		} else if v.DownstreamUnverified {
			v.Classification = "stale"
			counts.Stale++
		} else {
			counts.Recorded++
		}
		views = append(views, v)
	}
	return views, counts
}
func Diff(before, after Branch, pack Bundle) ([]Difference, error) {
	if before.RootID != after.RootID || before.BundleHash != after.BundleHash {
		return nil, fmt.Errorf("compare only branches of the same recording")
	}
	if err := Verify(before, pack); err != nil {
		return nil, err
	}
	if err := Verify(after, pack); err != nil {
		return nil, err
	}
	a, _ := Views(before, pack)
	b, _ := Views(after, pack)
	out := []Difference{}
	for i := range a {
		if Hash(a[i]) != Hash(b[i]) {
			out = append(out, Difference{EventID: a[i].ID, Method: a[i].Method, Before: a[i], After: b[i]})
		}
	}
	return out, nil
}
