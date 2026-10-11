# Kun 0.12 / RunDesk 0.38.0 验证记录

验证日期：2026-10-08。worker protocol 12、fork schema 3 保持不变。

本次为**部分验证完成的开发交付**。已取得的通过结果如下；全量 Go、vet 和 race 虽已启动，但执行会话失效前未取回最终输出，随后 Go 工具链不可用，因此这些项目记为未确认，不能视为全量回归通过。

| 项目 | 本次结果与范围 |
| --- | --- |
| 专项 Go | `TestKunEvaluation\|TestKunCompar\|TestKunHypothesis` 通过，app 2.082s；发生在最后一次未知终态修正及零游标测试补充之前，不代表最终源码全量通过 |
| 实际 HTTP + worker | 通过；RunDesk 0.38.0、Kun 0.12.0 和本地模型夹具，来源/普通 Hybrid/假设 Hybrid 共 6 次模型调用；规则与评分报告固定、幂等保存/读取不新增模型调用 |
| 界面交互 | 9 组 Node 交互检查通过；包含评分、对照、假设、Hybrid、Live、Harness、运行时切换、inspect 与 panels；不等于真实浏览器验收 |
| 静态契约 | 75 个 JS/CJS 语法检查通过；OpenAPI 157 paths / 189 operations，本地引用、参数唯一性、两项新增 POST 的必填幂等头检查通过；依赖未变化 |
| Linux CLI | 当前宿主/worker 版本、hello/EOF 和旧协议拒绝通过；实际 HTTP 已运行构建产物 |
| Windows | 两个 AMD64 PE 构建产物存在并通过文件头检查；最终编译日志未取回，未做 Windows 运行验收 |
| 全量 Go / vet / race | **未确认**；执行结果未取回，不能沿用上一版本通过记录 |

专项测试覆盖固定规则/范围、完整回复超出显示节选的评分、缺失/被替代/超大回复、不可变报告持久化、幂等与权限。最后增加的未知终态及零游标后端用例须在 Go 工具链恢复后重跑；零游标界面行为已由 Node 检查覆盖。

## 复验命令

在安装项目 `go.mod` 要求的 Go 版本后，于源码根目录运行：

```sh
go test ./... -count=1 -timeout=300s
go vet ./...
go test -race ./internal/app -run 'TestKunEvaluation|TestKunCompar|TestKunHypothesis' -count=1 -timeout=180s
make build
node scripts/kun-evaluation-test.cjs
node scripts/kun-evaluation-http-test.cjs
```

实际 HTTP 脚本使用本地模型夹具，不证明真实远程服务效果。真实浏览器布局/焦点/移动端、真实 Codex/模型/MCP、生产负载、独立验证集和 Windows 运行仍未验收。此前 Chromium 因环境禁止 socket 而无法启动，本次没有重复尝试。

机器可读证据见 `kun-build-validation.json`、`kun-contract-validation.json`、`kun-cli-validation.json`、`kun-evaluation-http-validation.json` 和各 `kun-*-ui-validation.json`。上一版原始结果保留在对应 `*-0.37.0.*` 文件中，不作为本次全量通过证据。
