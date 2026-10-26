# RunDesk Control API（RDCA）v1

RunDesk 自身 API 的名称为 **RunDesk Control API（RDCA）**。当前契约随 RunDesk 0.49.4 发布。基础路径 `/api/v1`；机器可读定义为 `GET /api/v1/openapi.json`，源码为 `internal/app/openapi.json`，由 `scripts/generate-openapi.py` 生成。既有 `/api` 入口保留兼容，新应用使用 v1。

RunDesk 不依赖 HarnessRouter。UHP 是后续独立兼容入口的目标，未来与 RDCA 共用任务、权限和轨迹核心；当前未实现或声明 UHP 兼容。本次命名不改变既有请求、响应和行为，详见 [API 定位与兼容计划](API-STRATEGY.md)。

## 稳定范围

应用依赖任务、会话、文件、事件信封、错误与幂等语义。保持既有必需字段与含义，新增可选字段、错误码或原生事件时客户端应容忍未知值。后端原生 `data`、模型配置、Kun 内部调试/实验接口不等于跨后端统一能力；使用前读取对应能力声明。

0.49.1 的 `Session.observeHTTP` 表示保存的 Codex HTTP 采集偏好，默认关闭。`PATCH /sessions/{sid}` 可传 `{"observeHTTP":true}` 或 `false`；沿用会话写入权限和归属校验，应用凭据需 `read` + `run`。修改不打断当前任务，下一轮提交时重连并恢复原 thread 应用。`observation.pending` 表示偏好待应用，`observation.appliedEnabled` 表示原生线程已确认配置代理；开启偏好但不支持接入时后者仍为 false。保存值不等于采集已发生。Kun 拒绝开启此 Codex 专用设置。完整配置与覆盖边界见 [统一调试](UNIFIED_DEBUGGING.md)。

OpenAPI 同时包含应用和管理接口。`x-administrator-only=true` 为管理操作；`x-application-scopes` 列出应用所需 scopes，此外仍受应用、实例、项目和资源归属约束。不能将所有列出的路径都视为应用授权。

## 认证和归属

管理员先登记应用并配置专用实例，再创建独立应用凭据。应用后端使用：

```http
Authorization: Bearer <application-token>
Content-Type: application/json
```

完整凭据仅在创建时返回一次；服务端保存哈希。凭据不能放到浏览器代码中。`GET /whoami` 返回调用身份；`GET /workspaces` 只返回此凭据允许的项目 `id/name`。

| Scope | 用途 |
| --- | --- |
| read | 必需；本应用任务、会话、消息、事件、轨迹与幂等回执 |
| run | 提交/继续/停止/取消任务，管理自己的会话 |
| files | 授权项目的上传、文件列表与读取 |
| approvals | 还需 run；回应本应用执行审批 |
| schedules | 还需 run；管理本应用定时规则 |

应用请求可省略 instanceId/source，由凭据绑定。显式提交其他归属会拒绝；`source.taskId` 仅为业务标签，不负责去重。模型、Skills、MCP、安装、全局并发及凭据管理属于管理员。

[注册与集中配置](APPLICATION-CONNECT.md) · [凭据生命周期和范围](APPLICATION-CREDENTIALS.md)

## 应用 Docker 环境

读取 `/meta` 的 `application-runtime` 能力后，可使用：

| 操作 | 路径 | 权限与行为 |
| --- | --- | --- |
| 读取状态 | GET `/applications/{appId}/runtime` | read；缓存观察值，不启动或探测 Docker |
| 准备环境 | POST `/applications/{appId}/runtime/prepare` | read + run；仅本应用当前绑定的 Codex Docker 环境 |

准备请求：

```json
{"instanceId":"BOUND_INSTANCE_ID","workspaceId":"BOUND_WORKSPACE_ID","probe":true}
```

返回 appId、instanceId、workspaceId、executionMode、instanceRevision、image，以及可选 environment（id/state/imageId/revision/observedAt/pendingUpdate）。probe=true 另返回 probed、accountReady、servers（MCP 名到工具名数组）和 errors。modelTested 始终为 false；认证元数据不证明实际模型可用。响应不暴露认证内容、宿主路径或 MCP 原始配置。

prepare 按实例/工作区串行保证容器复用，可重复调用，不使用任务幂等回执，也不提交模型任务。保持现有活动会话及租约；不会改变镜像、构建、更新配置或删除环境。绑定变化返回 `application_binding_changed`，非 Codex Docker 配置返回 `docker_configuration_required`，已选择配置但环境未更新返回 `environment_update_required`（均为 409）；按错误提示在管理界面处理。

### 已选容器诊断（0.49.3）

`/meta` 声明 `application-runtime-diagnostics` 后，应用可调用 `POST /applications/{appId}/runtime/diagnostics`（read + run；当前应用绑定）。它只探测已运行的 Codex Docker 容器，不会启动、选镜像、重建或回退宿主机，也不调用模型。先执行 `prepare`（`probe:true`）可初始化实际配置读取连接；配置连接不存在或忙时，图片结果明确返回 `mcp_config_unavailable`。

```json
{
  "instanceId": "BOUND_INSTANCE_ID",
  "workspaceId": "BOUND_WORKSPACE_ID",
  "nonce": "0123456789abcdef01234567",
  "mcpImageServer": "YOUR_CONFIGURED_HTTP_MCP_SERVER",
  "challenge": {
    "path": "outputs/OWNED_SESSION_ID/.rundesk-probes/0123456789abcdef01234567/request.json",
    "nonce": "0123456789abcdef01234567"
  }
}
```

`nonce` 为 24 位小写十六进制；`challenge` 与 `mcpImageServer` 均可省略。共享目录挑战必须属于本应用、实例和工作区的现有会话：应用写入 `{ "schemaVersion": 1, "nonce": "..." }`；容器以拒绝符号链接的目录访问读取，原子、排他创建同目录 `response.json`，内容为 `schemaVersion`、`nonce` 和原始请求字节的 `requestSHA256`。应用须独立读取响应、验证哈希与请求未变化，并清理本次挑战目录；仅 HTTP 成功不证明共享挂载一致。

返回顶层 `probed`、`accountReady`、`servers` 和 `errors`，来自现有容器配置连接的只读 account/read、mcpServerStatus/list；连接未初始化/忙时明确 probed=false，不会创建连接或启动环境。GET runtime 仍然只是缓存状态，不提供本次认证/工具探测结果。`runtime` 中的实际 profile ID/版本、Python、FFmpeg/FFprobe 版本、适配器契约版本和源码 commit，以及 `checks`（profile/ffmpeg/ffprobe/pythonDependencies/adapter/sourcePin）由固定容器程序报告。pythonDependencies 仅核对已安装分发包元数据与 profile.lock 精确版本，不导入重型模型或执行渲染。调用方须核对自己要求的工具版本。不会把旧镜像默认为新契约；缺失字段、工具或版本命令失败明确标为 incomplete/missing/invalid。

可选图片检查从同一运行容器使用实际有效 MCP 配置，固定调用只读 `rundesk_diagnostic_image({nonce})`。尊重服务启用、enabled_tools/disabled_tools；HTTP 请求头和 token 环境变量只在容器内使用，配置经 stdin 传递。仅支持 HTTP JSON；SSE/stdio 返回 unsupported，重定向拒绝，响应不超过 2 MiB。`imageTransport` 返回 state（passed/failed/unsupported）、nonce、PNG SHA-256、mimeType、width、height 和可选固定错误 code；调用方须与预先约定的测试图独立核对。该接口没有自定义工具名、参数、URL 或命令入口。

诊断前后校验应用绑定、实例/工作区/环境修订、容器归属、镜像和有效 MCP 配置；变化返回 409。响应不包含宿主路径、URL、密钥、原图或原始命令输出。`modelTested`、`modelVisionVerified` 始终 false：图片通过实际容器的 MCP HTTP 传输，与模型在某次任务中理解了图片，是不同的验收步骤。

## 最小任务流程

```bash
curl --fail-with-body "$RUNDESK_URL/api/v1/tasks" \
  -H "Authorization: Bearer $RUNDESK_APP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: business-operation-0001' \
  -d '{"workspaceId":"WORKSPACE_ID","title":"整理结果","input":{"text":"读取已授权输入文件，整理结果并写入 outputs。"}}'
```

返回 **202** 与 Task 对象，包括 `id`、`sessionId`、`status`、`spec`；只表示接受排队。保存这些 ID，然后查询 `GET /tasks/{id}`，通过 `sessionId` 获取会话、轨迹、回复与文件。不要从 202 或幂等回执 completed 推断任务完成。

| 操作 | 路径 | 语义 |
| --- | --- | --- |
| 提交排队任务 | POST `/tasks` | TaskSpec，必填 workspaceId/input，需幂等键 |
| 列出任务 | GET `/tasks` | `{items,nextCursor}`；limit 1–500；可按 status/appId/instanceId 筛选 |
| 查询任务 | GET `/tasks/{id}` | 返回当前 Task |
| 取消任务 | POST `/tasks/{id}/cancel` | 排队任务不启动后端；运行中取消与完成可能竞争 |
| 新建直接会话 | POST `/sessions` | 必填 workspaceId，需幂等键 |
| 继续会话 | POST `/sessions/{sid}/turns` | Input，需新的逻辑操作键；相同请求重试复用原键 |
| 查询会话 | GET `/sessions/{sid}` | 当前 runId/turnId/status 与归属 |
| 停止当前运行 | POST `/sessions/{sid}/stop` | `{expectedRunId}` 防止误停后续运行 |

TaskSpec.input 的稳定入口是 `{text,files?,skills?}`；files 使用服务端返回的项目相对路径，skills 引用管理员已安装的能力。模型和权限由配置决定，应用不能借输入扩大权限。

常见任务状态有 queued、dispatching、starting、running、waiting、stopping 及终态 completed、failed、interrupted、canceled、unconfirmed；未知状态不应自动判成功。等待审批仍占并发额度。重启保留排队任务；已开始或结果不明的工作不自动重跑。完整队列约束见 [QUEUE](QUEUE.md)。

## 事件与回复

`GET /sessions/{sid}/events?after=0&limit=1000` 返回事件数组，按宿主 id 升序；将最大 id 保存为下次 after。每页最多 1000 条。

`GET /sessions/{sid}/events?stream=1&after=<id>` 返回 SSE，支持 `Last-Event-ID`；取它与 after 中较大的游标恢复订阅。服务每约 300ms 检查新事件；客户端按事件 id 去重。SSE 连接结束不证明运行结束，应重新查询任务/会话状态。

```json
{"id":123,"sessionId":"...","time":"...","direction":"internal","method":"run/state","data":{}}
```

信封稳定，method/data 保留运行时差异；不要将 Codex 原生字段假定为 Kun 字段，也不要将 Kun sequence 用作宿主 id。流式 delta 是过程证据，最终回复应采用明确的完成消息/最终阶段记录。`GET /sessions/{sid}/messages/{eid}` 读取可识别的助手消息，`GET /sessions/{sid}/export?format=markdown` 导出对话。

轨迹和只读调试说明见 [DEBUG](DEBUG.md)、[DEBUG-SERVICE](DEBUG-SERVICE.md)。未知、截断、缺失用量保持未知，不推算隐藏状态。Kun 的单步、断点和分叉为管理员扩展能力。

## 文件与产物

- `POST /workspaces/{wid}/uploads` 使用 multipart 字段 `file`，单文件最多 32 MiB；201 返回 `{path,name,size}`。上传不支持任务式幂等，网络结果不明时不要盲目反复上传。
- 将返回的 `path` 放进任务 input.files。保留文件与调用身份的归属，不构造其他应用上传路径。
- `GET /sessions/{sid}/files` 列出该会话可见文件。
- `GET /workspaces/{wid}/file?path=<URL编码路径>` 获取授权文件；允许 uploads/outputs 下的普通文件。`preview=1` 对受支持类型返回受限预览。
- 工具可生成其他工作区文件，但并非任意路径都自动成为可下载产物；需要交付的内容应写入平台约定的 outputs。

文件存在不等于本次创建，更不等于业务内容正确；Kun 固定产物规则用于进一步验收。

## 审批、取消与恢复

`GET /sessions/{sid}/approvals` 读取待审批列表，按返回的请求与允许决定回应 `POST /sessions/{sid}/approvals/{aid}`；不可将所有后端审批假定为同一原生结构。自动化应用仅在已获得 approvals scope 且明确允许的业务范围内作决定。

取消、超时和中断不撤销已发生的文件修改或远端操作。Codex 可显式核对并继续原生任务，Kun 有独立安全检查点恢复；两者并非可互换的恢复协议。结果未知时先检查证据与业务状态，不能换后端自动重跑。

[Codex 恢复](RECOVERY.md) · [Kun 检查点](KUN-CHECKPOINTS.md)

## 幂等与错误

提交 tasks、sessions、turns、recover、schedules 等 OpenAPI 标记的操作时，必须提供 `Idempotency-Key`，8–128 个允许字符；先将 Key 和请求体持久化到业务侧，再发请求。

- 同凭据、同 Key、同请求返回保存的 HTTP 回执，响应头 `Idempotency-Replayed: true`；这不是当前运行状态。
- 同 Key 不同请求为 409。幂等命名空间按 credential.id 隔离，两个同应用凭据也不共用；轮换后应根据 task/session ID 核对，不能用新凭据盲目重发旧业务 Key。
- `GET /requests/{key}` 查询回执，state 为 processing、completed 或 unconfirmed；completed 只表示 HTTP 处理结束。
- request_in_progress 需等待；request_unconfirmed 需检查副作用，不允许通过新 Key 掩盖不确定性。服务重启不自动重新执行原请求。

错误形状：

```json
{"error":"可读原因","code":"机器错误码","requestId":"关联编号","retryable":false,"details":{}}
```

检查 HTTP 状态和 code，保留 requestId。典型状态为 400 参数、401 认证、403 范围、404 资源、409 版本/幂等/状态冲突、413 上限、429 容量、502 上游协议、503 不可用、504 超时。retryable 不等于可用新 Key 重新提交；读回执并遵守同一逻辑操作语义。

## 可执行示例与契约维护

[Python 客户端](../examples/application_client.py) 和 [最小接入程序](../examples/application_quickstart.py) 只使用 Python 标准库。运行前设置 RUNDESK_URL、RUNDESK_APP_TOKEN、RUNDESK_WORKSPACE_ID 和本次固定的 RUNDESK_OPERATION_KEY；程序提交后查询真实 Task 状态，不自动重试写操作。

路由和契约修改必须同步更新 `scripts/generate-openapi.py`，重新生成 `internal/app/openapi.json`；运行 Go 应用/API/凭据/幂等回归。后端变化需验证既有 API 行为与取消/恢复/副作用边界。当前验证结果见 [VALIDATION](VALIDATION.md)。

## Codex MCP 服务级审批

0.47.1 的 `/meta` 声明 `mcp-server-approval-policy`。保存 MCP 配置时支持 `default_tools_approval_mode`，值为 `auto`、`prompt`、`writes`、`approve`；`tools.<name>.approval_mode` 优先于服务默认值。`approve` 用于管理员明确选定的持续授权，工具禁用列表仍生效。该字段是 Codex 配置；Kun 明确拒绝它，继续使用单工具审批。配置仍按实例及项目环境保存，凭据脱敏和配置版本校验不变。

新闻 Docker 完整访问使用已有 `PATCH /instances/{iid}`，包含当前 `revision` 与 `permissions={sandbox: danger-full-access, approvalPolicy: never, reviewer: user}`。客户端先核对 Codex Docker 身份；普通研究启动不修改权限。

原生字段依据：[Codex MCP 配置](https://learn.chatgpt.com/docs/extend/mcp)。

## 初始化镜像（0.48.0）

以下接口仅管理员可用，应用凭据不能调用：

- `GET /setup/runtime-profiles`：返回内置配置目录、当前 Linux 架构和支持状态；不联网或安装。
- `POST /setup/runtime-images`：要求 `Idempotency-Key`；请求 `{profileId, codexVersion, source, reference}`。source 为 build/pull/local；Codex 为精确版本。立即返回 queued 的 ImageBuild，不等待构建/拉取。
- 使用 `/instances/default/builds` 及 `/{bid}/log`、`/{bid}/cancel`、删除接口管理 `scope=setup` 的任务。相同镜像、来源和配方的在途请求复用任务；不同在途配方占用同一标签返回 409。

`succeeded` 且 `offlineReady=true` 表示离线工具检查完成，不表示模型、账号、公网搜索或业务研究通过。平台/配方/Codex 版本不匹配会失败；已有应用不自动绑定或更新。当前模板版本为 1.1.0。

`PUT /setup` 新增 `applicationsOnly`，允许仅使用 Docker 应用时完成初始化，而不要求本机 Codex/Kun 可执行程序。它不改变通用助手运行后端。内置模板随二进制发布，管理员主机无需 Python/npm。
