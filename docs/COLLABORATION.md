# RunDesk 0.14.0：协作预览版

本版落实：一个负责人、已有 Agent 配置、后台委派、结果回收；黑板使用 Gitea，远程任务使用 A2A。没有额外角色、岗位、公司组织层级。没有加入知识库/RAG。

## 使用

1. 启动 RunDesk，管理员登录后点击侧栏“协作”。
2. “连接与能力”：登记已有本地或 Docker 应用配置及工作项目；填写能力说明。也可以登记远程 A2A JSON-RPC 服务，并读取 Agent Card。
3. 若使用黑板，在 Gitea 中准备一个**私有、已有**仓库，填写 Gitea 地址、owner、repo 和访问令牌的环境变量名。例如在启动 RunDesk 的环境中设置 `RUNDESK_GITEA_TOKEN`。实际令牌不通过页面填写，配置中只保存变量名。首次创建和执行协作会向所选仓库写入目标和模型结果，请选择正确的仓库权限范围。
4. 新建协作，填写目标、负责人、允许参与的 Agent、交互方式和负责人轮次上限。
5. 详情显示各子任务、模型回复、共享记录和错误；本地任务可跳转原会话处理审批、检查轨迹。

三个交互模式：
- blackboard：负责人委派，Gitea Issue 共享讨论与结果。
- p2p：允许 Worker 请求其他 Worker，不同步 Gitea。
- hybrid：同时启用 Gitea 和 P2P。

负责人也是普通 Agent。配置在协作创建时快照，不受后续登记修改影响。每次执行创建独立 Session，通过持久化共享记录恢复上下文，不共享完整会话历史。原应用的模型、skills、MCP、Docker 和权限配置继续生效。

## 协调契约

此版使用最终回复中的结构化 JSON 交接，不解析自然语言猜测是否委派，也尚未注入动态协作 MCP 工具。负责人提示中会自动包含所选 Agent 能力、目标、黑板地址和最近共享记录。最终回复必须为：

```json
{"action":"delegate","summary":"并行核查两个方面","tasks":[{"agent":"worker-a","text":"自包含目标、输入引用、验收要求"}]}
```

或者：

```json
{"action":"finish","summary":"最终结果与引用","tasks":[]}
```

```json
{"action":"wait","summary":"需要用户补充什么","tasks":[]}
```

普通 Worker 可返回文本。在 p2p / hybrid 中，Worker 也可返回 delegate JSON；补查结束后系统向请求者创建后续任务，传入结果，再交回负责人。禁止委派自身或负责人，允许列表、每次最多8项、全程最多64项共同约束委派范围。负责人默认8轮，上限32轮。不会无限重试或无限创建团队。

负责人回复不符合契约时进入“需要你处理”，保留原回复。用户补充消息后，在剩余预算内自动再次唤醒；明确暂停的协作需要手动继续。当前运行全部子任务完成后再启动下一轮负责人，合并结果，避免每个细小状态变化触发模型。默认输入上下文最多最近80条记录并限制约96 KiB；完整记录仍在详情和Gitea中，不能宣称模型每轮读过全部历史。

暂停只停止新委派，已经提交的任务继续运行。取消会尝试取消所有已知子任务，远程取消失败不会伪报成功。无法确认远程提交状态时进入等待，不自动重发或重新规划；在远程服务核对 Task ID 后，通过界面绑定，再继续。若远程提交没有产生可查询 Task，需人工处理并新建协作，本版不提供“假定失败后重发”。

## 持久化与恢复

- 沿用 SQLite 存储；协作、子任务关联、队列任务和黑板同步标记落盘。
- 本地子任务使用确定 ID；队列 Task/Session 原子写入。恢复时即使关联尚未保存，也能找回原任务，不重复执行。
- 外部 A2A 不保证 messageId 去重，因此提交前保存 sending 状态。发送结果未知则等待人工核对。
- 子任务执行成功不代表证据可信，负责人需核验后返回 finish；目前没有独立人工验收状态机。
- 对发布、付款、评论等外部副作用继续遵守原 Agent 工具权限和用户授权。本版未实现平台级发布幂等，不能宣称全链路 exactly-once。

## Gitea 黑板范围

每次协作在所选仓库创建一个 Issue，事件和成果作为带唯一标记的评论写入。定期轮询评论（约15秒）补充共享上下文，运行中的负责人会收到新的内容。Gitea 评论作为不可信资料，不直接执行命令。等待/暂停状态不会因外部评论自动恢复。

写入失败可见并自动再次核对；初次建单结果不明时，先搜索确定标记，不盲目重建。可以绑定含本任务标记的已有 Issue。评论列表上限1000，超出会报错，不将不完整扫描视为成功。评论去重是标记核对机制，不是远端事务保证。

本版采用轮询，**未添加 Webhook、PR 审核、仓库文件自动提交或媒体上传**。报告内容、路径和引用在评论中共享；本地路径不会自动转换成远程可下载链接。远程 Agent 要读取文件，需要另外配置授权文件服务或共同存储。不要将本地路径当成跨机器文件传输。视频等大文件仍留在文件存储。

## A2A 0.3 JSON-RPC 接入范围

本版固定对照 https://a2a-protocol.org/v0.3.0/specification/，没有宣称支持 A2A 1.0、所有传输或通过官方一致性测试。

能力卡：`GET /api/v1/a2a/{agent}/agent-card.json`
JSON-RPC：`POST /api/v1/a2a/{agent}`

采用显式能力卡 URL，不占据站点根目录的 `.well-known/agent-card.json`。能力卡声明 JSONRPC、text/plain、不支持 streaming / pushNotifications。支持 message/send、tasks/get、tasks/cancel；未知方法返回 -32601。客户端按查询方式获取异步任务。message/send 总是返回 Task，调用方应配置 blocking=false。当前不提供阻塞等待模式。

示例：

```json
{"jsonrpc":"2.0","id":"request-1","method":"message/send","params":{"message":{"kind":"message","role":"user","messageId":"unique-client-message-id","parts":[{"kind":"text","text":"研究目标"}]},"configuration":{"blocking":false}}}
```

入站同一凭据、Agent、messageId 去重；参数改变时拒绝复用。任务与上下文均按凭据和 Agent 隔离。contextId 关联后续新任务；不自动拼接以前的会话历史。当前不支持入站 taskId 续写、文件输入、推送和流式；Codex 审批仍在本地会话完成。远程 input-required 可显示并轮询，但本版没有向远程任务提交补充输入的界面，请在远程服务完成后继续观察。

出站可接收 Task 或即时 Message。文本结果提取用于协调；原始 artifacts 同时留存于 API 记录，未主动下载远程文件。只支持 0.3 JSON-RPC 服务；能力卡读取后请检查地址和能力，再保存登记。

## API 与权限

协作管理接口仅管理员（管理员 Bearer 或浏览器 Cookie）：

| 方法 | 路径（均以 /api/v1 开头） |
|---|---|
| GET/PUT | /collaboration/config |
| POST | /collaboration/discover：url、tokenEnv |
| GET/POST | /collaborations |
| GET | /collaborations/{cid} |
| POST | /collaborations/{cid}/actions：action=message/pause/resume/cancel，text |
| POST | /collaborations/{cid}/board：issue |
| POST | /collaborations/{cid}/work/{work}/reconcile：remoteId |

创建：goal、leader、agents（ID数组）、mode、maxRounds。POST /collaborations 支持原有 Idempotency-Key。

A2A 同时允许管理员和已有独立应用凭据。应用凭据只能访问相同 instance 与允许 workspace 的已登记 Agent；提交/查询/取消要求 run scope。不能用该凭据读取整个协作黑板或能力配置。普通成员协作入口暂未开放。没有跨用户共享个人文件；本地管理员协作产物归管理员。

HTTP 请求有超时、响应大小上限，禁止跟随重定向以防凭据被转发。允许管理员配置本机、局域网服务；这不是面向不可信租户的任意 URL 抓取接口。生产远程连接应使用 HTTPS。服务端环境变量凭据只由管理员控制。

## 本次验证

Go 全量测试；协作定向 race 测试；HTTP 模拟 A2A/Gitea：委派、真实本地队列执行、自动回收、消息去重、跨凭据拒绝、黑板同步去重、P2P回传、预算限制、远程不确定状态、本地提交窗口重启后取消。

浏览器测试覆盖配置、新建、无效模型结果阻塞、会话定位、取消、移动布局和JS异常。

未连接你的真实 Codex 账号、Docker daemon、Gitea 或第三方 A2A 服务。本版为可试用的协作预览，不代表这些环境已完成联调。Demo 模型返回普通演示文本，负责人会按契约进入等待；不会伪造真实协作成功。
