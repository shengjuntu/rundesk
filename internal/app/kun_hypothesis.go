package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/redaction"
	"github.com/shengjuntu/rundesk/internal/store"
)

type KunReplayReview struct {
	Position    int    `json:"position"`
	Sequence    int64  `json:"sequence"`
	Tool        string `json:"tool"`
	Status      string `json:"status"`
	RecordHash  string `json:"recordHash"`
	OutputHash  string `json:"outputHash"`
	OutputBytes int    `json:"outputBytes"`
	Preview     string `json:"preview"`
	Redacted    bool   `json:"redacted"`
	Truncated   bool   `json:"truncated"`
}
type KunReplayReviews struct {
	PreviewID   string            `json:"previewId"`
	PreviewHash string            `json:"previewHash"`
	Items       []KunReplayReview `json:"items"`
}
type KunHypothesisInput struct {
	ExpectedHash string  `json:"expectedHash"`
	Position     *int    `json:"position"`
	RecordHash   string  `json:"recordHash"`
	Output       *string `json:"output"`
	Reason       string  `json:"reason"`
	Title        string  `json:"title"`
}

func (m *Manager) hypothesisParent(id string) (kunForkDraft, error) {
	d, err := m.kunForkDraft(id)
	if err != nil {
		return d, err
	}
	if d.Bundle.Mode != "hybrid" || d.Preview.Origin.Mode != "hybrid" || d.Bundle.Hypothesis != nil {
		return d, failure(409, "hypothesis_parent_invalid", "请选择未修改工具结果的 Hybrid 预览；不支持 Live 或叠加替换")
	}
	if d.Bundle.Schema != p.ForkSchema || d.Bundle.State.Manifest == nil || d.Bundle.State.Manifest.EngineVersion != p.EngineVersion {
		return d, failure(409, "fork_runtime_incompatible", "旧版本预览仅供查看；请用当前 Kun 重新运行来源并创建预览")
	}
	return d, nil
}

// Bounded display-only text. No raw tape, arguments, messages or credentials
// are exported. Free text cannot be reliably classified as credential fields.
func replayReview(position int, r p.ReplayRecord) KunReplayReview {
	text, redacted := r.Output, false
	if json.Valid([]byte(text)) {
		var v any
		decoder := json.NewDecoder(bytes.NewBufferString(text))
		decoder.UseNumber()
		if decoder.Decode(&v) == nil {
			before := string(p.JSON(v))
			after := string(p.JSON(redaction.Fields(v)))
			redacted = before != after
			if redacted {
				text = after
			}
		}
	}
	runes := []rune(text)
	return KunReplayReview{Position: position, Sequence: r.Sequence, Tool: r.Tool, Status: r.Status, RecordHash: p.ReplayRecordHash(r), OutputHash: p.TextHash(r.Output), OutputBytes: len(r.Output), Preview: string(runes[:min(len(runes), 2048)]), Redacted: redacted, Truncated: len(runes) > 2048}
}

func (m *Manager) KunReplayReviews(id string) (KunReplayReviews, error) {
	d, err := m.hypothesisParent(id)
	if err != nil {
		return KunReplayReviews{}, err
	}
	out := KunReplayReviews{PreviewID: id, PreviewHash: d.Preview.Hash, Items: []KunReplayReview{}}
	for n, r := range d.Bundle.Records {
		out.Items = append(out.Items, replayReview(n, r))
	}
	return out, nil
}

// Only saved values are used. Creating an overlay starts no session or worker
// and does not require reopening the source run. Admission rechecks config.
func (m *Manager) CreateKunHypothesis(id string, in KunHypothesisInput) (KunForkPreview, error) {
	d, err := m.hypothesisParent(id)
	if err != nil {
		return KunForkPreview{}, err
	}
	if in.ExpectedHash == "" || in.ExpectedHash != d.Preview.Hash {
		return KunForkPreview{}, failure(409, "fork_preview_changed", "父预览指纹不一致，请重新读取")
	}
	if err = experimentTitle(in.Title); err != nil {
		return KunForkPreview{}, err
	}
	if in.Position == nil || *in.Position < 0 || *in.Position >= len(d.Bundle.Records) || in.Output == nil {
		return KunForkPreview{}, failure(400, "hypothesis_input_invalid", "必须选择一条录制结果并提供替换文本（可为空字符串）")
	}
	r := d.Bundle.Records[*in.Position]
	if in.RecordHash != p.ReplayRecordHash(r) {
		return KunForkPreview{}, failure(409, "hypothesis_record_changed", "所选录制结果指纹不一致")
	}
	h := &p.ReplayHypothesis{ParentPreviewID: id, ParentHash: d.Preview.Hash, ParentBundleHash: d.Bundle.ContentHash, Position: *in.Position, SourceSequence: r.Sequence, RecordHash: in.RecordHash, OriginalOutputHash: p.TextHash(r.Output), OutputHash: p.TextHash(*in.Output), Output: *in.Output, Reason: strings.TrimSpace(in.Reason)}
	d.Bundle.Hypothesis = h
	if err = p.ValidateHypothesis(d.Bundle); err != nil {
		return KunForkPreview{}, failure(400, "hypothesis_input_invalid", err.Error())
	}
	d.Bundle.ContentHash = p.ForkHash(d.Bundle)
	if len(p.JSON(d.Bundle)) > p.MaxForkBytes {
		return KunForkPreview{}, failure(413, "hypothesis_too_large", "加入替换后超过 4 MiB 分叉记录上限")
	}
	child, now := store.ID(), store.Now()
	d.Target.ID, d.Target.Title, d.Target.Created, d.Target.Updated = store.ID(), in.Title, now, now
	d.Preview.ID, d.Preview.Title, d.Preview.CreatedAt, d.Preview.TargetSessionID = child, in.Title, now, d.Target.ID
	d.Preview.Origin.PreviewID, d.Preview.Origin.BundleHash = child, d.Bundle.ContentHash
	origin := d.Preview.Origin
	d.Target.KunFork = &origin
	d.Preview.Hypothesis = h
	d.Preview.Hash = forkDraftHash(d)
	err = m.Store.PutMany(store.Record{Kind: "kun_fork_bundle", ID: child, Value: d}, store.Record{Kind: "kun_fork_preview", ID: child, Value: d.Preview})
	return d.Preview, err
}

func (s *Server) kunHypothesisRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kun-forks/recordings/{fid}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := experimentQuery(r); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := s.Manager.KunReplayReviews(r.PathValue("fid"))
		debugRespond(w, out, err)
	})
	mux.HandleFunc("POST /api/kun-forks/{fid}/hypotheses", func(w http.ResponseWriter, r *http.Request) {
		var in KunHypothesisInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Manager.CreateKunHypothesis(r.PathValue("fid"), in)
		debugRespond(w, out, err)
	})
}
