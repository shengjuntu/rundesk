# 0.10.0 验证记录

## 已完成

- `go test -race ./...`：所有包含测试的包通过。覆盖原有会话、队列、Cron、应用凭据、审批、恢复和轨迹逻辑，并新增 Docker 环境绑定、资源参数、固定镜像、文件保留、原生会话兼容、租约释放、管理员权限、重启恢复和轨迹快照隔离测试。
- `go vet ./...`：通过，无输出。
- Linux amd64 静态二进制构建与版本输出：通过。
- Windows amd64 交叉编译：通过；未在 Windows 实机运行。
- Python 实机验收脚本和模拟器语法检查：通过。
- OpenAPI：生成 65 条路径、81 个操作，新增运行环境管理契约与会话环境字段。
- Chromium 桌面与 390px 手机端：共 84 项检查通过，无未捕获 JavaScript 错误。

| 浏览器检查 | 数量 | 记录 |
| --- | ---: | --- |
| 应用、完整 Skills、MCP 和草稿 | 13 | product-validation-0.10.0.json |
| Docker 环境管理 | 12 | docker-validation-0.10.0.json |
| 任务队列 | 7 | queue-validation-0.10.0.json |
| 定时任务 | 7 | schedule-validation-0.10.0.json |
| 应用凭据 | 7 | credentials-validation-0.10.0.json |
| 失败恢复 | 8 | recovery-validation-0.10.0.json |
| 轨迹过程与关联分析 | 16 | process-validation-0.10.0.json |
| HTTP 与运行错误 | 14 | diagnostics-validation-0.10.0.json |

Docker 浏览器检查使用独立的 Docker CLI 模拟器、真实租约监管进程和 Codex 原生 JSONL 演示协议。验证界面创建应用与独立项目、启动、重建、数据保留、原线程继续、认证路径、错误显示和刷新恢复。

额外 Linux 子进程测试真实启动另起进程组的工具，撤销租约后确认工具结束，同时另一无关进程继续运行。测试发现了 procfs 与当前 PID namespace 不一致的问题；监管器已使用 NSpid 映射、pidfd 和启动时间校验，避免误杀和 PID 复用。

## 未完成的实机验证

当前执行环境没有 Docker CLI/daemon，未真实创建 Docker 容器、构建示例镜像、登录 Codex 或执行付费模型任务，也未验证外部业务 MCP。Rootless Docker、cgroup 配额、SELinux/AppArmor 与嵌套沙箱依赖目标主机配置。

因此模拟测试通过不能代替真实容器、模型认证和业务工具的联调。请在部署主机按 DOCKER-ENVIRONMENTS.md 使用独立数据目录运行 `scripts/docker-real-smoke.py`，再验收真实任务、审批、取消和产物。

本版无完整多用户登录/RBAC、镜像构建管理、远程 Docker 或 Windows/macOS Docker 执行支持。

终端记录位于 `validation-0.10.0/`，截图位于 `screenshots/0.10.0/`。
