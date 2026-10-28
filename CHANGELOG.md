# Changelog

版本段描述当时的发布行为与验证范围，不替代[当前状态](docs/STATUS.md)。

## 0.58.0（2026-10-28）

- 保持一套配方与三步初始化，本机构建分别选择中国／国际网络配置和 Codex 来源。自动模式优先复用匹配的受管 Linux 平台包，支持明确原生包、已有兼容基础镜像及 npm 安装；记录实际来源、版本、平台和摘要，原配置重建沿用冻结身份。
- 基础镜像复用保留 Node.js 22+、npm、`node` 用户及 Codex App Server 要求，检查失败不静默改为另一种安装。原生包复用核对原下载完整性并保存独立缓存，不复制宿主 HOME 或账号配置。
- 为 OpenMontage 增加完整 GitHub 归档地址选择与 SHA-256 校验缓存，保留固定提交和业务 smoke。新增阿里云 Debian 源选项，已有源列表通过显式补充缺失项和 revision 保存更新；不覆盖部署者原有设置，不将公共代理设为全国唯一默认。
- 保留 Agent-Reach vendored 打包修复、公开镜像下载优先、Kun 失败修复与版本化经验。记录用户报告的六套自定义配方实机构建成功，明确区分其部署证据与本版源码的测试范围；具体检查见[验证说明](docs/VALIDATION.md)。

## 0.57.0（2026-10-27）

- 三步向导的第三步改为“准备 Docker 环境”，默认下载预构建镜像，同时保留本机构建和已有本地镜像检查；拉取后核对预设身份、Codex 版本、平台和离线工具，再使用固定 Image ID。
- 增加外置公开镜像目录 `config/public-images.json`，按预设、版本和平台登记固定 Digest 与多个来源；目录初始为空，真实发布地址由维护者提供。保存使用 revision 并发检查，已提交任务保留原输入。
- 构建中心可检查镜像的发布条件，确认同一来源、镜像与冻结上下文的 offline/smoke 证据后，提交后台推送任务；记录进度、脱敏日志及仓库 Digest。发布使用服务账户的 Docker 登录，另有空凭据环境下的匿名访问检查；不自动修改仓库可见性或公开镜像目录。
- 保留 Kun 构建修复与版本化经验；修复成功不会自行上传公共仓库。源码未包含已经发布的官方镜像，不将上传、匿名下载或目标机器运行视为已验收。

## 0.56.1（2026-10-27）

- 启动向导调整为“寻找和配置 LLM、配置和安装 Codex、构建 Docker”三步；硬件发现与 Kun 启用在同一页，选择发现的模型服务后直接填写该页配置。
- 完成概览、修复经验及进入工作台放在第三步，保留可选步骤跳过、构建源编辑、后台进度及 Kun 自动修复。已完成的初始化状态不重置，可从“环境检查”重开向导。
- 本版不改变 Kun、SDK、worker 协议和后端安装/构建契约；0.56.0 的检查仍作为历史证据，不计为本版重跑。

## 0.56.0（2026-10-27）

- 首次启动先显示全屏初始化页，提供环境、Kun、Codex、Docker 和完成五步，可返回、跳过和稍后配置；已完成的安装不重置，显式会话链接可直接查看。工作台不先抢占首次界面。
- 增加服务器硬件、GPU、工具及 loopback 模型服务自动发现，支持手工 endpoint；目录发现与主动推理/自动函数调用测试分别记录，不把 HTTP 200 或模型名称当作 Kun 就绪。
- 增加专用 Kun 初始化实例、持久 worker 路径和协议握手；保留默认 Codex 实例。模型测试绑定地址/模型/凭据且限时有效，初始化工具不开放宿主 Bash。
- Codex 增加无需 Node/npm 的精确版本原生平台包安装，使用选定 npm 源、SHA-512 校验和执行版本核对；后台任务持久保存、刷新续看，未确认中断不自动重试。旧同步 npm 安装接口兼容。
- 增加受限 Kun 安装修复，最多三次同版本重试；构建助手同时支持 Kun，能接手明确失败的冻结快照，导出/修改/打包独立配方并验证。新任务可逐项开启后台失败自动交接，关闭页面后继续，每个来源只创建一个修复任务。初始化可勾选多套 Docker 预设，保留构建源选择和每项日志。
- 加入可检索的版本化修复经验，候选/已验证由真实安装、镜像与 offline/smoke 证据判定。经验不会自动改写全局 Skill、源列表或已有配方，不修改模型权重。
- 配套 Kun、SDK、worker 与 UHP 协议不变；实际检查和目标环境验收边界见[验证说明](docs/VALIDATION.md)。

## 0.55.0（2026-10-27）

- 将六套构建配方及通用 Docker 下载模板改为 `recipes/<profile>/` 的外置文件；Go 不再嵌入配方 ZIP。Dockerfile 在 examples 中直接维护，CLI 从磁盘模板生成上下文；指定目录缺失时明确报错，编辑文件后的新构建不需重编译或重启。
- 增加可编辑的下载源列表，分为 international 与 china，覆盖 pip、conda、npm、Docker、APT、YUM/DNF、Hugging Face 和 GitHub。构建前按配方检测与参数声明展示相关类型，可跨组选择、逐项覆盖；没有对应参数的配方不会被自动改写。
- 增加 Dockerfile 内容预览和完整构建目录导出。每次构建保存源列表摘要、所选条目与有效参数；按原配置重建保留原快照，不吸收后来修改的文件或源配置。
- 保留 news2douyin、people-search 1.1.2 的 Agent-Reach vendored 源码、许可证、打包补丁及资源检查；默认配方支持基础镜像、Debian APT、npm、pip 参数，OpenMontage 同时使用 GitHub 归档源参数并保留 commit/SHA 校验；其他类型由适用的配方声明使用。
- 原应用、实例、工作区、HOME、认证、镜像与历史记录不自动替换或迁移。真实 Docker 构建、镜像站连通性、速度与业务任务仍需在部署环境验收；本轮实际检查以[验证说明](docs/VALIDATION.md)为准。

## 0.54.1（2026-10-26）

- 将用户实测修复同步到 news2douyin、people-search 1.1.2：从固定 Agent-Reach 提交内置本地源码，移除重复 wheel `force-include`，保留上游 LICENSE、指南及 Skill；记录来源、原始 archive SHA-256、补丁与逐文件摘要。
- CLI 与 Go 内嵌配方统一从 `/opt/rundesk-profile/vendor/agent-reach` 安装；Dockerfile 在 pip 前复制 vendor，不再通过 GitHub archive 下载该源码。其他 apt、pip、npm 依赖仍需下载，不声明完全离线构建。
- 更新运行镜像的预置版本识别及离线资源检查，拒绝将旧 1.1.0 镜像视为新版预置。内置配方保留精确 Codex 版本选择；自定义配方仍按导入时固定的 Dockerfile、标签和 lock 构建，不改变冻结契约。
- 重新生成六套内嵌上下文；增加两套研究配方的生成、资源、摘要、导入与旧镜像拒绝回归，并实际构建/安装修复后的 wheel 检查资源。范围见[验证说明](docs/VALIDATION.md)。本环境没有 Docker，不把用户报告的 news2douyin 构建成功算作本轮重跑，也不声称 people-search 已完成 Docker 验收。

## 0.54.0（2026-10-26）

- WebUI 以实例为日常管理入口，集中模型、Skills、MCP、插件、工作区和任务记录；无需先将实例注册为应用。构建中心继续负责配方、镜像和实例准备，应用接入保留独立入口。
- 配置界面保留当前实例与工作区上下文，显示实际作用范围。此次只整理操作入口；Docker 后端仍按实例与工作区保存配置，未实现跨工作区自动同步、统一能力库自动收集或 Agent 配置格式转换。
- 撤下 Kun 分叉、评测任务集、K4 Skill 工作台和离线实验的常驻入口，并收起 Kun 调试中的分叉/离线实验创建入口。保留后端 API、研究数据、查看组件和已有分支的固定预览；普通轨迹、调试问答与 Kun 调试控制继续可用。
- 更新中英文 README 与 [WebUI 说明](docs/WEBUI.md)，保留三个使命，区分正式使用路径与研究 API/历史资料。K4 参数化工具流程不再与普通 Skills 管理混排。
- RunDesk 版本更新为 0.54.0；Kun、SDK、worker 协议、UHP 协议及 RDCA schema 不变，不迁移或删除实例、工作区、配置及历史数据。
- 本轮实际检查单独记录在 [WebUI 验证](docs/validation/current/webui-054.json)；0.53.0 UHP、0.52.0 及更早验证仍保持原版本范围，不作为本轮重跑结果。

## 0.53.0（2026-10-25）

- UHP 新任务使用持久截止时间，覆盖排队、队列暂停、派发与重启恢复；省略 `timeout_seconds` 时默认 3600 秒，上限 86400 秒。超时按原任务/run 身份协作取消，不撤销既有副作用。
- `metadata.rundesk_progress` 区分排队、运行、等待审批、调试暂停及终态，审批返回对应工作台入口。进度通过 GET 轮询，保留标准 SSE 事件序列。
- 发现与响应显式提供适配器能力和执行语义；`metadata.rundesk_require` 在创建任务前拒绝不支持的必需能力。能力不代表模型模态或账号就绪。
- Codex 与 Kun 都不承诺 `max_step` 工具轮数或 `max_output_tokens` 硬预算，统一写入 `ignored_fields`。旧活动记录中的步数约束会安全停止并标为 `legacy_step_budget_unsupported`，已完成记录保留。`instructions` 为任务文本，会保留在原生会话历史，不具有原生 system 优先级。
- 新增 Python 标准库 UHP 客户端与 HTTP 测试，涵盖同键同体重试、进度、续接、取消、文件与 SSE。官方套件脚本固定至 `2026.10.19.post3` 原始提交；验证范围见 [验证记录](docs/VALIDATION.md)。
- 修复已停止任务的异步启动晚于下一轮准入时仍执行旧输入的问题；启动前复核 run 身份、取消与截止时间。补齐 OpenAPI 生成器遗漏的观测与轨迹定义，重新生成不再丢失既有契约。
- 修复 HTTP/1 观测代理在上游提前返回 SSE 时，请求体自动读取与转发竞争造成的截断；新增可确定复现的双向读写回归。
- 保留中英文 README 的三个使命与整理后的文档导航；升级说明同步新的默认超时与旧步数预算行为。

## 0.52.0 文档整理（2026-10-25）

- 重写中英文 README，明确 Agent 落地调试、APP 与 Harness 连接、Harness 研究实验三个使命及对应用户。
- 按三类用户重组文档导航，同步 UHP、运行预设、插件、独立 Kun 与轨迹阅读器的当前说明。
- 合并 K1 阶段文档，清除已完成的计划和旧“当前版本”描述；历史测试证据集中归档，保留原始失败与跳过记录。
- 删除已合入源码的四份补丁，更新发行包校验清单。运行时代码、API、依赖与版本号不变。详见[整理记录](docs/DOCUMENTATION-CLEANUP.md)。

## 0.52.0 重构源码补充

本段补记随源码包已有、未单独提升运行版本号的实现：

- 独立运行模块、显式宿主服务、资源与权限契约、任务资源视图及原件引用/显式快照。
- 与 RDCA 共用任务核心的 UHP `/v1` 服务入口，目标协议 `2026-10-19`，有明确能力及语义限制。
- Codex、Kun 与保留代理证据的统一观测模型，以及共享固定历史范围的对话式轨迹阅读器。
- 各阶段原验证结果见[验证说明](docs/VALIDATION.md)，不作为本次文档整理新执行的测试。

## 0.52.0

- 新增跨应用常驻构建中心，列出六套内置配方、自定义配方、全局构建、镜像与检查结果；任务自由组合资源，不新增工作流层级。
- 导入动态 Docker context 与 manifest，冻结摘要和原始 ZIP；复用原构建队列，支持日志、取消和按历史输入重建。
- 增加按固定 Image ID 的检查与非强制实际删除、引用保护、专用 Codex Docker 实例启动、离线/声明 smoke 测试；保留项目、HOME 和历史。
- 提供原生 Codex build-tools 插件与 add-app-docker-image Skill、使用现有任务的构建助手及限时任务范围 MCP。
- 支持原生插件包只读检查、配置和有效 Skills/MCP 来源展示；不调用尚未稳定的原生安装/卸载接口，不宣称 Kun 兼容或插件已安装。
- 保留旧 video-build、内置配方字节、应用运行预设、独立 Kun 及默认关闭的 HTTP 观测。

## 0.51.0

- 保留自研 video-build，新增 HyperFrames、video-use、OpenMontage 三套独立上游配置和 Codex Docker 构建入口。
- 保留固定上游 Skills/原生工程与渲染流程，增加统一 Video App 生成 MCP 接入及精确源码、依赖、许可记录。
- 接入初始化构建目录、镜像身份与离线检查；生成配置和密钥在运行时提供，不打包到镜像。
- 配套 App 0.26.0 独立文本 TTS，不以切换环境代替自研工程转换。

## 0.50.0

- Add administrator-approved named runtime presets for one application. Each preset binds a dedicated Codex Docker instance and workspace; names and enabled state may change, while the execution identity remains fixed. Registration does not start containers, install tools or replace the application's default image.
- Extend connect, runtime preparation, diagnostics, sessions, queued tasks and schedules with explicit preset selection. Disabled presets retain history and reject new execution; cancellation and uncertain submissions keep their original session, binding and receipt.
- Freeze application-key preset bindings and match member grants by application, optional preset and workspace. New presets never expand old credentials; legacy default-workspace grants do not authorize dedicated preset workspaces.
- Add management cards for creating/binding presets, scoped configuration and task history. Creation retries reuse accepted resources; renaming or disabling a preset does not interrupt running environments.
- Pair with Video App 0.25.0: project creation freezes the selected production profile, and connection records the exact runtime binding. Changing the App startup default does not migrate existing projects or sessions; tool profiles and rendering backends remain separate.
- Keep Kun as independent 0.17.0, worker protocol 16, RDCA `/api/v1`, native Codex and default-off HTTP observation. Historical release validation remains intact; real Docker, model authentication and autonomous production remain deployment acceptance work.
- See [application runtime presets](docs/APPLICATION-RUNTIME-PRESETS.md) and [0.50.0 validation scope](docs/validation/archive/validation-0.50.0.json).

## 0.49.5

- Update video-build to 1.4.0: HyperFrames 0.8.87, GSAP 3.15.0, Node.js, FFmpeg and fonts are the default animation toolchain. Pinned MIT video-use and adapter contract 2 remain an explicit optional compatibility path.
- Ship original Codex-directed production guidance for Video App 0.23: autonomous storyboarding, asset selection/generation, TTS, HTML/CSS/JavaScript animation and actual output review. Stage artifacts do not add repeated approval gates.
- Keep App MCP authoring/rendering separate from the managed tool image; the HyperFrames main path requires no shared production directory. Old video_use exchange and archived EDLs retain their own prerequisites.
- Add offline HyperFrames media checks and explicit profile dependency reporting. Installation, PNG transport and local smoke evidence do not prove real model production or visual understanding.
- Retain the independent Kun 0.17.0 boundary, worker protocol 16, native Codex adapter and default-off HTTP observation. RunDesk Control API (RDCA) retains /api/v1; UHP compatibility remains planned and unimplemented.
- Preserve validation scope by release; earlier 0.49.4 checks are historical evidence, not 0.49.5 full race acceptance.

## 0.49.4

- Extract Kun 0.17.0 into an independently buildable project. RunDesk no longer contains or builds `cmd/kun` or the Kun execution kernel; it links a checked lightweight SDK snapshot under `third_party/kun-sdk`.
- Keep JSONL protocol 16 core methods, negotiate the selected engine version and actual capabilities, and report missing/incompatible workers explicitly without falling back to Codex.
- Move diagnostic evidence access to the existing host-provided `__trace_mcp` read-only bridge. Kun no longer knows the RunDesk trace database schema. Old host/core compatibility and the diagnostic provider requirement are documented separately.
- Preserve execution-directory layout and explicit 0.16.0 checkpoint/fork compatibility in Kun 0.17.0. No manifest editing or blanket migration is performed.
- Retain PiG/Pi acknowledgements, source notices and original MIT license texts. This release adds no new PiG modules, provider, HTTP service or multimodal capability.

## 0.49.3

- 新增应用范围的runtime/diagnostics：仅探测已选且运行中的Codex Docker容器，核对绑定与实际环境，不自动启动、重建、换镜像或回退宿主机。
- 固定挑战验证专用共享工作区双向读写；通过当前MCP配置取得nonce绑定的真实PNG并校验载荷，配置不可读或未探测时明确记录，不把工具发现当作图像传输或模型视觉证明。
- video-build升级1.3.0、adapter协议2，EDL镜头可选精确原文textRefs，绑定冻结UTF-8原文和指纹；旧无引用EDL保留兼容。
- 适配器验证与报告保存引用范围、覆盖计数和未映射镜头；保持固定MIT video-use版本、连续外部配音与独立归档，内容不强制分句或分场景。
- 使用新增镜像资源需重建、选择并更新应用执行环境；保留原工作区、HOME与认证。真实Docker与模型制作仍需部署验收。
- Kun0.16.0、worker协议16及Codex HTTP观测默认关闭的行为保持不变。

## 0.49.2

- video-build1.2.0打包固定MIT video-use源码、准确Python依赖锁、连续配音脚本素材适配器、离线烟测及三阶段Skill。
- 内置镜像按profile独立版本判断，video-build核对上游commit/adapter标签；保留现有工作区与认证。
- 与Video App0.21通过现有应用工作目录交换冻结输入及可校验产物，无需新增任意目录挂载权限。
- Kun及workerprotocol不变；Codex HTTP观测默认关闭的设置保留。

## RunDesk 0.49.1

- Default Codex HTTP observation to off, persist `observeHTTP` per session, and expose its toggle in the unified debugging workbench.
- Make `--observe-http` a creation default for new Codex sessions. Restarting with a different flag never overwrites saved session preferences.
- Start the pass-through proxy lazily for an enabled thread. Disabled sessions skip observation configuration discovery and endpoint routing.
- Save changes during active work without canceling requests; reconnect and resume the same Codex thread on the next submitted turn. Report requested settings, pending changes and applied state separately.
- Keep Kun's native model evidence unchanged and reject enabling the Codex-only capture setting for Kun sessions. Existing captured history remains readable.

## RunDesk 0.49.0

- Unify trace inspection, LLM request/response evidence, summaries, diagnostic questions and embedded Kun DevTools in one debugging workbench with a shared session and fixed history scope.
- Add an in-process pass-through HTTP observation proxy for supported Codex endpoint configuration. Forward original payloads and streams without API conversion, model replacement, additional retries or failover; record bounded copies and explicit capture-loss markers.
- Add indexed read-only `llm_requests` / `llm_request` queries, exact byte pagination, chunk timing metadata, and matching trace/debug MCP tools. Keep model capture bodies out of ordinary conversation event polling while preserving database evidence.
- Preserve unknown Codex RPC fields and JSON integer precision, and load all source events for full tool argument/result inspection. Keep incomplete association and evidence explicit.
- Keep Kun 0.16.0 / worker protocol 16 unchanged. Proxy capture defaults to enabled on loopback; Docker needs an explicitly reachable private endpoint. Built-in provider authentication paths remain subject to the documented capture coverage limits.

## RunDesk 0.48.1

- Add Video App 0.7.0 deployment guide and a uniform Docker lifecycle entry point under video-build.
- Document separate renderer/App and managed Codex images, persistent storage, HTTP MCP callbacks and initialization-time image preparation.
- Generalize the runtime binding error for research and video applications. Existing API, runtime image format, Kun and worker protocol are unchanged.
- Video App performs Docker/account/MCP readiness checks before new submissions; recovery keeps its original operation receipt.


## RunDesk 0.47.1

- Preserve and validate native Codex `default_tools_approval_mode`; advertise the configuration capability.
- Distinguish empty MCP tool approval from real data-entry forms, so it no longer asks for meaningless JSON.
- Support news2douyin 0.13.1 explicit Docker full-access setup and durable MCP approval configuration.
- Keep Docker mounts, container isolation, Kun 0.16.0 and worker protocol 16 unchanged. Kun rejects the unsupported server-wide Codex setting instead of silently ignoring it.

## RunDesk 0.47.0

- Add scoped application runtime status and prepare APIs, with expected-binding and environment-update guards.
- Reuse/start the configured Codex Docker environment without disconnecting active sessions or changing images.
- Expose sanitized account readiness and MCP tool discovery; do not run a model during preparation.
- Integrate with news2douyin 0.13.0 one-click deep research, container business MCP setup, search evidence records and guarded session binding.
- Add race, permission and concurrent reuse tests; validate the two live HTTP services with Docker/Codex/search fixtures.
- Keep Kun 0.16.0 and worker protocol 16 unchanged. Real container/model/search deployment still needs acceptance.

## RunDesk 0.46.0

- Add three example runtime profiles: news2douyin, people-search, video-build.
- Add a stdlib Python CLI and uniform per-directory entry points for inspect, validate, prepare, pack, build and doctor.
- Generate standalone contexts with fixed Codex versions, profile manifests and SHA256SUMS; compatible with the existing ZIP build UI.
- Add optional Wigolo 0.2.1 stdio MCP integration and Agent-Reach at a pinned user-selected fork commit.
- Include task Skills, source-evidence capture and a real FFmpeg media smoke script.
- Keep server behavior, Kun 0.16.0 and worker protocol 16 unchanged; Docker deployment and live search require environment acceptance.
