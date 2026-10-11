# Kun 0.14 / RunDesk 0.42.0 验证记录

验证日期：2026-10-09（北京时间，原始日志使用 UTC）。Go 1.25.12；Kun 0.14.0 / worker v14 / fork schema 3。最终八组发布回归全部退出 0，交付代码与回归源码指纹一致。

| 项目 | 已确认结果 |
| --- | --- |
| 全量 Go | 所有包通过，83.472s |
| 静态与竞态 | go vet 全包通过；app/kun/store 专项 race 通过，15.172s，包含产物捕获与评分 |
| 构建 | Linux 两程序构建；CLI、hello/EOF 和协议 13 拒绝通过；Windows 两程序交叉编译通过 |
| 界面 | 83 个 JS/CJS 语法检查、12 组 Node DOM 交互通过 |
| 既有 HTTP | 真实宿主与 worker、本地模型夹具，Hybrid/假设/回复评分通过，6 次模型调用 |
| K4 HTTP | 来源、独立验证、启用/停用及重启；正常对照 8 次、回退对照 20 次、产物拒绝对照 8 次；总计 48 次模型调用 |
| 接口 | 180 paths / 216 operations；本次无新路由；本地引用、参数唯一性、管理员边界、幂等头及可选字段检查通过 |
| 兼容 | 旧任务案例与评分的可选空字段不改变序列化；既有报告指纹测试通过；go.mod/go.sum 不变 |

## 产物规则验收

| 场景 | 结果 |
| --- | --- |
| 四类规则 | 实际动态来源与快速独立验证均保存存在、文本包含、相等和 SHA-256 的结束时判定；每例合计 6 项通过 |
| 规则预先固定 | 任务集/案例预览/Start 摘要绑定；不能事后添加文件规则改写已运行案例 |
| 错误和缺失文件 | 最终回复成功，仍使两例独立验证失败、晋升被拒；动态来源采纳被拒；对照两组各 4 例失败 |
| 历史不变 | 覆盖成功产物、修复失败产物后，评分完整对象不变；宿主重启后原成功评分与报告不变 |
| worker 持久化 | 直接 worker 测试捕获结束事件，改文件后重开 journal，原事件逐项相同 |
| 路径与读取边界 | 上级目录、绝对路径、设备名拒绝；根外符号链接不读取；目录和 FIFO 拒绝；大于 1 MiB/非法文本/证据缺失为未知 |
| 证据范围 | 运行身份、规则摘要或日志完整性不符，不采纳产物判定；不回读当前文件补证据 |
| 任务集界面 | 增加规则、路径本地校验、字面空白/HTML 文本、复制为新草稿、重复提交和迟到响应处理通过 |

规则不保存文件原文，只保存原始字节摘要、可用长度和判定。预存文件也可通过；这不证明本次创建或提供工作区原子快照。以上用本地模型夹具证明机制，不是实际业务质量或优化收益证据。Windows 只有交叉编译结果。

## 原始记录与复验

最终汇总：[validation/kun-0.42.0-final/summary.json](validation/kun-0.42.0-final/summary.json)。同目录保留 go-test、go-vet、go-race、build、windows、syntax、ui、http 八份日志。

- [产物 HTTP 证据](kun-artifacts-http-validation.json)、[K4 HTTP](kun-k4-http-validation.json)、[对照计量](kun-benchmarks-http-validation.json)
- [任务集界面](kun-task-sets-ui-validation.json)、[CLI/协议](kun-cli-validation.json)、[接口契约](kun-contract-validation.json)、[构建汇总](kun-build-validation.json)
- 集成检查保留于 `validation/kun-0.42.0-integration/`；0.41 的顶层证据副本保留于 `validation/kun-0.41.0-evidence/`，原始日志仍在对应版本目录。

复验：`python3 scripts/validate-kun.py --output validation/kun-local-1`；Go 不在 PATH 时指定 `--go /absolute/path/to/go`。需要 Python 3、Node、go.mod 指定 Go 和 race 所需 C 编译器。每次用新目录；HTTP 必须使用同次成功构建。源码指纹覆盖 cmd/internal/scripts、go.mod/go.sum/Makefile，不含文档和生成记录。

最终源码 SHA256：`9b5f8d48439c2be2457733a080e34223e3b8a6f8d8c1a65d1ca08373a48eaf70`。

## 尚未验收与未实现

真实浏览器布局/焦点/移动端、真实 Codex/远程模型/MCP、代表性业务任务集、Windows 执行、生产负载仍待对应环境。目录快照、工作区隔离/重置、文件生成来源证明、结构化文件/事实/外部系统裁判、统计推断和可选学习型 JEV 未实现。Node DOM 与本地 HTTP 夹具不替代这些范围。
