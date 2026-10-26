# Kun 模块、预算与 Harness 组合

Kun 提供固定模块、执行预算、两套内置循环组合、对照、Live 分叉与普通运行安全点组合切换。运行版本与恢复兼容规则见 [Kun 接口](KUN-SEPARATION.md)。仍不支持任意模块插件或任意状态迁移。

## 四模块和执行边界

| 模块 | 固定实现 | 输出及当前边界 |
| --- | --- | --- |
| Memory | full-history-v1 | 构建有序消息和工具定义，校验实际请求大小，记录消息数、请求字节数、Skill 哈希；保留完整历史，无窗口裁剪或摘要 |
| Planning | no-explicit-plan-v1 / explicit-plan-v1 | 随 Tool Loop / Plan-Act 内置组合选择；前者不请求结构化计划，后者显式生成计划，不推测模型隐藏推理 |
| Capability | fixed-catalog-v1 | 固定本轮内置工具和已授权 MCP 工具，编译参数 schema，记录工具身份和 schema 哈希 |
| Action | schema-action-v1 | 校验工具是否属于本轮目录及参数是否合法，返回声明式意图；模块自身不调用工具 |

四模块、`LoopPolicy`、`Provider` 接口位于独立 Kun 项目的 `internal/engine/modules.go`。当前有下文列出的两套内置组合，组合及配对由代码限定。`Harness` 保存 id/version/revision；每个模块保存 id/version/stateSchemaVersion、阶段和结构化状态，随现有 SQLite 快照持久化。

内核控制执行顺序：校验 → 审批 → 再检查预算和取消 → 持久化 dispatched → 统一 `executeAction` → 记录结果。无效参数形成 rejected 工具结果供模型纠正，不弹出审批、不请求外部工具、不消耗实际工具调用次数；重复错误受连续失败上限约束。工具结果丢失仍为 outcome_unknown，不自动重试。

这些是 Kun 内部接口；RunDesk 的宿主与后端边界见 [资源契约](RESOURCE-CONTRACTS.md)。Kun 支持下文的安全点内置组合切换和受版本守卫约束的恢复，尚未开放任意模块插件或任意状态迁移。

## 每轮预算

配置位于 Agent 引擎页面及 `agentRuntime.budget`：

```json
{
  "maxToolCalls": 64,
  "maxTotalTokens": 0,
  "maxActiveSeconds": 900,
  "maxConsecutiveFailures": 3
}
```

| 字段 | 默认值与含义 |
| --- | --- |
| 原有 maxSteps | 默认 20，模型调用次数上限 |
| maxToolCalls | 默认 64，内置工具和 MCP 实际派发次数的共同上限 |
| maxTotalTokens | 默认 0 关闭；非零时按模型报告的 total_tokens，或 prompt_tokens + completion_tokens，阻止达到阈值后的新动作 |
| maxActiveSeconds | 默认 900 秒；发现服务、模型/工具执行与内核工作计入，调试/审批等待另计 |
| maxConsecutiveFailures | 默认 3；连续 rejected/declined/failed 工具结果达到上限后停止新动作，工具成功后清零 |

除 token 阈值以外，API 中省略或填写 0 使用默认值，不能用 0 关闭这些保护；负数和超范围值拒绝保存。旧配置升级后使用上述默认值，长任务可提高调用和时间上限。

token 阈值是**依据已报告用量的后续动作门槛**，不是请求前的精确 token 预留，也不是账单硬限额。单次模型请求可能使累计量超过阈值。启用该阈值时发送 `stream_options.include_usage=true`；服务拒绝该参数会明确报错，服务未报告可信用量会阻止后续动作，而不是按零计费。没有后续动作的最终回答仍可以正常完成。

`BudgetUsage` 保存实际工具调用数、累计已报告 token、未报告用量的模型调用数、连续失败数、活动/等待毫秒数和停止原因。时间计数在记录边界更新，不是逐毫秒实时仪表。MCP 与模型网络调用受剩余活动时间取消；普通本地文件 I/O 仍是同步系统调用，预算不能保证立刻打断阻塞的操作系统 I/O。

停止原因包括 `model_calls`、`tool_calls`、`active_time`、`consecutive_failures`、`reported_token_threshold`、`token_usage_unknown`。已派发外部动作超时仍保留未知副作用语义，取消不等于远端回滚。模型失败直接结束本轮，不自动重试。

尚无费用预算、价格表、模型 tokenizer 或 token 预估。费用保持未知。

## 选择组合

在 Agent 引擎设置中选择 Kun 模块组合，保存后于下一次普通新轮次生效。已有 Kun 会话也可在下一轮使用新组合；正在运行的轮次不会因设置页保存而暗中改变；另可使用下文的明确切换控制。改变设置会使旧检查点/分叉预览的配置校验失败，不能借恢复更换实现。

| 组合 | LoopPolicy | Planning | 行为 |
| --- | --- | --- | --- |
| Tool Loop（默认） | tool-loop-v1 | no-explicit-plan-v1 | 按需调用工具，直到模型完成回复 |
| Plan-Act | plan-act-v1 | explicit-plan-v1 | 先调用一次模型生成显式计划，再进入工具循环 |

两套组合均使用 full-history-v1、schema-action-v1、fixed-catalog-v1。模块和策略版本为 1.0.0，模块状态结构为 1。没有开放任意插件加载或自由混配，也没有添加运行时依赖。

API 在原 PUT /api/v1/instances/{iid}/agent-runtime 的 config 中接收：

```json
{"harness":{"loopPolicy":"plan-act-v1"}}
```

harness 的 memory/planning/action/capability 字段可显式指定对应内置 ID；省略时按组合补齐。未知 ID 和不兼容的 LoopPolicy/Planning 配对拒绝，不静默降级。空配置仍选择原 Tool Loop。同一会话新轮次更换组合时 Harness revision 增加，未变更时保留；run.started 保存当前及上一 Harness 定义。revision 是会话内组合变更版本，不是模型步骤数。

## 规划和执行的边界

规划请求使用相同模型、认证、超时和预算。实际请求不携带工具定义，tool_choice 为 none；即使服务违反约束返回 tool_calls，引擎仍拒绝该规划结果，不登记待派发动作、不执行文件工具、不请求 MCP tools/call。有效计划必须为非空文本且不超过 32768 UTF-8 字节。

这是可见的任务计划，不是隐藏推理。计划保存在 planning 模块状态，不充当最终回复。执行请求将其作为辅助 assistant 消息插入原目标之后；后来的 steer 指令保持在计划之后，工具观测和新指令优先。实际模型输入完整记录在 model.started.request。原始会话历史不被此派生输入替换。

每次普通新轮次进入 Plan-Act 时规划一次；工具反馈或 steer 不会自动触发重规划。运行中显式从 Tool Loop 切入 Plan-Act 会初始化新计划；切出再切回也需要重新规划并消耗剩余预算。规划前后的暂停/单步/断点沿用 before_model / after_model，模型事件携带 purpose=plan 或 act。规划调用计入 step、有效报告 token、活动时间和超时；模型次数设为 1 时可能只完成计划，随后因预算停止，不能当作任务完成。

有效计划随安全检查点、Hybrid 和 Live 快照恢复；选中计划已完成的边界不会重复规划，选中规划前的边界会重新规划。继承预算、审批重置和目录校验约束不变；Hybrid 工具严格回放，Live 使用当前项目与真实工具。恢复校验增加了所存 Harness 定义、四模块实现版本和 stateSchemaVersion 检查。Hybrid / Live 添加说明消息时会相应移动计划的上下文插入位置。

## 在当前运行中切换

仅管理员可对**普通 Kun 运行**应用切换，包括从检查点恢复后的普通运行；诊断和 Hybrid/Live 分支拒绝。模型、工具或 MCP 请求正在执行时不可切换。

1. 在 Sources 暂停或单步，停到 **before_model / 模型请求前**。工具批次、MCP 审批、排队 steer/控制及结果未知动作必须先处理；不能通过切换清空它们。
2. 在 Layers 点击“跟随现场”退出历史快照检查。选择 Tool Loop 或 Plan-Act，填写本次切换原因，点击 **预览组合切换**。
3. 核对当前/目标组合、新 Harness revision、迁移说明和后续模型调用行为，点击 **应用已预览的组合**。
4. 应用后仍暂停。在 Sources 显式继续或单步才会发起后续调用；已有暂停期限不会被延长。

预览只刷新读取状态，没有模型或工具调用。目标组合/原因编辑、运行 revision 变化或切到历史快照都会使旧提案失效；不自动更新命令目标。网络响应丢失时，同一提案以原 requestId 重试，返回已保存回执；若已观察到新状态，查看切换记录，不生成反向切换来猜测结果。相同 requestId 改内容返回冲突。

| 状态 | 切换时的处理 |
| --- | --- |
| LoopPolicy / Planning | 两套内置配对一起切换，校验实现版本和状态结构，Harness revision 加 1 |
| Planning 状态 | 清除当前计划；切入 Plan-Act 后，继续时先做一次规划；切到 Tool Loop 后不再注入旧计划 |
| Memory 状态 | 重置上下文构建状态；消息、Skills 及原始观察历史保留，下一请求重新构建 |
| Action / Capability | 保留动作记录、工具目录与权限；不更改 schema、不继承未决审批、不产生工具调用 |
| 预算 | 已用模型次数、token、工具次数、时间和失败计数保留；剩余模型预算不足时拒绝切换 |
| 配置默认值 | 不修改实例设置，也不修改本 run 的原始启动配置 |
| 记录 | 一次事务写入新状态、迁移事件、控制回执和安全检查点；写入失败撤回内存中的迁移并停止运行 |

这是受限的内置组合切换，不支持装载任意模块、修改模型/工具权限或批次中途迁移。切换本身不调用模型/工具；Plan-Act 的后续规划使用原有模型预算，不能借切换退款或扩大权限。旧计划仍保留在历史快照，但不在当前有效规划状态中。

### 配置、恢复与分叉

`state.config.harness` 保留原始启动配置，`state.runtimeHarness` 记录当前运行的显式覆盖，`state.harness` 是当前生效的实现定义与 revision。环境清单继续校验原始配置、项目、Skills、MCP 和权限，Harness 指纹则反映切换后的有效组合。

中断后的检查点恢复保留覆盖与已完成计划，不回到旧默认值、不重复规划；如果切换后还没规划，则恢复后仍需要规划。Hybrid/Live 使用所选快照的覆盖、计划和继承预算，分支本身不能再切换。下一次**普通新轮次**读取实例当前默认配置并清除运行覆盖；如果实际组合发生变化，Harness revision 继续递增。旧快照不会被编辑。

控制仍使用原 `POST /api/v1/sessions/{sid}/kun/control`：

```json
{
  "requestId": "unique-harness-change-id",
  "runId": "current-run-id",
  "expectedStateRevision": 17,
  "operation": "set_harness",
  "harness": {"loopPolicy": "plan-act-v1"},
  "reason": "检查显式规划对当前任务的影响"
}
```

以上 runId/revision 必须换成读到的当前值。reason 要求 1–2048 字符，不能带 steer text 或工具 callId。应用 key 和成员凭据无权执行 set_harness，原有暂停、单步、审批权限不因此扩大。`kun/control.applied.data.harnessChange` 记录前后组合、阶段、步骤、原因和重置/保留模块；Layers 展示有证据入口的最近切换记录。

## 检查与对照

Network、Elements 和 Performance 区分规划/执行模型调用。Layers 展示四模块实现、版本、规划状态及显式计划，提供对应事件证据入口。聊天、回复操作和会话 Markdown 导出不把规划结果当作完成答复。

Harness 对照在同一会话内读取两份明确选择的固定快照：在 Network / Elements 选定一份快照，切换 Layers 设为对照起点，再选择另一份快照进行比较。可查看不同轮次的 LoopPolicy、模块版本、Harness revision、规划状态、配置指纹、累计调用/用量、活动时间和执行模式。只使用既有 GET 快照接口，不新增模型或工具调用；重复点击受保护，重设起点使迟到响应失效。

该对照是描述性检查，不是同条件基准测试。不同输入、阶段、工具服务状态和继承预算会影响数值；缺失用量为未知，不能把更少 token 当作质量提升。没有自动实验调度、验证集评分或成本收益结论。另有 [同基线跨会话日志对照](KUN-COMPARISON.md)：从分叉预览比较来源后续执行与分支、或两个同源分支。它不依赖在线 worker，也不是跨会话 Harness 评测基准。

## 兼容性和未完成项

执行预览和检查点须满足内核版本与能力守卫；Kun 0.17.0 对既有 0.16.0 格式的兼容规则见 [Kun 接口](KUN-SEPARATION.md)。只读历史保留，升级操作见 [UPGRADING](UPGRADING.md)。

尚未实现更多 Memory/Action/Capability 实现及其状态迁移、工具批次中途切换、自动重规划、任意模块插件、跨会话 Harness 基准，等扩展。K4 候选/IR/守卫/回退/独立验证集已接入，见 [KUN-K4.md](KUN-K4.md)。各轮测试与跳过范围见 [验证记录](VALIDATION.md)，真实远程服务与业务效果需在目标环境验收；当前缺口见 [状态](STATUS.md)。
