# Kun 0.5 / RunDesk 0.24.0 验证记录

日期：2026-10-05。Linux amd64，Go 1.25.12。所有模型/MCP 使用本地 fixture，宿主集成测试构建并运行真实 Kun 子进程。没有调用生产服务或付费模型。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包约 66 秒 |
| `go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s` | 通过 |
| `go test -race ./internal/app -run '^TestKun(Debug\|Checkpoint)' -count=1 -timeout=90s` | 通过；仅表示所选宿主调试/恢复测试，不是 app 全包竞态验收 |
| `go vet ./...` | 通过 |
| `node --check` | 50 个 JavaScript/CJS 文件通过 |
| Linux amd64 `make build` | RunDesk、Kun 构建成功 |
| Windows amd64 交叉构建 | 两个命令均成功；未在 Windows 实机运行 |
| OpenAPI 生成 | 134 个路径、161 个操作，含查询类型/快照参数、断点策略、暂停状态与新控制操作 |
| 浏览器交互及视觉验收 | **未运行**；Playwright 脚本已增加规则编辑、响应后暂停、Console 查询与控制提案场景 |
| 真实模型/MCP、news2douyin 业务流程 | **未运行**，需要部署侧实际服务 |

## 本次专项验证

- 四个边界的真实循环暂停：模型前无调用、工具前无写入、工具后结果已落盘、最终模型回答后仍可暂停。
- once 规则只命中一次；调用方修改原始配置不会修改已经接收的规则；恢复保留规则版本与命中计数。
- 参数校验失败后暂停，后续工具保持未派发；规则清空不自动解除已有暂停。
- 规则更新复用控制幂等，冲突 ID / 过期 revision 拒绝；原有单步语义继续通过。
- 一秒调试超时停止运行，模型/工具未派发，等待累计而非活动预算消耗。
- 工具前断点继续后仍进入 MCP 审批；继续/单步不能代替批准。
- 七种查询不改变状态、revision 或事件数；历史快照保持原版本，查询不会增加模型调用或污染消息。
- 未知查询、非法/缺失快照、未知条件字段、不支持的阶段及无效阈值拒绝。
- 真实 worker HTTP 集成：read scope 可查自身状态，但不能修改断点；跨应用查询拒绝；有效运行控制及过期提案校验正确。
- 保留上一版 worker 强制终止、宿主重启、最近安全检查点恢复、容量/授权、MCP 重新审批与目录核对测试。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s
go test -race ./internal/app -run '^TestKun(Debug|Checkpoint)' -count=1 -timeout=90s
go vet ./...
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/kun.exe ./cmd/kun
```

浏览器脚本需另配 Playwright / Chromium：

```bash
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-ui-smoke.cjs
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-mcp-ui-smoke.cjs
```

本版两个 UI 验证 JSON 标记 not_run。旧版本记录在 KUN-VALIDATION-0.4.md / -0.3.md / -0.2.md；旧截图与 UI JSON 不能作为本版通过证据。

未覆盖：长时间/大规模负载、故障磁盘、分布式租约、远端副作用恰好一次、生产服务行为和全部界面交互。功能边界见 [KUN-DEBUG.md](KUN-DEBUG.md) 和 [KUN-CHECKPOINTS.md](KUN-CHECKPOINTS.md)，不把本地测试通过等同于 K2 全体验收完成。
