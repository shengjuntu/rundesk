# 0.52.0 文档整理记录

日期：2026-10-25。基于用户提供的 `rundesk-v0.52.0-refactored-source(3).zip`；这是文档整理，运行版本仍为 **0.52.0**。

## 定位与入口

- 重写英文 `README.md` 与中文 `README.zh-CN.md`，按相同结构说明三个使命：企业 Agent 落地调试、应用与 Agent Harness 连接、Harness 开发与研究实验。
- 统一使用 RunDesk、Agent Harness、运行预设（Runtime Presets）等名称。明确应用的基本流程是提交任务、跟踪进度、获取制品。
- 按三类用户组织 `docs/README.md`，将领域示例的具体版本与部署细节留在专题入口。
- 同步 UHP 已有实现、统一观测与轨迹阅读器、独立 Kun、预设及原生插件的能力边界；未来目标与已实现功能分别描述。

## 删除与合并

| 原文件 | 处理及原因 |
| --- | --- |
| `OBSERVATION.patch`、`REFACTOR.patch`、`TRACE-READER.patch`、`UHP.patch` | 删除。内容已进入随包源码，是重构过程产物，当前构建与文档不再依赖；合计移除 1,408,851 字节。 |
| `docs/KUN-K1-CORE.md` | 合并后删除。四模块与预算说明纳入 [Harness](KUN-HARNESS.md)，JSON Schema 范围纳入[核心工具](KUN-PIG-CORE.md)；删除已经完成或与当前能力冲突的阶段计划。 |
| `docs/VALIDATION.md` 中连续堆叠的旧发布说明 | 移至[历史发布记录](validation/archive/RELEASES.md)，当前页说明各阶段证据和复验方法。 |

## 归档与保留

- 将原 `docs/` 根目录的报告/日志及 `validation/current/` 中混入的旧版本证据，共 **101 个文件**，移至 `docs/validation/archive/`。归档保持原 `docs/` 相对结构，便于追踪报告内的历史路径。
- 原始 JSON、JSONL 和日志逐字节保留，首次失败、未完成尝试和跳过记录均未删除；Markdown 链接随移动更新。历史报告中的旧版本和当时限制不改写为今天的结果。
- 当前证据入口保留资源重构、UHP、统一观测、轨迹阅读器的报告，另记录本次文档检查。原包最新功能阶段记录为 604 项通过、14 项外部 Kun 测试跳过；本次未重跑这些功能检查。
- 保留安装、API、操作、权限、恢复、实验、构建、应用示例、第三方来源与许可证文档。保留代码、测试、构建脚本、依赖、插件包、Skills 和内置配方资源。

## 核对

修正文档中的界面操作入口名称与本地链接，检查代码围栏，并按原 ZIP 比对所有非文档运行文件与归档证据。重新生成根目录 `SHA256SUMS`，打包后检查 ZIP 完整性和清单覆盖。检查结果见 [documentation-cleanup.json](validation/current/documentation-cleanup.json)。
