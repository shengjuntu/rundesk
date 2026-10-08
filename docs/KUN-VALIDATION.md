# Kun 0.6 / RunDesk 0.25.0 验证记录

日期：2026-10-05。Linux amd64，Go 1.25.12。模型/MCP 均为本地 fixture；宿主集成测试构建并运行真实 Kun 子进程。没有连接生产服务。

| 检查 | 结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包约 67 秒 |
| `go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s` | 通过 |
| `go test -race ./internal/app -run '^TestKun(Debug\|Checkpoint)' -count=1 -timeout=90s` | 通过，仅代表所选宿主调试/恢复用例 |
| 元数据更新后 API/OpenAPI/Version 相关回归 | 通过 |
| 最终差异遍历上限专项（`TestInspect`，race） | 通过 |
| `go vet ./...` | 通过 |
| JavaScript/CJS `node --check` | 52 个文件通过 |
| `node scripts/kun-inspect-test.cjs` | 通过；不等同于浏览器验收 |
| Linux amd64 `make build` | RunDesk、Kun 均成功 |
| Windows amd64 交叉构建 | 两个命令成功，未在 Windows 实机运行 |
| OpenAPI 生成 | 134 个路径、161 个操作，含 evidence/diff、fromSequence 和差异结构 |
| Playwright / 浏览器交互与布局 | 未运行；没有 Chromium，可复现脚本已更新 |
| 真实模型、MCP、news2douyin 业务 | 未运行 |

## 本轮专项证据

- worker 关闭再打开，读取 model.started 的 context/evidence，与 fixture 实际收到的 HTTP 请求 body 一致；模型最终回答不会混入原始输入。
- 精确事件与快照身份可追溯；其他时点明确返回 state_context。
- context/evidence/diff 不改变状态、revision 或事件，不增加模型调用，不重新执行工具。
- 差异覆盖同一快照、反向/跨 run 可用语义；跨 session 核心比较拒绝；读取权限允许本人会话，跨应用 403。
- 大整数精度、缺失与 null、数组位置、JSON Pointer 转义、敏感嵌套字段先脱敏、Unicode 预览、路径长度、256 变化及 20,000 节点上限。
- 非法/重复序号、缺少固定端点、未知类型与不存在的序号拒绝；已有控制幂等/过期 revision 行为保留。
- Node 前端逻辑测试：同名调用跨运行/步骤/服务器不串联、所选时点不泄漏未来响应、消息作为字面文本、实际输入优先于状态消息、比较忙碌状态和过期响应。
- 之前的条件断点、MCP 审批独立、预算、检查点失效/恢复、真实 worker 终止及宿主重启用例继续通过。

## 复现命令

```bash
go test ./... -count=1 -timeout=180s
go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s
go test -race ./internal/app -run '^TestKun(Debug|Checkpoint)' -count=1 -timeout=90s
go test ./internal/app -run 'Test.*(OpenAPI|Version|API)' -count=1 -timeout=90s
go test -race ./internal/kun -run TestInspect -count=1 -timeout=60s
go vet ./...
node scripts/kun-inspect-test.cjs
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/kun.exe ./cmd/kun
```

具备 Chromium 时再执行以下浏览器场景。本轮两个 UI JSON 均标记 not_run；新增的 Elements 展开保留、快照差异和关联 MCP 场景尚未通过浏览器实测。

```bash
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-ui-smoke.cjs
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-mcp-ui-smoke.cjs
```

未覆盖完整浏览器交互、长时间/大规模负载、故障磁盘、分布式租约、远端副作用恰好一次与生产服务行为。旧验证记录归档 KUN-VALIDATION-0.5.md / -0.4.md / -0.3.md / -0.2.md。不将旧截图或本地 fixture 结果标作本版生产验收；阶段状态见 KUN-PROGRESS.md。
