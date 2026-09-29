package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Steer never starts a new turn. The explicit precondition prevents a delayed
// browser request from being injected into a different task.
type SteerInput struct {
	Input
	ExpectedTurnID string `json:"expectedTurnId"`
	RequestID      string `json:"requestId"`
}
type SteerReceipt struct {
	RequestID string `json:"requestId"`
	TurnID    string `json:"turnId"`
	Status    string `json:"status"`
}
type storedSteer struct {
	SessionID   string       `json:"sessionId"`
	Receipt     SteerReceipt `json:"receipt"`
	Fingerprint string       `json:"fingerprint"`
	Error       string       `json:"error,omitempty"`
}

func (m *Manager) Steer(id string, in SteerInput) (SteerReceipt, error) {
	receipt := SteerReceipt{RequestID: in.RequestID, TurnID: in.ExpectedTurnID, Status: "submitting"}
	if strings.TrimSpace(in.Text) == "" || len(in.Text) > 256*1024 {
		return receipt, errors.New("补充消息不能为空或超过 256 KiB")
	}
	if in.ExpectedTurnID == "" || len(in.RequestID) < 8 || len(in.RequestID) > 128 {
		return receipt, errors.New("补充指令需要 expectedTurnId 和有效 requestId")
	}
	s, err := m.Session(id)
	if err != nil {
		return receipt, err
	}
	w, err := m.Workspace(s.WorkspaceID)
	if err != nil {
		return receipt, err
	}
	if err = m.validateInput(w, in.Input, s.InstanceID); err != nil {
		return receipt, err
	}
	h, err := m.getHandle(id)
	if err != nil {
		return receipt, err
	}
	h.op.Lock()
	defer h.op.Unlock()
	encoded, _ := json.Marshal(in)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	key := id + ":" + in.RequestID
	var saved storedSteer
	if err = m.Store.Get("steer", key, &saved); err == nil {
		if saved.Fingerprint != fingerprint {
			return receipt, errors.New("requestId 已用于另一条消息")
		}
		if saved.Receipt.Status == "accepted" {
			return saved.Receipt, nil
		}
		if saved.Error != "" {
			return saved.Receipt, errors.New(saved.Error)
		}
		return saved.Receipt, errors.New("此前提交结果尚未确认；请检查对话记录，不要重复发送")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return receipt, err
	}
	s, err = m.Session(id)
	if err != nil {
		return receipt, err
	}
	if s.Archived || (s.Status != "running" && s.Status != "waiting") || s.TurnID != in.ExpectedTurnID {
		return receipt, errors.New("当前任务已结束、正在切换或停止；补充指令未发送，请确认后作为新消息发送")
	}
	h.mu.Lock()
	c, canceled := h.client, h.canceled
	h.last = time.Now()
	h.mu.Unlock()
	if c == nil || canceled {
		return receipt, errors.New("任务连接不可用或正在停止，补充指令未发送")
	}
	saved = storedSteer{SessionID: id, Receipt: receipt, Fingerprint: fingerprint}
	// Persist before sending: retrying an uncertain request must not send twice.
	if err = m.Store.Put("steer", key, saved); err != nil {
		return receipt, err
	}
	text := in.Text
	input := []any{}
	for _, f := range in.Files {
		path := filepath.Join(w.Path, filepath.FromSlash(f))
		text += "\nUploaded file: " + path
		switch strings.ToLower(filepath.Ext(f)) {
		case ".png", ".jpg", ".jpeg", ".webp":
			input = append(input, map[string]string{"type": "localImage", "path": path})
		}
	}
	input = append(input, map[string]string{"type": "text", "text": text})
	for _, sk := range in.Skills {
		input = append(input, map[string]string{"type": "skill", "name": sk.Name, "path": sk.Path})
	}
	ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
	defer cancel()
	raw, err := c.Call(ctx, "turn/steer", map[string]any{"threadId": s.ThreadID, "expectedTurnId": in.ExpectedTurnID, "input": input})
	if err == nil {
		var result struct {
			TurnID string `json:"turnId"`
		}
		if json.Unmarshal(raw, &result) != nil || result.TurnID != in.ExpectedTurnID {
			err = errors.New("turn/steer 返回了不匹配的 turnId")
		}
	}
	if err != nil {
		saved.Receipt.Status = "unconfirmed"
		saved.Error = "补充指令未获接收确认（不会自动重发）：" + err.Error()
		_ = m.Store.Put("steer", key, saved)
		return saved.Receipt, errors.New(saved.Error)
	}
	saved.Receipt.Status = "accepted"
	if err = m.Store.Put("steer", key, saved); err != nil {
		return receipt, errors.New("Codex 已接收，但接收记录保存失败；请勿重复发送")
	}
	m.event(id, "internal", "run/steer", map[string]any{"runId": s.RunID, "turnId": s.TurnID, "requestId": in.RequestID, "status": "accepted", "input": in.Input})
	return saved.Receipt, nil
}
