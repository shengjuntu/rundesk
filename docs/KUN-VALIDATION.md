# RunDesk 0.32.0 / Kun 0.8.0 验证记录

日期：2026-10-08。Linux amd64，Go 1.25.12，worker 协议 v8。**核心与本地协议验证通过；真实浏览器和真实远程服务验收未完成。**

| 检查 | 本次结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 全量通过；含真实 Kun 子进程与本地模型/MCP fixture；最后的 MCP 回放状态显示调整又验证了 Kun/CLI/投影/web 包 |
| `go vet ./...` | 通过 |
| 专项 `go test -race` | kun、app、tracequery 的 Hybrid / ProjectionHybrid 用例通过；未运行全项目 race |
| JavaScript/CJS `node --check` | 63 个文件通过 |
| Node 检查器、面板、Hybrid 对话框 | 三组通过；DOM stub 验证交互和数据语义，不代表浏览器布局 |
| Linux amd64 构建 | RunDesk、Kun 成功 |
| Windows amd64 交叉构建 | 两个 PE32+ x86-64 命令成功；未在 Windows 实机运行 |
| CLI | RunDesk 0.32.0、Kun 0.8.0；hello v8，fork/forkHybrid=true、forkLive=false；debug-mcp 握手/初始化/工具列表/ping/EOF 正常，共 19 个工具 |
| OpenAPI | 150 个路径、180 个操作；5 个 Hybrid 操作仅管理员，不投影到成员 API；参数唯一性通过；修正原任务列表重复查询参数 |
| 真实 HTTP 冒烟 | 构建后的 RunDesk/Kun + 本地模型：来源 2 次调用，Hybrid 新增 2 次；2 个工具回放，来源文件/revision 不变，重复启动不增加模型调用 |
| Hybrid Chromium | 启动时环境拒绝本地套接字，报 Operation not permitted。未执行页面、视觉及 390px 布局验收，无本版成功截图 |
| 既有 5 组 Chromium 回归 | 本次没有重跑成功；保留 0.31 的历史报告，不视为 0.32 验收 |
| 依赖 | go.mod、go.sum 与 0.31.0 一致 |
| 真实 Codex、远程模型/MCP、业务系统、生产压力 | 未运行；不声明真实服务或生产验收 |

## 核心分叉

- 本地模型创建读取/写入录制。捕获后修改原文件，再分叉：模型使用旧录制内容，来源证明文件未再次覆盖，私有工作区未生成文件。
- 参数变化、调用顺序变化、录制耗尽均以 replay_miss 失败。回放完成事件带 executed=false，分支没有真实 tool.started 或 MCP 连接事件。
- 拒绝 epoch/revision/through/run 不一致的选择，拒绝内容篡改、目录/环境/schema 不匹配、真实 MCP 配置注入与模型配置变更。
- 参数对象键序/空白规范化，大整数不丢精度；重复键、非对象、尾随输入与非法数字拒绝。
- 从待处理工具快照分叉，保留模型步骤与报告用量，仅新增后续模型请求；补充指令位于回放工具消息之后。
- 来源完成后关闭 MCP fixture，分支仍能回放完成；来源工具仅调用一次，分支不连接、不审批、不派发。
- 继承已达到的 token 阈值时不发新模型请求；持久化并重开分支执行库后仍禁止普通新轮次。相同 start 请求不重复执行，检查点恢复入口明确拒绝 Hybrid。
- 统一投影把回放展示为 toolReplay 点事件，不伪造真实工具开始/耗时，也不误报缺失开始记录。

## 宿主与界面

实际 worker + HTTP 测试覆盖读取边界、创建/查询预览、指纹核验、启动和幂等；列表/详情不暴露私有上下文或工具正文。同预览改用新 HTTP Key 仍返回原会话/run。普通新轮次拒绝，来源游标/revision 不变。删除目标与删除标记同事务，删除后及宿主重启后均不能重新创建；未启动预览在来源删除、宿主重启后仍可启动。实例配置变化拒绝旧预览且不新增模型调用。

5 个新入口均拒绝应用 read/run/approvals key、成员 runner/viewer 及成员代理入口。重复/未知分页参数与非法页界限拒绝。

kun-hybrid-ui-test.cjs 执行真实前端代码，以可控 HTTP 和 DOM stub 验证：预览不启动运行、固定选择与 expectedHash、编辑使预览失效、重复点击保护、文字不被当作 HTML、迟到响应不覆盖新选择、关闭后迟到启动响应不导航、没有来源控制请求。

kun-hybrid-ui-smoke.cjs 已完成真实 HTTP 前置流程，随后因本地套接字权限无法启动 Chromium。脚本保留完整浏览器场景供兼容环境复跑；报告明确为 passed=false、browserStatus=blocked。Node 测试不能替代视觉、焦点、键盘、移动端和真实浏览器异步行为验收。

开发中修正了空动作账本恢复、空 Skills/MCP 列表经过 JSON 协议后的指纹差异、已删除目标重新创建的边界。全量回归还暴露原有 stderr 测试的竞争：stdout 响应与 stderr 回调不保证顺序；测试现在有界等待实际回调，RPC 生产逻辑不变。最终相关检查通过。

机器记录：kun-build-validation.json、kun-contract-validation.json、kun-cli-validation.json、kun-hybrid-ui-validation.json、kun-hybrid-browser-validation.json。0.31 的历史记录见 [KUN-VALIDATION-0.31.0.md](KUN-VALIDATION-0.31.0.md)，其截图仍在 screenshots/0.31.0/。

## 复现

```bash
go test ./... -count=1 -timeout=180s
go vet ./...
go test -race ./internal/kun ./internal/app ./internal/tracequery -run 'TestHybrid|TestProjectionHybrid' -count=1 -timeout=120s
node scripts/kun-inspect-test.cjs
node scripts/kun-panels-test.cjs
node scripts/kun-hybrid-ui-test.cjs
python3 scripts/generate-openapi.py
make build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/rundesk.exe ./cmd/rundesk
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /tmp/kun.exe ./cmd/kun
# 需要可启动 Chromium 的环境，Node 可解析 playwright。
CHROMIUM_PATH=/absolute/path/to/chromium node scripts/kun-hybrid-ui-smoke.cjs
```

K3-C / Live / 第二种 LoopPolicy / Harness 对比和 K4 尚未实现。没有以录制差异宣称优化收益。
