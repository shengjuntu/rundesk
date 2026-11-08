# Kun 调试：控制、证据与检查面板

本手册集中说明 Kun 的调试控制和只读内部检查。历史证据使用固定游标；当前 worker 状态与控制 revision 单独核对。读取面板不等于发出控制，断点、单步和继续不绕过审批、预算或检查点守卫。

<a id="kun-controls"></a>

## 暂停、断点与 Console

Kun 提供暂停/单步、控制回执、安全检查点、四个安全边界的条件断点和结构化 Console。没有 Go 源码行断点。[调用与快照](KUN-DEBUG.md#kun-inspection) 提供检查视图，Console 的独立诊断入口将固定证据交给另建的模型会话，见 [诊断](KUN-DIAGNOSIS.md)。

<a id="controls-1"></a>

### 设置断点

配置 → Agent 引擎 → 条件断点：设置新运行的默认规则。默认规则为空，不会额外暂停，也不增加模型或工具调用。

正在运行的会话：轨迹 → Kun 高级调试 → Sources → **读取当前断点**，编辑后点击 **应用断点到当前运行**。建议先暂停再编辑，否则运行推进可能使读到的状态版本过期。临时修改只影响当前 run 和它的检查点续跑，不改实例默认配置。界面自动刷新不会覆盖正在编辑的草稿。

每次规则替换都有原有控制协议的幂等回执和状态 revision 校验，规则版本加一，命中计数归零。相同 requestId 重试返回原回执；不同内容复用 ID、过期状态和非活跃运行均拒绝。删除所有规则可关闭后续条件断点；关闭调试工作台不会删除规则。已有暂停仍需明确点击继续，原有“每次模型请求前暂停”开关独立保留。

| 条件 | 语义 |
| --- | --- |
| `phase` | 必填：before_model、after_model、before_tool、after_tool |
| `tool` | 工具调用名精确匹配；MCP 使用实际别名。仅用于工具边界 |
| `model` | 当前有效模型名精确匹配 |
| `minStep` | 模型步数下限；before_model 按将要执行的步骤计数，首次为 1；其余边界按已发起步骤计数 |
| `minToolCalls` | 已派发工具数下限，不包括参数校验拒绝的调用 |
| `minFailures` | 全部工具的连续失败数下限，包括参数拒绝和审批拒绝；成功结果归零，并非同工具/同参数重复失败识别 |
| `minReportedTokens` | 累计已报告 token 下限；缺失的 usage 不推算为已知用量，无费用条件 |
| `once` | 该规则版本只命中一次；计数持久化，恢复保留 |

一条规则的条件全部满足才命中；多条规则任意一条满足即暂停，并记录同时命中的 ID。最多 16 条规则，ID 唯一。阈值 0 不限制。只接受上述类型化字段，未知字段、表达式、脚本和不支持的阶段拒绝。

模型响应后断点也覆盖最终回答：完整响应已存入日志，但 run 还没结束，可查询或补充指令。工具结果后断点发生在结果落盘之后，覆盖 succeeded/failed/rejected/declined；尚未确认结果的在途工具没有“完成后”断点，不提供不安全恢复。

<a id="controls-2"></a>

### 等待、单步与审批

- 同一次安全边界只求值一次。继续后不会在原位置立即重复命中；未设置 once 的规则在后续边界可再次命中。
- 原单步单位仍为一个模型或工具操作；默认在下一操作前暂停。有明确的操作后断点时，可以先在操作后暂停。
- 工具前断点与 MCP 审批依次发生。继续断点之后仍须通过审批；在审批等待时，继续/单步不能批准工具。查询和规则修改也不能批准工具。
- `pauseTimeoutSeconds`：0 一直等待，1–86400 秒到期停止运行，绝不自动继续。期限仅适用于调试暂停，MCP 审批等待不受此设置影响。修改规则不延长已经开始的暂停期限；恢复后新发生的暂停重新计时。
- 调试与审批等待沿用等待时间统计，不扣活动时间预算；暂停仍占用运行并发名额。停止和超时后的恢复仍要通过最近安全检查点检查。

暂停原因、命中规则版本、工具 callId 及可选期限出现在状态 `debug.pause` 和 `kun/run.paused` 事件中。规则和命中计数随状态快照/检查点保存。一次性规则在恢复后不会被当作全新规则重触发。

<a id="controls-3"></a>

### Console

Console 与其余面板共用“固定快照 / 跟随现场”的选择。查询类型为运行状态、上下文、工具目录、预算、模块、断点与控制、动作账本及选定事件证据。查询不会写事件、改变 revision、调用模型或工具，也不会把查询文字塞入目标会话。

上下文查询在 `model.started` 快照返回实际 `model_request` 及原始请求证据；其余快照/现场返回 `state_context`，不暗示状态已经提交给模型。预算值对应返回的状态版本，不是即时计时器；未知 usage 不当作已知零费用。

控制区先展示结构化提案，再执行原 `/kun/control` 协议。提案固定当前 runId、revision 和 requestId。查看历史时，控制仍明确针对当前运行；版本过期后须重新预览，不会自动换目标。只有显式选择“补充指令”并执行，才会向当前运行追加文本。

本地 Console 最多保留 20 条显示记录。清空显示不删除服务器事件。永久回执和命令仍在既有控制事件中。结构化查询不调用模型；自然语言问题通过新的独立诊断会话处理。没有任意代码执行、任意数据库查询或自动工具重放。

<a id="controls-4"></a>

### API

`GET /api/v1/sessions/{sid}/kun/query?kind=run&sequence=0`

- 要求该会话 read 权限；kind 可为 run/context/tools/budget/modules/breakpoints/actions/evidence/diff。evidence 要求正数 sequence；diff 同时要求正数 fromSequence 和 sequence，详细返回结构见本手册的[只读检查接口](#inspect-2)。
- sequence=0 或省略查询当前状态；正数查询同一会话的历史事件快照。负数、非法序号或未知类型拒绝。需要在线 worker；离线时仍可使用既有恢复检查入口打开日志，不自动恢复任务。
- 返回 sessionId、runId、revision、sequence、kind、data；沿用宿主脱敏。

`POST /api/v1/sessions/{sid}/kun/control`，要求 run 权限：

```json
{
  "requestId": "set-debug-rules-0001",
  "runId": "current-run-id",
  "expectedStateRevision": 8,
  "operation": "set_breakpoints",
  "debug": {
    "breakpoints": [
      {"id": "writes", "phase": "before_tool", "tool": "write_file", "once": true},
      {"id": "failures", "phase": "after_tool", "minFailures": 1}
    ],
    "pauseTimeoutSeconds": 300
  }
}
```

Worker 协议 v16 曾扩展 `query` 的 evidence/diff 与 fromSequence；保留 v5 的 set_breakpoints。hello 声明 conditionalBreakpoints/debugQueries/snapshotDiff/eventEvidence。RunDesk 和 Kun 必须同步升级。状态 schema 仍为 1，新增字段可选；旧快照可查，执行恢复须通过引擎和格式守卫；兼容范围见 [Kun 接口](KUN-SEPARATION.md)。

<a id="controls-5"></a>

### 仍待完成

基础调试、独立诊断与 steer 建议审核已接通。更多条件信号、高级上下文来源图、跨后端控制和真实服务/生产验收仍待完成。当前功能与限制见 [状态](STATUS.md)，本版验证见 [验证](VALIDATION.md)。

Sources 的结构化状态、按钮可用性和其余面板见 [结构化面板](#kun-panels)。

<a id="kun-inspection"></a>

## 调用证据与上下文检查

Kun 调试面板提供可读的 Network/Elements 和只读快照比较。这里的差异是两份执行状态的比较，不是文件差异、恢复操作或可执行 JSON Patch。

<a id="inspect-1"></a>

### 界面

- Network 按运行和步骤关联模型、工具请求/结果；MCP 按运行、服务器和 exchangeId 关联。行内显示结果是否已记录；详情给出请求/结果事件序号、耗时、服务报告用量及输出。缺少请求不表示调用已派发，缺少结果不推断成功或失败。工具内部没有上报的网络行为不可见。
- 点击调用默认固定它的结果快照，若无结果则固定请求快照。详情仅显示截至选定序号的证据；“查看请求快照 / 查看结果快照”可明确切换检查时点。列表继续显示后来记录，详情保持固定。部分旧事件被客户端缓存淘汰时，关联可能不完整。
- Elements 左侧为模型请求列表。点击后按实际发送顺序显示 system/user/assistant/tool 消息，内容可展开；消息字符数不是 token 数。工具声明、技能路径/哈希/捕获内容和完整原始 JSON 可展开检查。
- 只有 `model.started` 事件中的 request 称为“实际模型请求”；其他时点明确标为“状态上下文”。Skills 是运行启动时捕获的内容，是否进入某一请求应以系统消息核对，不宣称已有逐次读取/注入来源图。
- 两个面板保留原始证据，默认折叠。自动刷新保留当前展开状态和滚动位置。Sources 仍控制当前 run，历史快照只读。Performance 固定时点不会显示后续完成事件。
- 全局“比较两份快照”：选择一个调用快照，设为起点，再选择另一快照并比较。同一会话可以跨 run 比较，起终点 runId/revision 明示；数组按位置匹配，插入可能产生多项变化。更换起点会使旧的在途比较失效，响应不会覆盖新选择。

事件增量读取每次刷新最多 2 页，随后刷新继续补取；客户端保留最多 20,000 条 Kun 事件，并按 JSON 字符大小估算 64 MiB 限制，超限显示提示。它不是浏览器实际堆内存的硬上限。快照缓存最多 12 份。服务器记录不因客户端缓存淘汰而删除。

<a id="inspect-2"></a>

### 只读 API

沿用 `/api/v1/sessions/{sid}/kun/query`，要求该会话的 `read` 权限与归属校验。需要在线 worker，不自动调用模型、连接 MCP 或恢复任务；无新写入路由。

| 参数 / 类型 | 语义 |
| --- | --- |
| `kind=context&sequence=N` | N 为模型开始事件时返回 `capture=model_request`、request、skills、evidence；其他状态为 `state_context`、messages、skills、toolDefinitions |
| `kind=evidence&sequence=N` | N 必须大于 0；data 为精确持久化 Event，含 sequence/sessionId/runId/revision/type/time/data |
| `kind=diff&fromSequence=A&sequence=B` | A/B 必须大于 0 且在同一会话日志存在；data 为下述差异结果；允许 A=B 或反向比较 |

外层统一返回 sessionId、runId、revision、sequence、kind、data。diff 外层指终点，data.from/to 同时给出两端身份。未知查询、非法/重复序号参数、diff 缺少端点、非 diff 携带非零 fromSequence 返回 400；不存在的序号或 worker 查询拒绝返回 409。跨应用访问仍返回 403。

差异结果的主要字段：

| 字段 | 含义 |
| --- | --- |
| `from` / `to` | sequence、runId、revision |
| `changes[]` | path、change（add/remove/replace）、可选 before/after |
| `path` | JSON Pointer，转义 `/` 和 `~`；超过 512 Unicode 字符时截断并设置 pathTruncated，不能当完整指针使用 |
| `before` / `after` | type、bytes、preview、truncated；缺少一侧表示该值不存在，与 JSON null 有区别 |
| `bytes` | 该值完整 JSON 的 UTF-8 字节数 |
| `preview` | 最多 512 Unicode 字符；截断后不是完整 JSON，可直接作为文本阅读 |
| `truncated` | 顶层表示变化列表/节点遍历未完整；与每个值自己的预览截断独立 |
| `limit` / `nodeLimit` | 最多 256 个变化、20,000 个遍历节点；深度 24 或过长路径改为父值整体比较 |
| `arrayMatch` / `redaction` | position / structured_fields_before_diff |

JSON 数值用 json.Number 保留精度，不经过浮点转换。两份状态先按宿主相同的敏感字段规则脱敏，再比较并制作字符串预览，因此密码等敏感字段变化不会出现在差异里。自由文本不被解析为凭据对象，仍可能包含用户自己写入的内容，语义与原有记录保持一致。

Kun SDK 提供 snapshotDiff/eventEvidence；实际能力以 worker 握手为准。旧快照可读；执行检查点须满足版本守卫，见 [升级](UPGRADING.md)。

<a id="inspect-3"></a>

### 验证边界

核心测试覆盖重启后的精确证据、请求与真实 fixture HTTP body 一致、未来回答不混入旧输入、只读不改变状态/事件/调用次数、大整数、脱敏、缺失/null、路径转义、跨运行、上限与 Unicode。宿主测试用真实 Kun 子进程验证 read/归属权限。

`node scripts/kun-inspect-test.cjs` 验证关联键、快照截止、内容作为文本、实际请求来源、比较忙碌状态和过期响应；它不运行浏览器。浏览器场景及外部 Kun 的实际执行和跳过范围见 [验证记录](VALIDATION.md)，不能从脚本存在推断已经验收。

<a id="kun-panels"></a>

## 结构化检查面板

Kun 结构化面板包括 Network、Elements、Console、Sources、Performance、Application 和 Layers，并支持快照差异。各面板以当前 worker 能力和固定快照为依据；循环策略切换见 [Harness](KUN-HARNESS.md)。

<a id="panels-1"></a>

### Sources：当前运行控制

Sources 以当前在线状态为目标，即使其他面板固定在历史快照，也会明确展示当前 runId、revision、状态和阶段。暂停原因/期限、本次 MCP 审批、续跑来源、断点命中、待应用控制、最近控制记录及动作账本直接可读；原始控制状态折叠保留。

- 正在运行时可请求暂停；重复暂停/停止请求在已有待应用命令时禁用。
- 已在安全边界暂停且没有审批状态时，继续/单步可用。审批等待时须明确允许或拒绝；审批已有决定但尚未处理完时不再允许提交决定。
- 结束或离线时禁用运行控制。控制提交期间禁用 Sources 写入按钮，防止重复点击；服务端原有授权、幂等和 revision 校验仍是最终约束。
- 命令绑定点击时当前 run/revision；审批额外绑定当前 callId。查看历史审批只读，不会将旧审批直接用于当前运行。
- 较旧状态响应不会回退当前控制目标。当前断点读取后状态若已变化，应用按钮禁用并提示重新读取；草稿保留，主动重新读取会替换草稿。
- 编辑断点和检查点恢复分别放在折叠区。恢复仍须先显式检查，再明确提交；检查结果与当前 run/revision 不一致时禁用恢复按钮。

控制记录显示当前运行已加载的最近 50 条；账本显示最多 200 项，并标注完整项数。账本来自 ID 映射，未宣称按执行时间排序。查询与查看不会执行工具，控制不会自动重试或自动更换目标。

<a id="panels-2"></a>

### Performance：用量与耗时

显示状态版本对应的模型调用数、已派发工具数、已报告 token、活动时间、连续失败数及限制。人工等待单列，不扣入活动时间。服务没有报告有效用量时显示缺失计数，费用始终标为未估算。阈值达到不代表当前请求被截断，也不代表已经产生某个未记录的停止原因。

旧快照缺少模块标识时，新增预算字段的默认零值不能视为测量值，因此按未知显示。所有数值源自持久化/在线状态，没有前端模拟计时器，当前活动/等待值可能尚未更新。

模型、工具、MCP 完成记录分表展示，每类最多最近 200 条已加载记录。每行可跳到事件证据。三类耗时可能嵌套，不能累加当作总耗时。查看固定快照时，按 runId、sequence、revision 截止过滤；跟随现场时只展示当前运行。数据不完整时不宣称这些表涵盖服务器全部历史。

<a id="panels-3"></a>

### Application：MCP 目录与审批

服务表显示配置版本、传输、协议、连接状态、目录工具数和最近已加载事件。工具列表展示本轮捕获的别名、描述、审批模式和 Schema，最多展开 200 项，完整目录在折叠原始 JSON 中。

`approve` 表示始终允许，但仍需参数和预算校验；`prompt` 表示逐次询问；本轮 `approvalPolicy=never` 时，需要询问的调用将拒绝。未列出的工具不表示已允许，内置文件写权限也不能代替 MCP 权限。

所选时点的审批单独展示。历史审批只读，“查看当前控制”切换到 Sources，并重新以当前状态展示控制目标。修改 MCP 配置下一轮才生效，当前捕获目录不被 UI 草稿替换。

<a id="panels-4"></a>

### Layers：四模块的记录事实

固定展示 Memory、Planning、Action、Capability 的实现版本、状态结构版本、记录阶段和各自数据：

| 模块 | 当前显示依据 |
| --- | --- |
| Memory | 消息数、请求 JSON 字节数、压缩/截断标记；对应 context.built |
| Planning | Tool Loop 的未请求原因，或 Plan-Act 的规划状态、步骤与显式计划；关联 context.built 或 purpose=plan 的 model.completed |
| Action | 调用 ID、工具、结果和 Schema 哈希；对应 tool.validated 或 tool.completed |
| Capability | 工具数、Schema 方言与 format 处理方式；对应 capability.ready |

不生成健康评分或隐含推理。只在同一运行、截止时点内寻找已加载证据；找不到时明确说明，不把另一次运行的相似事件当作依据。恢复继承的状态可能没有本 run 对应事件，原始状态仍可查看。

<a id="panels-5"></a>

### 共享检查行为

四面板的“证据 #N”进入 Network 并固定该事件快照。模型/工具/MCP 使用关联调用视图，其余控制/上下文/目录事件使用通用事件证据视图。固定选择、只读比较与 Console 沿用同一序号。

非调用列表面板使用完整检查宽度。自动刷新按模块、工具和内容身份恢复展开项；滚动位置保留，避免新增记录将展开状态套到另一条。数据表在窄屏容器内横向滚动。

面板交互与后端数据的本版验证范围见 [验证](VALIDATION.md)，尚待完成的真实服务验收见 [状态](STATUS.md)。
