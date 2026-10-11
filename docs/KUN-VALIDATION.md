# Kun 0.13 / RunDesk 0.40.0 验证记录

验证日期：2026-10-08。Go 1.25.12；Kun 0.13.0 / worker v13 / fork schema 3。修复后发布回归全部取得退出码 0；交付源码与该次回归的源码指纹一致。

| 项目 | 已确认结果 |
| --- | --- |
| 全量 Go | `go test ./... -count=1 -timeout=300s` 通过；整组 87.131s，app 79.484s |
| 静态检查 | `go vet ./...` 通过 |
| 专项 race | app + kun：案例、Skill、快速路径、评分、对照、假设、任务集；14.486s，通过 |
| Linux 构建与 CLI | 两程序构建；版本、worker hello/EOF、协议 12 拒绝、fastPath/fastGuards/fastFallback 能力通过 |
| Windows | 两个 amd64 程序交叉编译通过；未执行 Windows 二进制 |
| JavaScript / UI | 81 个 JS/CJS 语法检查、11 组 Node 交互检查通过 |
| 既有真实 HTTP | 宿主 + worker + 本地模型；原任务集、Hybrid、假设、评分链路通过，6 次模型调用 |
| K4 真实 HTTP | 固定案例 → 自动参数化候选 → 两个独立验证案例 → 指定应用启用 → 宿主重启 → 新输入执行 → 停用通过；来源 2 次模型调用，验证和应用新输入共 0 次额外模型调用；实际检查了输出文件和可读取回复 |
| API 契约 | 173 paths / 208 operations；新增 16 个 K4 操作，引用、参数唯一性、管理员边界、必需幂等头、Input.fastSkillId 检查通过 |
| 依赖 / 源码 | go.mod/go.sum 未变；验证源码 SHA256 `6ae1eaf0773491430a0f1f12eeac870b1b605c2dc0bcdc9d97811c0185bc2c65` |

## K4 验收对照

| 设计要求 | 本次证据 |
| --- | --- |
| 候选有来源、适用范围和失败条件 | `TestKunSkillEndToEndIndependentGateAndApplicationScope`；来源必须动态完成、文本规则全通过、动作成功；输入与事件指纹固定 |
| 独立验证达标才启用 | 同测试 + `kun-k4-http-test.cjs`；拒绝来源输入重叠、未知报告和过期应用 revision；两例通过并有快路径完成证据才启用；成员/应用管理越权拒绝 |
| 受限 IR，不能缓存答案 | `TestFastPathNoModelAndFreshToolEvidence`、`TestFastExtractionBranchAndEvidenceGuard`、`TestFastGuardsAndIRRejectUnsafePrograms`；真实工具结果、字段提取、Schema、前向分支；拒绝任意代码、后向跳转、常量终止和远程 Schema |
| 权限与审批不被优化绕过 | `TestFastMCPStillRequiresDurableApproval`；快路径停在审批前，resume 不能授权，明确批准后才派发 |
| 偏离正确回退，不重放已完成副作用 | `TestFastFallbackKeepsLedgerAndDeduplicatesWrite`；中间断言失败保留工具结果/PC，动态模型请求同参数新 call ID 时记录 reused，实际工具计数仍为 1 |
| 固定案例唯一执行与持久化 | 并发五次 start 仅一个 run，两次来源模型调用；普通 turn 修改输入拒绝；删除后不复活；预览配置变化拒绝且不创建 worker；预览与报告可跨重启读取 |
| 可操作与可追溯 | `kun-k4-ui-test.cjs`；明确确认、重复点击抑制、关闭后忽略旧响应、未知报告不提供启用、应用过滤、revision、停用；HTTP 检查实际嵌入资源 |

这些证据满足本版 K4 基础闭环的本地验收，不表示任意业务质量、统计显著提升或真实远程服务已验收。

## 原始记录

- [最终汇总与源码指纹](validation/kun-0.40.0-final/summary.json)
- [全量 Go](validation/kun-0.40.0-final/go-test.log)、[vet](validation/kun-0.40.0-final/go-vet.log)、[race](validation/kun-0.40.0-final/go-race.log)
- [Linux 构建](validation/kun-0.40.0-final/build.log)、[Windows 交叉编译](validation/kun-0.40.0-final/windows.log)
- [语法](validation/kun-0.40.0-final/syntax.log)、[11 组界面交互](validation/kun-0.40.0-final/ui.log)、[两套实际 HTTP](validation/kun-0.40.0-final/http.log)
- [K4 UI](kun-k4-ui-validation.json)、[K4 HTTP](kun-k4-http-validation.json)、[CLI](kun-cli-validation.json)、[契约](kun-contract-validation.json)、[构建](kun-build-validation.json)

首轮记录保留在 `validation/kun-0.40.0/`，状态为 failed：静态嵌入漏列两个新脚本；一条测试错误地按 JSON 原始字节比较幂等回执。已修复嵌入清单并改为结构内容比较，然后完整重跑。首轮失败不作为发布通过证据。此前版本证据继续保留，未重新标记为本版结果。

## 复验

```sh
python3 scripts/validate-kun.py --output validation/kun-local-1
```

需要 Python 3、Node、go.mod 指定 Go 和 race 所需 C 编译器；Go 不在 PATH 时使用 `--go /absolute/path/to/go`。每次必须用新目录。8 组检查包含 11 组 UI、2 个 HTTP 流程。HTTP 只使用同次成功构建。日志直接保存，JSON 原子记录状态；中断或超时不计通过。

源码指纹覆盖 cmd/internal/scripts、go.mod/go.sum/Makefile，不含文档与生成报告。完整源码包不含构建产物。另提供代码与验收包，省略历史截图，其余源码/文档/日志保留。

## 实际边界

学习型 JEV 为可选扩展，本版没有训练或接入。真实浏览器布局/焦点/移动端、真实 Codex/远程模型/MCP、业务任务集效果、多次采样统计、Windows 执行和生产负载尚未验收。Node DOM 与本地模型夹具不能代替这些环境。
