# 应用接口入口

新应用请使用 [稳定 API v1](API-V1.md)，OpenAPI 为 `/api/v1/openapi.json`。以下为兼容 `/api` 的历史说明，仍保留原行为。

# HTTP API · v0.3

默认根地址 `http://127.0.0.1:3210/api`。JSON 字段使用 camelCase。配置令牌时，每次请求加入 `Authorization: Bearer $RUNDESK_TOKEN`。同源浏览器可以使用 POST /login 设置的 HttpOnly Cookie。

本版本是开发中的契约，不承诺跨版本稳定。错误通常返回 `{"error":"..."}`；字段/配置错误 400，未认证 401，跨来源/路径拒绝 403，资源不存在 404，运行/审批冲突 409，提交运行成功 202。少数资源校验仍返回 400，调用方应先按 HTTP 类别处理。

## ActiveVLM 接入

ActiveVLM 保留自己的业务 UI，通过服务端 HTTP 客户端调用本服务，或者部署同源 API 代理。不要把后台令牌写进公开前端构建产物。跨来源浏览器 fetch 默认会被拒绝。

```bash
# 读取工作区，取得 id
curl -s http://127.0.0.1:3210/api/workspaces

# 创建会话；workspaceId 替换为刚取得的 id
curl -s http://127.0.0.1:3210/api/sessions \
  -H 'Content-Type: application/json' \
  -d '{"workspaceId":"WORKSPACE_ID","instanceId":"default","title":"ActiveVLM task","model":""}'

# 使用返回的 session.id 提交任务
curl -s http://127.0.0.1:3210/api/sessions/SESSION_ID/turns \
  -H 'Content-Type: application/json' \
  -d '{"text":"整理这个工作区，给出下一步计划。","files":[],"skills":[]}'

# 从头订阅，或者 after=已持久化的事件 ID
curl -N 'http://127.0.0.1:3210/api/sessions/SESSION_ID/events?stream=1&after=0'
```

任务启动后独立于 HTTP 请求和浏览器连接。对于提交超时，先读取会话状态和事件再决定是否重试；v0.3 尚无 HTTP 幂等键。参考可直接运行的 [Python 客户端](../examples/activevlm_client.py)。

## 接口列表

| 方法 / 路径（省略 /api） | 请求与结果 |
| --- | --- |
| GET /meta | 版本、demo 标记、能力清单 |
| POST /login | `{"token":"..."}`，设置 Cookie |
| POST /logout | 删除当前浏览器 Cookie |
| GET /workspaces | 工作区数组，含 id/name/path/notes/revision |
| POST /workspaces | `{"name":"...","path":""}`；path 空则创建托管目录 |
| PUT /workspaces/{wid}/notes | `{"text":"...","revision":0}`；乐观版本检查 |
| GET /sessions | 全部应用会话，按更新时间排序 |
| POST /sessions | `{"workspaceId":"...","title":"...","model":""}` |
| GET /sessions/{sid} | 原生 threadId、runId、turnId、状态、错误等 |
| POST /sessions/{sid}/turns | `{"text":"...","files":[],"skills":[]}`，返回 202 会话状态 |
| POST /sessions/{sid}/stop | 请求中断当前运行 |
| GET /sessions/{sid}/events?after=N | 最多 1000 条，按 ID 升序；继续传最后一个 ID 分页 |
| GET /sessions/{sid}/events?stream=1&after=N | SSE，支持 Last-Event-ID |
| GET /sessions/{sid}/approvals | 当前未处理审批数组 |
| POST /sessions/{sid}/approvals/{aid} | 见下文 |
| GET /sessions/{sid}/files | outputs/{sid} 下最多 1000 个普通文件 |
| GET /sessions/{sid}/export | 会话信息 + 全部事件，NDJSON 下载 |
| POST /workspaces/{wid}/uploads | multipart `file`，单个文件最多 32 MiB |
| GET /workspaces/{wid}/file?path=... | 下载 uploads/ 或 outputs/ 中的文件 |
| GET /workspaces/{wid}/file?path=...&preview=1 | 对受支持类型启用安全预览 |
| GET /workspaces/{wid}/skills | Codex skills/list 的结果，data 按 cwd 分组 |
| GET /workspaces/{wid}/skills/{name} | 读取托管项目 Skill，`{"content":"..."}` |
| PUT /workspaces/{wid}/skills/{name} | `{"content":"---\n..."}`，写项目 SKILL.md |
| POST /workspaces/{wid}/skills/toggle | `{"path":"原生技能路径","enabled":true}` |
| GET /workspaces/{wid}/mcp | 用户/有效配置、用户层版本、来源、原生状态 |
| PUT /workspaces/{wid}/mcp/{name} | `{"version":"...","config":{...}}` 或 `{"version":"...","remove":true}` |
| GET /workspaces/{wid}/models | model/list 结果 |
| GET /workspaces/{wid}/config | 脱敏的 config/read 结果 |

附件先上传，再把返回的 `path` 放到 turns.files。图片转换成 `localImage` 输入；其他文件以明确的本地路径交给 Codex 工具读取。skills 数组元素为 `{"name":"...","path":".../SKILL.md"}`，必须来自当前原生清单并处于启用状态。

## 事件

```json
{
  "id": 123,
  "sessionId": "...",
  "time": "2026-09-27T00:00:00Z",
  "direction": "in",
  "method": "item/agentMessage/delta",
  "data": {
    "method": "item/agentMessage/delta",
    "params": {"threadId":"...","turnId":"...","itemId":"...","delta":"Hello"}
  }
}
```

`direction` 为 in/out/internal/stderr。in/out 的 data 是协议 envelope；RPC 响应没有 method，可以通过 envelope.id 与请求配对。结构化敏感字段会脱敏。internal 事件包括 run/input（原始用户消息、附件、Skills、笔记版本）、run/state、approval/pending/resolved/expired。实际提交的附加应用上下文见 out 的 turn/start。

SSE 使用标准 `id:` 和 `data:`，每条数据只有一行 JSON。客户端先按 event.id 去重，再持久化最后处理的游标。首次连接可从 0 重放；断线重连发送 Last-Event-ID。不要用 Token 增量或 message item 顺序代替事件游标。

Web UI 只在内存保留最近约 12000 条事件，事件页展示最近 250 条、对话页最近 200 项；完整日志可导出。服务端日志不会自动淘汰，长期运行应监控容量。

## 审批响应

命令和文件审批：

```json
{"decision":"accept"}
```

v0.4 按请求的 `availableDecisions` 校验；GET approvals 新增 `decisions` 数组供 UI 使用。显式空数组表示无可用决定。字段缺省时，命令使用 accept/decline/cancel，只有请求提供 proposedExecpolicyAmendment 才补规则对象；文件审批支持协议规定的 acceptForSession。不会自行添加命令会话批准或扩大命令前缀。

规则对象示例（必须与该请求提供的内容精确匹配）：

```json
{"decision":{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["echo","demo"]}}}
```

权限审批：`{"decision":"accept","scope":"turn"}` 或 `scope:"session"`，只批准原生请求的权限；decline 发送空权限。缺省仍为 turn。处理后保留 decision、scope、resolvedAt；审计事件同步记录。重复/过期/跨会话或未提供的决定返回 409。

`item/tool/requestUserInput`：

```json
{"answers":{"question_id":{"answers":["用户答案"]}}}
```

`mcpServer/elicitation/request`：

```json
{"decision":"accept","content":{"field":"value"}}
```

form 模式的 content 遵循原生 requestedSchema；url 模式应由用户完成外部认证后确认。所有审批使用本后台的 approval.id 路由到所属会话进程；原生 request.id 不接受客户端任意替换。已经处理、过期或后台重启前的审批不能重放。


## v0.2 新增接口与兼容性

原有 API 路径和事件 envelope 保持兼容。session 增加 `pinned`、`archived` 布尔字段；GET /sessions 仍返回全部会话，置顶优先、再按更新时间排序。归档后禁止提交新任务，恢复后可继续原有 thread。

| 方法 / 路径 | 请求与结果 |
| --- | --- |
| PATCH /sessions/{sid} | `{"title":"新名称","pinned":true,"archived":false}`，字段均可省略 |
| DELETE /sessions/{sid} | 仅允许非运行状态；删除 RunDesk 元数据、审批记录及事件，保留文件与原生线程 |
| GET /sessions/{sid}/export?format=markdown | 用户/助手消息导出，不附加隐藏注入笔记或原始协议；默认仍为 JSONL |
| DELETE /workspaces/{wid}/skills/{name} | 仅项目 Skill；将 SKILL.md 移至 `.rundesk/skill-backups/`，返回 backupPath |
| GET /workspaces/{wid}/mcp/export | schemaVersion=1 的 RunDesk MCP JSON；结构化密钥隐藏 |
| POST /workspaces/{wid}/mcp/import | `{"version":"...","bundle":{...},"overwrite":false}`，全量校验后一次写入，保留未导入服务 |
| GET /workspaces/{wid}/diagnostics | checks 数组含名称、状态、说明与耗时；不进行模型推理 |

历史事件查询支持 `limit`（1–1000）、`direction`（in/out/internal/stderr）、`method`（方法子串）、`q`（JSON 内容的字面关键词）、`category`（tools/approvals/errors）、`from` 与 `to`（RFC3339，含边界）。可以与 after 组合分页；SSE 也接受这些过滤参数。时间按时刻比较，允许时区偏移。查询只返回匹配事件，不返回总数；返回数量等于 limit 时可继续分页。

示例：

```text
GET /api/sessions/SESSION_ID/events?category=tools&direction=in&q=vision&limit=250&after=0
```

MCP 导入样例：

```json
{
  "version": "原生用户层当前版本",
  "overwrite": false,
  "bundle": {
    "schemaVersion": 1,
    "kind": "rundesk.mcp",
    "servers": {
      "vision": {"url":"https://example.com/mcp","bearer_token_env_var":"VISION_TOKEN","enabled":false}
    }
  }
}
```

导出中的 `[redacted]` 仅能在同名现有服务中沿用原值；在新环境或新服务中导入时需补充这些值。MCP 导入没有执行端回滚事务：写入由 Codex 原生配置接口一次完成，若随后重载失败，会明确返回 saved=true 和 reloadError，不把它报告为完全生效。首轮模型任务或真实 MCP 工具是否可执行，需要另行验证。

## v0.3 实例与作用域

| 方法 / 路径 | 请求与结果 |
| --- | --- |
| GET /instances | 实例数组：id/name/description/defaultModel/codexHome/managed/revision/created |
| POST /instances | `{"name":"视觉助手","description":"...","defaultModel":""}`；创建独立配置目录 |
| PATCH /instances/{iid} | `{"name":"...","description":"...","defaultModel":"...","revision":0}`；三个元数据字段全量替换，revision 必须匹配；不接受目录修改 |
| POST /instances/{iid}/reload | 关闭空闲连接，返回 closedConnections、busyConnections；活跃任务保留 |
| GET /workspaces/{wid}/account?instanceId={iid} | 原生 account/read，refreshToken=false；不进行登录或模型调用 |

POST /sessions 新增可选 instanceId。省略或空值选择 default；返回的 session 永远包含 instanceId。空 model 使用实例 defaultModel；后者也为空时交给 Codex 决定。已有会话的实例、工作区和模型不可通过 PATCH 修改。GET /sessions 仍返回全部实例的会话，客户端按 instanceId + workspaceId 筛选。

下列工作区接口均接受 `?instanceId=...`：skills（含 toggle）、mcp（含 import/export）、config、models、diagnostics、account。省略时仍为 default。工作区笔记、uploads 和文件访问继续按工作区共享，不随实例复制。

Skill 文件 GET/PUT/DELETE 另接受 `scope=project|instance`；旧 API 默认 project，WebUI 新建默认 instance。`skills/list` 返回 Codex 在该实例和工作区发现的全部来源，不接受 scope 筛选。例：

```text
PUT /api/workspaces/WORKSPACE_ID/skills/vision-guide?instanceId=INSTANCE_ID&scope=instance
GET /api/workspaces/WORKSPACE_ID/mcp?instanceId=INSTANCE_ID
```

DELETE Skill 返回 backupPath，project 相对于工作区，instance 相对于对应 CodexHome。内置与其他来源文件不通过这些 CRUD 接口编辑；可通过原生 toggle 开关控制。turns.skills 必须来自会话绑定实例的启用清单，其他实例的私有 Skill 路径会被拒绝。

实例只是配置身份，不是访问控制。默认实例的目录跟随 RunDesk 服务环境，新实例目录固定。`run/input` 事件增加 instanceId；原生配置连接的日志使用内部 `config-<instanceId>-<workspaceId>` 标识，与模型会话事件分开。


## v0.4 权限与运行状态

实例 POST/PATCH 新增可选 `permissions` 对象：

```json
{"name":"本地助手","description":"","defaultModel":"","revision":0,"permissions":{"sandbox":"workspace-write","approvalPolicy":"on-request","reviewer":"user","networkAccess":false}}
```

sandbox 为 workspace-write/read-only/danger-full-access；approvalPolicy 为 on-request/never；reviewer 为 user/auto_review。networkAccess 仅适用于 workspace-write，省略表示沿用 Codex。传 permissions 是整体替换；省略 permissions 保留原策略。缺失旧数据使用历史默认值。

| 接口 | 返回 |
| --- | --- |
| GET /sessions/{sid}/runtime | 本次/上次连接记录、live、requested、effective、notices |
| GET /instances/{iid}/runtime | 最近 50 个会话/配置连接记录 |
| GET /workspaces/{wid}/diagnostics?instanceId=... | checks 支持 ok/error/warning/skipped；沙箱项包含 command 数组、output、exitCode、hint |

notices 按 source+message 合并次数，每个连接最多保留 32 条、单条最多 8 KiB；完整事件仍在原日志中。新连接重新计数，旧完整事件保留。effective 只保留 App Server 返回的模型、Provider、cwd、沙箱、审批策略等字段；缺失字段不推测。runtime/effective 内部事件记录请求与实际值。重启后运行摘要标记历史，不视为当前故障。

诊断固定使用 workspace-write 与禁网，保留实例 CODEX_HOME、服务环境和工作区；15 秒单命令超时，不调用模型，不在失败后切换为沙箱外执行。Windows/macOS 分支尚待实机验收。

## v0.5 轨迹读取

`GET /api/sessions/{sid}/trace?after=0&limit=250&through=0`

返回 `{events, nextCursor, hasMore, snapshot, session, observedAt}`。events 沿用事件 envelope，只包括运行、item 生命周期、审批、Token、压缩、告警与运行配置等语义记录；逐字 delta 不在此接口返回。原始事件不变。

- limit 为 1–500，缺省 250；after 为已处理事件 ID。
- 第一页 through=0 获取当前快照。hasMore=true 时，后续页面传回 snapshot 作为 through，并使用 nextCursor；读完后下一次增量查询将 through 归零。
- nextCursor 可跨过没有轨迹意义的原始事件，不能用返回事件数量推算它。
- 大文本只返回有界预览，预览会带截断提示。客户端按 item/turn/request ID 合并生命周期，不能把预览当完整审计日志。
- observedAt 是本次后台读取时间；仅用于仍在运行的观测区间，不是工具结束时间。

`GET /api/sessions/{sid}/events/{eid}` 返回该会话中指定事件的完整记录；其他会话的事件 ID 返回 404。两个接口都沿用现有鉴权，没有新增写操作。完整 JSONL 仍使用原有导出接口。

## 运行中补充指令（v0.5.3）

`POST /api/sessions/{sid}/steer`

```json
{"text":"先解释原因，不修改文件","files":[],"skills":[],"expectedTurnId":"当前会话的 turnId","requestId":"客户端生成的唯一提交 ID"}
```

成功返回 HTTP 200：`{"requestId":"...","turnId":"...","status":"accepted"}`。accepted 仅表示 App Server 接收，不表示模型已经处理完毕。

仅接受 running / waiting 状态；starting / stopping、无活动 turn、目标 turn 不匹配时返回 409。服务器不会回退为 turn/start。文件及 Skills 复用既有输入校验。客户端在同一提交重试时必须保留 requestId 和完整请求；不同消息必须使用不同 ID。已接收的重复请求返回原回执，不再次注入。结果不明确的请求不会重新发送，需要查看对话/事件记录。服务重启后也保留去重记录，删除会话时清理。

WebUI 的补充按钮发送到当前轮次，停止按钮调用原有 stop 接口。此版未加入下一轮消息队列。
