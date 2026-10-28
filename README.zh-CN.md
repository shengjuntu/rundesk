# RunDesk

[English](README.md) · [文档导航](docs/README.md)

**面向 Agent Harness 的调试工作台、应用连接器与研究实验场。**

RunDesk 让 Agent 的执行过程可观测，也让应用更容易使用 Agent。它连接 Codex 与独立开发的 Kun 引擎，通过 Web 工作台和 API 管理任务、进度、审批、执行轨迹与制品。

**Agent Harness** 指围绕模型运行的执行系统，负责上下文、执行循环、工具调用和运行状态。RunDesk 将应用与开发者连接到这个运行系统。Skills、插件、MCP 服务和执行环境可以按需组合，不要求套入固定工作流。

## 三个使命

### 1. 为企业 Agent 落地提供调试界面

这是 RunDesk 的出发点。将 Agent 落地到企业应用的开发者，需要看清 Agent 实际做了什么、任务卡在哪里，以及为什么失败。

RunDesk 把用户请求、模型交互、工具调用、审批、异常和制品汇集到同一个调试工作台，帮助开发者观测 Agent 的行为与执行轨迹，依据记录定位问题。Codex 与 Kun 都支持轨迹检查；Kun 还提供内部状态、单步、断点、检查点和运行时分叉。每个后端实际支持的能力分别说明。

入口：[轨迹调试](docs/DEBUG.md)、[轨迹阅读器](docs/TRACE-READER.md)、[后端能力](docs/BACKENDS.md)。

### 2. 成为应用与 Agent Harness 之间的连接器

应用开发者应当能够专注自己的窄领域：新闻研究、媒体制作、人物搜索，或其他具体业务。RunDesk 负责降低接入与使用 Agent 运行系统的门槛。

应用侧的基本流程是：**提交任务 → 跟踪进度 → 获取制品**。RunDesk 提供应用凭据、会话、持久队列、事件、审批、取消和制品访问；应用负责业务数据、领域服务、用户体验及结果验收。

**运行预设（Runtime Presets）**用于提供预先准备、可重复使用的运行配置，目标是把所需 Skills、插件、MCP 服务和工具提前组合好，让应用直接使用。本版的命名预设绑定管理员批准的 Codex Docker 实例与工作区，通过构建配方和配置准备工具、Skills 与 MCP 接入。原生插件目前支持检查和已观察到的组件来源展示，稳定的原生安装/卸载尚未实现。仅登记一个预设，不代表已经构建镜像、安装插件或完成模型账号认证。

入口：[应用 API](docs/API-V1.md)、[运行预设](docs/APPLICATION-RUNTIME-PRESETS.md)、[示例配置](examples/README.md)、[构建中心](docs/BUILD-CENTER.md)。

### 3. 成为 Harness 开发者与研究者的实验场

Harness 开发者和研究者需要试验执行策略，观察改动带来的影响，并用记录下来的证据比较结果。

RunDesk 通过 Kun 集成提供模块状态检查和执行循环策略选择。研究 API 保留离线记录实验、Hybrid/Live 分叉、固定任务集和对照评测；经过验证的轨迹可以进一步形成参数化工具流程候选。这些能力主要面向 Kun 研究，业务效果仍需任务验证；尚未开放任意 Harness 模块的动态加载 SDK。

入口：[Harness 控制](docs/KUN-HARNESS.md)。[运行时分叉](docs/KUN-FORKS.md)与[重复采样对照](docs/KUN-BENCHMARKS.md)介绍保留的研究 API 和历史操作流程，不再对应当前主界面的导航入口。

## 以实例为中心使用

**实例**是日常操作的中心：选择一个 Codex 或 Kun 实例，检查模型和能力，选择工作区，再执行任务、查看制品与运行轨迹。构建中心从构建预设生成镜像，一个镜像可以用于创建多个独立实例。应用注册是另一个接入步骤，管理实例无需先注册应用。

0.54.0 将 Kun 分叉、评测任务集、K4 Skill 工作台和离线实验的启动入口撤出日常导航。对应 API、保存的数据和已有分支的固定预览查看继续保留。K4 的参数化工具流程与普通 `SKILL.md` 管理是不同能力。

实例中心 WebUI **尚未实现实例配置跨工作区自动同步**；Docker 配置仍保存在各自的实例/工作区运行环境中。自动收集能力到统一资源库、不同 Agent 配置格式转换，也仍待实现。具体行为与边界见 [WebUI](docs/WEBUI.md)。

## 当前源码版本

**RunDesk 0.59.0 · 外部 Kun 0.17.0 · worker 协议 16**

0.59.0 将初始化做成三页向导。Docker 是可选步骤：在 Codex 页即可完成基本设置，此后用左侧“环境设置”重开。Agent 列表分别提供准备镜像与从已有镜像添加 Agent；对话输入框显示 Agent 和工作区，Skills、MCP、插件仍在同一个设置对话框中。Docker 配置目前仍按 Agent 与工作区分别保存，详见 [WebUI](docs/WEBUI.md)。

0.57.0 的启动界面是**三步初始化向导：寻找和配置 LLM → 配置和安装 Codex → 准备 Docker 环境**。第一步在同一页发现服务器硬件和模型服务、测试推理与函数调用，并启用独立的 Kun 初始化助手；第三步默认下载预构建镜像，也可本机构建或检查已有本地镜像，并在此保存设置、进入工作台。安装和构建失败可交给可追踪的 Kun 修复任务，验证过的修复保留版本化经验供以后复用。前置条件和自动发现范围见[启动与部署](docs/SETUP.md)。

发布者可在同一镜像的 offline 和 smoke 检查通过后上传镜像，再通过可编辑的公开镜像目录分发固定 Digest。**随包目录暂为空，须先发布真实镜像再登记地址**；本版不虚构官方镜像空间，也不宣称已完成匿名公网下载验收。操作见[镜像分发](docs/IMAGE-DISTRIBUTION.md)。

0.58.0 保持一套配方，提供**中国网络／国际网络**两组下载设置。本机构建可复用版本匹配的受管 Linux Codex 安装，或明确选择兼容的基础镜像，减少重复下载 Codex。OpenMontage 支持完整归档地址与 SHA-256 校验缓存；源列表补充阿里云 Debian 镜像。升级保留已有源配置。详见[构建运行时复用](docs/BUILD-RUNTIME.md)与[下载源配置](docs/BUILD-SOURCES.md)。

本源码包含运行与资源契约、统一观测模型、对话式轨迹阅读器、构建中心，以及共享同一任务核心的两个 API 入口：

| 入口 | 用途 |
| --- | --- |
| **RDCA v1** — `/api/v1` | RunDesk 自有的应用与管理 API；机器契约位于 `/api/v1/openapi.json` |
| **UHP** — `/v1` | 面向协议 `2026-10-19` 的附加服务入口；已实现并有[明确限制](docs/UHP.md)，不声明完整规范兼容 |

0.55.0 从 `recipes/<profile>/` 的普通文件读取构建配方，不再把配方 ZIP 嵌入可执行文件。构建中心可预览 Dockerfile、下载完整构建目录，并在构建前选择或修改下载源。源列表分为“国际（美国/官方为主）”和“中国镜像”，覆盖 pip、conda、npm、Docker、APT、YUM/DNF、Hugging Face、GitHub；配方须声明对应的构建参数。每次构建保存实际源设置快照。详见[下载源配置](docs/BUILD-SOURCES.md)与[升级说明](docs/UPGRADING.md)。

news2douyin、people-search 1.1.2 保留 0.54.1 的 Agent-Reach 本地源码及 wheel 打包修复。Codex 继续精确选版，其他依赖仍需相应下载通道。

0.54.0 整理实例导航与配置上下文，不改变存储和 API schema。继续保留 0.53.0 引入的持久任务截止时间、进度与审批交接、明确的执行能力声明和 [Python UHP 客户端](examples/uhp_client.py)。不支持的步数与输出 Token 预算仍明确披露，应用可在提交时要求必需能力。

RunDesk 独立运行，不依赖 HarnessRouter 服务。详见[当前状态](docs/STATUS.md)、[系统架构](docs/ARCHITECTURE.md)和[验证范围](docs/VALIDATION.md)。视频和研究应用是第二个使命的接入示例，各自配套版本放在[示例文档](examples/README.md)中维护。

## 快速开始

Linux amd64 与 Windows amd64 可运行包包含 RunDesk、独立 Kun 程序、`recipes/` 和 `config/`，宿主无需先装 Go、Node.js 或 npm。

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

将程序部署到源码目录之外时，还需把 `recipes/`、`config/` 放到可执行文件旁。其他配方目录使用 `RUNDESK_RECIPES_DIR`，自选源列表 JSON 使用 `RUNDESK_BUILD_SOURCES`。WebUI 继续内嵌，用户需要修改的构建资源改为外置文件。详见[部署](docs/SETUP.md)。

打开 <http://127.0.0.1:3210> 开始初始化。扫描对象是运行 RunDesk 的机器，也可能是远程服务器；可手工填写模型服务地址，暂不需要的 Codex 或 Docker 可以跳过。仅体验界面、不调用真实模型：

```bash
./bin/rundesk --data ./demo-data --demo
```

使用 Kun 时，先构建独立 Kun 项目，再指定其可执行文件：

```bash
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

本包只构建 RunDesk。`third_party/kun-sdk` 是轻量 SDK，宿主构建无需 Kun 内核源码；`your-org` 模块路径为未发布占位，由 `go.mod` 本地替换解析。缺少或不兼容的 Kun 会明确报错。

Kun 连接支持工具调用的 Chat Completions 服务；Codex 使用原生模型与认证配置。不同实例可以使用不同后端，已有会话保持原后端。Windows、Docker 和可选 Codex 安装见[启动与部署](docs/SETUP.md)。

## 按角色阅读

| 面向用户 | 文档入口 |
| --- | --- |
| 企业 Agent 落地开发者 | [调试](docs/DEBUG.md) · [观测与采集](docs/UNIFIED_DEBUGGING.md) · [只读调试 API/MCP](docs/DEBUG-SERVICE.md) |
| 应用开发者 | [应用注册](docs/APPLICATION-CONNECT.md) · [API](docs/API-V1.md) · [运行预设](docs/APPLICATION-RUNTIME-PRESETS.md) · [Python 示例](examples/application_quickstart.py) |
| Harness 开发者与研究者 | [Kun 接入](docs/KUN.md) · [Harness 控制](docs/KUN-HARNESS.md) · [研究 API 与历史资料](docs/README.md#3-harness-开发与研究--harness-research) |
| 维护者与部署者 | [架构](docs/ARCHITECTURE.md) · [构建中心](docs/BUILD-CENTER.md) · [下载源](docs/BUILD-SOURCES.md) · [升级](docs/UPGRADING.md) · [安全](SECURITY.md) |

本地开发可用 `make test` 运行宿主与 SDK 测试；`make check` 增加 race 检查及 `go vet`。部分集成测试需要独立 Kun 可执行文件。复验方法与历史结果分别见[验证说明](docs/VALIDATION.md)。

## 执行边界

- Codex HTTP 观测默认关闭，按会话开启。`--observe-http=true` 只改变新建 Codex 会话的默认值，不覆盖已保存设置。采集仅覆盖经过已配置代理的流量，不完整证据会明确标注。
- 托管 Docker 当前支持 Codex。Kun Bash 是显式启用的宿主执行，文件工具的路径限制不构成操作系统沙箱；Kun 容器托管仍待实现。
- 任务完成、工具调用成功与业务结果验收是不同事实。未知副作用不会被静默重放，记录也不会揭示模型未公开的内部推理。
- 三个使命不代表原生插件安装、任意后端模块或完整 UHP 兼容已经完成。已实现能力与后续工作见[当前状态](docs/STATUS.md)。

## 致谢与许可

感谢 [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG)、Michael Kinsy 及其贡献者、Mario Zechner / Pi，以及 [HarnessRouter](https://github.com/HarnessRouter/harnessrouter)。独立 Kun 项目保留选取的 PiG SSE 代码与原文 MIT 许可；HarnessRouter/UHP 为应用与后端边界提供设计参考。[上游来源](docs/UPSTREAM.md)区分实际引入的代码与设计参考。

[许可协议](LICENSE) · [第三方声明](THIRD_PARTY_NOTICES.md) · [许可证原文](LICENSES)
