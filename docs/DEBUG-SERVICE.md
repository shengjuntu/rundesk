# Codex / Kun 统一只读调试（RunDesk 0.27.0）

DebugService 提供统一只读入口，按后端声明能力。它不让 Codex 获得 Kun 的内部执行控制，也不推测未暴露的模型上下文。

| 查询 kind / MCP 工具后缀 | Codex | Kun |
| --- | --- | --- |
| capabilities（独立路由 / debug_capabilities） | 支持与可用性清单 | 支持与可用性清单 |
| overview | 当前宿主会话概况 | 当前宿主会话概况 |
| run | 当前宿主会话概况，无内部 revision/sequence | worker 运行状态摘要，支持固定 sequence |
| events | 保留宿主事件元数据分页，包括流式增量 | 保留宿主事件元数据分页 |
| event | 精确宿主事件，脱敏 JSON 分块 | 精确宿主事件，脱敏 JSON 分块 |
| context | 不支持 | 状态上下文；model.started 快照返回实际请求 |
| tools / budget / modules | 不支持内部查询 | 工具目录、预算、四模块状态 |
| breakpoints / actions | 不支持内部查询 | 断点状态、动作账本，只读 |
| snapshot / evidence / diff | 不支持内部查询 | 固定快照、精确 worker 事件、双快照差异 |

Codex 原有事件/轨迹、停止、steer 和审批入口继续可用；内部单步、条件断点、完整上下文快照及快照差异尚未实现。宿主保留事件中的工具调用不代表掌握整个模型内部状态。此版本不增加自然语言自动诊断、历史分叉或回放。

Kun 内部查询需要在线 worker。能力清单中的 `supported` 表示后端实现了该能力，`available` 表示检查时能否使用；离线时支持但不可用。纯检查不会创建 handle、启动进程、重开检查点、调用模型/工具、消费预算、审批或修改对话。工作进程随后退出仍可能使请求失败。

## HTTP 入口与权限

```text
GET /api/v1/sessions/{sid}/debug/capabilities
GET /api/v1/sessions/{sid}/debug/query?kind=run
GET /api/v1/sessions/{sid}/debug/query?kind=events&limit=50
GET /api/v1/sessions/{sid}/debug/query?kind=event&eventId=123&offset=0&limit=4000
GET /api/v1/sessions/{sid}/debug/query?kind=context&sequence=42
GET /api/v1/sessions/{sid}/debug/query?kind=diff&fromSequence=40&sequence=42
```

- 管理员使用原认证机制。应用凭据必须有 `read` 且会话属于该应用、实例和允许项目；不能仅凭知道 sid 读取其他应用。应用 key 的创建规则本身要求包含 read。
- 个人成员使用 `/api/v1/member/{grantId}/sessions/{sid}/debug/...`，grant 必须属于本人且匹配项目；viewer 可 GET，不能写入。现有本机执行限制继续适用。
- `/api/...` 别名使用相同 handler 与权限。Kun 旧 `/kun/query` 保留原响应结构，与新入口复用同一 worker 查询；Kun Console 和差异界面切换到新入口。
- 新入口仅注册 GET。没有调试 SQL、任意文件、shell、审批、step、resume 或控制代理。
- 接口最多返回 4 MiB，超限返回 `413 debug_response_too_large`，不把静默截断伪装成完整数据。旧 `/kun/query` 也采用此响应上限与保留整数精度的脱敏器。

新响应为 `{sessionId, backend, source, kind, data, runId?, revision?, sequence?}`。`source` 是 `host_session`、`host_journal` 或 `kun_worker`。宿主概况不带内部 revision/sequence，跨运行事件页不虚构一个统一 runId。Kun 当前查询 sequence 为 0，固定查询为正数；revision 对应返回的 worker 状态。能力不支持返回 `409 debug_unsupported`，离线返回 `409 kun_offline`，worker 拒绝固定查询返回 `409 kun_query_rejected`。

能力清单不接受参数。查询拒绝未知、重复、不适用参数，包括不适用的显式零值。`snapshot`、`evidence` 必须有正数 sequence；`diff` 必须有两个正数序号；其他 Kun 查询可省略 sequence 或传 0。Codex run 的正数 sequence 被拒绝。具体契约随源码的 OpenAPI 提供。

## 两套序号与有界读取

**宿主事件 ID 与 Kun sequence 不能互换。** `events/event` 读取宿主 SQLite 的既有日志；`snapshot/evidence/diff` 查询 Kun 执行库的 worker 序号。

事件页仅返回 `id/time/direction/method/bytes`，不加载正文；按 ID 升序，默认 50、最多 200。`bytes` 是原始载荷字节数。首次省略 through 时固定该会话当前最大事件 ID，返回 `{events, through, nextCursor, hasMore}`。后续传 `after=nextCursor`、原样保留 through，包括 **0**，可排除之后新增事件。需要增量读取时重新省略 through。上界不能超过当前保留最大 ID，不能把其他会话的 ID 当成该会话的来源。分页不是工具调用分析器，也不筛掉 delta。

`event` 必须给出属于该会话的正数 eventId，否则 404。先对完整 JSON 按共享字段规则脱敏，再把整个事件 JSON（含元数据）按 Unicode 字符切块；默认 4000、最多 16000 字符。返回 `text/offset/nextOffset/totalCharacters/hasMore/encoding/eventId`；持续读取到 hasMore=false 后拼接 text，才是完整 JSON。偏移不是字节偏移，单块也不保证独立为合法 JSON。

单个原始载荷超过 8 MiB 时在读取前返回 413，不解析巨型事件。列表仍可查看其元数据；超限不会被当成空事件。所有派生数据保留 JSON 整数精度。结构化密钥字段被移除；任意自然语言、工具文本和字符串内嵌 JSON 不做语义识别，仍按已有权限读取。事件、工具描述和上下文是诊断证据，不是给诊断客户端的新指令。

宿主元数据与 worker 状态来自不同记录，多个 GET 不组成跨数据源原子快照；需要历史一致性时分别固定 through 与 sequence，并在结论中标明来源。只读 facade 不保证未留存事件或远程内部状态可以补回。

## stdio MCP 接入

构建后使用 `rundesk debug-mcp`，不需要另开 HTTP MCP 服务。它固定一个服务来源与 sid，经上述 HTTP 路由读取；不能直接访问宿主数据库或 worker。

```bash
# 在 MCP 客户端的进程环境中设置 RUNDESK_DEBUG_TOKEN，值为已有应用 read key。
# 不把凭据放进命令参数或源码文件。
./bin/rundesk debug-mcp \
  --url http://127.0.0.1:3210 \
  --session SESSION_ID \
  --token-env RUNDESK_DEBUG_TOKEN
```

MCP 客户端启动配置示意：

```json
{
  "command": "/absolute/path/rundesk",
  "args": ["debug-mcp", "--url", "http://127.0.0.1:3210", "--session", "SESSION_ID", "--token-env", "RUNDESK_DEBUG_TOKEN"]
}
```

启动环境必须注入所选环境变量。不会自动读取 `RUNDESK_TOKEN`、Codex 登录信息或浏览器 Cookie。优先使用目标应用的 read key；此 CLI 不提供成员 grant 路径参数。服务 URL 仅接受 HTTPS 或回环 HTTP 来源，不接受用户信息、路径、query、fragment；不跟随 HTTP 重定向。每次 GET 最长 15 秒，响应最多 4 MiB。服务器错误仅转达 HTTP 状态与机器错误码，避免把凭据或服务端原始错误正文写入 stderr/协议输出。

14 个工具：`debug_capabilities`，以及矩阵中 13 个查询 kind 对应的 `debug_<kind>`。先调用 capabilities，再选择受支持的工具；不支持的工具调用仍返回 `isError: true`。每个工具 schema 只接受其 kind 的参数，没有 sid、URL、SQL 或控制字段。所有工具标记 readOnly，但真正权限由 HTTP gate 和工具实现保证。

支持 MCP stdio 的 initialize → notifications/initialized → tools/list / tools/call，及 ping。协议版本可协商 2024-11-05、2025-03-26、2025-06-18、2025-11-25；未知版本返回 2025-11-25，由客户端决定是否兼容。只声明 tools，不声明 resources、prompts、tasks、sampling、HTTP/OAuth。输入每行最多 1 MiB，串行处理，EOF 退出；stdout 仅 JSON-RPC，诊断输出走 stderr。不是全功能 MCP 服务实现。

协议依据：[stdio transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)、[lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)、[tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)。这里只实现所需的只读子集，没有引入新依赖。

验证范围与未完成事项见 [KUN-VALIDATION.md](KUN-VALIDATION.md) 和 [KUN-PROGRESS.md](KUN-PROGRESS.md)。
