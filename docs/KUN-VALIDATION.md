# Kun 0.15 / RunDesk 0.43.0 验证记录

验证日期：2026-10-09（北京时间，原始日志使用 UTC）。Go 1.25.12；Kun 0.15.0 / worker v15 / fork schema 3。最终八组发布回归全部退出 0，交付代码与回归源码指纹一致。

| 项目 | 已确认结果 |
| --- | --- |
| 全量 Go | 所有包通过，86.028s |
| 静态与竞态 | go vet 全包通过；app/kun/store 专项 race 通过，15.988s，包含固定文件、产物与评分 |
| 构建 | Linux 两程序构建；CLI、hello/EOF、workspaceFixtures 能力和协议 14 拒绝通过；Windows 两程序交叉编译通过 |
| 界面 | 84 个 JS/CJS 语法检查、12 组 Node DOM 交互通过 |
| 既有 HTTP | 真实宿主与 worker、本地模型夹具，Hybrid/假设/回复评分通过，6 次模型调用 |
| K4 HTTP | 原来源/验证/应用流程及产物拒绝回归通过，共 48 次模型调用 |
| 独立目录 HTTP | 来源、两例快速验证、正常和回退各 8 次对照、停止/重启、空目录与拒绝覆盖；共 24 次模型调用 |
| 接口 | 180 paths / 216 operations；无新路由；引用解析、参数唯一性、管理员边界、幂等头及可选字段检查通过 |
| 兼容 | 旧任务案例、预览、守卫和评分的可选空字段保留序列化；既有报告指纹测试通过；go.mod/go.sum 不变 |

## 独立评测目录验收

| 场景 | 结果 |
| --- | --- |
| 预览与初始化 | 预览无文件操作/模型调用；来源首读为固定输入，实际目录与开始事件/案例预览一致 |
| 原项目边界 | 原有 state.txt 和 project-only.txt 保持原样；后者不会复制到新目录；根外路径读取拒绝 |
| 每例新输入 | 正常配对 8 个运行使用不同目录，每次首读均为该案例固定内容，两组各 4/4 通过 |
| 快速模式守卫 | 两例独立验证无新增模型调用；混合目录模式拒绝；通过的隔离候选不能晋升共享应用 |
| 停止与重启 | 停止后第二例不创建目录；第一例完成后改其输出，重启读取评分不变；显式继续跳过已完成身份且不重置文件 |
| 动态回退 | 4 个快速样本均触发断言回退并通过；回退留在同一目录与产物范围，动态组同样 4/4 通过 |
| 项目旧文件不能冒充产物 | 空目录案例回复声称 ORIGINAL，但不能读取项目旧文件；缺少预期产物导致 75 分且状态 fail |
| 已有目录 | 固定目录已存在时运行失败，既有文件不删除、不覆盖；不增加模型调用 |
| worker 幂等与重开 | 同一 run 重试不重置后改文件；第二个 run 拒绝；重开 journal 后保留原目录/状态；MCP 请求在创建目录前拒绝 |
| 校验与证据 | 路径越界、大小写重复、文件/目录冲突、数量/字节超限及 NUL 拒绝；预览深拷贝，错误运行或摘要证据保持未知 |
| 工作台 | 固定文件增删/复制、字面内容、路径校验和勾选开关通过；隔离候选隐藏共享应用启用入口 |

目录对照的本地模型夹具中，正常动态组共 8 次模型调用、快速组 0 次；强制回退的动态组 8 次、快速组 4 次。这只验证调度、计量和回退机制，不代表真实模型节省或业务质量结论。

以上使用真实本机宿主和 worker 子进程，以及本地确定性模型夹具。Windows 仅交叉编译；界面测试使用 Node DOM，未完成真实浏览器布局、焦点或移动端验收。

## 原始记录与复验

最终汇总：[validation/kun-0.43.0-final/summary.json](validation/kun-0.43.0-final/summary.json)。同目录保留 go-test、go-vet、go-race、build、windows、syntax、ui、http 八份日志。

- [独立目录 HTTP](kun-fixtures-http-validation.json)、[K4 HTTP](kun-k4-http-validation.json)、[产物 HTTP](kun-artifacts-http-validation.json)、[对照计量](kun-benchmarks-http-validation.json)
- [任务集界面](kun-task-sets-ui-validation.json)、[K4 界面](kun-k4-ui-validation.json)、[CLI/协议](kun-cli-validation.json)、[接口契约](kun-contract-validation.json)、[构建汇总](kun-build-validation.json)
- 集成检查保留于 `validation/kun-0.43.0-integration/`；0.42 顶层 JSON 证据副本保留于 `validation/kun-0.42.0-evidence/`，历史原始日志继续保留。

复验：`python3 scripts/validate-kun.py --output validation/kun-local-1`；Go 不在 PATH 时指定 `--go /absolute/path/to/go`。需要 Python 3、Node、go.mod 指定 Go 和 race 所需 C 编译器。每次用新目录；HTTP 必须使用同次成功构建。源码指纹覆盖 cmd/internal/scripts、go.mod/go.sum/Makefile，不含文档和生成记录。

最终源码 SHA256：`dfd38504ffb2d78f578b7f7e718f4027bc754c69a6aace578277e8f9a28cef9f`。

## 尚未验收与未实现

独立目录仅支持内置文件工具，是目录分离而非 OS、进程或网络沙箱；不复制或重置项目，不提供远程副作用隔离。固定输入中已有的文件仍可通过产物规则，不能证明本次新建。

真实 Codex/远程模型/MCP、代表性业务任务集、Windows 执行和生产负载仍需对应环境。项目快照、外部状态隔离、自动留存清理、结构化文件/事实/外部系统裁判、统计推断和可选学习型 JEV 未实现。本地夹具不替代这些范围。
