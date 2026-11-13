# WebUI：工作区、会话与制作环境 / Workspaces, conversations and production environments

RunDesk 0.74.0 的主界面按工作区组织普通对话。日常对话使用默认 Codex，应用通过获准预设选择制作环境。镜像及制作环境由构建中心维护，内部 Kun 维护从环境设置或构建失败记录进入。

0.67.0 整理同主题文档、测试及配方共享输入，沿用当前界面。内置配方从 `recipes/` 读取，所声明的共享文件来自 `recipes/common/`；编辑只影响下一次新建构建，历史快照与当前环境不变。此版本没有增加运行时全局 HTTP 采集设置，也没有改变构建目录的镜像归组方式。

## 技能中心

管理员侧栏的“技能中心”提供“搜索技能”和“已获取技能”两个入口。默认搜索三个市场，可勾选 GitHub；失败来源保留单独状态，不丢弃其他来源结果。也可直接粘贴支持的 GitHub 或 ClawHub 地址。获取后先显示固定目录和内置检查，打开文件只作纯文本预览。

技能详情保留 Go 与 NVIDIA 两类报告，可以切换查看引擎、版本和覆盖情况；如已有 NVIDIA 报告，重进页面优先显示它。刷新页面后恢复仍在运行的检查与取消入口。

技能详情可以重新快速检查或发起可用的 SkillSpector 扫描；检查运行时不能安装。扫描器未配置、失败、取消和检查不完整分别显示，不会变成安全通过。安装前选择当前工作区及范围，若有待复核内容，查看报告、勾选继续并填写理由。同名替换需明确选择，沿用完整目录备份。旧 ZIP／目录上传同样显示复核步骤。操作不清空当前会话的草稿；详细约束见[技能中心](SKILL-CENTER.md)。

## Skill-Up 效果测试

技能详情下方的“效果测试”与安全报告分别展示。用例入口固定为该版本的 `evals/eval.yaml`。选择已准备的测试环境后可校验或实际运行；执行需明确确认，网络默认关闭。没有 Docker 或用例时显示未就绪，不会在宿主启动 Agent。任务刷新后可继续查看或取消；容器清理未确认时会阻止新任务并提供重试清理。

报告显示每条用例的 PASS、FAIL、ERROR、SKIP／未知状态和变体，保留失败原因。基线可以来自另一个 Skill 快照，候选使用当前快照；用例、模型、配置或环境身份变化会显示不可比／不完整。外部 JSON 报告需显式确认导入，带有未核验来源标记，不能替代亲跑证据或安全结论。详见[效果测试指南](SKILL-CHECKS.md)。

## 初始化首页

首次非 Demo 安装先进入全屏初始化页，工作台在初始化结束或明确稍后配置之后显示。页面只有三步：**寻找和配置 LLM、配置和安装 Codex、准备 Docker 环境（可选）**。环境发现和 Kun 模型配置放在第一步；第二步可直接点击“完成基本设置，稍后准备 Docker”；第三步保留构建、镜像下载、经验与完整概览。可以自由返回或切换步骤，浏览器地址记录当前步骤。已完成安装在没有具体路由时打开普通工作区的 Codex 对话，可恢复已有会话或显示新草稿；左侧“环境设置”可重新打开向导。

环境扫描展示运行 RunDesk 的服务器信息及本机模型服务，不代表浏览器所在电脑。选择模型后主动测试推理和自动函数调用，再保存专用 Kun 维护助手。Codex 安装和 Docker 构建按明确选择提交后台任务，刷新后可继续查看状态和日志。失败可以重试，或交给已就绪的 Kun 生成独立修复任务，轨迹使用既有任务/会话界面。

第三步把“下载现成镜像”和“从配方本机构建”做成说明用途的选择卡片，默认下载；本机已有镜像检查作为另一种选择。只显示当前方式的表单，切换时保留已填写的草稿。Docker 检查使用紧凑状态行，暂时不用时仍可直接完成初始化。公开目录为空时明确显示未发布，可填写真实地址或选择其他准备方式；不会将示例地址当成可下载镜像。公开目录支持版本、平台与来源选择，保存目录使用 revision 防止覆盖他人的修改。

“准备进度”中，每个任务显示实际采用的方式。下载失败直接调整来源或重试，不自动改成本机构建，也不提交 Kun 构建修复。本机构建失败后，原卡片内显示自动交接状态或手动修复操作，并连接对应 Kun 对话、修复候选、日志与检查结果；Docker 页不再要求到另一个“Kun 修复任务”区域寻找关联。重新打开向导会按原构建编号读取持久修复记录。初次失败仍保留，修复任务结束也不直接变成“镜像就绪”：只有明确绑定的候选通过 offline/smoke 并形成已验证构建记录，才展示验证成功。未知或暂时读取不到的状态保留待确认提示。缺失原始构建时保留关联卡片，但不计入准备成功；独立镜像检查和未确认完整验证的 Kun 候选也不单独计入。

第三步继续列出已准备或跳过的组件及版本化经验，保存设置后进入工作台。扫描到工具、下载成功、Agent 说已解决，都不能替代相应的执行验证。初始化不改已有实例后端或应用镜像绑定。具体配置、期限和安装边界见[部署说明](SETUP.md)。

本机构建显示两个独立选择：下载网络与 Codex 来源。中国／国际分组预填下载地址，仍可逐项编辑；Codex 可自动复用匹配的受管 Linux 安装、明确要求原生包、指定已有兼容基础镜像或使用 npm。候选来源检查显示实际服务器记录，执行前仍会核对摘要和运行能力。源编辑器可把新版默认列表中的缺失项加入草稿，保留现有自定义值并通过 revision 保存。

## 实例、工作区和构建预设

| 对象 | 职责 |
| --- | --- |
| 构建预设 | 指定 Agent、系统依赖、内置能力和初始配置 |
| Docker 镜像 | 构建结果，可以用于创建多个实例 |
| Agent（存储标识 instance） | 长期管理的身份，关联运行方式、镜像和配置入口 |
| 工作区 | 项目文件、项目规则及制品所在的目录 |
| 运行环境 | 某个实例在某个工作区使用的容器及持久状态 |

默认实例继续使用本机 Codex。制作环境具有稳定的后台实例 ID；Docker 配置的实际作用范围还包括工作区。模型、Skills、MCP、插件共用原有设置对话框。查看或编辑前应核对环境和工作区。

应用页包括“任务记录”和“应用设置”。设置页展示可用预设、默认环境和启停状态。一个应用可以绑定多个预设，每个任务沿用一个获准环境。预设卡片的“配置能力”在原地打开环境设置。具体见[运行预设](APPLICATION-RUNTIME-PRESETS.md)。

主导航使用紧凑栏，日常对话、应用和构建中心按各自职责进入。桌面／平板主栏为 72px，手机为 62px，保留小字标签与提示。工作区／会话作为可收起、可调整宽度的第二栏，仅在会话页参与布局；管理页面使用释放出的内容空间。收起选择保存在当前浏览器，回到会话时恢复。没有保存偏好时，900px 以下默认收起工作区栏；600px 及以下使用抽屉。抽屉可用 Escape 或背景关闭，并将键盘焦点限制在其中，关闭后回到触发按钮。

## 构建工作台

构建中心默认沿“选择配方 → 准备镜像 → 制作环境”显示当前上下文。选择配方后可查看对应构建、已有镜像与已使用它的 Agent；已有镜像可以直接复用，不要求每次重新构建。配方有多个版本和构建结果，一个镜像也可服务多个 Agent。缺少可靠来源的镜像明确显示来源未知。

镜像卡片将配方名称与版本放在上方，固定 Image ID 和标签放在下方；历史记录优先采用冻结名称与版本，不用后来改名的配方替换历史身份。原配方、构建与测试、镜像及发布记录的深链接仍可进入高级视图，复用原后台队列、测试和请求重试记录。

创建制作环境时的模型设置选择已有配置或本次显式继承的默认 Codex 服务。模型提供方配置不写进镜像；具体作用范围、凭据与检查边界见[Codex 模型服务](CODEX-PROVIDERS.md)。

## 工作区与会话

左侧“会话”按工作区展示历史。普通工作区的新对话使用默认 Codex，无需挑选实例。初始化完成后的空白会话只是草稿，第一次发送才写入会话记录。恢复旧会话时遵守记录中的执行身份，应用项目或预设任务继续使用其获准环境。附件与已选 Skills 按原工作区和执行身份隔离。

对话按可靠的轮次身份组织，completed/succeeded 轮次默认收为用户问题标题和状态，点击展开这一轮的问题、回答和工具步骤。标题取用户输入的第一个非空行，最多 120 个 Unicode 码点；完整输入仍在正文中。手动展开或收起的选择按会话／轮次保存在当前浏览器，后续事件和重新加载继续使用该选择。进行中、等待处理、失败、中断及状态未知的轮次默认展开；即使用户手动收起，摘要仍显示其状态。如果轮次完成时键盘焦点正在正文内，会暂时保持展开，避免隐藏正在阅读的内容。

明确的宿主／原生对应优先；缺少直接运行标识时，只允许由输入及原生开始记录保守重建，并显示关联提示。没有可靠身份或身份冲突的记录单独保留，不据时间邻近关系强行合并。仍受当前最多 200 条显示项的界面边界约束，范围不完整时明确提示并保留轨迹入口。阅读偏好不触发任务、工具或模型调用。

普通工作区支持不同 Docker 实例的会话，各实例/工作区组合保持独立容器、HOME 和认证；工作区文件共享。应用默认环境、预设及应用项目的专用工作区仍按归属限制候选引擎，项目会话保留 appId/projectId/presetId。旧会话与已登记的环境继续按原身份读取。旧 Agent 深链接保留原工作区范围。

回归测试见 `internal/web/tests/workspace-conversations.cjs`（jsdom）与 `internal/web/tests/workspace-conversations-browser.cjs`（Playwright，`RUNDESK_BINARY` 指向当前编译产物）。浏览器测试使用独立临时服务，仅创建空闲会话和运行环境记录，不调用真实模型或启动 Docker。

<a id="codex-effective-configuration"></a>

## Codex 有效配置读取

环境设置中的“运行诊断 → Codex 有效配置”优先显示已有本地快照，显示同步时间及是否过期、同步失败。没有快照时显示未同步提示，点击“立即同步”才主动查询原生配置。读取期间按钮禁用并显示进度，防止重复提交；失败保留已有快照并在原位置提示，不能将旧快照解释为刚刚读取成功。切换 Agent、工作区或离开设置页后，较早的响应不会覆盖新页面的内容。打开页面的快照读取只访问本地文件，不启动配置进程或 Docker；连接诊断和能力扫描保留原有独立操作，不为读取配置提交模型任务。

快照来自 Codex 原生 `config/read` 的有效值、配置层与来源，脱敏后保存到 `<data>/config-snapshots/<instanceId>/<workspaceId>.json`。它是供界面读取的副本；不会用本地 TOML 猜测合并结果，也不会把脱敏占位符写回生效配置。默认本机 Agent 继续使用服务的 `CODEX_HOME` 或 `~/.codex`；托管 Agent 和 Docker 环境的实际配置位置见[部署说明](SETUP.md#configuration-storage)。

后台每五分钟检查已观察到的快照，只通过仍健康、已初始化的配置连接刷新，不遍历并启动所有 Agent／工作区，也不为了刷新快照重启容器。服务或配置连接重启后，已有快照仍可显示；重新同步需要实际配置连接可用。停止的环境可以保留带时间的旧快照，用户可在需要时主动同步。

原 `GET /workspaces/{wid}/config` 仍提供即时原生读取，界面通过可选 `view=snapshot` 使用快照，`refresh=true` 主动同步。MCP 等配置修改前继续读取即时原生配置、检查版本，并立即写入，后台周期不承担双向覆盖。已初始化配置连接上的只读 RPC 保持并行，写入、重载和回收继续互斥。本功能不复制 Skills 内容、插件、登录凭据，也不实现不同工作区的配置自动应用。

配置连接排队、初始化与 RPC 共用 45 秒预算。首次创建或准备 Docker 工作环境仍可能执行受各自时限约束的检查、容器准备与 Seed 同步，所以冷启动的总 HTTP 耗时可能更长。请求超时不被当作成功；连接失效后会在安全边界重新建立，不能把优化理解为上游配置错误、认证或外部服务故障一定已修好。已有日常会话的执行连接保持独立。

<a id="conversation-body"></a>

## 会话正文、运行中补充与附件

对话正文保持安静，品牌只出现在应用外壳。正文为 16px，工具标题 15px，代码和输出 14px。

工具及推理摘要默认折叠，由用户主动展开。事件到达或执行结束不改变用户的展开选择。命令正文展示命令、工作目录、输出及退出码；原始协议字段可在二级折叠中查看。只展示 Codex 提供的公开推理摘要，不推测未返回内容。

消息按会话、轮次、item 标识复用 DOM；输入和错误按事件编号标识。只更新变化的内容，不整页替换。展开项存在时暂停底部追随，避免拉走阅读位置。这里的工具／内容块折叠在切换会话或刷新后重置，不跨刷新保存；它与前文按会话／轮次保存的整轮展开偏好分别管理。对话仍只展示最近 200 项，完整记录在轨迹/事件视图查看。

设计参考：DeepSeek Harness 的 ui-chat 文档中 stable Node seats、tool 节点生命周期与流式更新原则。实现为 RunDesk 自身的原生 JavaScript，没有引入 DSH 代码或 React 依赖。
https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/README.md

开发验证：运行 `scripts/conversation-smoke.cjs`，需要 Playwright 与 Chromium，使用 `PLAYWRIGHT_PATH` 和 `CHROMIUM_PATH` 指定本机依赖。测试使用模拟协议事件，不调用真实模型。

<a id="conversation-1"></a>

### 运行中补充

运行中可按 Enter 或点击“补充”，将要求追加到当前任务；停止仍是独立按钮。启动和停止转换期间暂不可发送。界面展示提交中、已接收或未确认状态；未获确认保留草稿，不自动重发。任务恰好结束时，需确认后再次发送作为新任务。

补充指令不会自动批准待授权操作，也不保证中断已经开始的工具。运行中的请求被 App Server 接收后何时被模型采用，由 Codex 和所用模型/提供方决定。客户端不重写 agent loop。

<a id="conversation-2"></a>

### 长文本附件

默认阈值为 8,000 个 Unicode 字符，包含空白和换行；这是 RunDesk 自己的默认值。粘贴时将粘贴的长文本转为附件，保留输入框其余说明。直接输入的大段文本则在发送时整体转换。

附件卡片显示文件名和字符数，支持预览、移除。点击发送才通过现有上传接口上传 UTF-8 文本；图片和手动文件附件照常使用。输入框可以留空，系统会附上一条简短的阅读附件请求。运行中使用同一附件通道调用 turn/steer。

原文完整保留，不做摘要或裁剪。单附件最大 32 MiB，超出时报错。预览最多显示前 20,000 个 UTF-16 单元，长内容明确标注预览截断；实际文件完整。待发送附件暂存在浏览器内存，刷新或切换会话会清空；已发送的附件路径保存在事件记录里。文件化减少消息区占用，但不保证减少模型最终读取的 token 数。

## 轨迹耗时

从会话原有“轨迹”入口进入步骤和耗时分析，可查看最多十个最慢运行步骤，并在完整筛选范围中按耗时排序后分页。只有宿主提交到终态的有效区间标为“本轮总耗时”；原生开始后的区间与 Kun worker 区间分别标识，不能冒充包含启动前等待的完整耗时。界面显示 `clock`、`startMethod/endMethod` 依据；缺失记录显示未知。已知步骤的耗时合计只是工作量视图，可能包含重叠或嵌套，不能代替整轮经过时间。模型运行步骤和模型通信观测分别保留，避免两份记录被当成两次执行。所有筛选、统计和分页共享所选轮次及固定 `through`，读取最新记录才推进范围。详见[轨迹](TRACE.md#trace-overview)。

## 当前配置边界

本轮调整统一了操作入口，**尚未实现实例配置自动同步到所有工作区**。当前 Docker 运行环境仍按“实例 × 工作区”保存独立的 `CODEX_HOME`、`HOME` 和状态；在其中一个工作区添加的 Skills 或 MCP 配置，不会自动复制到另一个工作区。项目目录中的 Skills 也只属于该项目。配置页必须显示实际作用范围，不能将入口集中解释为配置已经全局共享。

镜像内置能力、环境追加配置和项目提供的能力仍来自不同位置。原生插件安装/卸载的支持范围见[插件能力](NATIVE-PLUGINS.md)；资源被观察到或包检查成功，不等于已经安装可用。

后续的实例配置版本、跨工作区应用、自动收集可复用资源以及 Agent 配置格式适配，需要独立实现和验证。本版不把现有能力页面声明为已经完成的全局资源库。

## 研究界面收敛

以下四个研究工具暂时撤出日常工具栏和创建入口，减少与实例配置、普通任务及 Skills 管理的混淆：

| 工具 | 原有用途 |
| --- | --- |
| Kun 分叉 | 从已停止运行的安全边界创建 Hybrid 回放或 Live 执行分支 |
| 评测任务集 | 固定输入、验收规则与案例执行定义 |
| K4 Skill 工作台 | 提炼参数化工具轨迹，独立验证后用于 Kun 快速路径；不是普通 `SKILL.md` 管理 |
| 离线实验 | 编辑冻结记录中的观察文本并比较差异，不重新运行模型或工具 |

这些功能已有部分执行闭环和测试，但仍主要面向 Kun 研究者；真实业务效果及目标运行环境需要进一步验收。本轮保留后端 API、保存的数据、研究查看组件和已有分支会话的“查看固定预览”，不清除历史研究记录。日常轨迹查看、调试问答、Kun 状态检查、单步、断点和检查点恢复继续保留。

历史专题文档中的顶部按钮或创建入口说明不再代表当前导航。研究 API 与数据语义仍见[分叉](KUN-FORKS.md#runtime-forks)、[任务集](KUN-EVALUATION.md#task-sets)、[K4](KUN-K4.md)和[离线实验](KUN-EXPERIMENTS.md)。

## English summary

Conversations can be read as expandable rounds. Trace timing distinguishes valid run wall intervals, known step work and missing durations. A compact primary rail reserves the workspace panel for conversations, while management pages use the released space; mobile navigation remains a dismissible drawer.

Build Center opens as a recipe → image → Agent workbench, reuses existing images and displays linked Agents. Friendly frozen recipe names lead image cards; identifiers remain available below. Docker-aware model discovery retains candidate failures. Codex provider settings target an explicit environment and remain distinct from the Kun maintenance model.

Docker setup separates downloading a ready image from building a recipe and checking an existing local image. Each preparation card keeps the original failure, exact Kun repair, candidate builds, logs and verification results together, including after reopening setup. A completed repair task is not a verified image; the maintenance record must identify a candidate with the required offline/smoke evidence. Download failures never silently switch to local builds.

New installations begin with three full-screen pages: find and configure LLM, configure and install Codex, and optionally prepare Docker environments. Basic setup can finish after Codex; Environment setup reopens the guide later. The Docker page defaults to a published prebuilt image and retains local build or existing-image options. The bundled public catalog stays empty until actual release references are provided. Conversations are organized in two levels: workspace, then conversation. Each conversation displays its Codex environment tag. New conversations inherit the workspace’s last available Codex environment; the tag can change it before session creation. Kun is reserved for maintenance, trace questions and retained research APIs. Maintenance directories and Kun records are excluded from the everyday conversation tree; historical records remain accessible by their existing links. Existing sessions keep their execution identity. Ordinary workspaces can host multiple Docker environments with separate homes, while application-owned workspaces retain their owner. Skills, MCP and plugins share a settings dialog. Build presets produce images; images can support multiple Agents. Application registration remains an integration concern.

Local builds select network settings and Codex runtime sources independently. China and international settings use the same recipes. Matching managed Linux archives or compatible local bases can be reused, while exact-version npm installation remains available. Download-source updates are merged into an editable draft and saved explicitly; they do not overwrite an existing deployment during upgrade.

This navigation change does not yet synchronize configuration across workspaces. Docker environments still have separate persistent homes for each instance/workspace pair. The four Kun research launchers have been withdrawn from everyday navigation while their APIs, stored records, and historical branch inspection remain available.

构建中心在 0.55.0 增加[外置配方与下载源](BUILD-SOURCES.md)：查看 Dockerfile、下载完整目录，按配方实际能力在国际/中国镜像之间选择和修改地址；源列表与默认分组可在页面维护。

构建中心提供公开镜像目录编辑与发布记录入口。镜像发布前展示同一来源镜像的 offline/smoke 条件，发布后显示固定 Digest；仓库公开可见性由维护者在托管平台设置，再单独检查匿名访问。具体行为见[镜像分发](IMAGE-DISTRIBUTION.md)。
