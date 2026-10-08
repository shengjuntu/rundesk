# Kun 0.3 / RunDesk 0.22.0 验证记录

日期：2026-10-05。环境：Linux amd64，Go 1.25.12。所有模型和 MCP 调用使用本地测试桩；RunDesk 集成测试构建并启动真实 Kun 子进程。未调用生产服务或付费模型。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过；包含原有 RunDesk 测试、新增 Kun 契约与真实 worker 集成测试 |
| `go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s` | 通过 |
| `go vet ./...` | 通过 |
| 浏览器 JS 和测试脚本 `node --check` | 49 个文件通过 |
| Linux amd64 `make build` | RunDesk、Kun 均构建成功 |
| Windows amd64 交叉构建 | 两个命令均通过；未在 Windows 实机运行 |
| 浏览器交互与视觉验收 | **本次未运行**：环境未安装 Chromium。已更新脚本，包括预算配置、四模块状态、跨面板固定选择与刷新 |
| 真实模型/MCP、news2douyin 业务流程 | **未运行**，需部署侧实际服务 |

## 新增验证

- schema：对象必填/类型/额外字段、字符串长度/正则、精确数值和 multipleOf、数组去重、local ref、组合/条件/依赖、tuple/contains；重复键、不支持关键字/远程引用、极端指数与递归求值限额拒绝。
- 工具预算：一个模型返回多个工具意图时，达到派发数上限后第二个工具未执行，挂起的调用得到取消结果。
- token：实际报告超过阈值或服务未报告用量时，阻止后续工具派发；不把未知用量当零。
- 连续失败与模型调用上限：准确停止，无效参数不增加实际工具调用数。
- 活动时间：取消超时模型请求；调试等待独立累计，不消耗活动时间额度。
- MCP 无效参数：在默认逐次审批策略下，既没有审批事件，也没有 tools/call，模型收到明确 rejected 结果后可结束。
- 模块状态：四模块版本及状态存在于实际模型请求的持久化快照中。
- 保留既有 stdio/HTTP、审批、去重、step/steer、只读/路径边界、未知结果及进程集成测试。

测试中曾发现发现阶段使用短期 context 会过早终止 stdio 进程，已分离“本轮进程生命周期”与“初始化请求期限”，通过现有 stdio 测试及竞态回归。schema 递归求值限额为不可被组合关键字吞掉的拒绝条件。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go test -race ./internal/kun ./internal/mcp ./cmd/kun -count=1 -timeout=90s
go vet ./...
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/kun.exe ./cmd/kun
```

浏览器脚本需要测试环境额外安装 Playwright 和 Chromium：

```bash
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-ui-smoke.cjs
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-mcp-ui-smoke.cjs
```

本版 `kun-ui-validation.json` 和 `kun-mcp-ui-validation.json` 明确为 not_run。上一版记录另存为 `*-0.21.0.json`，旧说明在 `KUN-VALIDATION-0.2.md`，旧截图保留对应版本目录；它们不能作为 0.22.0 的浏览器通过证据。

## 限制

- 自建 schema 校验器支持范围见 `KUN-K1-CORE.md`，不宣称完整 JSON Schema 一致性认证。
- 尚未做大规模/长期运行、完整集成路径竞态或 Windows 实机测试。
- 暂无检查点恢复、记忆摘要、费用预算和运行中权限撤销；测试通过不等于 K1/K2 全量验收完成。
