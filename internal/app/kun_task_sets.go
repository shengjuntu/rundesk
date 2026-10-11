package app

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/store"
)

// A task set freezes authored inputs and rules, not runs or benchmark results.
// Execution must bind a future run to this immutable definition explicitly.
type KunTaskCase struct {
	Key           string           `json:"key"`
	Title         string           `json:"title"`
	Input         string           `json:"input"`
	Rules         []KunReplyRule   `json:"rules"`
	ArtifactRules []p.ArtifactRule `json:"artifactRules,omitempty"`
}
type KunTaskSetInput struct {
	Title string        `json:"title"`
	Cases []KunTaskCase `json:"cases"`
}
type KunTaskSet struct {
	Schema    int           `json:"schema"`
	Evaluator string        `json:"evaluator"`
	ID        string        `json:"id"`
	Hash      string        `json:"hash"`
	Title     string        `json:"title"`
	CreatedAt string        `json:"createdAt"`
	CaseCount int           `json:"caseCount"`
	Cases     []KunTaskCase `json:"cases"`
}

var kunCaseKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func taskSetHash(out KunTaskSet) string { out.Hash = ""; return forkDigest(out) }

func validateKunTaskSet(in KunTaskSetInput) error {
	if !utf8.ValidString(in.Title) {
		return failure(400, "task_set_title_invalid", "任务集标题必须是有效 UTF-8 文本")
	}
	if err := experimentTitle(in.Title); err != nil {
		return err
	}
	if len(in.Cases) < 1 || len(in.Cases) > 20 {
		return failure(400, "task_set_cases_invalid", "任务集需要 1–20 个案例")
	}
	seen := map[string]bool{}
	total := len(in.Title)
	for _, c := range in.Cases {
		if !kunCaseKey.MatchString(c.Key) || seen[c.Key] {
			return failure(400, "task_set_case_key_invalid", "案例标识需为唯一的 1–64 位字母/数字/点/下划线/连字符，并以字母或数字开头")
		}
		seen[c.Key] = true
		if !utf8.ValidString(c.Title) {
			return failure(400, "task_set_case_title_invalid", "案例标题必须是有效 UTF-8 文本")
		}
		if err := experimentTitle(c.Title); err != nil {
			return err
		}
		if !utf8.ValidString(c.Input) || strings.TrimSpace(c.Input) == "" || utf8.RuneCountInString(c.Input) > 8192 || len(c.Input) > 32<<10 {
			return failure(400, "task_set_input_invalid", "每个案例需要非空的有效输入，最多 8192 字符 / 32 KiB UTF-8")
		}
		if err := validateReplyRules(c.Rules); err != nil {
			return err
		}
		if err := p.ValidateArtifactRules(c.ArtifactRules); err != nil {
			return failure(400, "artifact_rules_invalid", err.Error())
		}
		for _, r := range c.ArtifactRules {
			total += len(r.Path) + len(r.Kind) + len(r.Value)
		}
		total += len(c.Key) + len(c.Title) + len(c.Input)
		for _, r := range c.Rules {
			total += len(r.Kind) + len(r.Value)
		}
	}
	if total > 256<<10 {
		return failure(400, "task_set_too_large", "任务集文本合计不能超过 256 KiB UTF-8")
	}
	return nil
}

func (m *Manager) CreateKunTaskSet(in KunTaskSetInput) (KunTaskSet, error) {
	var out KunTaskSet
	if err := validateKunTaskSet(in); err != nil {
		return out, err
	}
	cases := make([]KunTaskCase, len(in.Cases))
	for i, c := range in.Cases {
		cases[i] = c
		cases[i].Rules = append([]KunReplyRule(nil), c.Rules...)
		cases[i].ArtifactRules = append([]p.ArtifactRule(nil), c.ArtifactRules...)
	}
	out = KunTaskSet{Schema: 1, Evaluator: kunEvaluator, ID: store.ID(), Title: in.Title, CreatedAt: store.Now(), CaseCount: len(cases), Cases: cases}
	out.Hash = taskSetHash(out)
	return out, m.Store.Put("kun_task_set", out.ID, out)
}

func (m *Manager) KunTaskSet(id string) (KunTaskSet, error) {
	var out KunTaskSet
	if err := m.Store.Get("kun_task_set", id, &out); err != nil {
		return out, failure(404, "task_set_not_found", "任务集不存在")
	}
	if out.ID != id || out.Schema != 1 || out.Evaluator != kunEvaluator || out.Hash != taskSetHash(out) || out.CaseCount != len(out.Cases) || validateKunTaskSet(KunTaskSetInput{Title: out.Title, Cases: out.Cases}) != nil {
		return out, failure(409, "task_set_integrity", "任务集版本、内容或指纹不一致")
	}
	return out, nil
}

func (s *Server) kunTaskSetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kun-task-sets", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		var in KunTaskSetInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.CreateKunTaskSet(in)
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-task-sets/{tid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := s.Manager.KunTaskSet(r.PathValue("tid"))
		debugRespond(w, out, err)
	})
	mux.HandleFunc("GET /api/kun-task-sets", func(w http.ResponseWriter, r *http.Request) {
		q, err := experimentQuery(r, "offset", "limit")
		if err != nil {
			respond(w, nil, err)
			return
		}
		offset, limit, err := experimentPage(q, 50)
		if err != nil {
			respond(w, nil, experimentPageError(err))
			return
		}
		items, err := s.Manager.Store.KunTaskSetList(r.Context(), offset, limit)
		if err != nil {
			respond(w, nil, err)
			return
		}
		more := len(items) > limit
		if more {
			items = items[:limit]
		}
		debugRespond(w, map[string]any{"items": items, "nextOffset": offset + len(items), "hasMore": more}, nil)
	})
}
