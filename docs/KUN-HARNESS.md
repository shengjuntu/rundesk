# K3-C 首批：内置组合、Plan-Act 与 Harness 对照

对应 RunDesk 0.34.0 / Kun 0.10.0 / worker 协议 v10。本页介绍 0.33 引入的内置组合与对照；0.34 另已实现 [Live 分叉首批](KUN-FORKS.md)，运行中更换模块尚未实现。

## 选择组合

在 Agent 引擎设置中选择 Kun 模块组合，保存后于下一次普通新轮次生效。已有 Kun 会话也可在下一轮使用新组合；正在运行的轮次保留原组合。改变设置会使旧检查点/分叉预览的配置校验失败，不能借恢复更换实现。

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

每个普通新轮次只规划一次；工具反馈或 steer 不会自动触发重规划。规划前后的暂停/单步/断点沿用 before_model / after_model，模型事件携带 purpose=plan 或 act。规划调用计入 step、有效报告 token、活动时间和超时；模型次数设为 1 时可能只完成计划，随后因预算停止，不能当作任务完成。

有效计划随安全检查点、Hybrid 和 Live 快照恢复；选中计划已完成的边界不会重复规划，选中规划前的边界会重新规划。继承预算、审批重置和目录校验约束不变；Hybrid 工具严格回放，Live 使用当前项目与真实工具。恢复校验增加了所存 Harness 定义、四模块实现版本和 stateSchemaVersion 检查。Hybrid / Live 添加说明消息时会相应移动计划的上下文插入位置。

## 检查与对照

Network、Elements 和 Performance 区分规划/执行模型调用。Layers 展示四模块实现、版本、规划状态及显式计划，提供对应事件证据入口。聊天、回复操作和会话 Markdown 导出不把规划结果当作完成答复。

Harness 对照在同一会话内读取两份明确选择的固定快照：在 Network / Elements 选定一份快照，切换 Layers 设为对照起点，再选择另一份快照进行比较。可查看不同轮次的 LoopPolicy、模块版本、Harness revision、规划状态、配置指纹、累计调用/用量、活动时间和执行模式。只使用既有 GET 快照接口，不新增模型或工具调用；重复点击受保护，重设起点使迟到响应失效。

该对照是描述性检查，不是同条件基准测试。不同输入、阶段、工具服务状态和继承预算会影响数值；缺失用量为未知，不能把更少 token 当作质量提升。没有自动实验调度、验证集评分、跨会话对照或成本收益结论。

## 兼容性和未完成项

RunDesk 和 Kun 两个二进制需一起升级。Kun 0.9 及更早的检查点/分叉包不可在 0.10 直接续跑；历史仍可只读检查，先创建本版普通运行再生成新检查点或预览。默认 Tool Loop 的执行流程保持兼容。

尚未实现运行中切换组合、更多 Memory/Action/Capability 实现、自动重规划、任意模块插件、跨会话 Harness 基准，以及 K4 的候选/IR/守卫/去优化/独立验证集。真实浏览器和真实远程服务验收仍待完成。证据见 [验证记录](KUN-VALIDATION.md)，整体状态见 [开发进度](KUN-PROGRESS.md)。
