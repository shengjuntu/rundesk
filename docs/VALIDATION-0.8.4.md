# RunDesk 0.8.4 验证记录

日期：2026-10-03。Linux，Go 1.27.1；界面使用本地 Chromium。

| 验证 | 结果 |
| --- | --- |
| Go 并发回归 | `go test -race ./... -count=1 -timeout=180s` 全部通过；随后新增的上一轮迟到通知测试另行 `-race` 通过 |
| 新恢复测试 | 10 个函数；包含 11 组原生状态核对分支及错误分类子情况 |
| 恢复界面 | 8 项通过，`recovery-validation-0.8.4.json` |
| 产品配置 | 13 项通过，`product-validation-0.8.4.json` |
| 轨迹与分析 | 16 项通过，`process-validation-0.8.4.json` |
| 错误详情 | 14 项通过，`diagnostics-validation-0.8.4.json` |
| 轨迹模型 | 既有 JS 生命周期、分页、缺失边界、空返回、来源等测试通过 |
| 静态检查 | `go vet ./...`、所有前端 JS 语法、Python 客户端编译及 `git diff --check` 通过 |
| OpenAPI | 1.4.0，50 路径、59 操作；引用、恢复 202 和强制 Idempotency-Key 检查通过 |
| 构建 | Linux amd64 静态程序、Windows amd64；Linux 版本命令输出 RunDesk 0.8.4 |

## 恢复验证范围

真实 Manager / HTTP handler / SQLite 事件日志与独立 JSONL 演示子进程共同验证：

- 原生 failed / interrupted 可核对；active、completed、不同 turnId、未知状态、缺少轮次或确认编号阻止继续。
- 提交前再次核对原生状态；会话变化、实例 revision 变化、过期和已被替换的 plan 拒绝执行。
- 同一会话和线程中产生新的恢复轮次；保留原失败事件、原始业务目标和 rootRunId。
- 有已执行步骤或产物时要求核对；已识别的配置问题要求声明已处理；策略拒绝不放行。
- 旧进程退出后读取持久化演示线程，再恢复同一线程；初始化阶段失败且未提交原生任务时可以重试；thread/read 失败不发起任务。
- 并发重复恢复提交只产生一次恢复；原接收回执可在完成后重放。旧计划不能在任务完成后启动第三轮。
- willRetry 提示保持原 active 状态，进展后清除；旧 turn/completed 和 turn/started 不覆盖新轮次，包含新轮尚无 turnId 的阶段。
- 删除会话清理核对结果。

浏览器使用实际 Go 服务、演示失败任务和产物：预览文件、必要确认、保留草稿、来源轮次跳转、390px 布局、Escape / 焦点恢复。一次提交在服务端返回 202 后故意丢失浏览器响应，第二次请求具有完全相同的 Key 和正文，最终时间线只有原失败轮次与一轮恢复。已完成阻止界面另用显式返回值注入，服务端对应分支由 Go 测试验证。

## 边界

原生 Codex 部分是明确标识的协议演示子进程，不是实际 Provider 的故障复现。本轮没有调用真实模型、执行外部 MCP 业务写入或视频制作；没有重新验证 news2douyin / Video App 二进制。不能据此声称实现跨外部系统 exactly-once、任意崩溃点续跑或无人值守自动恢复。

Windows 只完成交叉编译。OpenAPI 使用生成及内部引用/契约检查，没有运行第三方完整规范校验器。文件预览沿用既有受限内容展示。实际原生状态、授权和配置兼容性需在部署环境中检查；恢复入口在核对失败时阻止提交。

## 复现

```bash
go test -race ./... -count=1 -timeout=180s
go vet ./...
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk
node scripts/recovery-smoke.cjs
node scripts/product-smoke.cjs
node scripts/trace-process-smoke.cjs
node scripts/diagnostics-smoke.cjs
node scripts/trace-model-test.cjs
node scripts/process-model-test.cjs
python3 scripts/generate-openapi.py
```

Playwright/Chromium 仅用于开发验证，可用 PLAYWRIGHT_PATH / CHROME_PATH 指定位置；产品运行不依赖 Node 或 Python。
