# RunDesk 0.28.0 / Kun 0.6 验证记录

日期：2026-10-06。Linux amd64，Go 1.25.12，Chromium Headless Shell 138.0.7204.92。Kun 引擎及 worker 协议维持 0.6.0 / v6；本次新增宿主轨迹重建和只读检查页面，修复既有 Kun 页面问题。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包 64.812 秒；包含真实 Kun worker 子进程与本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 本次改动范围 `go test -race` | tracequery、debugapi、debugmcp、app 调试/分析集成、web 嵌入资源专项通过；没有重跑全项目 race |
| 最后一次 trace MCP 目录调整后的针对性回归 | tracequery 与 app 的 Projection / ReadOnlyScope / TraceAnalysis 测试通过 |
| JavaScript/CJS `node --check` | 56 个文件通过 |
| `node scripts/kun-inspect-test.cjs` | 通过，覆盖关联、固定快照、文本安全、上下文、差异与过期响应 |
| `node scripts/kun-panels-test.cjs` | 通过，覆盖面板、实际 DevTools 初始化、控制目标、忙碌保护与过期状态 |
| Linux amd64 `make build` | RunDesk、Kun 构建成功 |
| Windows amd64 交叉构建 | RunDesk、Kun 构建成功；未在 Windows 实机运行 |
| 已构建 RunDesk CLI 冒烟 | `--version` 为 0.28.0；真实 debug-mcp 进程 initialize/initialized/tools-list/ping/EOF 正常，19 个工具，stdout 仅协议 JSON |
| OpenAPI | 138 个路径、165 个操作；新查询、来源、through、筛选参数和重建类型已生成，路由授权测试通过 |
| Codex 检查页真实浏览器 | 通过；真实宿主 + 明确的 Codex 协议 DEMO，零真实 Codex/模型调用 |
| Kun 调试真实浏览器 | 通过；真实 worker + 本地模型 fixture，3 次模型请求，零页面异常 |
| Kun MCP 审批真实浏览器 | 通过；真实 worker + 本地模型/MCP fixture，4 次模型请求、2 次工具调用、3 次工具发现，零页面异常 |
| 真实 Codex、远程模型/MCP、news2douyin 业务、生产压力 | 未运行；不声明真实服务及生产环境验收 |

## 本次专项覆盖

- 原生记录按 run/turn/type/item 关联，晚到的已知 turn 回到原轮次；未知 turn、缺少 item ID 与重复开始保留独立或有歧义的证据。Kun 模型、工具、MCP 和审批采用各自标识，宿主 ID 与 worker sequence 不混用。
- 开始/结束缺失、逆序时钟、未知状态和真实零耗时分别表示；等待中的活动步骤不直接判成失败，终态遗留开始标为未知。步骤时长合计不冒充墙钟时间。
- 固定 through 的多页查询排除后来记录，空历史 through=0 仍固定；step 的可选 runId 必须匹配，证据不能越过 through。离线 Kun 查询保留的宿主轨迹不启动 worker。
- 脱敏先于预览、搜索和分块，UseNumber 保留大整数，数字组成的对象同样受预算限制；字面搜索包含 SQL 样文本不会转为 SQL。单条大载荷在解析前拒绝，取消请求可中止重建。
- 应用 read key 与会话归属、成员 viewer 项目授权继续生效；其他应用/项目、越界事件与 POST 被拒绝。MCP → HTTP → 授权 → 重建的集成没有创建 handle 或改变运行状态。
- 新浏览器检查页验证分页、长中文原始事件分块、HTML 样文本、固定历史、显式刷新、异步筛选过期响应、390px 窄屏及 GET-only 请求；桌面和移动截图已人工检查。
- Kun 浏览器验证单步、补充指令、实际请求上下文、固定快照与差异、四模块、条件断点、Console 提案、检查点恢复、重载持久化及 MCP 审批策略。验证中发现并修复三份未嵌入脚本、首次配置默认值、审批刷新作用域问题。

机器结果见 `debug-ui-validation.json`、`kun-ui-validation.json`、`kun-mcp-ui-validation.json`；截图见 `screenshots/0.28.0/`。浏览器使用 Chromium 与真实 RunDesk/worker，但上游请求由本地服务夹具响应。结构化 Console 已有；跨后端自然语言自动诊断、Codex 内部执行控制仍未实现。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/tracequery ./internal/debugapi ./internal/debugmcp ./internal/app ./internal/web -run 'Test(Projection|ReadOnlyScope|Debug|CodexDebug|KunDebug|TraceAnalysis|StrictSelectors|LifecycleValidation|ProtocolNegotiation|ClientOrigin|HTMLScript)' -count=1 -timeout=180s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
# Node 能解析 playwright；CHROMIUM_PATH 指向可执行的 Chromium。
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/debug-trace-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-mcp-ui-smoke.cjs
```

上一版记录保留为 [KUN-VALIDATION-0.27.0.md](KUN-VALIDATION-0.27.0.md)；本版阶段边界见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。源码包不含构建产物。
