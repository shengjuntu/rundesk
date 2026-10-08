# Kun 0.4 / RunDesk 0.23.0 验证记录

日期：2026-10-05。环境：Linux amd64、Go 1.25.12。模型和 MCP 使用本地 fixture；宿主测试构建并启动真实 Kun 子进程，没有调用生产模型或付费服务。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包约 65 秒 |
| `go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s` | 通过 |
| `go test -race ./internal/app -run '^TestKunCheckpoint' -count=1 -timeout=90s` | 通过，覆盖本次宿主恢复路径；不表示完整 app 包竞态验收 |
| `go vet ./...` | 通过 |
| JavaScript / CJS `node --check` | 49 个文件通过；更新恢复场景后再次检查对应脚本通过 |
| Linux amd64 `make build` | RunDesk、Kun 均成功 |
| Windows amd64 交叉构建 | 两个命令均成功，未在 Windows 实机运行 |
| OpenAPI 生成 | 132 个路径、159 个操作；包含恢复请求、返回结构、权限及幂等 Header |
| 浏览器交互与视觉验收 | **未运行**，当前环境没有 Chromium；恢复场景已补入 Playwright 脚本 |
| 真实模型/MCP、news2douyin 业务流程 | **未运行**，需要部署侧实际服务 |

## 新增恢复验证

- 一个模型返回两个写文件调用，在第一个完成后停机；重开引擎并恢复，只执行第二个。人为修改的第一个文件保留，不被重放覆盖。
- 恢复保留模型调用次数、已报告 token、工具调用数及来源；重复 start 不执行第二次，来源检查点已消费。
- 过期 epoch、配置预算变化、工作区变化拒绝；预算耗尽、模型在途、未知工具结果、未应用 steer、旧记录缺少恢复清单时不提供恢复。
- MCP 重连之后仍停在审批；模拟批准已经落盘但尚未派发就崩溃的窗口，旧一次性批准不会复用。
- 工具目录与已保存目录不同，在任何新模型/工具调用前失败。
- 强制终止真实 Kun 子进程，再关闭并重新创建 RunDesk Manager；检查重开 worker 不调用模型，恢复创建新 run，显式继续后模型只调用一次。
- HTTP 拒绝缺失幂等 Key、过期检查点、容量不足及重复消费；相同 Key 返回原始接收回执。
- 应用 read scope 可检查自身会话，但不能恢复；跨应用检查/恢复均拒绝。
- 原有 K1 参数校验、预算、step/steer、MCP、路径权限、会话和任务测试继续通过。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s
go test -race ./internal/app -run '^TestKunCheckpoint' -count=1 -timeout=90s
go vet ./...
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/kun.exe ./cmd/kun
```

浏览器脚本需 Playwright 和 Chromium：

```bash
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-ui-smoke.cjs
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-mcp-ui-smoke.cjs
```

本版两个 UI 验证 JSON 明确为 not_run。旧记录 `KUN-VALIDATION-0.3.md`、`KUN-VALIDATION-0.2.md` 及 0.21.0 截图/JSON 仅说明对应旧版，不作为本版浏览器证据。

测试没有覆盖分布式租约、故障磁盘、远端副作用恰好一次、全部崩溃指令窗口、大规模长时间运行或生产集成。恢复覆盖和限制以 [KUN-CHECKPOINTS.md](KUN-CHECKPOINTS.md) 为准；K1/K2 尚未整体验收完成。
