# 升级到当前版本

## 0.55.0：外置配方与构建源（2026-10-27）

- 保留原数据目录，停止旧服务后更新 RunDesk 程序。构建资源现在是外置文件：部署时同时放置本版 `recipes/` 和 `config/`，不再只复制单个二进制。可将 `RUNDESK_RECIPES_DIR` 指向自己的配方目录。
- 配方目录查找优先级为显式 `RUNDESK_RECIPES_DIR`、可执行文件旁的 `recipes/`、从当前工作目录向上查找的 `recipes/`。显式指定的目录缺失或无效会直接报错，不回退到另一份配方。修改 `recipes/<profile>/` 中的 Dockerfile、依赖或 vendor 后，从配方新建构建任务即可读取，无需重编译或重启。
- 随包 `config/build-sources.json` 提供默认源列表；首次使用复制到 `<data>/config/build-sources.json`，随后由“下载源设置”或直接编辑该文件维护。`RUNDESK_BUILD_SOURCES` 可指定独立文件。更新发行包不应覆盖你已维护的源列表或配方，合并新版默认条目时先保留自己的版本。
- 构建前可选国际/中国镜像分组，再逐项修改地址。切换组只填入条目，不保证某地区或网络一定可达；Docker 镜像拉取和容器内包下载分属不同层。
- 已有自定义配方和历史 `context.zip` 继续保留。按原配置重建使用已冻结输入与源参数；需要新版文件或新源时，从配方新建构建任务。旧记录没有源快照时保持原行为，不补填今天的默认值。
- 新镜像构建并检查通过后，再显式更新所需实例/应用；原镜像、HOME、认证、Skills/MCP、工作区与任务记录不删除。独立 Kun 0.17.0、SDK、worker 协议 16、UHP 协议保持原配套。

具体操作与边界见[构建源配置](BUILD-SOURCES.md)和[构建中心](BUILD-CENTER.md)。下方版本段保留当时的升级要求。

## 0.54.1：研究预置构建修复（2026-10-26）

- 源码构建者需重新编译 RunDesk，再替换程序并重启服务；内置 ZIP 编译在二进制中，单独替换 examples 不会更新已运行程序。保留原数据目录。
- 在构建中心选择内置 news2douyin 或 people-search，确认配方为 **1.1.2**，填写精确 Codex 版本（例如本次用户已测试的 `0.157.1`），**从配方新建构建任务**。历史任务“重建”使用冻结的旧 ZIP，不能获得本次修复。
- Agent-Reach 现为修复后的本地源码，构建不再下载其 GitHub archive；apt、Python/npm 依赖及构建后端仍需要相应下载通道。
- 自定义配方继续固定导入时的 Codex 版本；用 `pack --codex-version 0.157.1` 生成的 Dockerfile 默认值、标签与 profile.lock.json 保持一致，不需传版本参数。修复已有自定义配方时，生成并导入新的 ZIP，旧快照不修改。
- 构建并检查新镜像后，在对应实例/应用中显式选择它；不自动替换运行中的镜像，也不删除原镜像、HOME、认证、工作区或历史。
- RunDesk 为 0.54.1；独立 Kun 0.17.0、SDK、worker 协议 16、UHP 协议和 RDCA schema 不变。实际验证边界见[验证说明](VALIDATION.md)。

## 0.54.0：实例中心 WebUI（2026-10-26）

保留原数据目录替换 RunDesk 程序，并刷新浏览器。RunDesk 更新为 0.54.0；独立 Kun 0.17.0、轻量 SDK、worker 协议 16、UHP `2026-10-19` 与 RDCA schema 均不变。

- 实例成为日常管理入口；模型、Skills、MCP、插件、工作区与任务记录从实例上下文访问。已有应用和命名运行预设保持原绑定，不要求重新注册。
- Kun 分叉、评测任务集、K4 Skill 工作台和离线实验撤下常驻/创建入口。对应 API、记录及查看组件保留；已有分支会话继续提供固定预览查看。
- 本版不变更存储布局，不迁移或删除实例、工作区、容器 HOME、认证、Skills/MCP 配置、会话、制品或研究数据，也不自动重建镜像和容器。
- Docker 配置仍按实例与工作区分别保存；统一页面入口不代表跨工作区自动同步。自动收集能力库及 Agent 格式转换尚未实现。
- 日常轨迹、诊断、Kun 调试控制、RDCA 与 UHP 继续沿用现有契约。0.53.0 的默认任务时限及硬预算边界保持不变，旧版本升级注意事项见下一节。

当前界面说明见 [WebUI](WEBUI.md)，本轮实际检查见 [验证范围](VALIDATION.md)。历史研究文档中的顶部按钮和创建流程不再代表当前导航。

## 0.53.0：UHP 应用接入加固（历史）

升级前停止相关执行并备份数据目录。RunDesk 版本更新为 0.53.0；独立 Kun 0.17.0、轻量 SDK、worker 协议 16 和 UHP `2026-10-19` 不变。RDCA v1 与 UHP 的路径及已有对象归属保持原契约。

- **硬预算边界改为显式能力**：Codex/Kun 均不提供 UHP 工具轮次或输出 token 硬限制。合法 `max_step`、`max_output_tokens` 默认列入 `metadata.ignored_fields`；Codex 不再仅因合法 `max_step` 而拒绝请求，Kun 也不再用模型完成次数近似工具轮次。依赖硬限制的客户端须提交 `metadata.rundesk_require`，目前要求这些能力会返回 `422 rundesk_capability_unsupported`，不创建任务或物化本次附件。要求格式非法返回 400。
- **默认任务时限**：新 UHP 任务省略 `timeout_seconds` 时为 3600 秒，显式范围 1–86400 秒，包含排队时间；0 不是关闭时限。绝对截止时间随公共任务持久保存，并由队列、派发检查和 Manager 计时器执行，重启后恢复。到期最终映射为 `incomplete` / `timeout`，协作取消不回滚外部动作。APP 应读取 `metadata.rundesk_deadline`，按业务时长设置预算。
- **旧活动步数记录**：旧 UHP 记录保存了正数 `MaxStep` 且任务仍活动时，会停止执行并最终返回 `incomplete`，原因 `legacy_step_budget_unsupported`，避免继续假设旧步数承诺有效。旧终态记录保留原结果，不重新解释或执行。旧任务没有新的公共 deadline 字段时，不会凭空增加一小时预算；已有 UHP 截止时间仍由原响应刷新路径检查。
- **指令与进度**：额外指令继续以 `task_text` 进入 `session_history`，不提供原生优先级或单次任务后的自动清除；要求 `native_instructions` 会被拒绝。新增 `metadata.rundesk_progress` 供轮询，待审批时返回工作台相对地址；沿用已有访问控制，不提供审批令牌或新控制接口。标准 SSE 不增加私有进度事件，初始与最终快照包含进度元数据。
- **客户端恢复**：保留原凭据、operation key 和确切请求，未知提交结果用原身份/键重试。已有幂等回执仍指向首次执行，不因升级或修改请求体而创建新任务；不能为了刷新能力或预算而换键重试未知结果。示例见 [UHP 客户端](../examples/uhp_client.py)。
- **验证基线**：官方套件更新至 `2026.10.19.post3`，固定提交 `038d4d27afed078db339b107f8bf6031ebb37872`；旧 post2 报告保持历史身份。Extended 与应用内预算专项分别报告，不能当作 Full 的 T-12 已通过。真实 Kun、Docker 和模型链路仍需目标环境验收，见 [UHP](UHP.md) 与 [验证范围](VALIDATION.md)。

能力声明依据是 RunDesk 适配器契约，不代替模型模态、账号或供应商就绪检查。以上为新提交与明确说明的旧活动记录处理变化，不自动修改应用预设、Skills、插件、MCP 或模型配置。

## 0.52.0：重构源码与文档整理（历史）

上一阶段在 0.52.0 基础上加入资源/运行模块重构、UHP 服务、统一观测模型及轨迹阅读器，当时运行版本号保持 0.52.0。其配套为独立 Kun 0.17.0 和 worker 协议 16。

- 新个人产物默认登记原件引用，需要保留独立版本时显式保存快照；旧托管副本仍可读。见[资源契约](RESOURCE-CONTRACTS.md)与[文件库](PERSONAL-FILES.md)。
- RDCA 继续使用 `/api/v1`，新增 UHP `/v1` 不迁移其他服务的历史身份或会话。见 [UHP](UHP.md)。
- 统一观测和轨迹阅读器复用已有事件记录；历史范围显式刷新，未采集内容不能补回。见[观测模型](OBSERVATION-MODEL.md)与[阅读器](TRACE-READER.md)。
- 2026-10-25 的文档整理阶段仅修改文档、证据目录与包校验清单，不引入数据迁移或新的运行行为；它早于本页上方的 0.53.0 功能加固，检查报告保留原范围。

## 通用升级步骤

以下通用步骤适用于当前源码；后续版本专题保留各自配套应用与环境说明。

当前版本 RunDesk 0.55.0，配套独立 Kun 0.17.0、worker 协议 16、fork schema 3；视频工具 profile 为 video-build 1.4.0。

1. 等待活动任务结束，停止原服务，备份数据目录、工作区、Codex HOME/配置、独立 Kun 执行日志及服务环境设置。
2. 在 RunDesk 源码执行 `make build`，本包只构建宿主；部署到别处时一并复制 `recipes/`、`config/` 或设置外置路径。Kun 在其独立源码目录执行 `go build -o bin/kun ./cmd/kun`。将 Kun 放在 RunDesk 可执行文件旁，或通过 `--kun /absolute/path/to/kun` 指定。RunDesk 使用轻量 SDK 快照构建，不再包含 `cmd/kun` 或引擎源码。
3. 保留原系统用户、`--data`、`--codex`、`--kun`、CODEX_HOME 与服务参数。启动后用 `rundesk --version` 和 `kun --version` 核对，再刷新浏览器；旧会话继续读取，缺少或不兼容 Kun 明确报错，不自动切到 Codex。
4. 本版不会自动安装 Codex、切换后端、启用 Bash 或开启 HTTP 观测。更换引擎后新建会话，历史会话保持原归属。HTTP 观测保留已有会话开关和默认关闭语义。

0.49.4 已将 Kun 拆分为独立工程；从更早版本升级要分别部署新宿主与独立 Kun。Kun 0.17.0 对既有 0.16.0 执行目录、checkpoint/fork 的兼容范围见 [拆分说明](KUN-SEPARATION.md)，不手工改 manifest 或绕过版本守卫。需要重新生成的预览或独立验证按当前内核重做，历史只读证据保留。

重启不会重放已开始或结果未确认的任务；排队任务保留。先检查原回执、任务状态与副作用证据再决定后续操作。一个可靠本地文件系统的数据目录只运行一个 RunDesk；不要删除运行中的 server.lock 来并行启动服务。

验证记录按实际版本与执行范围保存；0.49.4 的普通全量测试和局部 race 结果不是 0.49.5 全量 race 验收。当前能力与本版实际测试见 [STATUS](STATUS.md) 和 [VALIDATION](VALIDATION.md)。

## 0.52.0：构建中心与原生插件准备

保留原数据目录升级并刷新浏览器。构建中心读取原 setup 和应用构建记录，原有应用/实例、工作区、HOME、配方及制作入口不自动迁移。自定义配方保存在数据目录，无需重编译；删除构建记录不删除 Docker 镜像，删除镜像不删除项目或 HOME。旧历史若没有冻结 context.zip，重建会明确失败，应重新导入原输入。

“让 Codex 添加”需要已经可用的 Codex 实例和模型认证，使用新专用工作区。Docker 助手需要配置容器可达的 `RUNDESK_BUILD_MCP_ORIGIN` 或 `--public-url`，不得把容器 localhost 当作宿主。构建权限限当前任务并到期失效；不传 Docker socket 或宿主管理员令牌。

插件页和 ZIP 检查均只读。本版不调用尚未稳定的 plugin/list/read/install/uninstall，也不把展开 Skill 当作原生插件已安装。Skill bootstrap 会在回执明确标注。详见[构建中心](BUILD-CENTER.md)与[原生插件](NATIVE-PLUGINS.md)。

## 0.51.0：三个独立上游视频配置

自研 `examples/video-build` 及已有实例不迁移、不切换。新增 hyperframes、video-use、openmontage 三套独立配置，构建新镜像后建立专用实例和工作区，运行时配置其 `video_app_media` MCP。完整上游代码或必要 Skill 快照按各自固定来源保留。OpenMontage 的完整源码在镜像构建时下载并验证摘要，prepare/pack 不代表镜像已构建。

调用任意文本 TTS 需升级 Video App 至 0.26.0；已有配音、生成服务、项目和凭据保留。三个上游从 RunDesk 独立会话运行，App 自研导演入口保持原流程。详见 [独立视频环境](UPSTREAM-VIDEO-ENVIRONMENTS.md)。

## 0.50.0：应用多运行环境预设

原应用的默认实例/工作区、会话、HOME、认证和凭据保持原绑定，不自动创建或迁移预设。管理员在应用配置内登记命名预设，为每个预设配置独立 Codex Docker 实例和工作区。省略 presetId 的客户端保持原行为；旧应用凭据不会自动获得新预设权限，需要重新签发包含 presetIds 的凭据。

配套 Video App 0.25.0 可用 --production-profile 或 VIDEO_PRODUCTION_PROFILE 指定新项目默认预设，项目会保存选择与会话绑定；改变启动默认不迁移旧项目。App 自身渲染容器、IndexTTS与ComfyUI独立运行。预设名称不自动安装OpenMontage/video-use或转换既有剪辑工程；管理员需提供实际可用镜像和适配工具。

具体配置、禁用规则和权限见 [多运行环境预设](APPLICATION-RUNTIME-PRESETS.md)。该版本验证见 [0.50.0 报告](validation/archive/validation-0.50.0.json)，旧日志保留原版本边界。

## 0.49.5：HyperFrames 自主导演工具镜像

配套 Video App 0.23.0、video-build 1.4.0。新工具镜像默认提供 HyperFrames 0.8.87、GSAP 3.15.0、Node.js、FFmpeg 和字体，原创 Skill 支持 Codex 自主分镜、选材、生成、动画和检查。App 渲染镜像与 RunDesk 工具镜像分别更新；新的主流程通过 App MCP 保存源工程与素材 ID，无需共享 App 私人数据或额外生产目录。

重新构建或选择真实已有的 video-build 1.4.0 镜像，在 RunDesk 显式选择并更新/重建应用运行环境；保留原工作区、HOME、Codex 认证和 MCP 配置。仅替换源码包、App 或镜像标签不会更新已运行容器，不会自动迁移旧工程后端。

video-use 改为可选。已有 video_use EDL 工作流继续使用 adapter contract 2、固定上游 MIT 资源及原专用共享目录；需要它时按 profile 配方显式使用 `--with-video-use` 构建。默认 HyperFrames-only 镜像没有这项可选依赖，不能用于误判旧视频工具已齐备。实际构建选项和检查见 [video-build](../examples/video-build/README.md)。

环境诊断和离线媒体烟测分别验证工具、路径/权限、图片传输或本地短动画，不会启动模型，也不证明模型视觉、IndexTTS 音质或真实新闻制作通过。真实部署、生成服务与自主创作仍需用材料验收；不将历史日志当成本轮实际测试。

---
以下保留历史版本升级说明。

## 0.47.0 升级到 0.47.1

先停止研究并保留原数据目录，替换 RunDesk/Kun 程序；Kun 内核版本不变。升级 news2douyin 到 0.13.1，在研究设置选择 Docker 完整访问并“连接并配置环境”，为既有环境补齐运行权限及持久 MCP 审批。仅更新程序不会自动放宽旧实例权限，也不会自动接受已弹出的审批。现有镜像中的 Codex 若支持服务级审批配置，无需仅为此次设置修改重建镜像；不支持时构建明确版本后显式更新环境。

## 0.48.0：Exa 与初始化镜像准备

替换 RunDesk 二进制；Kun 0.16.0 不变。停止旧服务并使用相同数据目录启动，打开初始化向导准备 `news2douyin` 镜像。镜像工具层已改变，旧镜像不会被原地补齐；首次请构建或拉取新配方 1.1.0 的镜像。

配套 news2douyin 升级到 0.13.2。新研究设置从 Wigolo 迁移为 Exa；“连接并配置环境”会启用 Exa 并停用旧 Wigolo，同时保留其他 MCP、自定义 Exa 地址和凭据。正在执行的旧任务仍保留历史后端快照，应先结束后再切换环境。若已有环境绑定另一镜像，需在 RunDesk 显式更新，不会通过重连悄悄替换。

首次无需公共仓库账户即可本机构建；公开镜像仍需由发布者实际构建、验收并推送，本源码包未发布公共镜像。

## 0.49.0：统一调试与直通观测

替换 RunDesk 程序并刷新浏览器，原来独立的轨迹、只读检查和 Kun DevTools 入口统一为“调试工作台”。历史数据继续读取，Kun 版本和协议不变；旧版本未保存的模型 HTTP 正文不会补回。新观测以附加事件写入现有数据库，并增加事件索引。

0.49.0 的代理默认启用并监听随机回环端口；0.49.1 的默认值与会话开关见下一节。支持的 Codex provider 使用线程级地址覆盖，不永久修改其模型配置。内置 provider 使用原生 `openai_base_url`，明确认证路由后才推导默认上游；无法确认时保留原连接并标记缺口。要关闭新增正文采集，启动时传 `--observe-http=false`。正文副本可能包含提示词、业务资料和工具参数，应纳入原数据目录的访问与保留策略。

Docker 场景显式配置 `--observe-listen` 与 `--observe-advertise`，确认容器到宿主的私有网络可达；没有配置时保留原模型连接并标明采集缺口。不要把“configured”当作真实模型链路已验证。完成一轮后检查请求、响应、采集标记，再判断该认证/网络路径是否满足调试要求。详细操作见 [统一调试与采集](UNIFIED_DEBUGGING.md)。

## 0.49.1：默认关闭与会话采集设置

HTTP 采集默认改为关闭，代理按需启动。`--observe-http=true` 仅设置以后新建 Codex 会话的默认值；它不覆盖已有会话保存的 `observeHTTP`，重启也不改变保存值。0.49.0 的旧会话没有该字段时按关闭处理；其历史观测仍可查询。

需要采集时，在当前 Codex 会话的调试工作台开启，或 PATCH 会话的 `observeHTTP`。运行中可以保存偏好，当前请求保持原设置继续；空闲修改也在下一轮提交生效。下一轮自动重连并恢复原 Codex thread，应用新设置。`observation.pending` 表示偏好待应用，`appliedEnabled` 表示原生线程已确认配置代理；即使保存为开启，不支持采集时后者仍为 false。等待下一轮不代表正在采集。

关闭的会话不进行采集配置发现、不设置代理地址。服务启动后，在首次处理已启用线程之前，仅指定监听参数不会创建代理监听器。Kun 不允许启用这个 Codex 专用开关，其原生调试证据继续使用既有路径。无需迁移旧证据或重建数据库；Kun 版本与 worker 协议不变。
