package app

import (
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"path/filepath"
)

// Resolve current authorizations and skill contents without connecting MCP.
// Repeated at launch so a preview never carries resolved credentials forward.
func (m *Manager) kunLiveInputs(s Session, w Workspace, b p.ForkBundle) ([]p.Skill, []p.MCPServer, string, error) {
	i, err := m.Instance(s.InstanceID)
	if err != nil {
		return nil, nil, "", err
	}
	sk := []Skill{}
	for _, v := range b.State.Skills {
		sk = append(sk, Skill{Name: v.Name, Path: v.Path})
	}
	skills, err := m.kunInputSkills(w, i, sk)
	if err != nil {
		return nil, nil, "", err
	}
	servers, err := m.kunRuntimeMCP(i.ID)
	if err != nil {
		return nil, nil, "", err
	}
	if len(skills) == 0 {
		skills = nil
	}
	if len(servers) == 0 {
		servers = nil
	}
	workspace := filepath.Clean(w.Path)
	if real, e := filepath.EvalSymlinks(workspace); e == nil {
		workspace = real
	}
	revision := fmt.Sprintf("%s:%d/%s:%d", i.ID, i.Revision, w.ID, w.Revision)
	manifest := b.State.Manifest
	if b.Mode != "live" || manifest == nil || manifest.Workspace != workspace || manifest.SkillsHash != forkDigest(skills) || manifest.MCPHash != forkDigest(servers) || manifest.ContextRevision != revision || b.State.ApprovalPolicy != i.Permissions.normalized().ApprovalPolicy {
		return nil, nil, "", failure(409, "fork_live_environment_changed", "Live 工作区、技能、MCP 凭据或当前权限已变化，请重新创建来源运行与预览")
	}
	return skills, servers, revision, nil
}
