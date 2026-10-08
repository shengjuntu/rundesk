# RunDesk 0.27.0 / Kun 0.6 验证记录

日期：2026-10-05。Linux amd64，Go 1.25.12。Kun 引擎与 worker 协议源文件经逐文件比对与 0.26.0 相同；本次新增宿主只读调试服务、MCP 代理及相关查询复用。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包 66.569 秒；包含真实 Kun worker 子进程与本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 本次改动范围 `go test -race` | debugapi、debugmcp、app 调试集成与 CLI 专项全部通过；没有重跑全项目 race |
| JavaScript/CJS `node --check` | 54 个文件通过 |
| `node scripts/kun-inspect-test.cjs` | 通过，包括统一查询 URL、固定差异与过期响应保护 |
| `node scripts/kun-panels-test.cjs` | 通过，保留实际 DevTools 入口的模拟 DOM/API 回归 |
| Linux amd64 `make build` | RunDesk、Kun 构建成功 |
| Windows amd64 交叉构建 | 两个命令成功，PE 文件存在；未在 Windows 实机运行 |
| 已构建 RunDesk CLI 冒烟 | `--version` 为 0.27.0；真实 debug-mcp 进程 initialize/initialized/tools-list/EOF 正常，14 个工具，stdout 仅协议 JSON |
| OpenAPI | 138 个路径、165 个操作；新增普通与 member 调试路由的授权标识检查通过 |
| 浏览器/真实布局 | 未运行，环境无 Chromium；Node 模拟 DOM 不能替代浏览器验收 |
| 真实 Codex、模型、MCP、news2douyin 业务 | 未运行；Codex 新查询使用宿主事件 fixture，未声明原生服务验收 |

## 本次专项覆盖

- Codex capability 明确只支持宿主概况和保留事件；内部上下文、快照、差异等查询返回 unsupported；宿主运行概况不虚构 worker revision/sequence。
- 应用自身 read key 可以查询，另一应用不能读取；成员 viewer 只能读授权项目，不能读同应用其他项目或通过新接口写入。
- 宿主事件逐 ID 分页：固定 through 后排除新事件，包含 delta，空历史 through=0 保持固定；不同会话的 eventId 不可读取。
- 事件按 Unicode 字符分块，完整 JSON 脱敏先于切块；拼接后保留中文、emoji 和 9007199254740993 大整数，结构化密钥不会跨块泄漏。
- 事件列表只加载元数据；原始事件超 8 MiB 在解析前拒绝。HTTP 客户端拒绝超 4 MiB 响应、无效 JSON 和重定向，不泄露错误正文中的凭据。
- Kun 新旧查询入口的 data/revision/sequence 一致，覆盖状态、上下文、工具、预算、模块、断点、账本、证据和差异；固定快照可读，跨应用拒绝。
- 暂停中的纯查询不调用模型、不改变状态 revision；离线 Kun capability/query 不创建运行 handle 或启动 worker。
- MCP 初始化与 initialized 通知、协议协商、ping、工具目录、错误层次；非法 JSON/ID、缺字段、未知工具、错误参数、输入上限均覆盖。
- MCP → HTTP → application gate → DebugService 的串行集成覆盖 Codex 运行、能力不支持和空历史读取；只有固定会话的 GET，无控制/任意 URL/会话切换字段。
- CLI 凭据必须由显式环境变量提供，不继承管理员 RUNDESK_TOKEN，不接受 token 命令行参数；MCP 文本查询不会写入目标对话。

这些检查验证本地实现与边界，不证明远程 Codex 内部提供了相应信息，也不证明外部 MCP 客户端、真实服务或生产压力环境已验收。没有新增运行时依赖。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/debugapi ./internal/debugmcp ./internal/app ./cmd/rundesk \
  -run 'Test(CodexDebug|DebugMember|KunDebug|LifecycleValidation|ProtocolNegotiation|ClientOrigin|StrictSelectors|DebugMCPCLI)' \
  -count=1 -timeout=120s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/kun.exe ./cmd/kun
```

真实浏览器准备好后执行已有 Playwright 脚本，再做真实服务验收；当前 UI JSON 保持 not_run。上一版记录归档为 KUN-VALIDATION-0.26.0.md；阶段边界见 KUN-PROGRESS.md。
