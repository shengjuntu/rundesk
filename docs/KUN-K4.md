# K4 参数化 Skill 与快速路径

RunDesk 0.40.0 / Kun 0.13.0 / worker 协议 13 / fork schema 3。

本版实现 KUN-DESIGN-v0.2 第 15–16 节的 K4 基础闭环：成功案例 → 有来源的候选 → 固定独立验证批次 → 完整评分 → 指定应用启用 → 守卫执行或动态回退。可选学习型 JEV 没有训练或接入；不承诺对任意业务都提高质量或降低成本。

0.41 增加 [多次采样对照](KUN-BENCHMARKS.md)：同一候选固定案例的动态/快速重复执行与描述性报告，使用独立的批次和报告，不替代本节的启用门槛。

## 管理员操作

1. 为目标应用使用专用的本机 Kun 实例，配置模型和工具权限。在“评测任务集”保存来源案例与另一份独立验证集。验证集至少两个案例，输入不能与来源重复。
2. 在来源任务集下点击“读取执行环境”，选择案例、实例、工作区，再“生成案例执行预览”。预览显示完整有效输入，包括项目说明和输出目录，以及模型、权限、MCP 名称和预算。预览不启动会话或模型。
3. 核对后勾选确认并执行。每份预览绑定唯一 session/run；重复提交仍返回同一运行。案例输入不可 steer，不可切换 Harness、续跑或从该案例分叉；删除会话保留墓碑，旧预览不能复活会话。
4. 来源成功并通过固定规则后，点击“从此案例创建 Skill 候选”。填写领域、适用范围、失败条件，以及独立验证集 ID/指纹和输入 Schema。
5. 默认自动提炼成功工具轨迹：与 JSON 输入字段唯一匹配的工具参数替换为引用，其他参数保留为字面量；最终结果来自最后一个工具。不会提取模型最终答案或把失败轨迹中的文字作为规则。也可在工作台提交可审核的受限 IR。
6. 保存候选，再“生成固定验证批次”。候选和批次都不可原位修改。核对所有固定输入和工具范围后勾选确认执行。
7. 运行结束后“汇总并保存验证报告”。缺失、未同步或未结束的记录保留未知；即使动态回退后答对，也不能作为快速路径验证通过。
8. 所有验证案例均通过固定规则且快速路径正常完成，才出现可启用应用。只能选择与来源相同实例和工作区的应用，服务端同时验证应用 revision。启用仅更新指定应用的能力绑定，不修改全局 SKILL.md。工作台提供停用。

并发限额可能让一个批次仅启动部分案例。检查启动返回的每项 `error`；等待已启动案例结束后，可用新的逻辑请求重试同一批次。已启动的固定计划不会再次执行。批次在重启后可从候选的“读取验证批次记录”重新打开。

## 应用调用

应用仍使用原有专用凭据、项目授权和会话归属。创建应用会话后，在普通 turn 的 Input 中显式选择已启用的候选：

```json
{
  "text": "{\"domain\":\"file-copy\",\"path\":\"new-result.txt\",\"content\":\"new evidence\"}",
  "fastSkillId": "服务端返回的候选 ID"
}
```

`fastSkillId` 不授予权限。人类会话、未启用应用、范围改变、非 Kun 后端、诊断、分叉、恢复、附带文件或额外 Skill 的请求不能借此运行候选。原有动态请求省略此字段。已有多轮上下文的运行会在第一个快速动作前回退到动态循环，避免忽略对话历史。

## 受限 IR

没有任意 Go/Python/Shell、远程 Schema 或向后跳转。最多 64 条指令、整个程序最多 256 KiB。输入是带 `domain` 的 JSON 对象；Schema 使用 Kun 既有的有界 2020-12 子集。

| 指令 | 行为 |
| --- | --- |
| `tool` | 解析参数引用或字面量，放入正常 pending 队列；由已有网关校验、审批、记账、派发 |
| `extract` | 从值或工具返回的 JSON 字符串按 JSON Pointer 提取字段，保留工具来源标记 |
| `assert` | 对中间值运行有界 Schema 断言；失败回退 |
| `branch` | 比较值并跳到后续指令；禁止循环和越界 |
| `finish` | 只接受成功工具结果及其派生值；禁止字面量答案或只由输入构造的答案 |

引用形式为 `input#/field` 或先前保存的变量名；参数也可使用 `{"literal":"fixed-value"}`。分支跳过的变量在读取时不可用，会回退。`extract` 不能执行表达式。快速终止事件是 `kun/fast.completed`，不伪装成模型调用，模型调用次数保持真实值。

示例：

```json
[
  {"op":"tool","store":"written","tool":"write_file","args":{"path":{"ref":"input#/path"},"content":{"ref":"input#/content"}}},
  {"op":"tool","store":"read","tool":"read_file","args":{"path":{"ref":"input#/path"}}},
  {"op":"finish","value":{"ref":"read"}}
]
```

## 守卫、账本与回退

候选固定来源的引擎版本、配置/权限摘要、Skill 摘要、MCP 配置摘要、实际工具目录摘要、工作区、项目/实例 revision 和 Harness。每条快速指令前比较守卫；第一条还验证输入 Schema、领域和是否存在旧对话。工作区文件和外部世界不被快照或冻结；服务端未改变版本/目录但改变业务语义时，守卫不能推断该变化，仍依靠实时结果和断言。工具参数仍通过现有 Schema 校验；MCP 审批不会因快路径被跳过；预算照常累积。

`PC`、中间值、来源标记、已完成结果及动作状态持久化在 worker 状态和事件快照。守卫、字段或断言失败产生 `kun/fast.fallback`；保留已完成工具消息、结果和动作账本，再继续动态 Loop，不从第一步重启。

动态循环若请求相同工具和规范化参数的已成功快速副作用（write_file 或 MCP），记为 `reused` 并带 `reusedFrom`，返回已记录结果，绝不再次派发。只读文件工具可再次读取当前内容。改变参数的新动作仍由通常校验/审批决定；这不是对语义相似请求的万能去重。未知工具结果导致原运行停止，不自动重试。快速路径运行不允许 checkpoint resume；中断后需检查账本并显式新建任务，避免不确定副作用重放。

## 独立验证与证据

- 候选只接受 1–10 个已结束、固定规则全部通过且动作成功的动态来源案例。
- 验证任务集在候选保存时固定为完整快照；拒绝来源任务集自身、相同输入以及验证集内部重复输入。JSON 对象字段顺序和空白不会制造不同输入。
- 每个验证案例绑定候选 ID/hash、任务集 ID/hash、完整输入、配置及唯一运行身份。每个评分报告固定宿主日志截止、事件摘要、完整最终回复指纹和每条规则结果。
- 启用门槛是至少两个不同验证输入全部通过，且每例有该候选的 `kun/fast.completed`，没有动态回退。客户端不能直接提交分数绕过门槛。
- 验证是项目内可追溯的保留集验证，不证明输入从未被人或模型看过，也不等于统计显著、第三方盲测、事实真实性或产物语义正确性。文本规则由作者负责设计。
- 配置/工具变化会使运行时守卫回退；停用阻止后续新请求选用，不撤销已派发动作或自动终止在途任务。

## 管理 API

以下均仅管理员可访问，兼容 `/api` 和 `/api/v1`。POST 要求 `Idempotency-Key`；同一逻辑请求重试必须复用，新的逻辑操作使用新 Key。未知字段和查询参数被拒绝。完整结构见 `/api/v1/openapi.json`。

| 路径 | 操作 |
| --- | --- |
| `/kun-case-runs` | POST 固定预览；GET 按 taskSetId 分页 |
| `/kun-case-runs/{cid}` | GET 预览、绑定会话和当前状态 |
| `/kun-case-runs/{cid}/start` | POST expectedHash + confirmExecute |
| `/kun-case-runs/{cid}/score` | GET 单案例固定日志评分，不启动 worker |
| `/kun-skills` | POST 保存候选；GET 元数据分页 |
| `/kun-skills/{kid}` | GET 不可变候选与来源 |
| `/kun-skills/{kid}/validations` | POST expectedHash，生成全案例固定批次 |
| `/kun-skill-validations` | GET 按 candidateId 分页 |
| `/kun-skill-validations/{vid}` | GET 固定批次 |
| `/kun-skill-validations/{vid}/start` | POST expectedHash + confirmExecute，检查每项启动结果 |
| `/kun-skill-validations/{vid}/evaluate` | POST expectedHash，保存完整汇总报告 |
| `/kun-skill-reports/{rid}` | GET 不可变报告 |
| `/kun-skills/{kid}/promote` | POST expectedHash/reportId/reportHash/appId/expectedAppRevision |
| `/kun-skills/{kid}/disable` | POST appId，仅停用该应用的此候选 |

候选提炼可重新打开已完成来源的本地 worker 读取持久化状态，不调用模型或工具。评分只读取有界宿主日志（4096 事件 / 32 MiB），响应不超过 4 MiB。

## 兼容与验收范围

必须一起更新 RunDesk 和 Kun。协议 12 的 worker 不能与协议 13 混用；旧日志可查看，旧引擎预览/检查点不能跨版本执行，需重新生成预览。没有新增运行时依赖。

本地 Go、真实进程/HTTP fixture 和 Node DOM 测试覆盖 K4 基础验收。真实浏览器布局、远程模型/MCP、业务任务集效果、生产压力和 Windows 运行仍需对应环境。具体执行结果以 [当前验证记录](KUN-VALIDATION.md) 为准。
