# 0.49.0 验证记录

本页保留 0.49.0 发布时的验证结果与当时的限制；当前版本及持久线程测试的后续结论见 [当前验证](../../../VALIDATION.md)。

本轮验证日期：2026-10-17。RunDesk **0.49.0** / Kun **0.16.0** / worker 协议 **16**。环境使用 Go 1.25.12、Node 24.19.0、Linux amd64 和 Chromium 155.0.8059.84。交付为源码，未发布容器镜像。

## 代码与构建

- 最终 `go test ./... -count=1 -timeout=300s` 全部通过。
- `go vet ./...` 通过。
- 代理、结构化查询、RPC、存储、Debug API 和 Debug MCP 的 `go test -race` 通过；应用层观测、参数精度、只读权限和成员边界的定向 race 测试通过。
- `CGO_ENABLED=0` 构建 `rundesk` 与 `kun` 通过，版本输出分别为 0.49.0 和 0.16.0。
- 90 个 JavaScript 文件语法检查通过；当前文档链接、OpenAPI 版本和既有权限契约检查通过。

命令、时长与结果见 [checks.json](./checks.json)、[完整 Go 测试](./go-test.log)、[race 采集与查询](./go-race-capture.log)、[race 应用边界](./go-race-observation-app.log)。最后新增的 advertise URL 空 query 分隔符拒绝测试单独通过，见 [定向验证](./advertise-query-validation.log)。

## 采集与查询

本地测试覆盖原始路径/query、请求/响应字节、认证转发与保存副本脱敏、状态码、SSE 流式顺序、取消、不重试、采集截断、队列/内存预算、写入故障与恢复后的丢失标记。WebSocket 覆盖原始帧、双向字节、半关闭后的尾部响应，以及只读解码的掩码、分片、控制帧、压缩标记和不完整数据。

Manager 集成测试使用明确的 App Server RPC 测试替身，贯通线程参数覆盖 → 真实 localhost 直通代理 → SQLite → DebugService；核对自定义/内置 provider、原始 JSON 大整数和中文、认证头、两轮关联、模型/权限参数保留、普通事件订阅清单、关闭与覆盖缺口。此测试重复 50 次通过，覆盖 100 个 provider 子例、200 次 HTTP 交换。测试在开发中修正了将普通事件清单当成原始正文读取的断言，最终使用 `Store.Event` 核对完整记录；没有因此放宽代理完整性判断。

结构化查询测试覆盖会话/轮次/固定事件上界、逐字节分页、UTF-8 边界、模型正文超过通用事件读取上限、WebSocket 消息选择、严格参数校验、只读 MCP 和独立诊断快照。普通轮询/SSE 不再反复传输整份模型正文，但原始数据库证据仍可通过专用查询读取。

## 浏览器与 Kun 回归

- [统一工作台](./unified-browser.log)：实际 Chromium + 明确的 DebugService 测试替身；共用范围、全部来源事件、大整数、HTTP/SSE 字节重组、可读 WebSocket 消息、压缩标记、采集状态、单个 Kun 面板、历史控制保护和移动端布局通过。
- [宿主轨迹](./trace-browser.log)：实际 RunDesk + 协议演示；固定历史、分页、原始 Unicode、过期异步响应、显式刷新、GET 只读和移动端通过。
- [Kun 工作台](./kun-browser.log)：实际 Kun worker + 本地模型 fixture；单步、工具边界、steer、实际上下文、快照差异、模块、用量、Console、断点与检查点恢复、刷新持久化通过。
- [Kun MCP](./kun-mcp-browser.log)：发现不执行、工具审批、请求/结果检查、工具定义快照、下轮持续允许与窄屏通过。
- [Kun Hybrid](./kun-hybrid-browser.log)：显式预览/启动、严格工具回放、原副作用不重复、继承与新增用量、未命中停止和移动端通过。

上述浏览器测试均无页面异常。它们使用本地、可控测试数据，不代表线上模型质量或真实业务验收。

## 真实 Codex CLI 覆盖

执行：

```bash
python3 scripts/codex-observation-smoke.py \
  --codex /opt/codex/versions/0.158.0/bin/codex \
  --include-openai
```

Codex **0.158.0** 使用全新的临时配置目录、合成 API Key 和本地 Responses/SSE/WebSocket 服务。自定义 provider 的基线/线程级 `base_url` 覆盖，以及内置 provider 的基线/线程级 `openai_base_url` 覆盖四项通过。核对原模型、输入、instructions、工具目录、流式字段、Bearer 和自定义请求头保持。完整命令与四项结果见 [CLI 日志](./codex-observation-smoke.log)。

成功测试使用 `ephemeral: true, environments: []`。本环境尝试 `--persistent` 时，未覆盖地址的基线也在 `thread/start` 超时；因此只确认了真实 CLI 的原生配置覆盖和模型传输协议，**没有把它表述为 RunDesk 持久线程的真实模型端到端验收**。

## 尚待目标环境验收

没有使用真实 ChatGPT/OpenAI 账户或真实模型凭据；没有完成 news2douyin 真实研究、Docker 到宿主的观测链路、托管工作区路由、生产负载和真实搜索质量验证。当前版本的保留上限、已完成连接才入库、压缩数据、认证路由和历史缺失边界见 [统一调试与采集](../../../UNIFIED_DEBUGGING.md)。

部署后应使用实际 Codex 版本和认证完成一轮任务，检查会话 `observation` 状态和每条请求的完整性，再核对新闻侧提交回执、搜索证据及报告。工具返回成功与业务目标达成分别确认。
