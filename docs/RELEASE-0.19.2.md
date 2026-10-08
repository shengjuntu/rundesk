# RunDesk 0.19.2 — Queued task authorization / 排队任务授权

New application API and inbound A2A tasks persist the submitting credential ID. The queue checks authorization before dispatch; the final admission check is serialized with revocation. No secret token is stored on the task. Expired, revoked, missing or insufficient credentials prevent a queued task from starting and leave a visible failure reason.

新应用 API 与入站 A2A 任务持久化记录提交凭据 ID。队列派发前检查授权，最终接收检查与撤销操作互斥；不在任务上保存秘密 Token。凭据过期、撤销、不存在或权限不足时，排队任务不再启动并留下失败原因。

Schedules also persist this binding. Due occurrences check authorization; invalid credentials disable the schedule and advance its revision, with a reason. Every occurrence inherits the binding and is checked again when starting. Saving a schedule using a new valid application credential authorizes future occurrences only. Administrator edits preserve an existing binding. A transient storage error prevents execution and does not permanently disable the schedule; a queued task encountering an authorization storage error fails rather than executing without verification.

定时规则同样保存绑定。到期时授权失效会停用规则、更新修订号并记录原因；产生的每个任务继承绑定，启动前再次检查。应用使用新有效凭据保存规则只重新授权后续任务，已排队任务不自动换绑。管理员编辑保留已有绑定。临时存储错误阻止执行但不永久停用定时规则；排队任务遇到授权存储错误则失败，不跳过检查。

## Upgrade behavior / 升级行为

- Running/admitted tasks are not automatically killed. Revocation is not rollback; cancel them explicitly if necessary.
- Legacy tasks/schedules without a recorded key and trusted internal/admin submissions retain prior semantics. Existing application schedules gain the binding when saved using an application key.
- Accepted operations remain in the audit trail. Re-authorization does not automatically retry failed work or transfer old idempotency receipts.
- This covers application credentials and their queue admission, not a new user/tenant authorization model. Separate execution environments remain necessary for untrusted filesystem access.
- 已接收进入运行的任务不自动终止；撤销不等于回滚，需要时显式取消。
- 没有凭据记录的旧任务／规则和可信内部／管理员提交保留原行为；旧应用规则用应用凭据重新保存后取得绑定。
- 历史记录保留；重新授权不自动重试失败任务，也不迁移旧幂等回执。
- 本版覆盖应用凭据与队列接收检查，不新增用户／租户模型。对不可信文件系统访问仍需独立执行环境。

## Validation / 验证

Full Go suite; focused race tests for revoked/expired/deleted/insufficient credentials, schedule inheritance and replacement, JSON spoofing rejection, restart persistence, A2A provenance and unchanged running-task behavior. Credential-dialog browser smoke covers the updated revocation notice. Linux and Windows builds; Windows cross-compilation only. No real model or Docker-engine validation.

执行全量 Go 测试、授权竞态测试，覆盖撤销／过期／删除／权限不足、定时任务继承与换绑、伪造字段拒绝、重启持久化、A2A 来源及运行任务不误停。浏览器验证更新后的撤销说明。构建 Linux／Windows 程序；Windows 仅交叉编译。未验证真实模型或 Docker 引擎。
