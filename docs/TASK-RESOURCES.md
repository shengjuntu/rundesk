# Task 控制与资源视图

RunDesk 的任务状态继续由平台 SQLite 对象及事件管理。资源视图把 Task、Run、原生会话身份和资源引用放在一起；不会复制工作区、Codex home、Kun 执行数据库或产物正文。

## 接口边界

| 契约 | 职责 | 使用位置 |
| --- | --- | --- |
| `TaskControl` | 入队、查询、列举、取消平台任务 | 任务 HTTP 路由与应用归属检查 |
| `QueueControl` | 队列暂停和并发配置 | 管理员队列路由 |
| `TaskResourceControl` | 查询当前资源引用、显式导出目录视图 | 任务资源路由 |

这些接口不暴露原生客户端或整个 `Manager`。当前平台实现仍由 `Manager` 组合现有队列和会话服务；HTTP 的 `Server.Tasks` 可独立注入任务控制实现。配置、资源检查、运行时调试各自保留接口，不要求提交任务的调用方实现全部管理能力。

这里的 Task 是现有持久队列任务。兼容的直接 Session/Run 接口继续存在；Codex 原生子 agent 不会因为出现一条原生线程而自动变成平台排队 Task。

## 在线查询

`GET /api/v1/tasks/{taskId}/resources` 每次从当前平台记录和资源绑定构造视图，返回：

- `schema`：视图格式版本，目前为 `1`。
- `observedAt`：本次观察时间。
- `stateSource: "platform_sqlite"`：平台任务状态的权威来源。
- `availability`：能否构造资源绑定视图；`available` 不代表已检查所有原始文件是否仍存在。
- `configurationBasis: "current_bindings"`：资源配置是当前绑定，不是声称历史执行使用过这些配置的快照。
- `task`：任务、会话、原始 Run 编号、状态、取消标记和生命周期时间。
- `runtime`：后端、执行环境及可确认的原生 thread/turn 关联。
- `resources`：不带宿主绝对路径和凭据的资源描述与绑定。

应用凭据需要 `read` 权限，且必须拥有该任务的应用、实例和项目。返回资源引用不等于获得原始文件内容的访问权；文件读取仍经过既有 `files` 权限和归属检查。旧 `/api` 路径沿用同样的授权和处理逻辑。

一个 Session 可以在任务结束后继续执行。因此 `runtime.runId` 始终是 Task 记录的 Run；只有 Session 当前 Run 与它一致时才填充 thread/turn，并标记 `nativeAssociation: "current_run"`。尚未执行为 `not_started`，历史原生关联无法确认时为 `unavailable`，不会把后续轮次误记为原任务。

删除终态任务关联的 Session 后，Task 记录仍可查询。此时保留已知任务状态和绑定归属编号，返回 `availability: "unavailable"`、`unavailableReason: "session_not_found"`、`configurationBasis: "unavailable"`；资源数组为空，并省略 `runtime`。查询和导出均不会重建已删除会话或推测原生身份。

输出资源指向原有 Session 输出目录。它是会话级资源，并不将后来写入同一目录的文件自动宣称为某个历史 Run 的产物。

## 目录视图

管理员可显式调用 `POST /api/v1/tasks/{taskId}/resources/export`，请求体为 `{}`，并按其他 RDCA 写操作携带 `Idempotency-Key`。返回 `taskId`、`observedAt` 和生成文件的路径。

生成位置为 `<data>/tasks/<taskId>/resources.json`。文件以临时文件加原子替换方式刷新，包含与在线查询相同的引用视图。应用凭据和普通成员不能使用此宿主文件系统操作。

这是可重建的观察快照，不是真正的 Linux `/proc` 挂载，也不是持续同步的第二份状态。修改或删除它不改变任务；再次导出会从平台记录重建。需要最新状态时用 GET；需要刷新磁盘视图时提交新的导出操作。相同 `Idempotency-Key` 的重试回放原操作回执，不创建新观察时间。

默认查询不创建目录；导出只写这一份小型 JSON 元数据，不复制产物。已有原生数据目录和个人文件库的引用／快照保留规则分别由对应资源服务管理。
