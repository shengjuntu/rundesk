# Kun 0.1 验证记录

验证日期：2026-10-05。环境：Linux amd64，Go 1.25.12，Chromium Headless 138。所有模型调用使用本地 HTTP 测试桩，没有调用付费模型服务。

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过，包含原有 RunDesk 测试和新增 Kun 测试 |
| 新增 Kun / 集成路径 `go test -race` | 通过 |
| `go vet ./...` | 通过 |
| 全部浏览器 JavaScript `node --check` | 通过 |
| Linux amd64：RunDesk、Kun 构建 | 通过，并实际启动两个独立进程 |
| Windows amd64：RunDesk、Kun 交叉编译 | 通过；未在 Windows 实机运行 |
| 浏览器流程与移动端布局 | 通过，无页面脚本异常；检查明细见 `kun-ui-validation.json` |

## 覆盖的行为

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
go test -race ./internal/kun ./internal/adapters/kun ./cmd/kun ./internal/app \
  -run 'TestKun|TestLoop|TestSteerQueued|TestSteerWhile|TestRecoverDispatched|TestCancelPaused|TestToolsStay|TestModelCancellation|TestSSEDecoder|TestJournal'
go vet ./...
```

浏览器验证需要额外安装 Playwright 和 Chromium，仅用于测试，不是运行依赖：

```bash
PLAYWRIGHT_PATH=/absolute/path/to/node_modules/playwright \
CHROMIUM_PATH=/absolute/path/to/chromium \
node scripts/kun-ui-smoke.cjs
```

脚本在临时目录启动 RunDesk 与本地模型桩，使用回环端口 38730；退出时关闭进程并移除临时工作区。生成截图和 `docs/kun-ui-validation.json`。中文截图需要测试机器安装中文字库。

## 验证限制与已有问题

- 未对真实云模型账号、所有 OpenAI 兼容实现、Windows 运行行为或大规模长期会话做验证。
- 原始 0.19.4 源码的 Skills 目录测试在 Go 1.25.1 下可复现路径问题，使用 Go 1.25.12 后通过；本版提高最低 Go 版本。
- 全量测试的一次运行中，原有 `TestLongStderrDoesNotBlockReplies` 在异步 stderr 回调到达前断言失败；单项连续复跑三次及随后全量复跑通过。该既有测试的偶发时序问题未在本次改写。
- 本版尚未实现的能力见 [KUN.md](KUN.md)，不以测试桩通过代替真实提供商兼容性证明。
