package app

// Persisted history belongs only to the explicitly selected protocol demo.
import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/shengjuntu/rundesk/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type demoTurn struct {
	ID     string           `json:"id"`
	Status string           `json:"status"`
	Items  []map[string]any `json:"items"`
}
type demoThread struct {
	ID     string            `json:"id"`
	Status map[string]string `json:"status"`
	Turns  []demoTurn        `json:"turns"`
}
type demoHistory struct {
	mu  sync.Mutex
	dir string
}

func newDemoHistory() *demoHistory {
	return &demoHistory{dir: filepath.Join(os.Getenv("CODEX_HOME"), "rundesk-demo-threads")}
}
func (h *demoHistory) path(id string) string {
	return filepath.Join(h.dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(id))))
}
func (h *demoHistory) read(id string) (demoThread, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.readLocked(id)
}
func (h *demoHistory) readLocked(id string) (demoThread, error) {
	var v demoThread
	b, e := os.ReadFile(h.path(id))
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return v, e
}
func (h *demoHistory) turn(id, tid, status, reply string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, e := h.readLocked(id)
	if e != nil {
		v = demoThread{ID: id, Turns: []demoTurn{}}
	}
	runtime := "idle"
	if status == "inProgress" {
		runtime = "active"
	}
	v.Status = map[string]string{"type": runtime}
	t := demoTurn{ID: tid, Status: status, Items: []map[string]any{}}
	if reply != "" {
		t.Items = append(t.Items, map[string]any{"type": "agentMessage", "text": reply})
	}
	if len(v.Turns) > 0 && v.Turns[len(v.Turns)-1].ID == tid {
		v.Turns[len(v.Turns)-1] = t
	} else {
		v.Turns = append(v.Turns, t)
	}
	b, _ := json.Marshal(v)
	_ = os.MkdirAll(h.dir, 0700)
	temp := h.path(id) + "." + store.ID()
	if os.WriteFile(temp, b, 0600) == nil {
		_ = os.Rename(temp, h.path(id))
	}
}
func demoWriteOutput(dir, text, name, body string) {
	marker := "Save deliverable files in "
	if i := strings.Index(text, marker); i >= 0 {
		out := strings.SplitN(text[i+len(marker):], ". Uploaded files", 2)[0]
		if rel, e := filepath.Rel(dir, out); e == nil && safePath(filepath.ToSlash(rel)) {
			if root, e := os.OpenRoot(dir); e == nil {
				defer root.Close()
				_ = root.MkdirAll(rel, 0700)
				if f, e := root.OpenFile(filepath.Join(rel, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); e == nil {
					_, _ = f.WriteString(body)
					_ = f.Close()
				}
			}
		}
	}
}
