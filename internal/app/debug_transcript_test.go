package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/tracequery"
)

func TestTranscriptHTTPReadBoundaryAndSnapshot(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	a, _, token := setupKey(t, m, "transcript-owner", w.ID, "read")
	_, _, foreign := setupKey(t, m, "transcript-other", w.ID, "read")
	s, err := m.CreateSessionWithSource(w.ID, "transcript", "", a.InstanceID, SessionSource{Kind: "application", AppID: a.AppID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Store.Add(s.ID, "internal", "run/input", map[string]any{"runId": "r", "input": map[string]string{"text": "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, userTestAdmin, true)
	root := "/api/v1/sessions/" + s.ID + "/debug/"
	res := appRequest(h, "GET", root+"query?kind=transcript&limit=40", "", token, "")
	var response struct {
		Source  string
		Through int64
		Data    tracequery.TranscriptPage
	}
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &response) != nil || response.Source != "unified_observation" || response.Through != first.ID || len(response.Data.Entries) != 1 || response.Data.Entries[0].Role != "user" {
		t.Fatal(res.Code, res.Body.String())
	}
	m.Store.Add(s.ID, "internal", "run/steer", map[string]any{"runId": "r", "input": map[string]string{"text": "future"}})
	res = appRequest(h, "GET", fmt.Sprintf("%squery?kind=transcript&through=%d", root, first.ID), "", token, "")
	if res.Code != 200 || strings.Contains(res.Body.String(), "future") {
		t.Fatal("snapshot", res.Code, res.Body.String())
	}
	for _, query := range []string{"kind=transcript&source=host", "kind=transcript&after=0", "kind=transcript&limit=51"} {
		if res := appRequest(h, "GET", root+"query?"+query, "", token, ""); res.Code != 400 {
			t.Fatal(query, res.Code)
		}
	}
	if res := appRequest(h, "GET", root+"query?kind=transcript", "", foreign, ""); res.Code != 403 {
		t.Fatal("ownership", res.Code)
	}
	if res := appRequest(h, "POST", root+"query?kind=transcript", `{}`, token, ""); res.Code != 403 {
		t.Fatal("read-only", res.Code)
	}
	if res := appRequest(h, "GET", root+"capabilities", "", token, ""); res.Code != 200 || !strings.Contains(res.Body.String(), `"transcript":{"available":true,"supported":true}`) {
		t.Fatal("capabilities", res.Code, res.Body.String())
	}
	if len(m.handles) != 0 {
		t.Fatal("query started runtime")
	}
}
