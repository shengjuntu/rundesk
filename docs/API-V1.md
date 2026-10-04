# RunDesk 应用接口 v1（RunDesk 0.7.0）

RunDesk 为两类客户端提供同一套运行能力：人通过 WebUI，应用通过 HTTP API。Codex 执行 Agent loop；应用负责自己的交互、业务数据和审核流程。

## 地址、认证与兼容

- 新入口：`http://127.0.0.1:3210/api/v1`。运行中的 OpenAPI：`GET /api/v1/openapi.json`；源码内文件为 `internal/app/openapi.json`。
- 后台 Token：`Authorization: Bearer $RUNDESK_TOKEN`；同源 WebUI 使用登录 Cookie。来源校验、文件边界与旧接口相同。
- `/api` 兼容接口保留，已有 news2douyin 0.11 和 Video App 客户端无需立即迁移。WebUI 已使用 v1。
- v1 保持已有字段含义与响应顶层结构，允许新增可选字段。应用应忽略未知字段。破坏性变化将使用新的 API 主版本。
- 事件 envelope、会话字段和提交回执由 RunDesk 定义。`config`、`models`、`skills/list`、MCP 状态等原生载荷仍受 Codex App Server 版本影响，OpenAPI 中标为 NativeObject，不能将原生内部字段视为独立稳定协议。

本版仍是单用户后台，不是多租户平台。`source.appId` 是调用方声明的业务标签，不是认证身份；具有后台 Token 的客户端共享访问权限。独立应用凭据和授权范围留待后续版本。

## 资源与状态

| 资源 | 作用 |
| --- | --- |
| Instance | 长期配置身份：独立 CodexHome、默认模型、实例 Skill/MCP、权限默认值 |
| Workspace | 工作目录与项目笔记；一个实例可用于多个工作区 |
| Session | 固定绑定实例、工作区和创建时确定的模型，保留原生线程 |
| runId | RunDesk 为一次任务分配的编号，每次新提交改变 |
| turnId | Codex 为当前原生轮次分配的编号，启动阶段可能尚未产生 |
| Event | 已持久化的运行事件，使用递增 id 续传 |
| Request receipt | HTTP 操作的原始接收结果；与任务是否完成分开 |
| File | 工作区上传文件或会话 outputs 中的产物 |

会话状态：idle → starting → running / waiting → completed / interrupted / failed。停止期间为 stopping。等待审批不是执行失败。未知原生状态原样保留，客户端不可假定终态集合永远不变。

## 创建与提交去重

以下 v1 POST 必须提供 `Idempotency-Key`：`/instances`、`/workspaces`、`/sessions`、`/sessions/{sid}/turns`。Key 为 8–128 字符，首字符为字母或数字，其余允许字母、数字、下划线、点、冒号、连字符，建议使用 UUID。

应用应先把 Key 保存进自己的业务任务记录，再提交。Key 在一个 RunDesk 数据目录内全局唯一，不是按应用名隔离。相同 Key、方法、路径、查询参数和 JSON 内容返回原回执；对象字段顺序不影响比较，数组顺序有意义。不同内容使用同一 Key 返回 `409 idempotency_conflict`。

```http
POST /api/v1/sessions
Idempotency-Key: create-case-42-session
Content-Type: application/json

{"workspaceId":"WORKSPACE_ID","instanceId":"INSTANCE_ID","source":{"kind":"application","appId":"news2douyin","taskId":"case-42"}}
```

```http
POST /api/v1/sessions/SESSION_ID/turns
Idempotency-Key: research-case-42-attempt-1
Content-Type: application/json

{"text":"研究这条新闻的历史背景，保存引用和报告。","files":[],"skills":[]}
```

返回 202 Session 是“已接收”，不是“已完成”。`Idempotency-Replayed: true` 表示回放原回执；回执中的 starting 等状态不会跟随任务更新，当前状态应再读 `GET /sessions/{sid}`。

超时后，查询：

```http
GET /api/v1/requests/research-case-42-attempt-1
```

- `state=completed`：HTTP 处理结果已记录。检查 `httpStatus` 与 `response`；成功回执包含 session id / runId，失败回执也会保留。
- `state=processing`：原请求仍在当前进程中处理。同 Key 重试返回 `409 request_in_progress`、`Retry-After: 1`、`retryable=true`，不会再次执行。
- `state=unconfirmed`：进程中断或记录未完成，无法确认操作结果。不会重新执行；先检查会话、来源业务编号与事件。不能盲目换新 Key。
- `404 request_not_found`：此 Key 尚未找到记录，可以重试原请求和原 Key。请求可能仍在传输，继续复用 Key 才能去重。

接收记录在调用业务逻辑前持久化，结果随后持久化。崩溃窗口标为不确定，本版不承诺跨外部副作用的 exactly-once。记录不自动过期，删除会话也不删除旧回执，避免迟到请求重新创建或执行；长期使用应监控数据目录容量。

旧 `/api` 创建接口可以不带 Key，保持旧行为；主动带 Key 时同样获得去重保护。v1 不会对上传、审批等其他操作自动套用创建 Key，具体操作遵循自己的协议。

## 补充、停止与审批

运行中补充沿用已有 `POST /sessions/{sid}/steer`，携带 `expectedTurnId` 和稳定 `requestId`；它不会退化为新任务提交。

v1 停止必须带目标 runId：

```http
POST /api/v1/sessions/SESSION_ID/stop
Content-Type: application/json

{"expectedRunId":"RUN_ID"}
```

目标已完成或正在停止时，同一 runId 返回成功；如果会话已经开始另一个 run，返回 `409 run_conflict`，不影响新任务。WebUI 同样使用这个前置条件。旧 `/api/.../stop` 保持不带请求体的调用方式。

审批仍读取 pending approvals 并按服务端提供的 decisions 提交；过期审批不能重放。不新增默认批准、不扩大权限。HTTP 200 表示停止/审批请求被处理，最终运行结果以状态和事件为准。

## 错误与关联编号

```json
{"error":"会话不存在","code":"session_not_found","requestId":"a-generated-id","retryable":false}
```

0.8.2 的新错误响应还提供可选 `details`：`time`、`origin`、`method`、`path`（不含查询字符串）、`durationMs`、`causes`；识别到 Codex RPC 错误时还有 `rpcCode` 和可用的 `rpcData`。错误文本和结构化数据会遮盖常见密钥字段。历史幂等回执保持原样，因此客户端必须允许 `details` 缺省。API 描述版本为 1.3.1。

响应包含 `X-Request-ID` 和 `RunDesk-API-Version`。可以传入有效的 `X-Request-ID` 关联业务日志；它不是幂等 Key，不影响去重。幂等回放保留原回执内容，错误回执中的 requestId 属于原请求；当前 HTTP 访问编号以响应头为准。

关键代码：invalid_request、unauthorized、forbidden、session_not_found、instance_not_found、workspace_not_found、revision_required、revision_conflict、session_busy、session_archived、run_conflict、idempotency_key_required、idempotency_conflict、request_in_progress、request_unconfirmed、request_not_found、storage_unavailable。

`retryable=false` 不是“永远不能操作”，表示不能据此自动重发。HTTP 5xx 或网络错误可能发生在操作已接收之后，必须保留 Key。接口找不到路径或方法时返回 JSON 404；过大的 JSON 请求返回 413。

## 事件、产物与业务关联

`GET /sessions/{sid}/events?stream=1&after=N` 返回 SSE。以持久化事件 id 去重，重连可使用 after 或 Last-Event-ID，取两者中较大的有效游标。关闭 SSE 不停止模型任务。普通 GET 同一路径按 after/limit 分页，最多 1000 条。

应用可用 `GET /sessions?appId=news2douyin&taskId=case-42` 找到自己标记的会话，也可按 instanceId、workspaceId 筛选。`source` 创建后固定，业务 taskId 不要求全局唯一，一个业务课题可以有多次执行会话。

产物通过原有 files / uploads / file 接口读取；结构化业务结果建议由应用自己的 MCP 工具写回，例如 news2douyin 保存来源、证据和报告。RunDesk 不解析这些应用的业务数据库。

## 实例与会话配置

- `GET /instances/{iid}/configuration?workspaceId={wid}`：只读实例、工作区与关联会话数，不启动 Codex。
- 增加 `probe=1`：主动发现 Skills 和 MCP，可能建立配置连接，不调用模型 turn。每项失败单独返回 errors。
- `GET /sessions/{sid}/configuration`：增加会话绑定、上次提交选用的 Skills 和模型，以及原生 runtime 记录。也可显式 `probe=1`。
- 当前能力发现不等于本次会话调用记录，不保证 MCP 执行成功。实例默认模型与已有会话模型分开显示；运行生效值只使用 Codex 实际返回值。
- v1 `PATCH /instances/{iid}` 必须带当前 revision；只替换提供的字段，省略字段保持原值。旧 `/api` 仍沿用三个元数据字段整体替换的语义。
- Skills CRUD / MCP 管理继续使用工作区接口并携带 instanceId。实例 Skill 写入 scope=instance；项目 Skill 写入 scope=project。MCP 保存继续使用原生版本冲突检查。

可运行的标准库 Python 客户端见 `examples/application_client.py`。它不自动重试写操作，由应用决定何时查询或继续。


## 已完成回复与评价（0.6.1 新增）

这三个接口沿用上述认证、同源检查、关联编号与错误格式。OpenAPI 文档版本增至 1.1.0；接口路径仍为 `/api/v1`。

| 方法与路径（省略 /api/v1） | 行为 |
| --- | --- |
| `GET /sessions/{sid}/messages/{eid}` | 从持久化事件读取完成的模型回复，返回 sessionId、eventId、itemId、turnId、text、time。text 保留原始 Markdown。 |
| `GET /sessions/{sid}/feedback` | 返回此会话的评价数组；没有评价时为 `[]`。 |
| `PUT /sessions/{sid}/messages/{eid}/feedback` | 设置或取消一条回复的评价。 |

`eid` 是方向为 `in`、方法为 `item/completed`、item.type 为 `agentMessage` 的**持久化事件 id**，不是 itemId 或 turnId。后台验证该事件属于 sid。流式片段、用户消息、命令和不存在的事件不能写评价，客户端无需上传回复原文。

```json
{"rating":"down","comment":"需要补充事件背景和来源。"}
```

rating 必须为 `up`、`down` 或 `none`。comment 选填，去掉首尾空白后最多 2000 个 Unicode 字符；rating=none 会同时清空 comment。响应包含 sessionId、eventId、rating、comment、updated。

评价保存在 RunDesk 数据库，重启后保留，随会话删除。当前为单用户部署：每条回复只有一份评价，多客户端采用最后写入覆盖，不代表按应用/用户隔离的评分，也不会自动发给模型服务或触发模型训练。WebUI 在打开会话时读取评价，保存后更新本页面；其他已打开页面需重新打开会话或刷新。

PUT 设置一个确定的状态，可用相同内容重试，不要求 Idempotency-Key；重复 PUT 不增加记录，updated 为最后一次成功写入时间。网络响应丢失时，先 GET 当前评价确认状态。

错误代码：`invalid_event_id`（400）、`message_not_found`（404）、`message_not_completed`（400）、`invalid_rating`（400）、`comment_too_long`（400）。不同会话之间不会通过 eid 返回回复原文。

分享由 WebUI 先展示选中回复，再复制、下载 Markdown，或由用户点击系统分享；此版本不建立公开链接。朗读通过浏览器语音接口进行，不调用 RunDesk/Codex 的模型任务。两者没有新增后台发布或音频生成接口。


## 应用登记（0.7.0）

`GET /applications` 返回应用列表及按所属 instance 汇总的 sessionCount、activeCount 和 lastActivity。`GET /applications/{appId}` 返回单个应用。

`PUT /applications/{appId}` 接收 name、description、instanceId、workspaceId、entryUrl 和可选 revision。name、instanceId 必填；appId 为 1–80 个字母、数字、点、下划线、冒号或连字符，首位为字母或数字。entryUrl 为空或不带登录凭据的 HTTP(S) 地址。workspaceId 用于管理视图默认项目，创建业务会话仍需明确传入 workspaceId。

一个应用固定绑定一个专用 instance，每个 instance 也只能属于一个应用。`default` 留给通用助手。修改绑定返回 `409 application_binding_conflict`；修改名称等元数据需当前 revision，冲突返回 `409 revision_conflict`。完全相同的 PUT 重试返回已有记录，不增版本，也不创建会话；不要求 Idempotency-Key。登记不创建 Codex 任务。

旧客户端的首条带 `source.kind=application` 的新会话可自动登记（专用、无冲突的配置）。启动迁移仅使用历史 source 的一对一证据，不凭实例名称猜测。默认配置和有歧义的历史任务保持可用。应用来源仍不是独立权限，本版继续使用后台 Token/Cookie。

## 完整技能目录（0.7.0）

以下路径均支持 `instanceId` 和 `scope=instance|project`；新目录 API 默认 scope=instance。旧 `/skills/{name}` 接口的默认 scope=project 保持不变。

| 方法与路径 | 用途 |
| --- | --- |
| POST /workspaces/{wid}/skill-bundles?name={name} | 导入一个完整技能；Content-Type 为 application/zip 或 multipart/form-data |
| GET /workspaces/{wid}/skill-bundles/{name} | 文件相对路径、字节数、可执行标记与总大小 |
| GET /workspaces/{wid}/skill-bundles/{name}/file?path={relativePath} | 安全文本预览；二进制只返回说明，长文本显示前 256 KiB |
| GET /workspaces/{wid}/skill-bundles/{name}/export | 导出完整 ZIP，保留脚本可执行位 |
| DELETE /workspaces/{wid}/skill-bundles/{name} | 将整个技能目录移动到备份位置，返回 backupPath |

multipart 每个字段名是文件相对路径（如 `research/scripts/run.py`），上传文件名仅作提示。ZIP 与目录都接受根部 `SKILL.md`，或一层包装目录。不得夹带多个技能；总计最多 1000 个文件、32 MiB，SKILL.md 最多 256 KiB。拒绝绝对路径、父目录跳转、符号链接、特殊文件和大小写冲突。普通资源二进制可保留。空目录不单独归档；浏览器上传无法携带原生权限，以 shebang 标记脚本可执行。

同名技能默认返回 `409 skill_exists`。用户选择替换时加 `replace=1`，先完整备份原目录，再安装新目录。备份位于当前根目录下 `.rundesk/skill-backups/`，相对位置包含在响应中。上传、浏览、导出沿用现有认证和同源检查；导入不会执行技能脚本。原有 SKILL.md 编辑不影响兄弟文件；旧的单 MD 删除接口也保留旧语义，新 UI 使用完整目录移除。

OpenAPI 文档版本 1.2.0；48 个路径、57 个操作。应用执行协议仍为 `/api/v1`。

## 0.8.0：独立轨迹分析会话

`POST /api/v1/sessions` 新增一个与普通创建参数互斥的请求形式，继续使用已有 `Idempotency-Key` 与提交回执：

```json
{
  "traceAnalysis": {
    "sessionId": "原任务会话 ID",
    "runId": "所选运行 ID",
    "eventIds": [123, 124]
  }
}
```

服务端校验来源、运行与事件归属，固定原会话当时的最大事件游标。返回新 session，`threadId` 初始为空，`source.kind` 为 `human`，`traceOrigin` 包含 `sessionId`、`runId`、`eventIds`、`through`、`capturedAt` 和来源标题。沿用来源实例、项目和模型；不接受同时设置 workspaceId、instanceId、model、title 或业务来源。最多携带 32 个事件编号。

创建本身不调用模型、不提交原任务；后续将问题提交到**新 session 的** `/turns`。首次运行启动新的原生线程，恢复时仍沿用该分析线程；只读 MCP 的来源快照保持不变。响应关联信息会持久化并出现在会话列表。新会话的事件、停止、审批和反馈走现有接口。

内置 stdio MCP 为 `rundesk_trace`，只在分析会话的 thread/start 或 thread/resume 覆盖配置中加入。工具为 `trace_list_runs`、`trace_find_steps`、`trace_get_step`、`trace_read_event`、`trace_statistics`。这些工具不开放为匿名 HTTP 查询端点，不接收 SQL。详情与边界见 [TRACE.md](TRACE.md)。OpenAPI 文档更新为 1.3.0，路径和操作数量保持 48 / 57。
