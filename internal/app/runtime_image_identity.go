package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RuntimeImageIdentity is captured from a submitted context. Later edits of an
// external recipe must not change verification of this build or its rebuilds.
type RuntimeImageIdentity struct {
	ProfileVersion string                    `json:"profileVersion"`
	AgentReach     bool                      `json:"agentReach"`
	Upstream       *independentImageIdentity `json:"upstream,omitempty"`
	ToolLabels     map[string]string         `json:"toolLabels,omitempty"`
}

func freezeRuntimeImageIdentity(build *ImageBuild, directory string) error {
	if build.Scope != "setup" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(directory, "profile.lock.json"))
	if os.IsNotExist(err) {
		return nil // Older, non-profile contexts retain their legacy checks.
	}
	if err != nil {
		return err
	}
	var lock struct {
		Profile struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"profile"`
		Selection struct {
			AgentReach bool `json:"agentReach"`
		} `json:"selection"`
		Upstream independentImageIdentity `json:"upstream"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	if lock.Profile.ID != build.ProfileID || !profileRelease.MatchString(lock.Profile.Version) {
		return failure(400, "invalid_recipe_identity", "profile.lock.json 的预置身份或版本无效")
	}
	identity := &RuntimeImageIdentity{ProfileVersion: lock.Profile.Version, AgentReach: lock.Selection.AgentReach, ToolLabels: map[string]string{}}
	if independentVideoProfile(build.ProfileID) {
		if len(lock.Upstream.Commit) != 40 || len(lock.Upstream.SourceSHA256) != 64 {
			return failure(400, "invalid_recipe_identity", "上游配方缺少固定源码身份")
		}
		lock.Upstream.Version = lock.Profile.Version
		identity.Upstream = &lock.Upstream
	}
	dockerfile, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(build.Dockerfile)))
	if err != nil {
		return err
	}
	labels := map[string]string{}
	for _, line := range strings.Split(string(dockerfile), "\n") {
		if !strings.HasPrefix(line, "LABEL ") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "LABEL "), "=", 2)
		if len(parts) != 2 {
			continue
		}
		value, err := strconv.Unquote(parts[1])
		if err == nil {
			labels[parts[0]] = value
		}
	}
	if labels["io.rundesk.profile"] != build.ProfileID || labels["org.opencontainers.image.version"] != identity.ProfileVersion || labels["io.rundesk.codex.version"] != build.CodexVersion {
		return failure(400, "invalid_recipe_identity", "Dockerfile 标签与 profile.lock.json / Codex 版本不一致")
	}
	if build.ProfileID == "video-build" {
		for _, key := range []string{"io.rundesk.hyperframes.version", "io.rundesk.gsap.version", "io.rundesk.hyperframes.lock", "io.rundesk.video-use.enabled"} {
			if labels[key] == "" {
				return fmt.Errorf("recipe is missing tool identity label %s", key)
			}
			identity.ToolLabels[key] = labels[key]
		}
		if labels["io.rundesk.video-use.enabled"] == "true" {
			for _, key := range []string{"io.rundesk.video-use.commit", "io.rundesk.video-use.adapter"} {
				identity.ToolLabels[key] = labels[key]
			}
		}
	}
	build.RuntimeIdentity = identity
	return nil
}

func frozenRuntimeImageMatches(build ImageBuild, image imageInspection) bool {
	identity := build.RuntimeIdentity
	labels := image.Config.Labels
	if labels["io.rundesk.profile"] != build.ProfileID || labels["org.opencontainers.image.version"] != identity.ProfileVersion || labels["io.rundesk.codex.version"] != build.CodexVersion || labels["io.rundesk.agent-reach"] != strconv.FormatBool(identity.AgentReach) {
		return false
	}
	if identity.Upstream != nil {
		upstream := identity.Upstream
		if labels["io.rundesk.upstream.repository"] != upstream.Repository || labels["io.rundesk.upstream.commit"] != upstream.Commit || labels["io.rundesk.upstream.source.sha256"] != upstream.SourceSHA256 || labels["io.rundesk.video-app-media.contract"] != "1" {
			return false
		}
	}
	for key, value := range identity.ToolLabels {
		if labels[key] != value {
			return false
		}
	}
	return true
}
