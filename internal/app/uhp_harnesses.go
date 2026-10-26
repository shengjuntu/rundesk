package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// A UHP configured harness is an existing RunDesk configuration and workspace
// binding. It does not own another configuration tree or native runtime.
type uhpHarness struct {
	ID, InstanceID, WorkspaceID string
	Source                      SessionSource
	Base, Name, DefaultModel    string
	CreatedAt                   int64
}

func (h uhpHarness) Wire() map[string]any {
	return map[string]any{
		"id": h.ID, "object": "harness", "name": h.Name, "base": h.Base,
		"baseLabel": h.Base, "defaultModel": h.DefaultModel, "createdAt": h.CreatedAt,
		// Image input is a native capability, not something the facade invents.
		"rundesk_capabilities": uhpExecutionCapabilities(h.Base),
		"rundesk_execution":    uhpExecutionSemantics(),
	}
}

func makeUHPHarness(i Instance, w Workspace, source SessionSource, name string) uhpHarness {
	identity := strings.Join([]string{i.ID, w.ID, source.Kind, source.AppID, source.PresetID}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	created, _ := time.Parse(time.RFC3339Nano, i.Created)
	model := i.DefaultModel
	if runtimeKind(i.AgentRuntime.Kind) == "kun" {
		model = i.AgentRuntime.Model
	}
	if name == "" {
		name = i.Name + " / " + w.Name
	}
	return uhpHarness{ID: "chrn_" + hex.EncodeToString(sum[:16]), InstanceID: i.ID, WorkspaceID: w.ID, Source: source, Base: runtimeKind(i.AgentRuntime.Kind), Name: name, DefaultModel: model, CreatedAt: created.UnixMilli()}
}

func (s *Server) uhpHarnesses(r *http.Request) ([]uhpHarness, error) {
	m := s.Manager
	presets, err := m.allApplicationPresets()
	if err != nil {
		return nil, err
	}
	out := []uhpHarness{}
	seen := map[string]bool{}
	add := func(iid, wid string, source SessionSource, name string) {
		i, ie := m.Instance(iid)
		w, we := m.Workspace(wid)
		if ie != nil || we != nil {
			return
		}
		if _, err := agentBackend(i.AgentRuntime.Kind); err != nil {
			return
		}
		h := makeUHPHarness(i, w, source, name)
		if !seen[h.ID] {
			seen[h.ID] = true
			out = append(out, h)
		}
	}
	if k := principal(r); k != nil {
		for _, wid := range k.WorkspaceIDs {
			source := SessionSource{Kind: "application", AppID: k.AppID}
			if m.checkApplicationPresetExecution(source, k.InstanceID, wid) == nil {
				add(k.InstanceID, wid, source, "")
			}
		}
		for _, binding := range k.PresetBindings {
			for _, p := range presets {
				if p.AppID == k.AppID && p.ID == binding.PresetID && p.Enabled && p.InstanceID == binding.InstanceID && p.WorkspaceID == binding.WorkspaceID {
					add(p.InstanceID, p.WorkspaceID, SessionSource{Kind: "application", AppID: p.AppID, PresetID: p.ID}, p.Name)
				}
			}
		}
	} else {
		// Dedicated preset resources must not accidentally be exposed as a
		// generic cross-product of instance and workspace.
		reservedInstances, reservedWorkspaces := map[string]bool{}, map[string]bool{}
		for _, p := range presets {
			reservedInstances[p.InstanceID], reservedWorkspaces[p.WorkspaceID] = true, true
			if p.Enabled {
				add(p.InstanceID, p.WorkspaceID, SessionSource{Kind: "application", AppID: p.AppID, PresetID: p.ID}, p.Name)
			}
		}
		for _, i := range m.Instances() {
			if reservedInstances[i.ID] {
				continue
			}
			for _, w := range m.Workspaces() {
				if !reservedWorkspaces[w.ID] {
					add(i.ID, w.ID, SessionSource{Kind: "human"}, "")
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].InstanceID == DefaultInstance) != (out[j].InstanceID == DefaultInstance) {
			return out[i].InstanceID == DefaultInstance
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Server) uhpModels(h uhpHarness) (map[string]any, error) {
	i, err := s.Manager.Instance(h.InstanceID)
	if err != nil {
		return nil, failure(404, "harness_not_found", "The configured harness is unavailable.")
	}
	models := []map[string]any{}
	defaultModel := h.DefaultModel
	basis := "native_model_catalog_and_account"
	if h.Base == "kun" {
		cfg := i.AgentRuntime.Normalized()
		_, executableErr := exec.LookPath(s.Manager.kunExecutable())
		ready := cfg.Validate() == nil && executableErr == nil && (cfg.APIKeyEnv == "" || os.Getenv(cfg.APIKeyEnv) != "") && !s.Manager.Demo
		if cfg.Model != "" {
			models = append(models, map[string]any{"id": cfg.Model, "label": cfg.Model, "backend": h.Base, "available": ready, "default": true})
		}
		defaultModel, basis = cfg.Model, "configured_model_worker_and_credential"
	} else {
		// A successful model-list request alone does not establish authentication.
		accountRaw, accountErr := s.Manager.ConfigCall(h.WorkspaceID, "account/read", map[string]any{"refreshToken": false}, h.InstanceID)
		var account struct {
			Account  json.RawMessage `json:"account"`
			Required *bool           `json:"requiresOpenaiAuth"`
		}
		ready := accountErr == nil && json.Unmarshal(accountRaw, &account) == nil && (len(account.Account) > 0 && string(account.Account) != "null" || account.Required != nil && !*account.Required)
		cursor := ""
		seenCursor, seenModel := map[string]bool{}, map[string]bool{}
		for page := 0; page < 100; page++ {
			params := map[string]any{"limit": 100}
			if cursor != "" {
				params["cursor"] = cursor
			}
			raw, callErr := s.Manager.ConfigCall(h.WorkspaceID, "model/list", params, h.InstanceID)
			var result struct {
				Data []struct {
					ID          string `json:"id"`
					Model       string `json:"model"`
					DisplayName string `json:"displayName"`
					IsDefault   bool   `json:"isDefault"`
					Hidden      bool   `json:"hidden"`
				} `json:"data"`
				NextCursor *string `json:"nextCursor"`
			}
			if callErr != nil || json.Unmarshal(raw, &result) != nil {
				break
			}
			for _, item := range result.Data {
				id := item.Model
				if id == "" {
					id = item.ID
				}
				if id == "" || seenModel[id] || item.Hidden {
					continue
				}
				seenModel[id] = true
				if defaultModel == "" && item.IsDefault {
					defaultModel = id
				}
				label := item.DisplayName
				if label == "" {
					label = id
				}
				models = append(models, map[string]any{"id": id, "label": label, "backend": h.Base, "available": ready, "default": false})
			}
			if result.NextCursor == nil || *result.NextCursor == "" || seenCursor[*result.NextCursor] {
				break
			}
			cursor = *result.NextCursor
			seenCursor[cursor] = true
		}
		if defaultModel != "" && !seenModel[defaultModel] {
			models = append(models, map[string]any{"id": defaultModel, "label": defaultModel, "backend": h.Base, "available": false, "default": true})
		}
		for _, model := range models {
			model["default"] = model["id"] == defaultModel
		}
	}
	return map[string]any{"harness_id": h.ID, "backend": h.Base, "default": defaultModel, "fallback": defaultModel, "models": models, "rundesk_availability_basis": basis}, nil
}

func (s *Server) uhpSelectModel(h uhpHarness, requested string) (string, error) {
	catalog, err := s.uhpModels(h)
	if err != nil {
		return "", err
	}
	model := requested
	if model == "" {
		model, _ = catalog["default"].(string)
	}
	for _, row := range catalog["models"].([]map[string]any) {
		if row["id"] == model && row["available"] == true {
			return model, nil
		}
	}
	return "", failure(422, "model_unavailable", "The requested model is not available for this harness.")
}
