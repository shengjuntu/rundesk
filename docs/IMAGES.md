# 镜像版本与构建信息（0.10.1）

应用是一份长期配置，可登记多个镜像版本，拥有多个项目环境。应用目标版本用于新环境；已有环境保存自己的镜像和资源快照，直到管理员显式更新。通用助手仍使用本机 Codex。

## 页面操作

1. 在 RunDesk 所在 Linux 主机通过命令行或 CI 构建镜像，或手动从仓库拉取。
2. 打开「应用 → 镜像与构建 → 登记镜像」，填写标签、Image ID 或仓库 Digest。
3. RunDesk 读取本机镜像信息，检查 Linux、主机架构及禁止 VOLUME 的要求；登记不启动容器，也不更改应用目标。
4. 点击「设为应用版本」。服务端核对固定 Image ID 的可用性，并保存应用目标。
5. 查看「环境使用情况」。待更新环境仍能使用原镜像和资源继续工作；点击「更新此环境」只更新选定的空闲环境。

同一标签解析到不同 Image ID 时会产生独立版本；重复登记相同标签与 Image ID 返回原记录。版本记录按应用隔离，最多 200 条/应用；本版不删除镜像、版本记录或执行全局清理。

应用概览仅显示镜像摘要、环境总数和待更新数。镜像版本、平台、大小、Image ID、仓库 Digest、构建来源和预装能力在独立页面查看。Image ID 与仓库 Digest 分开显示，不假定二者相等。

## 构建信息从哪里来

这版支持已有镜像的元数据管理，没有 RunDesk 内部构建队列、拉取任务或构建日志。界面不提供不可用的构建按钮。

使用 `docker image inspect` 读取身份、平台、大小、创建时间及以下标签。标签是构建者声明，不是验证过的来源证明。界面只展示约定字段，不展示 Config.Env 或其他任意标签，不执行容器内命令来猜测能力。

| 标签 | 显示内容 |
| --- | --- |
| org.opencontainers.image.version | 软件/镜像发布版本 |
| org.opencontainers.image.source | 源码地址 |
| org.opencontainers.image.revision | 源码提交 |
| org.opencontainers.image.created | 构建时间声明 |
| io.rundesk.build.dockerfile | Dockerfile 路径 |
| io.rundesk.build.url | 外部 CI 构建记录地址 |
| io.rundesk.codex.version | Codex 版本 |
| io.rundesk.capabilities.tools | 工具清单（文本，可附版本） |
| io.rundesk.capabilities.skills | 镜像默认 Skills 清单 |
| io.rundesk.capabilities.mcp | MCP 服务程序和依赖清单 |

单个标签最多读取 4096 字节，超过则按未提供处理。未知字段显示“未提供”；不把登记时间、拉取时间或镜像创建时间冒充声明的构建时间。外部地址以文本展示，不自动访问。不要在这些元数据里填写凭据。

`examples/docker/Dockerfile` 已提供标签示例，可传入 `IMAGE_VERSION`、`SOURCE_URL`、`SOURCE_REVISION`、`BUILD_DATE`、`BUILD_URL` 等构建参数。`CODEX_VERSION` 仍必须明确指定。工具清单由维护镜像的人随 Dockerfile 更新。

镜像预装能力与环境中实际启用的配置不同：首次初始化从 seed 补充文件，之后已有 Skills、MCP 连接和认证保存在宿主机。升级镜像不会覆盖已经初始化的配置；要更新技能请显式导入目录或 ZIP。

## 版本和更新规则

- 登记：读取镜像，不更改目标或现有环境。
- 选择：固定应用目标 Image ID。允许正在使用旧 Docker 环境的任务继续运行，不关闭其连接。
- 新建环境：复制当时的应用目标和资源快照；第一次启动使用此快照。
- 启动、停止、移除后启动：沿用该环境已有的固定 Image ID。
- 更新/重建：先验证目标镜像身份、平台和挂载兼容性，再关闭空闲连接、停止并重建这个容器。运行任务或活动连接会阻止重建。
- 目标检查失败：在停止/删除原容器前返回错误。通过检查不代表所有运行条件都已满足；后续容器创建或 Codex 探测仍可能失败，失败原因会保留，可修复后重试。
- 选择旧版本后更新环境：可以重新部署旧镜像，但不会回退宿主机文件、原生历史或配置的变化。

页面更新操作同时提交环境 revision 和 applicationRevision；目标被其他管理员修改时返回 409，需刷新。旧客户端仍可省略 applicationRevision，语义为“使用请求执行时的应用目标”。

兼容未登记的旧配置：标签在首次创建时固定，显式重建时重新解析。应用页提示“标签未登记”；建议登记并选择固定版本以获得可追踪的升级影响。

检查状态是“上次检查”，带时间。检查失败可能是镜像缺失、Docker 离线或权限故障，保留具体原因，不武断标成“已删除”。读取镜像目录和页面刷新只读持久化记录，不连接 Docker；“检查可用性”按固定 Image ID 重新核对。容器状态实时检查仍在「运行环境」。

## 管理 API

全部接口仅限管理员；现有应用凭据不能登记、选择、检查或列出镜像目录。

| 方法 | 路径（前缀 /api/v1） | 请求/结果 |
| --- | --- | --- |
| GET | /instances/{iid}/images | versions、execution、instanceRevision、environments、pendingUpdates |
| POST | /instances/{iid}/images | `{ "reference": "rundesk-news:1.0" }`，成功返回 ImageVersion |
| POST | /instances/{iid}/images/{vid}/check | 无请求体；返回更新后的 ImageVersion |
| POST | /instances/{iid}/images/{vid}/select | `{ "revision": 3 }`，成功返回 Instance |
| POST | /environments/{eid}/actions | `{ "action": "recreate", "revision": 2, "applicationRevision": 4 }` |

`check` 的 HTTP 200 表示检查结果已记录；客户端必须检查 `availability`（available/unavailable）和 `error`。登记或选择无法使用的镜像返回结构化错误。选定版本的 execution 增加 `imageId` 和 `imageVersionId`；直接 PUT execution 时二者必须与本应用登记版本一致，不能引用另一个应用的记录。

本版不改变应用任务、Cron、幂等提交和 App Server 通信方式。镜像管理不是完整多用户权限系统；完整多用户仍为后续版本范围。

参考：[Docker image inspect](https://docs.docker.com/reference/cli/docker/image/inspect/)、[OCI 元数据字段](https://github.com/opencontainers/image-spec/blob/main/annotations.md)。
