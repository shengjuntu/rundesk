package app

import (
	"encoding/json"
	"testing"
)

func TestCodexRunTurnAliasesPreserveEachNativeRound(t *testing.T) {
	m := testManager(t)
	s, err := m.CreateSession(m.Workspaces()[0].ID, "aliases", "")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, text := range []string{"first round", "second round"} {
		if _, err = m.Start(s.ID, Input{Text: text}); err != nil {
			t.Fatal(err)
		}
		current := waitState(t, m, s.ID, "completed")
		h, _ := m.getHandle(s.ID)
		h.op.Lock()
		h.op.Unlock()
		if current.RunID == "" || current.TurnID == "" {
			t.Fatal(current)
		}
		expected[current.RunID] = current.TurnID
	}
	events, err := m.Store.Events(s.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{}
	for _, event := range events {
		if event.Method == "run/turn" {
			var value struct {
				RunID    string `json:"runId"`
				TurnID   string `json:"turnId"`
				ThreadID string `json:"threadId"`
			}
			if err = json.Unmarshal(event.Data, &value); err != nil {
				t.Fatal(err)
			}
			if value.ThreadID == "" || expected[value.RunID] != value.TurnID {
				t.Fatal("incorrect native ownership", value)
			}
			aliases[value.RunID] = value.TurnID
		}
	}
	if len(aliases) != 2 {
		t.Fatal("native aliases missing", aliases)
	}
}
