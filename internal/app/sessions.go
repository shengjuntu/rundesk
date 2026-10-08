package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SessionPatch struct {
	Title    *string `json:"title,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

func (m *Manager) PatchSession(id string, p SessionPatch) (Session, error) {
	if p.Title != nil {
		title := strings.TrimSpace(*p.Title)
		if title == "" || len([]rune(title)) > 120 {
			return Session{}, errors.New("标题需为 1–120 个字符")
		}
		p.Title = &title
	}
	if _, e := m.Session(id); e != nil {
		return Session{}, e
	}
	h, e := m.getHandle(id)
	if e != nil {
		return Session{}, e
	}
	h.op.Lock()
	defer h.op.Unlock()
	s, e := m.Session(id)
	if e != nil {
		return s, e
	}
	if p.Archived != nil && *p.Archived && (active(s.Status) || m.queueReserved(id)) {
		return s, errors.New("运行中的会话不能归档，请先停止任务")
	}
	e = m.update(id, func(s *Session) {
		if p.Title != nil {
			s.Title = *p.Title
		}
		if p.Pinned != nil {
			s.Pinned = *p.Pinned
		}
		if p.Archived != nil {
			s.Archived = *p.Archived
		}
	})
	if e != nil {
		return s, e
	}
	return m.Session(id)
}

// Delete only RunDesk's metadata/journal. Original files and native Codex
// history intentionally remain available to the operator.
func (m *Manager) DeleteSession(id string) error {
	if _, e := m.Session(id); e != nil {
		return e
	}
	h, e := m.getHandle(id)
	if e != nil {
		return e
	}
	h.op.Lock()
	defer h.op.Unlock()
	s, e := m.Session(id)
	if e != nil {
		return e
	}
	if active(s.Status) || m.queueReserved(id) {
		return errors.New("运行中的会话不能删除，请先停止任务")
	}
	h.mu.Lock()
	c := h.client
	k := h.kun
	h.kun = nil
	h.client = nil
	h.thread = ""
	h.requests = map[string]Approval{}
	h.mu.Unlock()
	if c != nil {
		c.Close()
		<-c.Done()
	}
	if k != nil {
		k.Close()
	}
	if e = m.Store.DeleteSession(id); e != nil {
		return e
	}
	m.mu.Lock()
	delete(m.sessions, id)
	delete(m.handles, id)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Markdown(id string) (string, error) {
	s, e := m.Session(id)
	if e != nil {
		return "", e
	}
	type message struct{ role, text string }
	ordered := []*message{}
	byID := map[string]*message{}
	var after int64
	for {
		events, e := m.Store.Events(id, after, 1000)
		if e != nil {
			return "", e
		}
		for _, ev := range events {
			after = ev.ID
			if ev.Method == "run/input" || ev.Method == "run/steer" {
				var v struct {
					Input Input `json:"input"`
				}
				if json.Unmarshal(ev.Data, &v) == nil {
					ordered = append(ordered, &message{"用户", v.Input.Text})
				}
				continue
			}
			if ev.Method == "kun/control.queued" {
				var v struct {
					Data struct {
						Command struct {
							Operation string `json:"operation"`
							Text      string `json:"text"`
						} `json:"command"`
					} `json:"data"`
				}
				if json.Unmarshal(ev.Data, &v) == nil && v.Data.Command.Operation == "steer" {
					ordered = append(ordered, &message{"用户", v.Data.Command.Text})
				}
				continue
			}
			if ev.Method == "kun/model.completed" {
				var v struct {
					Data struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"data"`
				}
				if json.Unmarshal(ev.Data, &v) == nil && v.Data.Message.Content != "" {
					ordered = append(ordered, &message{"Kun", v.Data.Message.Content})
				}
				continue
			}
			if ev.Direction != "in" {
				continue
			}
			var envelope struct {
				Params struct {
					ItemID string `json:"itemId"`
					Delta  string `json:"delta"`
					Item   struct {
						ID   string `json:"id"`
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				} `json:"params"`
			}
			if json.Unmarshal(ev.Data, &envelope) != nil {
				continue
			}
			p := envelope.Params
			var itemID, text string
			replace := false
			if ev.Method == "item/agentMessage/delta" {
				itemID = p.ItemID
				text = p.Delta
			} else if ev.Method == "item/completed" && p.Item.Type == "agentMessage" {
				itemID = p.Item.ID
				text = p.Item.Text
				replace = true
			} else {
				continue
			}
			row := byID[itemID]
			if row == nil {
				row = &message{role: "Codex"}
				byID[itemID] = row
				ordered = append(ordered, row)
			}
			if replace {
				row.text = text
			} else {
				row.text += text
			}
		}
		if len(events) < 1000 {
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nRunDesk 会话 `%s` · 原生线程 `%s`\n\n", strings.ReplaceAll(s.Title, "\n", " "), s.ID, s.ThreadID)
	for _, row := range ordered {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", row.role, row.text)
	}
	return b.String(), nil
}
