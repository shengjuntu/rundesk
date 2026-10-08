# RunDesk 0.34.0 / Kun 0.10.0 验证记录

日期：2026-10-08。Linux amd64，Go 1.25.12，worker 协议 v10，fork schema 2。**本地核心与协议验证通过；真实浏览器、真实远程服务和生产环境验收未完成。**

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全量通过；含实际 Kun 子进程、本地模型/MCP HTTP fixture |
| `go vet ./...` | 通过 |
| 专项 race | kun、app、tracequery 的 Live / Hybrid / PlanAct / KunPlanAct / ProjectionHybrid 用例通过；未运行全项目 race |
| JavaScript/CJS 语法 | 65 个文件 `node --check` 通过 |
| Node 交互 | 检查器、面板、Hybrid、Harness、Live 五组通过；使用 DOM stub，不代表浏览器布局 |
| Linux amd64 | `make build` 编译两个命令，CLI 与协议检查通过 |
| Windows amd64 | 两个命令交叉构建成功；未在 Windows 实机运行。首次省略 buildvcs=false 触发宿主 VCS 检测错误，按项目构建参数重试通过 |
| CLI | RunDesk 0.34.0、Kun 0.10.0；hello v10，planAct/harnessComposition/forkHybrid/forkLive=true |
| Debug MCP | initialize / tools/list / ping / EOF 正常；版本 0.34.0，19 个工具；没有调用真实会话服务 |
| OpenAPI | 150 路径 / 180 操作；5 个分叉操作仍仅管理员；mode/Live 详情/confirmLive 契约与参数唯一性通过 |
| 依赖 | go.mod / go.sum 与 0.33.0 一致 |
| 真实浏览器 | 本版未执行；0.32 Chromium 被环境本地套接字权限阻止，不把旧报告当作本版验收，无本版成功截图 |
| 真实 Codex、远程模型/MCP、业务系统、生产压力 | 未运行 |

## Live 专项证据

- 内核使用真实本地模型 HTTP 和文件工具：来源结束后改变文件，Live 读取当前内容而非旧录制，并能执行来源没有录制的新工具参数、实际写入新结果。来源状态指纹保持一致，分支产生 executionMode=live 的工具证据，不产生 replayed。
- 未确认、模式篡改、工作区/配置/上下文版本改变均被拒绝；未确认不会调用模型或创建执行状态。继承 token 预算耗尽时，启动在 MCP 连接、模型请求或写文件前拒绝。
- 本地 MCP HTTP fixture：从来源已 approve 的历史动作分叉后再次出现审批；拒绝不调用工具，批准后真实调用一次。凭据摘要变化在连接前拒绝；重连后的目录漂移在模型、工具和审批前阻止执行。
- Plan-Act 从计划已完成的边界创建 Live，保留计划、模块定义及继承步骤，不重新规划。Hybrid 和检查点原有回归仍通过。
- 实际 RunDesk 管理器 + Kun 子进程 + HTTP：Live 预览列出待处理工具但不泄露历史模型/工具正文或解析后的凭据。未确认返回 400、不创建目标；应用 key 即使具备 read/run/approval scope 并提交确认也返回 403。
- 宿主集成测试在预览后改变项目文件：Live 用待处理原参数重新读写当前文件并新增模型调用，MCP 新连接；同一或不同 Idempotency-Key 重复提交都返回原目标，不增加模型/连接/副作用。来源执行游标与 revision 不变。
- 同一宿主进程只改变 MCP 凭据环境变量、保持配置 revision 不变：启动返回 fork_live_environment_changed，在目标创建和连接前停止。删除目标后，旧预览不能复活该会话。
- Node 用实际前端代码验证：默认 Hybrid；选择 Live 后预览绑定模式，显示工作区/待处理参数并按文本渲染；未勾选确认不能发送 start。改模式使预览失效，重新打开或生成预览清空确认，重复点击保护有效；请求携带相同 expectedHash 和 confirmLive=true。面板与会话横幅显示真实执行，不宣称 Live 工具调用为零。

Live 隔离的是执行记录；测试中的真实写入正说明它会改变共享项目文件。模型与 MCP 服务均为本地可控 fixture，以上不代表远程提供商兼容性、真实业务、任务质量或优化收益。一次预览的重复提交保护不等于跨分支外部副作用“恰好一次”。

## 记录与复现

本版机器记录为 kun-build-validation.json、kun-contract-validation.json、kun-cli-validation.json、kun-harness-ui-validation.json、kun-hybrid-ui-validation.json、kun-live-ui-validation.json。0.33 记录另存为 *-0.33.0.json，历史说明见 [KUN-VALIDATION-0.33.0.md](KUN-VALIDATION-0.33.0.md)。旧浏览器报告保留原版本字段，不视为本次运行。

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/kun ./internal/app ./internal/tracequery -run 'TestLive|TestHybrid|TestPlanAct|TestKunPlanAct|TestProjectionHybrid' -count=1 -timeout=120s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
node scripts/kun-hybrid-ui-test.cjs
node scripts/kun-harness-test.cjs
node scripts/kun-live-ui-test.cjs
python3 scripts/generate-openapi.py
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
```

运行中换模块、跨会话基准及 K4 尚未实现。两个二进制需同时升级，旧引擎的检查点和分叉预览不能跨版本执行。使用与边界见 [KUN-FORKS.md](KUN-FORKS.md)。
