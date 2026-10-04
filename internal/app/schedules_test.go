package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScheduleTimezoneDSTAndValidation(t *testing.T) {
	for _, v := range []struct{ expr, zone, after, want string }{
		{"0 9 * * *", "Asia/Shanghai", "2027-01-01T00:00:00Z", "2027-01-01T01:00:00Z"},
		{"30 2 * * *", "America/New_York", "2027-03-14T05:00:00Z", "2027-03-15T06:30:00Z"},
		{"30 1 * * *", "America/New_York", "2027-11-07T05:30:00Z", "2027-11-07T06:30:00Z"},
	} {
		c, e := parseSchedule(v.expr, v.zone)
		if e != nil {
			t.Fatal(e)
		}
		at, _ := time.Parse(time.RFC3339, v.after)
		if got := c.Next(at).UTC().Format(time.RFC3339); got != v.want {
			t.Fatal(got, v.want)
		}
	}
	for _, v := range [][2]string{{"* * * * * *", "UTC"}, {"@every 1h", "UTC"}, {"0 0 31 2 *", "UTC"}, {"0 0 * * *", "Local"}, {"0 0 * * *", "Mars/Moon"}} {
		if _, e := parseSchedule(v[0], v[1]); e == nil {
			t.Fatal(v)
		}
	}
}
func TestSchedulesAtomicOccurrenceMisfireAndOverlap(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	spec := ScheduleSpec{Name: "daily", Cron: "* * * * *", Timezone: "UTC", Enabled: true, Misfire: "once", Overlap: "skip", Task: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "news"}}}
	s, e := m.SaveSchedule("", spec, 0)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	m.scheduleMu.Lock()
	s.NextAt = at.Format(time.RFC3339)
	m.schedules[s.ID] = s
	m.Store.Put("schedule", s.ID, s)
	m.scheduleMu.Unlock()
	m.scheduleTick(at.Add(5 * time.Minute))
	if len(m.Tasks()) != 1 {
		t.Fatal("missed executions flooded or lost")
	}
	first := m.Tasks()[0]
	if first.ScheduledFor != s.NextAt {
		t.Fatal(first)
	}
	m.scheduleTick(at.Add(5 * time.Minute))
	if len(m.Tasks()) != 1 {
		t.Fatal("duplicated occurrence")
	}
	m.scheduleTick(at.Add(6 * time.Minute))
	v, _ := m.Schedule(s.ID)
	if len(m.Tasks()) != 1 || v.LastReason == "" {
		t.Fatal("overlap not skipped")
	}
	m.CancelTask(first.ID)
	m.scheduleTick(at.Add(7 * time.Minute))
	if len(m.Tasks()) != 2 {
		t.Fatal("next occurrence did not enqueue")
	}
	if _, e = m.SaveSchedule(s.ID, spec, 0); e == nil {
		t.Fatal("stale revision")
	}
	v, _ = m.Schedule(s.ID)
	spec.Enabled = false
	v, e = m.SaveSchedule(s.ID, spec, v.Revision)
	if e != nil || v.NextAt != "" {
		t.Fatal(v, e)
	}
	if e = m.DeleteSchedule(s.ID, v.Revision); e != nil {
		t.Fatal(e)
	}
	if len(m.Tasks()) != 2 {
		t.Fatal("deleting rule deleted accepted tasks")
	}
}
func TestScheduleSkipBacklogAndAPI(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	spec := ScheduleSpec{Name: "skip", Cron: "* * * * *", Timezone: "UTC", Enabled: true, Task: TaskSpec{WorkspaceID: m.Workspaces()[0].ID, Input: Input{Text: "news"}}}
	h := NewHandler(m, "", true)
	b, _ := json.Marshal(spec)
	r := v1Request(h, "POST", "/schedules", string(b), "schedule-key-001")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = v1Request(h, "POST", "/schedules", string(b), "schedule-key-001")
	if r.Code != 200 || len(m.Schedules()) != 1 {
		t.Fatal("duplicate rule")
	}
	s := m.Schedules()[0]
	at := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	m.scheduleMu.Lock()
	s.NextAt = at.Format(time.RFC3339)
	m.schedules[s.ID] = s
	m.scheduleMu.Unlock()
	m.scheduleTick(at.Add(3 * time.Minute))
	if len(m.Tasks()) != 0 {
		t.Fatal("skip misfire enqueued")
	}
	v, _ := m.Schedule(s.ID)
	if v.LastReason == "" {
		t.Fatal("missing skip explanation")
	}
	r = v1Request(h, "POST", "/schedules/preview", `{"cron":"0 9 * * *","timezone":"Asia/Shanghai"}`, "")
	if r.Code != 200 || len(object(t, r)["times"].([]any)) != 5 {
		t.Fatal(r.Body.String())
	}
}
