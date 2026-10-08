# RunDesk 0.17.0 — MCP test workbench / MCP 独立测试台

- Connect to an existing local stdio or Streamable HTTP server without an Agent turn; list tools with pagination, inspect input schemas, call a tool with a JSON object. / 无需 Agent 对话即可读取工具、查看参数结构、直接调用。
- Explicit administrator confirmation, 30-second timeout, 2 MiB response limit, configured-secret masking, recorded latency and error states. / 管理员确认、超时与响应限制、已知密钥脱敏、耗时与错误记录。
- Persist a record before executing; crashes leave an unconfirmed record. Replay reads history only. Live execution remains explicit. / 执行前持久化记录，回放仅查看历史。
- Extend existing idempotency middleware to test submissions; do not mislabel it as a newly invented task-queue feature. / 复用已有幂等机制，扩展到测试入口。
- Chinese/English workbench, desktop/mobile layout; OpenAPI and capability audit updated. / 中英文界面、移动端适配与 API、能力清单更新。

## Limits / 边界

Handshake protocol revisions 2025-03-26, 2025-06-18 and 2025-11-25 only. No Docker test execution, OAuth login, sampling/elicitation, mock-server replay, or 2026 stateless protocol. Direct tests bypass Codex sandbox/approvals and run as the service account. Local stdio child cleanup uses process groups on Linux/macOS; Windows descendant cleanup is not guaranteed. Timeout does not prove a remote side effect was cancelled. History deletion retains idempotency receipts, which can contain the original masked result. Free-text secret detection is not guaranteed.

本版不是完整的任意 MCP 兼容层，不新增多租户隔离、原生记忆系统、跨节点调度或自动更新。详见 [能力清单](CAPABILITY-STATUS-0.17.0.md)。

## Validation / 验证

Go package tests; targeted race checks; local stdio and HTTP/SSE fixtures; tool error and known-secret masking; repeat keys execute once; app credentials cannot access tests; browser checks for list/call/replay, both languages and mobile width. Real third-party MCP services and Docker execution are not validated.

Upgrade: back up data, stop the old binary, replace it, retain the same `--data` directory. / 升级保持原数据目录。
