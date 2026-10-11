# RunDesk 0.37.0 / Kun 0.12.0 验证记录

日期：2026-10-08。Linux amd64，Go 1.25.12；worker v12，fork schema 3。**本地内核、协议、实际 worker/HTTP、界面交互语义及构建通过；真实浏览器布局、远程服务和生产验收未完成。**

| 检查 | 本次结果 |
| --- | --- |
| 全量 `go test ./... -count=1 -timeout=180s` | 全部通过；app 包 78.507s，包含实际 Kun 子进程、本地模型/MCP fixture |
| `go vet ./...` | 通过 |
| 专项 race | kun、kunproto、app：TestHypothesis*、TestHybrid*、TestKunHypothesis*、TestKunCompar* 通过；未运行全项目 race |
| JavaScript/CJS 语法 | 72 文件 `node --check` 通过 |
| Node 交互 | 检查器、面板、Hybrid、Live、Harness 对照、安全组合切换、跨会话对照、假设编辑共 8 组通过；DOM stub，不是浏览器布局验收 |
| 独立 HTTP 冒烟 | 实际 RunDesk + Kun + 本地模型：来源/原录制 Hybrid/假设 Hybrid 共 6 次调用；1 次请求观察到假设 tool 文本及显式说明；原文件删除后仍回放，读取对照不增加调用 |
| Linux amd64 | RunDesk、Kun 构建通过 |
| Windows amd64 | RunDesk、Kun 交叉构建通过；未实机运行 |
| CLI / worker | RunDesk 0.37.0、Kun 0.12.0、hello/EOF 正常；forkHypothesis=true；旧 v11 请求被拒绝 |
| OpenAPI | 153 路径 / 183 操作；schema 引用、参数唯一性、两个新接口的管理员标记、POST 幂等 Key 和列表精简契约通过 |
| 依赖 | go.mod/go.sum 无变化 |

## 新增证据

- `internal/kunproto/hypothesis_test.go`：父 bundle、录制与原/新输出指纹绑定，Live/位置/篡改拒绝，UTF-8 和 64 KiB 上限、理由约束，明确空字符串及同值替换拒绝。
- `internal/kun/fork_test.go`：假设和空输出进入分支上下文，完成事件带假设指纹、原/新输出指纹和 executed=false；匹配失败不使用假设，未重复源写入，也没有在分支工作区写入或连接 MCP；篡改叠加项即使重新计算 bundle hash 仍被拒绝。
- `internal/app/kun_hypothesis_test.go`：摘要脱敏/截断和大整数保留；预览无执行；幂等响应身份、字段/指纹拒绝；原 tape 不变、原失败状态保留；列表不返回假设正文/理由；对照已使用/未使用/未知及证据；真实 worker 在来源删除、宿主重启后消费假设，重复启动不重复调用，删除目标后不复活。
- `internal/app/kun_fork_test.go`：应用、成员 runner/viewer、旧/新 API 和成员代理拒绝读取录制及写入假设。
- `scripts/kun-hypothesis-test.cjs`：显式摘要读取、父指纹核对、原节选不进入替换框、UTF-8 字节限制、切换结果清空输入、空输出、重复请求保护、关闭/异步过期响应忽略、Live/叠加编辑禁止和字面文本渲染。
- `scripts/kun-hybrid-ui-test.cjs`：集成编辑器保存后切换到子预览；不自动启动；显式启动提交子 ID 和新 hash。
- `scripts/kun-hypothesis-http-test.cjs`：实际 HTTP 与 worker 完整闭环、同基线对照、固定范围重复读取、服务端嵌入新界面资源和新契约。

## 未验收和历史记录

0.36 的 Chromium 启动被 `process singleton socket() failed: Operation not permitted` 阻止，原证据保存在 `kun-compare-browser-validation-0.36.0.json`。本轮没有再次尝试绕过此限制，也没有执行真实浏览器：新编辑器的布局、焦点、移动端和浏览器兼容性仍待验收。Node DOM 与实际 HTTP 测试不能替代这些检查。

没有使用真实 Codex、远程模型账号、真实远程 MCP、Windows 实机或生产负载；没有独立任务质量评分、费用测算和优化收益结论。历史验证文件保留原版本与原结果，不代表新版本已完成同类验收。

当前机器报告：`kun-build-validation.json`、`kun-contract-validation.json`、`kun-cli-validation.json`、`kun-hypothesis-ui-validation.json`、`kun-hypothesis-http-validation.json` 及对应既有 Node suite 报告。0.36 当前报告已复制到带 `-0.36.0` 的文件供追溯。
