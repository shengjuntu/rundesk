# 轨迹阅读、耗时分析与问答

从会话的“轨迹”进入共同调试工作台，先按轮次阅读，再按需核对步骤、模型通信和原始证据。所有视图共享固定会话、轮次与宿主事件上界；读取历史不会控制原任务。下文依次说明定位方法、耗时和独立问答，以及阅读器的分页与内容边界。

<a id="debug-overview"></a>

## 从任务定位到证据

RunDesk 将执行过程作为可检查证据保存。使用共同的调试工作台检查 Kun 与 Codex；轨迹、HTTP、摘要问答和 Kun 高级面板共享会话与历史范围。Codex HTTP 采集默认关闭，需要在目标会话开启后于下一轮提交应用。内部状态、单步和运行分叉由 Kun 扩展提供。记录反映实际输入输出和控制动作，不推测模型隐藏推理。

<a id="debug-1"></a>

1. 点击会话的“轨迹”进入调试工作台，在轨迹阅读器按 Run 查看输入、回复、工具步骤、审批和异常；按需打开原始证据。
2. 定位失败或空返回步骤，核对工具参数、实际结果与原始事件；缺少结束事件保持未知，不能等同失败或成功。
3. 使用只读调试检查固定宿主事件游标，查看轮次、步骤、异常和统计。分块读取完整事件，避免把预览当作全部证据。
4. 需要模型解释时创建独立诊断会话，固定来源范围；审核建议后再明确发送控制操作，不让分析自动修改原任务。

[直通观测与统一工作台](UNIFIED_DEBUGGING.md) · [共同只读接口与 MCP](DEBUG-SERVICE.md) · [诊断与建议审核](KUN-DIAGNOSIS.md) · [轨迹界面](TRACE.md#trace-overview) · [轨迹阅读器](TRACE.md#trace-reader)

<a id="trace-overview"></a>

## 选择轮次与步骤

点击会话的“轨迹”进入共同调试工作台，检查轨迹、LLM 请求/响应、摘要与问答，以及 Kun 高级调试。各视图共享会话、所选轮次和宿主事件上界 `through`；不会通过切换页签悄悄推进历史。Codex HTTP 采集默认关闭，按会话开启后在下一轮提交生效；已有记录保持可查。

轨迹入口首先提供[对话式阅读](TRACE.md#trace-reader)，可按需打开步骤、模型通信和原始证据。

<a id="trace-1"></a>

1. 点击已有会话的“轨迹”，打开“运行轨迹”调试工作台。首次读取固定该会话当前的宿主事件上界；上方可选择轮次。
2. 在“步骤检查”按类型、需关注项或脱敏预览搜索筛选。每页显示 20 个步骤，分页由后端结构化查询提供。
3. 选择步骤，查看状态、关联依据、起止来源和预览。缺少开始/结束、轮次身份或唯一配对时明确显示未知和异常信号，不补造成功状态或耗时。
4. 点击“读取完整结构（所有来源事件）”，逐块读取开始、结束及其他来源事件。原始 JSON 文本保留大整数精度；超过页面 32M 字符范围时停止显示并提示，可继续使用来源事件分页。
5. 在“LLM 请求 / 响应”查看通过直通代理实际保留的 HTTP 交互；正文分块、采集上限和完整性判断见 [统一调试与采集](UNIFIED_DEBUGGING.md)。
6. 点击“读取最新记录”才推进所有视图的历史范围。“返回对话”关闭工作台，继续原任务的操作沿用对话入口。

页面搜索只匹配有界预览，不代表全文检索。没有匹配不能证明完整正文不存在该文本。耗时仅在完整、有效的事件边界间提供；并行步骤的耗时合计不等于运行墙钟时间。工具返回成功、模型回复完成和业务目标达成分别判断。

Kun 高级页嵌入原 DevTools 面板，历史事件也受共同 `through/runId` 限制。当前 worker 控制状态独立更新；选定历史快照不改变控制目标，控制仍检查当前 run/revision。宿主事件 ID 与 Kun worker sequence 不能互换。

<a id="trace-reader"></a>

## 对话式阅读与证据抽屉

The trace entry point now opens a reading view over the unified observation
model. The existing step inspector, HTTP/WebSocket inspector, analysis panel and
capability-gated Kun controls remain in the same dialog and share its history
cursor. No agent source, task execution or observation storage changes are
required for this view.

<a id="reader-1"></a>

- The round outline selects a run from its original user request. On narrow
  screens it starts collapsed. The existing project/session navigation is reused.
- User input and assistant messages are readable in chronological order. Tool
  start/end records form one action using the existing lifecycle projector.
  Tool arguments/results and recorded reasoning start folded. Failures, pending
  approvals and incomplete lifecycle records are expanded and marked.
- Text blocks over 4,000 characters show an 800-character excerpt with an
  explicit expansion control. Expansion uses already-loaded text, does not query
  the model, and is distinct from a source-content truncation warning.
- Markdown headings, lists, tables and code are rendered with DOM nodes; HTML is
  literal. Only HTTP(S) text links are active. Images and files are source paths,
  not auto-fetched copies. Native diffs show added/removed lines.
- Evidence opens on demand. The drawer shows source identity, clocks,
  associations, completeness, and references to original journal records.
  Each event is read in 4,000-character pages. Exact JSON integers are retained.
  HTTP references open the existing byte-preserving transport inspector.
- Network records can be shown in the reading view but remain independent of
  runtime actions. Sharing a run or timestamp does not prove a model-call match.
- Inspection is read-only. To approve, resume, step or otherwise control a task,
  use the existing supported controls; browsing cannot issue those operations.

<a id="trace-2"></a>

## 耗时分析

耗时分析沿用选定会话、轮次和固定事件上界。有效的开始与结束记录才能给出完整区间；缺少记录、无效时钟或无法配对时显示未知，不把它们当作零时长。

“本轮总耗时”使用宿主 `run/input` 到已记录终态的完整区间，包含提交后、原生执行开始前的等待。只有原生 `turn/started` 边界时，界面标为“原生执行区间”；只有 Kun worker 起止记录时标为“Kun 执行区间”。后两者不包含宿主启动前的等待，不能当作完整用户等待时间。`clock`、`startMethod` 和 `endMethod` 声明区间依据。

已知步骤耗时回答“记录中的各类步骤用了多少区间时间”。步骤可能并行、嵌套或重叠，因此步骤耗时合计不是整轮总耗时，也不是 CPU 时间。模型运行步骤与 HTTP／WebSocket 观测是不同证据来源，不把两者相加冒充执行时间。

步骤列表可选择记录顺序、耗时从长到短或从短到长。排序先覆盖完整筛选结果，再分页；没有完整耗时的步骤排在已知区间之后。统计返回最多十个最慢运行步骤及按类型的工作量。相同轮次与 `through` 下的查询结果可复查；点击读取最新记录才纳入后续事件。

`kind=statistics` 响应的 `data.timing.runs` 中，`durationMs` 只表示依据已声明来源的完整、有效同钟区间；进行中轮次可返回 `observedElapsedMs`，它是同一时钟中已保留事件的跨度，不是“当前时间减开始时间”。`byType` 和 `knownWorkDurationMs` 汇总已知运行步骤工作量，`withoutCompleteDuration` 计入未知区间，`pointSteps` 单独计数。HTTP／WebSocket 观测不计入该运行步骤汇总。

整轮区间按所选轮次和固定 `through` 展示，不受步骤类型或预览搜索过滤影响；按类型工作量和最慢步骤则属于当前步骤筛选范围。

调试查询新增 `sort=recorded|duration_desc|duration_asc`，仅用于 `steps` 和 `issues`。默认仍是记录顺序，不改变已有客户端的分页语义。

<a id="trace-3"></a>

## 创建独立诊断

在“摘要与问答”先选择轮次，再输入问题并点击“创建诊断并发送问题”。统计摘要来自相同后端步骤查询；模型问答会创建独立 RunDesk 会话，固定原会话、所选轮次和事件上界，然后通过只读工具按需读取证据。模型用量计入诊断会话，不将分析问题追加到原业务线程。

正常模式的 Codex 轨迹问答使用环境设置中的专用 Kun 维护助手，并保存原实例、工作区、轮次和游标的 `traceOrigin`；不把问题追加到原应用任务。诊断使用独立目录与只读 trace 工具，禁用写文件和 Bash，不续跑原业务任务。缺少 Kun 配置时仍可直接查看原始轨迹。Demo 和保留研究调用的边界见[内部维护](MAINTENANCE.md)。

| trace 工具 | 用途 |
| --- | --- |
| `trace_list_runs` | 来源快照内的轮次和状态 |
| `trace_find_steps` | 筛选步骤预览 |
| `trace_get_step` | 步骤详情与原始事件引用 |
| `trace_read_event` | 脱敏事件 JSON 的 Unicode 分块 |
| `trace_find_issues` | 确定性异常信号 |
| `trace_statistics` | 步骤计数与已知耗时统计 |
| `trace_propose` | 有证据引用的建议，不执行控制 |
| `trace_list_llm_requests` | 已保留模型请求的结构化列表 |
| `trace_read_llm_request` | 模型请求元数据及精确字节分块 |

工具只接受固定查询参数，不开放任意 SQL、shell 或数据库写入。日志、工具输出和模型正文属于不可信证据，不能当成诊断指令。回答应引用会话及宿主事件 ID，区分记录事实、推断和缺失数据。

原会话新增记录不改变已创建的诊断依据。需要更新依据时，从原会话工作台显式读取最新记录，再创建诊断。给原任务发送补充指令、停止或批准工具属于独立执行操作，不由只读查询触发。

<a id="trace-4"></a>

## 容量与来源

生命周期重建最多读取 20000 条记录、64 MiB，单条非模型观测事件最多 8 MiB。超限报错，不返回部分统计冒充完整结果。诊断快照导出另有限制：最多 100000 条事件、128 MiB 总载荷；`llm/exchange` 可以超过通用单事件 8 MiB 上限，使用专用字节分块查询。

普通会话事件订阅对大型 `llm/exchange` 返回 `payloadOmitted: true` 和 `readWith: llm_request` 的清单，避免把完整模型正文反复推到对话；原始数据库记录保留。历史未采集的数据、被删除的来源和未经过代理的调用不能通过摘要补回。真实模型分析质量及业务效果需要实际任务验证。

<a id="reader-2"></a>

## 阅读投影接口

`GET /api/v1/sessions/{id}/debug/query?kind=transcript&through=99&runId=...&offset=0&limit=40`

The response uses the same DebugService authorization and fixed snapshot as
`activity`. Its entries carry the canonical `observation.Activity` plus a role,
display title and bounded blocks. Storage remains the event journal. This is a
reading projection, not another source of truth or a full model transcript.

The API defaults to 20 entries, caps requests at 50, caps content at 12,000 Unicode
characters and 32 blocks per entry, and caps source reads at 16 MiB per page. The
browser replaces a page of 40 entries rather than accumulating the entire
conversation or fetching every raw payload. Missing, bounded and partial content
are disclosed separately through `contentUnavailable`, `contentTruncated` and
`contentPartial`. Original saved evidence remains accessible through its existing
read APIs and their limits.

Open Codex message/reasoning deltas are scoped by exact native identifiers and
the same history cutoff. Completed items use their retained native content.
Host input/native input echoes are deduplicated only where retained content is
known to match. Kun plan/act calls retain their distinct purpose and native IDs.

<a id="reader-3"></a>

## 阅读器的内容边界

- History advances with **Read latest records**. There is no implicit live follow
  that could change evidence under an open inspection.
- Search covers loaded content on the current page. It is not global full-text
  search. Pagination remains available when the display filters hide a page.
- Content is structurally redacted under the existing policy; arbitrary natural
  language is not a universal credential detector.
- Child-agent trees are not inferred from message order or timestamps. No new
  hierarchy is asserted without explicit native relationships.
- Images/files remain references. This version does not render external image
  URLs, execute Mermaid/HTML, or fetch linked artifacts automatically.
- Transcript display does not add token counters from different observation
  channels or calculate billing.

<a id="reader-4"></a>

## 阅读器验证入口

- `scripts/trace-reader-ui-test.cjs`: DOM-only safety, folding, pages, fixed
  cursors, lazy evidence, stale responses and teardown with JSDOM.
- `scripts/unified-debug-ui-test.cjs`: actual Chromium with explicit service
  fixtures; trace and narrow layout plus legacy step, HTTP/WebSocket byte paging,
  historical Kun control protection, one dialog and read-only inspection.
- `internal/tracequery/transcript_test.go`: retained Codex/Kun fixtures, native
  pairing, snapshot boundaries, content limits/redaction and exact identifiers.
- API/service tests cover selector validation, capability exposure and ownership.

The browser fixture is not a live provider run. Screenshots used for development
are not shipped as session evidence.
## Kun 调试与研究复用

<a id="debug-2"></a>

### Kun 内部调试

工作台中的 Kun 高级面板保留原 DevTools 的 Network 调用证据、Elements 上下文与快照差异、Sources 暂停/单步/补充指令、Performance 用量、Application MCP、Layers 模块及 Console 结构化查询。历史记录保持固定游标；“读取最新记录”才推进共同历史范围。“检查当前控制状态”用于当前 worker 状态，不能把它当成历史快照。控制需要当前运行和状态 revision；单步/继续不绕过审批、预算或检查点守卫。

[断点与 Console](KUN-DEBUG.md#kun-controls) · [调用证据与快照](KUN-DEBUG.md#kun-inspection) · [控制与模块面板](KUN-DEBUG.md#kun-panels) · [安全检查点](KUN-CHECKPOINTS.md)

<a id="debug-3"></a>

### 回放与验证改动

- 离线实验保存不可变来源和记录层编辑，不改原始执行证据。
- Hybrid 从安全检查点建立独立分支，重新调用模型，工具仅严格回放；录制未命中即停止。
- Live 从固定预览明确启动真实工具执行；不保证可撤销外部副作用。
- 单条工具结果假设另存替换和实际使用证据，原录制保持不变。
- 同基线对照分列继承与新增用量、真实与回放工具；缺失不按零。固定文本规则或产物规则生成不可变评分报告，评分高不自动等于业务有效。

[分叉](KUN-FORKS.md#runtime-forks) · [工具结果假设](KUN-FORKS.md#tool-hypotheses) · [对照](KUN-FORKS.md#branch-comparison) · [规则评分](KUN-EVALUATION.md#branch-scoring)

<a id="debug-4"></a>

### 将调试成果复用到业务

K4 从成功案例参数化工具轨迹，记录适用范围与失败条件。候选使用独立任务集验证，通过门槛后才可启用到指定应用；快速路径沿用审批、预算和动作账本，偏离守卫后回退动态执行。已完成的相同副作用在同一回退运行中复用，结果未知时不自动重做。

固定配对重复采样、产物规则及文件初始化为质量和效率比较提供证据。独立文件目录不等于进程/网络隔离，且当前禁用 Bash/MCP，隔离候选不直接晋升共享应用。业务收益需在实际任务集确认。

[K4 使用](KUN-K4.md) · [任务集](KUN-EVALUATION.md#task-sets) · [重复对照](KUN-EVALUATION.md#repeated-benchmarks) · [产物规则](KUN-EVALUATION.md#artifact-rules) · [独立目录](KUN-EVALUATION.md#evaluation-fixtures)
