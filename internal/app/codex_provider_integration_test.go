package app

import (
	"github.com/your-org/rundesk/internal/store"
	"testing"
)

func TestBuildWorkbenchLaunchInheritsFrozenDefaultProvider(t *testing.T) {
	m, _, _, _, image := buildCenterTestApp(t)
	defaults, err := m.SaveDefaultCodexProvider(providerInput(0))
	if err != nil {
		t.Fatal(err)
	}
	input := BuildCenterImageLaunchInput{RequestID: store.ID(), ProviderMode: "inherit-default", ProviderRevision: defaults.Revision}
	launched, err := m.LaunchBuildCenterImage(BuildCenterImageKey(image.ImageID), input)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := m.CodexProvider(launched.Instance.ID, launched.Workspace.ID)
	if err != nil || binding.Model != defaults.Model || binding.BaseURL != defaults.BaseURL || binding.State != "configured" {
		t.Fatal(binding, err)
	}
	change := providerInput(defaults.Revision)
	change.Model = "later-default"
	if _, err = m.SaveDefaultCodexProvider(change); err != nil {
		t.Fatal(err)
	}
	replay, err := m.LaunchBuildCenterImage(BuildCenterImageKey(image.ImageID), input)
	if err != nil || replay.Instance.ID != launched.Instance.ID {
		t.Fatal(replay, err)
	}
	session, err := m.CreateSession(launched.Workspace.ID, "default model", "", launched.Instance.ID)
	if err != nil || session.Model != defaults.Model {
		t.Fatal(session, err)
	}
	input.RequestID = store.ID()
	before := len(m.Instances())
	if _, err = m.LaunchBuildCenterImage(BuildCenterImageKey(image.ImageID), input); err == nil {
		t.Fatal("stale default accepted for new launch")
	}
	if len(m.Instances()) != before {
		t.Fatal("stale default left partial Agent")
	}
}

func TestApplicationProjectKeepsProviderSnapshot(t *testing.T) {
	m, _, app, preset := runtimePresetRegistryFixture(t)
	in := providerInput(0)
	if _, err := m.SaveCodexProvider(preset.InstanceID, preset.WorkspaceID, in); err != nil {
		t.Fatal(err)
	}
	project, err := m.PutApplicationProjectEnvironment(app.AppID, "d5d5d5d5d5d5d5d5d5d5d5d5", preset.ID, ApplicationProjectEnvironmentInput{Name: "Provider project"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := m.CodexProvider(project.InstanceID, project.WorkspaceID)
	if err != nil || !view.Configured || view.Model != in.Model || view.BaseURL != in.BaseURL {
		t.Fatal(view, err)
	}
}
