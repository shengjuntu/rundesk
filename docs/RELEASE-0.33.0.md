# RunDesk 0.33.0 / Kun 0.9.0

本版完成 K3-C 首批模块组合、第二种 LoopPolicy 和 Harness 固定快照对照。K3 整体及 K4 尚未完成。

- 新增 Plan-Act：先生成一份显式计划，再进入工具循环。规划调用禁用工具、执行端拒绝规划 tool_calls，额外请求进入原预算和调试边界。
- 配置支持 Tool Loop / Plan-Act 两套内置组合，未知模块和不兼容配对拒绝。普通新轮次可选择组合并记录 Harness revision；运行中、检查点和 Hybrid 不更换组合。
- 序列化显式计划并校验四模块实现/状态版本。检查点重启和 Hybrid 保留已完成计划；来源规划前边界会重新规划。steer 指令在上下文中仍位于旧计划之后。
- 设置、Network、Elements、Performance、Layers 接通组合与规划状态。计划留在调试记录，不混入最终回复和会话导出。
- Layers 对照同一会话两份固定快照的模块版本、策略、配置及预算。对照只读，不声称同条件实验或优化收益。
- 不新增 API 路径或运行时依赖。OpenAPI 仍为 150 路径 / 180 操作，新增 KunHarnessConfig 契约。

需要同时更新 RunDesk 和 Kun：worker 协议 v9，Kun 0.9.0。旧 0.8 检查点和分叉包不能跨版本执行，历史仍可查看。Codex 内部调试能力没有新增。

本地内核、真实 worker/HTTP、Node 交互、专项 race 和构建的证据见 [验证记录](KUN-VALIDATION.md)。本版未完成真实浏览器、真实模型/Codex/MCP 和生产环境验收；Node DOM 测试不代表视觉、移动端或焦点验收。使用说明见 [KUN-HARNESS.md](KUN-HARNESS.md)。
