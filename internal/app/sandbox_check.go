package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 65536 - b.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func sandboxSubcommand(help, platform string) ([]string, error) {
	name := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[platform]
	if name == "" {
		return nil, fmt.Errorf("尚未实现 %s 平台的沙箱诊断", platform)
	}
	if regexp.MustCompile(`(?m)^\s*` + name + `(?:\s|$)`).MatchString(help) {
		return []string{"sandbox", name}, nil
	}
	if strings.Contains(help, "[COMMAND]") || strings.Contains(help, "<COMMAND>") {
		// A Commands section without this platform must not be treated as flat CLI.
		if !strings.Contains(help, "Commands:") {
			return []string{"sandbox"}, nil
		}
	}
	return nil, fmt.Errorf("无法识别本机 Codex sandbox 命令格式，请查看 --help 输出；未尝试关闭沙箱")
}

func (m *Manager) diagnosticCommand(w Workspace, i Instance, args []string) (string, int, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.codexExecutable(), args...)
	cmd.Dir, cmd.Env = w.Path, m.command(w, i).Env
	cmd.WaitDelay = 2 * time.Second
	configureProbeProcess(cmd)
	var out limitedOutput
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		err = fmt.Errorf("诊断超时或取消: %w", ctx.Err())
	}
	return out.String(), code, err
}

func (m *Manager) sandboxCheck(w Workspace, i Instance) Check {
	start := time.Now()
	c := Check{Name: "沙箱实际执行", Status: "error"}
	if m.Demo {
		c.Status = "skipped"
		c.Detail = "演示模式；未检查真实系统沙箱。"
		return c
	}
	c.Command = []string{m.codexExecutable(), "sandbox", "--help"}
	help, code, err := m.diagnosticCommand(w, i, []string{"sandbox", "--help"})
	c.Output, c.ExitCode = help, &code
	if err != nil {
		c.Detail = "读取 sandbox --help 失败: " + err.Error()
		c.DurationMS = time.Since(start).Milliseconds()
		return c
	}
	args, err := sandboxSubcommand(help, runtime.GOOS)
	if err != nil {
		c.Detail = err.Error()
		c.DurationMS = time.Since(start).Milliseconds()
		return c
	}
	args = append([]string{"-c", `sandbox_mode="workspace-write"`, "-c", `sandbox_workspace_write.network_access=false`}, args...)
	args = append(args, "--")
	if runtime.GOOS == "windows" {
		args = append(args, "cmd.exe", "/d", "/c", "echo", "rundesk-sandbox-ok")
	} else {
		args = append(args, "/bin/echo", "rundesk-sandbox-ok")
	}
	c.Command = append([]string{m.codexExecutable()}, args...)
	out, code, err := m.diagnosticCommand(w, i, args)
	c.Output, c.ExitCode, c.DurationMS = out, &code, time.Since(start).Milliseconds()
	if err == nil && strings.Contains("\n"+strings.ReplaceAll(out, "\r\n", "\n")+"\n", "\nrundesk-sandbox-ok\n") {
		c.Status = "ok"
		c.Detail = "workspace-write、禁网沙箱已启动并执行最小命令；不调用模型。"
	} else {
		c.Detail = "沙箱测试未通过；未切换到沙箱外执行。"
		if err != nil {
			c.Detail += " " + err.Error()
		}
		if strings.Contains(out, "RTM_NEWADDR") || strings.Contains(out, "Operation not permitted") || strings.Contains(out, "user namespace") {
			c.Hint = "检查 bubblewrap 与系统用户命名空间限制。Ubuntu 24.04 可按官方说明安装并加载 bwrap AppArmor 配置：https://developers.openai.com/codex/concepts/sandboxing#prerequisites"
		}
	}
	return c
}
