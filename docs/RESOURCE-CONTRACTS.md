# v0.52.0 基础上的资源与执行模块重构

RunDesk 负责 Task 的准入、队列、授权、状态和证据；Codex/Kun 后端分别负责驱动自己的原生运行时。资源由平台登记与解析，原始文件、工作区和引擎状态保留在原来的位置。此次不改变外部 Kun 协议，不引入 HarnessRouter 依赖，也没有声明 UHP 兼容。

## 模块契约

| 契约 | 职责 | 实际入口 |
| --- | --- | --- |
| `TaskControl` | 提交、查询、列出、取消任务 | RDCA task 路由 |
| `QueueControl` | 并发额度与排队管理 | 管理员 queue 路由 |
| `TaskResourceControl` | 读取资源视图、显式生成目录视图 | task resources 路由 |
| `RuntimeBackend` | 后端准入检查、执行、取消、能力描述 | Codex/Kun 独立模块 |
| `resource.Catalog` | 通过稳定身份及版本查询描述 | 会话资源集合 |
| `resource.Bindings` | 角色到资源引用及访问方式的绑定 | workspace、outputs、runtime-state 等 |
| `resource.Resolver` | 已授权范围内将引用解析为宿主位置 | 后端工作区、输出目录、Kun 状态目录 |
| `PolicyService` | 读取平台权限声明及其版本 | 后端原生策略转换 |
| `PersonalArtifacts` | 文件目录、引用附加、显式快照、删除 | 个人文件库 |

后端不接收整个 `Manager`。`runtime_host.go` 是唯一集中的宿主组装层，注入生命周期、资源、策略、原生配置和事件服务。Kun 的游标与宿主事件投影继续原子提交；后端不能任意写数据库。Codex 与 Kun 保留各自的线程/worker、续接、审批、停止及调试流程，不通过一个通用算法重新执行工具循环。

当前这些契约属于内部模块；连接句柄和已有扩展 DTO 尚未变成第三方插件 SDK。配置页面与原生辅助操作保留原有接口，避免以文件移动冒充所有配置格式都已统一。

## 身份、来源与版本

`Ref` 包含 `kind/id/version`。`Descriptor` 明确资源所有者、来源、存储方式与版本含义；`Binding` 明确它在当前会话中的角色和访问方式。资源描述不携带凭据、MCP 命令/环境变量值或主机绝对路径；只有宿主 Resolver 可取得路径。

| 字段 | 含义 |
| --- | --- |
| `storage=reference` | 仅引用原资源；不意味着备份 |
| `storage=managed` | 平台安排存储位置；实际原生状态仍由引擎维护 |
| `storage=snapshot` | 明确保留了独立内容版本 |
| `versionKind=binding` | 配置或绑定修订号；不是目录内容哈希 |
| `versionKind=content` | 固定内容身份，例如个人产物 SHA-256 |
| `versionKind=live` | 来源可以变化；没有固定历史内容 |

当前配置清单包括工作区、会话输出、原生状态、项目/实例 Skill 根目录、MCP、plugin 与权限来源。Skill 目录声明不等于每个 Skill 已加载；Codex MCP/plugin 继续由原生配置管理；Kun MCP 投影其配置修订与服务名称，Kun 原生 plugin 明确为 unsupported。后端的真实能力决定如何配置与启用资源。

Resolver 验证作用域、绑定、版本及路径。作用域仅表达平台对象关联，不代替用户认证。HTTP 入口仍校验应用、实例、项目和 scope；执行仍受原生沙箱、审批及操作系统环境约束。

## Task、Run 与原生会话

排队 Task 关联 Session 与一次 Run；Session 后续可以继续。资源检查明确当前配置的观察时间，不能把今天的配置当作历史执行配置。`run/input` 保留 `resources` 和 `resourceBasis=declared_at_admission` 元数据；原生回报的生效配置仍是独立证据。此记录不会冻结 Skill 内容、复制原生数据库或保证引擎加载了所有声明。

原生 subagent 属于运行时的线程/代理关系。平台 child Task 属于可独立排队和取消的任务关系；本次没有把原生子代理自动提升为平台 Task，也没有强制业务采用固定的父子任务工作流。

任务目录通过显式导出生成，只有一个可重建的 `resources.json`，不作为另一份可写任务数据库。实时状态以 API/SQLite 为准，原生状态以原生引擎为准。详见 [任务资源视图](TASK-RESOURCES.md)。

## 文件兼容与迁移

无需迁移已有 SQLite schema 或移动原工作区。旧个人文件记录缺少 storage 时按 managed 读取；新工作区上传/产物只记录引用与内容哈希。直接上传到个人库本身仍保存唯一托管原件。跨工作区附加可以显式物化，原件不会因此迁移。

引用文件改变或移除后，旧引用明确返回错误。需要独立保留当前版本时调用 snapshot 或使用工作台“保存快照”。删除新引用不删除原始工作区文件；删除快照也不会删除它的原始来源。HTTP 下载为确保返回的是已验证版本，可以使用请求期间的临时缓冲，响应结束即清理，不成为第二份持久产物。

已有库副本与新引用可混用。新引用语义应由本版本读取；回退到未修改的 v0.52.0 无法读取新增 reference 条目，因此升级前应按原有运维流程保存数据备份。此次不在用户服务上部署，也不修改用户运行中的数据目录。
