# RunDesk 0.30.0 / Kun 0.7.0 验证记录

日期：2026-10-06。Linux amd64，Go 1.25.12，Chromium Headless Shell 138.0.7204.92。本版新增宿主侧建议审核、当前版本预览、明确发送与回执关联；Kun 引擎仍为 0.7.0，worker 协议仍为 v7。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过；app 包 68.242 秒，含真实 Kun 子进程和本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 本次改动范围 `go test -race` | app 的 DiagnosticReview / KunDiagnostic、store 的 TraceSnapshot 专项通过；未运行全项目 race |
| 后补成员权限专项 | 管理员原路径与成员代理路径上的 GET/preview/apply 均拒绝成员凭据；测试通过 |
| JavaScript/CJS `node --check` | 58 个文件通过 |
| `kun-inspect-test.cjs` / `kun-panels-test.cjs` | 两组 Node 回归通过 |
| Linux amd64 `make build` | RunDesk 与 Kun 构建成功 |
| Windows amd64 交叉构建 | 两个 PE32+ x86-64 命令构建成功；未在 Windows 实机运行 |
| 已构建 CLI | RunDesk 0.30.0、Kun 0.7.0；Kun hello 返回 v7；debug-mcp initialize/initialized/tools-list/ping/EOF 正常，19 个工具，stdout 仅 JSON 协议 |
| OpenAPI | 140 个路径、168 个操作；3 个审核操作均标记仅管理员，未投影到成员 API |
| 诊断与审核 Chromium 流程 | 通过；4 次诊断模型请求，单独继续来源后 1 次业务模型请求；零页面异常 |
| Codex 检查 Chromium 回归 | 通过；明确的 Codex 协议 DEMO，零真实 Codex/模型调用 |
| Kun 常规调试 Chromium 回归 | 通过；本地模型 3 次请求，零页面异常 |
| Kun MCP 审批 Chromium 回归 | 通过；本地模型 4 次请求、2 次工具调用、3 次工具发现，零页面异常 |
| 依赖 | `go.mod` / `go.sum` 与 0.29.0 一致 |
| 真实 Codex、远程模型/MCP、news2douyin 业务、生产压力 | 未运行；不声明真实服务或生产环境验收 |

## 关键验证

- 建议来自真实 Kun worker 的 `trace_propose` 完成事件。拒绝非建议事件、其他诊断的 reviewId、检查类建议、空文本，以及客户端附加的目标或执行字段。
- 预览记录固定审核文本、来源运行编号与当前 revision。创建预览不改变来源 revision、事件或模型调用数；编辑文本使页面预览失效。
- 另一个控制改变来源 revision 后，旧预览返回 409，不产生排队指令。来源结束或切换新轮次后，旧建议不能重新预览并重定向。
- 模拟 worker 已接收命令而宿主未保存回执，再使用同一个审核记录发送；实际 worker 的命令账本去重，队列仅一条，revision 不重复增加，来源保持暂停。
- 浏览器主动丢弃成功 apply 的 HTTP 响应，通过记录查询恢复为 queued；重试同一 reviewId 不重复排队、不调用来源模型。
- 明确通过常规控制继续来源后，模型输入只出现一次审核后的文本，原建议文本不被发送；后续 applied 回执关联到原始宿主事件。applied 表示文本进入上下文，未断言业务目标达成。
- 宿主重启后仍能读取已导入的回执和历史审核记录；已知回执重试不启动 worker。
- 应用 key 无审核读取/创建/发送权限，成员与成员代理入口同样拒绝。原有应用调试控制权限保持既有边界。
- 浏览器验证审核对话框、文本安全、错误反馈、预览失效、回执链接、刷新持久化和 390px 布局；桌面与移动截图已检查。

浏览器中的预期 409 分别覆盖过期版本和已结束来源，不能把这些拒绝当成执行成功。开发时修正了测试对空队列省略字段及重复状态提示元素的选择假设，最终完整流程通过。

机器结果见 `kun-diagnostic-ui-validation.json`、`debug-ui-validation.json`、`kun-ui-validation.json`、`kun-mcp-ui-validation.json`；本次截图位于 `screenshots/0.30.0/`。所有上游模型/MCP 由本地 fixture 响应，不能代替真实服务验收。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/app ./internal/store -run 'Test(DiagnosticReview|KunDiagnostic|TraceSnapshot)' -count=1 -timeout=100s
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

上一版记录见 [KUN-VALIDATION-0.29.0.md](KUN-VALIDATION-0.29.0.md)，实现范围见 [KUN-DIAGNOSIS.md](KUN-DIAGNOSIS.md)，阶段状态见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。当前只有 Kun steer 建议的审核发送，检查/配置建议与 Codex 原生建议未接入；K2 未宣称完成全部验收。
