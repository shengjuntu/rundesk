# RunDesk 0.27.0 / Kun 0.6.0

本版把 Codex 与 Kun 接入统一只读调试入口，使外部诊断工具可以复用现有会话权限读取证据。

- Codex：宿主运行概况、保留事件元数据分页、精确事件脱敏分块；能力清单明确内部上下文、断点和快照差异不可用。
- Kun：共享上下文、工具、预算、模块、断点、动作账本、快照、事件证据和差异查询；不启动离线 worker，不改变运行状态。Console 和差异界面复用新接口，旧 Kun 查询结构保留。
- `rundesk debug-mcp`：固定服务和会话的 stdio MCP，只提供 14 个只读工具，使用环境变量凭据，通过相同 HTTP 授权路径访问。
- 区分宿主事件 ID 与 Kun sequence；稳定事件上界包括空历史 through=0；Unicode 分块前完整脱敏并保留大整数精度；明确响应与单事件解析上限。

Kun 引擎保持 0.6.0 / worker 协议 v6。Codex 原有轨迹、停止、steer、审批没有变成内部单步或条件断点。K2 不标记全部完成；自动自然语言诊断、分叉、第二种 LoopPolicy 和 K4 优化仍未实现。

全量 Go 回归、静态检查、本次改动范围竞态检查、54 项 JS/CJS 语法检查、两组 Node 回归、Linux/Windows 构建及真实 CLI 协议冒烟通过。浏览器与真实服务未验收。

验证结果见 [KUN-VALIDATION.md](KUN-VALIDATION.md)，接入方法与能力矩阵见 [DEBUG-SERVICE.md](DEBUG-SERVICE.md)。源码不含二进制，使用 Go 1.25.12+ 执行 `make build`。
