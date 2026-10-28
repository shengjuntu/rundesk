package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/shengjuntu/rundesk/internal/store"
)

type CodexInstallInput struct {
	Version         string `json:"version"`
	Confirm         bool   `json:"confirm"`
	Method          string `json:"method,omitempty"`
	SourceGroup     string `json:"sourceGroup,omitempty"`
	CatalogRevision string `json:"catalogRevision,omitempty"`
	NPMSourceID     string `json:"npmSourceId,omitempty"`
	Registry        string `json:"registry,omitempty"`
	RequestID       string `json:"requestId,omitempty"`
}
type CodexInstallJob struct {
	ID        string             `json:"id"`
	Status    string             `json:"status"`
	Input     CodexInstallInput  `json:"input"`
	Result    CodexInstallResult `json:"result"`
	Platform  string             `json:"platform"`
	CreatedAt string             `json:"createdAt"`
	UpdatedAt string             `json:"updatedAt"`
}
type codexInstallRecord struct {
	CodexInstallJob
	RequestHash string `json:"requestHash,omitempty"`
}

// Running records survive a restart but process-local admission does not.
var activeCodexInstalls sync.Map
var codexInstallAdmission sync.Mutex

func codexInstallKey(m *Manager, id string) string { return m.Data + "/" + id }

func (m *Manager) normalizeCodexInstall(in CodexInstallInput) (CodexInstallInput, error) {
	in.Version = strings.TrimSpace(in.Version)
	in.Registry = strings.TrimSpace(in.Registry)
	if !in.Confirm || len(in.Version) > 64 || !codexRelease.MatchString(in.Version) {
		return in, failure(400, "invalid_install_request", "Choose an exact release and explicitly confirm installation")
	}
	if in.RequestID != "" && !buildCenterID.MatchString(in.RequestID) {
		return in, failure(400, "invalid_request_id", "requestId must contain 24 lowercase hexadecimal characters")
	}
	if in.Method == "" {
		in.Method = "auto"
	}
	if in.Method != "auto" && in.Method != "native" && in.Method != "npm" {
		return in, failure(400, "invalid_install_method", "Choose auto, native or npm installation")
	}
	if in.SourceGroup != "" || in.CatalogRevision != "" || in.NPMSourceID != "" {
		catalog, err := m.BuildSources()
		if err != nil {
			return in, err
		}
		if in.CatalogRevision != "" && in.CatalogRevision != catalog.Revision {
			return in, failure(409, "source_catalog_changed", "Download sources changed; reload them before installing")
		}
		if in.SourceGroup == "" {
			in.SourceGroup = catalog.DefaultGroup
		}
		foundGroup := false
		var chosen *BuildSourceEntry
		for _, group := range catalog.Groups {
			if group.ID == in.SourceGroup {
				foundGroup = true
			}
			for _, source := range group.Sources {
				if source.Type == "npm" && (in.NPMSourceID != "" && source.ID == in.NPMSourceID || in.NPMSourceID == "" && group.ID == in.SourceGroup && chosen == nil) {
					s := source
					chosen = &s
				}
			}
		}
		if !foundGroup {
			return in, failure(400, "invalid_source_group", "The selected download source group does not exist")
		}
		if chosen == nil {
			return in, failure(400, "npm_source_missing", "Choose an npm registry from the source catalog")
		}
		if in.Registry == "" {
			in.Registry = strings.TrimSpace(chosen.Values["registry"])
			if in.Registry == "" {
				return in, failure(400, "npm_registry_missing", "The selected npm source requires a registry URL")
			}
		}
		in.NPMSourceID, in.CatalogRevision = chosen.ID, catalog.Revision
	}
	if in.Registry == "" {
		in.Registry = "https://registry.npmjs.org"
	}
	in.Registry = strings.TrimRight(strings.TrimSpace(in.Registry), "/")
	if err := buildSourceValue("url", in.Registry); err != nil {
		return in, err
	}
	return in, nil
}

// StartCodexInstall returns immediately. Replaying a task-scoped requestId
// returns the original record even if the catalog has since been edited.
func (m *Manager) StartCodexInstall(in CodexInstallInput) (CodexInstallJob, error) {
	codexInstallAdmission.Lock()
	defer codexInstallAdmission.Unlock()
	fingerprint := buildAssistantDigest(in)
	id := store.ID()
	if in.RequestID != "" {
		if !buildCenterID.MatchString(in.RequestID) {
			return CodexInstallJob{}, failure(400, "invalid_request_id", "requestId must contain 24 lowercase hexadecimal characters")
		}
		id = buildAssistantDigest("codex-install:" + in.RequestID)[:24]
		var old codexInstallRecord
		if err := m.Store.Get("codex-install", id, &old); err == nil {
			if old.RequestHash != fingerprint {
				return CodexInstallJob{}, failure(409, "request_id_conflict", "requestId has already been used for another installation")
			}
			return m.CodexInstall(id)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return CodexInstallJob{}, err
		}
	}
	in, err := m.normalizeCodexInstall(in)
	if err != nil {
		return CodexInstallJob{}, err
	}
	if m.Demo {
		return CodexInstallJob{}, failure(400, "demo_install_disabled", "Demo mode does not install software")
	}
	if !m.installMu.TryLock() {
		return CodexInstallJob{}, failure(409, "install_busy", "A Codex installation is already running")
	}
	record := codexInstallRecord{CodexInstallJob: CodexInstallJob{ID: id, Status: "queued", Input: in, Result: CodexInstallResult{Version: in.Version, Method: in.Method, Registry: in.Registry}, Platform: runtime.GOOS + "/" + runtime.GOARCH, CreatedAt: store.Now(), UpdatedAt: store.Now()}, RequestHash: fingerprint}
	key := codexInstallKey(m, id)
	activeCodexInstalls.Store(key, true)
	if err = m.Store.Put("codex-install", id, record); err != nil {
		activeCodexInstalls.Delete(key)
		m.installMu.Unlock()
		return CodexInstallJob{}, err
	}
	response := record.CodexInstallJob
	go func() {
		defer m.installMu.Unlock()
		defer activeCodexInstalls.Delete(key)
		record.Status = "running"
		record.UpdatedAt = store.Now()
		_ = m.Store.Put("codex-install", id, record)
		// The normalized source snapshot is frozen at admission, so later catalog
		// changes cannot alter an in-flight attempt or its selected registry.
		record.Result = m.executeCodexInstall(m.ctx, in, func(log string) {
			record.Result.Log = log
			record.UpdatedAt = store.Now()
			_ = m.Store.Put("codex-install", id, record)
		})
		record.Status = "failed"
		if record.Result.OK {
			record.Status = "succeeded"
		} else if m.ctx.Err() != nil {
			record.Status = "interrupted"
		}
		record.UpdatedAt = store.Now()
		if err := m.Store.Put("codex-install", id, record); err == nil && record.Status == "succeeded" && record.Result.OK {
			// Only completed, version-verified attempts are eligible. The
			// repair owner verifies the failed source and task association.
			_, _ = m.recordSetupRepairInstallExperience(record.CodexInstallJob)
		}
	}()
	return response, nil
}
func (m *Manager) CodexInstall(id string) (CodexInstallJob, error) {
	var record codexInstallRecord
	if !buildCenterID.MatchString(id) {
		return record.CodexInstallJob, failure(400, "invalid_install_id", "Invalid installation ID")
	}
	if err := m.Store.Get("codex-install", id, &record); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return record.CodexInstallJob, failure(404, "install_not_found", "Codex installation not found")
		}
		return record.CodexInstallJob, err
	}
	if record.Status == "queued" || record.Status == "running" {
		if _, ok := activeCodexInstalls.Load(codexInstallKey(m, id)); !ok {
			// A reader can have loaded the running record immediately before
			// the worker persisted success and removed its active marker.
			// Reload after observing that marker, so it cannot replace a
			// confirmed terminal result with an interrupted state.
			if err := m.Store.Get("codex-install", id, &record); err != nil {
				return record.CodexInstallJob, err
			}
			if record.Status != "queued" && record.Status != "running" {
				return record.CodexInstallJob, nil
			}
			record.Status = "interrupted"
			record.Result.OK = false
			record.Result.Error = "RunDesk stopped before this installation was confirmed; inspect the log and start a new attempt"
			record.UpdatedAt = store.Now()
			if err := m.Store.Put("codex-install", id, record); err != nil {
				return record.CodexInstallJob, err
			}
		}
	}
	return record.CodexInstallJob, nil
}
func (m *Manager) CodexInstalls() ([]CodexInstallJob, error) {
	raw, err := m.Store.List("codex-install")
	if err != nil {
		return nil, err
	}
	jobs := []CodexInstallJob{}
	for _, data := range raw {
		var job CodexInstallJob
		if err = json.Unmarshal(data, &job); err != nil {
			return nil, err
		}
		job, err = m.CodexInstall(job.ID)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt > jobs[j].CreatedAt })
	if len(jobs) > 100 {
		jobs = jobs[:100]
	}
	return jobs, nil
}
func (m *Manager) installCodexWithOptions(parent context.Context, in CodexInstallInput) CodexInstallResult {
	normalized, err := m.normalizeCodexInstall(in)
	if err != nil {
		return CodexInstallResult{Version: in.Version, Error: err.Error()}
	}
	return m.executeCodexInstall(parent, normalized, nil)
}
func (m *Manager) executeCodexInstall(parent context.Context, in CodexInstallInput, progress func(string)) CodexInstallResult {
	if in.Method == "npm" {
		return m.installCodexNPM(parent, in.Version, in.Registry)
	}
	return m.installCodexNative(parent, in, progress)
}
func (s *Server) codexInstallJobRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/setup/codex-installs", func(w http.ResponseWriter, r *http.Request) {
		var in CodexInstallInput
		if !decode(w, r, &in) {
			return
		}
		job, err := s.Manager.StartCodexInstall(in)
		if err != nil {
			respond(w, nil, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	})
	mux.HandleFunc("GET /api/setup/codex-installs", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := s.Manager.CodexInstalls()
		respond(w, map[string]any{"items": jobs}, err)
	})
	mux.HandleFunc("GET /api/setup/codex-installs/{cid}", func(w http.ResponseWriter, r *http.Request) {
		job, err := s.Manager.CodexInstall(r.PathValue("cid"))
		respond(w, job, err)
	})
}
