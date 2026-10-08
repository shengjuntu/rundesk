# RunDesk 0.35.0 / Kun 0.11.0 验证记录

日期：2026-10-08。Linux amd64，Go 1.25.12，worker 协议 v11。**本地核心、协议及构建检查通过；真实浏览器、远程服务与生产验收未完成。**

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全量通过；含实际 Kun 子进程、本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 专项 race | kun、app、tracequery 的 RuntimeHarness / KunRuntimeHarness / PlanAct / KunPlanAct / Hybrid / Live / ProjectionHybrid 用例通过；未运行全项目 race |
| JavaScript/CJS 语法 | 66 个文件 `node --check` 通过 |
| Node 交互 | 检查器、面板、Hybrid、Live、Harness 对照、运行组合切换六组通过；DOM stub，不代表浏览器布局 |
| Linux amd64 | RunDesk、Kun 编译通过；最终 OpenAPI 文案更新后重新构建宿主 |
| Windows amd64 | 两个命令交叉构建通过；未在 Windows 实机运行 |
| CLI 与 worker hello | RunDesk 0.35.0、Kun 0.11.0、协议 v11；runtimeHarness / planAct / harnessComposition / forkHybrid / forkLive=true |
| Debug MCP | initialize / tools/list / ping / EOF 正常；版本 0.35.0、19 个工具；没有调用真实会话服务 |
| OpenAPI | 150 路径 / 180 操作；set_harness、reason、runtimeHarness 契约和参数唯一性通过；普通与成员路由均声明 set_harness 仅管理员 |
| 依赖 | go.mod / go.sum 与 0.34.0 一致 |
| 真实浏览器 | 本版未执行；先前 Chromium 启动被环境套接字权限阻止，没有本版成功截图 |
| 真实 Codex、远程模型/MCP、业务系统、生产压力 | 未执行 |

## 本批专项证据

- 真实内核与本地模型 HTTP：普通 Tool Loop 在 before_model 暂停时切换 Plan-Act，保持暂停且模型调用数为零，原始配置、上下文与预算保持不变，Harness revision 加 1；单步后才产生一次禁用工具的规划调用。
- 切出 Plan-Act 后，下一请求不再注入旧计划；再显式切入会重新规划，原有模型次数和 token 不退款。以前的固定快照逐字保持一致。
- 取消/关闭并重开真实执行数据库，使用原始配置可恢复切换后的 Harness 与已完成计划；改变原始启动配置则拒绝。恢复不会重复规划；Hybrid/Live 分叉也保留该有效组合和计划。
- 新普通轮次清除 runtimeHarness，回到实例默认 Tool Loop，并记录正确的新 Harness revision。分叉和诊断会话不能通过 set_harness 更换固定组合。
- 不安全阶段、待处理工具、已有审批决定、排队 steer、prepared/unknown 动作、未知模块版本及耗尽模型/token 预算被拒绝，状态不变；缺失/超长原因、不兼容配对和夹带 steer 等无效命令同样拒绝。
- 同 requestId/同命令重试返回原回执，不再变更 revision；改内容冲突，旧 revision 拒绝。注入数据库提交失败，验证迁移不会保留在内存、不会留下成功回执，执行停止。
- 实际 RunDesk 管理器 + Kun 子进程 + HTTP：应用 key 即使有 read/run/approvals，也在 v1 和旧路由上被拒绝；管理员切换后宿主仍显示暂停，实例默认值不改。实际 cancel → checkpoint → resume 保留有效组合；切换证据进入宿主事件投影，下一普通轮次恢复默认。
- Node 使用实际前端代码验证原因必填、只读预览、文本不解释为 HTML、状态和目标编辑使提案失效、历史只读、不安全状态禁用、响应丢失时保留原命令身份、重复点击保护、不自动继续，以及选择变化/关闭后的迟到结果不建立可执行提案。

HTTP 模型/MCP 均为可控本地 fixture；以上不是远程服务兼容性、真实业务效果或优化收益验收。运行中切换只覆盖两套内置组合，不代表任意插件热替换或通用状态迁移。

## 记录与复现

本版机器记录为 kun-build-validation.json、kun-contract-validation.json、kun-cli-validation.json，以及四份 kun-*-ui-validation.json：harness、hybrid、live、runtime-harness。检查器和面板测试输出在命令结果中。0.34 记录归档为 *-0.34.0.json，说明见 [KUN-VALIDATION-0.34.0.md](KUN-VALIDATION-0.34.0.md)；旧浏览器报告保留原版本字段，不代表本版验收。

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/kun ./internal/app ./internal/tracequery -run 'TestRuntimeHarness|TestKunRuntimeHarness|TestPlanAct|TestKunPlanAct|TestHybrid|TestLive|TestProjectionHybrid' -count=1 -timeout=120s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
node scripts/kun-hybrid-ui-test.cjs
node scripts/kun-live-ui-test.cjs
node scripts/kun-harness-test.cjs
node scripts/kun-runtime-harness-test.cjs
python3 scripts/generate-openapi.py
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
```

更多模块迁移、跨会话基准与 K4 尚未实现。升级时同时更新两个程序；旧检查点/分叉预览不能跨版本执行。使用说明见 [KUN-HARNESS.md](KUN-HARNESS.md)，阶段进度见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。
