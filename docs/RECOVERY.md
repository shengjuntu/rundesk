# 错误诊断、核对与继续

先确认错误来自页面、RunDesk、Codex 连接还是模型服务，再核对原任务与外部副作用，最后显式决定是否继续。查看诊断或核对恢复计划都不会自动重发任务。本文先说明错误证据，再说明 Codex 原生任务的恢复操作与契约。

<a id="error-diagnostics"></a>

## 定位错误与连接故障

<a id="diagnostics-1"></a>

### 如何查看 HTTP 500

1. 出错后点击提示中的「查看详情」，或点击主界面、轨迹页顶栏的「错误记录」。登录失败时也可从登录页进入。
2. 在列表选择记录，先看来源、实际 HTTP 状态、请求路径和请求编号，再看响应详情。展开「会话、时间和其他定位信息」查看关联会话、项目、应用配置、耗时、错误代码和重试标记。
3. 点击「复制诊断信息」。剪贴板不可用时显示可手动复制的文本。将 requestId 与后台 `api_error` 或 `api_panic` 日志对应即可进一步定位。
4. 运行失败消息上的「查看错误详情」查看该事件的错误内容、事件编号及提供的 run/turn 标识。

设置对话框内出错时，窗口中会出现「查看错误详情」，原表单继续保留。错误详情不会自动重发任何操作。消息发送返回 5xx 或网络断开时，输入保留；手动重试沿用原幂等 Key。

<a id="diagnostics-2"></a>

### 不同错误的区别

| 来源 | 界面提供的信息 |
| --- | --- |
| 页面 HTTP 错误 | 实际状态、请求方法与路径、响应类型、requestId、JSON/文本/HTML 响应内容 |
| 非 JSON 的 500 | 错误页作为纯文本显示，不运行脚本或插入 HTML |
| 空错误响应 | 明确显示服务端没有提供错误内容，不补造原因 |
| HTTP 200 但预期 JSON | 标为响应格式错误，保留实际的 200 状态 |
| 无响应网络失败 | 标为未取得 HTTP 状态，不猜测是 500 |
| Codex 运行事件 | 展示原生 error、turn/completed.error 或 run/state.error；其中提到的上游 500 不冒充页面 HTTP 状态 |
| SSE 中断 | 记录事件连接中断并保留浏览器自动重连；EventSource 不提供状态与正文，界面明确说明未知 |

这项功能增加可见性，不会自动诊断“你那一次”500 的根因。错误可能来自 RunDesk、代理、Codex 或模型服务，需要实际响应与日志支持。旧错误若既未保留在页面、也没有运行事件或服务日志，升级无法补回当时的响应。

<a id="diagnostics-3"></a>

### 保存与边界

- 当前来源、当前浏览器标签页的 sessionStorage 保留最近 50 组观察到的错误，刷新后可查看；不是服务器全部请求的集中日志。
- 同一路径、状态和消息在 60 秒内重复时归组，记录次数与最近 5 个 requestId。运行事件按事件编号去重，归组保留最近 200 个事件编号。
- 可清空浏览器诊断缓存；退出登录也会清空。原任务事件库不被删除，重新读取历史运行事件时仍可能显示对应错误。
- 错误响应最多读取前 32 KiB并显示截断说明；正常成功响应不按这个限制截断。模型/provider 未提供的信息不能补造。
- 请求正文、上传数据、Authorization/Cookie 等认证头不会进入诊断记录。已知密钥字段、Bearer 和常见密钥文本被遮盖；任意业务文本无法保证完全自动脱敏。
- 页面正常请求、技能目录/ZIP 导入和文本预览使用统一错误通道。浏览器直接下载链接及图片/音视频资源由浏览器处理，不能保证在此面板取得它们的原始错误响应。
- 不自动重试创建、提交、停止或审批。SSE 原有重连机制保留。

<a id="diagnostics-4"></a>

### 后端支持

新错误响应附带 details，包含可用的错误链、RPC code/data、时间、请求路径和耗时，保留既有错误契约。后台仅对 5xx 新错误输出带 requestId 的日志，不记录请求正文或查询参数。幂等重放保持原回执；界面同时区分当前响应头的请求编号与原响应正文编号。

API panic 在尚未发送响应时转换为 JSON 500，浏览器获得 requestId，堆栈只写入服务日志；已经发送内容的 SSE/文件流保持中断语义，不往流里追加 JSON。不提供任意服务器文件或日志读取接口。

<a id="diagnostics-5"></a>

### 连接与运行故障分类

v1 区分上游 RPC 502、连接/记录不可用 503 与超时 504，保留原始错误。`details.origin=codex_transport` 表示本地 App Server 连接故障；`transportOperation` 给出发生阶段。内部异常和技能自动恢复失败仍为 500。已有幂等回执保留原状态，未加入自动重发。另见 [后端能力与边界](BACKENDS.md)。

<a id="task-recovery"></a>

## 核对副作用后显式继续

本页描述 Codex 原生任务的显式恢复；Kun 另见 [安全检查点](KUN-CHECKPOINTS.md)。执行由原生 Codex 负责；RunDesk 核对状态、组织恢复说明、记录关联和展示过程。

<a id="recovery-1"></a>

### 界面操作

1. Codex 报告 `willRetry=true` 时，会话显示「Codex 正在重试」。等待原任务继续；不新建轮次。后续回复、工具步骤或最终结束事件会清除重试提示。
2. 当前任务进入 failed / interrupted 后，点击「核对并继续」。核对不会发送模型任务。
3. 查看原任务、原生线程状态、原错误、已观察到的工具步骤和会话产物。文件可点击预览，完整步骤可从「查看原任务过程」打开。
4. 有工具操作、已有产物、未知错误或人工中断时，需先核对实际结果并勾选确认。已识别的登录、额度、上下文等问题需要先处理；未知错误不会自动判断为可重试故障。
5. 可补充已完成和待完成的内容，然后点击「继续未完成任务」。主输入框草稿不受影响。

继续成功后仍是同一 Session；已有 Thread 会沿用，新增 runId / turnId。时间线保留原失败轮次，新增「恢复轮次」和「查看来源轮次」。如果失败发生在原生线程创建之前，继续时才首次创建线程。

恢复窗口收到提交响应不明的错误时，保留原请求内容，允许「重试同一请求」核对接收回执。相同 Key 和请求返回同一接收结果；不会因为浏览器丢失响应就再次创建轮次。最终完成状态仍以 Session / 事件为准。

<a id="recovery-2"></a>

### 核对规则

| 观察结果 | 行为 |
| --- | --- |
| 原生任务 active / inProgress | 阻止继续，等待或先停止原任务 |
| 对应原生轮次 completed | 展示已记录的回复和产物，不重复执行 |
| 已提交但没有可靠 turnId、原生最后一轮不匹配、未知状态 | 阻止继续，要求先核对原生历史 |
| thread/read 不支持、读取失败、旧进程正在退出 | 不发起任务；处理原因后重新核对 |
| 原生 failed / interrupted，状态可对应 | 按错误类别和已有操作要求核对后继续 |
| 初始化阶段失败，尚未发起 thread/start 或 turn/start | 保留原目标、附件和技能，允许核对后再次尝试 |
| 原生策略或审批拒绝类别 | 恢复入口不绕过限制 |

核对结果 15 分钟内有效，每个会话只保留最近一次。删除会话会删除该核对结果；旧请求回执仍按原规则保留。再次核对会使前一次 planId 失效。

提交前，后台在会话操作锁内重新检查来源 runId、Session 更新时间、Thread ID、实例 revision 和原生最后一轮状态。旧 plan、并发启动、会话变化或配置 revision 变化返回冲突，不提交新任务。上一轮的迟到事件不能结束新启动的恢复轮次。

原生进程仍存活时，通过该会话连接执行 `thread/read(includeTurns=true)`；旧进程退出后才使用相同实例配置读取持久化历史。继续时必要的重连使用 `thread/resume`，随后 `turn/start`。无法确认的执行不会凭本地 failed 状态直接重发。

<a id="recovery-3"></a>

### 应用 API

先核对，HTTP 200 返回 RecoveryPlan；即使不能继续，也返回原因和已能读取的证据。

```http
POST /api/v1/sessions/SESSION_ID/recovery/check
Content-Type: application/json

{"expectedRunId":"FAILED_RUN_ID"}
```

应用按 `canContinue`、`requiresReview`、`requiresFix` 决定展示与业务核对。读取自己的业务记录确认副作用后，再提交：

```http
POST /api/v1/sessions/SESSION_ID/recover
Content-Type: application/json
Idempotency-Key: recover-case-42-attempt-2

{"planId":"PLAN_ID","expectedRunId":"FAILED_RUN_ID","reviewedEffects":true,"issueResolved":false,"note":"报告草稿已保存；发布尚未执行。"}
```

`reviewedEffects` 和 `issueResolved` 是调用方的显式声明，不能从「按钮被点击」或「发生了 500」直接推导。服务端使用已保存的原任务和核对数据，不接受客户端替换恢复上下文。补充说明最多 4000 UTF-8 字节。

202 Session 包含 `recovery={planId,sourceRunId,sourceTurnId?,rootRunId,checkedAt}`。连续失败后再次恢复仍保留原始任务的 rootRunId、附件和显式技能。普通新消息不会标记为恢复轮次。

每次新的恢复动作使用**新的逻辑操作 Key**；同一次恢复请求的网络重试必须复用**原 Key 和完全相同的请求内容**。不要重用失败原任务的 Key，也不要在回执不明确时盲目换 Key。可以通过 `GET /api/v1/requests/{key}` 查询接收状态。

主要 409 代码：`run_conflict`、`session_busy`、`recovery_not_available`、`recovery_input_missing`、`recovery_plan_missing`、`recovery_plan_expired`、`recovery_review_required`、`recovery_fix_required`、`recovery_stale`。核对到不允许继续的状态通过 plan 的 `category/reason` 说明；不能把 HTTP 200 视为可执行。

`GET /meta` 包含 `task-recovery` 和 `native-retry-status`。Session 可选字段 `retry={message,time}` 表示已收到原生重试通知；`run/retry` 事件可触发状态刷新。HTTP 错误的 `retryable` 与原生 `willRetry` 含义不同。

Python 客户端提供 `check_recovery()` 和 `recover()`，不自动确认外部操作或重试写入。已有 news2douyin / Video App 调用方式保持兼容；应用要展示本流程时再接入两个新接口。

<a id="recovery-4"></a>

### 实际边界

- 本版提供人工 / 应用显式确认后的继续，没有增加自动重启任务、定时退避或无人值守恢复调度器。Codex 自己的重试仅被观察和展示。
- 恢复是同一上下文中的新轮次，不能回到原进程的精确内存位置。恢复提示要求 Codex 先检查已有文件与应用记录，只做剩余部分。
- RunDesk 不知道外部 MCP 的业务事务是否成功。工具 completed、文件存在和模型声称完成都不是业务成功证明；HTTP 接收去重不等于跨应用写操作 exactly-once。应用仍需提供结果查询和业务幂等能力。
- 预览最多整理 5000 条生命周期事件、60 个工具步骤和 50 个会话文件路径。截断有提示；文件和步骤可继续从原过程核对。原生回复最多展示 6000 字符，恢复说明内原目标最多 12000 字符；原附件仍保留。
- 恢复沿用会话模型和当前应用权限、技能与 MCP 配置。实例 revision 改动会使核对失效；原生配置文件、项目技能和外部服务可能独立变化，不是配置快照。审批仍由原流程处理。
- 本地会话锁只协调这份 RunDesk。其他程序直接操作同一原生线程时，核对与实际提交之间仍可能发生变化；不要把两次读取理解为跨进程事务锁。
- 已经 completed、状态不确定或历史缺失时，先查看产物、原生历史和应用业务记录，确认未完成内容后再决定后续任务，不通过改数据库状态强行恢复。

本次以本地 JSONL 模拟器和浏览器故障注入验证；没有复现真实 Provider 的中途故障。原生协议依照已核对的 Codex App Server `thread/read` / `thread/resume` / `turn/start` 和 error `willRetry` 结构；旧版本不支持核对方法时会明确阻止继续。
