# RunDesk 0.36.0 / Kun 0.11.0 验证记录

日期：2026-10-08。Linux amd64，Go 1.25.12；Kun 0.11.0、协议 v11、fork schema 2 均未变更。**本地代码、协议、实际 worker/HTTP 与 Node 交互通过；真实浏览器启动被环境权限阻止，远程服务和生产验收未完成。**

| 检查 | 本版结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全量通过；internal/app 75.908s，包含实际 Kun 子进程与本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 专项 race | app/store 的 TestKunCompar*、TestHybridHostLifecycle、TestLiveHostConfirmation*、TestHybridAuthorization* 通过；未运行全项目 race |
| JavaScript/CJS 语法 | 69 文件 node --check 通过 |
| Node 交互 | 检查器、面板、Hybrid、Live、Harness 对照、安全组合切换、新增跨会话对照共 7 组通过；DOM stub，不是布局验收 |
| 独立 HTTP 冒烟 | 实际 RunDesk/Kun，来源/Hybrid/Live 共 6 次本地模型调用；读取两种对照不增加调用，真实派发/回放与用量分离正确 |
| Linux amd64 | RunDesk、Kun 构建通过 |
| Windows amd64 | 两个命令交叉构建通过；未实机运行 |
| CLI | RunDesk 0.36.0、Kun 0.11.0、hello/EOF 正常；worker 能力未改变 |
| OpenAPI | 151 路径 / 181 操作；新 GET 契约、schema 引用、参数唯一性与管理员标记检查通过 |
| 依赖/引擎 | go.mod / go.sum 与 0.35.0 一致；没有修改 internal/kun 或 internal/kunproto |
| 真实浏览器 | 本版已尝试；Chromium process singleton 的 socket() 被环境拒绝（Operation not permitted），没有成功布局截图 |
| 真实 Codex、远程模型/MCP、业务系统、生产压力 | 未执行 |

## 本版专项证据

- 实际 Hybrid 运行后对照：来源真实读取一次，分支仅回放一次，两侧各 2 次新增模型调用及 10 个已报告 token；不返回私有工具正文，查询不增加模型调用。
- 实际 Live 运行后对照：继承模型步骤单列，新增 1 次模型调用及 5 token，两个工具为真实派发，不误计为回放。
- 离线宿主 fixture：模型/worker 路径设为不存在仍可读取，未创建 handle；相同安全点的 Hybrid/Live 可比较，不同模型/基线拒绝。
- 双宿主游标固定，包括尚未启动分支时的 0；分支后来开始、来源后来进入新轮次，重读原范围的结果保持不变。来源删除后不把缺失记录当成零，也不启动恢复。
- 区分缺少 usage、缺失开始事件、缺失/空预算、未完成调用及规划文本；不产生错误的 token/时间差值，不把规划当作回复。
- 会话/轮次/序号冲突拒绝；真实派发、回放、失败、审批/参数拒绝和未见结果分别统计。Unicode 回复预览有界，完整指纹和事件证据保留。
- 原 /api、/api/v1 与成员代理入口对应用/成员凭据默认拒绝；未知/重复/空或负游标参数拒绝。
- Store 在同一只读事务里测量载荷并读取固定范围；跨会话/轮次事件排除，未来超大记录不污染旧范围；4096 条及 8 MiB 单条上限回归通过，总载荷硬限制 32 MiB。
- 最终核对修复“暂停后已恢复”的记录状态显示，并复跑 TestKunCompar* 与 Linux/Windows 宿主构建通过。
- 新前端使用实际发布的 JS，验证显式 GET、重复读取保护、空游标保留、未知/部分用量显示、同源候选过滤、文本不解释为 HTML、选择变化和关闭后的迟到响应不覆盖结果。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/app ./internal/store -run 'TestKunCompar|TestHybridHostLifecycle|TestLiveHostConfirmation|TestHybridAuthorization' -count=1 -timeout=120s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
node scripts/kun-hybrid-ui-test.cjs
node scripts/kun-live-ui-test.cjs
node scripts/kun-harness-test.cjs
node scripts/kun-runtime-harness-test.cjs
node scripts/kun-compare-test.cjs
python3 scripts/generate-openapi.py
make build
# 需要可运行的 Chromium 与 Playwright；CHROMIUM_PATH 可指定已安装浏览器。
node scripts/kun-compare-ui-smoke.cjs
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
```

机器记录：kun-build-validation.json、kun-contract-validation.json、kun-cli-validation.json、kun-compare-ui-validation.json、kun-compare-browser-validation.json。0.35 的当前记录已归档为 *-0.35.0。旧版本浏览器截图/报告保留原版本，不代表本版验收。

本版没有业务质量评分、成本节省结论或 K4 优化收益。功能范围见 [KUN-COMPARISON.md](KUN-COMPARISON.md)，进度见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。
