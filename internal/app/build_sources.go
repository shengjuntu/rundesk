package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/shengjuntu/rundesk/examples/profiletemplates"
)

// Build source catalogs are ordinary deployment files, never compiled assets.
// Addresses are public configuration. Credentials belong in a separate secret
// mechanism and are intentionally not accepted in this catalog or a snapshot.
type BuildSourceField struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Argument string `json:"argument"`
	Kind     string `json:"kind,omitempty"`
}
type BuildSourceType struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Fields        []BuildSourceField `json:"fields"`
	DetectPattern string             `json:"detectPattern,omitempty"`
}
type BuildSourceEntry struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Type                  string            `json:"type"`
	Values                map[string]string `json:"values"`
	Description           string            `json:"description,omitempty"`
	RequiresConfiguration bool              `json:"requiresConfiguration,omitempty"`
}
type BuildSourceGroup struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Sources []BuildSourceEntry `json:"sources"`
}
type BuildSourceCatalog struct {
	SchemaVersion int                `json:"schemaVersion"`
	DefaultGroup  string             `json:"defaultGroup,omitempty"`
	Revision      string             `json:"revision,omitempty"`
	Groups        []BuildSourceGroup `json:"groups"`
	Types         []BuildSourceType  `json:"types,omitempty"`
}
type BuildSourceOptions struct {
	Group           string                       `json:"group"`
	CatalogRevision string                       `json:"catalogRevision,omitempty"`
	Selections      map[string]string            `json:"selections,omitempty"`
	Overrides       map[string]map[string]string `json:"overrides,omitempty"`
}
type BuildSourceSnapshot struct {
	Definitions  []BuildSourceType            `json:"definitions,omitempty"`
	Group        string                       `json:"group"`
	CatalogSHA   string                       `json:"catalogSHA"`
	Selections   map[string]string            `json:"selections"`
	Values       map[string]map[string]string `json:"values"`
	Arguments    map[string]string            `json:"arguments"`
	Unconfigured []string                     `json:"unconfigured,omitempty"`
}
type BuildSourceCapability struct {
	Type      string   `json:"type"`
	Detected  bool     `json:"detected"`
	Supported bool     `json:"supported"`
	Arguments []string `json:"arguments"`
}
type BuildSourceCapabilities struct {
	Types []BuildSourceCapability `json:"types"`
}

var buildSourceCatalogMu sync.Mutex
var sourceIdentifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,79}$`)

func normalizedBuildSourceTypes(types []BuildSourceType) ([]BuildSourceType, error) {
	if len(types) == 0 || len(types) > 32 {
		return nil, failure(400, "invalid_source_types", "须在外置列表定义1–32种构建源类型")
	}
	kinds, args := map[string]bool{}, map[string]bool{}
	for i := range types {
		kind := &types[i]
		if !sourceIdentifier.MatchString(kind.ID) || kinds[kind.ID] || kind.Name == "" || len(kind.Name) > 160 || len(kind.Fields) < 1 || len(kind.Fields) > 16 || len(kind.DetectPattern) > 1024 {
			return nil, failure(400, "invalid_source_types", "源类型定义无效")
		}
		kinds[kind.ID] = true
		if kind.DetectPattern != "" {
			if _, err := regexp.Compile(kind.DetectPattern); err != nil {
				return nil, failure(400, "invalid_source_types", "源识别表达式无效")
			}
		}
		fields := map[string]bool{}
		for j := range kind.Fields {
			field := &kind.Fields[j]
			if !regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,79}$`).MatchString(field.ID) || fields[field.ID] || field.Label == "" || len(field.Label) > 160 || !regexp.MustCompile(`^RD_[A-Z][A-Z0-9_]{0,75}$`).MatchString(field.Argument) || args[field.Argument] {
				return nil, failure(400, "invalid_source_types", "源字段或构建参数定义无效")
			}
			fields[field.ID], args[field.Argument] = true, true
			if field.Kind == "" {
				field.Kind = "url"
			}
			if field.Kind != "url" && field.Kind != "urls" && field.Kind != "image" {
				return nil, failure(400, "invalid_source_types", "源字段仅支持url、urls或image格式")
			}
		}
	}
	return types, nil
}

func sourceTypeFrom(types []BuildSourceType, id string) (BuildSourceType, bool) {
	for _, kind := range types {
		if kind.ID == id {
			return kind, true
		}
	}
	return BuildSourceType{}, false
}

func buildSourceValue(format, value string) error {
	if len(value) > 2048 || strings.ContainsAny(value, "\x00\r\n\t") || recipeSecretLiteral.MatchString(value) {
		return failure(400, "invalid_build_source", "源地址含无效字符或凭据；请单独配置访问认证")
	}
	if value == "" {
		return nil
	}
	if format == "image" {
		if !imageReference.MatchString(value) || strings.Contains(value, "://") || (strings.Contains(value, "@") && !regexp.MustCompile(`@[A-Za-z0-9_+.-]+:[a-fA-F0-9]{32,}$`).MatchString(value)) {
			return failure(400, "invalid_build_source", "基础镜像应是完整镜像引用，不是 URL")
		}
		return nil
	}
	addresses := []string{value}
	if format == "urls" {
		addresses = strings.Fields(value)
		if len(addresses) > 16 {
			return failure(400, "invalid_build_source", "Conda 最多配置 16 个 channel URL")
		}
	}
	for _, address := range addresses {
		u, err := url.Parse(address)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(address, " \\|`\"<>") {
			return failure(400, "invalid_build_source", "源必须是无账号、查询参数和片段的 HTTP(S) 地址")
		}
	}
	return nil
}

func validateBuildSourceValues(types []BuildSourceType, kind string, values map[string]string) error {
	definition, ok := sourceTypeFrom(types, kind)
	if !ok || len(values) > len(definition.Fields) {
		return failure(400, "invalid_build_source_type", "不支持的源类型或字段")
	}
	fields := map[string]BuildSourceField{}
	for _, field := range definition.Fields {
		fields[field.ID] = field
	}
	for name, value := range values {
		field, ok := fields[name]
		if !ok {
			return failure(400, "invalid_build_source_field", "源配置包含未知字段")
		}
		if err := buildSourceValue(field.Kind, value); err != nil {
			return err
		}
	}
	return nil
}

func normalizeBuildSourceCatalog(in BuildSourceCatalog) (BuildSourceCatalog, error) {
	if in.SchemaVersion != 1 || len(in.Groups) < 1 || len(in.Groups) > 16 {
		return in, failure(400, "invalid_source_catalog", "源列表须为 schemaVersion 1，包含 1–16 个分组")
	}
	types, err := normalizedBuildSourceTypes(in.Types)
	if err != nil {
		return in, err
	}
	in.Types = types
	groups, entries := map[string]bool{}, map[string]bool{}
	for _, group := range in.Groups {
		if !sourceIdentifier.MatchString(group.ID) || groups[group.ID] || group.Name == "" || len(group.Name) > 160 || len(group.Sources) > 128 {
			return in, failure(400, "invalid_source_catalog", "源分组名称、标识或条目数量无效")
		}
		groups[group.ID] = true
		for _, source := range group.Sources {
			if !sourceIdentifier.MatchString(source.ID) || entries[source.ID] || source.Name == "" || len(source.Name) > 160 || len(source.Description) > 2000 || strings.ContainsAny(source.Name+source.Description, "\x00\r") {
				return in, failure(400, "invalid_source_catalog", "源条目需要唯一标识和有效名称")
			}
			entries[source.ID] = true
			if err := validateBuildSourceValues(in.Types, source.Type, source.Values); err != nil {
				return in, err
			}
		}
	}
	if in.DefaultGroup == "" {
		in.DefaultGroup = "international"
		if !groups[in.DefaultGroup] {
			in.DefaultGroup = in.Groups[0].ID
		}
	}
	if !groups[in.DefaultGroup] {
		return in, failure(400, "invalid_source_catalog", "默认源分组不存在")
	}
	return in, nil
}

func buildSourceDefaultFile() (string, error) {
	paths := []string{}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "config", "build-sources.json"))
	}
	if dir, err := os.Getwd(); err == nil {
		for level := 0; level < 4; level++ {
			paths = append(paths, filepath.Join(dir, "config", "build-sources.json"))
			dir = filepath.Dir(dir)
		}
	}
	for _, path := range paths {
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", failure(409, "source_catalog_missing", "找不到外置 config/build-sources.json；请随程序部署配方和配置目录，或指定 RUNDESK_BUILD_SOURCES")
}

func (m *Manager) buildSourceCatalogFile() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("RUNDESK_BUILD_SOURCES")); configured != "" {
		return configured, nil
	}
	target := filepath.Join(m.Data, "config", "build-sources.json")
	if _, err := os.Stat(target); err == nil {
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	source, err := buildSourceDefaultFile()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	if err = os.WriteFile(target, data, 0600); err != nil {
		return "", err
	}
	return target, nil
}

func (m *Manager) readBuildSources() (BuildSourceCatalog, string, error) {
	path, err := m.buildSourceCatalogFile()
	if err != nil {
		return BuildSourceCatalog{}, "", err
	}
	catalog, err := readBuildSourceCatalog(path)
	return catalog, path, err
}

func readBuildSourceCatalog(path string) (BuildSourceCatalog, error) {
	var catalog BuildSourceCatalog
	f, err := os.Open(path)
	if err != nil {
		return catalog, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return catalog, failure(400, "invalid_source_catalog", "源列表不可读或超过 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&catalog) != nil {
		return catalog, failure(400, "invalid_source_catalog", "外置源列表 JSON 格式无效")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return catalog, failure(400, "invalid_source_catalog", "源列表须为单个 JSON 对象")
	}
	catalog, err = normalizeBuildSourceCatalog(catalog)
	if err != nil {
		return catalog, err
	}
	sum := sha256.Sum256(data)
	catalog.Revision = hex.EncodeToString(sum[:])
	return catalog, nil
}

// Defaults are deployment files, not an implicit migration of a user's catalog.
// The UI can review missing entries and save an explicit merge using the normal
// revision-checked PUT. Reading defaults never modifies persistent settings.
func (m *Manager) BuildSourceDefaults() (BuildSourceCatalog, error) {
	path, err := buildSourceDefaultFile()
	if err != nil {
		return BuildSourceCatalog{}, err
	}
	return readBuildSourceCatalog(path)
}

func (m *Manager) BuildSources() (BuildSourceCatalog, error) {
	buildSourceCatalogMu.Lock()
	defer buildSourceCatalogMu.Unlock()
	catalog, _, err := m.readBuildSources()
	return catalog, err
}

func (m *Manager) SaveBuildSources(in BuildSourceCatalog) (BuildSourceCatalog, error) {
	buildSourceCatalogMu.Lock()
	defer buildSourceCatalogMu.Unlock()
	current, path, err := m.readBuildSources()
	if err != nil {
		return in, err
	}
	if in.Revision != current.Revision {
		return in, failure(409, "source_catalog_conflict", "源列表已变化，请刷新后再保存")
	}
	in, err = normalizeBuildSourceCatalog(in)
	if err != nil {
		return in, err
	}
	in.Revision = ""
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return in, err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".build-sources-")
	if err != nil {
		return in, err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return in, err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return in, err
	}
	if err = temp.Close(); err != nil {
		return in, err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return in, err
	}
	in, _, err = m.readBuildSources()
	return in, err
}

var sourceARG = regexp.MustCompile(`(?im)^\s*ARG\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s*=|\s|$)`)

func sourceCapabilitiesFor(dockerfile []byte, types []BuildSourceType) BuildSourceCapabilities {
	text := string(dockerfile)
	// Comments are excluded so a documentation example cannot claim support.
	lines := strings.Split(text, "\n")
	for n, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[n] = ""
		}
	}
	text = strings.Join(lines, "\n")
	usageLines := append([]string{}, lines...)
	for n, line := range usageLines {
		if regexp.MustCompile(`(?i)^\s*ARG\s`).MatchString(line) {
			usageLines[n] = ""
		}
	}
	usageText := strings.Join(usageLines, "\n")
	declared := map[string]bool{}
	for _, match := range sourceARG.FindAllStringSubmatch(text, -1) {
		declared[match[1]] = true
	}
	out := BuildSourceCapabilities{Types: []BuildSourceCapability{}}
	for _, kind := range types {
		detected := false
		if kind.DetectPattern != "" {
			detected = regexp.MustCompile(kind.DetectPattern).MatchString(text)
		}
		capability := BuildSourceCapability{Type: kind.ID, Detected: detected, Arguments: []string{}}
		for _, field := range kind.Fields {
			// A declaration alone does not make a custom Dockerfile configurable.
			used := regexp.MustCompile(`\$(?:` + regexp.QuoteMeta(field.Argument) + `\b|\{` + regexp.QuoteMeta(field.Argument) + `(?:\}|[:?+%-]))`).MatchString(usageText)
			if declared[field.Argument] && used {
				capability.Arguments = append(capability.Arguments, field.Argument)
				capability.Supported, capability.Detected = true, true
			}
		}
		if capability.Detected {
			out.Types = append(out.Types, capability)
		}
	}
	return out
}

func recipeDockerfile(data []byte, name string) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range archive.File {
		if file.Name == name {
			reader, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer reader.Close()
			content, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
			if err != nil || len(content) > 1<<20 {
				return nil, failure(400, "invalid_dockerfile", "Dockerfile 不可读或超过 1 MiB")
			}
			return content, nil
		}
	}
	return nil, failure(400, "invalid_dockerfile", "配方缺少 Dockerfile")
}

func builtinRecipeSourceContext(profile string) ([]byte, error) {
	if !sourceIdentifier.MatchString(profile) {
		return nil, failure(404, "recipe_not_found", "构建配方不存在")
	}
	data, err := profiletemplates.Files.ReadFile(profile + ".zip")
	if err != nil {
		return nil, failure(404, "recipe_not_found", "外置构建配方不存在或不可读")
	}
	return data, nil
}

// Metadata and build input come from one read of an editable recipe.
func builtinRecipeFromSource(profile string, data []byte) (BuildRecipe, error) {
	var recipe BuildRecipe
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return recipe, err
	}
	var metadata struct {
		Profile struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Version     string `json:"version"`
		} `json:"profile"`
	}
	found := false
	for _, file := range archive.File {
		if file.Name == "profile.lock.json" {
			reader, err := file.Open()
			if err != nil {
				return recipe, err
			}
			err = json.NewDecoder(io.LimitReader(reader, 256<<10)).Decode(&metadata)
			reader.Close()
			if err != nil {
				return recipe, err
			}
			found = true
			break
		}
	}
	if !found {
		return recipe, failure(400, "invalid_recipe", "外置配方缺少 profile.lock.json")
	}
	manifest, err := normalizeRecipeManifest(BuildRecipeManifest{SchemaVersion: 1, Name: metadata.Profile.Title, Description: metadata.Profile.Description, Version: metadata.Profile.Version, Dockerfile: "Dockerfile"})
	if err != nil {
		return recipe, err
	}
	sum := sha256.Sum256(data)
	contextSHA := hex.EncodeToString(sum[:])
	return BuildRecipe{BuildRecipeManifest: manifest, ID: "builtin-" + profile, Kind: "builtin", ProfileID: profile, Revision: 1, ContextSHA256: contextSHA, RecipeSHA: recipeDigest(manifest, contextSHA)}, nil
}

func (m *Manager) resolveBuildSources(options *BuildSourceOptions, data []byte, dockerfile string) (*BuildSourceSnapshot, error) {
	if options == nil {
		return nil, nil // Existing API clients keep the recipe's original behavior.
	}
	catalog, err := m.BuildSources()
	if err != nil {
		return nil, err
	}
	if options.CatalogRevision != "" && options.CatalogRevision != catalog.Revision {
		return nil, failure(409, "source_catalog_conflict", "构建源列表已变化，请刷新后重新提交")
	}
	group := options.Group
	if group == "" {
		group = catalog.DefaultGroup
	}
	var selectedGroup *BuildSourceGroup
	entries := map[string]BuildSourceEntry{}
	for _, item := range catalog.Groups {
		if item.ID == group {
			copy := item
			selectedGroup = &copy
		}
		for _, source := range item.Sources {
			entries[source.ID] = source
		}
	}
	if selectedGroup == nil {
		return nil, failure(400, "invalid_source_group", "请选择源列表中的分组")
	}
	content, err := recipeDockerfile(data, dockerfile)
	if err != nil {
		return nil, err
	}
	capabilities := sourceCapabilitiesFor(content, catalog.Types)
	supported := map[string]bool{}
	detected := map[string]bool{}
	for _, capability := range capabilities.Types {
		detected[capability.Type] = true
		for _, argument := range capability.Arguments {
			supported[argument] = true
		}
	}
	for kind, values := range options.Overrides {
		if err := validateBuildSourceValues(catalog.Types, kind, values); err != nil {
			return nil, err
		}
		definition, _ := sourceTypeFrom(catalog.Types, kind)
		for _, field := range definition.Fields {
			if values[field.ID] != "" && !supported[field.Argument] {
				return nil, failure(400, "source_parameter_unsupported", "配方没有使用该源参数，请先修改 Dockerfile；源设置未生效")
			}
		}
	}
	for kind, id := range options.Selections {
		source, ok := entries[id]
		if !ok || source.Type != kind || !detected[kind] {
			return nil, failure(400, "invalid_source_selection", "源条目不存在、类型不符或配方未使用此类源")
		}
	}
	snapshot := &BuildSourceSnapshot{Definitions: catalog.Types, Group: group, CatalogSHA: catalog.Revision, Selections: map[string]string{}, Values: map[string]map[string]string{}, Arguments: map[string]string{}}
	for _, capability := range capabilities.Types {
		kind := capability.Type
		if !capability.Supported {
			if options.Selections[kind] != "" {
				return nil, failure(400, "source_parameter_unsupported", "配方未提供可用的源参数，请先修改 Dockerfile")
			}
			snapshot.Unconfigured = append(snapshot.Unconfigured, kind)
			continue
		}
		var source BuildSourceEntry
		if id := options.Selections[kind]; id != "" {
			source = entries[id]
		} else {
			for _, candidate := range selectedGroup.Sources {
				if candidate.Type == kind {
					source = candidate
					break
				}
			}
		}
		values := map[string]string{}
		for field, value := range source.Values {
			values[field] = value
		}
		for field, value := range options.Overrides[kind] {
			values[field] = value
		}
		definition, _ := sourceTypeFrom(catalog.Types, kind)
		applied := map[string]string{}
		for _, field := range definition.Fields {
			value := values[field.ID]
			if value != "" && supported[field.Argument] {
				applied[field.ID] = value
				snapshot.Arguments[field.Argument] = value
			}
		}
		if source.RequiresConfiguration && len(applied) == 0 {
			return nil, failure(400, "source_configuration_required", "所选源需要填写适用于此配方的地址；也可选择保留原来源")
		}
		if len(applied) > 0 {
			snapshot.Values[kind] = applied
		}
		if source.ID != "" {
			snapshot.Selections[kind] = source.ID
		}
	}
	return snapshot, nil
}

func buildSourceArguments(snapshot *BuildSourceSnapshot) ([]string, error) {
	if snapshot == nil {
		return nil, nil
	}
	types, err := normalizedBuildSourceTypes(snapshot.Definitions)
	if err != nil {
		return nil, err
	}
	known := map[string]struct{ kind, field, format string }{}
	for _, kind := range types {
		for _, field := range kind.Fields {
			known[field.Argument] = struct{ kind, field, format string }{kind.ID, field.ID, field.Kind}
		}
	}
	names := []string{}
	for argument, value := range snapshot.Arguments {
		definition, ok := known[argument]
		if !ok || value == "" || snapshot.Values[definition.kind][definition.field] != value {
			return nil, failure(409, "build_source_snapshot_changed", "冻结的源参数快照无效")
		}
		if err := buildSourceValue(definition.format, value); err != nil {
			return nil, err
		}
		names = append(names, argument)
	}
	sort.Strings(names)
	args := []string{}
	for _, name := range names {
		args = append(args, "--build-arg", name+"="+snapshot.Arguments[name])
	}
	return args, nil
}

// Include the non-sensitive effective configuration in the frozen context so
// cache/reuse keys cannot confuse two builds that selected different sources.
func freezeBuildSources(data []byte, snapshot *BuildSourceSnapshot) ([]byte, error) {
	if snapshot == nil {
		return data, nil
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files, modes := map[string][]byte{}, map[string]os.FileMode{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || file.Name == "SHA256SUMS" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, buildContextLimit+1))
		reader.Close()
		if readErr != nil || len(content) > buildContextLimit {
			return nil, failure(413, "build_context_limit", "构建文件过大")
		}
		files[file.Name], modes[file.Name] = content, file.Mode()
	}
	encoded, _ := json.MarshalIndent(snapshot, "", "  ")
	files["build-sources.lock.json"], modes["build-sources.lock.json"] = append(encoded, '\n'), 0644
	if base := snapshot.Arguments["RD_BASE_IMAGE"]; base != "" {
		if content, ok := files["profile.lock.json"]; ok {
			var lock map[string]any
			if json.Unmarshal(content, &lock) == nil {
				lock["baseImage"] = base
				encoded, _ := json.MarshalIndent(lock, "", "  ")
				files["profile.lock.json"] = append(encoded, '\n')
			}
		}
	}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(files[name]), name)
	}
	files["SHA256SUMS"], modes["SHA256SUMS"] = []byte(sums.String()), 0644
	names = append(names, "SHA256SUMS")
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(modes[name])
		file, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = file.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (s *Server) buildSourceRoutes(mux *http.ServeMux) {
	m := s.Manager
	mux.HandleFunc("GET /api/build-center/sources/defaults", func(w http.ResponseWriter, r *http.Request) {
		catalog, err := m.BuildSourceDefaults()
		respondBuildSource(w, catalog, err)
	})
	mux.HandleFunc("GET /api/build-center/sources", func(w http.ResponseWriter, r *http.Request) {
		catalog, err := m.BuildSources()
		respondBuildSource(w, catalog, err)
	})
	mux.HandleFunc("PUT /api/build-center/sources", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var in BuildSourceCatalog
		if !decode(w, r, &in) {
			return
		}
		catalog, err := m.SaveBuildSources(in)
		respondBuildSource(w, catalog, err)
	})
	mux.HandleFunc("GET /api/build-center/recipes/{rid}/sources", func(w http.ResponseWriter, r *http.Request) {
		recipe, err := m.BuildCenterRecipe(r.PathValue("rid"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		var data []byte
		if recipe.Kind == "builtin" {
			data, err = builtinRecipeSourceContext(recipe.ProfileID)
		} else {
			data, err = verifiedFrozenContext(filepath.Join(m.recipeSnapshotDir(recipe.ID, recipe.Revision), "context.zip"), recipe.ContextSHA256)
		}
		if err != nil {
			respond(w, nil, err)
			return
		}
		content, err := recipeDockerfile(data, recipe.Dockerfile)
		if err != nil {
			respond(w, nil, err)
			return
		}
		catalog, err := m.BuildSources()
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, sourceCapabilitiesFor(content, catalog.Types), nil)
	})
}

func respondBuildSource(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		var typed *apiError
		if errors.As(err, &typed) {
			status = typed.Status
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
