# 构建中心：配方、镜像与 Agent

日常准备工具环境从构建中心开始：选择配方，构建或复用镜像，再创建制作环境。本文将同一流程的操作、身份、状态、镜像元数据和管理契约集中说明；原应用绑定的 ZIP 构建与镜像版本接口作为兼容管理能力列在后两节，不新增日常入口。

<a id="build-workbench"></a>

## 配方到 Agent 的工作台

“构建中心”是跨应用的常驻入口，0.55.0 增加外置配方与构建前下载源配置，0.56.0 接入首次初始化、Kun 构建助手和失败修复经验；0.57.0 加入公开镜像目录和验证后发布，0.58.0 补充 Codex 运行时复用及归档缓存。新闻搜索、媒体搜索、深度研究和视频制作可以共用配方或镜像；应用、Skills、MCP、插件、实例和任务自由组合，不要求定义工作流或应用层级。

| 资源 | 负责的内容 |
| --- | --- |
| 原生插件 | 按 Codex 格式分发 Skills、MCP 声明和资源 |
| 构建配方 | Dockerfile、上下文 ZIP、来源、原生插件引用和本地 smoke 声明 |
| 镜像 | 实际固定 Image ID、来源、引用、可用性和检查结果 |
| 公开镜像目录 | 已发布镜像的预设版本、平台与固定仓库 Digest，不包含实例认证 |
| 实例与环境 | 模型配置、独立 HOME、工作区及现有容器生命周期 |
| 任务 | Codex 日常执行；Kun 内部维护；进度、审批、轨迹和产物 |

<a id="workbench-1"></a>

### 配方 → 镜像 → Agent 工作台

工作台先选择配方，再构建或选择已有镜像，最后从该镜像创建制作环境。构建进度、候选镜像及关联的制作环境显示在同一个工作台；失败可查看原日志并继续处理。一个配方可有多个构建结果，一个镜像可创建多个制作环境，已有镜像可以直接使用。

镜像首先显示配方名称、版本与状态，技术标签和固定 Image ID 作为次级信息。构建与来源记录新增可选的冻结 `recipeName`、`recipeVersion`；旧记录没有这些字段时不补造历史名称和版本。高级配方、镜像、构建/测试和发布深链接继续可用。

Agent 创建时可显式继承当前默认 Codex 模型服务，也可保留原配置。模型地址、认证引用和所选模型属于运行环境，不加入 Dockerfile 或镜像。配置及验证范围见[Codex 模型服务](CODEX-PROVIDERS.md)。

<a id="workbench-2"></a>

### 使用

打开左侧“构建中心”，选择已有配方构建，或导入自己的 manifest 与 Docker context ZIP。“查看配方”可预览 Dockerfile 并下载完整构建目录；“下载源设置”维护国际/中国镜像源列表，构建前按配方支持逐项选择或修改。内置六项为 news2douyin、people-search、video-build、HyperFrames、video-use、OpenMontage。构建在后台排队，可以查看日志、取消和重建。已有 setup/应用构建也在全局列表中显示；原有入口继续工作。

0.69.1 的 OpenMontage 1.1.1 将进一步精简的固定运行源码随完整配方提供，省略审阅过的演示媒体、文档预览图及未引用的示例 Logo；首次构建也不再下载整个 GitHub 归档。原入口、源选择和最终检查保留；旧导入配方与历史构建不会自动换源或改包。源码包范围及仍需联网的依赖见[OpenMontage 随包源码](BUILD-SOURCES.md#openmontage-bundled-source)。

本机构建的 Codex 来源与网络配置分别选择：自动复用匹配的受管 Linux 原生包、明确使用受管包、指定兼容的本地基础镜像，或通过 npm 安装。原生包摘要与基础镜像身份随任务冻结，失败不会通过修改版本或降低 smoke 来规避。具体条件见[构建运行时复用](BUILD-RUNTIME.md)。预构建镜像下载不需要选择这些构建参数。

0.68.0 的六套内置配方按需复用系统、媒体、浏览器公共基础层。选好配方后仍只提交一次构建；同一任务日志依次显示公共层准备或复用、配方构建与最终检查。公共层失败直接使当前任务失败，可沿用原失败处理和 Kun 修复入口。基础层使用自己的文件、源参数、平台与固定父镜像身份判断复用，Codex 和领域依赖留在配方中，不新增独立工作台或手工选择公共层的步骤。

构建详情提供可展开的公共层身份，初始化准备卡显示已准备层数。“不使用缓存”在新建构建中重建公共层；用于按原配置重建时，保留已冻结的实际基础镜像 ID，只重建配方层和未完成的基础层，不改用后来移动的上游标签。带公共层计划的新任务仍执行本次来源准备与记录，普通 Docker 层缓存可继续复用。

构建成功后在镜像区检查固定身份、查看引用、执行离线检查或声明的 smoke，或者创建并启动专用 Codex Docker 实例。启动环境不发起模型任务。已有应用的镜像更新仍需在其原管理界面显式操作。

构建助手接受用途或开源项目地址，使用“环境设置”中已配置的本机 Kun 维护助手发起专用队列任务；弹窗不再要求选择引擎与工作区。它在新专用工作区读取 `add-app-docker-image` Skill，分析依赖、来源和许可证，生成可审阅的配方及 ZIP，通过受限构建 MCP 构建、读日志、修复和测试。会话和任务继续在 RunDesk 中调试。引用项目仅为编号上下文，不直接借用其目录；维护助手需要可用的引擎、模型认证和工具能力。

新的 Kun 维护任务采用 `protocolVersion:1`：每次 `build_start` 前必须调用 `build_plan_record`，两者使用相同的 `requestId`。计划记录目标、诊断假设、保留能力与验收条件。宿主从同一候选的冻结镜像、上下文和实际测试记录判断验证状态；模型回复“成功”或任务完成都不代表修复已通过。旧任务保持原合同，旧 Codex 构建助手 API 作为兼容路径保留。具体边界见[内部维护](MAINTENANCE.md)。

初始化可为本次新构建启用后台失败自动交接，也可手动将明确选中的失败或中断构建交给 Kun。`sourceBuildId` 为该修复任务授权只读来源快照；助手能读取原日志、导出冻结上下文，在自身工作区修改后打包成新的独立配方。未显式选择的其他任务、配方和工作区不属于修复范围。修复构建保留原源配置，除非本次明确选择新的配置；旧配方、镜像及应用绑定不覆盖。

0.64.0 的初始化页将原构建、后台交接、修复对话、候选与检查结果合在同一准备任务中。管理员可按精确的原构建编号读取持久修复关联，重新打开页面不必重新提交修复。助手任务结束与候选通过验证分别判断：`maintenance.verifiedBuildId` 及对应 offline/smoke 证据才表示维护验证成功；原失败记录保持原状态。

助手为每次任务 bootstrap 一个独立 Skill，并向对应 Codex 线程或 Kun 请求注入短期构建 MCP；回执返回 `runtimeKind`、可选 `sourceBuildId`，以及 `skillBootstrap:true`、`nativePluginInstalled:false`。MCP 提供上下文导出/打包、源能力读取和构建检查，Kun 不需要为打包而开启宿主 Bash。`build_wait` 可等待 1–45 秒后读取状态，避免长构建反复消耗模型轮询；服务端每项构建助手最多提交三次构建。初始化 Kun 构建任务的两小时预算不覆盖其他用户实例原有设置。原生插件源包见 [rundesk-build-tools](../plugins/rundesk-build-tools/README.md)，插件安装另见[原生插件支持](NATIVE-PLUGINS.md)。

修复经验记录失败指纹、源快照、实际改动、成功镜像和验证编号。服务端从冻结目录比较文件摘要和有界 Dockerfile 差异；没有冻结目录时明确标记缺失，不读取后来变化的配方来补证据。成功构建仍须有精确绑定的 offline 和 smoke 才能形成已验证镜像经验，缺项保留候选。后续修复可检索匹配经验，不能据一次网络失败永久改写全局下载策略。完整边界见[初始化与经验](SETUP.md#修复经验与持续改进)。

<a id="workbench-3"></a>

### 公开镜像分发

普通用户可从初始化第三步下载预构建镜像，避免在本机重新下载和安装配方依赖。维护者在构建中心编辑公开镜像目录，或对已验证的固定镜像提交发布任务。发布入口核对同一来源构建、Image ID、冻结上下文和平台对应的 offline 与 smoke 结果；缺项会显示原因。上传成功记录仓库 Digest，供登记到公开目录，不会自动改变实例或应用绑定。

仓库登录沿用 RunDesk 服务账户的 Docker 配置；托管平台公开可见性由维护者设置，之后可在发布记录中单独检查匿名访问。默认目录为空，代码不假定任何 RunDesk 官方账号已存在。完整步骤、文件格式及中断处理见[镜像分发](IMAGE-DISTRIBUTION.md)。

<a id="workbench-4"></a>

### 动态配方

内置来源配方可声明 `sharedFiles` 以复用 `recipes/common/`。导出时物化普通文件，本地同名文件覆盖，缺失共享来源失败；快照和锁文件不保留共享声明。详见[共享输入](BUILD-SOURCES.md#shared-recipe-files)。导入的独立 ZIP 不需要 common 目录。

自定义配方存于 `data/build-recipes/<id>/revisions/<revision>/`，包含冻结 manifest、原始 ZIP 和摘要；无需修改宿主源码或重新编译。每次导入产生独立配方版本，修改内容应导入新版本。归档不删除历史输入。默认配方的唯一来源在 `recipes/<profile>/`，可直接编辑；`recipe.json` 在冻结上下文时生成 `profile.lock.json`，Go 不嵌入配方 ZIP，也不依赖 Python。修改影响新建构建，无需重编译或重启；已冻结的历史 ZIP 不改变。news2douyin、people-search 保留 1.1.2 的 Agent-Reach 本地源码修复。外置目录、源字段及边界见[下载源配置](BUILD-SOURCES.md)。

manifest 示例：

```json
{
  "schemaVersion": 1,
  "name": "My research tools",
  "version": "1.0.0",
  "description": "Pinned tools for a research task",
  "dockerfile": "Dockerfile",
  "plugins": [],
  "smoke": {
    "command": ["python3", "-c", "from pathlib import Path; Path('/tmp/check.txt').write_text('ok')"],
    "timeoutSeconds": 60,
    "expectedFiles": ["/tmp/check.txt"]
  }
}
```

镜像须提供 Codex CLI、Python 3 及所声明命令，适配现有 Codex Docker 环境。插件引用只记录原生 ID、可选来源和版本，不是 RunDesk 自定义插件包，也不自动安装插件。ZIP 上限 32 MiB，解压后 128 MiB、4000 项；拒绝越界路径、链接、特殊文件、重复路径和常见凭据文件。不要把真实服务密钥、Codex HOME、宿主 socket 或业务数据放入镜像。

重建建立新任务，复制原始冻结上下文、源参数及其摘要，不读取已被修改的工作目录或源列表。未保留原始输入的旧历史会明确拒绝重建。输入相同可复用 Docker 层缓存；`noCache` 仅在确有需要时指定。

<a id="workbench-5"></a>

### 检查、启动和删除

构建与镜像检查复用原有单队列。基础检查或内置 doctor 只验证本机工具、原生 App Server 初始化及已声明的离线行为，不调用模型。未通过检查但已生成实际 Image ID 的构建也保留镜像目录，供核对和清理，不能因此视为可用。smoke 使用冻结 argv 与产物路径，在无网络、无宿主挂载、只读根目录、限时限资源的临时容器中执行。声明命令和检查范围可审阅；通过不代表外部 MCP、模型认证、TTS 音质或完整业务质量合格。没有适用 smoke 的镜像明确返回不可用。

“创建并启动实例”默认建立专用实例、工作区及 HOME；只允许显式复用匹配且空闲的独立实例，不能借用默认助手或其他应用/预设的保留资源。失败后应先核对已创建资源与原回执，不能换 requestId 盲目重复。

以下操作分别管理不同资源：

- 原应用入口删除构建记录：清理任务与日志，不删除 Docker 镜像；全局平台构建记录保留为镜像来源。
- 删除镜像：按固定 Image ID 执行非强制 Docker 删除，保留登记历史、项目与 HOME；实例、环境、活动检查及 Docker 中的停止/外部容器引用都会阻止删除。不会对移动后的同名标签误删新镜像，不使用 prune。
- 移除容器：继续使用环境管理的原生命周期入口，保留宿主工作区和 HOME。

<a id="workbench-6"></a>

### 管理 API

以下为 RDCA `/api/v1` 下的管理路径，普通应用凭据和成员不能访问。新建/构建/重建/启动/测试/删除镜像均使用 24 位小写十六进制 `requestId`；同一操作重试必须沿用，参数改变需新值。该字段由各资源实现持久回执，不使用通用 JSON Idempotency-Key 拦截 multipart ZIP。

| 路径 | 操作 |
| --- | --- |
| `/build-center/recipes` | GET 列表；POST multipart `manifest`、`context`、`requestId` 导入 |
| `/build-center/recipes/{rid}` | GET 明细；DELETE `?revision=N` 归档自定义项 |
| `/build-center/recipes/{rid}/build` | POST `recipeRevision`、`requestId`，可选 reference、codexVersion、noCache、buildSources、codexRuntime |
| `/build-center/runtime-options` | GET `?codexVersion=X.Y.Z` 查看匹配的受管 Linux 原生安装候选 |
| `/build-center/sources` | GET 源列表；PUT 携带 revision 保存，文件即时生效 |
| `/build-center/recipes/{rid}/sources` | GET 配方涉及的下载类型及参数支持 |
| `/build-center/recipes/{rid}/dockerfile` | GET Dockerfile 内容，可选 codexVersion |
| `/build-center/recipes/{rid}/context.zip` | GET 完整上下文；内置配方须提供精确 codexVersion |
| `/build-center/builds` | GET 全局列表 |
| `/build-center/builds/{bid}` | GET 状态；平台成功记录保留为镜像来源 |
| `/build-center/builds/{bid}/log` | GET `?after=N` 增量脱敏日志 |
| `/build-center/builds/{bid}/rebuild` | POST `requestId`、可选 noCache，复制冻结输入 |
| `/build-center/builds/{bid}/cancel` | POST 取消排队/运行任务 |
| `/build-center/images`、`/{key}` | GET 按实际身份聚合的目录与明细 |
| `/build-center/images/{key}/check` | POST 核对固定镜像身份 |
| `/build-center/images/{key}` | DELETE `imageId`、`requestId` 删除实际镜像 |
| `/build-center/images/{key}/launch` | POST `name`、`requestId`，可选专用 instanceId/workspaceId |
| `/build-center/images/{key}/tests` | POST `kind:offline\|smoke`、`requestId`，返回排队任务 |
| `/build-center/tests/{id}`、`/{id}/log` | GET 检查结果和日志 |
| `/setup/public-images` | GET 公开镜像目录；PUT 携带 revision 保存 |
| `/build-center/images/{key}/publication-readiness` | GET 镜像来源与 offline/smoke 发布条件 |
| `/build-center/publications` | GET 发布记录；POST sourceBuildId、target、requestId、confirm 创建发布任务 |
| `/build-center/publications/{id}`、`/{id}/log` | GET 状态与增量脱敏日志 |
| `/build-center/publications/{id}/cancel` | POST 取消排队或运行中的发布 |
| `/build-center/publications/{id}/check-public` | POST 使用独立空 Docker 配置核对固定 manifest 的匿名访问、Digest 和平台 |
| `/build-center/assistant-runs` | POST prompt/requestId，可选 instanceId、workspaceId、sourceBuildId；GET `?sourceBuildId=精确原构建编号` 读取对应修复，最多 20 条并返回 hasMore |
| `/build-center/assistant-runs/{id}` | GET 指定 Codex/Kun 助手与任务、维护验证绑定 |
| `/build-center/assistant-runs/{id}/revoke` | POST 撤销构建工具权限 |

唯一例外为 `POST /api/build-center/mcp`（及 v1 别名）：只接受任务短期 Bearer 能力，端点自己验证任务、会话、实例、工作区、路径和有效期。它只可导入自己工作区的配方、构建自己的/内置配方、查看或取消自己的构建、检查自己的产出；不能删除镜像、启动/改变实例、安装插件或访问其他任务。它不是通用管理 API，管理员令牌也不替代该任务能力。

Docker 助手需将 `RUNDESK_BUILD_MCP_ORIGIN` 或 `--public-url` 设置为容器可达的来源，服务也须监听可达接口。容器内 localhost 指向容器自身；禁网络实例或未提供非回环来源会明确拒绝。短期凭据只在宿主私有数据目录和线程内存中使用，不写入镜像、提示词或共享 Codex HOME。

<a id="application-builds"></a>

## 应用绑定的 ZIP 构建接口（兼容）

此节描述应用绑定的旧 ZIP 构建接口：它是管理员操作，不占用或创建 Codex session，也不通过模型生成 Dockerfile。日常配方与镜像准备使用前述构建工作台；本接口保留既有调用与状态语义。普通成员和应用 API 凭据不能上传、启动、读取日志或管理构建。

<a id="builds-1"></a>

### 操作

1. 在应用运行环境中启用 Docker。主机需要 Linux、本机 Docker Engine、Docker CLI 和 Buildx/BuildKit；服务用户需要访问所配置的 Unix socket。
2. 推荐用 [典型配置 CLI](../recipes/README.md) 的 `pack` 生成已填写 Codex 版本的 ZIP，根目录即 Dockerfile，无需 build args。也可自行准备 Dockerfile、工具、Skills 等目录。ZIP 根目录就是构建上下文；如果 ZIP 顶层包含文件夹，Dockerfile 路径要包含该文件夹，COPY 的路径也仍相对于 ZIP 根目录。
   使用 `recipes/generic/Dockerfile` 时，先把 `ARG CODEX_VERSION` 改成你已验证版本的默认值（`ARG CODEX_VERSION=...`），其他版本标签也可填写默认值；本版页面不传入 build args。将 `recipes/generic` 内的 Dockerfile 与 seed 目录一起打包。
3. 使用已有应用上下文的 ZIP 构建操作或本节 API 上传 ZIP，填写名称、Dockerfile 相对路径、超时分钟数，可选择不使用缓存。日常操作以构建中心为入口。
4. 上传后状态为「待启动」。检查后点击「开始构建」。所有应用共用一个串行构建队列。
5. 查看日志和错误。成功后点击「查看镜像版本」，再显式设置应用版本、更新指定项目环境。构建成功不会自动切换正在使用的镜像。

上传校验不会执行 Dockerfile。启动构建会执行其中的指令，并可能拉取基础镜像及联网下载依赖。上下文和日志仅供管理员使用；不要把认证文件或 API Key 放入 ZIP。`.dockerignore` 原样保留，Docker 按其规则选择构建输入；它不会阻止这些文件先被上传保存。

<a id="builds-2"></a>

### 构建身份

RunDesk 为每次任务生成独立的 `rundesk-build/<instanceId>:<buildId>` 标签，避免覆盖已有应用标签。后台调用本机默认 builder，加载单平台结果，并从 `--iidfile` 读取 SHA256 Image ID。镜像版本按这个固定 ID 登记，而不是重新解析一个可能变化的标签。

构建记录保存 ZIP SHA256、Dockerfile 路径、文件数、大小、开始/结束时间、状态、镜像 ID 和版本关联。SHA256 是上传内容的指纹，不代表可重现构建：基础镜像标签、网络依赖等仍可能变化。建议在 Dockerfile 中固定依赖版本，并提供 OCI 与 RunDesk 标签。构建通过只表示 Docker 成功及镜像平台/挂载规则检查通过，不代表 Codex、Skills 或 MCP 已完成运行验收。

<a id="builds-3"></a>

### 状态与恢复

`draft → queued → running → succeeded / failed`。

- 重复提交同一任务的 start 返回原任务，不重复启动。上传响应丢失时应先刷新记录，避免生成多个待启动副本。
- 取消排队任务不会执行 Docker；取消运行任务会向构建进程组发出中断，必要时强制结束客户端。
- 取消或超时不保证 Docker daemon 端完全回滚，可能已有镜像和缓存。界面明确保留这个区别，不自动删除 Docker 资源。
- 服务关闭或重启后未完成任务标为 `interrupted`，不自动重跑。检查 Docker 状态后重新上传并启动新任务。
- 单次默认超时 30 分钟，可配置 1–120 分钟。
- 构建失败时原应用目标、容器和项目数据不变。镜像已生成但登记失败时保留 Image ID，可解决问题后手动登记。

<a id="builds-4"></a>

### 存储与限制

构建数据在 `<data>/image-builds/<buildId>`，与项目 session 数据分开。ZIP 最大 32 MiB，解压后最大 128 MiB，最多 4000 个文件/目录；拒绝绝对路径、路径穿越、重复路径、符号链接和特殊文件，保留脚本的可执行位（普通文件规范为 0644，可执行文件和目录为 0755）；外层任务目录只允许服务用户访问。

上下文只在待启动、排队和运行期间保留；任务结束后删除上下文。日志最多保存 4 MiB，达到上限会明确提示，构建仍可继续。页面增量读取日志，「下载已读取日志」下载当前已经加载的部分。每应用最多 200 条记录，全局最多 20 个未完成任务；删除旧记录可释放 RunDesk 占用。

镜像和 BuildKit 缓存保存在 Docker 的数据目录，不在项目挂载目录中。这些仍可能增长；本版不自动运行 prune，也不自动删除镜像。由管理员使用 Docker 的磁盘使用和定向清理工具管理。

<a id="builds-5"></a>

### API

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

路径表中的根路径实际不带末尾斜线。契约版本与当前服务 `/api/v1/openapi.json` 一致。

首版不提供 Git 拉取、仓库推送、跨平台构建、用户自定义 build args/secrets、远程 builder 或自动重试。更大的上下文可在命令行或 CI 构建后登记镜像。

官方接口参考：[Docker build](https://docs.docker.com/reference/cli/docker/buildx/build/)；[构建上下文](https://docs.docker.com/build/concepts/context/)。

<a id="builds-6"></a>

### 初始化阶段的典型镜像

0.48.0 可在初始化向导提前构建/拉取/检查典型镜像，无需先注册应用。使用同一持久化后台队列，`scope=setup`，记录位于 `/instances/default/builds`；这只是任务归属，不会把通用助手改成 Docker。普通上传接口仍不允许在 default 创建任意构建。

初始化任务有 source（build/pull/local）、profileId、codexVersion 和 offlineReady。失败时不切换业务环境、不自动回退到现场构建；可以修正网络/仓库/磁盘问题后重试。成功后保留本机镜像、ID 和日志。取消或超时不保证 Docker daemon 端撤销已生成的层；服务重启标记 interrupted，不自动重复提交。

现有应用的 ZIP 构建仍是高级入口，成功后需管理员显式选择或更新镜像。

<a id="image-metadata"></a>

## 镜像元数据与应用版本管理

应用是一份长期配置，可登记多个镜像版本，拥有多个项目环境。应用目标版本用于新环境；已有环境保存自己的镜像和资源快照，直到管理员显式更新。通用助手使用本机后端；托管镜像路径当前适用于 Codex。

<a id="images-1"></a>

### 页面操作

1. 在 RunDesk 所在 Linux 主机通过命令行或 CI 构建镜像，或手动从仓库拉取。
2. 打开「应用 → 镜像与构建 → 登记镜像」，填写标签、Image ID 或仓库 Digest。
3. RunDesk 读取本机镜像信息，检查 Linux、主机架构及禁止 VOLUME 的要求；登记不启动容器，也不更改应用目标。
4. 点击「设为应用版本」。服务端核对固定 Image ID 的可用性，并保存应用目标。
5. 查看「环境使用情况」。待更新环境仍能使用原镜像和资源继续工作；点击「更新此环境」只更新选定的空闲环境。

同一标签解析到不同 Image ID 时会产生独立版本；重复登记相同标签与 Image ID 返回原记录。版本记录按应用隔离，最多 200 条/应用；这组兼容接口不删除版本记录或执行全局清理。实际镜像删除使用前述全局构建中心的固定身份与引用检查。

应用概览仅显示镜像摘要、环境总数和待更新数。镜像版本、平台、大小、Image ID、仓库 Digest、构建来源和预装能力在独立页面查看。Image ID 与仓库 Digest 分开显示，不假定二者相等。

<a id="images-2"></a>

### 构建信息从哪里来

支持外部镜像登记，也支持「构建任务」上传 ZIP、串行构建、查看日志和取消。成功结果按固定 Image ID 自动登记；不会自动部署。详细流程见 [构建说明](BUILD-CENTER.md#application-builds)。

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

`recipes/generic/Dockerfile` 已提供标签示例，可传入 `IMAGE_VERSION`、`SOURCE_URL`、`SOURCE_REVISION`、`BUILD_DATE`、`BUILD_URL` 等构建参数。`CODEX_VERSION` 仍必须明确指定。工具清单由维护镜像的人随 Dockerfile 更新。

镜像预装能力与环境中实际启用的配置不同：首次初始化从 seed 补充文件，之后已有 Skills、MCP 连接和认证保存在宿主机。升级镜像不会覆盖已经初始化的配置；要更新技能请显式导入目录或 ZIP。

<a id="images-3"></a>

### 版本和更新规则

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

<a id="images-4"></a>

### 管理 API

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
