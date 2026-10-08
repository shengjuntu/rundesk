# Kun 0.2 验证记录

验证日期：2026-10-05。环境：Linux amd64，Go 1.25.12，Chromium Headless 138。所有模型和 MCP 服务使用本地测试桩；stdio 测试实际启动子进程，未调用付费模型或生产 MCP 服务。

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过，包含原有 RunDesk 测试和新增 Kun 测试 |
| Kun、MCP 传输及重点集成路径 `go test -race` | 通过 |
| `go vet ./...` | 通过 |
| 全部浏览器 JavaScript `node --check` | 通过 |
| Linux amd64：RunDesk、Kun 构建 | 通过，并实际启动两个独立进程 |
| Windows amd64：RunDesk、Kun 交叉编译 | 通过；未在 Windows 实机运行 |
| 浏览器流程与移动端布局 | 通过，无页面脚本异常；检查明细见 `kun-ui-validation.json` 和 `kun-mcp-ui-validation.json` |

## 本次 MCP 验证

- HTTP JSON / SSE、会话与版本头、分页工具发现、过滤、原名/别名映射；重复游标在模型调用前失败。
- SSE 字节分片、CRLF、多行数据、EOF、大小限制；服务器 ping / 不支持请求响应；收到匹配结果即结束仍打开的 HTTP 流。
- 真实 stdio 子进程、环境变量隔离、取消并等待进程退出。
- 默认审批真正阻止外部调用；继续不能绕过；错误 callId 拒绝；重复 approve 命令幂等。
- approve / reject / never 策略、`isError` 已知失败；SSE 超时保留未知结果，实际工具调用计数为 1，不重试。
- Kun 配置独立于 Codex、实例隔离、凭据遮盖、导入原子性、陈旧版本拒绝、已禁用服务不解析缺失密钥引用。
- 使用真实 Kun worker：read/run 凭据和其他应用不能审批，增加 approvals scope 后成功。
- 修改配置不改变暂停运行的工具/权限快照；下一轮使用新版本和始终允许策略，没有第二次审批。
- 已知 MCP 凭据被服务回显时，不出现在投影记录和模型结果中；重启清除待审批和连接状态。
- 浏览器操作 MCP 配置、读取列表（工具执行次数为 0）、审批卡片、Network 请求/响应、Elements 工具定义、Application 状态、页面重载和移动视口。两轮合计 4 次模型调用、2 次工具执行、仅 1 次审批。
- 原有 Kun 单步/补充指令流程及 Codex MCP 权限编辑器浏览器回归通过。

## 保留的基础验证

- SSE 参数分片、换行兼容和流结束；不完整响应不能执行工具。
- 模型调用前暂停；单步模型停在工具前，单步工具停在下一模型前。
- 版本冲突、重复命令、重复 run ID 以及不同输入复用 ID。
- 暂停期间补充指令与最终回答期间补充指令，均实际进入下一次模型请求。
- 取消模型请求、暂停任务停止、进程中断后的未知工具结果，不自动重复写入。
- 工作区路径边界、只读配置、记录不包含注入的 API Key 字段。
- 模型请求与对应状态快照、Skills 内容，以及工具执行结果持久化。
- RunDesk 启动真实 Kun 子进程、写入产物、生成回复、Markdown 导出及第二轮控制。
- UI 配置保存、Sources 步进、Elements 快照、Performance 用量、页面刷新后恢复历史、390px 移动视口。

## 复现

```bash
make build
go test ./...
go test -race ./internal/mcp ./internal/mcptest ./internal/kun
go test -race ./internal/app \
  -run 'TestKun|TestMCP'
go vet ./...
```

浏览器验证需要额外安装 Playwright 和 Chromium，仅用于测试，不是运行依赖：

```bash
PLAYWRIGHT_PATH=/absolute/path/to/node_modules/playwright \
CHROMIUM_PATH=/absolute/path/to/chromium \
node scripts/kun-mcp-ui-smoke.cjs
```

脚本在临时目录启动 RunDesk 与本地模型桩，使用回环端口 38731（基础 Kun 脚本使用 38730）；退出时关闭进程并移除临时工作区。生成截图和 `docs/kun-mcp-ui-validation.json`。中文截图需要测试机器安装中文字库。

## 验证限制与已有问题

- 未对真实云模型账号、生产 MCP 服务、全部协议扩展、所有 OpenAI 兼容实现、Windows 运行行为或大规模长期会话做验证。
- 原始 0.19.4 源码的 Skills 目录测试在 Go 1.25.1 下可复现路径问题，使用 Go 1.25.12 后通过；本版提高最低 Go 版本。
- 在 0.20.0 验证时的全量测试一次运行中，原有 `TestLongStderrDoesNotBlockReplies` 在异步 stderr 回调到达前断言失败；单项连续复跑三次及随后全量复跑通过。该既有测试的偶发时序问题未改写；0.21.0 本次全量运行通过。
- 本版尚未实现的能力见 [KUN.md](KUN.md)，不以测试桩通过代替真实提供商兼容性证明。
