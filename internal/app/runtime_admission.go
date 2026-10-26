package app

import (
	"path/filepath"
	"strings"
)

func (b codexBackend) Prepare(a RuntimeAdmission) (Input, error) {
	in := a.Input
	if in.FastSkillID != "" {
		return in, failure(409, "skill_ordinary_application_only", "快速 Skill 只支持普通 Kun 应用运行")
	}
	if a.Session.KunCase != nil || in.KunCase != nil {
		return in, failure(409, "case_single_run", "案例会话只允许固定预览首次启动；重跑请创建新预览")
	}
	if a.Session.KunFork != nil || in.KunFork != nil {
		return in, failure(409, "hybrid_single_run", "分叉会话只允许固定预览的首次启动；重试实验请创建新分叉")
	}
	if in.KunResume != nil {
		return in, failure(400, "not_kun", "该会话不使用 Kun")
	}
	if a.Recovery != nil {
		if err := b.transport.ValidateRecovery(a.Session, a.Handle, a.Recovery); err != nil {
			return in, err
		}
	}
	return in, nil
}

func (b kunBackend) Prepare(a RuntimeAdmission) (Input, error) {
	s, w, h, in := a.Session, a.Workspace, a.Handle, a.Input
	for _, f := range in.Files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif":
			return in, failure(400, "kun_text_only", "首版 Kun 不支持图像输入，请改用文本文件")
		}
	}
	if a.Recovery != nil {
		return in, failure(400, "kun_recovery_unsupported", "首版 Kun 不支持原生 Codex 恢复或分析入口，请使用 Kun DevTools")
	}
	if s.TraceOrigin != nil && (len(in.Files) > 0 || len(in.Skills) > 0 || in.KunResume != nil) {
		return in, failure(400, "diagnostic_input_only", "诊断会话只接收问题文本；不接收文件、技能或恢复请求")
	}
	if in.FastSkillID != "" {
		if s.KunCase != nil || s.KunFork != nil || s.TraceOrigin != nil {
			return in, failure(409, "skill_ordinary_application_only", "快速 Skill 只支持普通 Kun 应用运行")
		}
		if _, err := b.startRequest(s, w, in); err != nil {
			return in, err
		}
	}
	if s.KunCase != nil || in.KunCase != nil {
		if s.KunCase == nil || in.KunCase == nil || s.RunID != "" || a.Queued {
			return in, failure(409, "case_single_run", "案例会话只允许固定预览首次启动；重跑请创建新预览")
		}
		if _, err := b.preparation.CaseStartRequest(s, w, in); err != nil {
			return in, err
		}
	}
	if s.KunFork != nil || in.KunFork != nil {
		if s.KunFork == nil || in.KunFork == nil || in.KunFork.PreviewID != s.KunFork.PreviewID || s.RunID != "" || len(in.Files) > 0 || len(in.Skills) > 0 || in.KunResume != nil {
			return in, failure(409, "hybrid_single_run", "分叉会话只允许固定预览的首次启动；重试实验请创建新分叉")
		}
	}
	if in.KunResume != nil {
		skills, err := b.preparation.ValidateResume(s, w, h, in)
		if err != nil {
			return in, err
		}
		in.Skills = skills
	}
	return in, nil
}
