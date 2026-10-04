# 定时任务

进入「任务」页下方的「定时任务」，选择执行助手或应用、项目与任务内容；设置五段 Cron、IANA 时区，预览未来五次。示例 `0 9 * * *` + `Asia/Shanghai` 为每天北京时间 09:00。后台需持续运行。

只接受五段（分、时、日、月、周），拒绝秒字段、@every 和表达式内时区前缀；时区独立填写，不接受 Local。解析使用固定依赖 robfig/cron v3.0.1，Go 程序嵌入时区数据，无需容器。日与周同时有限制时为 OR；春季夏令时不存在的本地时间跳过，秋季重复时间可能执行两次。界面预览会标出时区。

POST /api/v1/schedules 接受 ScheduleSpec，需 Idempotency-Key。GET /schedules 列出规则。GET/PUT/DELETE /schedules/{id} 管理单项；PUT 接受 {spec,revision}，DELETE 接受 {revision}。POST /schedules/preview 接受 {cron,timezone}，返回 times、timezone，不需要幂等键。

- misfire=skip：调度检查比计划时间晚一分钟或以上时跳过；once：最多补交一项，再前进到未来。
- overlap=skip：此前任务仍排队、执行或待核对时跳过；queue：继续提交独立任务，仍受队列并发约束。
- 队列满时跳过并记录理由，不自动追补。
- 重新启用从未来开始，不补停用期间的任务。改 Cron/时区重新计算下一次时间。
- 停用或删除规则只影响未来；已经提交的任务仍在队列中，可单独取消。
- 暂停队列不暂停规则；需要停止入队时停用规则。

每次执行有确定 ID；下一执行时间、任务和会话在同一 SQLite 事务中保存，重启不会重复生成同一时间点任务。执行阶段遵循队列的待核对机制，不承诺外部副作用的 exactly-once。任务名称、模型在保存规则时确定；技能、MCP、笔记、权限在实际执行时读取。模板不接受 notBefore。最多 500 条规则。
