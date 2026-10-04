package app

import (
	"encoding/json"
	"github.com/shengjuntu/rundesk/internal/store"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Product metadata maps each app to an existing configuration identity.
// Execution, sessions, credentials and workspace ownership remain unchanged.
type Application struct {
	AppID        string `json:"appId"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	InstanceID   string `json:"instanceId"`
	WorkspaceID  string `json:"workspaceId,omitempty"`
	EntryURL     string `json:"entryUrl,omitempty"`
	Origin       string `json:"origin"`
	Revision     int    `json:"revision"`
	Created      string `json:"created"`
	SessionCount int    `json:"sessionCount"`
	ActiveCount  int    `json:"activeCount"`
	LastActivity string `json:"lastActivity,omitempty"`
}
type ApplicationInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
	EntryURL    string `json:"entryUrl"`
	Revision    *int   `json:"revision,omitempty"`
}

var applicationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,79}$`)

func (m *Manager) applicationRecords() ([]Application, error) {
	rows, e := m.Store.List("application")
	if e != nil {
		return nil, e
	}
	out := []Application{}
	for _, b := range rows {
		var a Application
		if e = json.Unmarshal(b, &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}
func (m *Manager) Applications() ([]Application, error) {
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	rows, e := m.applicationRecords()
	if e != nil {
		return nil, e
	}
	for n := range rows {
		for _, s := range m.Sessions() {
			if s.InstanceID != rows[n].InstanceID {
				continue
			}
			rows[n].SessionCount++
			if active(s.Status) {
				rows[n].ActiveCount++
			}
			if s.Updated > rows[n].LastActivity {
				rows[n].LastActivity = s.Updated
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Created == rows[j].Created {
			return rows[i].AppID < rows[j].AppID
		}
		return rows[i].Created < rows[j].Created
	})
	return rows, nil
}
func validEntryURL(v string) bool {
	if v == "" {
		return true
	}
	u, e := url.Parse(v)
	return e == nil && len(v) <= 2048 && !strings.ContainsAny(v, "\r\n\x00") && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}
func (m *Manager) RegisterApplication(id string, p ApplicationInput) (Application, error) {
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	return m.registerApplication(id, p, "registered")
}

// Caller holds applicationMu. Identical retries are reads, even with an old revision.
func (m *Manager) registerApplication(id string, p ApplicationInput, origin string) (Application, error) {
	if !applicationID.MatchString(id) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 || len(p.Description) > 4096 || !validEntryURL(p.EntryURL) {
		return Application{}, failure(400, "invalid_application", "应用需要有效编号、名称和不带登录凭据的 HTTP(S) 入口")
	}
	if p.InstanceID == "" || p.InstanceID == DefaultInstance {
		return Application{}, failure(409, "assistant_reserved", "通用助手的默认配置不能登记为应用，请使用专用配置")
	}
	if _, e := m.Instance(p.InstanceID); e != nil {
		return Application{}, e
	}
	if p.WorkspaceID != "" {
		if _, e := m.Workspace(p.WorkspaceID); e != nil {
			return Application{}, e
		}
	}
	rows, e := m.applicationRecords()
	if e != nil {
		return Application{}, e
	}
	var prev *Application
	for _, a := range rows {
		if a.AppID == id {
			v := a
			prev = &v
		}
		if a.InstanceID == p.InstanceID && a.AppID != id {
			return Application{}, failure(409, "application_binding_conflict", "此配置已属于另一个应用")
		}
	}
	next := Application{AppID: id, Name: strings.TrimSpace(p.Name), Description: p.Description, InstanceID: p.InstanceID, WorkspaceID: p.WorkspaceID, EntryURL: p.EntryURL, Origin: origin, Created: store.Now()}
	if prev != nil {
		if prev.InstanceID != p.InstanceID {
			return Application{}, failure(409, "application_binding_conflict", "应用已绑定其他配置，不会自动迁移历史会话")
		}
		if prev.Name == next.Name && prev.Description == next.Description && prev.WorkspaceID == next.WorkspaceID && prev.EntryURL == next.EntryURL {
			return *prev, nil
		}
		if p.Revision == nil || *p.Revision != prev.Revision {
			return Application{}, failure(409, "revision_conflict", "应用信息已变化，请刷新后保存")
		}
		next.Created = prev.Created
		next.Origin = prev.Origin
		next.Revision = prev.Revision + 1
	}
	if e = m.Store.Put("application", id, next); e != nil {
		return Application{}, e
	}
	return next, nil
}
func (m *Manager) bindApplicationSource(source SessionSource, i Instance, wid string) error {
	if source.Kind != "application" {
		return nil
	}
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	rows, e := m.applicationRecords()
	if e != nil {
		return e
	}
	for _, a := range rows {
		if a.AppID == source.AppID {
			if a.InstanceID != i.ID {
				return failure(409, "application_binding_conflict", "应用来源与登记的配置不一致")
			}
			return nil
		}
		if a.InstanceID == i.ID {
			return failure(409, "application_binding_conflict", "此配置已绑定另一个应用")
		}
	}
	if i.ID == DefaultInstance || !applicationID.MatchString(source.AppID) {
		return nil
	}
	for _, s := range m.Sessions() {
		if s.Source.Kind == "application" && (s.Source.AppID == source.AppID && s.InstanceID != i.ID || s.InstanceID == i.ID && s.Source.AppID != source.AppID) {
			return nil
		}
	}
	_, e = m.registerApplication(source.AppID, ApplicationInput{Name: i.Name, Description: i.Description, InstanceID: i.ID, WorkspaceID: wid}, "session-source")
	return e
}
func (m *Manager) migrateApplications() error {
	groups := map[string]map[string]bool{}
	owners := map[string]map[string]bool{}
	work := map[string]string{}
	ss := m.Sessions()
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].Updated == ss[j].Updated {
			return ss[i].ID < ss[j].ID
		}
		return ss[i].Updated < ss[j].Updated
	})
	for _, s := range ss {
		if s.Source.Kind != "application" || s.Source.AppID == "" {
			continue
		}
		id := s.Source.AppID
		if groups[id] == nil {
			groups[id] = map[string]bool{}
		}
		groups[id][s.InstanceID] = true
		if owners[s.InstanceID] == nil {
			owners[s.InstanceID] = map[string]bool{}
		}
		owners[s.InstanceID][id] = true
		work[id] = s.WorkspaceID
	}
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	rows, e := m.applicationRecords()
	if e != nil {
		return e
	}
	registered := map[string]bool{}
	bound := map[string]bool{}
	for _, a := range rows {
		registered[a.AppID] = true
		bound[a.InstanceID] = true
	}
	ids := []string{}
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if registered[id] || len(groups[id]) != 1 || !applicationID.MatchString(id) {
			continue
		}
		for iid := range groups[id] {
			if iid == DefaultInstance || len(owners[iid]) != 1 || bound[iid] {
				continue
			}
			i, e := m.Instance(iid)
			if e != nil {
				return e
			}
			if _, e = m.registerApplication(id, ApplicationInput{Name: i.Name, Description: i.Description, InstanceID: iid, WorkspaceID: work[id]}, "session-migration"); e != nil {
				return e
			}
			bound[iid] = true
		}
	}
	return nil
}
func (s *Server) applicationRoutes(mux *http.ServeMux) {
	s.applicationConnectRoutes(mux)
	s.applicationCapabilityRoutes(mux)
	mux.HandleFunc("GET /api/applications", func(w http.ResponseWriter, r *http.Request) { v, e := s.Manager.Applications(); respond(w, v, e) })
	mux.HandleFunc("GET /api/applications/{appId}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.Applications()
		if e != nil {
			respond(w, nil, e)
			return
		}
		for _, a := range v {
			if a.AppID == r.PathValue("appId") {
				writeJSON(w, 200, a)
				return
			}
		}
		writeErr(w, 404, failure(404, "application_not_found", "应用尚未登记"))
	})
	mux.HandleFunc("PUT /api/applications/{appId}", func(w http.ResponseWriter, r *http.Request) {
		var p ApplicationInput
		if !decode(w, r, &p) {
			return
		}
		v, e := s.Manager.RegisterApplication(r.PathValue("appId"), p)
		respond(w, v, e)
	})
}
