package app

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var staticVideoProject = regexp.MustCompile(`(?m)^\s*VIDEO_APP_PROJECT_ID\s*=`)

// Copy only the Agent's current configuration and approved instance Skills.
// Threads, traces, caches and task files never cross project boundaries.
// New project homes are snapshots; existing homes are never overwritten.
func (m *Manager) seedApplicationProjectHome(source, target string) error {
	if source == target || !strings.HasPrefix(target, filepath.Join(m.Data, "environments")+string(os.PathSeparator)) {
		return failure(409, "application_project_home_invalid", "项目配置目录无效")
	}
	files := map[string]os.FileMode{}
	for _, name := range []string{"config.toml", "auth.json"} {
		from := filepath.Join(source, name)
		info, e := os.Lstat(from)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return failure(409, "application_project_config_invalid", "Agent 配置必须是普通文件且不超过 8 MiB")
		}
		if name == "config.toml" {
			data, e := os.ReadFile(from)
			if e != nil {
				return e
			}
			if staticVideoProject.Match(data) {
				return failure(409, "application_project_static_mcp", "请先从 Agent 的 video_app_media MCP 环境配置移除固定 VIDEO_APP_PROJECT_ID；项目 ID 由 RunDesk 注入")
			}
		}
		files[name] = info.Mode().Perm()
	}
	skills := filepath.Join(source, "skills")
	info, e := os.Lstat(skills)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if os.IsNotExist(e) {
		e = nil
	}
	if info != nil && !info.IsDir() {
		return failure(409, "application_project_skills_invalid", "Agent Skills 目录无效")
	}
	total := int64(0)
	if info != nil {
		e = filepath.WalkDir(skills, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if entry.IsDir() {
				return nil
			}
			if !info.Mode().IsRegular() || info.Size() > 8<<20 {
				return failure(409, "application_project_skills_invalid", "Agent Skill 不允许符号链接或超大文件")
			}
			total += info.Size()
			if total > 64<<20 {
				return failure(409, "application_project_skills_invalid", "Agent Skills 总大小超过 64 MiB")
			}
			return nil
		})
	}
	if e != nil {
		return e
	}
	if e = os.MkdirAll(target, 0700); e != nil {
		return e
	}
	for _, name := range []string{"config.toml", "auth.json"} {
		if mode, found := files[name]; found {
			if e = copyProjectConfigFile(filepath.Join(source, name), filepath.Join(target, name), mode); e != nil {
				return e
			}
		}
	}
	if info == nil {
		return nil
	}
	return filepath.WalkDir(skills, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(skills, path)
		if e != nil {
			return e
		}
		dest := filepath.Join(target, "skills", rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		return copyProjectConfigFile(path, dest, info.Mode().Perm())
	})
}

func copyProjectConfigFile(source, target string, mode os.FileMode) error {
	if _, e := os.Lstat(target); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	from, e := os.Open(source)
	if e != nil {
		return e
	}
	defer from.Close()
	to, e := os.OpenFile(target+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer os.Remove(target + ".tmp")
	n, copyErr := io.Copy(to, io.LimitReader(from, (8<<20)+1))
	if copyErr == nil && n > 8<<20 {
		copyErr = fmt.Errorf("配置文件超过 8 MiB")
	}
	if copyErr == nil {
		copyErr = to.Chmod(mode & 0700)
	}
	if closeErr := to.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return copyErr
	}
	return os.Rename(target+".tmp", target)
}
