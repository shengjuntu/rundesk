# 应用 Docker 运行环境（0.10.1）

通用助手始终运行本机 Codex。应用可选择 Docker 镜像；一个应用对应专用配置，同一个应用的每个项目有独立环境。同项目会话复用容器，同时使用独立 App Server 连接。UI 不要求用户理解 instance；API 保留 instanceId 以兼容现有客户端。

| 层级 | 内容 | 生命周期 |
| --- | --- | --- |
| 应用配置 | 镜像引用、默认模型、权限、CPU/内存/进程/网络限制 | 长期保存 |
| 项目环境 | 固定镜像 ID、CODEX_HOME、HOME、项目目录 | 长期保存 |
| 容器 | 上述环境的执行载体 | 可停止、移除、重建 |
| 会话 | 原生 thread、turn、运行事件、输入及产物引用 | 不随容器删除 |

## 从界面开始

1. 管理员在 Linux 主机安装 Docker Engine 和 CLI，并让运行 RunDesk 的服务用户访问本机 socket。
2. 构建或拉取应用镜像。在「应用 → 添加应用」选择 Docker，填写镜像；新应用默认新建独立项目，避免共用默认工作区；已有应用在「运行环境」中修改。
3. 在「镜像与构建」登记已有镜像，并设为应用版本；回到运行环境，点击「新建独立项目」。一个应用可创建多个项目环境；每个环境可复制自己的 API 接入信息。
4. 启动容器。在该环境卡片点击「配置此项目的能力」，设置模型、认证、Skills、MCP。认证页显示该容器的登录命令。
5. 应用提交任务时传入对应 workspaceId；已有 API、队列、Cron、SSE、审批和恢复接口不变。应用凭据仍须授权这个项目。
6. 任务空闲后可停止、移除或重建容器。已有原生线程在后续会话轮次通过 thread/resume 恢复，不会自动重发中断任务。

默认 2 CPU、2048 MiB 内存、256 进程、15 分钟空闲回收，最多同时运行 16 个环境。每分钟检查一次空闲状态，空闲配置连接也会关闭。停止容器并不暂停任务队列；后续任务可再次启动它。

「刷新列表」读取已记录状态；「核对状态」实时读取 Docker，显示退出码、OOM 标记和错误。状态在后台重启后先标为待核对。Docker 不可用、镜像缺失、配置过期等返回结构化 4xx/5xx，包含 requestId，并显示在错误记录中，不统一吞成 HTTP 500。

## 数据、技能与认证

宿主机数据布局：

```text
<data>/environments/<environment-id>/codex/   # 原生历史、配置、认证、Skills
<data>/environments/<environment-id>/home/    # 工具缓存和用户目录
<data>/environments/<environment-id>/leases/  # 运行控制，仅容器只读可见
<data>/environments/<environment-id>/traces/  # 选定来源会话的只读分析快照
<workspace.path>/                            # 项目文件、上传和产物
```

容器内保留相同绝对路径，附件、技能脚本和产物无需改写路径。目录不随着 session 增长而新增容器。只读根文件系统、内存 tmpfs 和有上限的 Docker 日志限制容器自身写入；镜像不能声明匿名 VOLUME。

**挂载只改变存储位置，不会自动减少历史、产物或缓存。** 本版不自动删除宿主机数据，请按业务保留周期备份、归档和清理。Docker 镜像清理由管理员管理，本版不执行全局 prune。

Skills、MCP、认证、原生记忆以「应用＋项目」环境为范围；应用模型默认值与权限作为共享默认配置。镜像可提供 `/opt/rundesk-seed/config.toml` 和 `skills/` 完整目录，首次初始化只补充缺失文件，不覆盖已有编辑，不复制 auth.json 或历史。更新镜像不会覆盖已初始化的技能；新版本技能可在界面显式导入。

通用助手和旧本机应用的认证不会被复制进容器。镜像中禁止包含密钥；容器不会继承 RunDesk 的服务 Token、宿主机 API Key 或 docker.sock。使用认证页的容器登录命令，或在 MCP 配置中明确提供所需凭据。

## 镜像和主机要求

首版支持 **Linux 本机 Docker Engine 20.10+、Linux 内核 5.3+、同架构镜像**。只接受 Unix socket，默认 `/var/run/docker.sock`；可设置 `RUNDESK_DOCKER_HOST=unix:///run/user/1000/docker.sock` 使用 Rootless Docker。普通 Docker 使用服务进程的 UID/GID；Rootless Docker 使用映射到 daemon 用户的容器 UID 0。推荐 RunDesk 与 rootless daemon 使用同一普通用户。另行配置的 daemon userns-remap 暂不支持，会明确拒绝启动。

Windows/macOS 仍可运行本机助手；这两个平台以及远程 Docker daemon 的路径映射尚未实现。容器监管需要 pidfd 与 subreaper；安全策略禁止这些调用时会明确报错，不能降级为无法可靠取消的执行。

```bash
# 在发布包根目录构建；版本由管理员明确选择。
docker build -f examples/docker/Dockerfile \
  --build-arg CODEX_VERSION=0.159.2 \
  -t rundesk-news:1.0 examples/docker
```

示例版本曾用于先前的本机协议验证；本版未在真实 Docker 中验证它。可选择你已验证的 Codex 版本。示例 Dockerfile 包含 Node、Python、Git；按应用需要添加工具、MCP 依赖和 `seed/skills/<name>/` 目录。不要声明 VOLUME、安装时嵌入密钥，或依赖启动 ENTRYPOINT：RunDesk 会覆盖入口并挂载自身静态编译的监管助手。

构建 RunDesk Linux 二进制请使用 `CGO_ENABLED=0 go build -buildvcs=false -o bin/rundesk ./cmd/rundesk`，确保挂载进容器的助手不依赖宿主机动态库。升级 RunDesk 后建议在空闲时重建已有环境，以使用新版本监管助手。

Codex 自身的权限设置继续生效；不会自动关闭原生沙箱。目标镜像与主机需支持所选的 Codex 沙箱，真实验收需实际执行所用工具。若不支持嵌套沙箱，管理员可在应用高级运行权限中明确选择容器内的 `danger-full-access`，此时 Codex 可访问该容器内全部可写挂载；这属于显式权限选择，RunDesk 不会自动降级。不要以添加 privileged 或挂载 docker.sock 作为解决方式。资源限额依赖主机 cgroup 支持，Rootless 部署应正确启用 cgroup v2/systemd。容器仅是应用执行边界，本版没有完整多用户登录、RBAC 或用户级配额。

## MCP 与应用网络

stdio MCP 命令在同一容器中启动，其依赖应放进应用镜像。HTTP MCP 使用容器能够访问的服务地址。容器内的 localhost 指向容器自身；访问宿主机服务可使用 `host.docker.internal`（bridge 网络下映射 host-gateway），服务需要监听容器能到达的地址。Rootless 网络访问取决于主机网络配置。

不要将宿主机 RunDesk 管理员凭据放入镜像。需要访问业务应用或 RunDesk API 的 MCP，应使用对应应用、项目和操作范围的凭据。`network=none` 会阻止模型和外部 MCP 的网络连接，适用于明确不需要外部网络的环境。

## 生命周期与迁移

登记并选择的应用版本固定 Image ID；旧标签配置仍在首次创建环境时解析并固定。停止、移除后自动恢复使用环境原 ID。修改应用镜像或资源后，已有环境继续使用原快照，页面显示待更新。显式更新才采用当前应用目标；未登记标签则在重建时重新解析。先检查目标镜像，再停止原容器。详见 [镜像与版本管理](IMAGES.md)。

0.9.2 升级保留已有应用的本机模式。切换本机/Docker 后，旧会话保留原运行方式绑定；需要恢复原模式才能继续旧会话，或新建会话使用新环境。切换前已排队的任务也保留提交时的绑定，不会静默搬迁历史。

容器退出或后台重启后，不自动重发模型任务。先在错误记录、运行状态及轨迹中核对，再使用现有「核对并继续」。终止连接会撤销该连接的租约；监管助手结束其子进程，包括另起进程组的工具，不停止同环境的其他会话。后台意外失联后，租约最长约 20 秒到期。

轨迹分析在新的分析会话中运行，容器只看到所选来源会话、固定事件游标的只读 SQLite 快照，不挂载 RunDesk 总数据库。环境中的同项目会话处于同一信任范围，不用于隔离互不信任的用户。

## API

以下管理接口仅允许管理员 Token/Cookie；应用凭据无 Docker 控制权限。

| 方法 | 路径（/api/v1） | 用途 |
| --- | --- | --- |
| GET | /docker/status | 检查 Docker |
| PUT | /instances/{iid}/execution | `{revision, spec:{mode,image,...}}` |
| GET | /environments?instanceId=... | 已记录环境列表 |
| POST | /environments | `{instanceId,workspaceId}`，同一组合返回同一环境，不启动 Docker |
| GET | /environments/{eid} | 实时核对状态 |
| POST | /environments/{eid}/actions | `{revision,action}`，start/stop/remove/recreate/inspect |

更新需当前 revision，冲突返回 409。会话响应增加 executionMode、environmentId。完整字段见 OpenAPI。业务应用无需发送容器 ID、宿主机路径、启动命令或额外 Docker 参数。

## 实机验收

本版自动验证使用 Docker CLI 模拟器、真实租约监管进程和 Codex JSONL 演示协议，不等同真实容器验收。请先使用独立测试数据目录启动真实模式的 RunDesk：

```bash
./bin/rundesk --data /absolute/path/to/rundesk-docker-acceptance
python3 scripts/docker-real-smoke.py --image rundesk-news:1.0
```

脚本检查真实容器启动、Codex 配置/Skills/MCP 状态接口及移除重建后的持久数据，最后只移除它创建的容器，不调用模型，不删除项目数据。再登录该环境并执行一条实际研究任务，确认沙箱、模型认证、MCP 网络与产物路径。通过之前，不应把模拟结果当成生产验收。

参考：[Docker bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)、[Docker exec](https://docs.docker.com/reference/cli/docker/container/exec/)、[Codex App Server](https://developers.openai.com/codex/app-server/)、[Codex 配置](https://developers.openai.com/codex/config-reference/)。
