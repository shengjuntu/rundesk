package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
)

func taskSetFixture() KunTaskSetInput {
	return KunTaskSetInput{Title: "库存案例", Cases: []KunTaskCase{
		{Key: "in-stock", Title: "库存充足", Input: "  库存为 100。\n请给出建议。  ", Rules: []KunReplyRule{{Kind: "reply_contains", Value: "充足"}}},
		{Key: "empty", Title: "无库存", Input: "PRIVATE-INPUT: 库存为 0", Rules: []KunReplyRule{{Kind: "reply_contains", Value: "补货"}, {Kind: "reply_not_contains", Value: "PRIVATE-RULE"}}},
	}}
}

func TestKunTaskSetImmutablePersistenceAndPagination(t *testing.T) {
	m := testManager(t)
	m.Kun, m.Codex = "no-worker", "no-codex"
	h := NewHandler(m, userTestAdmin, true)
	in := taskSetFixture()
	body := string(p.JSON(in))
	r := appRequest(h, "POST", "/api/v1/kun-task-sets", body, userTestAdmin, "task-set-once")
	var saved KunTaskSet
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &saved) != nil || saved.CaseCount != 2 || saved.Hash == "" || saved.Hash != taskSetHash(saved) || saved.Cases[0].Input != in.Cases[0].Input {
		t.Fatal(r.Code, r.Body.String())
	}
	r = appRequest(h, "POST", "/api/v1/kun-task-sets", body, userTestAdmin, "task-set-once")
	var retry KunTaskSet
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &retry) != nil || forkDigest(saved) != forkDigest(retry) {
		t.Fatal("idempotency", r.Code, r.Body.String())
	}
	in.Title = "changed"
	r = appRequest(h, "POST", "/api/v1/kun-task-sets", string(p.JSON(in)), userTestAdmin, "task-set-once")
	if r.Code != 409 {
		t.Fatal("changed request reused key", r.Code)
	}
	second, err := m.CreateKunTaskSet(in)
	if err != nil || second.ID == saved.ID {
		t.Fatal(second, err)
	}
	in.Cases[0].Input = "changed after save"
	in.Cases[0].Rules[0].Value = "changed rule"
	if second.Hash != taskSetHash(second) || second.Cases[0].Input == in.Cases[0].Input || second.Cases[0].Rules[0].Value == in.Cases[0].Rules[0].Value {
		t.Fatal("caller aliases retained")
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextOffset int  `json:"nextOffset"`
		HasMore    bool `json:"hasMore"`
	}
	for index, expected := range []string{second.ID, saved.ID} {
		r = appRequest(h, "GET", fmt.Sprintf("/api/kun-task-sets?offset=%d&limit=1", index), "", userTestAdmin, "")
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].ID != expected || page.NextOffset != index+1 || page.HasMore != (index == 0) {
			t.Fatal(r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), "PRIVATE-") || strings.Contains(r.Body.String(), `"cases"`) || strings.Contains(r.Body.String(), `"rules"`) {
			t.Fatal("list leaked case bodies")
		}
	}
	r = appRequest(h, "GET", "/api/v1/kun-task-sets/"+saved.ID, "", userTestAdmin, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "PRIVATE-INPUT") {
		t.Fatal("full definition missing", r.Code)
	}
	if len(m.handles) != 0 {
		t.Fatal("task set started runtime")
	}
	data := m.Data
	m.Close()
	m2, err := New(data, "no-codex", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	read, err := m2.KunTaskSet(saved.ID)
	if err != nil || forkDigest(read) != forkDigest(saved) {
		t.Fatal("restart changed set", err)
	}
	read.Cases[0].Input = "tampered"
	if err = m2.Store.Put("kun_task_set", read.ID, read); err != nil {
		t.Fatal(err)
	}
	if _, err = m2.KunTaskSet(read.ID); err == nil {
		t.Fatal("tampering accepted")
	}
}

func TestKunTaskSetBoundsAndAuthorization(t *testing.T) {
	for _, mutate := range []func(*KunTaskSetInput){
		func(v *KunTaskSetInput) { v.Title = " " },
		func(v *KunTaskSetInput) { v.Title = string([]byte{0xff}) },
		func(v *KunTaskSetInput) { v.Cases = nil },
		func(v *KunTaskSetInput) { v.Cases[1].Key = v.Cases[0].Key },
		func(v *KunTaskSetInput) { v.Cases[0].Key = "../bad" },
		func(v *KunTaskSetInput) { v.Cases[0].Key = strings.Repeat("a", 65) },
		func(v *KunTaskSetInput) { v.Cases[0].Title = strings.Repeat("界", 121) },
		func(v *KunTaskSetInput) { v.Cases[0].Input = " \n" },
		func(v *KunTaskSetInput) { v.Cases[0].Input = string([]byte{0xff}) },
		func(v *KunTaskSetInput) { v.Cases[0].Input = strings.Repeat("界", 8193) },
		func(v *KunTaskSetInput) { v.Cases[0].Rules = nil },
		func(v *KunTaskSetInput) { v.Cases[0].Rules[0].Kind = "script" },
		func(v *KunTaskSetInput) { v.Cases[0].Rules = append(v.Cases[0].Rules, v.Cases[0].Rules[0]) },
	} {
		v := taskSetFixture()
		mutate(&v)
		if validateKunTaskSet(v) == nil {
			t.Fatal("invalid definition accepted", v)
		}
	}
	v := taskSetFixture()
	v.Cases = nil
	for i := 0; i < 20; i++ {
		c := taskSetFixture().Cases[0]
		c.Key = fmt.Sprintf("case-%d", i)
		v.Cases = append(v.Cases, c)
	}
	if validateKunTaskSet(v) != nil {
		t.Fatal("20 valid cases rejected")
	}
	v.Cases = append(v.Cases, v.Cases[0])
	if validateKunTaskSet(v) == nil {
		t.Fatal("21 cases accepted")
	}
	v.Cases = v.Cases[:20]
	for i := range v.Cases {
		v.Cases[i].Input = strings.Repeat("界", 8192)
	}
	if validateKunTaskSet(v) == nil {
		t.Fatal("aggregate byte limit ignored")
	}
	v.Cases = v.Cases[:1]
	if validateKunTaskSet(v) != nil {
		t.Fatal("8192 Unicode characters rejected")
	}

	m, user, code, _, viewer, source, _ := userFixture(t)
	_, _, key := setupKey(t, m, "task-set-key", source.WorkspaceID, "read", "run", "approvals")
	h := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{code, viewer, key} {
		for _, prefix := range []string{"/api", "/api/v1", "/api/v1/member/" + user.Grants[0].ID} {
			for _, route := range []struct{ method, path string }{{"GET", "/kun-task-sets"}, {"POST", "/kun-task-sets"}, {"GET", "/kun-task-sets/id"}} {
				r := appRequest(h, route.method, prefix+route.path, `{}`, token, "denied-task-set")
				if r.Code != 403 {
					t.Fatal(route, prefix, r.Code)
				}
			}
		}
	}
	if r := appRequest(h, "GET", "/api/v1/kun-task-sets?limit=50", "", userTestAdmin, ""); r.Code != 200 {
		t.Fatal("valid max page rejected", r.Code)
	}
	for _, path := range []string{"?limit=51", "?offset=-1", "?offset=20971521", "?limit=1&limit=2", "?unknown=1", "/id?unknown=1"} {
		r := appRequest(h, "GET", "/api/v1/kun-task-sets"+path, "", userTestAdmin, "")
		if r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	for index, body := range []string{`{"title":"x","cases":[],"hash":"client"}`, `{"title":"x","cases":[{"key":"a","title":"a","input":"x","rules":[],"extra":1}]}`} {
		r := appRequest(h, "POST", "/api/v1/kun-task-sets", body, userTestAdmin, fmt.Sprintf("invalid-task-set-%d", index))
		if r.Code != 400 {
			t.Fatal("unknown fields accepted", r.Code)
		}
	}
	r := appRequest(h, "POST", "/api/v1/kun-task-sets", string(p.JSON(taskSetFixture())), userTestAdmin, "")
	if r.Code != 400 {
		t.Fatal("idempotency key missing", r.Code)
	}
}
