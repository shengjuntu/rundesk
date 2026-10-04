# RunDesk 0.8.3 验证记录

日期：2026-10-03。Linux，Go 1.27.1；浏览器使用本地 Chromium。

| 验证 | 结果 |
| --- | --- |
| Go 并发回归 | `go test -race ./...` 全部通过；随后新增的回执校验及输入日志故障测试另行 `-race` 通过 |
| 新增故障测试 | 14 个测试函数，覆盖 RPC 5 类故障及应用层 9 类故障/契约；分类测试另含多组 v1/legacy 对照 |
| 错误界面 | 14 项通过，`diagnostics-validation-0.8.3.json` |
| 产品操作 | 13 项通过，`product-validation-0.8.3.json` |
| 轨迹/分析 | 16 项通过，`process-validation-0.8.3.json` |
| 静态检查 | `go vet ./...` 和 `git diff --check` 通过 |
| OpenAPI | 1.3.2，48 路径、57 操作；生成、引用和新增字段检查通过 |
| 构建 | Linux amd64、Windows amd64；Linux 命令显示 RunDesk 0.8.3 |

## 故障复现

`internal/rpc/failure_test.go` 在修改前实际出现 5 项失败：阻塞写超出调用期限、已取消调用仍发送、损坏 JSON 等到超时、长 stderr 堵住回复、exit 7 原因丢失。修复后全部通过。

`internal/app/failure_prevention_test.go` 使用独立 JSONL 子进程，检查初始化失败清理、坏配置连接替换、缺失 turn.id 不滞留 running、后台停止拒绝新任务、输入日志失败不调用 Codex、上游 500 与本地状态区分、数据库读取诊断、不完整回执不重复执行、v1/legacy 分类契约。事件日志写失败用 SQLite trigger 注入；没有更改生产数据库配置。

正常路径仍覆盖并发会话、审批、继续原线程、重复请求去重、文件和技能配置、单轴 Turn 时间线、独立分析会话及大轨迹分页。

## 边界

子进程故障是测试构造；界面 HTTP 故障通过 Chromium 请求拦截注入，运行事件使用虚构测试数据。没有复现用户部署的那次 500，也没有调用真实模型或外部 MCP。Windows 仅交叉编译；剪贴板测试使用可检查的桩。OpenAPI 没有运行第三方完整规范校验器。磁盘满、内核 I/O 卡死和代理错误未穷举。

## 复现命令

```bash
go test -race ./...
go vet ./...
go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk
node scripts/diagnostics-smoke.cjs
node scripts/product-smoke.cjs
node scripts/trace-process-smoke.cjs
```

浏览器测试需开发环境安装 Playwright/Chromium；可以用 PLAYWRIGHT_PATH、CHROME_PATH 指定位置。产品运行无需 Node 或 Python。
