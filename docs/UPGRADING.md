# 升级到当前版本

## 0.67.1：video-build 检查临时空间修复（2026-11-08）

程序、`/api/v1/meta`、OpenAPI 与界面版本应为 **0.67.1**。本补丁修正宿主镜像检查所用的临时空间配置，保留上游配方、工具二进制和 doctor 验收条件；独立 Kun 0.17.0、worker 协议 16 和 UHP `2026-10-19` 不变。

video-build 的宿主离线检查和 CLI doctor 使用 2 GiB `/tmp`；构建中心通用 smoke 也使用 2 GiB，覆盖导入配方运行同一 doctor 的情况。其他配方的专用检查额度及正式运行容器配置保持原样。

按下方原流程升级 RunDesk 宿主并继续指定原 `--data`。已构建的 `rundesk-video-build:0.157.1` 可以保留；升级后从“使用已有镜像”或构建中心重新执行对应检查，无需仅为本补丁重新构建镜像。只有实际通过 offline/smoke 后才登记 `offlineReady` 等状态，不手工修改就绪标记，也不把旧失败记录改写成成功。

针对上述已有镜像，可从“环境设置”重新打开初始化第三步，选择“使用本机已有镜像”，选定 video-build 预设和 Codex `0.157.1`，填入 `rundesk-video-build:0.157.1` 后点击“检查已有镜像”。此路径不拉取或重新构建镜像，仍核对固定镜像身份并执行真实工具检查。

用户现有二进制和镜像无需现场打补丁；等待并使用正常发布的修复版本。调查结论及用户实机验证单独保留在[验证说明](VALIDATION.md#video-build-tmpfs-investigation-2026-11-08)，不计作本包已完成的真实 Docker 复验。

## 0.67.0：整理专题与共享配方输入（2026-11-06）

0.67.0 合并同主题文档与测试，并将配方公共工具、Skills 和已审阅的本地依赖归入 `recipes/common/`。这些整理继续保留，已有用户数据、API 契约、环境绑定及历史冻结上下文保持原语义。

升级应部署完整的 `recipes/`，包括 `common/`。配方声明中的顶层 `sharedFiles` 字典以“上下文目标相对路径 → common 内来源相对路径”引用共享内容。同名本地普通文件优先覆盖；缺失共享来源明确失败，不悄悄改用另一来源。generic 模板仍自含。

旧配方目录中的工具如果整包复制回来，会成为本地覆盖并遮住公共文件的后续更新。请在新的程序目录中只迁移确认过的自定义差异。

导出、打包及新构建会把所需共享内容物化成普通文件，生成独立可构建的上下文；冻结快照与 `profile.lock.json` 不保留 `sharedFiles` 声明，不需要再查公共目录。需要只交付一套配方时，交付其导出上下文，不直接复制一个仍含共享引用的来源目录。Agent-Reach 继续完整本地安装，保留已审阅的 wheel 打包补丁。详见[共享输入与作用范围](BUILD-SOURCES.md#shared-recipe-files)。

相关操作与契约统一到[构建中心](BUILD-CENTER.md)、[轨迹分析](TRACE.md)、[Kun 调试](KUN-DEBUG.md)、[分叉与对照](KUN-FORKS.md)及[评测](KUN-EVALUATION.md)。重命名的测试入口保留原有独立场景；这项整理不删除后端能力、授权检查或历史记录。

历史原始日志、截图和测试视频保留在此前的 0.65.0 源码包；校验摘要见[历史资料说明](validation/README.md)。本版实际检查以[验证范围](VALIDATION.md)为准，不继承旧报告通过数。

## 更新程序和外置资源

1. 等待活动任务结束，停止原服务，备份数据目录、工作区、Codex HOME/配置、独立 Kun 执行日志和服务环境设置。
2. 将本版解压到新的程序目录，不覆盖解压到旧源码树。源码执行 `make build`；部署到别处时一并提供本版 `recipes/`、`config/`、许可和启动文件。自己的配方及源配置先单独保留，再审阅合并。操作见[启动与部署](SETUP.md)。
3. 保留原系统用户、`--data`、`--codex`、`--kun`、`CODEX_HOME` 与服务参数。独立 Kun 可放在 RunDesk 旁或用绝对路径指定；本包只构建宿主，不包含 Kun 内核源码。旧 Kun 执行目录、checkpoint/fork 的兼容范围见[Kun 拆分](KUN-SEPARATION.md)，不手工修改 manifest 或绕过版本检查。
4. 从源码目录之外启动，核对 `rundesk --version`、`kun --version` 和 `/api/v1/meta`，再刷新浏览器。应确认程序从部署目录读取资源，没有依赖源码树的后备路径。

一个可靠本地文件系统的数据目录只运行一个 RunDesk。不要删除运行中的 `server.lock` 来并行启动。重启不会重放已开始或结果未确认的任务；排队任务保留。先检查原回执、任务状态与副作用证据，再决定是否重试。

## 从旧配方目录升级

- `recipes/` 是唯一可编辑配方来源；`<profile>/` 保存配方特有内容，`common/` 保存声明引用的公共输入。源码中的 `recipe.json` 在冻结构建或导出上下文时生成 `profile.lock.json`。旧外置锁文件配方仍可读；同一来源目录不要同时保留两种声明，因此应使用新的程序目录。
- 原 `python3 examples/<profile>/run.py <命令>` 已移除，改用 `python3 scripts/recipes.py <命令> <profile>`。Python 应用客户端位于 `clients/python/`，systemd 文件位于 `deploy/systemd/`；生产 Go 读取器位于 `internal/`。轻量 Agent-Reach 选项、可选 video-use 兼容变体、原始来源与许可证继续保留。
- 配方查找优先级为显式 `RUNDESK_RECIPES_DIR`、可执行文件旁的 `recipes/`、从当前工作目录向上查找的 `recipes/`。显式路径缺失或无效会直接报错。修改外置 Dockerfile、依赖或 vendor 后，须从配方新建构建，无需重编译或重启。
- 历史构建继续使用冻结的 `context.zip` 与源参数。“原配置重建”不读取今天的新配方或网络默认值；旧记录缺少上下文时明确失败，应重新导入原输入。删除构建记录不删除 Docker 镜像，删除镜像不删除项目或 HOME。
- news2douyin、people-search 1.1.2 保留修复后的本地 Agent-Reach 及 wheel 打包修复，构建不再下载该上游源码；APT、Python/npm 包和构建后端仍需相应通道。自定义配方通过 `pack --codex-version X.Y.Z` 固定精确版本，导入新的 ZIP，不修改旧快照。见[配方与 CLI](../recipes/README.md)。

## 保留本地源、缓存和运行环境

`<data>/config/build-sources.json` 及 `RUNDESK_BUILD_SOURCES` 指定的文件继续保留。发行包默认条目的新增字段可在编辑器中合并到草稿，审阅后保存；不会覆盖自己的地址或默认分组。并发保存仍核对 revision。国际／中国分组只填入选择的源，不保证目标网络可达；Docker 拉取和容器内包下载分别判断。

运行时来源与网络源独立。受管 Linux Codex 原生包必须精确版本匹配；显式原生包或基础镜像失败不会自动换源。按原身份重建依赖数据目录中的安装与缓存及其摘要，删除或损坏时明确报错，不由移动后的镜像标签或另一版本代替。OpenMontage 的新归档 URL 与 SHA-256 缓存仅随新配方生效。见[构建源](BUILD-SOURCES.md)与[运行时复用](BUILD-RUNTIME.md)。

已有镜像、Agent、应用绑定、工作区、容器 HOME、认证、Skills/MCP、会话、制品和修复经验不删除或自动替换。Docker 配置仍按 Agent 与工作区分别保存；页面集中不表示跨工作区自动同步。旧容器可能挂载升级前的监管程序，若模型连接测试提示命令不可用，可在空闲时明确重建容器，保留持久配置及工作目录。发现本机 vLLM 不会自动切换已有 Codex Provider。见[WebUI](WEBUI.md)与[模型服务](CODEX-PROVIDERS.md)。

## 初始化、修复和镜像发布

已有 completed 初始化状态不重置，可从“环境设置”重开三步向导；Docker 是可选步骤。自动发现不切换默认后端，也不开启宿主 Bash。Codex 安装使用精确版本及摘要，不自动登录。下载失败保持下载重试，Kun 修复任务完成与候选通过 offline/smoke 分别判断；不自动替换现有镜像。修复经验按真实记录追加版本，不覆盖默认源、Skill 或已有配方。见[初始化](SETUP.md#首次初始化)与[内部维护](MAINTENANCE.md)。

构建助手需要容器可达的 `RUNDESK_BUILD_MCP_ORIGIN` 或 `--public-url`；容器 localhost 不是宿主。权限限当前任务并到期失效，不传 Docker socket 或宿主管理员令牌。插件检查只读，不把展开 Skill 当作原生插件安装。见[构建中心](BUILD-CENTER.md#build-workbench)与[插件边界](NATIVE-PLUGINS.md)。

公开镜像目录 `<data>/config/public-images.json` 或 `RUNDESK_PUBLIC_IMAGES` 指定的文件由管理员维护；随包目录为空。发布需真实仓库权限和匹配的 offline/smoke 记录；上传后仍需设置公开可见性、核对匿名 manifest 并验收完整下载，再登记固定 Digest。升级不公开上传已有产物，不自动改写目录。离线机器可由管理员 `docker save` / `docker load` 后检查本地镜像。见[镜像分发](IMAGE-DISTRIBUTION.md)。

## 会话、应用和维护的兼容边界

日常会话与应用任务使用 Codex。旧普通 Kun 会话保留可读，不能继续提交日常消息；需在原工作区新建 Codex 会话。指向 Kun 的日常 API 提交返回 `409 kun_maintenance_only`，不静默回退；应用、定时任务和外部调用应显式检查目标。Kun 维护与研究 API 及已保存数据保留，轨迹问答需要独立维护助手。候选验证和任务完成分开，新计划协议不重写旧合同。

应用已有默认环境、命名预设及凭据保持原授权。新建凭据预选当前启用环境，旧凭据不会自动获得新环境；新增预设后须签发包含它的新凭据并更新调用方。省略 presetId 保持默认环境行为；各项目的独立环境与配置快照不自动更新。上游 video-use、HyperFrames、OpenMontage 与自研 video-build 是独立配方，预设名称不安装工具或转换旧工程。见[运行预设](APPLICATION-RUNTIME-PRESETS.md)、[项目环境](APPLICATION-PROJECT-ENVIRONMENTS.md)与[视频环境](UPSTREAM-VIDEO-ENVIRONMENTS.md)。

工作区与会话继续两层组织，每个会话选择 Codex 环境。轮次折叠、栏宽等浏览器偏好不改变任务状态；旧轮次只按已有证据保守重建。Kun 分叉、评测任务集、K4 和离线实验撤下日常启动入口，相关 API、保存数据和既有分支查看仍可用。

HTTP 观测默认关闭。`--observe-http=true` 只设置以后新建 Codex 会话的默认值，不覆盖已有 `observeHTTP`；旧会话缺失字段按关闭处理。会话开关在下一轮应用，`pending` 不代表正在采集；未采集内容不能补回。Docker 需正确的监听与容器可达地址，观测正文纳入原数据访问与保留策略。见[统一调试](UNIFIED_DEBUGGING.md)。

## UHP 和文件兼容

- Codex/Kun 均不执行 UHP 工具轮次或输出 token 硬预算。合法 `max_step`、`max_output_tokens` 明确忽略；依赖硬限制须提交 `metadata.rundesk_require`，目前返回 `422 rundesk_capability_unsupported`，不创建任务或物化本次附件。
- 旧活动 UHP 记录若保存正数 `MaxStep`，会停止并返回 `incomplete` / `legacy_step_budget_unsupported`；旧终态结果保留。无公共 deadline 的旧任务不凭空增加一小时预算。
- 新 UHP 任务默认时限 3600 秒，显式范围 1–86400 秒，包含排队；0 不是关闭。绝对截止时间持久保存并在重启后恢复，到期映射 `incomplete` / `timeout`，协作取消不回滚外部动作。
- 额外指令以 `task_text` 留在 `session_history`，不提供原生优先级或单次自动清除；要求 `native_instructions` 会被拒绝。进度与待审批地址沿用已有访问控制，标准 SSE 不新增私有进度事件。
- 未知提交结果保留原凭据、operation key 和确切请求重试；幂等回执仍指向首次执行，不为刷新能力或预算换键重试。官方 Extended 与应用内专项分别验收，不代表 Full T-12 或未提供的 `createHarness` 已验证。完整边界见 [UHP](UHP.md)。
- 新个人产物默认登记原件引用，需永久冻结时显式保存快照；旧托管副本仍可读。源文件变更或删除分别报告 `changed` / `missing`，不以新内容冒充历史产物。任务资源导出是可重建视图，不是第二套任务状态源。见[文件库](PERSONAL-FILES.md)与[任务资源](TASK-RESOURCES.md)。

更早版本的完整升级记录保留在 0.65.0 源码包。本页保留当前部署仍需遵守的兼容要求；具体能力与实际验收分别见[当前状态](STATUS.md)和[验证范围](VALIDATION.md)。
