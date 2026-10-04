package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"strings"
	"time"
)

type ApplicationKey struct {
	ID           string   `json:"id"`
	AppID        string   `json:"appId"`
	InstanceID   string   `json:"instanceId"`
	Name         string   `json:"name"`
	WorkspaceIDs []string `json:"workspaceIds"`
	Scopes       []string `json:"scopes"`
	Created      string   `json:"created"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
	RevokedAt    string   `json:"revokedAt,omitempty"`
}
type KeyInput struct {
	Name         string   `json:"name"`
	WorkspaceIDs []string `json:"workspaceIds"`
	Scopes       []string `json:"scopes"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
}
type keyRecord struct {
	Key  ApplicationKey `json:"key"`
	Hash string         `json:"hash"`
}

func keyHash(secret string) string {
	v := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(v[:])
}
func (k ApplicationKey) valid() bool {
	if k.RevokedAt != "" {
		return false
	}
	if k.ExpiresAt == "" {
		return true
	}
	t, e := time.Parse(time.RFC3339Nano, k.ExpiresAt)
	return e == nil && t.After(time.Now())
}
func (k ApplicationKey) scope(s string) bool {
	for _, v := range k.Scopes {
		if v == s {
			return true
		}
	}
	return false
}
func (k ApplicationKey) workspace(id string) bool {
	for _, v := range k.WorkspaceIDs {
		if v == id {
			return true
		}
	}
	return false
}
func (k ApplicationKey) owns(iid, wid string, source SessionSource) bool {
	return k.InstanceID == iid && k.workspace(wid) && source.Kind == "application" && source.AppID == k.AppID
}
func (m *Manager) ApplicationKeys(appID string) ([]ApplicationKey, error) {
	rows, e := m.Store.List("application-key")
	if e != nil {
		return nil, e
	}
	out := []ApplicationKey{}
	for _, b := range rows {
		var r keyRecord
		if e = json.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		if r.Key.AppID == appID {
			out = append(out, r.Key)
		}
	}
	return out, nil
}
func (m *Manager) CreateApplicationKey(appID string, in KeyInput) (ApplicationKey, string, error) {
	var a Application
	if e := m.Store.Get("application", appID, &a); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return ApplicationKey{}, "", failure(404, "application_not_found", "应用不存在")
		}
		return ApplicationKey{}, "", e
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 || len(in.WorkspaceIDs) < 1 || len(in.WorkspaceIDs) > 100 {
		return ApplicationKey{}, "", failure(400, "invalid_key", "请填写名称（最多 120 字符），选择 1–100 个项目")
	}
	seen := map[string]bool{}
	for _, id := range in.WorkspaceIDs {
		if seen[id] {
			return ApplicationKey{}, "", failure(400, "invalid_key", "项目重复")
		}
		seen[id] = true
		if _, e := m.Workspace(id); e != nil {
			return ApplicationKey{}, "", e
		}
	}
	scopes := map[string]bool{}
	for _, v := range in.Scopes {
		if v != "read" && v != "run" && v != "schedules" && v != "files" && v != "approvals" {
			return ApplicationKey{}, "", failure(400, "invalid_scope", "未知权限")
		}
		if scopes[v] {
			return ApplicationKey{}, "", failure(400, "invalid_scope", "权限重复")
		}
		scopes[v] = true
	}
	if !scopes["read"] || ((scopes["schedules"] || scopes["approvals"]) && !scopes["run"]) {
		return ApplicationKey{}, "", failure(400, "invalid_scope", "read 为必需；schedules 和 approvals 还需 run")
	}
	if in.ExpiresAt != "" {
		at, e := time.Parse(time.RFC3339Nano, in.ExpiresAt)
		if e != nil || !at.After(time.Now()) {
			return ApplicationKey{}, "", failure(400, "invalid_expiry", "有效期需为未来的 RFC3339 时间")
		}
		in.ExpiresAt = at.UTC().Format(time.RFC3339Nano)
	}
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	keys, e := m.ApplicationKeys(appID)
	if e != nil {
		return ApplicationKey{}, "", e
	}
	count := 0
	for _, k := range keys {
		if k.valid() {
			count++
		}
	}
	if count >= 100 {
		return ApplicationKey{}, "", failure(429, "key_limit", "每个应用最多 100 个有效凭据")
	}
	k := ApplicationKey{ID: store.ID(), AppID: appID, InstanceID: a.InstanceID, Name: in.Name, WorkspaceIDs: in.WorkspaceIDs, Scopes: in.Scopes, Created: store.Now(), ExpiresAt: in.ExpiresAt}
	secretBytes := make([]byte, 32)
	if _, e = rand.Read(secretBytes); e != nil {
		return k, "", e
	}
	secret := "rd_app_" + k.ID + "." + base64.RawURLEncoding.EncodeToString(secretBytes)
	if e = m.Store.Put("application-key", k.ID, keyRecord{Key: k, Hash: keyHash(secret)}); e != nil {
		return k, "", e
	}
	return k, secret, nil
}
func (m *Manager) RevokeApplicationKey(appID, id string) (ApplicationKey, error) {
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	var r keyRecord
	e := m.Store.Get("application-key", id, &r)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && r.Key.AppID != appID) {
		return r.Key, failure(404, "key_not_found", "凭据不存在")
	}
	if e != nil {
		return r.Key, e
	}
	if r.Key.RevokedAt == "" {
		r.Key.RevokedAt = store.Now()
		e = m.Store.Put("application-key", id, r)
	}
	return r.Key, e
}
func (m *Manager) authenticateApplication(secret string) (ApplicationKey, error) {
	invalid := failure(401, "unauthorized", "应用凭据无效、已撤销或已过期")
	if !strings.HasPrefix(secret, "rd_app_") {
		return ApplicationKey{}, invalid
	}
	id, _, ok := strings.Cut(strings.TrimPrefix(secret, "rd_app_"), ".")
	if !ok || len(id) != 24 {
		return ApplicationKey{}, invalid
	}
	var r keyRecord
	e := m.Store.Get("application-key", id, &r)
	if errors.Is(e, sql.ErrNoRows) {
		return r.Key, invalid
	}
	if e != nil {
		return r.Key, failure(503, "credential_storage_unavailable", "无法核对应用凭据")
	}
	if !same(keyHash(secret), r.Hash) || !r.Key.valid() {
		return r.Key, invalid
	}
	return r.Key, nil
}

type applicationPrincipal struct{}

func principal(r *http.Request) *ApplicationKey {
	k, _ := r.Context().Value(applicationPrincipal{}).(*ApplicationKey)
	return k
}
func (s *Server) authorize(r *http.Request) (*http.Request, error) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		secret, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok {
			return r, failure(401, "unauthorized", "需要 Bearer 凭据")
		}
		if s.Token != "" && same(secret, s.Token) {
			return r, nil
		}
		key, e := s.Manager.authenticateApplication(secret)
		if e != nil {
			return r, e
		}
		return r.WithContext(context.WithValue(r.Context(), applicationPrincipal{}, &key)), nil
	}
	if r.URL.Path == "/api/login" || s.Token == "" {
		return r, nil
	}
	c, e := r.Cookie("rundesk")
	if e == nil && same(c.Value, s.cookie) {
		return r, nil
	}
	return r, failure(401, "unauthorized", "需要登录")
}
func (s *Server) credentialStillValid(r *http.Request) bool {
	k := principal(r)
	if k == nil {
		return true
	}
	var v keyRecord
	if s.Manager.Store.Get("application-key", k.ID, &v) != nil {
		return false
	}
	return v.Key.valid()
}
func requestNamespace(r *http.Request, key string) string {
	if k := principal(r); k != nil {
		return "application:" + k.ID + ":" + key
	}
	return key
}
func sessionVisible(r *http.Request, s Session) bool {
	k := principal(r)
	return k == nil || k.owns(s.InstanceID, s.WorkspaceID, s.Source)
}
func taskVisible(r *http.Request, t Task) bool {
	k := principal(r)
	return k == nil || k.owns(t.Spec.InstanceID, t.Spec.WorkspaceID, t.Spec.Source)
}
func scheduleVisible(r *http.Request, s Schedule) bool {
	k := principal(r)
	return k == nil || k.owns(s.Spec.Task.InstanceID, s.Spec.Task.WorkspaceID, s.Spec.Task.Source)
}
func (s *Server) keyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/whoami", func(w http.ResponseWriter, r *http.Request) {
		if k := principal(r); k != nil {
			writeJSON(w, 200, map[string]any{"kind": "application", "credential": k})
		} else {
			writeJSON(w, 200, map[string]string{"kind": "administrator"})
		}
	})
	mux.HandleFunc("GET /api/applications/{appId}/keys", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.ApplicationKeys(r.PathValue("appId"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/applications/{appId}/keys", func(w http.ResponseWriter, r *http.Request) {
		var in KeyInput
		if !decode(w, r, &in) {
			return
		}
		k, token, e := s.Manager.CreateApplicationKey(r.PathValue("appId"), in)
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		writeJSON(w, 201, map[string]any{"credential": k, "token": token})
	})
	mux.HandleFunc("DELETE /api/applications/{appId}/keys/{keyId}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.RevokeApplicationKey(r.PathValue("appId"), r.PathValue("keyId"))
		respond(w, v, e)
	})
}
