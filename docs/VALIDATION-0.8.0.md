# RunDesk 0.8.0 验证记录

日期：2026-10-03。Linux，Go 1.27.1，Chromium 154.0.8037.57；真实原生协议验证使用 Codex CLI 0.159.2。

| 验证 | 结果 |
| --- | --- |
| 全量 Go race | `go test -race ./...` 全部通过，含新只读查询与分析会话测试 |
| 静态检查 | `go vet ./...`、JS 语法及 `git diff --check` 通过 |
| 原生 Codex MCP | 7 项通过：会话级配置、运行前重载、5 个工具发现、实际工具调用、快照边界、数据库/长期配置不变、普通线程不继承分析工具 |
| 新过程与分析 UI | 13 项通过：查到/空结果/403、HTML 安全、完整回复、刷新、创建失败重试、独立分析、后续问答、双向跳转、手机布局、2,001 工具步骤、导出、无 JS 异常 |
| 现有产品 UI | 13 项通过：助手/应用归属、模型、完整技能目录与 ZIP、独立 MCP 页、草稿、应用登记和手机布局 |
| 回复交互 | 18 项通过：复制、评价持久化、分享预览、朗读状态、键盘交互及手机会话列表 |
| 对话流式回归 | 节点复用、展开/折叠、输出滚动、会话隔离和手机宽度通过 |
| 事件投影 | 旧轨迹模型与新过程模型测试通过；保留缺失边界、重复回放、拒绝状态、技能归因、空结果、工具错误、输出来源和阶段语义 |
| OpenAPI | 1.3.0 通过标准校验，48 个路径、57 个操作 |
| 构建 | Linux amd64 静态程序；Windows amd64 交叉编译 |

分析会话单元测试验证：创建幂等、新原生线程、原任务事件和运行身份不变、会话级 MCP 配置、只读文件沙箱、快照固定、跨会话引用拒绝、关联信息持久化。查询测试验证 SQLite 拒绝 DELETE、工具拒绝其他会话/未来事件、输入保留、MCP 错误、完整事件分段、字面量查询及未知 SQL 参数拒绝。

原生验证直接启动真实 Codex App Server，完成 initialize、thread/start、config/mcpServer/reload、mcpServerStatus/list 与 mcpServer/tool/call。测试未发送 turn/start，不进行模型推理，也不使用账号凭据。

浏览器中的 Twitter 查询是虚构测试样例，没有访问 Twitter。分析会话发送和独立线程由演示协议验证；本轮不声称验证了真实模型的分析质量、外部社交查询或 Windows 实机运行。系统朗读与原生分享仍使用显式 API 桩。

普通对话的未发送草稿沿用 0.7.0 的内存保存机制，验证了页面间切换保留；不将其计为跨浏览器刷新保存。过程页自身的分析问题草稿保存在当前浏览器标签页的 sessionStorage。

结构化报告：

- `native-trace-validation-0.8.0.json`
- `process-validation-0.8.0.json`
- `product-validation-0.8.0.json`
- `screenshots/0.8.0/replies/reply-actions-report.json`

## 复现

在根目录构建 `bin/rundesk` 后运行。浏览器测试需要开发环境 Playwright/Chromium，产品运行无需 Node。

```bash
go test -race ./...
go vet ./...
node scripts/trace-model-test.cjs
node scripts/process-model-test.cjs
node scripts/trace-process-smoke.cjs
node scripts/product-smoke.cjs
node scripts/reply-actions-smoke.cjs
node scripts/conversation-smoke.cjs
python3 scripts/trace-native-mcp-smoke.py --codex /path/to/codex --binary bin/rundesk
```

可通过 `PLAYWRIGHT_PATH` 和 `CHROME_PATH` 指定测试依赖。旧 `scripts/trace-smoke.cjs` 针对 0.5.x 的时间比例轨道，保留作历史参考，不是本版 GUI 验收入口。0.6.1→0.7.0 迁移和既有业务客户端验证记录仍保存在对应旧版报告，本轮没有把旧版结果计为新的升级或业务联调测试。
