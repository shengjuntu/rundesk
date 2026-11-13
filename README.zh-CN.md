# RunDesk

[English](README.md) · [文档导航](docs/README.md)

**面向 Agent Harness 的调试工作台、应用连接器与研究实验场。**

RunDesk 让 Agent 的执行过程可观测，也让应用更容易使用 Agent。日常会话和应用任务统一由 Codex 执行；独立开发的 Kun 引擎负责内部维护与轨迹问答。Web 工作台和 API 管理任务、进度、审批、执行轨迹与制品。

**Agent Harness** 指围绕模型运行的执行系统，负责上下文、执行循环、工具调用和运行状态。RunDesk 将应用与开发者连接到这个运行系统。Skills、插件、MCP 服务和执行环境可以按需组合，不要求套入固定工作流。

## 三个使命

### 1. 为企业 Agent 落地提供调试界面

这是 RunDesk 的出发点。将 Agent 落地到企业应用的开发者，需要看清 Agent 实际做了什么、任务卡在哪里，以及为什么失败。

RunDesk 把用户请求、模型交互、工具调用、审批、异常和制品汇集到同一个调试工作台，帮助开发者观测 Agent 的行为与执行轨迹，依据记录定位问题。Codex 与 Kun 都支持轨迹检查；Kun 还提供内部状态、单步、断点、检查点和运行时分叉。每个后端实际支持的能力分别说明。

入口：[轨迹调试](docs/TRACE.md#debug-overview)、[轨迹阅读器](docs/TRACE.md#trace-reader)、[后端能力](docs/BACKENDS.md)。

### 2. 成为应用与 Agent Harness 之间的连接器

应用开发者应当能够专注自己的窄领域：新闻研究、媒体制作、人物搜索，或其他具体业务。RunDesk 负责降低接入与使用 Agent 运行系统的门槛。

应用侧的基本流程是：**提交任务 → 跟踪进度 → 获取制品**。RunDesk 提供应用凭据、会话、持久队列、事件、审批、取消和制品访问；应用负责业务数据、领域服务、用户体验及结果验收。

**运行预设（Runtime Presets）**用于提供预先准备、可重复使用的运行配置，目标是把所需 Skills、插件、MCP 服务和工具提前组合好，让应用直接使用。本版的命名预设绑定管理员批准的 Codex Docker 实例与工作区，通过构建配方和配置准备工具、Skills 与 MCP 接入。原生插件目前支持检查和已观察到的组件来源展示，稳定的原生安装/卸载尚未实现。仅登记一个预设，不代表已经构建镜像、安装插件或完成模型账号认证。

入口：[应用 API](docs/API-V1.md)、[运行预设](docs/APPLICATION-RUNTIME-PRESETS.md)、[构建配方](recipes/README.md)、[构建中心](docs/BUILD-CENTER.md#build-workbench)。

### 3. 成为 Harness 开发者与研究者的实验场

Harness 开发者和研究者需要试验执行策略，观察改动带来的影响，并用记录下来的证据比较结果。

RunDesk 通过 Kun 集成提供模块状态检查和执行循环策略选择。研究 API 保留离线记录实验、Hybrid/Live 分叉、固定任务集和对照评测；经过验证的轨迹可以进一步形成参数化工具流程候选。这些能力主要面向 Kun 研究，业务效果仍需任务验证；尚未开放任意 Harness 模块的动态加载 SDK。

入口：[Harness 控制](docs/KUN-HARNESS.md)。[运行时分叉](docs/KUN-FORKS.md#runtime-forks)与[重复采样对照](docs/KUN-EVALUATION.md#repeated-benchmarks)介绍保留的研究 API 和历史操作流程，不再对应当前主界面的导航入口。

## 会话与制作环境

打开工作区就能用默认 Codex 开始会话。应用通过获准的制作预设提交任务，构建中心负责准备配方、镜像与制作环境。模型、Skills、MCP 和插件设置作用于对话框标注的环境和工作区。

后台继续使用稳定的实例 ID 来核对授权、配置和执行记录。日常用户从工作区直接进入会话。见[存储契约](docs/STORAGE.md)与[界面说明](docs/WEBUI.md)。

## 当前源码版本

**RunDesk 0.75.1 · 外部 Kun 0.17.0 · worker 协议 16**

0.75.1 修复 Docker 公共基础层把裸镜像 ID 传给 `FROM` 的问题：构建参数使用已核验的本地专用标签，冻结记录与缓存仍以实际镜像 ID 为准。升级宿主后可重新发起失败构建，见[升级说明](docs/UPGRADING.md)。

0.75.0 补齐本地获取：ZIP、文件夹与已安装 Skill 都能先生成独立快照，再检查或测试。环境诊断需明确点击，分别显示工具缺失、版本不兼容、协议失败和模型认证未验证，详见[技能中心](docs/SKILL-CENTER.md)。

0.74.0 接入[Skill-Up 效果测试](docs/SKILL-CHECKS.md)：在选定 Docker 环境中显式校验／运行用例，查看逐用例结果、导入外部报告及比较不同 Skill 版本。报告来源、用例身份与配置缺失不能自动当作回归通过，Docker 不可用时不会退回宿主执行。

0.73.0 新增原生[技能中心](docs/SKILL-CENTER.md)：搜索 ClawHub、SkillsMP、skills.sh 与 GitHub，获取固定目录、预览源码、进行内置检查或可选 SkillSpector 扫描，再安装到工作区／运行环境。风险项及检查不完整需显式确认与理由。

0.73.1 已用真实 NVIDIA SkillSpector 2.13.4 验证静态扫描与安装门禁，补充严格覆盖核对、持久报告切换和刷新恢复任务。未验证模型辅助扫描与 Windows 实机运行。

0.72.0 在每轮准入时保存配置声明版本与执行身份，并把可核实的制品来源关联到产生它的 Run。选择一轮轨迹后可以查看当时有证据的配置。升级继续使用原工作区、会话、引擎历史和应用预设，旧数据的查询方式见[升级说明](docs/UPGRADING.md)。

0.67.1 的 video-build 临时空间修复继续保留：检查使用 2 GiB `/tmp`，上游 doctor 检查条件不变。用户对同一镜像的隔离验证与本版实际检查分别记录在[验证说明](docs/VALIDATION.md#video-build-tmpfs-investigation-2026-11-08)；只有实际检查通过才登记就绪状态。

0.67.0 将同主题文档和测试整理为完整流程，并通过 `sharedFiles` 显式复用配方公共内容。导出目录全部是普通文件，可独立构建；配方内同名文件可覆盖共享目标。可再生检查结果继续进入 `build/qa/`，早期原始证据保留在此前的 0.65.0 源码包。

`recipes/` 是唯一可编辑配方来源。公共工具、Skills 和已审阅的本地依赖保存在 `recipes/common/`，各配方显式声明引用；缺失来源明确失败，generic 模板保持自含。生产读取器、应用客户端和部署文件分别位于 `internal/`、`clients/python/`、`deploy/systemd/`。构建快照生成各自的锁文件。news2douyin、people-search 1.1.2 保留已审阅的 Agent-Reach 本地源码及 wheel 打包修复；其他依赖仍需相应下载通道。操作见[配方与 CLI](recipes/README.md)及[启动部署](docs/SETUP.md)。

新安装采用**寻找和配置 LLM → 配置和安装 Codex → 准备 Docker 环境**三步向导。Docker 可跳过，以后从“环境设置”重开。下载现成镜像、本机构建、检查本机镜像分别选择；每个准备任务集中展示失败、Kun 修复、候选构建与验证，修复任务结束不等于镜像已验证。

日常会话和应用任务使用 Codex；Kun 负责安装与配方修复、只读轨迹问答。Skill 提取及 MCP 自动优化仍待实现。工作区组织会话，普通新会话使用默认 Codex；应用可绑定多个批准的环境，并按项目建立独立工作区。详见[WebUI](docs/WEBUI.md)、[内部维护](docs/MAINTENANCE.md)及[运行预设](docs/APPLICATION-RUNTIME-PRESETS.md)。

构建中心连接**配方 → 镜像 → 制作环境**。Dockerfile 与源列表是可编辑文件，源分国际／中国镜像两组，覆盖 pip、conda、npm、Docker、APT、YUM/DNF、Hugging Face 和 GitHub；实际生效取决于配方声明的参数。构建冻结输入，可复用精确匹配的受管 Codex 运行时。Codex 模型配置与 Kun 模型发现分别保存，作用于明确的 Agent／工作区环境。详见[构建中心](docs/BUILD-CENTER.md#build-workbench)、[下载源](docs/BUILD-SOURCES.md)及[模型服务](docs/CODEX-PROVIDERS.md)。

发布者可在匹配的 offline、smoke 检查通过后分发固定 Digest 镜像。**随包公开目录暂为空，须先发布真实镜像再登记地址**；不预填官方镜像空间，也不据此宣称匿名公网下载已验收。见[镜像分发](docs/IMAGE-DISTRIBUTION.md)。

两个 API 入口共享同一任务核心：

| 入口 | 用途 |
| --- | --- |
| **RDCA v1** — `/api/v1` | 应用与管理 API；机器契约位于 `/api/v1/openapi.json` |
| **UHP** — `/v1` | 面向协议 `2026-10-19` 的附加接口，具备[明确限制](docs/UHP.md)，不声明完整兼容 |

RunDesk 独立运行，不依赖 HarnessRouter。UHP 不支持的步数及输出 Token 硬预算继续明确披露。当前能力、执行边界及各配方组件版本分别见[当前状态](docs/STATUS.md)、[系统架构](docs/ARCHITECTURE.md)和[配方文档](recipes/README.md)。

## 快速开始

Linux 或 Windows 部署目录包含 RunDesk、`recipes/`、`config/` 和对应平台的启动文件；打包时明确提供独立 Kun 才会一并加入。部署后宿主机无需 Go、Python、Node.js 或 npm。

Linux 解压后执行：

```bash
chmod +x ./start-rundesk.sh
./start-rundesk.sh
```

Windows 解压后双击 `start-rundesk.cmd`。打开 <http://127.0.0.1:3210>，新安装先进入三步向导。已完成初始化后，可点击左侧导航栏底部的齿轮“环境检查”重新打开。可以连接已有本机模型服务，也可以填写远程服务地址；需要拉取、构建、检查或运行镜像时再准备 Docker Engine。

### 从源码构建

使用 **Go 1.25.12** 或兼容的更新版本。WebUI 已嵌入，无需前端构建。

```bash
make build
./bin/rundesk --data ./data
```

用 `make package PACKAGE_DIR=../rundesk-portable` 在源码树之外生成新的部署目录；需要 Kun 时增加 `KUN_BINARY=/absolute/path/to/kun`。部署到源码目录之外时，还需把 `recipes/`、`config/` 放到可执行文件旁。其他配方目录使用 `RUNDESK_RECIPES_DIR`，自选源列表 JSON 使用 `RUNDESK_BUILD_SOURCES`。WebUI 继续内嵌，用户需要修改的构建资源改为外置文件。详见[部署](docs/SETUP.md)。

打开 <http://127.0.0.1:3210> 开始初始化。扫描对象是运行 RunDesk 的机器，也可能是远程服务器；可手工填写模型服务地址，暂不需要的 Codex 或 Docker 可以跳过。仅体验界面、不调用真实模型：

```bash
./bin/rundesk --data ./demo-data --demo
```

使用 Kun 时，先构建独立 Kun 项目，再指定其可执行文件：

```bash
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

本包只构建 RunDesk。`third_party/kun-sdk` 是轻量 SDK，宿主构建无需 Kun 内核源码；`your-org` 模块路径为未发布占位，由 `go.mod` 本地替换解析。缺少或不兼容的 Kun 会明确报错。

Kun 连接支持工具调用的 Chat Completions 服务；Codex 使用原生模型与认证配置。日常执行使用 Codex 环境，Kun 配置用于维护与保留的研究 API。已有记录保留原身份，普通 Kun 会话不再接收新的日常任务。Windows、Docker 和可选 Codex 安装见[启动与部署](docs/SETUP.md)。

## 按角色阅读

| 面向用户 | 文档入口 |
| --- | --- |
| 企业 Agent 落地开发者 | [调试](docs/TRACE.md#debug-overview) · [观测与采集](docs/UNIFIED_DEBUGGING.md) · [只读调试 API/MCP](docs/DEBUG-SERVICE.md) |
| 应用开发者 | [应用注册](docs/APPLICATION-CONNECT.md) · [API](docs/API-V1.md) · [运行预设](docs/APPLICATION-RUNTIME-PRESETS.md) · [Python 示例](clients/python/application_quickstart.py) |
| Harness 开发者与研究者 | [Kun 接入](docs/KUN.md) · [Harness 控制](docs/KUN-HARNESS.md) · [研究 API 与历史资料](docs/README.md#3-harness-开发与研究--harness-research) |
| 维护者与部署者 | [架构](docs/ARCHITECTURE.md) · [构建中心](docs/BUILD-CENTER.md#build-workbench) · [下载源](docs/BUILD-SOURCES.md) · [升级](docs/UPGRADING.md) · [安全](SECURITY.md) |

本地开发可用 `make test` 运行宿主与 SDK 测试；`make check` 增加 race 检查及 `go vet`。部分集成测试需要独立 Kun 可执行文件。复验方法与当前范围见[验证说明](docs/VALIDATION.md)，历史结果的保留位置见[来源说明](docs/validation/README.md)。

## 执行边界

- Codex HTTP 观测默认关闭，按会话开启。`--observe-http=true` 只改变新建 Codex 会话的默认值，不覆盖已保存设置。采集仅覆盖经过已配置代理的流量，不完整证据会明确标注。
- 托管 Docker 当前支持 Codex。Kun Bash 是显式启用的宿主执行，文件工具的路径限制不构成操作系统沙箱；Kun 容器托管仍待实现。
- 任务完成、工具调用成功与业务结果验收是不同事实。未知副作用不会被静默重放，记录也不会揭示模型未公开的内部推理。
- 三个使命不代表原生插件安装、任意后端模块或完整 UHP 兼容已经完成。已实现能力与后续工作见[当前状态](docs/STATUS.md)。

## 致谢与许可

感谢 [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG)、Michael Kinsy 及其贡献者、Mario Zechner / Pi，以及 [HarnessRouter](https://github.com/HarnessRouter/harnessrouter)。独立 Kun 项目保留选取的 PiG SSE 代码与原文 MIT 许可；HarnessRouter/UHP 为应用与后端边界提供设计参考。[上游来源](docs/UPSTREAM.md)区分实际引入的代码与设计参考。

[许可协议](LICENSE) · [第三方声明](THIRD_PARTY_NOTICES.md) · [许可证原文](LICENSES)
