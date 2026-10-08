package kun

import (
	"bytes"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/redaction"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const diffLimit = 256
const diffNodeLimit = 20000
const diffPreviewLimit = 512

type snapshotRef struct {
	Sequence int64  `json:"sequence"`
	RunID    string `json:"runId"`
	Revision int64  `json:"revision"`
}
type valuePreview struct {
	Type      string `json:"type"`
	Bytes     int    `json:"bytes"`
	Preview   string `json:"preview"`
	Truncated bool   `json:"truncated"`
}
type snapshotChange struct {
	Path          string        `json:"path"`
	PathTruncated bool          `json:"pathTruncated,omitempty"`
	Change        string        `json:"change"`
	Before        *valuePreview `json:"before,omitempty"`
	After         *valuePreview `json:"after,omitempty"`
}
type snapshotDiff struct {
	From         snapshotRef      `json:"from"`
	To           snapshotRef      `json:"to"`
	Changes      []snapshotChange `json:"changes"`
	Truncated    bool             `json:"truncated"`
	Limit        int              `json:"limit"`
	NodeLimit    int              `json:"nodeLimit"`
	PreviewLimit int              `json:"previewLimit"`
	ArrayMatch   string           `json:"arrayMatch"`
	Redaction    string           `json:"redaction"`
}

func previewValue(v any) *valuePreview {
	typ := "null"
	switch v.(type) {
	case map[string]any:
		typ = "object"
	case []any:
		typ = "array"
	case string:
		typ = "string"
	case json.Number:
		typ = "number"
	case bool:
		typ = "boolean"
	}
	raw := p.JSON(v)
	runes := []rune(string(raw))
	preview := &valuePreview{Type: typ, Bytes: len(raw), Preview: string(raw)}
	if len(runes) > diffPreviewLimit {
		preview.Preview = string(runes[:diffPreviewLimit])
		preview.Truncated = true
	}
	return preview
}

// diffSnapshots is a bounded structural comparison, not an executable patch.
// Arrays match by position; previews use Unicode characters, sizes use JSON bytes.
// Credential-shaped fields are removed BEFORE comparing or making previews.
func diffSnapshots(from, to p.Snapshot) (snapshotDiff, error) {
	out := snapshotDiff{
		From:    snapshotRef{from.Sequence, from.State.RunID, from.State.Revision},
		To:      snapshotRef{to.Sequence, to.State.RunID, to.State.Revision},
		Changes: []snapshotChange{}, Limit: diffLimit, NodeLimit: diffNodeLimit,
		PreviewLimit: diffPreviewLimit, ArrayMatch: "position", Redaction: "structured_fields_before_diff",
	}
	if from.State.SessionID != to.State.SessionID {
		return out, fmt.Errorf("snapshots belong to different sessions")
	}
	decode := func(s p.State) (any, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(p.JSON(s)))
		d.UseNumber()
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		return redaction.Fields(v), nil
	}
	a, err := decode(from.State)
	if err != nil {
		return out, err
	}
	b, err := decode(to.State)
	if err != nil {
		return out, err
	}
	visited := 0
	var walk func(string, any, bool, any, bool, int)
	walk = func(path string, a any, hasA bool, b any, hasB bool, depth int) {
		if out.Truncated {
			return
		}
		visited++
		if visited > diffNodeLimit {
			out.Truncated = true
			return
		}
		if hasA == hasB && reflect.DeepEqual(a, b) {
			return
		}
		if hasA && hasB && depth < 24 && len(path) <= 2048 {
			if am, ok := a.(map[string]any); ok {
				if bm, ok := b.(map[string]any); ok {
					keys := make([]string, 0, len(am)+len(bm))
					for k := range am {
						keys = append(keys, k)
					}
					for k := range bm {
						if _, ok := am[k]; !ok {
							keys = append(keys, k)
						}
					}
					sort.Strings(keys)
					for _, k := range keys {
						av, ah := am[k]
						bv, bh := bm[k]
						escaped := strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
						walk(path+"/"+escaped, av, ah, bv, bh, depth+1)
						if out.Truncated {
							break
						}
					}
					return
				}
			}
			if aa, ok := a.([]any); ok {
				if ba, ok := b.([]any); ok {
					for n := 0; n < max(len(aa), len(ba)); n++ {
						var av, bv any
						if n < len(aa) {
							av = aa[n]
						}
						if n < len(ba) {
							bv = ba[n]
						}
						walk(path+"/"+strconv.Itoa(n), av, n < len(aa), bv, n < len(ba), depth+1)
						if out.Truncated {
							break
						}
					}
					return
				}
			}
		}
		if len(out.Changes) >= diffLimit {
			out.Truncated = true
			return
		}
		change := snapshotChange{Path: path, Change: "replace"}
		if runes := []rune(path); len(runes) > 512 {
			change.Path = string(runes[:512])
			change.PathTruncated = true
		}
		if hasA {
			change.Before = previewValue(a)
		} else {
			change.Change = "add"
		}
		if hasB {
			change.After = previewValue(b)
		} else {
			change.Change = "remove"
		}
		out.Changes = append(out.Changes, change)
	}
	walk("", a, true, b, true, 0)
	return out, nil
}
