# 失败任务的核对与继续

RunDesk 0.8.4 增加显式的任务恢复流程。执行仍由原生 Codex 负责；RunDesk 核对状态、组织恢复说明、记录关联和展示过程。

## 界面操作

1. Codex 报告 `willRetry=true` 时，会话显示「Codex 正在重试」。等待原任务继续；不新建轮次。后续回复、工具步骤或最终结束事件会清除重试提示。
2. 当前任务进入 failed / interrupted 后，点击「核对并继续」。核对不会发送模型任务。
3. 查看原任务、原生线程状态、原错误、已观察到的工具步骤和会话产物。文件可点击预览，完整步骤可从「查看原任务过程」打开。
4. 有工具操作、已有产物、未知错误或人工中断时，需先核对实际结果并勾选确认。已识别的登录、额度、上下文等问题需要先处理；未知错误不会自动判断为可重试故障。
5. 可补充已完成和待完成的内容，然后点击「继续未完成任务」。主输入框草稿不受影响。

继续成功后仍是同一 Session；已有 Thread 会沿用，新增 runId / turnId。时间线保留原失败轮次，新增「恢复轮次」和「查看来源轮次」。如果失败发生在原生线程创建之前，继续时才首次创建线程。

恢复窗口收到提交响应不明的错误时，保留原请求内容，允许「重试同一请求」核对接收回执。相同 Key 和请求返回同一接收结果；不会因为浏览器丢失响应就再次创建轮次。最终完成状态仍以 Session / 事件为准。

## 核对规则

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

## 应用 API

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

## 实际边界

- 本版提供人工 / 应用显式确认后的继续，没有增加自动重启任务、定时退避或无人值守恢复调度器。Codex 自己的重试仅被观察和展示。
- 恢复是同一上下文中的新轮次，不能回到原进程的精确内存位置。恢复提示要求 Codex 先检查已有文件与应用记录，只做剩余部分。
- RunDesk 不知道外部 MCP 的业务事务是否成功。工具 completed、文件存在和模型声称完成都不是业务成功证明；HTTP 接收去重不等于跨应用写操作 exactly-once。应用仍需提供结果查询和业务幂等能力。
- 预览最多整理 5000 条生命周期事件、60 个工具步骤和 50 个会话文件路径。截断有提示；文件和步骤可继续从原过程核对。原生回复最多展示 6000 字符，恢复说明内原目标最多 12000 字符；原附件仍保留。
- 恢复沿用会话模型和当前应用权限、技能与 MCP 配置。实例 revision 改动会使核对失效；原生配置文件、项目技能和外部服务可能独立变化，不是配置快照。审批仍由原流程处理。
- 本地会话锁只协调这份 RunDesk。其他程序直接操作同一原生线程时，核对与实际提交之间仍可能发生变化；不要把两次读取理解为跨进程事务锁。
- 已经 completed、状态不确定或历史缺失时，先查看产物、原生历史和应用业务记录，确认未完成内容后再决定后续任务，不通过改数据库状态强行恢复。

本次以本地 JSONL 模拟器和浏览器故障注入验证；没有复现真实 Provider 的中途故障。原生协议依照已核对的 Codex App Server `thread/read` / `thread/resume` / `turn/start` 和 error `willRetry` 结构；旧版本不支持核对方法时会明确阻止继续。
