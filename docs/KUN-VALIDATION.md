# RunDesk 0.26.0 / Kun 0.6 验证记录

日期：2026-10-05。Linux amd64，Go 1.25.12。Kun 执行引擎及协议代码与 0.25.0 相同，本轮主要变更前端检查界面与产品版本元数据。

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全部通过，app 包约 68 秒；包含真实 Kun 子进程的宿主集成测试 |
| `go vet ./...` | 通过 |
| JavaScript/CJS `node --check` | 54 个文件通过 |
| `node scripts/kun-inspect-test.cjs` | 通过，保留上一版调用关联/实际输入/差异交互回归 |
| `node scripts/kun-panels-test.cjs` | 通过，数据视图和真实 DevTools 入口在模拟 DOM/API 中执行 |
| Linux amd64 `make build` | RunDesk、Kun 构建成功 |
| Windows amd64 交叉构建 | 两个命令成功；未在 Windows 实机运行 |
| OpenAPI 生成 | 134 个路径、161 个操作；本版无新增 API 路由 |
| Playwright 浏览器/布局验收 | 未运行，环境中无 Chromium；两份脚本已更新 |
| 真实模型、MCP、news2douyin 业务 | 未运行，Go 回归使用本地 fixture |

本轮没有重新执行竞态测试；执行引擎、协议与并发后端没有变化。上一版竞态结果可见 KUN-VALIDATION-0.25.0.md，不将它计作本轮测试。

## 前端专项覆盖

- 运行与快照截止过滤，不将其他运行或未来完成记录放入选定时点；离线且没有固定快照时不随意汇总历史运行。
- 有效零用量、缺失/非法用量、输入+输出已报告合计分别处理；旧快照缺少预算模块标识时不把解码默认零当测量值。
- 预算上限、关闭 token 阈值、超额进度条、活动/等待分列；费用未知，嵌套时长没有合计。
- MCP 逐次询问与 never 策略组合明确显示拒绝；工具描述作为文本，不生成 HTML 元素。
- 四模块卡片的证据定位与身份稳定；继承状态没有本 run 证据时不串到另一运行。
- 实际 RunDeskKun.open 初始化、Network/Elements/Layers/Application/Performance/Sources 切换、通用事件跳转和共享固定快照。
- 自动刷新保留模块展开；相同原始状态标题在不同模块中不会碰撞。
- 历史快照固定时 Sources 仍绑定当前 run/revision；纯检查不发送 POST。
- 控制忙碌时重复点击不发送第二次请求；终态/无审批禁用对应按钮；较旧状态响应不能回退当前控制目标。
- 审批时继续禁用，明确允许绑定当前 callId；规则读取后状态变化时禁用旧草稿提交。

模拟 DOM 不验证真实 CSS 布局、可访问性或浏览器事件差异，仍需浏览器验收。Playwright 已更新结构化面板断言、折叠控制区和可用性检查，但 UI JSON 保持 not_run。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o /tmp/kun.exe ./cmd/kun
```

浏览器环境准备好后另行执行：

```bash
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-ui-smoke.cjs
PLAYWRIGHT_PATH=/absolute/node_modules/playwright CHROMIUM_PATH=/absolute/chromium node scripts/kun-mcp-ui-smoke.cjs
```

本轮不声明 K2 全部完成。统一调试服务接入、更多信号、自然语言诊断、生产环境验收和 K3/K4 范围见 KUN-PROGRESS.md。没有新增运行时依赖。
