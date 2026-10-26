# 验证范围与复验

本页区分 0.53.0 UHP 加固、此前 0.52.0 文档整理与功能验证，以及更早的发布记录。测试替身通过不等于真实模型、Docker 或业务制品已验收。

## 0.53.0 UHP 加固

本轮结果以 [uhp-hardening.json](validation/current/uhp-hardening.json) 中实际完成的检查为准。验证分别记录公共任务 deadline、UHP 能力披露与严格准入、预算忽略和旧活动记录处理、指令历史语义、进度/审批只读投影、客户端 HTTP fixture，以及官方套件；不能用其中一类结果替代另一类。

官方套件基线为 `2026.10.19.post3`，固定提交 `038d4d27afed078db339b107f8bf6031ebb37872`，线协议仍为 `2026-10-19`。Extended 的 50 项与新增 Full T-12 的覆盖不同：T-12 依赖 `createHarness`，本服务不提供该写入接口，不能直接用它验收。本版对硬工具轮次预算的承诺是明确不支持及严格拒绝，专用测试验证这一边界，不宣称通过 T-12 或完整 Full。

Go 专项使用原生协议模拟器和已提交事件覆盖运行身份、到期/取消竞争、队列暂停、重启和审批识别；Python 客户端分别通过 HTTP fixture 和已编译 RunDesk 的隔离 demo 服务复验。真实 Kun、Docker 和模型凭据链路在本环境不可用，仍待目标部署环境完成 APP–Harness 任务、审批、取消与产物闭环验收。具体 pass、skip 和构建结果只以本轮报告为准，不继承下方旧报告的数字。

回归中发现并修复了原版 HTTP/1 观测代理的提前响应竞争；报告保留普通全量首轮失败、基线复现和修复后分包复验，区分全量与增量结果。

## 0.52.0 文档整理（历史）

2026-10-25 的修改仅涉及文档、历史证据目录、已合入的补丁文件和发行包 `SHA256SUMS`。检查包括中英文使命与能力边界复核、Markdown 本地链接及围栏、原始源码逐文件比对、归档证据完整性，以及重新打包后的文件校验。

该文档整理阶段未重跑 Go 功能回归、浏览器、Docker 或真实模型测试，不把下面的既有结果计作该阶段新测试。机器检查结果见 [documentation-cleanup.json](validation/current/documentation-cleanup.json)，具体清理范围见[整理记录](DOCUMENTATION-CLEANUP.md)。这些记录不回写为 0.53.0 的检查结果。

## 0.52.0 既有功能证据

`validation/current/` 保留构成本源码基础的资源重构、UHP、观测模型和阅读器各阶段报告。它们按实际先后解释，较早报告描述的是当时实现，不能覆盖后续阶段。

| 阶段 | 随包记录及范围 |
| --- | --- |
| 0.52.0 轨迹阅读器 | [报告](validation/current/trace-reader.json)：完整 Go 套件记录 604 项通过、14 项外部 Kun 测试跳过；核心 race 40 项通过，应用定向 race 62 项通过、4 项跳过；vet 与 Linux/Windows 构建通过。JSDOM 和实际 Chromium 配合明确服务/原生协议测试替身检查阅读、固定历史、证据分页与窄屏布局。 |
| 统一观测模型 | [报告](validation/current/unified-observation.json)：记录 594 项通过、14 项跳过；核心 race 73 项通过，应用定向 race 80 项通过、4 项跳过。验证多源身份、迟到事件、完整性、固定分页和权限；当时未做新的浏览器验收。 |
| UHP 服务 | [实现报告](validation/current/uhp-implementation.json)、[套件报告](validation/current/uhp-conformance-demo.json)：当时固定 post2 套件在原生 Codex 协议模拟后端上 50/50 通过；不代表真实后端或全部规范语义已符合，差异见 [UHP](UHP.md)。 |
| 资源与运行模块重构 | [报告](validation/current/resource-refactor.json)：记录宿主/SDK 回归、定向 race、vet、构建和资源边界检查。该阶段早于 UHP 实现，其中“UHP 未实现”是当时状态。 |

这些功能阶段没有真实供应商模型调用，部分 Kun 集成依赖缺失；Windows 为交叉编译。Docker、原生后端实际认证、生产负载和领域结果质量仍需在目标部署环境验收。不能用早期版本的外部 Kun 通过结果抵消后续报告中的跳过项。

## 本地复验

构建工具链与依赖见 [SETUP](SETUP.md)。从源码根目录执行：

```bash
make build
make test
make check
```

`make test` 包含 RunDesk 和轻量 Kun SDK；`make check` 包含 race 和 `go vet`。外部 Kun 集成测试需显式提供独立 Kun 与新编译宿主的绝对路径，例如：

```bash
RUNDESK_TEST_KUN=/absolute/path/to/kun/bin/kun \
RUNDESK_TEST_HOST=/absolute/path/to/rundesk/bin/rundesk \
go test ./... -count=1 -timeout=300s
```

未提供外部依赖时，应保留实际 skip 清单，不能只统计 pass。前端及协议专项脚本的依赖、测试替身和复验范围分别见[轨迹阅读器](TRACE-READER.md#validation)、[统一调试](UNIFIED_DEBUGGING.md)和 [UHP](UHP.md#验证与复现)。

UHP 应用接入和公共任务时限定向复验：

```bash
go test ./internal/app -run 'TestUHP|TestTaskDeadline' -count=1 -timeout=300s
python3 scripts/test_uhp_client.py
python3 scripts/test_uhp_client_integration.py --binary ./bin/rundesk --output-dir ./build/uhp-client-checks
```

这些命令不是完整 Go 回归或真实环境验收。官方套件还需外部固定 checkout 和已启动服务，命令及报告范围见 [UHP 验证](UHP.md#验证与复现)。

## 历史发布证据

旧版本报告、原始日志、首次失败、重试及摘要集中在[验证归档](validation/archive/README.md)，原始 JSON/JSONL/日志保持字节不变。原长篇发布验证说明保存在[历史记录](validation/archive/RELEASES.md)，不再与当前入口混排。
