# Kun + Agent DevTools：RunDesk 内置 Agent Loop 与调试器设计

RFC：KUN-0001 · 版本：v0.1 · 状态：建议稿，尚未实现  
日期：2026-10-04 · 基线：用户提供的 RunDesk v0.19.4 源码  
源码包 SHA-256：`2c6b9a9dfb8561e6d33958c6f0b1468acace8335242e0d6dc66fd8cf575b0674`

Kun 是 RunDesk 内置的、可检查、可干预、可组合的 Agent Loop 执行内核。RunDesk 为它提供会话、权限、任务调度、持久化和界面；Kun 为原生任务提供可控制的逐步执行，并为 Codex 等外部运行时提供有明确覆盖边界的观察视图。

Agent DevTools 是建立在统一调试协议上的交互工作台：Network、Elements、Console、Sources、Performance、Application、Layers 七个面板共享同一份运行、步骤和状态选择；同时提供供诊断 Agent 调用的结构化调试工具。它与 Kun 一起设计，但不要求所有被观察的执行后端都由 Kun 驱动。

沿用用户草案中的开发者工作台定位。代码包与配置标识用 `kun`，产品名称用 **Kun**。本文定义工程边界与验收条件，不代表已接入任何候选库，也不包含运行代码变更。

## 1. 先确定的设计决策

| 事项 | 建议决策 | 原因 |
| --- | --- | --- |
| 产品归属 | Kun 内置于 RunDesk，保持 Go 后端、HTML/JS 前端 | 复用已有管理能力与部署方式 |
| 执行关系 | Codex Adapter 与 Kun Adapter 并列 | 每个 run 只有一个 Loop 调度者，避免两层循环相互驱动 |
| 执行状态 | Kun 定义自身执行状态，由 RunDesk Store 保存 | 不接管应用会话和账号，但必须能表达下一步执行位置 |
| 技术依赖 | pi-go 放在可替换的 driver/provider 适配层 | Kun 公共协议不暴露第三方消息、会话或 Hook 类型 |
| 首选候选 | 优先验证 `sonnes/pi-go`，保留 `guanshan/pi-go` 备选 | 前者提供分层 SDK；最终选择取决于安全边界与恢复验证 |
| 观测事实 | 原始证据保留，状态按字段注明来源与缺失 | 不能从工具日志推断完整提示词、隐藏推理或确切执行状态 |
| 快照与恢复 | `Snapshot` 用于查看，`Checkpoint` 用于恢复 | 历史事件截取与可执行状态不同 |
| 模块替换 | 四模块返回声明式结果，由内核统一提交 | 模块不能各自开启循环、直接调用外部工具或扩大权限 |
| 编排策略 | 首版提供简单工具循环；另设可替换 `LoopPolicy` | 四个模块本身不能表达所有 ReAct、Plan-Act、Reflection 调度差异 |
| 优化 | 先记录与评估，再引入 Skill、快速路径和决策模型 | 有轨迹不等于已证明存在可泛化的确定性算法 |
| 兼容 | 缺少 runtime 字段的历史会话仍按 Codex 处理 | 不迁移、改写或自动重跑既有原生线程 |

## 2. v0.19.4 实际已有的基础

以下来自本次源码静态检查；没有执行完整测试，也没有连接真实 Codex、模型或 MCP 服务。

| 源码位置 | 现有实现 | Kun 接入方式 |
| --- | --- | --- |
| `internal/app/manager.go` | `Start/run/StopRun/Approve`、会话状态、`handle.client *rpc.Client`，直接调用 Codex RPC | 保留应用准入与会话协调；把后端调用收进 runtime adapter |
| `internal/app/steer.go` | `expectedTurnId`、`requestId`、提交去重、`turn/steer` | 保留 HTTP 语义；Kun 增加指令入队与实际应用回执 |
| `internal/app/queue.go`、`schedules.go` | 任务队列、并发限制、定时触发 | 继续由 app 调度；Kun 不再创建第二个业务队列或 Cron |
| `internal/app/instances.go`、`applications.go` | Instance 配置身份、应用绑定、配置修订 | 在现有应用设置中加入 RuntimeSpec 和 KunSpec |
| `internal/app/execution.go`、`environments.go` | local/docker 执行环境 | 与 codex/kun 后端选择分开；容器接入需要单独适配 |
| `internal/app/approvals.go` | 原生请求与允许选项校验 | Codex 路径保留；Kun 用中立审批对象接入同一审批 UI |
| `internal/store/store.go`、`batch.go` | SQLite objects/events、WAL；PutMany 仅事务写 objects | 新增事件、执行状态、动作账本的联合事务入口 |
| `internal/store/trace_snapshot.go` | 按来源会话和事件游标导出只读分析数据库 | 继续用于证据隔离，不当作 Loop checkpoint |
| `internal/store/trace.go` | 生命周期事件方法白名单 | 为 Kun 事件补查询与投影，不能只写入新事件名 |
| `internal/tracequery`、`internal/web/trace*.js` | 单一 Turn 时间轴、步骤详情、只读分析工具 | 复用现有阅读结构，增加步骤状态与检查点 |
| `internal/mcptest/client.go` | 有界、一次性 MCP 诊断客户端；不支持 sampling/elicitation 请求 | 提取传输经验，另补运行期连接管理、取消与请求路由 |
| `internal/app/mcp_tests.go` | 工具发现与独立测试；测试调用显式绕过 Agent 审批 | 不能把测试入口直接接为 Kun 的执行入口 |
| `internal/app/application_keys.go` | 外部应用访问 RunDesk 的凭证 | 与模型、Jina、Apify 等上游 API 凭证严格区分 |

旧版架构文档里部分“下一步”内容已经在代码中实现。本设计以当前源码为基线，不将这些能力重新列为 Kun 新功能。

## 3. 系统边界

```mermaid
flowchart TD
    UI["RunDesk UI / 应用 API"] --> App["app：会话、任务、授权"]
    App --> Port["AgentRuntime 接口"]
    Port --> Codex["Codex Adapter"]
    Port --> Kun["Kun Core / Driver"]
    Codex --> CS["Codex App Server"]
    Kun --> Model["模型 Provider"]
    Kun --> Action["统一工具执行入口"]
    App --> Store["RunDesk Store"]
    Kun --> Store
    Codex -. "观测证据" .-> Inspect["状态投影与检查器"]
    Kun -. "原生状态" .-> Inspect
    Store --> Inspect
```

**RunDesk app 持有业务状态。** 用户、应用、项目、会话身份、任务入队、Cron、访问控制、密钥解析、产物登记与总预算都留在 app。

**Kun 持有执行语义。** 当前阶段、上下文引用、待执行动作、控制命令、模块状态、尝试次数和停止条件由 Kun 定义。其持久化通过 RunDesk 提供的 Store 接口完成；单个 run 只有一个状态写入者。

**Driver 执行 Kun 的协议。** 采用 pi-go 时，不再使用它的 CLI 产品层，也不另开不受 RunDesk 管理的会话存储。只有安全点、工具执行入口和状态序列化经过验证的 driver，才能声明可干预与恢复能力。

如果 SDK 的 Hook 只能观察、不能在必要边界暂停或恢复，先复用其模型 Provider 层，由 Kun 实现小型 driver；不把一个不透明的 `Run()` 包装成“完全受控”。这是依赖验证的退路，不是预先决定重写整个 SDK。

Codex 的运行、停止、原生审批和已支持的 steer 继续通过原生接口。Kun 的观测层不负责决定 Codex 下一步做什么。

## 4. 后端、环境与模式分别配置

建议新增：

```json
{
  "runtime": {
    "kind": "kun",
    "profileId": "kun-default",
    "profileRevision": 1
  },
  "execution": {"mode": "local"},
  "kun": {
    "mode": "intervene",
    "harnessId": "tool-loop-v1",
    "harnessRevision": 1
  }
}
```

`runtime.kind` 表示执行者；`execution.mode` 表示所在环境；`kun.mode` 表示工作台启用的控制能力。三者不可混用。这里是拟议配置，不是现有 API。

配置入口在应用级，存储复用绑定的 Instance。无应用绑定的通用助手使用自己的默认设置。创建 Session 时固定 runtime 身份，启动 Run 时解析并保存模型、工具、策略、技能和 Harness 的有效版本；运行中的编辑不暗中改变这一版本。

| Kun 模式 | Codex 后端 | Kun 原生后端 |
| --- | --- | --- |
| off | 沿用原行为，不启动 Kun 增强采集 | 保留基础 Loop 与必要执行日志，关闭增强检查和优化 |
| observe | 已有 RPC 事件；可选 Hook/rollout 补充 | 增强状态检查，不开放调试修改 |
| intervene | Kun v0.1 不提供内部断点；原生控制继续可用 | 支持已实现并验证的安全点暂停、修改、继续 |
| optimize | 不支持改写其内部执行路径 | 在已验证的守卫与工具授权范围内使用快速路径 |

“关闭 Kun 就恢复旧 RunDesk”适用于未选择 Kun 后端的既有应用；原生任务不能关闭自己的执行内核后继续执行。切换应用默认后端只影响新会话，不把现有 Codex thread 原地变成 Kun 会话。

API 返回有效能力表，例如 `canSteer`、`canPauseAtBoundary`、`canResumeCheckpoint`、`canFork`、`canOptimize`，并附限制原因。能力由模式、adapter 版本、执行环境和当前授权共同决定；不以一个 `supportsKun` 布尔值代替。

## 5. 首个原生 Loop

默认策略采用简单工具循环：

1. 接收经 app 授权的运行输入，固定配置版本、预算与 run ID。
2. 在安全点消费控制命令与补充指令，提交新的状态 revision。
3. Capability 生成本步可用工具与 Skill 集合；Memory 构造模型上下文。
4. LoopPolicy 决定是否需要显式规划；默认不为每一步额外增加一次规划模型调用。
5. 保存 `before_model` 检查点并调用模型，流式显示文本；不执行尚未完整接收和验证的工具参数。
6. 完整保存模型响应与工具调用意图，逐个验证工具身份、参数、权限和预算。
7. 保存 `before_tool` 检查点；根据断点、审批或停止请求等待，批准后再次校验仍有效的执行条件。
8. 通过统一工具执行入口执行，保存结果与副作用状态；结果进入下一次上下文。
9. 如无工具调用且符合结束条件，提交最终回复；否则继续循环。达到预算上限时明确结束原因。

模型调用、工具调用、压缩和检查器等待各有独立的 step 类型。轮次结束表示执行结束；业务成功由应用验收或 evaluator 单独给出。工具未报错、模型说“完成”都不能自动证明业务成功。

首版工具调用串行执行。以后只有显式标注可并行、无依赖冲突的动作才并行；返回上下文时保持稳定的调用顺序。每个 tool call ID 对应一个最终结果；中断、拒绝和未知结果有独立状态，不能伪造成正常成功结果。

最小预算包含：模型调用次数、工具调用次数、估计/实际 token、累计费用（有价格依据时）、运行活动时间、单工具超时、连续失败次数。用户暂停时间单列，不当作模型延迟；队列占位与暂停超时由 app 管理。

## 6. 四模块与调度策略

| 模块 | 输入与输出 | 首版默认实现 | 明确边界 |
| --- | --- | --- | --- |
| Memory | 状态与预算 → ContextBundle、压缩建议、引用来源 | 历史窗口、项目笔记、显式 Skill、工具输出裁剪；超限摘要 | 不删除原始记录，不单独掌握会话真相 |
| Planning | 目标、已有计划、反馈 → PlanDelta / 下一步建议 | 无显式计划或简单计划 | 不要求展示模型隐藏推理；没有结构化计划时明确为空 |
| Action | 工具意图、结果 → 校验结果、执行/恢复建议 | 参数校验、错误分类、有限重试 | 模块输出由内核和宿主授权；不得自己旁路调用工具 |
| Capability | 应用许可、工具目录、Skills → 可暴露能力集合 | MCP + 内置工具；Skill 元数据检索及按需读取 | 只能在已有授权内筛选，Skill 内容不是权限授予 |

`LoopPolicy` 决定模块调用顺序和循环阶段，首版仅实现 `tool-loop-v1`，以后增加 Plan-Act 或 Reflection。否则“四个模块可替换”仍会被固定控制流限制。

模块实现声明 `id/version/stateSchemaVersion`，并提供序列化、恢复与兼容校验。所有输出先变成状态变更，由 Kun 串行提交。模块需要模型或工具时，向内核申请并进入同一事件、预算和授权通道。

替换模块在安全点创建新 Harness revision。存在未处理的模型调用或工具批次时不直接更换消息协议、模型或工具 schema；必须完成、取消或显式作废旧意图。不能通过改模块实现替换来继承原动作的审批。

## 7. 事件、Snapshot 与 Checkpoint

### 7.1 事件是证据，状态是主要阅读入口

保留现有 `events` 表及原始 Codex envelope。新增 `kun/*` 生命周期事件与中立投影，不制造看似来自 Codex 的原生通知。前端轨迹查询、后端 `tracequery` 和只读导出同时扩展。

建议的事件扩展字段：

- `schemaVersion`、`sessionId`、`runId`、`turnId`、`stepId`、`parentStepId`、`attempt`。
- `runtimeKind`（codex/kun）、`engine`（如 pi-go）、`captureMethod`（rpc/hook/rollout/provider/native）。
- `module`（memory/planning/action/capability/runtime），允许模块为空或有辅助标签。
- `stateRevision`、`snapshotId`、`checkpointId`、`sourceEventIds`。
- `occurredAt`、`receivedAt`、`sourceSequence`；延迟回填不能只按接收时间假装是现场顺序。

建议事件：`kun/run.started`、`kun/context.built`、`kun/model.started`、`kun/model.completed`、`kun/tool.prepared`、`kun/tool.finished`、`kun/checkpoint.committed`、`kun/control.applied`、`kun/run.finished`。文本 delta 是传输片段，不为每个 token 创建完整快照。

Hook 与 RPC 捕获同一次调用时，优先按原生 thread/turn/item/call ID 归并。缺少可靠标识时保存候选关联及不确定性，不靠时间接近就合并或重复计入工具次数。

### 7.2 Snapshot：可检查的状态视图

| 状态域 | 记录内容 |
| --- | --- |
| 上下文 | 有序消息引用、角色、项目笔记版本、Skill 内容哈希、压缩来源、实际发送的请求视图 |
| 能力 | 本步暴露的工具定义、schema 哈希、服务状态、请求策略与有效策略 |
| 计划 | 结构化目标、计划版本、未完成步骤；无法获取时注明缺失 |
| 执行 | phase、step、attempt、待处理工具调用、预算、用量、停止原因 |
| 环境 | workspace/environment 引用、代码版本、受跟踪产物清单；不冒充文件系统完整快照 |
| 来源 | 字段级证据 ID、采集时间、覆盖范围、脱敏与截断标记 |

每个字段至少区分 `observed`、`reconstructed`、`inferred`、`missing`，另记 `redacted/truncated`。`confidence` 只用于可解释的诊断推断，原始事实不人为分配 0.9 等分数。

Kun 可以精确记录自己在客户端构造并发出的请求，但不能据此声称知道模型服务端内部上下文或隐藏推理。Codex 仅展示实际获得的数据；本地 vLLM 请求日志也需要明确其捕获点与字段覆盖。

状态在不可变消息块、工具定义块和技能块上保存引用，按边界保存 manifest；避免每一步复制完整历史导致存储二次增长。迟到的观察证据生成新的投影 revision，旧分析仍能按原游标与版本复现。

### 7.3 Checkpoint：可继续执行的承诺

Checkpoint 引用一个 Snapshot，并额外保存：

- driver 与模块的版本、序列化状态、下一执行阶段。
- 已消费的控制命令游标、待执行工具列表及每个调用的账本状态。
- 模型/工具/Skill/Harness 的版本与内容引用。
- 待处理授权的绑定信息、剩余预算、环境兼容条件。
- `resumeEligibility`（supported/conditional/unsupported）及原因。

首版稳定边界是：模型请求前、完整模型响应提交后且工具执行前、工具最终结果提交后、压缩提交前后。运行中网络连接、流式 token 中间位置和任意 goroutine 栈不承诺可恢复。

内核事件、state revision、动作意图或动作结果、检查点指针通过 Store 的一个本地事务提交。大内容先持久化成不可变块，再提交引用；事务内不执行网络或工具操作。Codex 原生路径可继续保留既有写入方式。

这只能保证本地一致性，不能使外部 API 和 SQLite 成为同一个事务。

## 8. 暂停、Steer 与分叉

### 8.1 安全点控制

建议使用每 run 单写入者和控制命令队列。模型/工具 I/O 可在受管理的工作协程运行，状态修改只由主执行者提交。等待 I/O 时仍要接收停止和暂停请求，不能持有阻止控制接口的全程互斥锁。

外部会话状态尽量沿用当前 `running/waiting/stopping/...`；把 `waitReason=approval/debugger/user_input` 及内部 phase 单列，避免所有队列代码都依赖新的顶层状态值。暂停继续占用该 session 的在途资格。

控制操作包含：`pause`、`resume`、`step`、`steer`、`patch_context`、`replace_tool`、`cancel`。每个操作携带 `requestId`、预期 run/turn、`expectedStateRevision`，并记录 `queued/applied/rejected` 与实际应用 step。相同 ID、不同请求内容返回冲突。

| 情况 | 规定行为 |
| --- | --- |
| 模型正在流式输出时暂停 | 返回“正在请求暂停”；在下一个安全点进入 paused，不宣称立刻冻结模型 |
| 工具执行中暂停 | 停止派发后续工具；等待当前调用结束或报告取消结果 |
| 发送 Steer | 入队后显示已接收；安全点应用后显示已生效；不等同于已影响刚发出的模型请求 |
| 单步 | 从暂停点执行一个定义明确的模型/工具/压缩步骤，再停下；不是一个 token |
| 修改待执行工具参数或替换实现 | 作废旧审批，重新校验 schema、权限、预算并生成新动作身份 |
| 状态已变化 | 返回 revision 冲突；不把过期页面的控制应用于新的任务 |
| 取消外部调用但结果不可确认 | 标记 outcome_unknown；不把“本地连接关闭”等同于“远端未执行” |

新指令在模型返回后、工具执行前生效时，必须显式保留或作废原工具意图。作废的 call ID 需要完整的取消结果记录，不能静默丢掉半个消息协议。

### 8.2 回放、继续与分叉是三个功能

| 功能 | 是否执行模型/工具 | 说明 |
| --- | --- | --- |
| 轨迹回放 | 否 | 读取已有状态与事件；Codex 和 Kun 都可用 |
| 检查点继续 | 是 | 仅恢复兼容且允许恢复的 Kun checkpoint |
| 分叉实验 | 可能 | 新建 session/run，关联来源 checkpoint；明确模拟与真实执行模式 |

Fork 默认先生成预览，列出上下文差异、工具版本、环境差异和将发生的外部动作。模拟模式复用已记录的工具结果或测试 fixture，明显标注模拟数据；真实执行模式仍通过当前授权与动作账本。

对 Codex 轨迹可做“提取任务后在 Kun 新运行”的迁移实验，但这不是从 Codex 隐藏状态精确恢复。不要把它标为同一个 checkpoint 的重执行。

恢复历史上下文不能恢复外部世界：数据库、发送过的消息、远端任务不会因分叉而回滚。代码版本、文件快照与隔离工作区可作为后续独立能力，检查点首先说明自己覆盖什么。

## 9. 工具、权限与崩溃恢复

所有动态 Loop、编译路径、模块调用都经过同一个工具执行入口：工具解析 → schema 校验 → 有效授权 → 动作记录 → 执行 → 结果记录。不能通过 JIT 或工具替换跳过其中任一步。

沿用 v0.19.4 的用户交互：默认策略、始终允许、每次询问、禁用。但 Kun 必须自己执行对应规则；写入 Codex 原生 `approval_mode` 不会自动约束 Kun。

定义 Kun 的显式策略映射；旧 `auto/writes` 或其他未知策略没有可靠映射时要求配置解析或返回不支持，不悄悄降为允许。MCP 工具注释只作为信息来源，不能单独成为扩大权限的依据。

“始终允许”在规则仍匹配、授权仍有效时免去重复提示。审批绑定应用、工具身份与 schema、参数指纹、执行环境、策略 revision 和授权范围。改变参数、实现或越出范围必须重新判断。授权撤销在新动作派发前生效。

动作账本记录 `prepared/dispatched/succeeded/failed/cancelled/outcome_unknown`，并包含 invocation ID、attempt、幂等能力和远端回执。崩溃发生在远端完成与本地结果提交之间时，恢复为 outcome_unknown：

- 有远端幂等键或可靠结果查询时，先核对再决定是否继续。
- 明确只读的操作可按应用策略重试，但仍记录额外成本与数据时效变化。
- 不具备幂等保证的写操作不自动重发，进入恢复处理。

HTTP 提交幂等键只解决“任务重复提交”，不能证明工具副作用恰好执行一次。

权限策略也不等于系统沙箱。Kun 首版不自动获得 Codex 的文件系统/网络隔离；本地工具必须声明执行边界。Docker 初期未适配时返回明确不支持，不回退到宿主机执行。执行不可信生成代码的进程隔离与资源限制应由专门 executor 承担。

## 10. 模型与凭证

模型配置使用 ProviderProfile：协议类型、endpoint、model、credentialRef、能力标记和 revision。首版验证一个 OpenAI 兼容工具调用协议；兼容性以实际服务验证为准，不因接口路径相同就承诺所有模型可用。

多模态文件保留内容块与文件引用；服务不支持图像/文件时明确拒绝相关输入或按已配置的转换流程处理，不能静默丢掉附件。模型请求、输出 token、缓存用量和费用分别记录；缺少用量时标记 unknown/estimated。

上游凭证由宿主解析，通过必要作用域注入 Provider 或工具。Kun 状态只保存引用与版本，不保存 API key 明文。首版可以采用明确绑定到应用的服务端环境变量引用；通用加密凭证仓库作为单独工程项。

完整调试请求和工具输出按会话访问权限保护；默认排除认证头、密钥与敏感环境变量。预览脱敏与内部恢复内容引用分开，不能用脱敏后的请求冒充原始精确输入。展示 capture policy、redaction 和 truncation 标记。

## 11. pi-go 选型：先验证适配点

本次查阅的是项目公开说明，尚未锁定 commit、审计代码或完成集成测试。

| 候选 | 已核实的公开说明 | 对 Kun 的含义 |
| --- | --- | --- |
| `sonnes/pi-go` | 提供 ai/agent/durable/session 等分层；配置在构造后不可变；有生命周期 Hook | 优先进行嵌入验证；热修改需要由安全点重建或 adapter 承担 |
| `guanshan/pi-go` | 包含 ai/agent/coding-agent 分层；README 明确 alpha、移植中 | 可做备选；不能把它描述为已完全兼容 TypeScript 原版 |

选型门槛：

1. 所有模型和工具调用能经过 Kun 的记录、预算和授权入口。
2. 模型前、工具前和压缩边界可以暂停，暂停时仍能停止。
3. 状态可序列化且恢复语义明确，不依赖不可导出的私有对象。
4. 单次 Run 内的补充指令不会被静默变成新的业务任务。
5. 待执行调用及结果未知情况不会被库自动重跑或掩盖。
6. 仅引入需要的包，不引入 CLI/TUI 产品层或第二份会话数据库。
7. 锁定 commit 与许可声明，并以契约测试防止升级改变以上行为。

如果原生 agent 层无法满足上述条件，仍可评估其 ai 层，Kun 自己掌握小型驱动循环。选型结果通过一份短 ADR 固定，而不是在设计草案中提前宣称完全受控。

## 12. Codex 捕获：已有 RPC 为主，其他路径补充

RunDesk 已拥有 App Server 请求、响应和通知，首版优先复用。Hook 和 rollout watcher 只补缺失证据；不把安装额外采集器作为开始设计的前置条件。

Hook 支持与信任规则要按用户实际安装的 Codex 版本探测。观测 Hook 不阻塞工具等待用户调试；可干预 Hook 如果未来启用，必须单列模式、超时与能力，不能隐藏在 observe 中。

Rollout watcher 需要增量游标、文件轮转与半行处理、去重、会话归属和迟到回填。访问范围限定为应用已授权的线程，不能默认扫描整个用户历史。

额外采集失败时记录覆盖缺口，观察层降级，既有 Codex 运行路径继续按其原有规则工作；这不改变 v0.19.4 已有的核心事件日志失败处理。原生 Kun 的恢复日志落盘失败则停止派发新的副作用动作。

“无需改用户 Agent 代码”是适配已有后端的目标，不是对任意黑盒 Agent 都能获得完整内部状态的保证。

## 13. Agent DevTools 产品、协议与调试工具

### 13.1 七个面板的共同基座

保留当前按 Turn 纵向展开的轨迹页，从“调试”入口展开 DevTools。桌面采用可调整大小的详情区域，移动端采用全屏单面板；不在主对话区同时铺开七块内容。

所有面板共享 `session/run/step/snapshot/stateRevision` 选择。点击 Network 的一次模型调用，Elements 定位到该调用实际使用的上下文，Layers 定位到当时的模块状态，Performance 高亮同一次调用。需要明确区分“跟随现场”和“固定历史快照”；查看历史时，新事件不抢走当前选择。

公共状态栏显示：后端、模式、正在运行/请求暂停/已暂停/等待审批、观察覆盖、当前检查点、预算。只有 adapter 声明可用的操作才可执行；无能力时给出原因。原始 JSON 留在折叠区。

Chrome DevTools 是交互方式的隐喻，不意味着能够直接看到概率模型的真实内部决策过程。这里展示的是决策前可见输入、动作及可验证的状态变化；“为何这样做”的诊断必须附证据并标明推断。

### 13.2 七面板首版合同

| 面板 | 用户要回答的问题 | 主要内容与交互 | 对执行的影响 |
| --- | --- | --- | --- |
| Network | 调了什么、发了什么、返回什么？ | 模型/工具/MCP 列表与耗时；请求、响应、参数、输出、错误、重试、usage；关联快照与原始证据 | 查看无副作用；重新运行只能进入显式重放流程 |
| Elements | 这一步模型实际看到了什么？ | 有序上下文树：system/developer/user/tool、记忆、Skill、工具定义；token 分布、截断/压缩、前后差异与来源 | 编辑生成上下文补丁；在安全点或新分支应用，原记录不改写 |
| Console | 现在能查什么、怎样干预？ | 结构化只读查询、自然语言诊断、控制提案、操作回执；错误可跳转到对应步骤 | 查询与修改分离；修改调用同一控制协议 |
| Sources | 在哪停住、下一步是什么？ | LoopPolicy 阶段、模块版本、工具定义与相关 Skill；断点、单步、继续、检查点 | 原生 Kun 按安全点阻塞；外部后端按能力限制 |
| Performance | 时间与费用花在哪里？ | 模型/工具瀑布、等待与活动时间、重试、token/缓存、压缩、用量与费用归因 | 基础性能展示首版提供，不依赖 JIT |
| Application | 当前持久状态是什么？ | 会话/项目记忆、压缩/淘汰谱系、Skill 版本、MCP 连接、产物和检查点；凭证只显示引用与状态 | 持久修改沿用应用权限，明确生效范围和 revision |
| Layers | 哪个模块出现异常？ | Memory/Planning/Action/Capability 的输入输出、状态、调用关系、规则信号和对比 | 首版检查，后续模块替换形成新的 Harness revision |

Network 展示语义调用，不承诺抓取任意工具内部所有 HTTP 请求。一次 MCP 工具调用就是一个工具 span；其下游网络只有提供额外证据时才展开。工具结果完整内容与有限预览分离，实际未捕获/已截断的数据不得用“查看完整”误导。

Elements 区分“被发现的 Skill”“被选中的 Skill”“已读取的 Skill”“实际注入模型的 Skill”，不把存在于目录中等同于生效。Codex 原始数据不足时显示字段缺口；Kun 的精确范围是自身构造并发送的客户端请求。

Sources 首版是 Loop 阶段调试，不是 Go 任意源码行断点。Memory、Planning 等自定义代码内部若没有显式安全点，只能停在模块边界；未来外接 Delve 等代码调试能力另立设计。

Application 的“当前存储”与 Elements 的“当时输入”必须分开。修改项目记忆不会改变历史快照；记忆已经存入持久层，也不证明某次模型请求实际注入了它。

Layers 首版给出带证据的规则信号，例如同工具同参数重复失败、计划在新目标下没有更新、上下文预算溢出、工具发现失败。信号包含 rule ID、规则版本、事实、解释和误报反馈；不能仅凭相关性宣称 Planning 必然导致 Action 的错误。

### 13.3 断点与单步

断点是有版本的声明式规则，作用范围为 session/run；本轮应用时固定版本，临时修改须有回执。首批支持：

- 模型调用前/后、工具执行前/后、压缩前/后。
- 指定工具 ID、模型或模块；指定错误类别。
- 同一工具及参数指纹连续失败达到阈值。
- 上下文使用量、累计费用或剩余预算达到阈值。
- 自定义结构化信号满足类型化条件。

条件使用受限的字段、比较与逻辑运算，不在调试服务器执行任意 JS、Go 或 shell。断点调试与工具审批是两个独立等待条件：点击继续只解除断点，不能顺带批准工具。

`onLowConfidence` 只有在某个明确的决策模块提供可解释、经过校准或明确标为启发式的信号时才可用。不能把模型文字中的“我有 90% 信心”当作系统概率。JEV 不存在时，其余断点照常运行。

“错误时暂停”适合作为开发模式的可选预设，默认不应用于 Cron/无人值守任务，避免后台任务无限占位。设定暂停超时与人工处理策略，超时停止或保持等待由应用明确配置，不擅自继续有风险的动作。

单步的单位是一个模型、工具或压缩步骤。未来若支持工具并行，设置断点后需描述哪些调用已在飞行；暂停完成条件为不再派发新动作且在飞行结果已被核对，不能声称冻结已发往远端的操作。

### 13.4 Console 与 Agent Debug Tools

调试能力提供统一的 DebugService，HTTP/UI 和只读 MCP 工具调用同一实现。已有 `internal/tracequery` 与来源快照限制可以逐步扩展，不另建一套拥有全库权限的调试服务器。

| 工具组 | 拟议工具 | 返回/行为 |
| --- | --- | --- |
| 运行与能力 | `debug_get_run`、`debug_get_capabilities` | 当前状态、有效能力、版本、可用安全点 |
| 调用证据 | `debug_list_calls`、`debug_get_call` | 有界查询、原始证据引用、请求/响应及分页 |
| 状态 | `debug_get_snapshot`、`debug_get_context`、`debug_diff_snapshots` | 固定版本状态、真实上下文、变更来源 |
| 分析 | `debug_get_performance`、`debug_get_layers` | 时间/费用与模块状态、规则诊断 |
| 检查点 | `debug_list_checkpoints`、`debug_get_checkpoint` | 恢复资格、依赖版本、未知副作用 |
| 修改提案 | `debug_propose_patch`、`debug_preview_fork` | 无副作用的差异、目标、预期执行清单与提案 ID |
| 运行控制 | `debug_control` | 暂停/继续/单步/停止/steer 的受权命令与回执 |
| 应用与分叉 | `debug_apply_patch`、`debug_create_fork` | 校验提案及当前 revision 后修改未来执行或创建新运行 |

这些是拟议工具名，不表示本次已安装。实际首版可只暴露前五组及提案；可变更工具按 app 授权和运行模式独立开放。

普通诊断 Agent 默认获得固定来源范围的只读工具；自然语言 Console 使用独立诊断会话，成本单列。它查询目标 run，不把解释问题写入目标 Agent 的上下文，也不让目标 Agent 因等待自己提供调试服务而死锁。

自然语言指令如“换一个搜索结果试试”，先解析成类型化提案并展示目标、差异和执行模式；在已有授权足够时可应用，否则按当前权限规则处理。结构化查询/控制不依赖 LLM；JEV 是未来可选决策优化，不能充当控制台权限判断器。

工具授权细分为 `debug.read`、`debug.control`、`debug.patch`、`debug.fork`、`debug.optimize`。每次访问校验 app/project/session 归属；read 权限不推出文件写入、system prompt 编辑或真实重执行权限。

控制与修改都带 request ID、目标 run/turn、expected revision、操作者及原因；修改成功返回旧/新 revision、应用 step、checkpoint 和审计事件。历史快照不可原地编辑；工具输出替换标为 synthetic，保留原返回，并禁止冒充原服务响应。

### 13.5 时间旅行的三种模式

| 模式 | 具体语义 | 新的模型/工具调用 | 边界 |
| --- | --- | --- | --- |
| Deterministic / 记录编辑回放 | 查看原记录；补丁形成新分支，依赖被改结果的下游标为 stale | 无 | 不是重算概率模型，更不代表新路径已经验证成功 |
| Live / 真实重执行 | 从可恢复 checkpoint 创建新分支，重新执行之后的模型与工具 | 有 | 重新授权，可能产生费用与外部副作用；不能承诺输出相同 |
| Hybrid / 模型重算与工具录制回放 | 重新调用模型，工具结果由录制数据或显式 fixture 提供 | 模型有，工具不真实执行 | 命中严格匹配才复用；未命中停下并报告，不自动转为 Live |

Hybrid 的匹配键至少包含工具身份/实现版本、schema 哈希、规范化参数指纹、相关环境与 fixture 版本。同一名称、不同 URL 或不同查询条件不能复用同一个结果；重复调用还需要调用位置或调用序列规则消除歧义。

如果新路径要求未录制的动作，返回 `replay_miss`，由操作者补充 fixture 或显式创建 Live 分支。纯录制回放无需新增模型/工具调用；不要用“免费”掩盖存储、计算或原有调用成本。

每条分支记录来源 checkpoint、patch、模式、真实/模拟 span、工具副作用策略和预算。diff 同时比较上下文、计划、调用、结果、产物、费用与业务验收；含 stale 或模拟数据的分支不能标成通过真实集成验证。

### 13.6 本地优先与框架无关

调试数据默认保存于当前 RunDesk 部署的本地数据库及文件目录，不要求接入外部 tracing SaaS，不默认上传第三方遥测。这里的“本地”是 RunDesk 所在机器；如果部署在云端，就不是操作者桌面。

历史浏览、结构化查询、记录回放与状态 diff 可离线运行。调用远程模型/工具、自然语言诊断则按其 endpoint 联网；不能把“调试记录本地存储”宣传为整个 Agent 数据从不离开机器。

定义框架中立 DebugEvent、Snapshot、Checkpoint 和 CapabilityReport。原生 Kun 直接产生数据，Codex adapter 输出已有证据，未来可增加 JSONL/OTel 导入。导入 OTel 或一份日志不自动获得暂停、恢复或精确上下文能力。

所谓透明捕获，是对已集成的 RunDesk 后端无需用户改业务逻辑。其他语言/框架可能需要 SDK wrapper、装饰器或导入适配；声明适配成本，不对所有 Agent 承诺零改动接入。

### 13.7 Performance 的核算规则

性能面板从 K1 提供基础版本，与编译优化解耦。时间分别统计排队、模型首 token/完整响应、工具执行、审批等待、调试暂停、压缩和本地调度；并行任务区间重叠时，总耗时不能简单相加。

费用保留 Provider 的实际 usage、缺失标记、缓存读写分类、价格表来源与版本；模型自报 token、SDK 估计和服务端实际结算分别标记。工具费用未知时显示未知，不当作零；不能把录制结果所节省的工具费用当成实测账单。

现有 v0.19.4 usage 汇总与 Kun 明细需按调用 ID 去重。是否启用 JIT/JEV 是可比较的运行配置，面板本身不是优化器；同一套指标服务优化前后的评估。

### 13.8 HTTP 与事件协议

拟议资源，保持现有 API 版本化与 SSE 通道：

- `GET /api/v1/sessions/{sid}/runtime-capabilities`
- `GET /api/v1/sessions/{sid}/runs/{rid}/calls?after=...`
- `GET /api/v1/sessions/{sid}/runs/{rid}/snapshots?after=...`
- `GET /api/v1/sessions/{sid}/runs/{rid}/checkpoints/{cid}`
- `GET /api/v1/sessions/{sid}/runs/{rid}/performance`
- `PUT /api/v1/sessions/{sid}/runs/{rid}/breakpoints`
- `POST /api/v1/sessions/{sid}/runs/{rid}/controls`
- `POST /api/v1/sessions/{sid}/runs/{rid}/patch-previews`
- `POST /api/v1/sessions/{sid}/runs/{rid}/patches`
- `POST /api/v1/sessions/{sid}/runs/{rid}/fork-previews`
- `POST /api/v1/sessions/{sid}/runs/{rid}/forks`

示例控制请求（拟议契约）：

```json
{
  "requestId": "debug-command-0001",
  "expectedTurnId": "turn-001",
  "expectedStateRevision": 42,
  "operation": "pause",
  "boundary": "before_tool",
  "reason": "inspect search arguments"
}
```

HTTP 接收成功只表示命令已入队，响应带 command ID 和 queued 状态。SSE 的 `kun/control.applied` 才说明在哪个安全点生效；客户端断线后可查询命令最终状态，不需要重新提交。

补丁请求额外固定 base snapshot、补丁字段、scope（本 run/新分支/应用配置）及预览版本；基础版本改变时重新预览。审批、断点、补充用户输入各有独立 ID 和等待原因，不能共用一个含糊的“继续”回调。

现有开始、停止、steer、审批入口继续保留，路由至所选 adapter。所有端点和工具采用同一 DebugService 授权、审计、分页与内容大小限制；知道 checkpoint/blob ID 不等于有跨应用读取权限。

## 14. 代码落点与迁移顺序

以下是拟议目录，尚未添加实现：

| 目录/文件 | 职责 |
| --- | --- |
| `internal/agentruntime/` | 运行入口、流、控制命令、能力描述的中立契约 |
| `internal/adapters/codex/` | 收拢现有 Codex 调用和事件翻译 |
| `internal/kun/` | 执行状态、LoopPolicy、安全点、四模块协议 |
| `internal/kun/driver/` | pi-go 驱动适配，依赖被封装在这里及 Provider 适配层 |
| `internal/kun/provider/` | 模型调用、流式内容、用量与能力协商 |
| `internal/kun/toolbridge/` | MCP 和内置工具接入，统一调用宿主授权与执行服务 |
| `internal/kun/inspect/` | Snapshot、覆盖率、状态差异、诊断投影 |
| `internal/debugservice/` | 跨后端查询、断点、控制、补丁、重放的应用服务 |
| `internal/debugtools/` | 供诊断 Agent 调用的只读/受权调试工具，复用 tracequery |
| `internal/app/kun.go` | 配置解析、宿主服务注入、控制与原有会话状态映射 |
| `internal/store/kun.go` | 联合提交、检查点、动作账本、内容块存储 |
| `internal/web/devtools/` | 七面板及共用选择状态，复用轨迹页，不引入大型 SPA |

AgentRuntime 的最小契约负责 Start、事件流、Control、能力说明和资源关闭；检查点能力另作可选接口。不要让该接口依赖 `rpc.Message`，也不要求 Codex 实现它不具备的检查点方法。

先抽出最小边界并让 Codex 行为保持一致，再接 Kun。特别检查：`handle.client`、`Approval.Request`、Steer、原生历史导入、trace analysis、配置连接、空闲回收和进程名额。配置连接仍可能需要 Codex；Kun 专属的 Provider/MCP/Skill 配置不能为了读取配置而隐式启动 Codex。

现有并发限制复用，但“Codex 活跃进程数”与“Kun 活跃 run 数”分别统计。暂停仍占据会话资格；应用总并发、工具子进程名额和模型连接数有各自限制，不能把 Kun 塞进旧进程计数后绕过容量控制。

数据库做追加式迁移，保留历史事件；旧字段缺省为 Codex。导出、恢复、清理与删除同步处理新内容块的引用关系。降级旧二进制是否可读需单独验证，不承诺任意版本自动回退。

## 15. 分阶段验收

里程碑编号为 Kun 设计阶段，不预占 RunDesk 下一个发布版本。

| 阶段 | 交付 | 验收 |
| --- | --- | --- |
| K0：适配边界 | AgentRuntime 契约、Codex adapter、pi-go 依赖验证 ADR | 现有开始/停止/steer/审批/队列行为回归；候选 driver 通过关键控制验证 |
| K1：最小原生运行与检查 | Provider、简单工具循环、串行执行、预算、动作账本；Network/Elements/基础 Performance；只读 debug tools | 不启动 Codex 的应用完成模型→工具→最终回复；面板联动定位实际上下文；缺失与未知结果正确标记 |
| K2：基本干预与恢复 | Sources/Console、断点、单步/steer、检查点继续、控制幂等；Application 基础只读状态 | 暂停时可停止；过期控制被拒；工具执行前后崩溃不会导致未知写操作自动重跑 |
| K3：组合与分叉实验 | Layers、四模块版本化、第二种 LoopPolicy、Harness 对比；Deterministic/Live/Hybrid；Application 谱系与配置修改 | 更换模块后差异可追溯；Hybrid 未命中必须停止；stale/模拟/真实结果分明；本地历史调试可离线 |
| K4：优化实验 | Skill 候选、快速路径 IR、守卫、去优化、可选决策模型 | 独立验证集达标才启用；偏离条件正确回退且不重放已完成副作用 |

K1 内置最小固定四模块，不等待抽象完美才开始原生闭环。K2 的控制入口从 K1 架构中预留，避免后来改造主循环；完整可配置模块和分叉实验可以后置。

测试分为两种：使用可编排 fake Provider/MCP 的确定性契约测试；使用用户实际模型和工具的集成验收。前者通过不能冒充后者通过。

第一条业务验收建议选 news2douyin 的材料准备子任务：查询一个选定新闻事件、获取相关网页、形成带来源的事件时间线与稿件材料并保存。验证“搜索已配置始终允许时不重复提示”“运行中补充筛选条件”“工具失败后可检查并继续”。视频渲染与发布不作为第一条 Loop 的依赖。

DevTools 专项验收还包括：点击一个调用后七面板采用同一 snapshot；浏览历史不随新事件跳转；手动暂停与审批互不代答；诊断会话不污染目标会话；补丁保留原记录；低置信度信号无来源时不可设置该断点；无 Provider usage 时费用不显示为已知零；关闭增强调试不插入额外模型调用，也不保留活动断点。

## 16. 优化层的研究边界

Loop JIT 首先编译的是经过验证的控制流程与参数化数据处理，不把一条成功轨迹里的输出常量直接当作所有输入的答案。首个快速路径采用受限 IR：工具调用、字段提取、schema 校验、条件分支与终止；解释执行即可，不必先生成并运行任意 Go/Python 代码。

守卫覆盖输入 schema、适用任务域、工具与 Skill 版本、权限、环境前提和中间结果断言。每个快速动作仍写入动作账本；中途守卫失败时，动态 Loop 继承已经完成的结果和执行位置，不能从头重放产生重复副作用。

“零 token”只可能适用于完全不调用模型的那段控制或处理逻辑；联网工具自身可能产生费用、延迟或模型调用。必须同时报告端到端成功率、费用和总时延。

JEV 暂作为可替换 `DecisionPolicy` 候选。分别评估“是否需要调用工具”“选哪个工具”“何时停止”，使用可校准的置信度与弃权回退机制。不能让该模型批准权限或预测一个没有证据的任务成功。

轨迹蒸馏先生成带来源、适用条件、失败案例与验证记录的 Skill 草案；通过评估后才晋升到指定应用的 Capability。初期不自动写入全局 Skill，也不把失败轨迹中的注入指令提炼成规则。

JIT-Agent 的 Harness 生成、轨迹快速路径编译和运行时概率决策是不同机制，可以在 Kun 中协作，但不能用同一个“JIT”指标混合宣传。

## 17. 本次核实的参考与尚待验证项

以下网页查阅日期为 2026-10-04；是设计参考，不是依赖锁定或性能实测。

- [sonnes/pi-go](https://github.com/sonnes/pi-go)：用于核对 SDK 分层、Hook、不可变配置与会话持久化接口说明；仍需源码和集成验证。
- [guanshan/pi-go](https://github.com/guanshan/pi-go)：用于核对模块结构及 alpha/兼容性边界。
- [JIT-Agent](https://github.com/bingreeky/JIT)：其公开 README 使用 Memory、Planning、Action、Capability 四模块组织 Harness；Kun 借鉴分解方法，协议与控制权仍自行定义。
- [OpenAI 插件文档](https://developers.openai.com/plugins/build/plugins)：支持插件携带生命周期 Hook，并说明 Hook 信任要求；不能据此推断任意已安装 Codex 的全部 Hook 均可用。

用户材料中的 AgentJIT、JEV、SPARK、Trace2Skill、SkillTTA 及各项性能数字，本轮未逐项独立核实，不用作首版收益承诺。正式进入 K4 再对候选论文、实现、许可、评测设置和适配成本做专项核实。

新增 DevTools 材料中的 AgentLens、AgentTracer、RunLens、AgentXray、AgentSea、Meterbility、GraphMind、AgentixLens、Rewind 等按用户提供的交互参考理解。本轮未逐项核验项目身份、接口或功能，不将这些描述作为已证实的竞品能力，也不直接引入其代码。普通 tracing 平台同样可能提供实时状态或调试功能；Kun 的定位用自身能力与边界描述，不以未经核实的排他比较为依据。

优先冻结的是 RunDesk 与 Kun 的责任边界、单一执行权、状态协议和副作用恢复语义。pi-go 的具体 commit、首个实际 Provider，以及 K1 启用的内置工具清单，在 K0 的适配验证中确定。
