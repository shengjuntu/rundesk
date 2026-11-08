package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/your-org/rundesk/internal/tracequery"
)

func TestDebugTimingSortBaseAndMemberRemainFrozenAndScoped(t *testing.T) {
	m, _, _, viewer, code, shared, private := userFixture(t)
	var seq int64
	add := func(ms int64, method string, value any) {
		t.Helper()
		seq++
		at := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC).Add(time.Duration(ms) * time.Millisecond).Format(time.RFC3339Nano)
		if ok, err := m.Store.ImportKun(shared.ID, seq, at, method, value); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	add(0, "run/input", map[string]any{"runId": "r"})
	item := func(method, id string, ms int64) {
		add(ms, method, map[string]any{"params": map[string]any{"turnId": "t", "item": map[string]any{"id": id, "type": "commandExecution", "exitCode": 1}}})
	}
	for n := 1; n <= 25; n++ {
		item("item/started", fmt.Sprint(n), 1000)
		item("item/completed", fmt.Sprint(n), int64(n+1)*1000)
	}
	item("item/completed", "missing-start", 27000)
	add(30000, "run/state", map[string]any{"runId": "r", "status": "completed"})
	terminal, err := m.Store.LatestEvent(shared.ID, "run/state")
	if err != nil {
		t.Fatal(err)
	}
	// New evidence must not enter any of the explicitly frozen route results.
	item("item/started", "future", 31000)
	item("item/completed", "future", 999000)
	h := NewHandler(m, userTestAdmin, true)
	routes := []struct{ root, token string }{
		{"/api/v1/sessions/" + shared.ID + "/debug/query?", userTestAdmin},
		{"/api/v1/member/" + viewer.Grants[0].ID + "/sessions/" + shared.ID + "/debug/query?", code},
	}
	for _, route := range routes {
		for _, kind := range []string{"steps", "issues"} {
			res := appRequest(h, "GET", fmt.Sprintf("%skind=%s&runId=r&type=commandExecution&sort=duration_desc&limit=2&through=%d", route.root, kind, terminal.ID), "", route.token, "")
			var reply struct {
				Through int64 `json:"through"`
				Data    struct {
					Steps      []tracequery.Step `json:"steps"`
					Total      int               `json:"total"`
					NextOffset int               `json:"nextOffset"`
					HasMore    bool              `json:"hasMore"`
				} `json:"data"`
			}
			if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &reply) != nil || reply.Through != terminal.ID || reply.Data.Total != 26 || reply.Data.NextOffset != 2 || !reply.Data.HasMore || len(reply.Data.Steps) != 2 || reply.Data.Steps[0].DurationMS == nil || reply.Data.Steps[1].DurationMS == nil || *reply.Data.Steps[0].DurationMS != 25000 || *reply.Data.Steps[1].DurationMS != 24000 {
				t.Fatal(kind, res.Code, res.Body.String())
			}
			res = appRequest(h, "GET", fmt.Sprintf("%skind=%s&runId=r&type=commandExecution&sort=duration_asc&offset=25&limit=2&through=%d", route.root, kind, terminal.ID), "", route.token, "")
			reply.Data.Steps = nil
			if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &reply) != nil || len(reply.Data.Steps) != 1 || reply.Data.Steps[0].DurationMS != nil || reply.Data.HasMore {
				t.Fatal("unknown duration/page boundary", res.Code, res.Body.String())
			}
		}
		res := appRequest(h, "GET", fmt.Sprintf("%skind=statistics&runId=r&type=commandExecution&through=%d", route.root, terminal.ID), "", route.token, "")
		var stats struct {
			Data struct {
				Timing tracequery.TimingSummary `json:"timing"`
			} `json:"data"`
		}
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &stats) != nil || len(stats.Data.Timing.Runs) != 1 || stats.Data.Timing.Runs[0].DurationMS == nil || *stats.Data.Timing.Runs[0].DurationMS != 30000 || stats.Data.Timing.KnownWorkDurationMS != 325000 || stats.Data.Timing.CompleteDurationSteps != 25 || stats.Data.Timing.WithoutCompleteDuration != 1 || len(stats.Data.Timing.SlowestSteps) != 10 {
			t.Fatal("timing did not traverse shared debug service", res.Code, res.Body.String())
		}
		for _, invalid := range []string{"kind=steps&sort=other", "kind=statistics&sort=duration_desc", "kind=steps&sort=duration_desc&sort=recorded", "kind=steps&sort=duration_desc&limit=51"} {
			res = appRequest(h, "GET", route.root+invalid, "", route.token, "")
			if res.Code != 400 {
				t.Fatal("invalid sort/paging accepted", invalid, res.Code, res.Body.String())
			}
		}
	}
	res := appRequest(h, "GET", "/api/v1/member/"+viewer.Grants[0].ID+"/sessions/"+private.ID+"/debug/query?kind=steps&sort=duration_desc", "", code, "")
	if res.Code != 403 {
		t.Fatal("member timing query escaped project grant", res.Code, res.Body.String())
	}
	if len(m.handles) != 0 {
		t.Fatal("inspection started a backend runtime")
	}
}
