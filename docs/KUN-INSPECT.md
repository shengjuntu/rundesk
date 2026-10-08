# Kun 0.6 调用证据、上下文与快照差异

RunDesk 0.25.0 在 K2 调试基础上补充可读的 Network/Elements 和只读比较。这里的差异是两份执行状态的比较，不是文件差异、恢复操作或可执行 JSON Patch。

## 界面

- Network 按运行和步骤关联模型、工具请求/结果；MCP 按运行、服务器和 exchangeId 关联。行内显示结果是否已记录；详情给出请求/结果事件序号、耗时、服务报告用量及输出。缺少请求不表示调用已派发，缺少结果不推断成功或失败。工具内部没有上报的网络行为不可见。
- 点击调用默认固定它的结果快照，若无结果则固定请求快照。详情仅显示截至选定序号的证据；“查看请求快照 / 查看结果快照”可明确切换检查时点。列表继续显示后来记录，详情保持固定。部分旧事件被客户端缓存淘汰时，关联可能不完整。
- Elements 左侧为模型请求列表。点击后按实际发送顺序显示 system/user/assistant/tool 消息，内容可展开；消息字符数不是 token 数。工具声明、技能路径/哈希/捕获内容和完整原始 JSON 可展开检查。
- 只有 `model.started` 事件中的 request 称为“实际模型请求”；其他时点明确标为“状态上下文”。Skills 是运行启动时捕获的内容，是否进入某一请求应以系统消息核对，不宣称已有逐次读取/注入来源图。
- 两个面板保留原始证据，默认折叠。自动刷新保留当前展开状态和滚动位置。Sources 仍控制当前 run，历史快照只读。Performance 固定时点不会显示后续完成事件。
- 全局“比较两份快照”：选择一个调用快照，设为起点，再选择另一快照并比较。同一会话可以跨 run 比较，起终点 runId/revision 明示；数组按位置匹配，插入可能产生多项变化。更换起点会使旧的在途比较失效，响应不会覆盖新选择。

事件增量读取每次刷新最多 2 页，随后刷新继续补取；客户端保留最多 20,000 条 Kun 事件，并按 JSON 字符大小估算 64 MiB 限制，超限显示提示。它不是浏览器实际堆内存的硬上限。快照缓存最多 12 份。服务器记录不因客户端缓存淘汰而删除。

## 只读 API

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

本版升级 worker 协议到 v6、引擎到 0.6.0；hello 增加 snapshotDiff/eventEvidence。状态 schema 保持 1，旧快照可读；旧版本安全检查点不跨引擎版本恢复。两个可执行文件必须同步更新。

## 验证边界

核心测试覆盖重启后的精确证据、请求与真实 fixture HTTP body 一致、未来回答不混入旧输入、只读不改变状态/事件/调用次数、大整数、脱敏、缺失/null、路径转义、跨运行、上限与 Unicode。宿主测试用真实 Kun 子进程验证 read/归属权限。

`node scripts/kun-inspect-test.cjs` 验证关联键、快照截止、内容作为文本、实际请求来源、比较忙碌状态和过期响应；它不运行浏览器。两个 Playwright 场景已更新，当前环境无 Chromium，未运行。详情见 [验证记录](KUN-VALIDATION.md)。
