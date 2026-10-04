# RunDesk 0.8.2 验证记录

日期：2026-10-03。Linux，Go 1.27.1，Chromium 154。

| 验证 | 结果 |
| --- | --- |
| HTTP/运行错误界面 | 14 项通过，`diagnostics-validation-0.8.2.json` |
| 现有产品界面 | 13 项通过，`product-validation-0.8.2.json` |
| 纵向时间轴与分析 | 16 项通过，`process-validation-0.8.2.json` |
| Go 并发回归 | `go test -race ./...` 全部通过 |
| 静态检查 | `go vet ./...`、修改的 JS 语法、`git diff --check` 通过 |
| API 描述 | 生成 OpenAPI 1.3.1；48 路径、57 操作；新增可选 ErrorDetails，引用与字段检查通过 |
| 构建 | Linux amd64 与 Windows amd64；版本 0.8.2 |

## 新增的有效检查

Go 测试覆盖：带 requestId 的错误链/RPC data；密钥字段遮盖及日志关联；日志不记录查询参数；API panic 转换为 JSON 错误而不向界面暴露堆栈；SSE flush 及中途 abort 保持流语义；幂等回放的错误响应保持原内容和原编号。

浏览器检查覆盖：JSON 500；非 JSON/HTML 500 保留为安全文本；空 502；200 但非 JSON；无响应网络错误；大响应截断；错误信息复制和合法 JSON 保留；重复归组及最近请求编号；刷新恢复；设置弹窗内的入口；发送失败输入保留；只在手动重试时发出新请求且沿用幂等 Key；原请求正文不进入诊断；运行错误与实际页面 HTTP 状态区分；事件重复回放不重复计数；SSE 一次断连不重复记多条；轨迹入口；390px 手机与 Escape；清空记录；无未捕获 JS 异常。

现有产品回归涵盖技能文件夹/ZIP 导入、应用和助手配置、消息发送、原地展开的时间轴、2,001 工具步骤以及独立分析会话。

## 测试边界

HTTP 故障通过 Chromium 请求拦截明确注入，运行错误通过虚构的运行事件验证；真实 RunDesk 服务使用演示协议。没有复现用户部署中的那一次 500，也未调用真实模型/provider 来制造错误。Windows 仅交叉编译。剪贴板接口使用可检查的测试桩，不计为 Windows 系统剪贴板验证。

OpenAPI 本次完成生成、JSON/引用检查和后端错误契约测试，未重新运行第三方完整规范校验器。0.8.0 的原生 MCP、0.8.1 的界面历史验证保留在各自版本报告中，不计为新的模型验证。

桌面/手机截图已实际查看。截图中的 requestId、会话和错误均为测试数据。

## 复现

构建 bin/rundesk，并在开发环境准备 Playwright/Chromium：

```bash
go test -race ./...
go vet ./...
node scripts/diagnostics-smoke.cjs
node scripts/product-smoke.cjs
node scripts/trace-process-smoke.cjs
```

使用 PLAYWRIGHT_PATH、CHROME_PATH 指定浏览器测试依赖。产品无需 Node 或 Python。
