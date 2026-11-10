package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

func baseFixtureFiles(target string) map[string]string {
	plan := BuildBasePlan{SchemaVersion: 1, Target: target}
	files := map[string]string{
		"Dockerfile":             "ARG RD_BASE_IMAGE=node:22-bookworm-slim\nFROM ${RD_BASE_IMAGE}\nUSER root\nARG RD_TOOL_BASE_READY=0\nARG RD_NPM_REGISTRY=https://registry.npmjs.org\nRUN test \"${RD_TOOL_BASE_READY}\" = 1 || echo standalone\nRUN npm_config_registry=\"${RD_NPM_REGISTRY}\" true\nUSER node\n",
		"bases/install.sh":       "set -eu\necho shared\n",
		"bases/configure-apt.sh": "set -eu\necho apt\n",
	}
	parent := ""
	for _, id := range []string{"system", "media", "browser"} {
		name := "bases/" + id + ".Dockerfile"
		files[name] = "ARG RD_BASE_IMAGE=node:22-bookworm-slim\nFROM ${RD_BASE_IMAGE}\nUSER root\nARG RD_APT_MIRROR=http://deb.debian.org/debian\nARG RD_APT_SECURITY_MIRROR=http://deb.debian.org/debian-security\nCOPY bases/install.sh /install.sh\nRUN RD_APT_MIRROR=\"${RD_APT_MIRROR}\" RD_APT_SECURITY_MIRROR=\"${RD_APT_SECURITY_MIRROR}\" sh /install.sh " + id + "\n"
		plan.Stages = append(plan.Stages, BuildBaseStage{ID: id, Dockerfile: name, Parent: parent, Inputs: []string{"bases/install.sh", "bases/configure-apt.sh"}, Arguments: []string{"RD_APT_MIRROR", "RD_APT_SECURITY_MIRROR"}})
		parent = id
	}
	body, _ := json.Marshal(plan)
	files[buildBasePlanPath] = string(body)
	return files
}
func baseFixtureBuild(t *testing.T, m *Manager, target string, noCache bool) ImageBuild {
	t.Helper()
	data := buildZip(t, baseFixtureFiles(target))
	v, err := m.createBuildWithRecipe(DefaultInstance, "base fixture", "Dockerfile", 2, noCache, bytes.NewReader(data), nil, &ImageBuild{Scope: "recipe", Source: "build"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// This is a command-recording Docker fixture, not a real image build. It gives
// every successful base build a distinct ID, so following a mutable tag or
// silently rebuilding a frozen dependency is observable by the tests.
type baseFixtureDocker struct {
	*fakeDocker
	lock        sync.Mutex
	images      map[string]map[string]string
	root        string
	commands    [][]string
	generation  int
	failStage   string
	waitStage   string
	daemonError bool
}

func newBaseFixtureDocker() *baseFixtureDocker {
	d := &baseFixtureDocker{fakeDocker: newFakeDocker(), images: map[string]map[string]string{}, root: "sha256:" + strings.Repeat("1", 64)}
	d.images[d.root] = map[string]string{}
	return d
}
func (d *baseFixtureDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) >= 3 && args[0] == "image" && args[1] == "inspect" {
		d.lock.Lock()
		defer d.lock.Unlock()
		if d.daemonError {
			return nil, errors.New("Cannot connect to Docker daemon")
		}
		ref := args[2]
		if ref == "node:22-bookworm-slim" {
			ref = d.root
		}
		labels, ok := d.images[ref]
		if !ok {
			return nil, errors.New("No such image: " + ref)
		}
		return json.Marshal([]map[string]any{{"Id": ref, "Os": "linux", "Architecture": runtime.GOARCH, "Config": map[string]any{"Labels": labels}}})
	}
	return d.fakeDocker.Run(ctx, args...)
}
func (d *baseFixtureDocker) Command(args ...string) *exec.Cmd {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.commands = append(d.commands, append([]string{}, args...))
	key, stage, mode, id := "", "", "success", ""
	for i, a := range args {
		if a == "--label" && i+1 < len(args) {
			key = strings.TrimPrefix(args[i+1], "io.rundesk.base.key=")
		}
		if a == "--tag" && i+1 < len(args) && strings.HasPrefix(args[i+1], "rundesk-base/") {
			stage = strings.Split(strings.TrimPrefix(args[i+1], "rundesk-base/"), ":")[0]
		}
	}
	if len(args) > 0 && args[0] == "build" {
		d.generation++
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", key, d.generation)))
		id = fmt.Sprintf("sha256:%x", sum)
		if stage == d.failStage && stage != "" {
			mode = "fail"
		}
		if stage == d.waitStage && stage != "" {
			mode = "wait"
		}
		if mode == "success" {
			d.images[id] = map[string]string{"io.rundesk.base.key": key}
		}
		exe, _ := os.Executable()
		cmd := exec.Command(exe, append([]string{"-test.run=^TestBaseBuildProcess$", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "RUNDESK_BASE_HELPER="+mode, "RUNDESK_BASE_ID="+id)
		return cmd
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=^TestBuildCenterRecipeSmokeProcess$")
	cmd.Env = append(os.Environ(), "RUNDESK_RECIPE_SMOKE_FIXTURE=1")
	return cmd
}
func TestBaseBuildProcess(t *testing.T) {
	mode := os.Getenv("RUNDESK_BASE_HELPER")
	if mode == "" {
		return
	}
	if mode == "wait" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "fixture stage failure")
		os.Exit(2)
	}
	for i, a := range os.Args {
		if a == "--iidfile" && i+1 < len(os.Args) {
			if err := os.WriteFile(os.Args[i+1], []byte(os.Getenv("RUNDESK_BASE_ID")), 0600); err != nil {
				os.Exit(3)
			}
			os.Exit(0)
		}
	}
	os.Exit(4)
}
func (d *baseFixtureDocker) baseCommands() int {
	d.lock.Lock()
	defer d.lock.Unlock()
	n := 0
	for _, a := range d.commands {
		for _, s := range a {
			if strings.HasPrefix(s, "rundesk-base/") {
				n++
				break
			}
		}
	}
	return n
}
func loadBaseBuild(t *testing.T, m *Manager, v ImageBuild) ImageBuild {
	t.Helper()
	fresh, err := m.Build(v.InstanceID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func TestToolBasesShareSystemAndMediaAcrossPresets(t *testing.T) {
	m := quietRecipeManager(t)
	d := newBaseFixtureDocker()
	m.docker = d
	first := baseFixtureBuild(t, m, "browser", false)
	var logs bytes.Buffer
	args, err := m.prepareBuildBases(context.Background(), first, &logs)
	if err != nil {
		t.Fatal(err)
	}
	first = loadBaseBuild(t, m, first)
	if d.baseCommands() != 3 || len(first.BaseImages.Layers) != 3 || first.BaseImages.RootImageID != d.root || !strings.Contains(strings.Join(args, " "), "RD_TOOL_BASE_READY=1") {
		t.Fatal(first.BaseImages, args, d.baseCommands())
	}
	second := baseFixtureBuild(t, m, "media", false)
	_, err = m.prepareBuildBases(context.Background(), second, &logs)
	if err != nil {
		t.Fatal(err)
	}
	second = loadBaseBuild(t, m, second)
	if d.baseCommands() != 3 || first.BaseImages.Layers[0].ImageID != second.BaseImages.Layers[0].ImageID || first.BaseImages.Layers[1].ImageID != second.BaseImages.Layers[1].ImageID {
		t.Fatal("shared bases were rebuilt", first.BaseImages, second.BaseImages)
	}
	if !strings.Contains(logs.String(), "reusing") {
		t.Fatal(logs.String())
	}
	for _, entry := range d.commands {
		if entry[0] == "build" {
			for _, arg := range entry {
				if arg == "--builder" || arg == "--load" {
					t.Fatal("base requires BuildKit-specific CLI", entry)
				}
			}
		}
	}
}
func TestToolBaseKeysUseStageInputsAndAPTOnly(t *testing.T) {
	files := baseFixtureFiles("system")
	data := buildZip(t, files)
	v := ImageBuild{Dockerfile: "Dockerfile", BuildSources: &BuildSourceSnapshot{Arguments: map[string]string{"RD_NPM_REGISTRY": "https://npm.one.example", "RD_APT_MIRROR": "https://apt.one.example"}}}
	a, err := resolveBuildBaseSnapshot(v, data)
	if err != nil {
		t.Fatal(err)
	}
	files["Dockerfile"] += "# changed skills and Codex version\n"
	files["skills/SKILL.md"] = "new skill"
	v.BuildSources.Arguments["RD_NPM_REGISTRY"] = "https://npm.two.example"
	b, err := resolveBuildBaseSnapshot(v, buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if baseJSONHash(a) != baseJSONHash(b) {
		t.Fatal("leaf/npm changes invalidated base", a, b)
	}
	v.BuildSources.Arguments["RD_APT_MIRROR"] = "https://apt.two.example"
	c, err := resolveBuildBaseSnapshot(v, data)
	if err != nil {
		t.Fatal(err)
	}
	if baseJSONHash(a) == baseJSONHash(c) {
		t.Fatal("APT source absent from identity")
	}
	files["bases/install.sh"] += "echo changed\n"
	d, err := resolveBuildBaseSnapshot(v, buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if c.Layers[0].InputSHA256 == d.Layers[0].InputSHA256 {
		t.Fatal("base helper absent from input hash")
	}
}
func TestToolBaseFrozenRebuildIgnoresMovingTagsAndNoCache(t *testing.T) {
	m := quietRecipeManager(t)
	d := newBaseFixtureDocker()
	m.docker = d
	first := baseFixtureBuild(t, m, "media", false)
	if _, err := m.prepareBuildBases(context.Background(), first, io.Discard); err != nil {
		t.Fatal(err)
	}
	first = loadBaseBuild(t, m, first)
	oldID := first.BaseImages.Layers[1].ImageID
	d.root = "sha256:" + strings.Repeat("2", 64)
	d.images[d.root] = map[string]string{}
	newer := baseFixtureBuild(t, m, "media", true)
	if _, err := m.prepareBuildBases(context.Background(), newer, io.Discard); err != nil {
		t.Fatal(err)
	}
	if d.baseCommands() != 4 {
		t.Fatal("new no-cache build reused bases")
	}
	rebuilt, err := m.RebuildCenterBuild(first.ID, RebuildCenterBuildInput{RequestID: store.ID(), NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	args, err := m.prepareBuildBases(context.Background(), rebuilt, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if d.baseCommands() != 4 || !strings.Contains(strings.Join(args, " "), oldID) || rebuilt.ContextSHA256 != first.ContextSHA256 {
		t.Fatal("historical base identity drifted", args, rebuilt)
	}
	delete(d.images, oldID)
	_, err = m.prepareBuildBases(context.Background(), loadBaseBuild(t, m, rebuilt), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "冻结基础层") {
		t.Fatal("deleted historical dependency silently rebuilt", err)
	}
	if d.baseCommands() != 4 {
		t.Fatal("historical missing base started build")
	}
}
func TestToolBaseFailureAndCancellationKeepPartialIdentity(t *testing.T) {
	for _, mode := range []string{"fail", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			m := quietRecipeManager(t)
			d := newBaseFixtureDocker()
			m.docker = d
			if mode == "fail" {
				d.failStage = "media"
			} else {
				d.waitStage = "media"
			}
			v := baseFixtureBuild(t, m, "browser", false)
			ctx := context.Background()
			cancel := func() {}
			if mode == "cancel" {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
			}
			defer cancel()
			_, err := m.prepareBuildBases(ctx, v, io.Discard)
			if err == nil {
				t.Fatal("failed stage accepted")
			}
			v = loadBaseBuild(t, m, v)
			if v.BaseImages.RootImageID == "" || v.BaseImages.Layers[0].ImageID == "" || v.BaseImages.Layers[1].ImageID != "" || d.baseCommands() != 2 {
				t.Fatal("partial identity lost or descendant started", v.BaseImages, d.baseCommands())
			}
			matches, _ := filepath.Glob(filepath.Join(m.Data, "image-bases", "*", "context-*"))
			if len(matches) != 0 {
				t.Fatal("temporary context leaked", matches)
			}
		})
	}
}
func TestToolBaseCacheMissingVersusDaemonError(t *testing.T) {
	m := quietRecipeManager(t)
	d := newBaseFixtureDocker()
	m.docker = d
	one := baseFixtureBuild(t, m, "system", false)
	if _, err := m.prepareBuildBases(context.Background(), one, io.Discard); err != nil {
		t.Fatal(err)
	}
	one = loadBaseBuild(t, m, one)
	delete(d.images, one.BaseImages.Layers[0].ImageID)
	two := baseFixtureBuild(t, m, "system", false)
	if _, err := m.prepareBuildBases(context.Background(), two, io.Discard); err != nil {
		t.Fatal("new build could not repair stale cache", err)
	}
	if d.baseCommands() != 2 {
		t.Fatal(d.baseCommands())
	}
	d.daemonError = true
	three := baseFixtureBuild(t, m, "system", false)
	if _, err := m.prepareBuildBases(context.Background(), three, io.Discard); err == nil {
		t.Fatal("daemon error swallowed")
	}
	if d.baseCommands() != 2 {
		t.Fatal("daemon error started build/pull")
	}
}
func TestToolBasePlanRejectsUnsafeAndInvalidContracts(t *testing.T) {
	for _, mutation := range []string{"unknown", "path", "cycle", "argument", "missing", "duplicate", "duplicateJSON", "readyUnused", "commentUse", "prefixUse", "leafFROM", "extraFROM", "unlistedARG", "codexMode"} {
		t.Run(mutation, func(t *testing.T) {
			files := baseFixtureFiles("system")
			var p BuildBasePlan
			_ = json.Unmarshal([]byte(files[buildBasePlanPath]), &p)
			switch mutation {
			case "unknown":
				files[buildBasePlanPath] = `{"schemaVersion":1,"target":"system","extra":true,"stages":[]}`
			case "path":
				p.Stages[0].Inputs = append(p.Stages[0].Inputs, "../secret")
			case "cycle":
				p.Stages[1].Parent = "browser"
			case "argument":
				p.Stages[0].Arguments = append(p.Stages[0].Arguments, "RD_NPM_REGISTRY")
			case "missing":
				delete(files, "bases/install.sh")
			case "duplicate":
				p.Stages[0].Inputs = append(p.Stages[0].Inputs, "bases/install.sh")
			case "duplicateJSON":
				files[buildBasePlanPath] = strings.Replace(files[buildBasePlanPath], `"schemaVersion":1`, `"schemaVersion":1,"SchemaVersion":1`, 1)
			case "readyUnused":
				files["Dockerfile"] = strings.Replace(files["Dockerfile"], `RUN test "${RD_TOOL_BASE_READY}" = 1 || echo standalone`, `# ${RD_TOOL_BASE_READY}`, 1)
			case "commentUse":
				files["bases/system.Dockerfile"] = strings.Replace(files["bases/system.Dockerfile"], `RD_APT_MIRROR="${RD_APT_MIRROR}"`, `RD_APT_MIRROR=default`, 1) + "# ${RD_APT_MIRROR}\n"
			case "prefixUse":
				files["bases/system.Dockerfile"] = strings.Replace(files["bases/system.Dockerfile"], `${RD_APT_MIRROR}`, `$RD_APT_MIRROR_SUFFIX`, 1)
			case "leafFROM":
				files["Dockerfile"] = strings.Replace(files["Dockerfile"], "FROM ${RD_BASE_IMAGE}", "FROM node:other", 1)
			case "extraFROM":
				files["bases/system.Dockerfile"] += "FROM node:other\n"
			case "unlistedARG":
				p.Stages[0].Arguments = p.Stages[0].Arguments[:1]
			case "codexMode":
				p.Stages[0].Arguments = append(p.Stages[0].Arguments, "RD_CODEX_MODE")
				files["bases/system.Dockerfile"] += "ARG RD_CODEX_MODE=npm\nRUN echo ${RD_CODEX_MODE}\n"
			}
			if mutation != "unknown" && mutation != "duplicateJSON" {
				b, _ := json.Marshal(p)
				files[buildBasePlanPath] = string(b)
			}
			if _, _, _, err := parseBuildBases(buildZip(t, files), "Dockerfile"); err == nil {
				t.Fatal("bad base plan accepted")
			}
		})
	}
	legacy := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	if p, _, _, err := parseBuildBases(legacy, "Dockerfile"); err != nil || p != nil {
		t.Fatal("legacy recipe changed", p, err)
	}
	v := ImageBuild{Dockerfile: "Dockerfile", CodexRuntime: &BuildRuntimeSnapshot{Mode: "base"}}
	if p, err := resolveBuildBaseSnapshot(v, buildZip(t, baseFixtureFiles("browser"))); err != nil || p != nil {
		t.Fatal("explicit Codex base did not use legacy standalone behavior", p, err)
	}
}
func TestToolBaseJobUsesFrozenFinalImageAndRetainsProvenance(t *testing.T) {
	m := quietRecipeManager(t)
	d := newBaseFixtureDocker()
	m.docker = d
	v := baseFixtureBuild(t, m, "media", false)
	if _, _, err := m.executeBuild(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	fresh := loadBaseBuild(t, m, v)
	final := fresh.BaseImages.Layers[1].ImageID
	d.lock.Lock()
	defer d.lock.Unlock()
	found := false
	for _, command := range d.commands {
		if len(command) > 0 && command[0] == "build" {
			joined := strings.Join(command, " ")
			if strings.Contains(joined, "--tag rundesk-build/") {
				found = true
				if !strings.Contains(joined, "RD_BASE_IMAGE="+final) || !strings.Contains(joined, "RD_TOOL_BASE_READY=1") {
					t.Fatal(command)
				}
			}
		}
	}
	if !found {
		t.Fatal("preset build absent")
	}
}

func TestToolBaseSerialQueueRetainsCompletedDependencies(t *testing.T) {
	m := testManager(t)
	d := newBaseFixtureDocker()
	m.docker = d
	v := baseFixtureBuild(t, m, "media", false)
	finished := waitBuild(t, m, DefaultInstance, v.ID, "succeeded", "failed")
	if finished.Status != "succeeded" || finished.BaseImages == nil || finished.BaseImages.RootImageID == "" || len(finished.BaseImages.Layers) != 2 || finished.BaseImages.Layers[1].ImageID == "" {
		t.Fatal("queue completion lost dependency provenance", finished)
	}
	if d.baseCommands() != 2 {
		t.Fatal("queue did not prepare exact ancestor chain", d.baseCommands())
	}
	if _, err := os.Stat(filepath.Join(m.buildDir(v.ID), "context")); !os.IsNotExist(err) {
		t.Fatal("extracted context not cleaned", err)
	}
	if _, err := os.Stat(filepath.Join(m.buildDir(v.ID), "build-bases.lock.json")); err != nil {
		t.Fatal("frozen dependencies removed with extracted context", err)
	}
}

func TestToolBaseDownloadAndLocalAcquisitionDoNotBuild(t *testing.T) {
	for _, source := range []string{"pull", "local"} {
		t.Run(source, func(t *testing.T) {
			m := quietRecipeManager(t)
			d := newBaseFixtureDocker()
			m.docker = d
			v := baseFixtureBuild(t, m, "browser", false)
			// Even a retained plan/snapshot cannot turn acquisition of an existing
			// complete image into local APT preparation. The deliberately incomplete
			// fixture image can fail doctor validation; no tool base may be built.
			v.Scope, v.Source, v.Reference = "setup", source, "node:22-bookworm-slim"
			_, _, _ = m.executeBuild(context.Background(), v)
			if d.baseCommands() != 0 {
				t.Fatal("image acquisition unexpectedly prepared bases", d.commands)
			}
		})
	}
}
