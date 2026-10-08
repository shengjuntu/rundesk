# Kun 0.5 条件断点与结构化 Console

RunDesk 0.24.0 继续推进 K2。在既有单步、控制回执及检查点恢复上，增加四个安全边界的条件断点和无需模型的 Console。它没有 Go 源码行断点，也没有自然语言诊断 Agent。

## 设置断点

配置 → Agent 引擎 → 条件断点：设置新运行的默认规则。默认规则为空，不会额外暂停，也不增加模型或工具调用。

正在运行的会话：Kun DevTools → Sources → **读取当前断点**，编辑后点击 **应用断点到当前运行**。建议先暂停再编辑，否则运行推进可能使读到的状态版本过期。临时修改只影响当前 run 和它的检查点续跑，不改实例默认配置。界面自动刷新不会覆盖正在编辑的草稿。

每次规则替换都有原有控制协议的幂等回执和状态 revision 校验，规则版本加一，命中计数归零。相同 requestId 重试返回原回执；不同内容复用 ID、过期状态和非活跃运行均拒绝。删除所有规则可关闭后续条件断点；关闭 DevTools 窗口不会删除规则。已有暂停仍需明确点击继续，原有“每次模型请求前暂停”开关独立保留。

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

## 等待、单步与审批

- 同一次安全边界只求值一次。继续后不会在原位置立即重复命中；未设置 once 的规则在后续边界可再次命中。
- 原单步单位仍为一个模型或工具操作；默认在下一操作前暂停。有明确的操作后断点时，可以先在操作后暂停。
- 工具前断点与 MCP 审批依次发生。继续断点之后仍须通过审批；在审批等待时，继续/单步不能批准工具。查询和规则修改也不能批准工具。
- `pauseTimeoutSeconds`：0 一直等待，1–86400 秒到期停止运行，绝不自动继续。期限仅适用于调试暂停，MCP 审批等待不受此设置影响。修改规则不延长已经开始的暂停期限；恢复后新发生的暂停重新计时。
- 调试与审批等待沿用等待时间统计，不扣活动时间预算；暂停仍占用运行并发名额。停止和超时后的恢复仍要通过最近安全检查点检查。

暂停原因、命中规则版本、工具 callId 及可选期限出现在状态 `debug.pause` 和 `kun/run.paused` 事件中。规则和命中计数随状态快照/检查点保存。一次性规则在恢复后不会被当作全新规则重触发。

## Console

Console 与其余面板共用“固定快照 / 跟随现场”的选择。查询类型为运行状态、上下文、工具目录、预算、模块、断点与控制、动作账本。查询不会写事件、改变 revision、调用模型或工具，也不会把查询文字塞入目标会话。

上下文查询返回所选状态中的上下文，标记为 `state_context`，不暗示该状态已提交给模型。要检查实际模型请求，使用 Network/Elements 对应的模型开始快照。预算值对应返回的状态版本，不是即时计时器；未知 usage 不当作已知零费用。

控制区先展示结构化提案，再执行原 `/kun/control` 协议。提案固定当前 runId、revision 和 requestId。查看历史时，控制仍明确针对当前运行；版本过期后须重新预览，不会自动换目标。只有显式选择“补充指令”并执行，才会向当前运行追加文本。

本地 Console 最多保留 20 条显示记录。清空显示不删除服务器事件。永久回执和命令仍在既有控制事件中。没有自然语言诊断、任意代码执行、任意数据库查询或自动工具重放。

## API

`GET /api/v1/sessions/{sid}/kun/query?kind=run&sequence=0`

- 要求该会话 read 权限；kind 可为 run/context/tools/budget/modules/breakpoints/actions。
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

Worker 协议 v5 新增 `query` 方法和控制 operation `set_breakpoints`；hello 声明 conditionalBreakpoints/debugQueries。RunDesk 和 Kun 必须同步升级。状态 schema 仍为 1，新增字段可选；旧快照可查，但旧引擎版本的检查点不跨版本恢复。

## 仍待完成

K2 具备基础条件断点、结构化 Console、单步/steer、控制幂等与安全检查点恢复，但不宣称完整 DevTools 验收。自然语言诊断、统一跨后端 DebugService/MCP 调试工具、同参数连续失败信号、上下文容量/费用/模块条件、完整图形化上下文树和浏览器/生产服务验收尚未完成。K3 分叉与可配置模块组合、K4 轨迹编译仍未实现。
