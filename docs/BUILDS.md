# 应用镜像构建（0.12.0）

构建是应用的管理员操作，不占用或创建 Codex session，也不通过模型生成 Dockerfile。普通成员和应用 API 凭据不能上传、启动、读取日志或管理构建。

## 操作

1. 在应用运行环境中启用 Docker。主机需要 Linux、本机 Docker Engine、Docker CLI 和 Buildx/BuildKit；服务用户需要访问所配置的 Unix socket。
2. 准备 Dockerfile、工具、Skills 等目录。ZIP 根目录就是构建上下文；如果 ZIP 顶层包含文件夹，Dockerfile 路径要包含该文件夹，COPY 的路径也仍相对于 ZIP 根目录。
   使用 `examples/docker/Dockerfile` 时，先把 `ARG CODEX_VERSION` 改成你已验证版本的默认值（`ARG CODEX_VERSION=...`），其他版本标签也可填写默认值；本版页面不传入 build args。将 `examples/docker` 内的 Dockerfile 与 seed 目录一起打包。
3. 进入「应用 → 镜像与构建 → 构建任务 → 新建构建」。上传 ZIP，填写名称、Dockerfile 相对路径、超时分钟数，可选择不使用缓存。
4. 上传后状态为「待启动」。检查后点击「开始构建」。所有应用共用一个串行构建队列。
5. 查看日志和错误。成功后点击「查看镜像版本」，再显式设置应用版本、更新指定项目环境。构建成功不会自动切换正在使用的镜像。

上传校验不会执行 Dockerfile。启动构建会执行其中的指令，并可能拉取基础镜像及联网下载依赖。上下文和日志仅供管理员使用；不要把认证文件或 API Key 放入 ZIP。`.dockerignore` 原样保留，Docker 按其规则选择构建输入；它不会阻止这些文件先被上传保存。

## 构建身份

RunDesk 为每次任务生成独立的 `rundesk-build/<instanceId>:<buildId>` 标签，避免覆盖已有应用标签。后台调用本机默认 builder，加载单平台结果，并从 `--iidfile` 读取 SHA256 Image ID。镜像版本按这个固定 ID 登记，而不是重新解析一个可能变化的标签。

构建记录保存 ZIP SHA256、Dockerfile 路径、文件数、大小、开始/结束时间、状态、镜像 ID 和版本关联。SHA256 是上传内容的指纹，不代表可重现构建：基础镜像标签、网络依赖等仍可能变化。建议在 Dockerfile 中固定依赖版本，并提供 OCI 与 RunDesk 标签。构建通过只表示 Docker 成功及镜像平台/挂载规则检查通过，不代表 Codex、Skills 或 MCP 已完成运行验收。

## 状态与恢复

`draft → queued → running → succeeded / failed`。

- 重复提交同一任务的 start 返回原任务，不重复启动。上传响应丢失时应先刷新记录，避免生成多个待启动副本。
- 取消排队任务不会执行 Docker；取消运行任务会向构建进程组发出中断，必要时强制结束客户端。
- 取消或超时不保证 Docker daemon 端完全回滚，可能已有镜像和缓存。界面明确保留这个区别，不自动删除 Docker 资源。
- 服务关闭或重启后未完成任务标为 `interrupted`，不自动重跑。检查 Docker 状态后重新上传并启动新任务。
- 单次默认超时 30 分钟，可配置 1–120 分钟。
- 构建失败时原应用目标、容器和项目数据不变。镜像已生成但登记失败时保留 Image ID，可解决问题后手动登记。

## 存储与限制

构建数据在 `<data>/image-builds/<buildId>`，与项目 session 数据分开。ZIP 最大 32 MiB，解压后最大 128 MiB，最多 4000 个文件/目录；拒绝绝对路径、路径穿越、重复路径、符号链接和特殊文件，保留脚本的可执行位（普通文件规范为 0644，可执行文件和目录为 0755）；外层任务目录只允许服务用户访问。

上下文只在待启动、排队和运行期间保留；任务结束后删除上下文。日志最多保存 4 MiB，达到上限会明确提示，构建仍可继续。页面增量读取日志，「下载已读取日志」下载当前已经加载的部分。每应用最多 200 条记录，全局最多 20 个未完成任务；删除旧记录可释放 RunDesk 占用。

镜像和 BuildKit 缓存保存在 Docker 的数据目录，不在项目挂载目录中。这些仍可能增长；本版不自动运行 prune，也不自动删除镜像。由管理员使用 Docker 的磁盘使用和定向清理工具管理。

## API

全部路径以 `/api/v1/instances/{iid}/builds` 为前缀，仅管理员可用：

| 方法与路径 | 行为 |
|---|---|
| GET / | 列出当前应用构建 |
| POST / | multipart 上传 file、name、dockerfile、timeoutMinutes、noCache，创建 draft |
| GET /{bid} | 获取构建状态 |
| POST /{bid}/start | 显式入队，重复请求不重复执行 |
| POST /{bid}/cancel | 取消待启动、排队或运行任务 |
| GET /{bid}/log?after=0 | 按字节偏移读取，最多 64 KiB，返回 text、next、status、truncated |
| DELETE /{bid} | 删除非运行任务的记录和文件，不删除 Docker 镜像或缓存 |

路径表中的根路径实际不带末尾斜线。OpenAPI 版本 1.11.0。

首版不提供 Git 拉取、仓库推送、跨平台构建、用户自定义 build args/secrets、远程 builder 或自动重试。更大的上下文可在命令行或 CI 构建后登记镜像。

官方接口参考：[Docker build](https://docs.docker.com/reference/cli/docker/buildx/build/)；[构建上下文](https://docs.docker.com/build/concepts/context/)。
