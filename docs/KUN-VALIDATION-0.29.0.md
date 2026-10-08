# RunDesk 0.29.0 / Kun 0.7.0 验证记录

日期：2026-10-06。Linux amd64，Go 1.25.12，Chromium Headless Shell 138.0.7204.92。worker 协议升级到 v7；本版实现 Kun 独立诊断会话、固定来源查询和待审核建议。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包 65.645 秒；包含真实 Kun worker 子进程与本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 本次改动范围 `go test -race` | tracequery、kun、app 的诊断/建议/分析专项通过；没有重跑全项目 race |
| 来源快照大小专项 | 后补的 store 测试通过：固定游标排除未来超大事件；纳入超过 8 MiB 的事件时拒绝导出且不发布文件 |
| JavaScript/CJS `node --check` | 57 个文件通过 |
| `node scripts/kun-inspect-test.cjs` | 通过 |
| `node scripts/kun-panels-test.cjs` | 通过 |
| Linux amd64 `make build` | RunDesk、Kun 构建成功 |
| Windows amd64 交叉构建 | RunDesk、Kun 构建成功，PE32+ x86-64；未在 Windows 实机运行 |
| 已构建 CLI 冒烟 | RunDesk 0.29.0、Kun 0.7.0；Kun hello 返回 v7 与 diagnosticSession；debug-mcp initialize/initialized/tools-list/ping/EOF 正常，19 个工具，stdout 仅协议 JSON |
| OpenAPI | 138 个路径、165 个操作；固定 through 与 Kun diagnostic scope 已生成 |
| Kun 独立诊断真实浏览器 | 通过；真实宿主 + worker + 本地模型 fixture，4 次模型请求，零页面异常；最终文本输入限制也已回归 |
| Codex 检查页真实浏览器 | 通过；真实宿主 + 明确的 Codex 协议 DEMO，零真实 Codex/模型调用 |
| Kun 调试真实浏览器 | 通过；真实 worker + 本地模型 fixture，3 次模型请求，零页面异常 |
| Kun MCP 审批真实浏览器 | 通过；真实 worker + 本地模型/MCP fixture，4 次模型请求、2 次工具调用、3 次工具发现，零页面异常 |
| 第三方运行时依赖 | `go.mod` / `go.sum` 与 0.28.0 一致 |
| 真实 Codex、远程模型/MCP、news2douyin 业务、生产压力 | 未运行；不声明真实服务及生产环境验收 |

## 本次专项覆盖

- Kun 诊断使用独立会话和 worker 状态，仅提供六个固定来源查询和一个 `trace_propose` 工具。继承的文件工具、业务 MCP、Skills、项目提示词、任务断点被排除；模型伪造 `write_file` 请求被拒绝且没有输出文件。
- 来源 `sessionId/runId/through` 在诊断的多轮追问及 worker 重启后保持不变；会话不能切换诊断模式或换来源。通过实际 worker 子进程验证诊断时来源仍暂停，revision、事件和用量不变，诊断的模型请求与用量单独记录。
- 提前固定 through 排除随后到达的事件。快照只包含来源会话及范围内事件，查询输出脱敏；模型不能读其他会话或未来记录。导出有事件数、总大小和单条大小上限。
- 建议引用拒绝其他运行、其他会话、未来、缺失或重复的事件；严格参数适配拒绝未知/不适用参数、未知建议类型和嵌套执行字段。引用身份通过不代表模型的诊断结论真实，建议没有自动执行入口。
- Kun 诊断拒绝附件、Skills、检查点恢复；前端关闭附件和技能入口，长文本保持为问题文本。原业务 Kun 的单步、补充指令、条件断点、审批及检查点回归仍通过。
- 浏览器验证明确创建并启动独立诊断、待审核建议卡片、固定范围的证据跳转、刷新持久化、同源追问、HTML 样文本安全和 390px 窄屏；没有向来源发送 turn 请求。桌面和移动截图已检查。
- 创建诊断继续受管理员路由授权限制；应用 key 创建请求被拒绝。through=0 不被偷偷解释为最新，超前游标与后端变更被拒绝。

机器结果见 `kun-diagnostic-ui-validation.json`、`debug-ui-validation.json`、`kun-ui-validation.json`、`kun-mcp-ui-validation.json`；截图见 `screenshots/0.29.0/`。Kun 常规调试浏览器首次运行曾在 Sources 入口等待超时，重跑通过；本次未将该次失败算作通过记录。浏览器上游均为本地服务夹具，不能替代真实服务验收。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/tracequery ./internal/kun ./internal/app -run 'Test(TraceProposal|Diagnostic|KunDiagnostic|TraceAnalysis)' -count=1 -timeout=150s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
# Node 能解析 playwright；CHROMIUM_PATH 指向可执行的 Chromium。
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-diagnostic-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/debug-trace-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-mcp-ui-smoke.cjs
```

上一版记录保留为 [KUN-VALIDATION-0.28.0.md](KUN-VALIDATION-0.28.0.md)。本版使用说明见 [KUN-DIAGNOSIS.md](KUN-DIAGNOSIS.md)，完成度与未实现项见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。Codex 原生分析仍沿用其既有沙箱/工具配置，不能套用 Kun 的七工具独占隔离保证；Codex 内部逐步控制仍未实现。源码包不含构建产物。
