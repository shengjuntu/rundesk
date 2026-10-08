# RunDesk 0.31.0 / Kun 0.7.0 验证记录

日期：2026-10-06。Linux amd64，Go 1.25.12，Chromium Headless Shell 138.0.7204.92。本版实现宿主侧 K3-A 离线记录实验；Kun 引擎保持 0.7.0，worker 协议保持 v7。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全量通过；含真实 Kun 子进程和本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 专项 `go test -race` | experiment、store、app 的 Recording / Experiment 用例通过；未运行全项目 race |
| JavaScript/CJS `node --check` | 60 个文件通过 |
| `kun-inspect-test.cjs` / `kun-panels-test.cjs` | 两组 Node 回归通过 |
| Linux amd64 `make build` | RunDesk 与 Kun 构建成功 |
| Windows amd64 交叉构建 | 两个 PE32+ x86-64 命令构建成功；未在 Windows 实机运行 |
| 已构建 CLI | RunDesk 0.31.0、Kun 0.7.0；Kun hello 为 v7、fork=false；debug-mcp initialize/initialized/tools-list/ping/EOF 正常，19 个工具，stdout 仅 JSON 协议 |
| OpenAPI | 146 个路径、175 个操作；7 个实验操作仅管理员，未投影到成员 API；分页参数无重复 |
| 离线实验 Chromium | 通过；来源初始 3 次模型调用，显式新来源轮次另 1 次；实验操作 0 次模型调用，零页面异常 |
| 诊断建议审核 Chromium 回归 | 通过；4 次诊断模型请求、单独继续来源后的 1 次业务模型请求，零页面异常 |
| Codex 检查 Chromium 回归 | 通过；明确的 Codex 协议 DEMO，零真实 Codex/模型调用 |
| Kun 常规调试 Chromium 回归 | 通过；本地模型 3 次请求，零页面异常 |
| Kun MCP 审批 Chromium 回归 | 通过；本地模型 4 次请求、2 次工具调用、3 次工具发现，零页面异常 |
| 依赖 | `go.mod` / `go.sum` 与 0.30.0 一致 |
| 真实 Codex、远程模型/MCP、news2douyin、生产压力 | 未运行；不声明真实服务或生产环境验收 |

## 核心与存储

- 固定会话、明确轮次和 through 捕获，排除未来记录、其他轮次和其他会话；缺失或不一致的 worker 标识拒绝。保存前结构化脱敏，JSON 大整数保持精度，来源显示标题有界。
- 原始基线与父节点保持不变；多处替代按最早点传播保守失效。后续编辑同时携带上游未验证标志；逐个恢复重算剩余失效范围，全部恢复与根视图差异为零。
- 拒绝其他根比较、非工具事件、固定范围外事件、无变化提交、错误父指纹、无替代值的恢复、超限文本、空理由、过深分支和不支持的操作。
- 单事件超过 4 MiB、总量超过 16 MiB、超过 2,000 条事件均先检查后拒绝。较早合法 through 仍可捕获；恰好 2,000 条可以读取。
- 最大 32 个替代值、两侧均为最大长度且需要 JSON 转义的文本时，差异按 16 条分页，单页仍在 4 MiB 响应上限内；分页可读完整 32 条，未截断替代值。

## HTTP、权限与持久化

- 创建根与子分支复用 HTTP 幂等回执，重试不生成重复分支。严格拒绝未知/重复参数、错误页界限、无效上界及客户端附加 live 等字段；没有实验 execute 路由。
- 所有 7 个实验入口拒绝应用 read/run/approvals key、成员 viewer/runner 和成员代理凭据。
- 创建、编辑、读取和恢复不修改来源 session、事件或 worker 状态，不启动 worker。原文按 Unicode 字符分块。
- 删除来源并重开宿主数据库后，基线、分支、谱系仍可读取，也能继续创建恢复分支；没有从来源重新提取记录。

## 浏览器

- 从 Kun 调试检查器选择明确轮次后创建基线，固定检查器的 through；未选轮次时创建按钮禁用。
- 真实 Kun worker 在本地模型 fixture 驱动下读取长中文文本并写一次证明文件。编辑假设后，来源 revision、宿主事件上界、证明文件及模型调用数保持不变。
- 覆盖替代结果、后续失效、原始 JSON 分块、逐条前后移动、恢复为孙分支、相对父节点差异和三代谱系。
- 延迟基线请求不会覆盖后来选择的子分支；延迟逐条导航请求不会覆盖后来选择的事件。
- 来源显式进入新轮次后，原实验 bundleHash 和 through 保持不变；页面刷新、宿主重启、删除来源后仍能从全局入口查看实验。
- 工具结果和假设文本中的 HTML 按文本显示，零页面异常；390px 布局无横向溢出，桌面与移动截图已检查。

开发过程中修正了 SQLite TEXT 载荷的读取类型、浏览器夹具在 about:blank 访问 localStorage 的问题，以及迟到导航响应和大文本差异页上限。最终流程通过。

机器结果见 `kun-experiment-ui-validation.json`、`kun-diagnostic-ui-validation.json`、`debug-ui-validation.json`、`kun-ui-validation.json`、`kun-mcp-ui-validation.json`；本次截图在 `screenshots/0.31.0/`。浏览器的 4 个来源模型请求不计入实验请求：3 个用于准备原始轨迹，1 个用于验证之后的新轮次不会改变固定实验。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/app ./internal/experiment ./internal/store -run 'Test(Recording|Experiment)' -count=1 -timeout=100s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
# Node 能解析 playwright；CHROMIUM_PATH 指向可执行的 Chromium。
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-experiment-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-diagnostic-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/debug-trace-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-ui-smoke.cjs
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-mcp-ui-smoke.cjs
```

上一版见 [KUN-VALIDATION-0.30.0.md](KUN-VALIDATION-0.30.0.md)。实现范围见 [KUN-EXPERIMENTS.md](KUN-EXPERIMENTS.md)，阶段状态见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。本版不包含完整依赖图、执行结果重算、检查点运行时分叉、Hybrid/Live、实验删除或 K4 优化。
