package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
)

func buildAssistantRuntimeFingerprint(i Instance) string {
	return buildAssistantDigest(struct {
		Config      p.Config
		Permissions Permissions
		Execution   ExecutionSpec
	}{i.AgentRuntime.Normalized(), i.Permissions.normalized(), i.Execution.normalized()})
}

// The same task-bound tools serve either harness. Kun receives their headers
// through its transport configuration, never in model instructions or a file.
func (m *Manager) configureKunBuildAssistant(s Session, w Workspace, request p.Start) (p.Start, error) {
	var link struct {
		ID string `json:"id"`
	}
	if err := m.Store.Get("build-assistant-session", s.ID, &link); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return request, nil
		}
		return request, err
	}
	if err := m.checkBuildAssistantExecution(s); err != nil {
		return request, err
	}
	var record buildAssistantRecord
	if err := m.Store.Get("build-assistant", link.ID, &record); err != nil {
		return request, err
	}
	if record.WorkspaceID != w.ID || record.WorkspacePath != w.Path {
		return request, failure(409, "assistant_workspace_changed", "构建助手工作区已变化")
	}
	secret, err := os.ReadFile(m.buildAssistantSecretPath(record.ID))
	if err != nil {
		return request, err
	}
	if keyHash(string(secret)) != record.CapabilityHash {
		return request, fmt.Errorf("build capability changed")
	}
	content, err := os.ReadFile(filepath.Join(w.Path, ".agents", "skills", "add-app-docker-image", "SKILL.md"))
	if err != nil {
		return request, err
	}
	request.Skills = []p.Skill{{Name: "add-app-docker-image", Path: ".agents/skills/add-app-docker-image/SKILL.md", Content: string(content), Hash: fmt.Sprintf("%x", sha256.Sum256(content))}}
	tools := []string{}
	approvals := map[string]string{}
	for _, raw := range buildAssistantToolSchemas() {
		name := raw.(map[string]any)["name"].(string)
		tools = append(tools, name)
		approvals[name] = "approve"
	}
	request.MCP = []p.MCPServer{{Name: "rundesk_build_center", Revision: record.ID, URL: record.Endpoint, Headers: map[string]string{"Authorization": "Bearer " + string(secret)}, StartupTimeout: 15, ToolTimeout: 60, Allowlist: true, EnabledTools: tools, ApprovalModes: approvals}}
	request.Config.AllowBash = false
	if !request.Config.AllowWrite {
		return request, failure(409, "assistant_write_permission_changed", "构建助手的工作区写入权限已被禁用")
	}
	request.Config.PauseBeforeModel = false
	if s.InstanceID == setupKunInstanceID {
		request.Config.MaxSteps = 100
		request.Config.Budget.MaxToolCalls = 240
		request.Config.Budget.MaxActiveSeconds = 7200
	}
	return request, nil
}

func (m *Manager) assistantWaitBuild(cap buildAssistantCapability, id string, seconds int) (ImageBuild, error) {
	if seconds == 0 {
		seconds = 30
	}
	if seconds < 1 || seconds > 45 {
		return ImageBuild{}, failure(400, "invalid_wait", "等待时间必须为1到45秒")
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := m.checkBuildAssistantCapability(cap); err != nil {
			return ImageBuild{}, err
		}
		build, err := m.assistantBuild(cap, id)
		if err != nil || !buildActive(build.Status) {
			return build, err
		}
		select {
		case <-m.ctx.Done():
			return build, m.ctx.Err()
		case <-timer.C:
			return build, nil
		case <-tick.C:
		}
	}
}

func (m *Manager) bootstrapBuildRepair(w Workspace, id string) error {
	b, err := m.BuildCenterBuild(id)
	if err != nil {
		return err
	}
	data, err := verifiedFrozenContext(filepath.Join(m.buildDir(id), "context.zip"), b.ContextSHA256)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(w.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.MkdirAll("artifacts", 0700); err != nil {
		return err
	}
	// A replay may reuse this dedicated workspace. Freeze the original bytes only;
	// later agent changes are allowed in context/ and remain separate from history.
	if _, err = root.Stat("artifacts/source-build.json"); err == nil {
		return nil
	}
	if err = unpackAssistantContext(root, "artifacts/context", data); err != nil {
		return err
	}
	smoke := RecipeSmokeSpec{}
	if b.RecipeSmoke != nil {
		smoke = *b.RecipeSmoke
	}
	manifest := BuildRecipeManifest{SchemaVersion: 1, Name: "Repair " + b.ID, Version: "0.1.0", Description: "Repair of frozen build " + b.ID, Dockerfile: b.Dockerfile, Plugins: []RecipePluginRequirement{}, Smoke: smoke}
	for name, value := range map[string]any{"artifacts/recipe.json": manifest, "artifacts/source-build.json": b} {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(append(encoded, '\n'))
		ce := f.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}

func unpackAssistantContext(root *os.Root, directory string, data []byte) error {
	if !safePath(directory) || !strings.HasPrefix(directory, "artifacts/") {
		return failure(400, "invalid_artifact_path", "上下文必须位于任务 artifacts/ 内")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(archive.File) > 4000 {
		return failure(413, "build_context_limit", "构建上下文文件过多")
	}
	total := int64(0)
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !safePath(name) || strings.Contains(name, "\\") || entry.Mode()&os.ModeSymlink != 0 {
			return failure(400, "invalid_build_context", "上下文包含无效路径或符号链接")
		}
		dest := filepath.Join(directory, filepath.FromSlash(name))
		if entry.FileInfo().IsDir() {
			if err = root.MkdirAll(dest, 0700); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return failure(400, "invalid_build_context", "上下文只接受普通文件")
		}
		if entry.UncompressedSize64 > uint64(buildContextLimit) {
			return failure(413, "build_context_limit", "构建上下文过大")
		}
		if err = root.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		content, err := io.ReadAll(io.LimitReader(r, buildContextLimit-total+1))
		r.Close()
		if err != nil {
			return err
		}
		total += int64(len(content))
		if total > buildContextLimit {
			return failure(413, "build_context_limit", "构建上下文过大")
		}
		mode := os.FileMode(0600)
		if entry.Mode()&0111 != 0 {
			mode = 0700
		}
		f, err := root.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = f.Write(content)
		ce := f.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}

func (m *Manager) assistantPackContext(cap buildAssistantCapability, directory, target string) (any, error) {
	if !safePath(directory) || !strings.HasPrefix(directory, "artifacts/") || !safePath(target) || !strings.HasPrefix(target, "artifacts/") || strings.HasPrefix(target, directory+"/") {
		return nil, failure(400, "invalid_artifact_path", "打包目录和ZIP必须分别位于任务 artifacts/ 下")
	}
	root, err := os.OpenRoot(cap.WorkspacePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	names := []string{}
	size := int64(0)
	err = fs.WalkDir(root.FS(), directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return failure(400, "invalid_build_context", "打包不接受符号链接或特殊文件")
		}
		if path == filepath.ToSlash(filepath.Join(directory, "SHA256SUMS")) {
			return nil
		}
		size += info.Size()
		names = append(names, path)
		if len(names) > 4000 || size > buildContextLimit {
			return failure(413, "build_context_limit", "打包文件数或大小超限")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	total := int64(0)
	var sums strings.Builder
	for _, name := range names {
		f, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return nil, fmt.Errorf("context changed")
		}
		header := &zip.FileHeader{Name: strings.TrimPrefix(filepath.ToSlash(name), filepath.ToSlash(directory)+"/"), Method: zip.Deflate}
		header.SetMode(info.Mode().Perm())
		entry, err := writer.CreateHeader(header)
		if err != nil {
			f.Close()
			return nil, err
		}
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(entry, hash), io.LimitReader(f, buildContextLimit-total+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		total += n
		if total > buildContextLimit || output.Len() > buildUploadLimit {
			return nil, failure(413, "build_context_limit", "构建上下文超过大小限制")
		}
		fmt.Fprintf(&sums, "%x  %s\n", hash.Sum(nil), header.Name)
	}
	entry, err := writer.Create("SHA256SUMS")
	if err != nil {
		return nil, err
	}
	if _, err = io.WriteString(entry, sums.String()); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if output.Len() > buildUploadLimit {
		return nil, failure(413, "build_upload_limit", "ZIP超过32MiB")
	}
	if err = root.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return nil, err
	}
	file, err := root.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, err = file.Write(output.Bytes())
	ce := file.Close()
	if err != nil {
		return nil, err
	}
	if ce != nil {
		return nil, ce
	}
	return map[string]any{"contextPath": target, "files": len(names), "bytes": output.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(output.Bytes()))}, nil
}
