# UHP 服务入口

本次在 RunDesk v0.52.0 重构版上加入独立的 `/v1` 服务入口，固定采用 UHP `2026-10-19` 的线协议。RDCA v1 继续使用 `/api/v1`；两者共享 RunDesk 的任务准入、队列、原生运行模块、取消和资源服务。运行 RunDesk 不需要启动 HarnessRouter。

这是一版有明确限制的实现，不声明完整 UHP conformant。发现文档中的 `conformance_class: core` 表示本版采用的基础接口等级；它不是独立认证结果。Codex 的 `max_step`、原生指令优先级和 Kun 图片输入等差异见下文。即使选定测试后端通过官方套件，也不能据此推断所有原生后端和全部规范要求都已验证。

## 协议身份与授权

`GET /v1/uhp` 无须凭据，返回固定协议版本和不含调用者资料的能力文档。其余端点要求 `Authorization: Bearer <token>`，可使用配置的管理员令牌或 RunDesk 应用凭据。浏览器 Cookie 和个人用户令牌不能用于这个入口。GET 操作要求应用凭据具有 `read`；提交、取消等操作要求 `run`；文件上传、附件输入和产物访问还要求 `files`。队列在真正运行之前再次检查提交凭据。

UHP 对象按**应用凭据 ID** 隔离；同一个应用的两个凭据也不能互相读取、续接或取消对方创建的 response、session 和 file。管理员令牌使用独立的管理员身份，不自动穿透应用凭据的 UHP 对象空间。撤销或过期的凭据不能继续轮询或接收流。越权对象与不存在的对象均返回 `404`。

每个已配置 harness 是 RunDesk 的实例、工作区及应用 preset 绑定的投影，ID 使用 `chrn_` 前缀。应用凭据只能看到自身授权的绑定；实例或 preset 的配置仍由 RunDesk 管理。UHP 本版不提供另一套 harness 配置写入接口。列表中的 `rundesk_capabilities` 报告后端实际输入能力；模型列表的 `rundesk_availability_basis` 说明可用性判定依据。模型列表、已配置凭据和运行程序的检查不能保证远端供应商随后一定成功。

上述隔离是 API 对象授权，不为同一工作区另建文件系统沙箱。显式绑定同一个工作区的原生运行仍遵守该工作区及原生引擎的访问策略；需要互不读取项目文件的应用应使用独立工作区和适当的执行环境。

所有 UHP 响应，包括错误，都带 `UHP-Version: 2026-10-19`。请求指定其他版本返回 `400 unsupported_protocol_version`，不会自动切换版本。非本机部署应通过 HTTPS 反向代理；SSE 代理须关闭缓冲。

## 端点

| 操作 | 路径 |
| --- | --- |
| 协议与能力发现 | `GET /v1/uhp` |
| harness 列表、单项、模型 | `GET /v1/harnesses`、`GET /v1/harnesses/{id}`、`GET /v1/harnesses/{id}/models` |
| 模型目录 | `GET /v1/models` |
| 提交任务 | `POST /v1/responses` |
| 响应读回、原始输入、取消、删除 | `GET /v1/responses/{id}`、`GET /v1/responses/{id}/input_items`、`POST /v1/responses/{id}/cancel`、`DELETE /v1/responses/{id}` |
| 会话列表、单项、任务历史、取消 | `GET /v1/sessions`、`GET /v1/sessions/{id}`、`GET /v1/sessions/{id}/turns`、`POST /v1/sessions/{id}/cancel` |
| 上传 | `POST /v1/files` |
| 产物列表、打包下载 | `GET /v1/sessions/{id}/files`、`GET /v1/sessions/{id}/files/archive` |
| 产物下载 | `GET /v1/containers/{container_id}/files/{file_id}/content` |

`GET /v1/responses/{id}/events` 是 RunDesk 的额外 SSE 读回入口，支持 `Last-Event-ID`；它并非这个固定版本 OpenAPI 中已经定义的端点。PDF 预览路径 `/v1/containers/{container_id}/files/{file_id}/pdf` 返回 `501 preview_unavailable`。会话删除与分享、harness 写入接口本版未提供。

## 调用示例

在配置好服务令牌、实例和工作区后，可直接查询配置并提交任务：

```sh
curl http://127.0.0.1:3210/v1/harnesses \
  -H "Authorization: Bearer $RUNDESK_TOKEN"

curl http://127.0.0.1:3210/v1/responses \
  -H "Authorization: Bearer $RUNDESK_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-task-001' \
  -d '{"input":"检查当前项目并给出简短总结","stream":true}'
```

省略 `metadata.harness_id` 时使用调用身份范围内的默认绑定。后续请求传入上一条响应的 `previous_response_id` 即可续接；不同任务应使用新的幂等键。

## 任务与会话

| UHP 对象 | RunDesk 对应关系 |
| --- | --- |
| `response` | 一次公共队列 task；一个 response ID 对应一个 task ID |
| `session` | 一个原生 RunDesk 会话；多次 task 可以沿用它 |
| `container` | 这个会话的产物命名空间，不是 Docker 容器 |
| `file` | 上传内容或原始产物的受控引用 |

第一条任务创建会话；`previous_response_id` 在原会话提交新 task，保留工作区和原生上下文。同一个会话已有运行中或排队任务时返回 `409 session_busy`。续接不能更换 configured harness；可用模型可以按任务更换。原生子 agent 仍属于原生运行事件，不会自动成为另一条平台 task。

提交回执、UHP 映射和 task 在同一个 SQLite 事务中写入。带 `Idempotency-Key` 的重试按调用身份查回第一次提交；第一次还在运行时等待同一结果，不再次执行，也不沿用 RDCA 的“处理中冲突”语义。键只在各自协议和调用身份内有效。断开 HTTP/SSE 连接不会取消任务；停止工作需要显式调用 cancel。

本版不能按请求关闭任务审计和幂等记录的持久化。传入 `store: false` 会被明确列入 `metadata.ignored_fields`，返回的 `store` 仍为 `true`，因此客户端不会得到虚假的“未保存”承诺。删除 response 会隐藏其常规 UHP 历史入口，不取消正在运行的 task，也不清除原生审计或幂等回执；原有幂等键仍对应原始执行结果。永久删除数据是单独的管理操作，不由本版 UHP 的 response DELETE 承担。

SSE、阻塞返回和后续 GET 共用一份原生日志投影。事件序号从零连续递增，终态不可逆，取消保留已经产生的输出。未采集到的 usage 为 `null`，不会用零占位。RunDesk 仍保留原始轨迹供授权的 RDCA 调试功能检查。

## 文件与存储

上传支持 multipart `file` 字段，单文件上限 **64 MiB**；内联 data URL 随 JSON 请求发送，完整 JSON 请求上限 **8 MiB**。超限返回 `413 file_too_large`。远程 URL 不会由服务器代为下载；使用 base64 data URL 或先上传取得 `file_id`。

单次任务文本上限为 256 KiB，最多 64 个附件。图片输入检查实际文件格式，当前接受 PNG、JPEG、WebP；文件名或声明的媒体类型不能把普通文本变成图片，最终还需所选原生模型支持图片。

独立上传时还未选定工作区，因此原始上传保存一次；第一次附加到某个工作区时，为原生运行环境物化一次，后续在同一工作区复用。内联附件直接写入选定工作区。输出文件从 `outputs/<原生会话 ID>/` 引用，不复制原生 agent 目录，也不建立额外的持久产物快照。

每个已登记产物保存内容摘要和来源。会话文件列表保留先前任务的引用；原始文件被修改或删除时，`rundesk_availability` 报告 `changed` 或 `missing`。历史下载不会用新内容冒充旧版本。需要永久冻结文件时，应使用 RunDesk 明确的快照流程。下载使用受限文件句柄和临时校验缓冲，返回原始字节、附件文件名及 `nosniff`。

UHP 的映射和投影位于现有 `state.db` 的 `uhp-*` 记录中；任务状态权威仍是公共 task/session 核心。它没有为每个任务再建立可独立写入的一套状态文件。任务资源导出是可重建视图，见 [TASK-RESOURCES](TASK-RESOURCES.md)。

## 当前边界

- 支持基础发现、模型查询、阻塞与流式任务、后台提交、同会话续接、取消、响应读回，以及文件和会话检查。
- `max_output_tokens` 暂不强制执行，明确列入 `metadata.ignored_fields`。`tools` 和 `include` 按固定 UHP 版本保留并忽略，也会逐项列入；客户端不能借此新增工具权限。未知请求字段同样可观察地忽略。
- `timeout_seconds` 由服务端持久预算控制，预算中止返回 `incomplete`。Kun 的步数预算使用原生步骤。Codex 尚无可可靠映射的工具轮次预算，提交 `max_step` 返回明确的不支持错误；这是完整 Core 规范覆盖的缺口。
- 额外任务指令沿用现有输入契约，以有标签的任务文本传递，并在 `metadata.rundesk_instruction_mode` 中报告 `task_text`。它不修改全局 harness 配置，也不保证原生 system/developer 角色的指令优先级。
- Codex 支持原生图片附件；Kun 当前不支持，不能通过 UHP 伪装成支持。发现文档保持基础等级，不对所有后端声明 Extended。
- `input_history` 为 false：输入数组以最后一条用户消息作为本次任务，不将早期消息建立为导入会话记录。
- 本版不提供 Full 所需的 harness CRUD 和会话分享，也不提供 UHP plugin、environment、memory 管理。RunDesk 已有的相应平台资源继续通过 RDCA 配置。
- 不迁移 HarnessRouter 已有的 ID、凭据、原生线程、历史轨迹或幂等回执；改接服务只影响在 RunDesk 新建的运行。

## 验证与复现

规范与官方套件固定在 HarnessRouter 提交 `caa3c0a109fffd84ef245dcdad23f2cdfeff7153`，协议版本 `2026-10-19`，套件版本 `2026.10.19.post2`。官方套件独立运行，不成为 RunDesk 的构建或运行依赖；本仓库不复制其实现。

2026-10-20 使用原生 Codex 协议模拟器完成官方 Extended 全套：**50/50 通过，0 失败、0 跳过、0 错误**。原始报告见 [uhp-conformance-demo.json](validation/current/uhp-conformance-demo.json)。这是选定模拟后端的线协议验证：取消项的任务已在取消请求之前完成；附件项验证了输入接收但没有验证模型读取内容。真正中途取消、部分输出、授权隔离和竞争需由 Go 集成测试另行覆盖。报告中的套件判定不扩大为真实 Codex、Kun 或全部规范语义的兼容承诺。

`scripts/test-uhp-conformance.sh` 检查外部 checkout 的固定提交及 suite 未修改，然后运行原始套件。准备 Python 3.10+ 和 `jsonschema`，对已经运行的服务执行：

```sh
export UHP_SUITE_CHECKOUT=/path/to/harnessrouter
export UHP_API_KEY=your-service-token
scripts/test-uhp-conformance.sh \
  --base-url http://127.0.0.1:3210 \
  --class extended \
  --model demo-fixture \
  --label 'RunDesk v0.52.0 UHP / native demo fixture' \
  --json docs/validation/current/uhp-demo-conformance.json \
  --plain
```

上例须连接显式 `--demo` 启动的 RunDesk；它验证 HTTP、原生事件适配、任务和存储行为，不调用真实模型。真实后端应使用另一个明确标注的报告，并选定实际 harness/model。`--only` 的局部结果不能替代完整等级报告；跳过检查也不算通过。官方套件对部分规范语义没有覆盖，因此还必须保留应用内的授权、竞争、取消、重启和文件边界测试。
