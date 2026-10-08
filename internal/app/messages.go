package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shengjuntu/rundesk/internal/store"
)

// Reply identifies one persisted, completed assistant item. Event IDs are
// scoped to their session at every read; arbitrary client text is never saved.
type Reply struct {
	SessionID string `json:"sessionId"`
	EventID   int64  `json:"eventId"`
	ItemID    string `json:"itemId"`
	TurnID    string `json:"turnId"`
	Text      string `json:"text"`
	Time      string `json:"time"`
}

type MessageFeedback struct {
	SessionID string `json:"sessionId"`
	EventID   int64  `json:"eventId"`
	Rating    string `json:"rating"`
	Comment   string `json:"comment"`
	Updated   string `json:"updated"`
}

func (m *Manager) Reply(sid string, eid int64) (Reply, error) {
	if _, err := m.Session(sid); err != nil {
		return Reply{}, err
	}
	event, err := m.Store.Event(sid, eid)
	if errors.Is(err, sql.ErrNoRows) {
		return Reply{}, failure(404, "message_not_found", "回复不存在")
	}
	if err != nil {
		return Reply{}, err
	}
	if event.Method == "kun/model.completed" {
		var v struct {
			RunID    string `json:"runId"`
			Sequence int64  `json:"sequence"`
			Data     struct {
				Purpose string `json:"purpose"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"data"`
		}
		if e := json.Unmarshal(event.Data, &v); e != nil {
			return Reply{}, e
		}
		if v.Data.Purpose != "plan" && v.Data.Message.Content != "" {
			return Reply{SessionID: sid, EventID: eid, ItemID: fmt.Sprint(v.Sequence), TurnID: v.RunID, Text: v.Data.Message.Content, Time: event.Time}, nil
		}
	}
	var data struct {
		Params struct {
			TurnID string `json:"turnId"`
			Item   struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		} `json:"params"`
	}
	if err = json.Unmarshal(event.Data, &data); err != nil {
		return Reply{}, err
	}
	if event.Direction != "in" || event.Method != "item/completed" || data.Params.Item.Type != "agentMessage" {
		return Reply{}, failure(400, "message_not_completed", "此操作仅支持已完成的模型回复")
	}
	return Reply{SessionID: sid, EventID: eid, ItemID: data.Params.Item.ID, TurnID: data.Params.TurnID, Text: data.Params.Item.Text, Time: event.Time}, nil
}

func (s *Server) messageRoutes(mux *http.ServeMux) {
	m := s.Manager
	readReply := func(r *http.Request) (Reply, error) {
		eid, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
		if err != nil || eid <= 0 {
			return Reply{}, failure(400, "invalid_event_id", "无效的回复编号")
		}
		return m.Reply(r.PathValue("sid"), eid)
	}
	mux.HandleFunc("GET /api/sessions/{sid}/messages/{eid}", func(w http.ResponseWriter, r *http.Request) {
		v, err := readReply(r)
		respond(w, v, err)
	})
	mux.HandleFunc("GET /api/sessions/{sid}/feedback", func(w http.ResponseWriter, r *http.Request) {
		sid := r.PathValue("sid")
		if _, err := m.Session(sid); err != nil {
			respond(w, nil, err)
			return
		}
		v, err := m.Store.MessageFeedback(sid)
		respond(w, v, err)
	})
	mux.HandleFunc("PUT /api/sessions/{sid}/messages/{eid}/feedback", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Rating  string `json:"rating"`
			Comment string `json:"comment"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Rating != "up" && input.Rating != "down" && input.Rating != "none" {
			writeErr(w, 400, failure(400, "invalid_rating", "评价必须为 up、down 或 none"))
			return
		}
		input.Comment = strings.TrimSpace(input.Comment)
		if utf8.RuneCountInString(input.Comment) > 2000 {
			writeErr(w, 400, failure(400, "comment_too_long", "评价说明不能超过 2000 字"))
			return
		}
		reply, err := readReply(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		if input.Rating == "none" {
			input.Comment = ""
		}
		v := MessageFeedback{SessionID: reply.SessionID, EventID: reply.EventID, Rating: input.Rating, Comment: input.Comment, Updated: store.Now()}
		err = m.Store.PutMessageFeedback(reply.SessionID, reply.EventID, v)
		if errors.Is(err, sql.ErrNoRows) {
			err = failure(404, "message_not_found", "回复已删除")
		}
		respond(w, v, err)
	})
}
