# Capability status / 能力状态 — 0.17.0

This audit reflects repository implementation, not third-party product claims. / 以代码为准，不采用未经核实的第三方能力描述。

| 项目 / Capability | 当前状态 / Status | 下一步 / Next |
|---|---|---|
| MCP independent tests / 独立测试 | New: local stdio + handshake Streamable HTTP; saved results; explicit live calls. / 新增本机测试与历史回放 | Docker execution, OAuth, newer protocol adapters; fixture replay as a mock server remains future work / 容器、OAuth、新协议、模拟服务器重放仍待做 |
| HTTP idempotency / 幂等键 | Existing persisted receipts, caller scope, conflict checks; extended to MCP tests / 已有持久化回执与身份隔离，本次扩展测试入口 | Client integrations must retain keys on retries / 应用重试保持 Key |
| Multi-user / 多用户 | Application/project grants and member access, logical isolation / 应用项目授权、逻辑隔离 | Tenant ownership across all records before untrusted enterprise hosting / 不可信多租户前需统一租户边界 |
| Operations / 生产运维 | Bounded probes; OS process-group cleanup for diagnostics on Linux/macOS / 诊断探针已有超时与进程组清理 | Service-manager deployment; not universal descendant cleanup or Windows Job Objects / 非所有子孙进程清理保证 |
| Metering / 计量配额 | Runtime token events and concurrency controls / 运行 token 事件和并发控制 | Application/project aggregate usage; missing usage must remain unknown, not zero / 按应用项目汇总，缺失不能算零 |
| Containers / 容器 | Linux local Docker, app/project environment lifecycle / 本机 Docker 生命周期 | Validate real deployments before multi-node scheduling / 先验证单机，再做跨节点 |
| Updates / 更新 | Manual binary replacement / 手工升级 | Signed/versioned release source before version notifications / 先确定可信发行源 |
| Native history / 原生历史 | Bulk migration not implemented / 批量迁移未实现 | Demand-driven import with provenance / 按需导入并保留来源 |
| Memory / 记忆 | Explicit project notes; not all native memory / 项目显式笔记 | Evidence-based memory observability, no invented attribution / 仅展示有证据的记忆来源 |
| Rich text / 富文本 | Markdown / Markdown | Defer until document-editing need / 有文档编辑需求再做 |
| Queue and Cron / 队列与定时 | Persisted queue, concurrency, schedule policies / 已有持久化队列、并发和定时策略 | Operational validation / 真实部署验证 |

MCP replay in this release means inspecting a recorded response without contacting the server. It does **not** inject a recorded response into an Agent or emulate a complete server. Failed/timeout calls are never retried automatically; downstream effects may be unknown. Records left `unconfirmed` by a crash must be reconciled manually.

本版回放是查看历史结果，不是将录制响应注入 Agent，也不是完整的 mocking/replay 框架。超时或连接失败不自动重试；服务崩溃留下的 unconfirmed 记录需要核对。

Protocol reference: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
