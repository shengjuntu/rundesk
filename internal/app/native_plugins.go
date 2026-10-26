package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"reflect"
	"sort"
	"strings"
)

const nativePluginUnsupported = "not_supported_by_stable_native_api"

// NativePluginDriver reserves the lifecycle boundary for a version-validated
// native adapter. No driver is registered: current plugin/* RPCs are documented
// as under development. Configuration and effective components are separate
// evidence and must never be presented as an installed-plugin inventory.
type NativePluginDriver interface {
	ListInstalled(instanceID, workspaceID string) (json.RawMessage, error)
	Install(instanceID, workspaceID string, params json.RawMessage) error
	Uninstall(instanceID, workspaceID string, params json.RawMessage) error
}

type NativePluginWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type NativePluginSupport struct {
	Inspection          bool   `json:"inspection"`
	Configuration       bool   `json:"configuration"`
	EffectiveComponents bool   `json:"effectiveComponents"`
	NativeInventory     bool   `json:"nativeInventory"`
	NativeMutation      bool   `json:"nativeMutation"`
	Code                string `json:"code"`
}
type NativePluginOrigin struct {
	Type              string `json:"type"`
	PluginID          string `json:"pluginId"`
	PluginDisplayName string `json:"pluginDisplayName,omitempty"`
}
type NativePluginSkill struct {
	Name    string             `json:"name"`
	Path    string             `json:"path"`
	Enabled *bool              `json:"enabled"`
	Origin  NativePluginOrigin `json:"origin"`
}
type NativePluginMCP struct {
	Name          string             `json:"name"`
	RuntimeStatus string             `json:"runtimeStatus,omitempty"`
	Origin        NativePluginOrigin `json:"origin"`
}
type NativePluginStatus struct {
	ID             string              `json:"id"`
	DisplayName    string              `json:"displayName,omitempty"`
	Configured     bool                `json:"configured"`
	Enabled        *bool               `json:"enabled"`
	Loaded         string              `json:"loaded"`
	InventoryState string              `json:"inventoryState"`
	Skills         []NativePluginSkill `json:"skills"`
	MCPServers     []NativePluginMCP   `json:"mcpServers"`
}
type NativePluginOverview struct {
	InstanceID  string                `json:"instanceId"`
	WorkspaceID string                `json:"workspaceId,omitempty"`
	Backend     string                `json:"backend"`
	Runtime     string                `json:"runtime"`
	ReadOnly    bool                  `json:"readOnly"`
	Support     NativePluginSupport   `json:"support"`
	Plugins     []NativePluginStatus  `json:"plugins"`
	Warnings    []NativePluginWarning `json:"warnings"`
}

func pluginText(value any) string {
	s, _ := value.(string)
	s = strings.TrimSpace(diagnosticText(s))
	if len(s) > 4096 {
		s = s[:4096]
	}
	return s
}

// This projection deliberately selects fields instead of forwarding raw config,
// endpoint URLs, tool payloads, environment variables, or authentication data.
func projectNativePlugins(c nativeConfig, skills, status json.RawMessage) ([]NativePluginStatus, []NativePluginWarning) {
	items := map[string]*NativePluginStatus{}
	warnings := []NativePluginWarning{}
	get := func(id string) *NativePluginStatus {
		if items[id] == nil {
			items[id] = &NativePluginStatus{ID: id, Loaded: "unknown", InventoryState: "unknown", Skills: []NativePluginSkill{}, MCPServers: []NativePluginMCP{}}
		}
		return items[id]
	}
	if raw, exists := c.Config["plugins"]; exists && raw != nil {
		configured, ok := raw.(map[string]any)
		if !ok {
			warnings = append(warnings, NativePluginWarning{"plugin_configuration_unknown", "Native plugin configuration has an unrecognized shape."})
		}
		for id, v := range configured {
			p := get(pluginText(id))
			p.Configured = true
			if cfg, ok := v.(map[string]any); ok {
				if enabled, ok := cfg["enabled"].(bool); ok {
					p.Enabled = &enabled
				}
				p.DisplayName = pluginText(cfg["displayName"])
			}
		}
	}
	var sk struct {
		Data []struct {
			Skills []struct {
				Name              string `json:"name"`
				Path              string `json:"path"`
				Enabled           *bool  `json:"enabled"`
				PluginID          string `json:"pluginId"`
				PluginDisplayName string `json:"pluginDisplayName"`
			} `json:"skills"`
		} `json:"data"`
	}
	if len(skills) > 0 && json.Unmarshal(skills, &sk) != nil {
		warnings = append(warnings, NativePluginWarning{"plugin_skills_unknown", "Native skill metadata could not be read."})
	}
	for _, data := range sk.Data {
		for _, s := range data.Skills {
			id := pluginText(s.PluginID)
			if id == "" {
				continue
			}
			p := get(id)
			if name := pluginText(s.PluginDisplayName); name != "" {
				p.DisplayName = name
			}
			p.Skills = append(p.Skills, NativePluginSkill{pluginText(s.Name), pluginText(s.Path), s.Enabled, NativePluginOrigin{"native_plugin", id, p.DisplayName}})
		}
	}
	var mc struct {
		Data []struct {
			Name          string `json:"name"`
			PluginID      string `json:"pluginId"`
			RuntimeStatus string `json:"runtimeStatus"`
		} `json:"data"`
	}
	if len(status) > 0 && json.Unmarshal(status, &mc) != nil {
		warnings = append(warnings, NativePluginWarning{"plugin_mcp_unknown", "Native MCP metadata could not be read."})
	}
	for _, s := range mc.Data {
		id := pluginText(s.PluginID)
		if id == "" {
			continue
		}
		p := get(id)
		runtime := pluginText(s.RuntimeStatus)
		if runtime == "connected" || runtime == "starting" {
			p.Loaded = "observed"
		}
		p.MCPServers = append(p.MCPServers, NativePluginMCP{pluginText(s.Name), runtime, NativePluginOrigin{"native_plugin", id, p.DisplayName}})
	}
	out := make([]NativePluginStatus, 0, len(items))
	for _, p := range items {
		sort.Slice(p.Skills, func(i, j int) bool { return p.Skills[i].Path < p.Skills[j].Path })
		sort.Slice(p.MCPServers, func(i, j int) bool { return p.MCPServers[i].Name < p.MCPServers[j].Name })
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, warnings
}

func nativePluginMCPWriteConflict(before, changes map[string]any, status json.RawMessage) error {
	plugins, _ := projectNativePlugins(nativeConfig{}, nil, status)
	for _, plugin := range plugins {
		for _, server := range plugin.MCPServers {
			value, exists := changes[server.Name]
			if exists && !reflect.DeepEqual(before[server.Name], value) {
				return failure(409, "native_plugin_component_read_only", "This MCP name belongs to a native plugin; configure it through the native plugin instead of importing a duplicate user server")
			}
		}
	}
	return nil
}

// Guard only names positively attributed by the effective native metadata. A
// legacy server lacking this optional read keeps its existing manual MCP flow.
// Removing an override and changing unrelated user entries remain possible.
func (m *Manager) checkNativePluginMCPWrite(wid string, before, changes map[string]any, ids ...string) error {
	i, err := m.Instance(ids...)
	if err != nil {
		return err
	}
	if runtimeKind(i.AgentRuntime.Kind) != "codex" {
		return nil
	}
	status, err := m.ConfigCall(wid, "mcpServerStatus/list", map[string]any{"limit": 100}, ids...)
	if err != nil {
		return nil
	}
	return nativePluginMCPWriteConflict(before, changes, status)
}

// NativePlugins uses only the existing stable configuration/skills/MCP reads.
// An omitted workspace returns capability metadata without starting a process.
func (m *Manager) NativePlugins(iid, wid string) (NativePluginOverview, error) {
	i, err := m.Instance(iid)
	if err != nil {
		return NativePluginOverview{}, err
	}
	backend := runtimeKind(i.AgentRuntime.Kind)
	out := NativePluginOverview{InstanceID: i.ID, WorkspaceID: wid, Backend: backend, Runtime: i.Execution.normalized().Mode, ReadOnly: true, Plugins: []NativePluginStatus{}, Warnings: []NativePluginWarning{}, Support: NativePluginSupport{Inspection: true, Code: nativePluginUnsupported}}
	if wid != "" {
		if _, err = m.Workspace(wid); err != nil {
			return out, err
		}
	}
	if backend != "codex" {
		out.Support.Code = "backend_unsupported"
		out.Warnings = append(out.Warnings, NativePluginWarning{"backend_unsupported", "This backend has no native Codex plugin support; packages are not unpacked into skills or MCP configuration."})
		return out, nil
	}
	out.Support.Configuration, out.Support.EffectiveComponents = true, true
	out.Warnings = append(out.Warnings, NativePluginWarning{nativePluginUnsupported, "Installed-plugin inventory and mutations are unavailable through the stable native API. Configured or observed components do not establish installation."})
	if wid == "" {
		return out, nil
	}
	// A config process is execution too. Do not allow this read to start a
	// disabled preset or combine a preset instance with another workspace.
	m.applicationMu.Lock()
	defer m.applicationMu.Unlock()
	rows, err := m.allApplicationPresets()
	if err != nil {
		return out, err
	}
	for _, p := range rows {
		if p.InstanceID != i.ID && p.WorkspaceID != wid {
			continue
		}
		if err = m.checkApplicationPresetExecution(SessionSource{Kind: "application", AppID: p.AppID, PresetID: p.ID}, i.ID, wid); err != nil {
			return out, err
		}
	}
	c, err := m.readConfig(wid, i.ID)
	if err != nil {
		return out, err
	}
	sk, skErr := m.Skills(wid, i.ID)
	mc, mcErr := m.ConfigCall(wid, "mcpServerStatus/list", map[string]any{"limit": 100}, i.ID)
	plugins, warnings := projectNativePlugins(c, sk, mc)
	out.Plugins, out.Warnings = plugins, append(out.Warnings, warnings...)
	if skErr != nil {
		out.Warnings = append(out.Warnings, NativePluginWarning{"plugin_skills_unknown", "Effective plugin skills are currently unavailable."})
	}
	if mcErr != nil {
		out.Warnings = append(out.Warnings, NativePluginWarning{"plugin_mcp_unknown", "Effective plugin MCP servers are currently unavailable."})
	}
	var page struct {
		NextCursor *string `json:"nextCursor"`
	}
	if json.Unmarshal(mc, &page) == nil && page.NextCursor != nil && *page.NextCursor != "" {
		out.Warnings = append(out.Warnings, NativePluginWarning{"plugin_mcp_partial", "MCP metadata is paginated; this overview does not prove a complete inventory."})
	}
	return out, nil
}

const pluginUploadLimit = 16 << 20
const pluginExpandedLimit = 64 << 20
const pluginManifestLimit = 1 << 20
const pluginEntryLimit = 2048

type PluginPackageManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}
type PluginPackageComponent struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Declared bool   `json:"declared"`
}
type PluginPackageDependency struct {
	Name        string `json:"name"`
	Requirement string `json:"requirement,omitempty"`
}
type PluginPackageCompatibility struct {
	Codex                 string `json:"codex"`
	Kun                   string `json:"kun"`
	StableNativeInstall   bool   `json:"stableNativeInstall"`
	OpenAIExtensionSource string `json:"openaiExtensionSource"`
}
type PluginPackageInspection struct {
	ReadOnly         bool                       `json:"readOnly"`
	InstallSupported bool                       `json:"installSupported"`
	Format           string                     `json:"format"`
	ManifestPath     string                     `json:"manifestPath"`
	Manifest         PluginPackageManifest      `json:"manifest"`
	Components       []PluginPackageComponent   `json:"components"`
	Dependencies     []PluginPackageDependency  `json:"dependencies"`
	Compatibility    PluginPackageCompatibility `json:"compatibility"`
	UnknownFields    []string                   `json:"unknownFields"`
	Warnings         []NativePluginWarning      `json:"warnings"`
}

func pluginArchivePath(name string) bool {
	trimmed := strings.TrimSuffix(name, "/")
	return trimmed != "" && len(trimmed) <= 1024 && path.Clean(trimmed) == trimmed && trimmed != "." && trimmed != ".." && !strings.HasPrefix(trimmed, "../") && !strings.HasPrefix(trimmed, "/") && !strings.ContainsAny(trimmed, "\\:\x00\r\n")
}

func readPluginJSON(f *zip.File) (map[string]any, error) {
	if f.UncompressedSize64 > pluginManifestLimit {
		return nil, errors.New("Plugin JSON exceeds 1 MiB")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, pluginManifestLimit+1))
	if err != nil || len(b) > pluginManifestLimit {
		return nil, errors.New("Cannot read bounded plugin JSON")
	}
	var result map[string]any
	if json.Unmarshal(b, &result) != nil || result == nil {
		return nil, errors.New("Plugin JSON must be an object")
	}
	return result, nil
}

// InspectPluginPackage examines declared native package resources in memory.
// It never extracts files, executes hooks, fetches dependencies, or writes config.
func InspectPluginPackage(reader io.Reader) (PluginPackageInspection, error) {
	out := PluginPackageInspection{ReadOnly: true, Components: []PluginPackageComponent{}, Dependencies: []PluginPackageDependency{}, UnknownFields: []string{}, Warnings: []NativePluginWarning{}, Compatibility: PluginPackageCompatibility{Codex: "unknown", Kun: "unsupported", OpenAIExtensionSource: "none"}}
	invalid := func(message string) (PluginPackageInspection, error) {
		return out, failure(400, "invalid_plugin_package", message)
	}
	b, err := io.ReadAll(io.LimitReader(reader, pluginUploadLimit+1))
	if err != nil || len(b) > pluginUploadLimit {
		return invalid("Plugin ZIP must be at most 16 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(zr.File) > pluginEntryLimit {
		return invalid("Invalid ZIP or more than 2048 entries")
	}
	files := map[string]*zip.File{}
	seen := map[string]bool{}
	var total uint64
	for _, f := range zr.File {
		if !pluginArchivePath(f.Name) || (!f.Mode().IsRegular() && !f.FileInfo().IsDir()) {
			return invalid("Unsafe ZIP path or non-regular entry")
		}
		key := strings.ToLower(strings.TrimSuffix(f.Name, "/"))
		if seen[key] {
			return invalid("Duplicate ZIP path")
		}
		seen[key] = true
		if f.UncompressedSize64 > pluginExpandedLimit || total > pluginExpandedLimit-f.UncompressedSize64 {
			return invalid("Expanded ZIP exceeds 64 MiB")
		}
		total += f.UncompressedSize64
		if !f.FileInfo().IsDir() {
			files[f.Name] = f
		}
	}
	// Permit either the package root or a single enclosing directory, as in a
	// repository-source ZIP. Multiple plugin roots are ambiguous and rejected.
	roots := map[string]bool{}
	for name := range files {
		suffixes := []string{"plugin.json"}
		if name == ".codex-plugin/plugin.json" || strings.HasSuffix(name, "/.codex-plugin/plugin.json") {
			suffixes = []string{".codex-plugin/plugin.json"}
		}
		for _, suffix := range suffixes {
			if name == suffix {
				roots[""] = true
			} else if strings.HasSuffix(name, "/"+suffix) {
				prefix := strings.TrimSuffix(name, suffix)
				if strings.Count(strings.TrimSuffix(prefix, "/"), "/") == 0 {
					roots[prefix] = true
				}
			}
		}
	}
	if len(roots) != 1 {
		return invalid("ZIP must contain exactly one root plugin.json or .codex-plugin/plugin.json")
	}
	root := ""
	for prefix := range roots {
		root = prefix
	}
	manifestPath := root + "plugin.json"
	out.Format = "agent-plugin"
	if files[manifestPath] == nil {
		manifestPath, out.Format = root+".codex-plugin/plugin.json", "codex-compat"
		out.Compatibility.Codex = "declared"
	}
	manifest, err := readPluginJSON(files[manifestPath])
	if err != nil {
		return invalid(err.Error())
	}
	out.ManifestPath = pluginText(manifestPath)
	out.Manifest = PluginPackageManifest{pluginText(manifest["name"]), pluginText(manifest["version"]), pluginText(manifest["description"])}
	if out.Manifest.Name == "" {
		return invalid("Plugin manifest needs a name")
	}
	known := map[string]bool{"$schema": true, "name": true, "version": true, "description": true, "author": true, "homepage": true, "repository": true, "license": true, "keywords": true, "skills": true, "mcpServers": true, "hooks": true, "commands": true, "agents": true, "dependencies": true, "extensions": true}
	for key := range manifest {
		if !known[key] {
			out.UnknownFields = append(out.UnknownFields, pluginText(key))
		}
	}
	if ext, ok := manifest["extensions"].(map[string]any); ok {
		if _, present := ext["com.openai"]; present {
			out.Compatibility.Codex, out.Compatibility.OpenAIExtensionSource = "declared", "plugin.json"
		}
	}
	if out.Compatibility.OpenAIExtensionSource == "none" && files[root+".codex-plugin/plugin.json"] != nil {
		if _, err = readPluginJSON(files[root+".codex-plugin/plugin.json"]); err != nil {
			return invalid(err.Error())
		}
		out.Compatibility.Codex, out.Compatibility.OpenAIExtensionSource = "declared", ".codex-plugin/plugin.json"
	}
	for name := range files {
		if !strings.HasPrefix(name, root) {
			continue
		}
		relative := strings.TrimPrefix(name, root)
		if strings.HasPrefix(relative, "skills/") && strings.HasSuffix(relative, "/SKILL.md") {
			out.Components = append(out.Components, PluginPackageComponent{"skill", pluginText(path.Base(path.Dir(relative))), pluginText(name), true})
		}
	}
	mcpPath := root + "mcp.json"
	if files[mcpPath] == nil {
		mcpPath = root + ".mcp.json"
	}
	if files[mcpPath] != nil {
		mcp, e := readPluginJSON(files[mcpPath])
		if e != nil {
			return invalid(e.Error())
		}
		servers, ok := mcp["mcpServers"].(map[string]any)
		if !ok {
			return invalid("MCP package JSON needs a mcpServers object")
		}
		for name := range servers {
			out.Components = append(out.Components, PluginPackageComponent{"mcp", pluginText(name), pluginText(mcpPath), true})
		}
	}
	for key, kind := range map[string]string{"hooks": "hook", "commands": "command", "agents": "agent"} {
		if _, exists := manifest[key]; exists {
			out.Components = append(out.Components, PluginPackageComponent{kind, key, pluginText(manifestPath), true})
			out.Warnings = append(out.Warnings, NativePluginWarning{"declared_component_only", "Declared " + key + " were not executed or validated for this host."})
		}
	}
	if deps, ok := manifest["dependencies"].(map[string]any); ok {
		for name, requirement := range deps {
			out.Dependencies = append(out.Dependencies, PluginPackageDependency{pluginText(name), pluginText(requirement)})
		}
	} else if _, exists := manifest["dependencies"]; exists {
		out.Warnings = append(out.Warnings, NativePluginWarning{"dependency_shape_unknown", "Dependency declarations use an unrecognized shape; no dependencies were resolved."})
	}
	sort.Strings(out.UnknownFields)
	sort.Slice(out.Components, func(i, j int) bool {
		if out.Components[i].Kind == out.Components[j].Kind {
			return out.Components[i].Name < out.Components[j].Name
		}
		return out.Components[i].Kind < out.Components[j].Kind
	})
	sort.Slice(out.Dependencies, func(i, j int) bool { return out.Dependencies[i].Name < out.Dependencies[j].Name })
	out.Warnings = append(out.Warnings, NativePluginWarning{"inspection_only", "Package declarations were inspected without installation. Compatibility is unverified; credentials and raw connection settings are not returned."})
	return out, nil
}

func (s *Server) nativePluginRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/instances/{iid}/plugins", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Manager.NativePlugins(r.PathValue("iid"), r.URL.Query().Get("workspaceId"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/plugins/inspect", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, pluginUploadLimit+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
			respond(w, nil, failure(400, "invalid_plugin_upload", "Upload a plugin ZIP of at most 16 MiB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("file")
		if err != nil {
			respond(w, nil, failure(400, "plugin_file_required", "Upload a plugin ZIP using multipart field file"))
			return
		}
		defer f.Close()
		v, e := InspectPluginPackage(f)
		respond(w, v, e)
	})
	for _, operation := range []string{"install", "uninstall"} {
		mux.HandleFunc("POST /api/instances/{iid}/plugins/"+operation, func(w http.ResponseWriter, r *http.Request) {
			i, e := s.Manager.Instance(r.PathValue("iid"))
			if e != nil {
				respond(w, nil, e)
				return
			}
			code := nativePluginUnsupported
			if runtimeKind(i.AgentRuntime.Kind) != "codex" {
				code = "backend_unsupported"
			}
			writeErr(w, http.StatusNotImplemented, failure(http.StatusNotImplemented, code, "Native plugin lifecycle is unavailable through the stable backend API; no changes were made"))
		})
	}
}
