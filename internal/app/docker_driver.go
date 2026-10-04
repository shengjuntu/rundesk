package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type dockerDriver interface {
	Run(context.Context, ...string) ([]byte, error)
	Command(...string) *exec.Cmd
}
type dockerCLI struct{}

func (dockerCLI) Command(args ...string) *exec.Cmd {
	endpoint := os.Getenv("RUNDESK_DOCKER_HOST")
	if endpoint == "" {
		endpoint = "unix:///var/run/docker.sock"
	}
	cmd := exec.Command("docker", append([]string{"--host", endpoint}, args...)...)
	for _, key := range []string{"PATH", "HOME", "DOCKER_CONFIG", "XDG_RUNTIME_DIR"} {
		if v, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+v)
		}
	}
	return cmd
}

type dockerOutput struct{ bytes.Buffer }

func (b *dockerOutput) Write(p []byte) (int, error) {
	n := len(p)
	if left := (256 << 10) - b.Len(); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
func (d dockerCLI) Run(ctx context.Context, args ...string) ([]byte, error) {
	if runtime.GOOS != "linux" {
		return nil, failure(400, "docker_platform_unsupported", "Docker 运行环境目前支持 Linux 本机 Engine；通用助手仍可在当前平台使用")
	}
	endpoint := os.Getenv("RUNDESK_DOCKER_HOST")
	if endpoint != "" && (!strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\n\r\x00")) {
		return nil, failure(400, "docker_endpoint_unsupported", "Docker 控制只支持本机 Unix socket")
	}
	cmd := d.Command(args...)
	out := &dockerOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	if e := cmd.Start(); e != nil {
		return nil, &apiError{Status: 503, Code: "docker_unavailable", Message: "无法启动 Docker CLI，请检查安装及服务用户权限", Cause: e}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var e error
	select {
	case e = <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		e = ctx.Err()
	}
	if e != nil {
		return out.Bytes(), &apiError{Status: 503, Code: "docker_command_failed", Message: fmt.Sprintf("Docker %s 失败：%s", args[0], strings.TrimSpace(out.String())), Cause: e}
	}
	return out.Bytes(), nil
}
func (m *Manager) dockerRun(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()
	return m.docker.Run(ctx, args...)
}
func (m *Manager) DockerStatus() (any, error) {
	out, e := m.dockerRun(10*time.Second, "version", "--format", "{{json .Server}}")
	if e != nil {
		return nil, e
	}
	var v any
	if json.Unmarshal(out, &v) != nil {
		return nil, failure(502, "docker_invalid_response", "Docker 版本响应无效")
	}
	return map[string]any{"server": v, "platform": runtime.GOOS + "/" + runtime.GOARCH, "transport": "local-unix-socket", "appServerTransport": "stdio"}, nil
}
