package app

// Tool bases are ordinary recipe files. Their preparation runs inside the
// existing serial job, before the preset build, rather than enqueueing a child
// job which could never run while its parent waited.
import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/shengjuntu/rundesk/internal/store"
)

type BuildBaseStage struct {
	ID         string   `json:"id"`
	Dockerfile string   `json:"dockerfile"`
	Parent     string   `json:"parent,omitempty"`
	Inputs     []string `json:"inputs"`
	Arguments  []string `json:"arguments"`
}
type BuildBasePlan struct {
	SchemaVersion int              `json:"schemaVersion"`
	Target        string           `json:"target"`
	Stages        []BuildBaseStage `json:"stages"`
}
type BuildBaseLayer struct {
	ID            string            `json:"id"`
	InputSHA256   string            `json:"inputSha256"`
	Arguments     map[string]string `json:"arguments"`
	Key           string            `json:"key,omitempty"`
	ParentImageID string            `json:"parentImageId,omitempty"`
	ImageID       string            `json:"imageId,omitempty"`
}
type BuildBaseSnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	Platform      string           `json:"platform"`
	Target        string           `json:"target"`
	PlanSHA256    string           `json:"planSha256"`
	RootReference string           `json:"rootReference"`
	RootImageID   string           `json:"rootImageId,omitempty"`
	Layers        []BuildBaseLayer `json:"layers"`
}

const buildBasePlanPath = "bases/plan.json"

var baseImageIdentity = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var baseArgument = regexp.MustCompile(`^RD_[A-Z][A-Z0-9_]{0,75}$`)
var baseARGDefault = regexp.MustCompile(`(?m)^ARG ([A-Z][A-Z0-9_]*)=([^\r\n]+)$`)

func uniqueBaseJSON(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return fmt.Errorf("base plan JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			folded := strings.ToLower(name)
			if !ok || seen[folded] {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[folded] = true
			if err = uniqueBaseJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueBaseJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
func baseVariableUsed(text []byte, name string) bool {
	lines := strings.Split(string(text), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || regexp.MustCompile(`(?i)^\s*ARG\s`).MatchString(line) {
			lines[i] = ""
		}
	}
	return regexp.MustCompile(`\$(?:` + regexp.QuoteMeta(name) + `\b|\{` + regexp.QuoteMeta(name) + `(?:\}|[:?+%-]))`).MatchString(strings.Join(lines, "\n"))
}
func singleBaseParent(text []byte) bool {
	lines := strings.Split(string(text), "\n")
	count := 0
	for _, line := range lines {
		if regexp.MustCompile(`(?i)^\s*FROM\s`).MatchString(line) {
			count++
			if !regexp.MustCompile(`(?i)^\s*FROM\s+\$\{RD_BASE_IMAGE\}(?:\s+AS\s+[a-z0-9_-]+)?\s*$`).MatchString(line) {
				return false
			}
		}
	}
	return count == 1
}

// Reading here is deliberately stricter than a generic ZIP lookup: the plan is
// executable build input and must not acquire aliases, links or case collisions.
func baseRecipeFiles(data []byte) (map[string][]byte, map[string]os.FileMode, error) {
	a, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, err
	}
	files, modes, seen := map[string][]byte{}, map[string]os.FileMode{}, map[string]bool{}
	var total int64
	for _, f := range a.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !safeBuildPath(name) || seen[strings.ToLower(name)] || f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return nil, nil, failure(400, "invalid_base_plan", "基础镜像配方含不安全、重复或非普通文件")
		}
		seen[strings.ToLower(name)] = true
		if f.FileInfo().IsDir() {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		body, err := io.ReadAll(io.LimitReader(r, buildContextLimit-total+1))
		r.Close()
		if err != nil {
			return nil, nil, err
		}
		total += int64(len(body))
		if total > buildContextLimit {
			return nil, nil, failure(413, "build_context_limit", "基础镜像配方超过解压限制")
		}
		files[name], modes[name] = body, f.Mode()
	}
	return files, modes, nil
}
func baseStageFiles(stage BuildBaseStage) []string {
	out := append([]string{stage.Dockerfile}, stage.Inputs...)
	sort.Strings(out)
	return out
}
func baseDefaultArguments(text []byte) map[string]string {
	out := map[string]string{}
	for _, m := range baseARGDefault.FindAllStringSubmatch(string(text), -1) {
		out[m[1]] = strings.Trim(strings.TrimSpace(m[2]), `"'`)
	}
	return out
}
func parseBuildBases(data []byte, dockerfile string) (*BuildBasePlan, map[string][]byte, map[string]os.FileMode, error) {
	// An absent plan is the legacy contract, including frozen old/imported ZIPs.
	a, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, nil, err
	}
	found := false
	for _, f := range a.File {
		if f.Name == buildBasePlanPath {
			found = true
			break
		}
	}
	if !found {
		return nil, nil, nil, nil
	}
	files, modes, err := baseRecipeFiles(data)
	if err != nil {
		return nil, nil, nil, err
	}
	body := files[buildBasePlanPath]
	if len(body) > 64<<10 {
		return nil, nil, nil, failure(400, "invalid_base_plan", "基础镜像计划超过64KiB")
	}
	if err = uniqueBaseJSON(json.NewDecoder(bytes.NewReader(body)), 0); err != nil {
		return nil, nil, nil, failure(400, "invalid_base_plan", "基础镜像计划含无效或重复JSON字段")
	}
	var plan BuildBasePlan
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err = d.Decode(&plan); err != nil {
		return nil, nil, nil, failure(400, "invalid_base_plan", "基础镜像计划JSON无效")
	}
	if d.Decode(new(any)) != io.EOF || plan.SchemaVersion != 1 || len(plan.Stages) < 1 || len(plan.Stages) > 3 {
		return nil, nil, nil, failure(400, "invalid_base_plan", "基础镜像计划须为schemaVersion1、1–3层")
	}
	leaf, ok := files[dockerfile]
	if !ok || !regexp.MustCompile(`(?m)^ARG RD_TOOL_BASE_READY(?:=|\s*$)`).Match(leaf) || !baseVariableUsed(leaf, "RD_TOOL_BASE_READY") || !regexp.MustCompile(`(?m)^ARG RD_BASE_IMAGE=`).Match(leaf) || !singleBaseParent(leaf) {
		return nil, nil, nil, failure(400, "invalid_base_plan", "预设缺少RD_TOOL_BASE_READY/RD_BASE_IMAGE兼容接口")
	}
	byID := map[string]BuildBaseStage{}
	for i, s := range plan.Stages {
		if !sourceIdentifier.MatchString(s.ID) || byID[s.ID].ID != "" || !safeBuildPath(s.Dockerfile) || len(s.Inputs) > 32 || len(s.Arguments) > 16 || (i == 0 && s.Parent != "") || (i > 0 && s.Parent != plan.Stages[i-1].ID) {
			return nil, nil, nil, failure(400, "invalid_base_plan", "基础镜像层须按父层顺序排列，且名称/文件有效")
		}
		text, ok := files[s.Dockerfile]
		if !ok || len(text) > 1<<20 {
			return nil, nil, nil, failure(400, "invalid_base_plan", "基础层Dockerfile不存在或过大")
		}
		if !regexp.MustCompile(`(?m)^ARG RD_BASE_IMAGE(?:=|\s*$)`).Match(text) || !singleBaseParent(text) {
			return nil, nil, nil, failure(400, "invalid_base_plan", "基础层须从RD_BASE_IMAGE继承")
		}
		seen := map[string]bool{}
		for _, n := range baseStageFiles(s) {
			if !safeBuildPath(n) || seen[strings.ToLower(n)] {
				return nil, nil, nil, failure(400, "invalid_base_plan", "基础层输入路径无效或重复")
			}
			seen[strings.ToLower(n)] = true
			if _, ok := files[n]; !ok {
				return nil, nil, nil, failure(400, "invalid_base_plan", "基础层输入文件缺失: "+n)
			}
		}
		args := map[string]bool{}
		for _, name := range s.Arguments {
			used := baseVariableUsed(text, name)
			if !baseArgument.MatchString(name) || name == "RD_BASE_IMAGE" || name == "RD_TOOL_BASE_READY" || name == "RD_CODEX_MODE" || args[name] || !regexp.MustCompile(`(?m)^ARG `+regexp.QuoteMeta(name)+`(?:=|\s*$)`).Match(text) || !used {
				return nil, nil, nil, failure(400, "invalid_base_plan", "基础层参数未声明或重复")
			}
			args[name] = true
		}
		for _, declaration := range sourceARG.FindAllStringSubmatch(string(text), -1) {
			name := declaration[1]
			if strings.HasPrefix(name, "RD_") && name != "RD_BASE_IMAGE" && baseVariableUsed(text, name) && !args[name] {
				return nil, nil, nil, failure(400, "invalid_base_plan", "基础层使用的下载参数未列入冻结输入: "+name)
			}
		}
		byID[s.ID] = s
	}
	selected, ok := byID[plan.Target]
	if !ok {
		return nil, nil, nil, failure(400, "invalid_base_plan", "找不到目标基础层")
	}
	chain := []BuildBaseStage{selected}
	for selected.Parent != "" {
		selected = byID[selected.Parent]
		chain = append([]BuildBaseStage{selected}, chain...)
	}
	plan.Stages = chain
	return &plan, files, modes, nil
}
func buildBaseDockerfiles(data []byte, dockerfile string) ([][]byte, error) {
	p, files, _, err := parseBuildBases(data, dockerfile)
	if err != nil || p == nil {
		return nil, err
	}
	out := make([][]byte, 0, len(p.Stages))
	for _, s := range p.Stages {
		out = append(out, files[s.Dockerfile])
	}
	return out, nil
}
func baseJSONHash(value any) string {
	body, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}
func baseLayerInputSHA(stage BuildBaseStage, files map[string][]byte, modes map[string]os.FileMode) string {
	h := sha256.New()
	for _, n := range baseStageFiles(stage) {
		fmt.Fprintf(h, "%d:%s:%o:%d:", len(n), n, modes[n]&0111, len(files[n]))
		h.Write(files[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}
func resolveBuildBaseSnapshot(v ImageBuild, data []byte) (*BuildBaseSnapshot, error) {
	p, files, modes, err := parseBuildBases(data, v.Dockerfile)
	if err != nil || p == nil {
		return nil, err
	}
	if v.CodexRuntime != nil && v.CodexRuntime.Mode == "base" {
		return nil, nil
	}
	root := baseDefaultArguments(files[v.Dockerfile])["RD_BASE_IMAGE"]
	if v.BuildSources != nil && v.BuildSources.Arguments["RD_BASE_IMAGE"] != "" {
		root = v.BuildSources.Arguments["RD_BASE_IMAGE"]
	}
	if root == "" || strings.Contains(root, "$") || buildSourceValue("image", root) != nil {
		return nil, failure(400, "invalid_base_plan", "基础镜像根引用须是明确的镜像名或摘要")
	}
	s := &BuildBaseSnapshot{SchemaVersion: 1, Platform: "linux/" + runtime.GOARCH, Target: p.Target, PlanSHA256: baseJSONHash(p), RootReference: root, Layers: []BuildBaseLayer{}}
	for _, stage := range p.Stages {
		defaults := baseDefaultArguments(files[stage.Dockerfile])
		args := map[string]string{}
		for _, name := range stage.Arguments {
			value, ok := defaults[name]
			if v.BuildSources != nil {
				if override, exists := v.BuildSources.Arguments[name]; exists {
					value, ok = override, true
				}
			}
			if !ok || strings.Contains(value, "$") {
				return nil, failure(400, "invalid_base_plan", "基础层参数须有明确默认值或已选下载源: "+name)
			}
			args[name] = value
		}
		s.Layers = append(s.Layers, BuildBaseLayer{ID: stage.ID, InputSHA256: baseLayerInputSHA(stage, files, modes), Arguments: args})
	}
	return s, nil
}
func baseSnapshotInputs(s *BuildBaseSnapshot) *BuildBaseSnapshot {
	if s == nil {
		return nil
	}
	body, _ := json.Marshal(s)
	var out BuildBaseSnapshot
	_ = json.Unmarshal(body, &out)
	out.RootImageID = ""
	for i := range out.Layers {
		out.Layers[i].Key = ""
		out.Layers[i].ParentImageID = ""
		out.Layers[i].ImageID = ""
	}
	return &out
}
func writeBuildBaseLock(dir string, s *BuildBaseSnapshot) error {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "base-lock-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(body, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(name, 0600); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "build-bases.lock.json"))
}
func (m *Manager) freezeBuildBases(v *ImageBuild, data []byte, dir string) error {
	expected, err := resolveBuildBaseSnapshot(*v, data)
	if err != nil {
		return err
	}
	if v.BaseImages != nil {
		if baseJSONHash(baseSnapshotInputs(v.BaseImages)) != baseJSONHash(expected) {
			return failure(409, "base_snapshot_changed", "冻结基础镜像与配方/下载源不一致")
		}
	} else {
		v.BaseImages = expected
	}
	if v.BaseImages == nil {
		return nil
	}
	return writeBuildBaseLock(dir, v.BaseImages)
}
func (m *Manager) persistBuildBases(v ImageBuild, s *BuildBaseSnapshot) error {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	latest, err := m.buildLocked(v.InstanceID, v.ID)
	if err != nil {
		return err
	}
	if err = writeBuildBaseLock(m.buildDir(v.ID), s); err != nil {
		return err
	}
	latest.BaseImages = s
	latest.Updated = store.Now()
	return m.Store.Put("image-build", latest.ID, latest)
}
func (m *Manager) checkedBaseImage(id, key string) (imageInspection, error) {
	if !baseImageIdentity.MatchString(id) {
		return imageInspection{}, failure(409, "base_image_identity", "基础镜像身份无效")
	}
	image, err := m.inspectImage(id)
	if err != nil {
		return image, err
	}
	if key != "" && image.Config.Labels["io.rundesk.base.key"] != key {
		return image, failure(409, "base_image_identity", "基础镜像的输入身份标签不一致")
	}
	return image, nil
}

// Only an explicit missing-image response permits acquisition or rebuilding.
// Daemon/socket errors, platform mismatches and invalid labels must fail closed.
func baseImageMissing(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such image") || strings.Contains(text, "image not found") || strings.Contains(text, "image missing")
}
func (m *Manager) prepareBuildBases(ctx context.Context, v ImageBuild, out io.Writer) ([]string, error) {
	if v.BaseImages == nil {
		if v.CodexRuntime != nil && v.CodexRuntime.Mode == "base" {
			fmt.Fprintln(out, "Tool bases: explicit Codex base selected; using the recipe's standalone tool installation on that frozen image.")
		}
		return nil, nil
	}
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(v.ID), "context.zip"), v.ContextSHA256)
	if err != nil {
		return nil, err
	}
	plan, files, modes, err := parseBuildBases(data, v.Dockerfile)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, failure(409, "base_snapshot_changed", "冻结配方缺少基础层计划")
	}
	expected, err := resolveBuildBaseSnapshot(v, data)
	if err != nil {
		return nil, err
	}
	if baseJSONHash(baseSnapshotInputs(v.BaseImages)) != baseJSONHash(expected) {
		return nil, failure(409, "base_snapshot_changed", "基础镜像输入与冻结配方不一致")
	}
	body, err := os.ReadFile(filepath.Join(m.buildDir(v.ID), "build-bases.lock.json"))
	if err != nil {
		return nil, err
	}
	var locked BuildBaseSnapshot
	if json.Unmarshal(body, &locked) != nil || baseJSONHash(&locked) != baseJSONHash(v.BaseImages) {
		return nil, failure(409, "base_snapshot_changed", "基础镜像锁定记录不一致")
	}
	// Work on an independent copy; Store encoding and cancellation can proceed
	// concurrently without observing a partially changed slice.
	encoded, _ := json.Marshal(v.BaseImages)
	var snapshot BuildBaseSnapshot
	_ = json.Unmarshal(encoded, &snapshot)
	if snapshot.RootImageID != "" {
		if _, err = m.checkedBaseImage(snapshot.RootImageID, ""); err != nil {
			return nil, fmt.Errorf("冻结根镜像已不可用，拒绝改用移动标签：%w", err)
		}
	} else {
		image, inspectErr := m.inspectImage(snapshot.RootReference)
		if inspectErr != nil {
			if !baseImageMissing(inspectErr) {
				return nil, inspectErr
			}
			fmt.Fprintf(out, "Tool bases: acquiring root %s (%s).\n", snapshot.RootReference, snapshot.Platform)
			if err = m.runtimeDockerCommand(ctx, out, "pull", "--platform", snapshot.Platform, snapshot.RootReference); err != nil {
				return nil, fmt.Errorf("基础根镜像下载失败: %w", err)
			}
			image, inspectErr = m.inspectImage(snapshot.RootReference)
		}
		if inspectErr != nil {
			return nil, inspectErr
		}
		if !baseImageIdentity.MatchString(image.ID) {
			return nil, failure(502, "base_image_identity", "根镜像身份无效")
		}
		snapshot.RootImageID = image.ID
		if err = m.persistBuildBases(v, &snapshot); err != nil {
			return nil, err
		}
	}
	parent := snapshot.RootImageID
	for i, stage := range plan.Stages {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		layer := &snapshot.Layers[i]
		key := baseJSONHash(struct {
			Version                  int
			Platform, Parent, Inputs string
			Arguments                map[string]string
		}{1, snapshot.Platform, parent, layer.InputSHA256, layer.Arguments})
		if layer.ImageID != "" {
			if layer.ParentImageID != parent || layer.Key != key {
				return nil, failure(409, "base_snapshot_changed", "冻结基础层的父镜像或输入身份不一致")
			}
			if _, err = m.checkedBaseImage(layer.ImageID, key); err != nil {
				return nil, fmt.Errorf("冻结基础层 %s 不可用，拒绝静默重建：%w", layer.ID, err)
			}
			fmt.Fprintf(out, "Tool base %d/%d %s: reusing frozen %s.\n", i+1, len(plan.Stages), stage.ID, layer.ImageID)
			parent = layer.ImageID
			continue
		}
		layer.Key, layer.ParentImageID = key, parent
		cacheDir := filepath.Join(m.Data, "image-bases", key)
		if err = os.MkdirAll(cacheDir, 0700); err != nil {
			return nil, err
		}
		if !v.NoCache {
			cached, readErr := os.ReadFile(filepath.Join(cacheDir, "image.json"))
			if readErr == nil {
				var old BuildBaseLayer
				if json.Unmarshal(cached, &old) != nil || old.Key != key || old.ParentImageID != parent || old.InputSHA256 != layer.InputSHA256 || baseJSONHash(old.Arguments) != baseJSONHash(layer.Arguments) {
					return nil, failure(409, "base_cache_changed", "公共基础镜像缓存记录损坏")
				}
				if _, checkErr := m.checkedBaseImage(old.ImageID, key); checkErr == nil {
					layer.ImageID = old.ImageID
					fmt.Fprintf(out, "Tool base %d/%d %s: reusing %s.\n", i+1, len(plan.Stages), stage.ID, layer.ImageID)
				} else if baseImageMissing(checkErr) {
					fmt.Fprintf(out, "Tool base %s: cached image unavailable; rebuilding from the pinned parent and frozen inputs.\n", stage.ID)
				} else {
					return nil, checkErr
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				return nil, readErr
			}
		}
		if layer.ImageID == "" {
			fmt.Fprintf(out, "Tool base %d/%d %s: building (key %s).\n", i+1, len(plan.Stages), stage.ID, key)
			ctxDir, err := os.MkdirTemp(cacheDir, "context-")
			if err != nil {
				return nil, err
			}
			func() {
				defer os.RemoveAll(ctxDir)
				for _, n := range baseStageFiles(stage) {
					dest := filepath.Join(ctxDir, filepath.FromSlash(n))
					if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
						return
					}
					mode := os.FileMode(0644)
					if modes[n]&0111 != 0 {
						mode = 0755
					}
					if err = os.WriteFile(dest, files[n], mode); err != nil {
						return
					}
				}
				iidfile := filepath.Join(ctxDir, "image-id")
				args := []string{"build", "--pull=false", "--platform", snapshot.Platform, "--iidfile", iidfile, "--tag", "rundesk-base/" + stage.ID + ":" + key, "--label", "io.rundesk.base.key=" + key, "--file", filepath.Join(ctxDir, filepath.FromSlash(stage.Dockerfile)), "--build-arg", "RD_BASE_IMAGE=" + parent}
				if v.NoCache {
					args = append(args, "--no-cache")
				}
				names := make([]string, 0, len(layer.Arguments))
				for name := range layer.Arguments {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					args = append(args, "--build-arg", name+"="+layer.Arguments[name])
				}
				args = append(args, ctxDir)
				if err = m.runtimeDockerCommand(ctx, out, args...); err != nil {
					return
				}
				var b []byte
				b, err = os.ReadFile(iidfile)
				if err != nil {
					return
				}
				layer.ImageID = strings.TrimSpace(string(b))
				_, err = m.checkedBaseImage(layer.ImageID, key)
			}()
			if err != nil {
				return nil, fmt.Errorf("公共基础层 %s 构建失败：%w", stage.ID, err)
			}
			cache, err := json.MarshalIndent(layer, "", "  ")
			if err != nil {
				return nil, err
			}
			if err = os.WriteFile(filepath.Join(cacheDir, "image.json"), append(cache, '\n'), 0600); err != nil {
				return nil, err
			}
		}
		if err = m.persistBuildBases(v, &snapshot); err != nil {
			return nil, err
		}
		parent = layer.ImageID
	}
	fmt.Fprintf(out, "Tool bases ready: %s -> preset %s.\n", parent, v.ProfileID)
	return []string{"--build-arg", "RD_BASE_IMAGE=" + parent, "--build-arg", "RD_TOOL_BASE_READY=1"}, nil
}
