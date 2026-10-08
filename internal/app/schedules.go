package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/robfig/cron/v3"
	"github.com/shengjuntu/rundesk/internal/store"
	"strings"
	"time"
	_ "time/tzdata"
)

type ScheduleSpec struct {
	Name     string   `json:"name"`
	Cron     string   `json:"cron"`
	Timezone string   `json:"timezone"`
	Enabled  bool     `json:"enabled"`
	Misfire  string   `json:"misfire"`
	Overlap  string   `json:"overlap"`
	Task     TaskSpec `json:"task"`
}
type Schedule struct {
	SubmittingKeyID string       `json:"submittingKeyId,omitempty"`
	ID              string       `json:"id"`
	Revision        int          `json:"revision"`
	Spec            ScheduleSpec `json:"spec"`
	Created         string       `json:"created"`
	Updated         string       `json:"updated"`
	NextAt          string       `json:"nextAt"`
	LastAt          string       `json:"lastAt,omitempty"`
	LastTaskID      string       `json:"lastTaskId,omitempty"`
	LastReason      string       `json:"lastReason,omitempty"`
}

func parseSchedule(expr, zone string) (cron.Schedule, error) {
	if len(strings.Fields(expr)) != 5 || strings.ContainsAny(expr, "@=") {
		return nil, failure(400, "invalid_cron", "使用五段 Cron：分 时 日 月 周，不支持秒或 @every")
	}
	if zone == "" || zone == "Local" {
		return nil, failure(400, "invalid_timezone", "请明确指定 IANA 时区，例如 Asia/Shanghai")
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return nil, failure(400, "invalid_timezone", "未知时区")
	}
	c, e := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expr)
	if e != nil {
		return nil, failure(400, "invalid_cron", e.Error())
	}
	c.(*cron.SpecSchedule).Location = loc
	if c.Next(time.Now()).IsZero() {
		return nil, failure(400, "invalid_cron", "未来五年没有执行时间")
	}
	return c, nil
}
func (m *Manager) loadSchedules() error {
	m.schedules = map[string]Schedule{}
	rows, e := m.Store.List("schedule")
	if e != nil {
		return e
	}
	for _, b := range rows {
		var s Schedule
		if e = json.Unmarshal(b, &s); e != nil {
			return e
		}
		if _, e = parseSchedule(s.Spec.Cron, s.Spec.Timezone); e != nil {
			return e
		}
		m.schedules[s.ID] = s
	}
	return nil
}
func (m *Manager) Schedules() []Schedule {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	out := []Schedule{}
	for _, s := range m.schedules {
		out = append(out, s)
	}
	return out
}
func (m *Manager) Schedule(id string) (Schedule, error) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	s, ok := m.schedules[id]
	if !ok {
		return s, failure(404, "schedule_not_found", "定时任务不存在")
	}
	return s, nil
}
func (m *Manager) SaveSchedule(id string, spec ScheduleSpec, revision int) (Schedule, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" || len([]rune(spec.Name)) > 120 {
		return Schedule{}, failure(400, "invalid_schedule", "名称需为 1–120 字符")
	}
	c, e := parseSchedule(spec.Cron, spec.Timezone)
	if e != nil {
		return Schedule{}, e
	}
	if spec.Misfire == "" {
		spec.Misfire = "skip"
	}
	if spec.Overlap == "" {
		spec.Overlap = "skip"
	}
	if (spec.Misfire != "skip" && spec.Misfire != "once") || (spec.Overlap != "skip" && spec.Overlap != "queue") || spec.Task.NotBefore != "" {
		return Schedule{}, failure(400, "invalid_schedule", "misfire 需为 skip/once，overlap 需为 skip/queue，模板不能含 notBefore")
	}
	t, _, e := m.prepareTask(spec.Task, true)
	if e != nil {
		return Schedule{}, e
	}
	spec.Task = t.Spec
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	s, ok := m.schedules[id]
	if id != "" && !ok {
		return s, failure(404, "schedule_not_found", "定时任务不存在")
	}
	if ok && revision != s.Revision {
		return s, failure(409, "revision_conflict", "定时任务已更新，请刷新")
	}
	if !ok {
		if len(m.schedules) >= 500 {
			return s, failure(429, "schedule_limit", "最多 500 项定时任务")
		}
		s = Schedule{ID: store.ID(), Created: store.Now()}
	}
	if spec.Enabled {
		if !ok || !s.Spec.Enabled || s.Spec.Cron != spec.Cron || s.Spec.Timezone != spec.Timezone {
			s.NextAt = c.Next(time.Now()).UTC().Format(time.RFC3339Nano)
		}
	} else {
		s.NextAt = ""
	}
	// An administrator editing timing does not silently replace the owner.
	// An application can re-authorize by saving with its new valid key.
	if t.SubmittingKeyID != "" {
		s.SubmittingKeyID = t.SubmittingKeyID
	}
	s.Spec = spec
	s.Revision++
	s.Updated = store.Now()
	if e = m.Store.Put("schedule", s.ID, s); e != nil {
		return s, e
	}
	m.schedules[s.ID] = s
	return s, nil
}
func (m *Manager) DeleteSchedule(id string, revision int) error {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	s, ok := m.schedules[id]
	if !ok {
		return failure(404, "schedule_not_found", "定时任务不存在")
	}
	if s.Revision != revision {
		return failure(409, "revision_conflict", "定时任务已更新，请刷新")
	}
	if e := m.Store.Delete("schedule", id); e != nil {
		return e
	}
	delete(m.schedules, id)
	return nil
}
func (m *Manager) scheduleTick(now time.Time) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	for _, s := range m.schedules {
		if m.ctx.Err() != nil {
			return
		}
		if !s.Spec.Enabled || s.NextAt == "" {
			continue
		}
		due, e := time.Parse(time.RFC3339Nano, s.NextAt)
		if e != nil || due.After(now) {
			continue
		}
		if authErr := m.checkTaskAuthorization(s.SubmittingKeyID, s.Spec.Task, true); authErr != nil {
			// Storage failures are fail-closed but do not permanently disable a
			// schedule. Retry the authorization check on a later tick.
			var ae *apiError
			if errors.As(authErr, &ae) && ae.Code == "task_authorization_revoked" {
				s.Spec.Enabled = false
				s.NextAt = ""
				s.Revision++
			}
			s.LastReason = authErr.Error()
			s.Updated = store.Now()
			if e = m.Store.Put("schedule", s.ID, s); e != nil {
				m.cancel()
				return
			}
			m.schedules[s.ID] = s
			continue
		}
		c, e := parseSchedule(s.Spec.Cron, s.Spec.Timezone)
		if e != nil {
			continue
		}
		previous := s.NextAt
		s.LastAt = previous
		s.Updated = store.Now()
		s.LastReason = ""
		next := c.Next(now)
		if next.IsZero() {
			s.Spec.Enabled = false
			s.NextAt = ""
			s.LastReason = "未来五年没有下一次执行，已停用"
		} else {
			s.NextAt = next.UTC().Format(time.RFC3339Nano)
		}
		if now.Sub(due) >= time.Minute && s.Spec.Misfire == "skip" {
			s.LastReason = "已跳过错过的执行时间"
			if e = m.Store.Put("schedule", s.ID, s); e != nil {
				m.cancel()
				return
			}
			m.schedules[s.ID] = s
			continue
		}
		// prepareSession may consult application configuration; never hold queueMu during validation RPC.
		spec := s.Spec.Task
		spec.SubmittingKeyID = s.SubmittingKeyID
		t, session, e := m.prepareTask(spec, false)
		if e != nil {
			s.LastReason = e.Error()
			if e = m.Store.Put("schedule", s.ID, s); e != nil {
				m.cancel()
				return
			}
			m.schedules[s.ID] = s
			continue
		}
		t.ID = fmt.Sprintf("cron-%x", sha256.Sum256([]byte(s.ID+"\n"+due.UTC().Format(time.RFC3339Nano))))
		session.TaskID = t.ID
		t.ScheduleID = s.ID
		t.ScheduledFor = previous
		m.queueMu.Lock()
		if existing, ok := m.tasks[t.ID]; ok {
			s.LastTaskID = existing.ID
			s.LastReason = "执行时间已提交"
		} else {
			if s.Spec.Overlap == "skip" {
				for _, old := range m.tasks {
					if old.ScheduleID != s.ID {
						continue
					}
					pending := taskPending(old.Status)
					if pending && old.RunID != "" {
						if actual, err := m.Session(old.SessionID); err == nil && actual.RunID == old.RunID && !active(actual.Status) {
							pending = false
						}
					}
					if pending || old.Status == "unconfirmed" {
						s.LastReason = "上一项仍在等待、执行或待核对，已跳过"
						break
					}
				}
			}
			if m.pendingCountLocked() >= 1000 {
				s.LastReason = "队列已满，已跳过"
			}
			if s.LastReason == "" {
				s.LastTaskID = t.ID
				e = m.Store.PutMany(store.Record{Kind: "schedule", ID: s.ID, Value: s}, store.Record{Kind: "task", ID: t.ID, Value: t}, store.Record{Kind: "session", ID: session.ID, Value: session})
				if e == nil {
					m.tasks[t.ID] = t
					m.mu.Lock()
					m.sessions[session.ID] = &session
					m.mu.Unlock()
				}
			}
		}
		if s.LastReason != "" {
			e = m.Store.Put("schedule", s.ID, s)
		}
		m.queueMu.Unlock()
		if e != nil {
			m.cancel()
			return
		}
		m.schedules[s.ID] = s
	}
}
