# Kun 0.3：K1 核心补强

对应 RunDesk 0.22.0。本轮完成固定四模块、统一工具派发、参数校验和执行预算；K1 完整验收仍有剩余项，不代表 K2 已完成。

## 四模块和执行边界

| 模块 | 固定实现 | 输出及当前边界 |
| --- | --- | --- |
| Memory | full-history-v1 | 构建有序消息和工具定义，校验实际请求大小，记录消息数、请求字节数、Skill 哈希；保留完整历史，无窗口裁剪或摘要 |
| Planning | no-explicit-plan-v1 | 明确返回没有请求结构化计划；不增加规划 LLM 请求，不伪造模型隐藏推理 |
| Capability | fixed-catalog-v1 | 固定本轮内置工具和已授权 MCP 工具，编译参数 schema，记录工具身份和 schema 哈希 |
| Action | schema-action-v1 | 校验工具是否属于本轮目录及参数是否合法，返回声明式意图；模块自身不调用工具 |

四模块、`LoopPolicy`、`Provider` 接口位于 `internal/kun/modules.go`。本轮只有 `tool-loop-v1`，实现选择由代码固定。`Harness` 保存 id/version/revision；每个模块保存 id/version/stateSchemaVersion、阶段和结构化状态，随现有 SQLite 快照持久化。

内核控制执行顺序：校验 → 审批 → 再检查预算和取消 → 持久化 dispatched → 统一 `executeAction` → 记录结果。无效参数形成 rejected 工具结果供模型纠正，不弹出审批、不请求外部工具、不消耗实际工具调用次数；重复错误受连续失败上限约束。工具结果丢失仍为 outcome_unknown，不自动重试。

这些是 Kun 内部接口。尚未完成跨后端 AgentRuntime/Codex adapter 的完整抽取；没有开放模块插件、热替换、兼容恢复或第二种 LoopPolicy。

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

## JSON Schema 支持范围

实现位于 `internal/kun/schema.go`，无新运行依赖。默认按有界 JSON Schema 2020-12 子集处理；显式 `$schema` 只接受该 dialect。

- 类型：object、array、string、number、integer、boolean、null 及类型数组。
- 对象：properties、required、additionalProperties、patternProperties、propertyNames、min/maxProperties、dependentRequired、dependentSchemas。
- 数组：items、prefixItems、min/maxItems、uniqueItems、contains、min/maxContains。
- 标量：enum、const、min/maxLength、pattern、minimum/maximum、exclusiveMinimum/Maximum、multipleOf；数值使用有理数精确比较。
- 组合：allOf、anyOf、oneOf、not、if/then/else；`$defs`、definitions 和本地 JSON Pointer `$ref`。
- 注释：title、description、$comment、format、default、examples、deprecated、readOnly、writeOnly。**format 仅为注释，不进行格式断言。**

不支持远程引用、`$id`/`$anchor`、动态引用、unevaluatedProperties/Items、旧 dialect 及未知关键字。正则使用 Go RE2 支持的语法，非完整 ECMA-262。检测到不支持语法时在模型调用前明确停止目录加载；不会跳过未知约束。服务初始化和工具发现此时可能已经进行，但没有执行 tools/call。

拒绝重复 JSON 对象键、尾随内容；schema ≤64 KiB，参数 ≤1 MiB，JSON 嵌套 ≤64。schema 节点 ≤4096，校验有深度/步数上限；数值表示 ≤256 字符，十进制指数绝对值 ≤1024。超过支持范围即拒绝，不静默降级。此实现不宣称完整 JSON Schema 或所有 MCP 服务兼容。

## 调试界面

新增 Layers 基础检查，可查看四模块版本、状态和预算。选中 Network/Elements 中的事件后固定快照；Elements、Layers、Application 共享该选择，切换或刷新保留选择；“跟随现场”恢复当前状态。Performance 显示所选轮次的调用记录及所选快照的预算。

Sources 明确控制当前运行；历史快照只读，不会让控制命令针对旧轮次。Layers 暂时没有健康评分、规则诊断或替换模块操作，Console 尚未实现。当前仍以结构化 JSON 为主，非完整七面板产品验收。

## 下一步验收

1. 在实际模型和 MCP 服务上完成 news2douyin 材料准备：选定新闻事件 → 检索/抓取 → 带来源时间线与材料 → 文件落盘。验证始终允许、steer 追加筛选条件、工具错误可检查，不把测试桩作为真实业务验证。
2. K1 剩余：Memory 窗口/摘要、token 估计/费用预算、逐 token UI、产物/环境证据与更完整的查询展示。
3. K2：检查点恢复、worker 所有权代际、Console、条件断点、上下文补丁，以及运行中权限撤销机制。
4. K3/K4：模块替换、第二种 LoopPolicy、分叉/混合回放，然后再做优化实验。

本版 `resumeCheckpoint=false`、`fork=false`；重启后保留中断记录供检查，不自动继续执行。
