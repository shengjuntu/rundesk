# RunDesk 0.7.0 验证记录

验证日期：2026-10-03。构建环境为 Linux、Go 1.27.1；浏览器为 Chromium 154.0.8037.57。当前发布从保存的 0.6.1 包恢复并修改。

## 已通过

| 验证 | 结果与覆盖范围 |
| --- | --- |
| `go test -race ./...` | 全量通过；覆盖配置、会话、API、应用绑定与迁移、技能完整目录、越界路径、符号链接、文件数量/大小限制、备份和作用域 |
| `go vet ./...` | 通过 |
| OpenAPI 校验 | `openapi_spec_validator` 通过；文档 1.2.0，共 48 个路径、57 个操作 |
| 产品界面 | 13 项通过；助手/应用切换、独立 Skills/MCP 页、真实目录上传、ZIP 替换与导出、配置归属、刷新、草稿、应用登记、手机布局 |
| 回复操作 | 18 项通过；复制、评价持久化、分享预览/导出、朗读状态、键盘操作、手机抽屉等 |
| 对话回归 | 流式节点复用、完成状态、嵌套展开、手动折叠、输出滚动、重复 ID、会话隔离与手机宽度通过 |
| 0.6.1 → 0.7.0 | 11 项通过；实际旧/新二进制依次打开同一测试数据目录，保留会话、线程、配置、模型、笔记、历史事件、技能及附属文件，继续旧线程与事件游标 |
| 既有业务客户端 | 6 项通过；news2douyin 0.11.1 原有 Python 接入代码和 rundesk-video-app 0.5.1 原有二进制完成专用配置、来源任务、应用归类及停止流程 |
| 发布构建 | Linux amd64 静态二进制；Windows amd64 交叉编译 |

结构化结果见 `product-validation.json`、`adapters-validation.json`、`upgrade-0.7.0.json` 和 `screenshots/0.7.0/replies/reply-actions-report.json`。界面截图位于 `screenshots/0.7.0/`。

## 验证边界

浏览器、升级和客户端验证使用 RunDesk 明确标识的 Codex 协议模拟器；HTTP、SQLite、文件操作、浏览器交互与客户端代码为实际执行。本轮不验证真实模型回答、新闻搜索、外部 MCP 执行或视频渲染。

news2douyin 测试仅在测试子类中绕过演示模式拒绝检查；生产代码中的检查保持不变。朗读与系统分享使用浏览器 API 桩，验证控制流程，不证明操作系统声音或分享目标可用。长技能描述为布局测试注入的样例。Windows 未做实机运行验证。

## 复现

在项目根目录运行。Go 构建依赖来自 `go.mod`；浏览器测试需要开发环境安装 Playwright/Chromium，不是产品运行依赖。各脚本使用独立临时数据目录并自行启动、停止测试服务；需要允许本机监听端口。

```bash
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildvcs=false -trimpath -o bin/rundesk-windows-amd64.exe ./cmd/rundesk

# 如未使用默认的 Playwright 安装路径，设置 PLAYWRIGHT_PATH；
# 如使用自备 Chromium，设置 CHROME_PATH。
node scripts/product-smoke.cjs
node scripts/conversation-smoke.cjs
node scripts/reply-actions-smoke.cjs

python3 scripts/upgrade-smoke.py --old /path/to/rundesk-0.6.1 --new bin/rundesk
python3 scripts/adapters-integration.py --rundesk bin/rundesk \
  --news /path/to/news2douyin-0.11.1 --video /path/to/rundesk-video-app-0.5.1/bin/video-app
```

客户端联调还需要 news2douyin 自身的 Python 依赖。`scripts/ui-smoke.cjs` 与 `scripts/integration-smoke.cjs` 为旧版界面脚本，包含旧 instance 选择器；0.7.0 产品导航以 `product-smoke.cjs` 为准，不将旧脚本列为本版已通过项。
