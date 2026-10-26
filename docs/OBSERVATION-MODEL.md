# 统一观测模型 v1

这次在 RunDesk v0.52.0 的资源与 UHP 重构基础上，增加共同的观测读取模型。Codex App Server、Kun SDK、observeproxy 和宿主任务事件进入同一份查询契约。后续轨迹展示、调试和诊断可以复用该模型。

原始证据继续保存在现有 `state.db` 的 `events` 表。模型是只读投影，既有历史记录可以直接查询，不迁移或复制 Agent 原生目录、检查点和模型正文。旧的轨迹、事件、HTTP 详情接口继续可用；本轮保持工作台布局与展示方式。

## 两种视图

| 查询 | 内容 | 顺序与分页 |
| --- | --- | --- |
| `observations` | 每条保留事件的统一元数据，包括未知原生方法、增量、用量信号和采集状态 | 宿主事件 ID 升序；`after`、`limit` |
| `activity` | 已有生命周期投影中的运行、执行步骤，以及独立的 HTTP 请求或 WebSocket 连接活动 | 首个证据事件 ID；`offset`、`limit` |

两者默认 50、最多 200 条，返回 `schemaVersion`、`sessionId` 和 `snapshotThrough`。首次省略 `through` 时，服务固定当前事件上界；后续分页和证据读取必须保留同一个值，包括 **0**。`observations` 使用 `nextAfter` 继续，`activity` 使用 `nextOffset` 继续，`hasMore` 表示还有记录。

```text
GET /api/v1/sessions/{sid}/debug/query?kind=observations&limit=50
GET /api/v1/sessions/{sid}/debug/query?kind=observations&through=500&after=100&source=kun_sdk
GET /api/v1/sessions/{sid}/debug/query?kind=activity&through=500&runId=RUN_ID&limit=50
GET /api/v1/sessions/{sid}/debug/query?kind=activity&through=500&source=observeproxy
```

`source` 选择 `host`、`codex_app_server`、`kun_sdk` 或 `observeproxy`；`recordKind` 选择规范化类别。未知的原生方法保留为通用事件，不因为尚未编写语义解析器而消失。参数仍使用严格白名单，拒绝不适用的字段。

外层 DebugResult 的 `source=unified_observation` 表示查询模型；单条记录的 `source` 表示证据来源，两者含义不同。外部 MCP 提供 `debug_observations`、`debug_activity`；独立诊断会话提供 `trace_list_observations`、`trace_list_activity`，使用同一只读实现。应用归属、实例、项目和成员授权继续由原有调试路由校验。读取不会启动 Agent、调用模型或执行任务控制。

## 身份、时间和关联

共同模型保留来源、类别、状态、会话、可核验的任务与轮次、原生编号、时间及其时钟、证据引用和完整性。原生身份与宿主身份分开：

| 信息 | 规则 |
| --- | --- |
| 宿主 `eventId` / `through` | 日志身份与固定历史范围；用于引用现有证据。 |
| `taskId` / `runId` | 从固定范围内的 `run/input` 和原生明确关联重建；不使用会话当前任务覆盖历史。 |
| Codex thread / turn / item | 保留原生身份与关联依据；迟到的已知 turn 仍归原轮次。 |
| Kun sequence / revision / step | 分别是原生事件游标、状态版本和模型步骤；不与宿主事件编号互换。 |
| observeproxy exchange / capture | 一次保留的传输或连接证据；不是自动推导出的模型调用编号。 |
| 时间 | 宿主记录、Kun worker 事件和代理传输时钟分别保留；无法恢复的历史接收时间不补写。 |

生命周期步骤复用 `tracequery` 已有配对逻辑，不建立第二套相互竞争的步骤状态机。Kun 模型配对区分相同 step 下的 purpose；工具、MCP 和审批继续保留原生边界、回放标记、缺失和歧义。

同一个 run 中的一次模型步骤可能对应多个传输尝试。只有共同 runId、先后顺序或相近时间，不能证明 SDK 事件和代理请求是同一次调用。第一版保留两种独立活动和来源引用，不将其合并成一条所谓完整调用。今后有可靠的共享请求编号时，可以增加明确关联。

WebSocket 可能跨多个轮次，其 `connectionStartLabels` 仅描述连接建立时的上下文，不作为整条连接或所有消息的 run 归属。查询结果不宣称覆盖未结束连接、绕过代理的通信或未留存的历史。

## 证据与读取边界

事件列表在 SQL 中只提取白名单元数据，不加载或返回模型请求、工具输出和流式正文。活动视图复用有界生命周期投影，返回类别标题与证据引用。证据引用指回原来的宿主事件；普通事件使用 `event` / `trace_read_event`，传输使用 `llm_request` / `trace_read_llm_request`。原始模型正文继续以字节分页读取，不把预览当作完整内容。

采集丢失、截断、缺少开始或结束、配对歧义以及预览限制都必须显式标记。没有某条记录表示证据未知；工具成功也不证明任务目标达成。已保存正文可能含业务数据，仍受现有调试权限控制。

生命周期投影保留 20,000 条、64 MiB 总载荷和单条 8 MiB 限制；超限失败，不返回部分统计冒充全量。所有统一查询限时、响应有界，取消请求停止读取。旧接口的分页和大小限制保持各自既有契约。

## 用量和后续扩展

观测记录为后续计量保留来源与证据。本版不把 Codex 累计 thread 用量、Kun 逐调用用量和代理响应中的同一份用量直接相加，不产生新的结算账单。缺失用量不能解释为零费用。后续用量账本需要明确累计基线、逐请求身份、权威来源、去重和价格版本。

本轮没有改变 Kun 的连接配置，也没有新增集中 API key/上游资源管理。相关配置身份、检查点和分叉约束见 [模型传输边界](MODEL-TRANSPORT-BOUNDARIES.md)。统一模型的覆盖范围是已经进入日志的证据；它不等同于所有 Agent 的通信都已接入代理。

实现入口：`internal/observation` 定义数据契约，`internal/tracequery` 提供三路投影，`internal/app/debug_service.go` 与两类 MCP 复用查询。

## 会话阅读投影

`GET /api/v1/sessions/{sid}/debug/query?kind=transcript&through=500&runId=RUN_ID&offset=0&limit=40`

`transcript` 在同一统一活动模型上增加可读内容。返回 `TranscriptPage`，其 `entries` 包含稳定 `id`、完整 `activity` 元数据、`role`、`title` 和 `blocks`。块区分文本、代码、JSON、diff 与文件引用；原始正文仍留在现有事件日志，没有第二份持久化副本。外部 MCP 同样提供 `debug_transcript`。轮次导航继续使用 `runs`。

- 默认 20、最多 50 条；分页与详情读取保留同一个 `through`，包括 0。只接受 `through/runId/offset/limit`。
- 单条最多 12,000 个 Unicode 字符和 32 个块；标题上限 256 字符，路径上限 1,024 字符。每页额外读取原始内容最多 16 MiB、单个事件最多 8 MiB，响应最多 4 MiB。原有生命周期总量限制仍适用。
- `contentTruncated` 表示阅读内容达到显示/读取预算；`contentUnavailable` 表示预期文本不可用或原始读取预算不足。正常的空工具输出、仅工具调用的模型回复、无正文的上下文状态不标记为缺失。`activity.evidence` 指向按需读取的来源。
- Codex 工具调用与结果沿原有生命周期身份配对，保留开始事件中提供而结束事件省略的参数。未结束消息只按明确 `turnId/itemId` 读取保留增量；歧义不合并。`contentPartial` 表示尚未观察到结束，不能当作完成证据。
- 用户正文以宿主输入为主；仅按完整文本匹配折叠一条此前宿主输入对应的一条纯文本原生回显。混合图像/文件内容、额外重复消息保留。此规则只影响显示，不修改原始事件或统一活动。
- Kun 的 plan/act、工具结果、快速回复和补充指令分别呈现；已有回放/复用标记保持原义。只显示接口已保留的可见推理，不补造模型内部过程。
- observeproxy 仍是独立的传输活动，只在阅读投影返回元数据，不加载 HTTP/WebSocket 正文，也不按相近时间绑定到模型步骤。

结构字段和嵌套 JSON 格式工具参数在显示前脱敏；自由文本不是凭据扫描结果。文件块只保留资源引用，不复制或自动读取目标文件。
