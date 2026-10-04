# 任务队列

在「任务」页新建任务，选择通用助手或应用、项目、模型和可选最早执行时间。任务各有独立会话，可打开会话查看回复、审批和轨迹。任务输入中 API 还可携带已有项目文件路径和技能引用，执行时重新验证。

POST /api/v1/tasks 直接接受 TaskSpec，需 Idempotency-Key，返回 202 与 task.id、sessionId。GET /tasks 返回 items 与 nextCursor，支持 status、appId、instanceId、limit（1–500）、cursor。GET /tasks/{id} 查询；POST /tasks/{id}/cancel 取消。source.taskId 只是业务标签，不能替代幂等键。

GET/PUT /queue 管理 paused、maxConcurrent（1–16，默认 4）、perInstance 和 revision。普通会话、恢复任务、队列任务共用额度；等待审批也占用额度。降低限制不会终止已有任务。暂停只暂停派发；仍可提交任务。每个实例受阻时，不阻塞其他实例。最多接受 1000 个待处理任务，原生连接总上限仍为 16。

任务与会话事务保存。执行前先持久化 runId。重启后 queued 任务仍可运行；已经接收的原生执行标为 interrupted；派发确认缺失则为 unconfirmed，必须检查轨迹，不自动重跑。失败任务通过原会话「核对并继续」恢复；原队列结果保留。取消排队任务不会启动 Codex；运行中取消可能与完成竞争，以最终状态为准。已排队会话不能绕过队列发送、删除或归档。

本地原生进程执行，不需要 Docker 或容器调度。SQLite 与单服务数据目录锁保证单进程调度；不支持多节点分布式队列。模型名称在提交时确定；运行时读取当时的技能、MCP、笔记和权限。
