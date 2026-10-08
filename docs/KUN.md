# Kun 0.12 / RunDesk 0.37.0

在 0.2 的 MCP 基础上增加 K1 核心模块与执行约束。采用已确认的结构：**分进程、同仓库、选择性复制 PiG 源码并自主发展**。本版包含模型／工具循环、MCP 配置与审批、四模块与两套内置组合、调用前参数校验、预算、运行记录、上下文检查和基础调试控制；不代表 KUN-DESIGN-v0.2 的所有阶段已经实现。

固定模块与预算见 [K1 核心说明](KUN-K1-CORE.md)。安全续跑见 [检查点恢复](KUN-CHECKPOINTS.md)。调试控制见 [条件断点与 Console](KUN-DEBUG.md)。调用证据与快照差异见 [检查说明](KUN-INSPECT.md)，四面板见 [结构化检查](KUN-PANELS.md)。已有 Codex/Kun 轮次、步骤、异常与统计的 [统一只读检查和 MCP](DEBUG-SERVICE.md)，已有 [独立诊断会话与建议审核](KUN-DIAGNOSIS.md)，以及 [K3-A 离线记录实验](KUN-EXPERIMENTS.md) 和 [K3-B/C Hybrid 与 Live 分叉](KUN-FORKS.md)；阶段状态见 [开发进度](KUN-PROGRESS.md)，新增 [K3-C 内置组合与 Harness 对照](KUN-HARNESS.md)，变更见 [0.37.0 发布记录](RELEASE-0.37.0.md)。

0.36 新增 [同基线跨会话对照](KUN-COMPARISON.md)，从已保存分叉预览读取来源和分支的固定宿主记录。没有新增模型调用、运行控制或 worker 协议变更。

0.37 新增 [单条工具结果假设与 Hybrid 重算](KUN-HYPOTHESES.md)：原录制保留，另存不可变替换；显式启动后记录真实使用证据。Kun 0.12.0 / 协议 v12 / fork schema 3，须一起升级两个命令；旧检查点/预览不能跨版本执行。

## 构建和启动

要求 Go **1.25.12 或更新兼容版本**。无需 Node.js；使用 Kun 时无需安装 Codex。

```bash
make build
# 仅当模型服务需要认证时，先在服务进程环境中设置 KUN_API_KEY
./bin/rundesk --data ./data
```

打开 http://127.0.0.1:3210。在配置页选择 **Agent 引擎**，选择 Kun，设置：
- OpenAI 兼容 API 基础地址，例如 `http://127.0.0.1:8000/v1`。
- 服务支持的模型名称。
- API Key 的环境变量名称，例如 `KUN_API_KEY`，而不是密钥内容；无需认证可留空。
- 是否允许写文件、每轮最多模型调用次数、请求超时、模型调用前断点。

保存后新建会话并发送消息。服务进程环境变更需要重启 RunDesk。配置按实例生效；已有会话固定后端归属，切换实例后端不会迁移旧会话。

默认在 RunDesk 可执行文件旁查找 `kun`（Windows 为 `kun.exe`）。开发时可显式指定 `--kun /absolute/path/to/kun`。两个二进制应来自同一源码版本。

Windows 可直接用 PowerShell 构建：

```powershell
$env:CGO_ENABLED = "0"
go build -buildvcs=false -trimpath -o bin/rundesk.exe ./cmd/rundesk
go build -buildvcs=false -trimpath -o bin/kun.exe ./cmd/kun
.\bin\rundesk.exe --data .\data
```

`--demo` 保留原有无真实模型调用语义，禁止启用 Kun。首次设置中的 Codex 安装检查可跳过；Kun 配置位于配置页的 Agent 引擎入口。

## 代码与进程边界

| 路径 | 职责 |
| --- | --- |
| `cmd/rundesk`、`internal/app` | 网页、授权、实例、会话、产物、任务队列及事件投影 |
| `internal/adapters/kun` | 启动独立 worker、握手和 JSONL 请求关联 |
| `internal/kunproto` | 仅共享可序列化的协议与状态类型 |
| `cmd/kun` | worker 命令入口、目录锁、stdin/stdout JSONL 协议 |
| `internal/mcp` | Kun 与管理端诊断共用的有界 MCP 传输；不依赖 PiG 运行时 |
| `internal/kun` | 模型调用、工具执行、状态机、控制命令、SQLite 记录 |
| `internal/web/kun.js` | 引擎配置和 DevTools 面板 |

每个加载的 Kun 会话对应一个 worker，普通会话可在同一进程中串行进行多轮对话；Hybrid / Live 分叉会话仅运行一次，不能普通续跑。任务并发仍由 RunDesk 队列控制。worker 空闲回收或 RunDesk 退出时关闭；不在后台继续充当独立守护进程。

Kun 独占 `<data>/kun/sessions/<sessionId>/state.db`，RunDesk 的业务数据库保存消费游标与事件投影。两侧独立事务，通过单调序号补取和去重衔接；不存在跨进程共享内存或跨库原子提交。

## 当前能力

| 能力 | 实现范围 |
| --- | --- |
| 模型 | OpenAI 兼容 Chat Completions；SSE 工具参数增量拼接，也接收完整 JSON 响应 |
| 工具 | `read_file`、`list_files`、可显式启用的 `write_file`；相对项目路径 |
| Skills | 扫描项目 `.agents/skills/*/SKILL.md` 和实例 Skills 目录；显式选取后注入，保存内容和 SHA-256 |
| MCP | stdio、Streamable HTTP JSON/SSE；每轮固定服务、权限和工具定义 |
| 会话 | 多轮文本历史；系统提示词按当前配置重新构建 |
| Network | 模型请求体、完整模型结果、工具参数与结果，以及 MCP initialize / tools/list / tools/call 请求与响应、耗时（均受大小上限约束） |
| Elements | 有序消息、实际请求/状态上下文区分、技能来源、工具定义、折叠原始证据 |
| Sources | 当前运行概览、审批、断点、待应用控制、控制记录、动作账本；原有暂停/单步/steer/停止/恢复；按状态禁用不可用操作 |
| Performance | 预算卡片、活动/等待分列、模型/工具/MCP 完成记录表及证据；不累加嵌套耗时，未知用量与费用不补零 |
| Application | MCP 服务状态表、工具审批模式与参数 Schema、所选时点审批；历史只读，尚无记忆存储视图 |
| Console | 结构化只读查询，使用当前状态或固定历史快照；预览并执行当前运行控制，不含自然语言诊断 |
| Layers | 四模块卡片、实现与状态结构版本、已记录事实和对应事件；无健康评分；支持普通运行安全点切换两套内置组合 |
| 预算 | 模型/工具调用次数、活动时间、连续工具失败、可选已报告 token 阈值；等待时间单独统计 |
| Hybrid | 最近已停止普通 Kun 运行的安全边界 → 固定预览 → 独立会话；模型重算，工具严格录制回放；继承预算，未命中停止 |
| Live | 固定预览与明确确认后，继承上下文/计划/预算并在当前项目真实执行；重新核验 MCP 目录与审批，独立执行记录但不隔离或回滚文件 |
| 记录 | 状态、事件、快照、命令回执、工具执行台账；API Key 字段不进入记录 |

不支持：Shell、PiG 插件/Node 扩展、图像输入、自动 Skills 激活、压缩/记忆管理、高级模块/费用断点、运行时替换工具结果、任意历史回滚、嵌套分叉、确定性代码生成、JEV/JIT、Docker worker。已有 Kun 独立自然语言诊断和宿主离线记录分支；已有普通运行安全点组合切换；四模块健康度、任意模块替换和迁移尚未实现；已有 Plan-Act 第二种 LoopPolicy 与两套内置组合，Layers 提供模块状态与固定快照对照，Application 目前仅覆盖 MCP。Codex 仍沿用现有后端，Kun 控制接口不会控制 Codex 的循环。

模型文本在请求结束后显示，目前没有逐 token UI。模型服务调用仍会发送任务上下文到配置的服务地址；“本地记录”不代表模型离线运行。

## 调试语义

- **暂停**：命令先持久化为 `queued`，在下一个模型／工具边界生效；不会冻结正在执行的 HTTP 请求。
- **单步**：在暂停状态允许一个模型调用或一个工具执行，随后在下一边界暂停；如果该步骤直接结束任务，则显示完成。
- **补充指令**：在下一个模型请求前应用。模型返回最终回答期间收到的补充指令会继续触发下一步；暂停时补充后再继续，下一次请求即包含它。
- **停止**：取消在途模型请求、终止 stdio 连接，对在途 MCP HTTP 请求尽力发送取消通知；服务端副作用不能保证停止或回滚，任务标记为 interrupted。取消请求也有独立的生效回执。
- **幂等**：相同 requestId 与相同内容返回既有回执；不同内容复用 ID 拒绝。调试 API 要求当前 runId 与 expectedStateRevision，陈旧状态拒绝，需刷新。
- **失联**：重开 worker 时，未完成运行标为 interrupted；已派发但无结果的工具标为 outcome_unknown。禁止自动重试或自动继续。待审批请求失效、连接标记关闭，后续新轮次会看到中断提示。
- **显式恢复**：失联或停止后可检查最近安全检查点并新建 run 继续；在途结果未知时拒绝，参见 [恢复范围](KUN-CHECKPOINTS.md)。
- **持久化失败**：停止 worker，RunDesk 结束当前任务并报告错误；不继续执行未被记录的下一步。

DevTools 每 1.5 秒刷新。选中调用后固定其快照，Elements/Layers/Application 共用该选择，切换与刷新不跳回现场；点击“跟随现场”清除固定选择。Sources 始终明确控制当前运行，不能控制历史快照。Elements 点击模型步骤时读取该步骤快照。当前只查询仍在线 worker 的完整快照；worker 回收后，RunDesk 轨迹保留模型请求/响应和工具事件。重新启动同一会话 worker 后可查询旧快照。本版不提供独立离线数据库浏览器。界面增量分页，每次刷新最多取 2 页；最多保留 20,000 条已加载 Kun 事件，另按 JSON 字符估算 64 MiB 截断，选中快照缓存最多 12 份。达到上限时明确提示部分证据可能缺失；API 可继续分页。各检查面板展开内容按身份保留，刷新保留滚动位置；Performance 固定快照不展示未来事件。快照差异只读，见 KUN-INSPECT.md。

删除 RunDesk 会话会关闭 worker，但保留其执行记录目录以便调查；本版没有自动清理这些目录的策略。状态与上下文默认保存在本机，包含任务内容；不要把真实 data 目录加入源码仓库。

## MCP 配置与权限

在 **工具 MCP** 页面保存服务。配置属于 Kun 实例，读写不启动 Codex；与 Codex 原生配置独立。可从原有 Codex 配置导出 RunDesk MCP JSON 再导入 Kun；导出的密钥为 `[redacted]`，新实例导入时需补齐实际值或改用环境变量引用。不会自动迁移或覆盖 Codex 配置。

同一实例的项目共用这份 Kun MCP 配置，stdio 进程的工作目录为当前任务项目。每轮开始读取一次配置，建立连接、完成初始化和分页工具发现；当前运行固定工具清单及权限，新配置下一轮生效。管理页面的 `configured` 只表示已经保存；DevTools Application 中的 `ready` 才表示该轮连接成功。轮次结束时关闭连接，下轮重新发现。

最小 HTTP 配置（服务地址与变量名替换为实际值）：

```json
{
  "url": "https://example.com/mcp",
  "bearer_token_env_var": "WEATHER_MCP_TOKEN",
  "startup_timeout_sec": 30,
  "tool_timeout_sec": 60,
  "tools": {"lookup": {"approval_mode": "approve"}}
}
```

最小 stdio 配置：

```json
{
  "command": "/absolute/path/to/mcp-server",
  "args": ["--stdio"],
  "env_vars": ["SERVICE_API_KEY"],
  "disabled_tools": ["delete_item"]
}
```

- **默认 / 每次询问**：工具进入持久化审批暂停点，聊天区和 Sources 均可允许或拒绝本次。继续和单步不能绕过审批。请求须匹配 `runId`、`expectedStateRevision`、`callId`。
- **始终允许（approve）**：下一轮开始后直接调用，不逐次弹窗。即使实例 `approvalPolicy=never`，显式允许仍然有效。
- **禁用**：不向模型提供该工具；`enabled_tools` 是允许清单（空数组表示全部禁用），`disabled_tools` 优先。
- **无交互审批（never）**：需要询问的工具直接返回拒绝结果，不等待交互。未实现 JEV 自动判断，`auto`、`writes` 和逐工具 `output_token_limit` 保存时明确拒绝，不静默降级。
- MCP 的 `readOnlyHint` 等 annotations 作为调试资料保留，不将其视为权限依据。

HTTP 支持静态请求头、`env_http_headers`、Bearer 环境变量引用，不跟随重定向。stdio 只继承 PATH、HOME、系统/临时目录、语言环境等基础变量，额外变量须通过 `env` / `env_vars` 指定。所有凭据引用在轮次开始解析，缺失则停止启动；已禁用服务不解析引用。服务命令不经过 shell 拆分。

凭据配置字段不进入 Kun 状态与上下文快照。已知模型密钥、MCP 请求头值及名称含 key/token/secret/password/credential/auth 的环境变量值，会对返回文本做精确匹配脱敏；这不是通用敏感内容识别，任意业务内容和未知密钥仍可能保存在轨迹中。RunDesk 业务配置数据库会保存显式填写的环境变量/请求头值；优先使用环境变量引用。

MCP 服务以 RunDesk 的操作系统账号在本机运行。`allowWrite=false` / read-only 限制的是 Kun **内置**文件工具，不会把外部 MCP 服务限制在项目内；允许 MCP 工具调用也可能允许其写文件或访问网络。Kun 本身无需 Node；用户配置的外部 MCP 命令可能依赖其他运行时。

## MCP 协议范围与故障处理

支持 MCP `2025-11-25`、`2025-06-18`、`2025-03-26` 握手版本的工具子集：initialize、initialized 通知、分页 tools/list、同步 tools/call。HTTP 保留会话 ID 和协商版本，POST 可返回 JSON 或 SSE；找到对应 RPC 响应即结束读取，不必等待服务关闭流。stdio 使用逐行 JSON。响应服务器 ping；其他服务器请求返回不支持，不宣告 sampling/elicitation 能力。

尚不支持旧版 HTTP+SSE 双端点传输、HTTP GET 长连接/断点续流、OAuth、MCP tasks、resources/prompts 操作、服务端 sampling/elicitation、listChanged 实时重载。HTTP 会话过期不自动重新握手或重试当前工具；下一轮新建会话。仅实现上述同步工具子集，不宣称完整 MCP 兼容。

模型可见名称由服务名、原工具名和哈希构成，实际调用保留原名。text、嵌入资源 text 和 structuredContent 进入文本模型；图片/音频/二进制内容只生成类型提示，有界原始结果保留在 Network 记录中。本版不是多模态模型接入。

| 边界 | 限制 |
| --- | --- |
| 保存的服务 / 每轮启用服务 | 100 / 16 |
| 单服务发现工具 / 所有服务向模型提供工具 | 256 / 128 |
| tools/list 分页 | 32 页；拒绝重复游标与重复名称 |
| 单次传输响应 / 保留结果 | 2 MiB / 512 KiB |
| 单工具 schema / description | 64 KiB / 16 KiB |
| 所有模型工具定义 | 512 KiB；仍受总上下文上限约束 |
| 启动/单次调用超时 | 可配置 0.1–600 秒；默认 30 / 60 秒 |

`isError=true` 是已知工具失败，保存为 failed 结果供模型处理。派发之后发生超时、断连、协议错误或结果超限，可能已经产生外部副作用：记录 `outcome_unknown`，停止该轮，不由 Kun 自动重试。同一工具在之后的新任务中仍可调用，用户应先核对外部结果。

停止 stdio 会终止并等待其进程退出；Linux/macOS 终止所属进程组（不保证约束主动脱离进程组的后代），其他系统当前只终止直接子进程。HTTP 取消通知是尽力而为；断开连接不能证明远端任务取消成功。

Network 记录 initialize、tools/list、tools/call 的业务请求/响应和耗时，不保留认证头、传输层通知或 DELETE 请求。Performance 展示条目耗时，MCP tools/call 与外层 tool 记录是嵌套关系，不能相加当作总耗时；不估算 MCP 价格。

规范参考：[传输](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)、[生命周期](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)、[工具](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)。

## 工具边界与限制

`os.OpenRoot` 将工具路径解析限制在工作区，拒绝绝对路径、上级穿越和逃逸符号链接；这不是隔离不可信本机进程的操作系统沙箱。工具只处理普通 UTF-8 文本，每文件最多 128 KiB；列目录最多 500 项。写入会覆盖目标内容，部分写入失败不会自动恢复原文件。

允许写文件是实例级明确授权，不逐个弹出审批。实例为 read-only 时强制禁用写入。Kun 不执行 Shell，也不将 Codex 的审批与网络配置宣传为自己的沙箱能力。

有效模型请求上限 2 MiB，模型完成结果上限 1 MiB，单请求流读取上限 8 MiB；达到限制时明确报错，当前没有自动截断或压缩。快照采用完整复制，尚无内容寻址去重和保留期管理，长会话可能占用较多磁盘。

## 协议与 API

worker 协议版本 7，每行一个 JSON 对象；stdout 仅输出协议，stderr 输出诊断。方法包括 `hello`、`start`、`state`、`events`、`snapshot`、`control`、`checkpoint`、`query`；`start.resume` 用于显式恢复。`hello` 宣告 `snapshotDiff=true`、`eventEvidence=true`、`resumeCheckpoint=true`、`fork=false`、`mcp=true`、`mcpApproval=true`、`diagnosticSession=true`。宿主离线记录分支不使用 worker fork，不改变该能力标志。最大消息为 8 MiB。升级时同时更新 RunDesk 与 Kun；旧版 worker 的握手会被拒绝。状态记录 schema 保持 1，新增字段为可选，旧记录可读取，但不匹配当前引擎版本或缺少恢复清单的记录不允许恢复。

HTTP 同时支持 `/api` 和 `/api/v1`：
- `PUT /instances/{iid}/agent-runtime`：管理员提交 `{revision, config}`。
- `GET /sessions/{sid}/kun/state`：当前状态。
- `GET /sessions/{sid}/kun/snapshots/{sequence}`：事件序号对应快照。
- `POST /sessions/{sid}/kun/control`：控制请求及 queued/applied/rejected 回执；set_breakpoints 替换当前 run 断点规则。
- `GET /sessions/{sid}/kun/query?kind=run&sequence=0`：只读结构化状态投影；正数 sequence 固定历史快照。
- `GET /sessions/{sid}/kun/checkpoint`：最近安全检查点资格；可重开离线 worker 读取记录，不调用模型或 MCP。
- `POST /sessions/{sid}/kun/resume`：提交上述 selection，v1 需 Idempotency-Key；新 run 沿用原上下文及预算，提交成功不代表重连校验或执行已经成功。
- 既有 `/sessions/{sid}/events`、SSE、轨迹和会话导出沿用，Kun 事件使用 `kun/*` 命名。

应用凭据仍受实例、项目和 read/run scopes 限制；控制要求 run scope，approve/reject 还要求 approvals scope；检查要求 read scope。管理配置只对管理员开放。

```json
{
  "requestId": "unique-command-id",
  "runId": "current-run-id",
  "expectedStateRevision": 8,
  "operation": "step"
}
```

精确请求结构见 `internal/kunproto/protocol.go` 和生成的 OpenAPI。

## 参考与后续

本版实际复制范围是 PiG 模型 SSE、MCP SSE 解码实现和一项模型协议回归测试；Loop、持久化、RunDesk 适配与调试控制由 Kun 自行实现。来源、固定版本、许可证与改动见 [UPSTREAM.md](UPSTREAM.md)。

本版已增加 Kun 内部 Provider/模块接口与统一工具派发入口；跨后端 AgentRuntime 抽取仍未完成。后续先验收实际模型/MCP、补记忆管理与剩余预算要求，K3-A、K3-B 和 K3-C 已有首批实现，继续补更多模块实现和迁移、跨会话基准与真实验收，最后进入 K4 轨迹归纳代码与去优化守卫。AgentJIT 仅为前期提供资料中的研究参考，本版没有实现或验证其效果。

验证范围、复现命令及已知测试时序问题见 [KUN-VALIDATION.md](KUN-VALIDATION.md)。
