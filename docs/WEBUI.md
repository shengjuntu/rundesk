# WebUI：工作区、会话与 Agent / Workspaces, conversations and Agents

本文描述 RunDesk 0.64.0 的界面与当前后端边界。升级不改变存储布局，也不迁移或删除原有实例、工作区和历史数据。界面将长期保存的“实例”称为 **Agent**；它不等同于 Docker 容器。应用的“应用设置”页展示可用 Agent 环境、默认绑定、凭据授权数量和项目工作区；见[运行预设](APPLICATION-RUNTIME-PRESETS.md)与[项目环境](APPLICATION-PROJECT-ENVIRONMENTS.md)。

RunDesk 的会话按工作区组织：进入工作区后新建会话，由会话选择 Codex 运行环境。同一工作区可以包含多个使用不同 Codex 配置的会话。Kun 不出现在日常 Agent 列表与新建任务选项中，内部维护从“环境设置”和构建中心进入。Agent 管理负责模型、Skills、MCP 和插件配置；应用接入和构建中心分别负责外部调用与运行环境准备，不要求用户先注册应用才能管理实例。

## 初始化首页

首次非 Demo 安装先进入全屏初始化页，工作台在初始化结束或明确稍后配置之后显示。页面只有三步：**寻找和配置 LLM、配置和安装 Codex、准备 Docker 环境（可选）**。环境发现和 Kun 模型配置放在第一步；第二步可直接点击“完成基本设置，稍后准备 Docker”；第三步保留构建、镜像下载、经验与完整概览。可以自由返回或切换步骤，浏览器地址记录当前步骤。已完成安装从 Agent 列表开始；左侧“环境设置”可重新打开向导。

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

默认实例保留本机使用方式。通过构建中心创建的 Docker 实例可以直接进入实例页面管理；重建容器不等于重新创建实例。Kun 当前仍仅支持本机 worker，不能通过界面布局变化获得 Docker 托管能力。

Agent 页面明确当前工作区；Docker 构建与镜像管理统一从左侧“构建中心”进入。会话页按“工作区 → 会话”两层组织，同一工作区同时展示不同 Codex 配置的会话。会话标题旁显示具体 Codex 环境标签；新会话可点击标签更换环境，已创建的会话保持原绑定。Skills、MCP 和插件在同一个高度稳定的设置对话框中。切换工作区后，应核对配置所属的运行环境；任务与轨迹保留原来的 Agent 和工作区归属。应用页分为“任务记录”和“应用设置”：设置页同时展示全部已绑定预设及启用数量，应用可绑定多个预设，每个任务选择一个。页面只有一个“添加预设”入口，各预设在自己的卡片中配置能力；默认环境、容器技术信息、应用项目和凭据收在展开项中。添加弹窗自动创建工作目录，技术字段默认收起，保存按钮固定在底部。详情见[运行预设](APPLICATION-RUNTIME-PRESETS.md)。

## 导航与阅读空间

主导航使用紧凑栏，Agent、对话、任务和构建中心仍按既有入口进入。桌面／平板主栏为 72px，手机为 62px，保留小字标签与提示。工作区／会话作为可收起、可调整宽度的第二栏，仅在会话页参与布局；管理页面使用释放出的内容空间。收起选择保存在当前浏览器，回到会话时恢复。没有保存偏好时，900px 以下默认收起工作区栏；600px 及以下使用抽屉。抽屉可用 Escape 或背景关闭，并将键盘焦点限制在其中，关闭后回到触发按钮。

## 构建工作台

构建中心默认沿“选择配方 → 准备镜像 → 创建 Agent”显示当前上下文。选择配方后可查看对应构建、已有镜像与已使用它的 Agent；已有镜像可以直接复用，不要求每次重新构建。配方有多个版本和构建结果，一个镜像也可服务多个 Agent。缺少可靠来源的镜像明确显示来源未知。

镜像卡片将配方名称与版本放在上方，固定 Image ID 和标签放在下方；历史记录优先采用冻结名称与版本，不用后来改名的配方替换历史身份。原配方、构建与测试、镜像及发布记录的深链接仍可进入高级视图，复用原后台队列、测试和请求重试记录。

创建 Agent 的模型设置选择已有配置或本次显式继承的默认 Codex 服务。模型提供方配置不写进镜像；具体作用范围、凭据与检查边界见[Codex 模型服务](CODEX-PROVIDERS.md)。

## 工作区与会话

左侧“会话”进入按工作区组织的列表。每个工作区展开后显示会话标题和引擎标签；搜索、置顶和归档沿用原记录。选中的工作区内只提供一个“新会话”入口，自动沿用该工作区最近使用的可用引擎，无需同时选择工作区和 Agent。新建工作区只需名称，已有目录放在可选展开项中。

会话环境标签只包含 Codex 配置；切换新会话的环境不改变工作区。未发送文字保存在草稿中，附件和技能保留在所属环境的草稿中，避免把配置或文件授权带到其他实例。已创建会话的引擎标签只读，需要其他引擎时在同一工作区新建会话。

对话按可靠的轮次身份组织，completed/succeeded 轮次默认收为用户问题标题和状态，点击展开这一轮的问题、回答和工具步骤。标题取用户输入的第一个非空行，最多 120 个 Unicode 码点；完整输入仍在正文中。手动展开或收起的选择按会话／轮次保存在当前浏览器，后续事件和重新加载继续使用该选择。进行中、等待处理、失败、中断及状态未知的轮次默认展开；即使用户手动收起，摘要仍显示其状态。如果轮次完成时键盘焦点正在正文内，会暂时保持展开，避免隐藏正在阅读的内容。

明确的宿主／原生对应优先；缺少直接运行标识时，只允许由输入及原生开始记录保守重建，并显示关联提示。没有可靠身份或身份冲突的记录单独保留，不据时间邻近关系强行合并。仍受当前最多 200 条显示项的界面边界约束，范围不完整时明确提示并保留轨迹入口。阅读偏好不触发任务、工具或模型调用。

普通工作区支持不同 Docker 实例的会话，各实例/工作区组合保持独立容器、HOME 和认证；工作区文件共享。应用默认环境、预设及应用项目的专用工作区仍按归属限制候选引擎，项目会话保留 appId/projectId/presetId。旧会话、旧深链接及 Agent 管理入口继续可用。

回归测试见 `internal/web/tests/workspace-conversations.cjs`（jsdom）与 `internal/web/tests/workspace-conversations-browser.cjs`（Playwright，`RUNDESK_BINARY` 指向当前编译产物）。浏览器测试使用独立临时服务，仅创建空闲会话和运行环境记录，不调用真实模型或启动 Docker。

## 轨迹耗时

从会话原有“轨迹”入口进入步骤和耗时分析，可查看最多十个最慢运行步骤，并在完整筛选范围中按耗时排序后分页。只有宿主提交到终态的有效区间标为“本轮总耗时”；原生开始后的区间与 Kun worker 区间分别标识，不能冒充包含启动前等待的完整耗时。界面显示 `clock`、`startMethod/endMethod` 依据；缺失记录显示未知。已知步骤的耗时合计只是工作量视图，可能包含重叠或嵌套，不能代替整轮经过时间。模型运行步骤和模型通信观测分别保留，避免两份记录被当成两次执行。所有筛选、统计和分页共享所选轮次及固定 `through`，读取最新记录才推进范围。详见[轨迹](TRACE.md)。

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

历史专题文档中的顶部按钮或创建入口说明不再代表当前导航。研究 API 与数据语义仍见[分叉](KUN-FORKS.md)、[任务集](KUN-TASK-SETS.md)、[K4](KUN-K4.md)和[离线实验](KUN-EXPERIMENTS.md)。

## English summary

Conversations can be read as expandable rounds. Trace timing distinguishes valid run wall intervals, known step work and missing durations. A compact primary rail reserves the workspace panel for conversations, while management pages use the released space; mobile navigation remains a dismissible drawer.

Build Center opens as a recipe → image → Agent workbench, reuses existing images and displays linked Agents. Friendly frozen recipe names lead image cards; identifiers remain available below. Docker-aware model discovery retains candidate failures. Codex provider settings target an explicit environment and remain distinct from the Kun maintenance model.

Docker setup separates downloading a ready image from building a recipe and checking an existing local image. Each preparation card keeps the original failure, exact Kun repair, candidate builds, logs and verification results together, including after reopening setup. A completed repair task is not a verified image; the maintenance record must identify a candidate with the required offline/smoke evidence. Download failures never silently switch to local builds.

New installations begin with three full-screen pages: find and configure LLM, configure and install Codex, and optionally prepare Docker environments. Basic setup can finish after Codex; Environment setup reopens the guide later. The Docker page defaults to a published prebuilt image and retains local build or existing-image options. The bundled public catalog stays empty until actual release references are provided. Conversations are organized in two levels: workspace, then conversation. Each conversation displays its Codex environment tag. New conversations inherit the workspace’s last available Codex environment; the tag can change it before session creation. Kun is reserved for maintenance, trace questions and retained research APIs. Maintenance directories and Kun records are excluded from the everyday conversation tree; historical records remain accessible by their existing links. Existing sessions keep their execution identity. Ordinary workspaces can host multiple Docker environments with separate homes, while application-owned workspaces retain their owner. Skills, MCP and plugins share a settings dialog. Build presets produce images; images can support multiple Agents. Application registration remains an integration concern.

Local builds select network settings and Codex runtime sources independently. China and international settings use the same recipes. Matching managed Linux archives or compatible local bases can be reused, while exact-version npm installation remains available. Download-source updates are merged into an editable draft and saved explicitly; they do not overwrite an existing deployment during upgrade.

This navigation change does not yet synchronize configuration across workspaces. Docker environments still have separate persistent homes for each instance/workspace pair. The four Kun research launchers have been withdrawn from everyday navigation while their APIs, stored records, and historical branch inspection remain available.

构建中心在 0.55.0 增加[外置配方与下载源](BUILD-SOURCES.md)：查看 Dockerfile、下载完整目录，按配方实际能力在国际/中国镜像之间选择和修改地址；源列表与默认分组可在页面维护。

构建中心提供公开镜像目录编辑与发布记录入口。镜像发布前展示同一来源镜像的 offline/smoke 条件，发布后显示固定 Digest；仓库公开可见性由维护者在托管平台设置，再单独检查匿名访问。具体行为见[镜像分发](IMAGE-DISTRIBUTION.md)。
