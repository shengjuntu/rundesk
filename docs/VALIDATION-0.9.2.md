# 0.9.2 重建验证

2026-10-04，从已恢复的 0.8.4 源码连续重建 0.9.0、0.9.1、0.9.2。

## 后端与构建

- `go test -race ./... -count=1 -timeout=180s` 通过；运行摘要位于 validation-0.9.2/go-test-race.txt。
- 增补的跨实例调度和定时任务归属/回执读取测试也以 race 通过（additional-tests.txt）。
- `go vet ./...`、OpenAPI 生成、Python SDK 语法检查、JavaScript 语法检查通过。
- 新增应用凭据测试：自动绑定、跨应用/项目拒绝、旧 API 同样受限、大小写字段别名规范化、只存哈希、只读权限、凭据回执隔离、管理员 Cookie 不提权、撤销/过期、审批权限和 SSE 撤销断开。
- 原有恢复测试遇到进程退出后的短暂 session_busy：只对只读核对的明确 busy 响应做最多一秒等待，不重试模型执行，不放宽服务端保护。
- Linux amd64 二进制实际运行；Windows amd64 交叉编译，未在 Windows 实机运行。二进制校验值见包根目录 SHA256SUMS。

## 浏览器

最新版 72 项 Chromium 检查通过，报告和桌面/390px 截图随包提供：

| 检查 | 项数 | 报告 |
|---|---:|---|
| 队列提交、暂停、取消、并发、刷新 | 7 | queue-validation-0.9.2.json |
| Cron、时区预览、编辑、停用、删除 | 7 | schedule-validation-0.9.2.json |
| 应用凭据创建、单次显示、权限、撤销 | 7 | credentials-validation-0.9.2.json |
| 通用助手、应用、完整 Skills、MCP、草稿 | 13 | product-validation-0.9.2.json |
| 失败恢复、接收响应丢失、原生重试 | 8 | recovery-validation-0.9.2.json |
| 轨迹、空结果、失败、关联分析和分页 | 16 | process-validation-0.9.2.json |
| HTTP 错误详情、脱敏、复制、SSE 故障 | 14 | diagnostics-validation-0.9.2.json |

轨迹与过程纯模型 JavaScript 测试也通过。凭据截图隐藏完整秘密。

## 验证边界

使用本项目的 Codex App Server JSONL 演示进程；未调用真实付费模型、外部搜索/MCP、news2douyin/video app 的生产服务或执行外部业务写入。此版提供接入能力，实际应用联调需在部署环境完成。应用 API 授权不等于完整多用户或 OS 隔离。历史验证文档保留供追溯，不代表本次重新实测历史原生版本。
