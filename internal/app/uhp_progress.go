package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"sort"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

// This extension describes platform progress, not another UHP status machine.
// It intentionally carries no approval ID, native arguments or credentials.
type uhpProgress struct {
	State          string                     `json:"state"`
	RequiredAction *uhpProgressRequiredAction `json:"required_action,omitempty"`
}

type uhpProgressRequiredAction struct {
	Type         string `json:"type"`
	WorkbenchURL string `json:"workbench_url"`
}

// updateProgress changes only response metadata. The caller owns uhpMu and
// decides when to persist the response. No event is emitted on polling.
// Terminal responses are derived solely from their own task and response;
// they never inspect a later turn of the same native session.
func (u *uhpService) updateProgress(v *uhpRecord, t Task) (bool, error) {
	var session *Session
	approval, paused := false, false
	if v.Projection.Response.Status == "in_progress" && taskPending(t.Status) &&
		t.Status != "queued" && t.Status != "dispatching" && t.RunID != "" &&
		!t.CancelRequested && t.StopReason == "" && v.CancelReason == "" && t.ID == v.TaskID && t.SessionID == v.HostSessionID {
		m := u.server.Manager
		// Kun journal import takes mu before committing the event and Session
		// projection. Use that same order to read an internally coherent view.
		// Never acquire queueMu, a handle lock or a native client here.
		m.mu.Lock()
		current := m.sessions[t.SessionID]
		if current != nil && current.RunID == t.RunID && current.TaskID == t.ID {
			copy := *current
			session = &copy
		}
		var err error
		if session != nil && session.Status == "waiting" {
			if runtimeKind(session.RuntimeKind) == "kun" {
				approval, paused, err = uhpKunWaiting(m.Store, *session)
			} else {
				approval, err = uhpPendingApproval(m.Store, *session)
			}
		}
		m.mu.Unlock()
		if err != nil {
			return false, err
		}
	}
	progress := uhpProjectProgress(v, t, session, approval, paused)
	if v.Projection.Response.Metadata == nil {
		v.Projection.Response.Metadata = map[string]any{}
	}
	before, _ := json.Marshal(v.Projection.Response.Metadata["rundesk_progress"])
	after, _ := json.Marshal(progress)
	// After persistence metadata is a map, whereas the fresh value is a struct.
	// Compare JSON values, not their differing object-key serialization order.
	var oldValue, newValue any
	if json.Unmarshal(before, &oldValue) == nil && json.Unmarshal(after, &newValue) == nil && reflect.DeepEqual(oldValue, newValue) {
		return false, nil
	}
	v.Projection.Response.Metadata["rundesk_progress"] = progress
	return true, nil
}

func uhpProjectProgress(v *uhpRecord, t Task, s *Session, approval, paused bool) uhpProgress {
	out := uhpProgress{State: "unknown"}
	// The frozen protocol terminal state wins over all live/session evidence.
	switch v.Projection.Response.Status {
	case "completed", "failed", "incomplete":
		out.State = v.Projection.Response.Status
		return out
	case "cancelled", "canceled":
		out.State = "cancelled"
		if t.Status == "interrupted" && v.CancelReason == "" {
			out.State = "interrupted"
		}
		return out
	case "in_progress":
	default:
		return out
	}
	if t.ID != v.TaskID || t.SessionID != v.HostSessionID {
		return out
	}
	if !taskPending(t.Status) {
		switch t.Status {
		case "completed", "failed", "incomplete", "interrupted":
			out.State = t.Status
		case "canceled", "cancelled":
			out.State = "cancelled"
		}
		if t.StopReason == "timeout" || v.CancelReason == "timeout" || v.CancelReason == "max_step" {
			out.State = "incomplete"
		}
		return out
	}
	if t.CancelRequested || t.StopReason != "" || v.CancelReason != "" || t.Status == "stopping" {
		out.State = "cancelling"
		return out
	}
	switch t.Status {
	case "queued":
		out.State = "queued"
		return out
	case "dispatching":
		out.State = "dispatching"
		return out
	}
	if s == nil || t.RunID == "" || s.ID != t.SessionID || s.RunID != t.RunID || s.TaskID != t.ID {
		return out
	}
	switch s.Status {
	case "starting":
		out.State = "dispatching"
	case "running":
		out.State = "running"
	case "stopping":
		out.State = "cancelling"
	case "completed", "failed", "incomplete", "interrupted":
		out.State = s.Status
	case "canceled", "cancelled":
		out.State = "cancelled"
	case "waiting":
		if approval {
			out.State = "waiting_approval"
			out.RequiredAction = &uhpProgressRequiredAction{Type: "approval", WorkbenchURL: "/?session=" + url.QueryEscape(s.ID)}
		} else if paused {
			out.State = "paused"
		}
	}
	return out
}

func uhpPendingApproval(db *store.Store, s Session) (bool, error) {
	rows, err := db.List("approval")
	if err != nil {
		return false, err
	}
	for _, raw := range rows {
		// Decode only the identity and lifecycle fields needed for the decision.
		var a struct {
			SessionID string `json:"sessionId"`
			RunID     string `json:"runId"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(raw, &a) == nil && a.SessionID == s.ID && a.RunID == s.RunID && a.Status == "pending" {
			return true, nil
		}
	}
	return false, nil
}

// uhpKunWaiting distinguishes a debugger pause from native tool approval using
// committed same-run evidence. It never opens or queries a worker. A bare
// Session.waiting, missing evidence or an overlarge control tail stays unknown.
func uhpKunWaiting(db *store.Store, s Session) (approval, paused bool, err error) {
	markers := []store.Event{}
	for _, method := range []string{"kun/run.paused", "kun/approval.requested"} {
		event, e := db.LatestEvent(s.ID, method)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return false, false, e
		}
		if _, ok := uhpProgressKunEvent(event, s); ok {
			markers = append(markers, event)
		}
	}
	if len(markers) == 0 {
		return false, false, nil
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].ID < markers[j].ID })
	controls, err := db.QueryEvents(s.ID, markers[0].ID, 1000, store.EventFilter{Method: "kun/control.applied"})
	if err != nil {
		return false, false, err
	}
	if len(controls) == 1000 {
		return false, false, nil
	}
	markers = append(markers, controls...)
	sort.Slice(markers, func(i, j int) bool { return markers[i].ID < markers[j].ID })
	for _, event := range markers {
		native, ok := uhpProgressKunEvent(event, s)
		if !ok {
			continue
		}
		switch event.Method {
		case "kun/approval.requested":
			approval = true
		case "kun/run.paused":
			paused = true
		case "kun/control.applied":
			var data struct {
				Command struct {
					Operation string `json:"operation"`
				} `json:"command"`
			}
			if json.Unmarshal(native.Data, &data) != nil {
				return false, false, nil
			}
			switch data.Command.Operation {
			case "approve", "reject":
				approval, paused = false, false
			case "resume", "step":
				paused = false
			}
		}
	}
	return approval, paused, nil
}

func uhpProgressKunEvent(event store.Event, s Session) (p.Event, bool) {
	var native p.Event
	valid := json.Unmarshal(event.Data, &native) == nil && event.SessionID == s.ID &&
		native.SessionID == s.ID && native.RunID == s.RunID && native.Type == event.Method
	return native, valid
}
