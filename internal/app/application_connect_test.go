package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestApplicationConnectPreservesConfiguration(t *testing.T) {
	m := testManager(t)
	in := ApplicationConnect{Name: "新闻研究", Description: "研究新闻", InstallationID: "installation-1", EntryURL: "http://localhost:3180", Defaults: ApplicationDefaults{Skills: map[string]map[string]string{"news": {"SKILL.md": "---\nname: news\ndescription: research\n---\ninitial", "scripts/a.py": "print('hello')"}}}}
	first, e := m.ConnectApplication("news", in)
	if e != nil || first.Status != "connected" {
		t.Fatal(first, e)
	}
	a := first.Application
	if e = m.SaveSkill(a.WorkspaceID, "news", "---\nname: news\ndescription: research\n---\nadmin edit", a.InstanceID, "instance"); e != nil {
		t.Fatal(e)
	}
	i, _ := m.Instance(a.InstanceID)
	i.DefaultModel = "admin-model"
	if _, e = m.PatchInstance(i.ID, InstancePatch{Name: i.Name, DefaultModel: i.DefaultModel, Revision: i.Revision}); e != nil {
		t.Fatal(e)
	}
	in.Defaults.Model = "overwrite-model"
	in.Defaults.Skills["news"]["SKILL.md"] = "---\nname: news\ndescription: research\n---\nnew default"
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := m.ConnectApplication("news", in)
			if e != nil || v.Application.InstanceID != a.InstanceID {
				t.Errorf("retry: %v %v", v, e)
			}
		}()
	}
	wg.Wait()
	content, _ := m.ReadSkill(a.WorkspaceID, "news", a.InstanceID, "instance")
	if content != "---\nname: news\ndescription: research\n---\nadmin edit" {
		t.Fatal(content)
	}
	i, _ = m.Instance(a.InstanceID)
	if i.DefaultModel != "admin-model" {
		t.Fatal(i)
	}
	if _, e = os.Stat(filepath.Join(i.CodexHome, "skills/news/scripts/a.py")); e != nil {
		t.Fatal(e)
	}
	rows, _ := m.Applications()
	if len(rows) != 1 || len(m.Instances()) != 2 {
		t.Fatal(rows, m.Instances())
	}
	in.InstallationID = "another"
	_, e = m.ConnectApplication("news", in)
	appCode(t, e, "installation_conflict")
	cfg, e := m.collaborationConfig()
	if e != nil || len(cfg.Agents) != 2 || cfg.Agents[0].ID != "rundesk-assistant" {
		t.Fatal(cfg, e)
	}
	c, e := m.createCollaboration(CollaborationInput{Goal: "研究新闻"})
	if e != nil || c.Leader != "rundesk-assistant" || len(c.Agents) != 2 {
		t.Fatal(c, e)
	}
}
func TestApplicationConnectAdoptsLegacy(t *testing.T) {
	m := testManager(t)
	i, _ := m.CreateInstance(InstancePatch{Name: "旧新闻"})
	w := m.Workspaces()[0]
	in := ApplicationConnect{Name: "新闻", InstallationID: "legacy", InstanceID: i.ID, WorkspaceID: w.ID}
	a, e := m.ConnectApplication("news2douyin", in)
	if e != nil || a.Application.InstanceID != i.ID || a.Application.WorkspaceID != w.ID {
		t.Fatal(a, e)
	}
	if len(m.Instances()) != 2 {
		t.Fatal(m.Instances())
	}
}

func TestApplicationCapabilitiesCopyDisabled(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	if e := m.SaveSkill(w.ID, "shared-research", "---\nname: shared-research\ndescription: research\n---\ncheck sources", DefaultInstance, "instance"); e != nil {
		t.Fatal(e)
	}
	base, _ := m.Instance()
	path := filepath.Join(base.CodexHome, "skills/shared-research/SKILL.md")
	if e := m.ToggleSkill(w.ID, path, false); e != nil {
		t.Fatal(e)
	}
	c, e := m.readConfig(w.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, rev := userMCP(c)
	if _, e = m.SaveMCP(w.ID, "shared-search", rev, map[string]any{"url": "http://localhost:9/mcp", "enabled": false}, false); e != nil {
		t.Fatal(e)
	}
	result, e := m.ConnectApplication("news", ApplicationConnect{Name: "新闻", Description: "研究", InstallationID: "copy-test"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.selectCapabilities(result.Application, capabilitySelection{Skills: []string{"shared-research"}, MCP: []string{"shared-search"}}); e != nil {
		t.Fatal(e)
	}
	original, e := m.readConfig(w.ID)
	if e != nil {
		t.Fatal(e)
	}
	servers, _ := userMCP(original)
	if servers["shared-search"].(map[string]any)["enabled"] != false {
		t.Fatal("changed assistant MCP")
	}
	target, e := m.readConfig(result.Application.WorkspaceID, result.Application.InstanceID)
	if e != nil {
		t.Fatal(e)
	}
	servers, _ = userMCP(target)
	if servers["shared-search"].(map[string]any)["enabled"] != true {
		t.Fatal("target not enabled")
	}
	raw, _ := m.Skills(w.ID)
	if !strings.Contains(string(raw), `"enabled":false`) {
		t.Fatal("changed assistant skill", string(raw))
	}
}
