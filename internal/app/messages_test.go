package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestReplyFeedbackValidationAndSessionScope(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	first, _ := m.CreateSession(m.Workspaces()[0].ID, "first", "")
	other, _ := m.CreateSession(m.Workspaces()[0].ID, "other", "")
	add := func(method, kind, text string) store.Event {
		ev, err := m.Store.Add(first.ID, "in", method, map[string]any{"params": map[string]any{"turnId": "turn-1", "item": map[string]any{"id": "answer", "type": kind, "text": text}}})
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	ev := add("item/completed", "agentMessage", "# Answer\n\n```sh\necho '<script>'\n```\n")
	path := fmt.Sprintf("/sessions/%s/messages/%d", first.ID, ev.ID)
	r := v1Request(h, "GET", path, "", "")
	if r.Code != 200 || object(t, r)["text"] != "# Answer\n\n```sh\necho '<script>'\n```\n" {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, test := range []struct {
		path, body, code string
		status           int
	}{
		{path + "/feedback", `{"rating":"bad"}`, "invalid_rating", 400},
		{path + "/feedback", `{"rating":"down","comment":"` + strings.Repeat("字", 2001) + `"}`, "comment_too_long", 400},
		{fmt.Sprintf("/sessions/%s/messages/%d/feedback", other.ID, ev.ID), `{"rating":"up"}`, "message_not_found", 404},
		{"/sessions/" + first.ID + "/messages/wrong/feedback", `{"rating":"up"}`, "invalid_event_id", 400},
	} {
		r := v1Request(h, "PUT", test.path, test.body, "")
		if r.Code != test.status || object(t, r)["code"] != test.code {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for _, ev := range []store.Event{add("item/started", "agentMessage", "partial"), add("item/completed", "commandExecution", "private command")} {
		r = v1Request(h, "PUT", fmt.Sprintf("/sessions/%s/messages/%d/feedback", first.ID, ev.ID), `{"rating":"up"}`, "")
		if r.Code != 400 || object(t, r)["code"] != "message_not_completed" {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r = v1Request(h, "PUT", path+"/feedback", `{"rating":"down","comment":"  补充来源  "}`, "")
	if r.Code != 200 || object(t, r)["comment"] != "补充来源" {
		t.Fatal(r.Code, r.Body.String())
	}
	// PUT replaces a single record, including on retries.
	r = v1Request(h, "PUT", path+"/feedback", `{"rating":"down","comment":"补充来源"}`, "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	values, err := m.Store.MessageFeedback(first.ID)
	if err != nil || len(values) != 1 {
		t.Fatal(values, err)
	}
	r = v1Request(h, "GET", "/sessions/"+other.ID+"/feedback", "", "")
	if strings.TrimSpace(r.Body.String()) != "[]" {
		t.Fatal(r.Body.String())
	}
	r = v1Request(h, "PUT", path+"/feedback", `{"rating":"none","comment":"must be cleared"}`, "")
	if r.Code != 200 || object(t, r)["comment"] != "" || object(t, r)["rating"] != "none" {
		t.Fatal(r.Body.String())
	}
	if err = m.DeleteSession(first.ID); err != nil {
		t.Fatal(err)
	}
	all, err := m.Store.List("message-feedback")
	if err != nil || len(all) != 0 {
		t.Fatal(all, err)
	}
	if err = m.Store.PutMessageFeedback(first.ID, ev.ID, MessageFeedback{SessionID: first.ID, EventID: ev.ID, Rating: "up"}); err == nil {
		t.Fatal("orphan feedback inserted")
	}
}

func TestReplyFeedbackSurvivesRestart(t *testing.T) {
	data := t.TempDir()
	m, err := New(data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	session, _ := m.CreateSession(m.Workspaces()[0].ID, "persist", "")
	ev, err := m.Store.Add(session.ID, "in", "item/completed", map[string]any{"params": map[string]any{"turnId": "turn-1", "item": map[string]any{"id": "answer", "type": "agentMessage", "text": "Saved reply"}}})
	if err != nil {
		m.Close()
		t.Fatal(err)
	}
	path := fmt.Sprintf("/sessions/%s/messages/%d/feedback", session.ID, ev.ID)
	r := v1Request(NewHandler(m, "", true), "PUT", path, `{"rating":"down","comment":"Need evidence"}`, "")
	if r.Code != 200 {
		m.Close()
		t.Fatal(r.Body.String())
	}
	m.Close()
	m, err = New(data, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r = v1Request(NewHandler(m, "", true), "GET", "/sessions/"+session.ID+"/feedback", "", "")
	var feedback []MessageFeedback
	if err = json.Unmarshal(r.Body.Bytes(), &feedback); err != nil || len(feedback) != 1 || feedback[0].Rating != "down" || feedback[0].Comment != "Need evidence" || feedback[0].EventID != ev.ID {
		t.Fatal(feedback, err)
	}
	reply, err := m.Reply(session.ID, ev.ID)
	if err != nil || reply.Text != "Saved reply" {
		t.Fatal(reply, err)
	}
}
