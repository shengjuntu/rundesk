# UHP 服务入口

RunDesk v0.53.0 在 v0.52.0 引入的 `/v1` 服务入口上补充严格能力准入、持久任务时限、进度与审批衔接及应用客户端示例，线协议仍固定为 UHP `2026-10-19`。RDCA v1 继续使用 `/api/v1`；两者共享 RunDesk 的任务准入、队列、原生运行模块、取消和资源服务。运行 RunDesk 不需要启动 HarnessRouter。

这是一版有明确限制的实现，不声明完整 UHP conformant。发现文档中的 `conformance_class: core` 表示本版采用的基础接口等级；它不是独立认证结果。两种后端均不提供 UHP 硬工具轮次预算或输出 token 预算，原生指令优先级和 Kun 图片输入等差异见下文。即使选定测试后端通过官方套件，也不能据此推断所有原生后端和全部规范要求都已验证。

## 协议身份与授权

`GET /v1/uhp` 无须凭据，返回固定协议版本和不含调用者资料的能力文档。其余端点要求 `Authorization: Bearer <token>`，可使用配置的管理员令牌或 RunDesk 应用凭据。浏览器 Cookie 和个人用户令牌不能用于这个入口。GET 操作要求应用凭据具有 `read`；提交、取消等操作要求 `run`；文件上传、附件输入和产物访问还要求 `files`。队列在真正运行之前再次检查提交凭据。

UHP 对象按**应用凭据 ID** 隔离；同一个应用的两个凭据也不能互相读取、续接或取消对方创建的 response、session 和 file。管理员令牌使用独立的管理员身份，不自动穿透应用凭据的 UHP 对象空间。撤销或过期的凭据不能继续轮询或接收流。越权对象与不存在的对象均返回 `404`。

每个已配置 harness 是 RunDesk 的实例、工作区及应用 preset 绑定的投影，ID 使用 `chrn_` 前缀。应用凭据只能看到自身授权的绑定；实例或 preset 的配置仍由 RunDesk 管理。UHP 本版不提供另一套 harness 配置写入接口。列表中的 `rundesk_capabilities` 报告后端输入和执行能力，`rundesk_execution` 说明预算、指令与进度语义；模型列表的 `rundesk_availability_basis` 说明可用性判定依据。模型列表、已配置凭据和运行程序的检查不能保证远端供应商随后一定成功。

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

## 能力发现与严格准入

客户端应先读取目标 harness 的 `rundesk_capabilities`，并在提交时用 `metadata.rundesk_require` 声明必须具备的能力。发现只帮助选择，服务端预检才是本次任务的准入依据：任意能力未知或不支持，均在创建任务和物化本次附件之前返回 `422 rundesk_capability_unsupported`；数组格式非法返回 `400 invalid_input`。

```json
{
  "input": "检查当前项目并给出简短总结",
  "background": true,
  "timeout_seconds": 600,
  "metadata": {
    "harness_id": "chrn_example",
    "rundesk_require": ["input_text", "timeout_seconds", "progress"]
  }
}
```

`rundesk_require` 接受最多 16 个非空能力名。可查询的名称为 `input_text`、`input_file`、`input_image`、`input_history`、`native_instructions`、`max_step`、`max_output_tokens`、`timeout_seconds` 和 `progress`。前两项、时限与进度两后端均支持；图片限 Codex 且仍需模型支持；历史导入、原生指令优先级及两种计数预算目前均为 false。

能力的依据是 RunDesk 适配器契约，`rundesk_execution.capability_basis` 为 `adapter_contract_not_model_readiness`。严格准入通过不保证模型模态、供应商或账号已就绪；例如 Codex 的 `input_image: true` 表示附件可传递给原生后端，实际运行仍需支持图片的模型及有效凭据。

不要求严格能力时，合法的 `max_step` 和 `max_output_tokens` 在两种后端上均列入 `metadata.ignored_fields`，不会限制实际工具轮次或输出量。依赖这些硬限制的 APP 应要求对应能力并处理拒绝，不能把参数已接收当作已执行。预算字段仍须是正整数，非法值不因暂不支持而被接受。返回的能力与执行语义由服务端写入，不能通过请求 metadata 覆盖。

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

## 任务时限、进度与审批

`timeout_seconds` 省略时为 **3600 秒**，显式值范围为 **1–86400 秒**，没有以 0 关闭时限的语义。绝对截止时间在入队前计算，包含排队时间，并以 `metadata.rundesk_deadline` 返回。时限保存在公共任务的 `Deadline` 中，队列及最终派发均检查；Manager 生命周期内的计时器负责到期处理，重启后从持久任务恢复。关闭轮询、断开 SSE 或暂停队列不会停止计时。

超时通过原生取消停止任务；最终 response 为 `incomplete`，`incomplete_details.reason` 为 `timeout`。取消是协作式的，可能需要等待后端停止，不回滚已经产生的文件或外部动作。已经完成的旧任务不会因同一会话的新任务而重新计时或被取消。

APP 可轮询 `GET /v1/responses/{id}` 的 `metadata.rundesk_progress`。`state` 可为 `queued`、`dispatching`、`running`、`waiting_approval`、`paused`、`cancelling`、`completed`、`cancelled`、`incomplete`、`failed`、`interrupted` 或 `unknown`。这是 RunDesk 的进度扩展，不替换 UHP 标准 `status`；只有同一原生 run 的待处理审批证据才标记为 `waiting_approval`，不能把所有等待状态都当作审批。

```json
{
  "state": "waiting_approval",
  "required_action": {
    "type": "approval",
    "workbench_url": "/?session=native_session_id"
  }
}
```

工作台相对地址供有权用户打开会话并处理审批，仍受工作台现有访问控制；它不是审批凭据。扩展不返回原始工具参数、令牌或宿主路径，也不新增 UHP 审批控制接口。历史终态保留自己的结果，不跟随会话后续轮次变化。

进度元数据可在 GET 及初始、最终 SSE 响应快照中读取；中途进度变化通过轮询观察。标准 SSE 继续传递文字、工具与终态事件，不插入私有进度事件，也不因相同状态的轮询重复生成事件。

## 应用客户端示例

[examples/uhp_client.py](../examples/uhp_client.py) 是 Python 3.10+ 标准库客户端，覆盖发现、上传、后台提交、轮询、续接、取消和产物下载。令牌通过环境变量 `UHP_API_KEY` 提供；服务根地址可用 `UHP_URL` 设置。

```sh
python3 examples/uhp_client.py harnesses
python3 examples/uhp_client.py upload notes.txt
python3 examples/uhp_client.py submit '总结附件内容' \
  --harness-id chrn_example --file-id file_example \
  --operation-key saved-job-001 --task-timeout 600 \
  --require-capability input_file --require-capability timeout_seconds
python3 examples/uhp_client.py read resp_example --wait 60
python3 examples/uhp_client.py files hsess_example
python3 examples/uhp_client.py download cntr_example file_example --output report.txt
```

示例中的 ID 应替换为实际返回值。续接提交使用 `--previous-response-id`；需要停止服务端执行时使用 `cancel`。`--wait` 是客户端观察时间，`--task-timeout` 才是服务端任务时限；等待超时不会取消任务。遇到待审批时客户端返回控制权，不自动批准工具。Python API 的 `events()` 支持用 `last_event_id` 续读 SSE。

提交前由 APP 持久保存 operation key 和确切请求。若连接失败导致结果未知，应使用同一凭据、同一键及同一请求恢复，不能重新上传并换键提交。客户端默认不自动重试；可用 `--transport-retries` 显式重试传输失败，HTTP 错误不重试。更换应用凭据 ID 会改变对象及幂等命名空间，不能用新凭据查回旧凭据的任务。

## 文件与存储

上传支持 multipart `file` 字段，单文件上限 **64 MiB**；内联 data URL 随 JSON 请求发送，完整 JSON 请求上限 **8 MiB**。超限返回 `413 file_too_large`。远程 URL 不会由服务器代为下载；使用 base64 data URL 或先上传取得 `file_id`。

单次任务文本上限为 256 KiB，最多 64 个附件。图片输入检查实际文件格式，当前接受 PNG、JPEG、WebP；文件名或声明的媒体类型不能把普通文本变成图片，最终还需所选原生模型支持图片。

独立上传时还未选定工作区，因此原始上传保存一次；第一次附加到某个工作区时，为原生运行环境物化一次，后续在同一工作区复用。内联附件直接写入选定工作区。输出文件从 `outputs/<原生会话 ID>/` 引用，不复制原生 agent 目录，也不建立额外的持久产物快照。

每个已登记产物保存内容摘要和来源。会话文件列表保留先前任务的引用；原始文件被修改或删除时，`rundesk_availability` 报告 `changed` 或 `missing`。历史下载不会用新内容冒充旧版本。需要永久冻结文件时，应使用 RunDesk 明确的快照流程。下载使用受限文件句柄和临时校验缓冲，返回原始字节、附件文件名及 `nosniff`。

UHP 的映射和投影位于现有 `state.db` 的 `uhp-*` 记录中；任务状态权威仍是公共 task/session 核心。它没有为每个任务再建立可独立写入的一套状态文件。任务资源导出是可重建视图，见 [TASK-RESOURCES](TASK-RESOURCES.md)。

## 当前边界

- 支持基础发现、模型查询、阻塞与流式任务、后台提交、同会话续接、取消、响应读回，以及文件和会话检查。
- `max_output_tokens` 暂不强制执行，明确列入 `metadata.ignored_fields`。`tools` 和 `include` 按固定 UHP 版本保留并忽略，也会逐项列入；客户端不能借此新增工具权限。未知请求字段同样可观察地忽略。
- `max_step` 在 Codex/Kun 均不强制执行，必须通过严格能力准入拒绝依赖硬工具轮次限制的请求。不能将原生模型完成次数等同于工具轮次；这是完整 Core 语义覆盖的缺口。
- 额外任务指令沿用现有输入契约，以有标签的任务文本传递，报告 `rundesk_instruction_mode: task_text` 和 `rundesk_instruction_retention: session_history`。这些文本会进入原生会话历史，后续续接仍可能受其影响，不提供一次任务后自动清除或原生 system/developer 优先级。要求 `native_instructions` 会在准入时被拒绝。
- Codex 支持原生图片附件；Kun 当前不支持，不能通过 UHP 伪装成支持。发现文档保持基础等级，不对所有后端声明 Extended。
- `input_history` 为 false：输入数组以最后一条用户消息作为本次任务，不将早期消息建立为导入会话记录。
- 本版不提供 Full 所需的 harness CRUD 和会话分享，也不提供 UHP plugin、environment、memory 管理。RunDesk 已有的相应平台资源继续通过 RDCA 配置。
- 不迁移 HarnessRouter 已有的 ID、凭据、原生线程、历史轨迹或幂等回执；改接服务只影响在 RunDesk 新建的运行。

## 验证与复现

协议版本仍为 `2026-10-19`；本版官方套件固定到 HarnessRouter 提交 `038d4d27afed078db339b107f8bf6031ebb37872`、套件版本 `2026.10.19.post3`。官方套件独立运行，不成为 RunDesk 的构建或运行依赖；本仓库不复制其实现。

0.52.0 的历史验证在 2026-10-20 使用原生 Codex 协议模拟器完成 post2 官方 Extended 全套：**50/50 通过，0 失败、0 跳过、0 错误**，当时固定提交为 `caa3c0a109fffd84ef245dcdad23f2cdfeff7153`。原始报告见 [uhp-conformance-demo.json](validation/current/uhp-conformance-demo.json)，不回写为本轮结果。该报告的取消项在请求取消前已完成，附件项未验证真实模型读取内容。

0.53.0 的实际执行范围与结果统一记录在 [uhp-hardening.json](validation/current/uhp-hardening.json)，应分别查看官方 Extended 套件、预算/时限/进度定向测试及客户端测试。post3 的 T-12 是 Full 等级的工具轮次预算测试，依赖本服务未提供的 `createHarness`；Extended 的 50 项不能据此验证硬步数限制。应用内测试验证能力披露与严格拒绝、时限调度及竞争、审批身份和重试等边界，不冒充 T-12 或完整 Full 验证。真实 Kun、Docker 与模型凭据链路仍待目标环境验收。

`scripts/test-uhp-conformance.sh` 检查外部 checkout 的固定提交及 suite 未修改，然后运行原始套件。准备 Python 3.10+ 和 `jsonschema`，对已经运行的服务执行：

```sh
export UHP_SUITE_CHECKOUT=/path/to/harnessrouter
export UHP_API_KEY=your-service-token
scripts/test-uhp-conformance.sh \
  --base-url http://127.0.0.1:3210 \
  --class extended \
  --model demo-fixture \
  --label 'RunDesk v0.53.0 UHP / native demo fixture' \
  --json build/uhp-0.53.0-conformance.json \
  --plain
```

上例须连接显式 `--demo` 启动的 RunDesk；它验证 HTTP、原生事件适配、任务和存储行为，不调用真实模型。真实后端应使用另一个明确标注的报告，并选定实际 harness/model。`--only` 的局部结果不能替代完整等级报告；跳过检查也不算通过。官方套件对部分规范语义没有覆盖，因此还必须保留应用内的授权、竞争、取消、重启和文件边界测试。

定向复验入口：

```sh
go test ./internal/app -run 'TestUHP|TestTaskDeadline' -count=1 -timeout=300s
python3 scripts/test_uhp_client.py
python3 scripts/test_uhp_client_integration.py --binary ./bin/rundesk --output-dir ./build/uhp-client-checks
```

`test_uhp_client.py` 使用本地 HTTP fixture；`test_uhp_client_integration.py` 使用指定的已编译 RunDesk，自行启动并清理隔离的 `--demo` 服务，验证完整 HTTP/SQLite/原生协议模拟链路。后者不能指向线上服务，也不会批准工具。两者均不证明真实模型执行或业务结果质量。
