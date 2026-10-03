# RunDesk 0.8.1 验证记录

日期：2026-10-03。Linux，Go 1.27.1，Chromium 154.0.8037.57。

| 检查 | 结果 |
| --- | --- |
| 纵向时间轴与独立分析 | 16 项通过，见 `process-validation-0.8.1.json` |
| 现有产品 UI | 13 项通过，见 `product-validation-0.8.1.json` |
| Go 回归 | `go test ./...` 全部通过 |
| 事件投影 | `trace-model-test.cjs`、`process-model-test.cjs` 通过 |
| 静态检查 | 修改的 JS 语法与 `git diff --check` 通过 |
| 构建 | Linux amd64；Windows amd64 交叉编译；版本 0.8.1 |

## 本次重点验证

- 只有一条纵轴；三个 Turn 节点的横坐标相同、纵坐标依次增加；首次进入全部显示概览。
- 成功、空返回、HTTP 403 分别可见；空返回不被误判为账号不存在，返回文本中的 HTML 不执行。
- 跨轮搜索、无匹配恢复、需关注筛选、下一处需关注；筛选隐藏所选轮次时禁用分析提交。
- 原地展开、历史选择与详情刷新恢复、长回复展开、完整输入和来源链接、技能归因说明。
- 独立分析创建失败可重试；仅建立一个新会话；来源事件不变；后续问题留在分析会话；与原任务双向跳转。
- 390px 手机无横向溢出，详情可关闭；桌面及手机真实浏览器截图已经查看。
- 2,001 个工具步骤分页，最后一个步骤可搜索、可看详情，导出包括全量步骤。
- 历史轮次展开期间追加新轮次，选择与筛选不被抢占；显式跳到最新一轮；概览状态刷新保留；Enter 展开和收起。
- 助手/应用归属、模型、技能文件夹和 ZIP、独立 MCP 页面、草稿和应用登记回归通过；无未捕获浏览器 JS 异常。

## 验证边界

浏览器使用真实 RunDesk 服务的演示协议和虚构 Twitter 事件，没有访问 Twitter 或调用真实模型。0.8.0 的真实 Codex 0.159.2 会话级 MCP 发现和调用报告继续作为协议依据，**本次没有重新计为 0.8.1 原生或模型测试**。Windows 没有实机运行。没有重新执行外部业务客户端联调或旧版本迁移测试。

原始测试报告和截图包含在包内。普通对话未发送草稿仍按已有内存机制保存；轨迹分析草稿及轨迹选择保存在当前标签页的 sessionStorage。

## 复现

先构建 `bin/rundesk`，开发环境准备 Playwright/Chromium（产品运行无需 Node）：

```bash
go test ./...
node scripts/trace-model-test.cjs
node scripts/process-model-test.cjs
node scripts/trace-process-smoke.cjs
node scripts/product-smoke.cjs
```

通过 `PLAYWRIGHT_PATH` 和 `CHROME_PATH` 指定测试依赖。历史截图与报告保留对应版本目录。
