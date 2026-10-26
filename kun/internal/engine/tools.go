package kun

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

func toolDefinitions(write bool, bash ...bool) []any {
	tool := func(name, desc string, props map[string]any, required []string) any {
		return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": desc, "parameters": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}}}
	}
	str := map[string]any{"type": "string"}
	out := []any{
		tool("read_file", "Read a UTF-8 text file inside the workspace; maximum 128 KiB.", map[string]any{"path": str}, []string{"path"}),
		tool("list_files", "List one workspace directory (at most 500 entries).", map[string]any{"path": str}, []string{"path"}),
	}
	if write {
		out = append(out, tool("write_file", "Write UTF-8 content to a workspace-relative file. Existing content is replaced. Maximum 128 KiB.", map[string]any{"path": str, "content": str}, []string{"path", "content"}))
	}
	out = append(out,
		tool("grep", "Search UTF-8 project files using a Go regular expression. Relative paths only; skips symlinks/binary/large files; returns bounded matches and truncation/skipped counts.", map[string]any{"pattern": str, "path": str, "glob": str, "ignoreCase": map[string]any{"type": "boolean"}, "literal": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500}}, []string{"pattern"}),
		tool("find", "Find project files by glob (for example *.go). Matches basenames or relative paths; ** is not supported. Symlinks are skipped; default path is dot.", map[string]any{"pattern": str, "path": str, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500}}, []string{"pattern"}))
	if write {
		out = append(out, tool("edit_file", "Apply exact, unique, non-overlapping replacements against the original UTF-8 file. Validate every edit before an atomic replacement; maximum 128 KiB.", map[string]any{"path": str, "edits": map[string]any{"type": "array", "minItems": 1, "maxItems": 32, "items": map[string]any{"type": "object", "properties": map[string]any{"oldText": str, "newText": str}, "required": []string{"oldText", "newText"}, "additionalProperties": false}}}, []string{"path", "edits"}))
	}
	if len(bash) > 0 && bash[0] {
		out = append(out, tool("bash", "Execute a non-interactive Bash command on the host, starting in the project directory. Pipes, redirects, scripts, git and build commands are supported. cwd/env do not persist. Host permissions apply, not file-tool restrictions. Returns stdout/stderr tails, exitCode and timeout/cancel flags. Each stream is limited to 64 KiB; no detached jobs.", map[string]any{"command": map[string]any{"type": "string", "minLength": 1, "maxLength": 32768}, "timeout": map[string]any{"type": "integer", "minimum": 1, "maximum": 600}}, []string{"command"}))
	}
	return out
}
func executeTool(workspace string, allowWrite bool, call p.ToolCall, contexts ...context.Context) (string, error) {
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if call.Function.Name == "grep" || call.Function.Name == "find" {
		return executeSearch(ctx, workspace, call)
	}
	if call.Function.Name == "edit_file" {
		if !allowWrite {
			return "", fmt.Errorf("edit_file is disabled")
		}
		return executeEdit(workspace, call)
	}
	var args map[string]json.RawMessage
	if e := json.Unmarshal([]byte(call.Function.Arguments), &args); e != nil || args == nil {
		return "", fmt.Errorf("arguments must be an object")
	}
	var path, content string
	if e := json.Unmarshal(args["path"], &path); e != nil {
		return "", fmt.Errorf("path must be a string")
	}
	for key := range args {
		if key != "path" && (key != "content" || call.Function.Name != "write_file") {
			return "", fmt.Errorf("unknown argument %s", key)
		}
	}
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") || strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path must be workspace-relative")
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	root, e := os.OpenRoot(workspace)
	if e != nil {
		return "", e
	}
	defer root.Close()
	switch call.Function.Name {
	case "list_files":
		f, e := root.OpenFile(clean, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e != nil {
			return "", e
		}
		defer f.Close()
		entries, e := f.ReadDir(501)
		if e != nil && e != io.EOF {
			return "", e
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		lines := []string{}
		for n, v := range entries {
			if n == 500 {
				lines = append(lines, "[truncated]")
				break
			}
			name := v.Name()
			if v.IsDir() {
				name += "/"
			}
			lines = append(lines, name)
		}
		return strings.Join(lines, "\n"), nil
	case "read_file":
		f, e := root.OpenFile(clean, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e != nil {
			return "", e
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return "", e
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("only regular files can be read")
		}
		b, e := io.ReadAll(io.LimitReader(f, 128*1024+1))
		if e != nil {
			return "", e
		}
		if len(b) > 128*1024 {
			return "", fmt.Errorf("file exceeds 128 KiB; no truncated content returned")
		}
		if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
			return "", fmt.Errorf("file is not UTF-8 text")
		}
		return string(b), nil
	case "write_file":
		if !allowWrite {
			return "", fmt.Errorf("write_file is disabled")
		}
		if e = json.Unmarshal(args["content"], &content); e != nil || len(content) > 128*1024 {
			return "", fmt.Errorf("content must be a string at most 128 KiB")
		}
		if e = root.MkdirAll(filepath.Dir(clean), 0700); e != nil {
			return "", e
		}
		// OpenRoot constrains symlink resolution to the workspace. Refuse non-regular targets.
		if info, e := root.Stat(clean); e == nil && !info.Mode().IsRegular() {
			return "", fmt.Errorf("target is not a regular file")
		} else if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		f, e := root.OpenFile(clean, os.O_WRONLY|os.O_CREATE|syscall.O_NONBLOCK, 0600)
		if e != nil {
			return "", e
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			f.Close()
			return "", fmt.Errorf("target must remain a regular file")
		}
		e = f.Truncate(0)
		if e == nil {
			_, e = f.WriteString(content)
		}
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
		return fmt.Sprintf("Wrote %d bytes to %s", len(content), clean), nil
	default:
		return "", fmt.Errorf("unknown tool %s", call.Function.Name)
	}
}
