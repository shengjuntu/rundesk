package app

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shengjuntu/rundesk/internal/skillsafety"
)

const skillCheckReportLimit = 4 << 20

var skillCheckImageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var skillCheckContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// SkillCheckExecution is execution evidence, not a quality or security verdict.
// A non-zero CLI exit remains separate from any report the tool managed to save.
type SkillCheckExecution struct {
	Output       string
	ExitCode     int
	Report       []byte
	CleanupError string
}

type skillCheckCapture struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	exceeded bool
}

func (b *skillCheckCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := b.limit - len(b.data)
	if n > left {
		b.exceeded = true
	}
	if left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *skillCheckCapture) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.exceeded
}

func skillCheckOutput(text string) string {
	text = skillsafety.Redact(text)
	if len(text) > 64<<10 {
		text = text[:64<<10] + "\n[output truncated]"
	}
	return text
}

// runSkillCheckCopy captures the docker cp tar stream without extracting it on
// the host. Docker's environment remains restricted by the existing driver.
func (m *Manager) runSkillCheckCopy(ctx context.Context, container string) ([]byte, error) {
	cmd := m.docker.Command("cp", container+":/output/iteration-1/result.json", "-")
	cmd.WaitDelay = 2 * time.Second
	stdout := &skillCheckCapture{limit: 8 << 20}
	stderr := &skillCheckCapture{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start report copy: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return nil, ctx.Err()
	}
	data, tooLarge := stdout.snapshot()
	logs, _ := stderr.snapshot()
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve Skill-Up report: %s", skillCheckOutput(string(logs)))
	}
	if tooLarge {
		return nil, errors.New("Skill-Up report transfer exceeded its limit")
	}
	return readSkillCheckTar(data)
}

func readSkillCheckTar(data []byte) ([]byte, error) {
	if len(data) > 8<<20 {
		return nil, errors.New("Skill-Up report transfer exceeded its limit")
	}
	stream := bytes.NewReader(data)
	reader := tar.NewReader(stream)
	header, err := reader.Next()
	if err != nil {
		return nil, errors.New("Skill-Up report archive is empty or invalid")
	}
	// Only one ordinary result.json is expected. Never materialize links, paths,
	// directory contents or mode bits supplied by the untrusted container.
	if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Name != "result.json" || header.Linkname != "" || header.Size < 1 || header.Size > skillCheckReportLimit {
		return nil, errors.New("Skill-Up report archive must contain one bounded regular result.json")
	}
	result, err := io.ReadAll(io.LimitReader(reader, skillCheckReportLimit+1))
	if err != nil || int64(len(result)) != header.Size {
		return nil, errors.New("Skill-Up report archive is truncated or too large")
	}
	if _, err = reader.Next(); err != io.EOF {
		return nil, errors.New("Skill-Up report archive contains unexpected additional entries")
	}
	for _, value := range data[len(data)-stream.Len():] {
		if value != 0 {
			return nil, errors.New("Skill-Up report archive contains unexpected trailing data")
		}
	}
	return result, nil
}

func skillCheckContainerArgs(job SkillCheckJob, sourceDir string) ([]string, error) {
	if !skillCenterID.MatchString(job.ID) || job.ContainerName != "rundesk-skillcheck-"+job.ID || !skillCheckImageID.MatchString(job.ImageID) {
		return nil, failure(400, "invalid_skill_check_identity", "测试容器或冻结镜像身份无效")
	}
	// version is an internal trusted diagnostic action. The public admission
	// path continues to accept only validate/run.
	if job.Action != "validate" && job.Action != "run" && job.Action != "version" {
		return nil, failure(400, "invalid_skill_check_action", "未知测试操作")
	}
	if !filepath.IsAbs(sourceDir) || strings.ContainsAny(sourceDir, ",:\r\n\x00") {
		return nil, failure(400, "invalid_skill_check_source", "测试来源必须是普通绝对目录")
	}
	info, err := os.Lstat(sourceDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, failure(400, "invalid_skill_check_source", "测试来源必须是普通目录")
	}
	real, err := filepath.EvalSymlinks(sourceDir)
	if err != nil || real != sourceDir {
		return nil, failure(400, "invalid_skill_check_source", "测试来源目录不能包含符号链接")
	}
	entrypoint := os.Getenv("RUNDESK_SKILLUP_CONTAINER_BIN")
	if entrypoint == "" {
		entrypoint = "/usr/local/bin/skill-up"
	}
	if !strings.HasPrefix(entrypoint, "/") || strings.ContainsAny(entrypoint, "\r\n\x00") || filepath.ToSlash(filepath.Clean(entrypoint)) != entrypoint {
		return nil, failure(400, "invalid_skill_check_executable", "Skill-Up 容器命令必须为可信绝对路径")
	}
	network := "none"
	if job.AllowNetwork {
		network = "bridge"
	}
	args := []string{"create", "--name", job.ContainerName, "--label", "io.rundesk.skillcheck=" + job.ID,
		"--pull", "never", "--read-only", "--no-healthcheck", "--log-driver", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--user", "65532:65532", "--pids-limit", "256", "--cpus", "2", "--memory", "2048m", "--memory-swap", "2048m",
		"--network", network, "--workdir", "/work", "--env", "HOME=/work", "--env", "XDG_CONFIG_HOME=/work/.config",
		"--tmpfs", "/work:rw,nosuid,nodev,size=256m,uid=65532,gid=65532,mode=0700",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=128m,uid=65532,gid=65532,mode=0700",
		"--tmpfs", "/output:rw,nosuid,nodev,size=64m,uid=65532,gid=65532,mode=0700",
		"--mount", "type=bind,src=" + sourceDir + ",dst=/source,readonly",
		"--entrypoint", entrypoint, job.ImageID, job.Action, "/source/evals/eval.yaml"}
	if job.Action == "run" {
		args = append(args, "--output-dir", "/output", "--iteration", "1", "--parallelism", "1")
	} else if job.Action == "version" {
		args = append(args[:len(args)-2], "--version")
	}
	return args, nil
}

func (m *Manager) cleanupSkillCheck(ctx context.Context, job SkillCheckJob) error {
	if !skillCenterID.MatchString(job.ID) || job.ContainerName != "rundesk-skillcheck-"+job.ID {
		return errors.New("测试容器清理身份无效")
	}
	data, err := m.docker.Run(ctx, "inspect", "--type", "container", "--format", "{{json .}}", job.ContainerName)
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "no such container") || strings.Contains(message, "no such object") {
			return nil
		}
		return fmt.Errorf("无法确认测试容器是否已清理：%s", skillCheckOutput(err.Error()))
	}
	var owned struct {
		ID     string `json:"Id"`
		Config struct{ Labels map[string]string }
	}
	if json.Unmarshal(data, &owned) != nil || !skillCheckContainerID.MatchString(owned.ID) {
		return errors.New("测试容器清理身份无法确认")
	}
	if owned.Config.Labels["io.rundesk.skillcheck"] != job.ID {
		return errors.New("同名容器不属于此测试，未执行删除")
	}
	if _, err := m.docker.Run(ctx, "rm", "-f", owned.ID); err != nil {
		return fmt.Errorf("测试容器清理未确认：%s", skillCheckOutput(err.Error()))
	}
	return nil
}

func (m *Manager) executeSkillCheck(ctx context.Context, job SkillCheckJob, sourceDir string) (result SkillCheckExecution, executionErr error) {
	return m.executeSkillCheckWithCleanup(ctx, job, sourceDir, 15*time.Second)
}

// Trusted diagnostics share the same container boundary with a shorter cleanup
// allowance; ordinary evaluation keeps its existing fifteen-second allowance.
func (m *Manager) executeSkillCheckWithCleanup(ctx context.Context, job SkillCheckJob, sourceDir string, cleanupBudget time.Duration) (result SkillCheckExecution, executionErr error) {
	result.ExitCode = -1
	if runtime.GOOS != "linux" {
		return result, failure(503, "skill_check_unavailable", "Skill-Up 执行需要 Linux 本机 Docker Engine")
	}
	endpoint := os.Getenv("RUNDESK_DOCKER_HOST")
	if endpoint != "" && (!strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\r\n\x00")) {
		return result, failure(400, "docker_endpoint_unsupported", "测试只支持本机 Docker Unix socket")
	}
	args, err := skillCheckContainerArgs(job, sourceDir)
	if err != nil {
		return result, err
	}
	container := ""
	// A failed create can still leave a daemon-side object after a transport
	// interruption. Recover only objects carrying this job's exact owner label.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupBudget)
		defer cancel()
		if err := m.cleanupSkillCheck(cleanupCtx, job); err != nil {
			result.CleanupError = err.Error()
			executionErr = errors.Join(executionErr, err)
		}
	}()
	created, err := m.docker.Run(ctx, args...)
	if err != nil {
		result.Output = skillCheckOutput(string(created))
		return result, fmt.Errorf("创建测试容器失败：%s", skillCheckOutput(err.Error()))
	}
	container = strings.TrimSpace(string(created))
	if !skillCheckContainerID.MatchString(container) {
		container = ""
		return result, errors.New("Docker 未返回有效的测试容器身份")
	}
	logs, startErr := m.docker.Run(ctx, "start", "--attach", container)
	result.Output = skillCheckOutput(string(logs))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	stateData, err := m.docker.Run(ctx, "inspect", "--format", "{{json .State}}", container)
	if err != nil {
		return result, errors.New("无法确认测试容器的退出状态")
	}
	var state struct {
		Status    string
		Running   bool
		ExitCode  *int
		Error     string
		OOMKilled bool
	}
	if json.Unmarshal(stateData, &state) != nil || state.Running || (state.Status != "exited" && state.Status != "dead") || state.ExitCode == nil {
		return result, errors.New("测试容器没有可确认的终止状态")
	}
	result.ExitCode = *state.ExitCode
	if state.Error != "" || state.OOMKilled {
		return result, errors.New("测试容器启动失败或超过内存限制")
	}
	if job.Action == "run" {
		result.Report, err = m.runSkillCheckCopy(ctx, container)
		if err != nil {
			return result, err
		}
	}
	if startErr != nil && result.ExitCode == 0 {
		return result, errors.New("测试连接失败，不能确认完整执行结果")
	}
	return result, nil
}

var _ io.Writer = (*skillCheckCapture)(nil)
