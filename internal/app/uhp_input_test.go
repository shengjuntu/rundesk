package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestUHPRequestIgnoredFieldsAreExactAndUnknownFieldsHaveNoEffect(t *testing.T) {
	req, err := parseUHPRequest(strings.NewReader(`{"input":"work","Input":"not work","stream":false,"Stream":true,"tools":[],"include":[],"max_output_tokens":100,"future":{"anything":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(req.Input) != `"work"` || req.Stream {
		t.Fatal("unknown case variants affected request", req)
	}
	if !reflect.DeepEqual(req.IgnoredFields, []string{"Input", "Stream", "future", "include", "max_output_tokens", "tools"}) {
		t.Fatal(req.IgnoredFields)
	}
	plain, err := parseUHPRequest(strings.NewReader(`{"input":"work","previous_response_id":null,"max_step":null,"timeout_seconds":null}`))
	if err != nil || len(plain.IgnoredFields) != 0 || plain.MaxStep != nil || plain.TimeoutSeconds != nil {
		t.Fatal(plain, err)
	}
}

func TestUHPRequestRejectsMalformedTypedFields(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"input":"x"} {"input":"y"}`, `{"Input":"x"}`,
		`{"input":null}`, `{"input":"x","stream":null}`, `{"input":"x","stream":"true"}`,
		`{"input":"x","metadata":[]}`, `{"input":"x","metadata":{"harness_id":3}}`,
		`{"input":"x","max_step":1.1}`, `{"input":"x","max_step":0}`, `{"input":"x","timeout_seconds":-1}`,
		`{"input":"x","tools":{}}`, `{"input":"x","tools":[null]}`, `{"input":"x","include":[7]}`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := parseUHPRequest(strings.NewReader(body)); err == nil {
				t.Fatal("accepted malformed request")
			}
		})
	}
}

func TestUHPInputHistoryUsesLastUserAndPreservesGuidance(t *testing.T) {
	req, err := parseUHPRequest(strings.NewReader(`{"input":[{"role":"user","content":"old user"},{"role":"assistant","content":[{"type":"output_text","text":"old answer"}]},{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"input_text","text":"new task"}]}],"instructions":"cite evidence"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.InstructionMode != "task_text" {
		t.Fatal("instruction priority was not disclosed")
	}
	in, err := (&Server{}).uhpPrepareInput(req, "owner", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(in.Text, "old user") || strings.Contains(in.Text, "old answer") || !strings.Contains(in.Text, "be brief") || !strings.Contains(in.Text, "cite evidence") || !strings.HasSuffix(in.Text, "new task") {
		t.Fatal(in.Text)
	}
}

func TestUHPHarnessCatalogDoesNotCrossDedicatedPresetResources(t *testing.T) {
	m := testManager(t)
	w, err := m.CreateWorkspace("preset workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	i, err := m.CreateInstance(InstancePatch{Name: "preset instance"})
	if err != nil {
		t.Fatal(err)
	}
	p := ApplicationRuntimePreset{ID: "production", AppID: "app", InstanceID: i.ID, WorkspaceID: w.ID, Name: "Production", Enabled: true}
	if err := m.Store.Put("application-runtime-preset", applicationPresetKey(p.AppID, p.ID), p); err != nil {
		t.Fatal(err)
	}
	s := &Server{Manager: m}
	items, err := s.uhpHarnesses(httptest.NewRequest("GET", "/v1/harnesses", nil))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, h := range items {
		if h.InstanceID == i.ID || h.WorkspaceID == w.ID {
			count++
			if h.InstanceID != i.ID || h.WorkspaceID != w.ID || h.Source.AppID != p.AppID || h.Source.PresetID != p.ID {
				t.Fatal("preset resource crossed into generic harness", h)
			}
		}
	}
	if count != 1 {
		t.Fatal("preset harness missing or duplicated", items)
	}
	p.Enabled = false
	if err := m.Store.PutMany(store.Record{Kind: "application-runtime-preset", ID: applicationPresetKey(p.AppID, p.ID), Value: p}); err != nil {
		t.Fatal(err)
	}
	items, err = s.uhpHarnesses(httptest.NewRequest("GET", "/v1/harnesses", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range items {
		if h.InstanceID == i.ID || h.WorkspaceID == w.ID {
			t.Fatal("disabled preset exposed", h)
		}
	}
}

func TestUHPInputFilesAreMaterializedAndRemoteURLsRefused(t *testing.T) {
	m := testManager(t)
	s := &Server{Manager: m}
	w := m.Workspaces()[0]
	req, err := parseUHPRequest(strings.NewReader(`{"input":[{"role":"user","content":[{"type":"input_text","text":"read"},{"type":"input_file","filename":"note.txt","file_data":"data:text/plain;base64,aGVsbG8="}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	in, err := s.uhpPrepareInput(req, "owner", w.ID)
	if err != nil || len(in.Files) != 1 || !strings.HasPrefix(in.Files[0], "uploads/") {
		t.Fatal(in, err)
	}
	if err := m.validateInput(w, in); err != nil {
		t.Fatal(err)
	}
	req.Input = json.RawMessage(`[{"role":"user","content":[{"type":"input_image","image_url":"http://127.0.0.1/private"}]}]`)
	_, err = s.uhpPrepareInput(req, "owner", w.ID)
	var ae *apiError
	if !errors.As(err, &ae) || ae.Code != "rundesk_remote_image_unsupported" {
		t.Fatal(err)
	}
}

func TestUHPHarnessCatalogScopesBindingsAndStableIDs(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	_, k, _ := setupKey(t, m, "uhp-catalog", w.ID, "read", "run")
	_, err := m.CreateWorkspace("private", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Manager: m}
	r := httptest.NewRequest("GET", "/v1/harnesses", nil)
	r = r.WithContext(context.WithValue(r.Context(), applicationPrincipal{}, &k))
	items, err := s.uhpHarnesses(r)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	h := items[0]
	if h.WorkspaceID != w.ID || h.InstanceID != k.InstanceID || h.Source.AppID != k.AppID || !strings.HasPrefix(h.ID, "chrn_") {
		t.Fatal(h)
	}
	i, _ := m.Instance(k.InstanceID)
	if _, err := m.PatchInstance(i.ID, InstancePatch{Name: "renamed", Revision: i.Revision}); err != nil {
		t.Fatal(err)
	}
	again, err := s.uhpHarnesses(r)
	if err != nil || len(again) != 1 || again[0].ID != h.ID {
		t.Fatal(again, err)
	}
	wire, _ := json.Marshal(h.Wire())
	if strings.Contains(string(wire), w.Path) || strings.Contains(string(wire), i.CodexHome) {
		t.Fatal("catalog leaked native paths")
	}
}

func TestUHPModelSelectionUsesNativeCatalogAndRejectsSubstitution(t *testing.T) {
	m := testManager(t)
	s := &Server{Manager: m}
	harnesses, err := s.uhpHarnesses(httptest.NewRequest("GET", "/v1/harnesses", nil))
	if err != nil || len(harnesses) == 0 {
		t.Fatal(harnesses, err)
	}
	h := harnesses[0]
	model, err := s.uhpSelectModel(h, "")
	if err != nil || model != "demo-fixture" {
		t.Fatal(model, err)
	}
	_, err = s.uhpSelectModel(h, "not-in-native-catalog")
	var ae *apiError
	if !errors.As(err, &ae) || ae.Code != "model_unavailable" || ae.Status != 422 {
		t.Fatal(err)
	}
}
