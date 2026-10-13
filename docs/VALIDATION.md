# 当前版本验证

RunDesk **0.47.1** / Kun **0.16.0** / worker 协议 **16**。2026-10-12 验证 Docker 完整访问与 MCP 持久授权修复。没有实际 Docker Engine 或在线模型验收。

| 检查 | 结果与范围 |
| --- | --- |
| Go 全量 | `go test ./...` 通过，包括 Kun；[记录](validation/current/go-tests.log) |
| 并发与审批 | MCP 审批及应用运行环境定向 race 通过；[记录](validation/current/go-race.log) |
| 授权界面 | 空对象工具授权无需 JSON，accept 发空对象，真实表单和拒绝保持有效；[记录](validation/current/approval-ui.log) |
| 两端 HTTP | 真实 HTTP 与业务 MCP，旧配置迁移、脱敏凭据保留、其他实例隔离、重复准备、研究和停止通过；[汇总](validation/current/http-summary.json) |
| news2douyin | 279 通过、19 个可选媒体测试跳过；修复后的 Docker 专项 18 通过；5 项 DOM 交互契约通过 |
| 构建与文档 | 两个 Linux 静态程序构建和版本输出通过；OpenAPI 185 条路径及本地文档链接通过 |

[本版汇总](validation/current/summary.json)。HTTP 联调的 Docker、Codex 和搜索为测试替身，没有验证模型是否实际免弹窗；部署时须用一轮连续读取多个来源/offset 的真实研究确认。UI 测试验证处理逻辑，不代表浏览器布局验收。

## 复验

```bash
go test ./...
go test -race ./internal/app -run 'Test(MCP.*Approval|Approval|Permissions|ApplicationRuntime)' -count=1 -timeout=180s
node scripts/mcp-approval-card-test.cjs
python3 scripts/check-docs.py
```

在 news2douyin 0.13.1 源码目录安装开发依赖后，执行 `python tools/integration_docker_research.py --rundesk-root /absolute/path/to/rundesk --output /absolute/path/to/validation`。升级及真实验收见 [升级说明](UPGRADING.md)。
