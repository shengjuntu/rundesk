# RunDesk 0.33.0 / Kun 0.9.0 验证记录

日期：2026-10-08。Linux amd64，Go 1.25.12，worker 协议 v9。**本地核心与协议验证通过；真实浏览器、真实远程服务和生产环境验收未完成。**

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全量通过；含实际 Kun 子进程、本地模型/MCP fixture；后补 steer 顺序和同会话换组合测试又通过专项 race |
| `go vet ./...` | 通过 |
| 专项 race | kun、app、tracequery 的 PlanAct / KunPlanAct / Hybrid / ProjectionHybrid 用例通过；未运行全项目 race |
| 最后一次界面投影调整 | web / tracequery 包及 Harness Node 交互再次通过 |
| JavaScript/CJS 语法 | 64 个文件 `node --check` 通过 |
| Node 交互 | 检查器、面板、Hybrid、Harness 四组通过；使用 DOM stub，不代表浏览器布局 |
| Linux amd64 | RunDesk、Kun 编译成功，CLI 和协议检查通过 |
| Windows amd64 | 两个命令交叉构建成功（PE32+ x86-64）；未在 Windows 实机运行 |
| CLI | RunDesk 0.33.0、Kun 0.9.0；hello v9，planAct/harnessComposition/forkHybrid=true、forkLive=false |
| Debug MCP | initialize / tools/list / ping / EOF 正常；版本 0.33.0，19 个工具；没有调用真实会话服务 |
| OpenAPI | 150 路径 / 180 操作；新增 KunHarnessConfig，路径数不变，参数唯一性通过 |
| 依赖 | go.mod / go.sum 与 0.32.0 一致 |
| 真实浏览器 | 本版未执行；上版 Chromium 被环境本地套接字权限阻止，不把旧报告当作本版验收，无本版成功截图 |
| 真实 Codex、远程模型/MCP、业务系统、生产压力 | 未运行 |

## 本批专项证据

- Plan-Act 真实本地模型 HTTP 流程：规划请求不带工具定义且 tool_choice=none；后续执行请求包含辅助计划与工具目录，实际文件工具执行一次，模型共调用 3 次，累计已报告 25 token。
- 规划服务返回工具调用、空白计划或超过 32768 字节的计划时停止；已返回用量进入预算，待派发账本和实际工具调用保持为空。模型次数或 token 阈值到达时不发后续执行请求。
- 参数组合不兼容时拒绝新 start，不改变当前已完成状态；新普通轮次从 Plan-Act 切换 Tool Loop，Harness revision 增加，默认工具循环不多调用规划模型。
- 检查点重开实际执行库后保留已完成计划；改变 LoopPolicy 的恢复请求拒绝。Hybrid 从计划已完成边界启动时不重新规划，保留预算和正确的消息插入顺序。
- steer 在规划后、执行前提交：实际后续 HTTP 请求中，新指令在辅助计划之后，不意外重规划。
- 恢复兼容检查拒绝未知 Harness 版本、未知模块和不兼容的状态 schema。默认 Tool Loop 的检查点和 Hybrid 既有回归仍通过。
- 实际 RunDesk 管理器 + Kun 子进程 + HTTP：保存 Plan-Act 并补齐模块配置，不兼容配置被拒；规划保留在历史快照，不能当作完成回复调用 Reply，也不混入会话 Markdown。已有会话下一普通轮次切换 Tool Loop 后，旧快照逐字保持一致，Harness revision=2。
- Node 使用实际前端代码检查只读固定快照对照、重复点击、重设起点后的迟到响应、缺失用量、计划文本不解释为 HTML、规划证据定位、设置保存完整往返。轨迹视图不会把规划归类为 agentMessage。

模型服务均为本地可控 fixture；以上不是远程提供商兼容性、任务质量、优化收益或真实业务验收。Harness 对照不控制输入条件，不等同独立验证集。

## 记录与复现

本版机器记录为 kun-build-validation.json、kun-contract-validation.json、kun-cli-validation.json、kun-harness-ui-validation.json、kun-hybrid-ui-validation.json。旧 0.32 记录单独归档为相应 *-0.32.0.json，历史说明见 [KUN-VALIDATION-0.32.0.md](KUN-VALIDATION-0.32.0.md)。其余浏览器 JSON 保留原版本字段，不视为本次运行。

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/kun ./internal/app ./internal/tracequery -run 'TestPlanAct|TestKunPlanAct|TestHybrid|TestProjectionHybrid' -count=1 -timeout=120s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
node scripts/kun-hybrid-ui-test.cjs
node scripts/kun-harness-test.cjs
python3 scripts/generate-openapi.py
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
```

Live、运行中换模块、跨会话基准及 K4 尚未实现。默认配置沿用 Tool Loop；两个二进制需同时升级，旧引擎的检查点/分叉包不能跨版本执行。使用与边界见 [KUN-HARNESS.md](KUN-HARNESS.md)。
