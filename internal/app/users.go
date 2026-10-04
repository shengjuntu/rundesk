package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

type UserGrant struct {
	ID          string `json:"id"`
	AppID       string `json:"appId"`
	WorkspaceID string `json:"workspaceId"`
	Role        string `json:"role"`
}
type User struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Enabled  bool        `json:"enabled"`
	Revision int         `json:"revision"`
	Grants   []UserGrant `json:"grants"`
	Created  string      `json:"created"`
	Updated  string      `json:"updated"`
}
type UserInput struct {
	Name     string      `json:"name"`
	Enabled  bool        `json:"enabled"`
	Grants   []UserGrant `json:"grants"`
	Revision int         `json:"revision"`
}
type userRecord struct {
	User User   `json:"user"`
	Hash string `json:"hash"`
}
type userLogin struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	Revision int    `json:"revision"`
	Hash     string `json:"hash"`
	Expires  string `json:"expires"`
}
type userContext struct{}
type userIdentity struct {
	User    User
	LoginID string
	Expires time.Time
}

func human(r *http.Request) *userIdentity {
	v, _ := r.Context().Value(userContext{}).(*userIdentity)
	return v
}
func withHuman(r *http.Request, p *userIdentity) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userContext{}, p))
}
func (m *Manager) Users() ([]User, error) {
	rows, e := m.Store.List("user")
	if e != nil {
		return nil, e
	}
	out := []User{}
	for _, b := range rows {
		var rec userRecord
		if e = json.Unmarshal(b, &rec); e != nil {
			return nil, e
		}
		out = append(out, rec.User)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created < out[b].Created })
	return out, nil
}
func (m *Manager) validateUser(in *UserInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 || len(in.Grants) > 100 {
		return failure(400, "invalid_user", "需要用户名称（最多 120 字符），最多授权 100 个应用项目")
	}
	if in.Grants == nil {
		in.Grants = []UserGrant{}
	}
	seen := map[string]bool{}
	for n := range in.Grants {
		g := &in.Grants[n]
		g.ID = keyHash(g.AppID + "/" + g.WorkspaceID)[:24]
		if seen[g.ID] || g.Role != "viewer" && g.Role != "runner" {
			return failure(400, "invalid_grant", "同一应用项目不能重复授权；角色只能为 viewer 或 runner")
		}
		seen[g.ID] = true
		var a Application
		if e := m.Store.Get("application", g.AppID, &a); e != nil {
			return failure(400, "invalid_grant", "授权的应用不存在")
		}
		i, e := m.Instance(a.InstanceID)
		if e != nil {
			return e
		}
		if i.Execution.Mode != "docker" {
			return failure(400, "docker_grant_required", "普通用户只能被授权到 Docker 应用项目")
		}
		found := false
		for _, v := range m.Environments(a.InstanceID) {
			if v.WorkspaceID == g.WorkspaceID {
				found = true
				break
			}
		}
		if !found {
			return failure(400, "environment_required", "请先为此应用创建项目环境，再分配给用户")
		}
	}
	return nil
}
func newUserSecret(prefix, id string) (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return prefix + id + "." + base64.RawURLEncoding.EncodeToString(b), nil
}
func (m *Manager) CreateUser(in UserInput) (User, string, error) {
	m.userMu.Lock()
	defer m.userMu.Unlock()
	if e := m.validateUser(&in); e != nil {
		return User{}, "", e
	}
	users, e := m.Users()
	if e != nil {
		return User{}, "", e
	}
	if len(users) >= 500 {
		return User{}, "", failure(409, "user_limit", "最多创建 500 个用户，暂不支持删除账号历史")
	}
	u := User{ID: store.ID(), Name: in.Name, Enabled: true, Revision: 1, Grants: in.Grants, Created: store.Now(), Updated: store.Now()}
	code, e := newUserSecret("rd_user_", u.ID)
	if e != nil {
		return u, "", e
	}
	if e = m.Store.Put("user", u.ID, userRecord{User: u, Hash: keyHash(code)}); e != nil {
		return u, "", e
	}
	return u, code, nil
}
func (m *Manager) UpdateUser(id string, in UserInput) (User, error) {
	m.userMu.Lock()
	defer m.userMu.Unlock()
	var rec userRecord
	e := m.Store.Get("user", id, &rec)
	if errors.Is(e, sql.ErrNoRows) {
		return User{}, failure(404, "user_not_found", "用户不存在")
	}
	if e != nil {
		return User{}, e
	}
	if rec.User.Revision != in.Revision {
		return rec.User, failure(409, "revision_conflict", "用户授权已变化，请刷新")
	}
	// A pure disable must work even if the application is no longer Docker.
	pureDisable := !in.Enabled && in.Name == rec.User.Name && reflect.DeepEqual(in.Grants, rec.User.Grants)
	if !pureDisable {
		if e = m.validateUser(&in); e != nil {
			return rec.User, e
		}
	}
	rec.User.Name = in.Name
	rec.User.Grants = in.Grants
	rec.User.Enabled = in.Enabled
	rec.User.Revision++
	rec.User.Updated = store.Now()
	return rec.User, m.Store.Put("user", id, rec)
}
func (m *Manager) RotateUserCode(id string, revision int) (User, string, error) {
	m.userMu.Lock()
	defer m.userMu.Unlock()
	var rec userRecord
	e := m.Store.Get("user", id, &rec)
	if errors.Is(e, sql.ErrNoRows) {
		return User{}, "", failure(404, "user_not_found", "用户不存在")
	}
	if e != nil {
		return User{}, "", e
	}
	if rec.User.Revision != revision {
		return rec.User, "", failure(409, "revision_conflict", "用户授权已变化，请刷新")
	}
	code, e := newUserSecret("rd_user_", id)
	if e != nil {
		return rec.User, "", e
	}
	rec.Hash = keyHash(code)
	rec.User.Revision++
	rec.User.Updated = store.Now()
	return rec.User, code, m.Store.Put("user", id, rec)
}
func secretID(value, prefix string) string {
	if !strings.HasPrefix(value, prefix) || len(value) > 256 {
		return ""
	}
	id, _, ok := strings.Cut(strings.TrimPrefix(value, prefix), ".")
	if !ok || len(id) != 24 {
		return ""
	}
	return id
}
func (m *Manager) authenticateUser(secret string) (*userIdentity, error) {
	invalid := failure(401, "unauthorized", "个人访问码无效或用户已停用")
	id := secretID(secret, "rd_user_")
	if id == "" {
		return nil, invalid
	}
	var rec userRecord
	e := m.Store.Get("user", id, &rec)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, invalid
	}
	if e != nil {
		return nil, failure(503, "credential_storage_unavailable", "无法核对用户信息")
	}
	if !rec.User.Enabled || !same(keyHash(secret), rec.Hash) {
		return nil, invalid
	}
	return &userIdentity{User: rec.User}, nil
}
func (m *Manager) authenticateUserCookie(secret string) (*userIdentity, error) {
	invalid := failure(401, "unauthorized", "登录已过期或授权已变化，请重新登录")
	id := secretID(secret, "rd_browser_")
	if id == "" {
		return nil, invalid
	}
	var login userLogin
	e := m.Store.Get("user-login", id, &login)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, invalid
	}
	if e != nil {
		return nil, failure(503, "credential_storage_unavailable", "无法核对登录")
	}
	expires, e := time.Parse(time.RFC3339Nano, login.Expires)
	if e != nil || !expires.After(time.Now()) || !same(keyHash(secret), login.Hash) {
		return nil, invalid
	}
	var rec userRecord
	if e = m.Store.Get("user", login.UserID, &rec); e != nil || !rec.User.Enabled || rec.User.Revision != login.Revision {
		return nil, invalid
	}
	return &userIdentity{User: rec.User, LoginID: login.ID, Expires: expires}, nil
}
func (m *Manager) loginUser(secret string) (string, error) {
	m.userMu.Lock()
	defer m.userMu.Unlock()
	p, e := m.authenticateUser(secret)
	if e != nil {
		return "", e
	}
	rows, e := m.Store.List("user-login")
	if e != nil {
		return "", e
	}
	count := 0
	for _, b := range rows {
		var v userLogin
		if e = json.Unmarshal(b, &v); e != nil {
			return "", e
		}
		at, _ := time.Parse(time.RFC3339Nano, v.Expires)
		if !at.After(time.Now()) || (v.UserID == p.User.ID && v.Revision != p.User.Revision) {
			if e = m.Store.Delete("user-login", v.ID); e != nil {
				return "", e
			}
		} else if v.UserID == p.User.ID {
			count++
		}
	}
	if count >= 20 {
		return "", failure(429, "login_limit", "有效登录过多，请退出旧设备，或请管理员重置访问码")
	}
	id := store.ID()
	cookie, e := newUserSecret("rd_browser_", id)
	if e != nil {
		return "", e
	}
	e = m.Store.Put("user-login", id, userLogin{ID: id, UserID: p.User.ID, Revision: p.User.Revision, Hash: keyHash(cookie), Expires: time.Now().Add(12 * time.Hour).UTC().Format(time.RFC3339Nano)})
	return cookie, e
}
func (s *Server) userStillValid(r *http.Request) bool {
	p := human(r)
	if p == nil {
		return true
	}
	var rec userRecord
	if s.Manager.Store.Get("user", p.User.ID, &rec) != nil || !rec.User.Enabled || rec.User.Revision != p.User.Revision {
		return false
	}
	if p.LoginID != "" {
		var login userLogin
		if !p.Expires.After(time.Now()) || s.Manager.Store.Get("user-login", p.LoginID, &login) != nil {
			return false
		}
	}
	return true
}
func (m *Manager) memberCatalog(u User) ([]map[string]any, error) {
	out := []map[string]any{}
	for _, g := range u.Grants {
		var a Application
		if e := m.Store.Get("application", g.AppID, &a); e != nil {
			continue
		}
		w, e := m.Workspace(g.WorkspaceID)
		if e != nil {
			continue
		}
		i, e := m.Instance(a.InstanceID)
		if e != nil {
			continue
		}
		out = append(out, map[string]any{"grant": g, "applicationName": a.Name, "projectName": w.Name, "canRun": g.Role == "runner" && i.Execution.Mode == "docker"})
	}
	return out, nil
}
func (s *Server) userRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/users", func(w http.ResponseWriter, r *http.Request) { v, e := m.Users(); respond(w, v, e) })
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		if s.Token == "" {
			respond(w, nil, failure(409, "administrator_token_required", "启用多用户前必须配置 RUNDESK_TOKEN"))
			return
		}
		var in UserInput
		if !decode(w, r, &in) {
			return
		}
		u, code, e := m.CreateUser(in)
		respond(w, map[string]any{"user": u, "accessCode": code}, e)
	})
	mux.HandleFunc("PUT /api/users/{uid}", func(w http.ResponseWriter, r *http.Request) {
		var in UserInput
		if !decode(w, r, &in) {
			return
		}
		u, e := m.UpdateUser(r.PathValue("uid"), in)
		respond(w, u, e)
	})
	mux.HandleFunc("POST /api/users/{uid}/access-code", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		u, code, e := m.RotateUserCode(r.PathValue("uid"), in.Revision)
		respond(w, map[string]any{"user": u, "accessCode": code}, e)
	})
	mux.HandleFunc("GET /api/member/catalog", func(w http.ResponseWriter, r *http.Request) {
		p := human(r)
		if p == nil {
			respond(w, nil, failure(403, "user_required", "请使用个人访问码登录"))
			return
		}
		v, e := m.memberCatalog(p.User)
		respond(w, map[string]any{"user": p.User, "projects": v}, e)
	})
}

// Member URLs reuse the existing application authorization and handlers. Every
// request is constrained to exactly one administrator-approved app/project pair.
func (s *Server) userGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := human(r)
		if p == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if (r.Method == "GET" && (r.URL.Path == "/api/whoami" || r.URL.Path == "/api/meta" || r.URL.Path == "/api/member/catalog")) || (r.Method == "POST" && r.URL.Path == "/api/logout") {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == "DELETE" || r.Method == "PUT" || r.Method == "PATCH" {
			writeErr(w, 403, failure(403, "user_forbidden", "成员不能删除历史或修改运行配置"))
			return
		}
		deny := func() {
			writeErr(w, 403, failure(403, "user_forbidden", "当前用户没有此应用项目的访问权限"))
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
		if len(parts) < 3 || parts[0] != "member" {
			deny()
			return
		}
		var grant *UserGrant
		for _, g := range p.User.Grants {
			if g.ID == parts[1] {
				v := g
				grant = &v
				break
			}
		}
		if grant == nil {
			deny()
			return
		}
		if parts[2] != "sessions" && parts[2] != "workspaces" && parts[2] != "requests" {
			deny()
			return
		}
		var a Application
		if s.Manager.Store.Get("application", grant.AppID, &a) != nil {
			deny()
			return
		}
		i, e := s.Manager.Instance(a.InstanceID)
		if e != nil {
			deny()
			return
		}
		scopes := []string{"read", "files"}
		if grant.Role == "runner" && i.Execution.Mode == "docker" {
			scopes = append(scopes, "run", "approvals")
		}
		// read-only members must not upload through the legacy files scope.
		if grant.Role != "runner" && r.Method != "GET" {
			deny()
			return
		}
		if i.Execution.Mode != "docker" && r.Method != "GET" {
			deny()
			return
		}
		key := ApplicationKey{ID: "user-" + p.User.ID + "-" + grant.ID + "-" + strconv.Itoa(p.User.Revision), AppID: a.AppID, InstanceID: a.InstanceID, WorkspaceIDs: []string{grant.WorkspaceID}, Scopes: scopes}
		r = r.Clone(context.WithValue(r.Context(), applicationPrincipal{}, &key))
		u := *r.URL
		r.URL = &u
		r.URL.Path = "/api/" + strings.Join(parts[2:], "/")
		r.URL.RawPath = ""
		next.ServeHTTP(w, r)
	})
}
