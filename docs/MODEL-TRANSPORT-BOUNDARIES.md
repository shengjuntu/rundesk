# 模型通信与统一观测的边界

本轮把 Codex App Server、Kun SDK 和已保留的 `observeproxy` 记录纳入共同的观测模型。统一记录不表示三种来源具有相同的采集范围：Kun 保留现有 SDK 事件，Codex 的 HTTP 采集继续按会话设置生效。本轮没有把 Kun 的模型地址改写到代理，也没有新增集中 API key 管理或结算服务。

## Kun 证据身份

| 字段 | 含义与使用边界 |
| --- | --- |
| 宿主 `events.id` | RunDesk 日志中的事件身份和排序依据；查询的 `through` 使用这一编号。 |
| SDK `sequence` | Kun 会话内持久日志游标，不能替代宿主事件编号。`ImportKun` 原子提交事件、业务投影和游标；重复编号不重新入库，缺号报错。 |
| SDK `sessionId` / `runId` | 原生事件所属会话和执行轮次；读取历史时不能用会话当前轮次覆盖。 |
| SDK `revision` | 原生状态版本，用于控制并发与检查点校验；不是事件身份。 |
| SDK `type` / `data` | 原生事件名称和负载。`data` 是开放 JSON，未知事件仍需可引用和读取。 |
| SDK `time` | 原生记录时间；时间相近不证明与另一通道的请求一一对应。 |

`kun/model.started` / `kun/model.completed` 可以包含 `step`、`purpose`、请求、消息和用量。显式规划 (`purpose=plan`) 与执行请求应分别保留；只有 `step` 相同不足以证明是同一次调用。工具的录制回放、已完成结果复用和假设输出也必须保留原生标记，不能展示为新执行的工具副作用。

SDK 事件入宿主日志前沿用结构化脱敏。统一模型引用这一份既有证据，不再复制完整原生目录、快照或模型正文。原生内部状态仍由 Kun 持有。

## 用量与关联

- Kun `model.completed.data.usage` 是每次调用报告的用量，已知字段有 `total_tokens`、`prompt_tokens` 和 `completion_tokens`；缺失、非法值和未报告必须保留为未知。
- Kun `BudgetUsage.reportedTokens` 是累计预算信息，不应与逐次调用的用量再次相加。`unreportedModelCalls` 表示统计覆盖缺口。
- Codex `thread/tokenUsage/updated` 包含累计快照；计量需要已知基线和增量计算，不能把每条快照直接求和。
- SDK 和 HTTP 响应可能同时报告同一请求的用量。只有可靠请求身份才能做跨来源去重；会话、轮次和相近时间只能表明上下文关联。统一观测记录本身不是可结算账单。
- 已有 WebSocket 观测可能跨轮次。`connectionStartLabels` 只描述连接建立时的上下文，不能赋予连接内所有消息同一个轮次。

## 为什么不直接替换 Kun Endpoint

Kun 的 `Config.Endpoint` 属于原生配置。检查点使用配置指纹；宿主的分叉启动也比较当前配置与冻结执行包中的完整配置。Fast 路径保存配置守卫。把这一地址替换成随机端口、临时 capability URL 会改变配置身份，进而影响恢复、分叉和守卫校验。

只在恢复时跳过代理不能解决该问题：来源运行可能已经冻结了临时地址。修改冻结执行包、重算原生指纹或忽略配置校验，也会破坏现有执行契约。

## 下一步的传输配置契约

RunDesk 的连接资源可分成稳定身份与凭据：稳定的 `connectionId`、上游 URL、协议和配置版本；独立的 `credentialRef`；每次执行的 `routeLease`、`captureId` 和证据引用。API key 不进入公开配置、诊断、事件或 URL。

接入现有 Kun 时，应先验证一种可跨进程重启重建的稳定模型端点：地址和路径不能每轮随机变化；观测开关只决定是否保存副本，不能改变原生配置身份。原生配置冻结的是这一稳定端点；实际上游和凭据由 RunDesk 的连接资源解析。路由凭据及其轮换规则必须与原生配置指纹分离。上游身份变化仍要触发明确配置版本变更，不能利用稳定代理地址绕过恢复校验。

稳定地址并不自动解决运行归属。每个任务需要独立的执行授权或可核验的请求标识；不能凭当前会话轮次给迟到请求改写归属。连接资源停用、任务结束和服务重启后的授权恢复需要确定行为，并且不能发生静默直连回退。

若原生协议以后提供独立于 `Config` 的传输覆盖，也可以由适配层使用该能力；这需要明确的能力检查，不能假设当前 SDK 已支持。上述方案都应先用真实外部 Kun 验证普通执行、取消、检查点恢复、跨宿主重启、Hybrid/Live 分叉和 Fast 守卫，再开启其 HTTP 代理能力。

当前代码依据：`third_party/kun-sdk/protocol/{protocol,checkpoint,modules,fast}.go`、`internal/store/kun.go`、`internal/app/{kun_checkpoint,kun_fork,kun_case_runs,kun_compare,usage}.go`。
