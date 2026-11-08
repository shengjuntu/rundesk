# RunDesk 文档 / Documentation

[English overview](../README.md) · [中文概览](../README.zh-CN.md)

RunDesk 的三个使命：为 Agent 落地提供调试工作台、连接领域应用与 Agent Harness、为 Harness 研究提供实验场。文档按完整任务组织；界面操作、对应契约与限制在同一专题查阅。

当前源码：**RunDesk 0.67.0 / 独立 Kun 0.17.0 / worker 协议 16**。新安装先进入[初始化](SETUP.md#首次初始化)；日常会话与应用由 Codex 执行，Kun 用于[内部维护](MAINTENANCE.md)与只读轨迹问答。配方声明引用的共享内容来自 `recipes/common/`，导出时物化为普通文件；操作见[下载源与配方文件](BUILD-SOURCES.md)及[升级说明](UPGRADING.md)。

## 1. Agent 落地与调试 / Agent debugging

| 任务 | 文档 |
| --- | --- |
| 阅读轮次、核对步骤耗时、查看证据与问答 | [轨迹阅读与分析](TRACE.md) |
| 配置模型 HTTP 采集并理解传输证据 | [统一工作台与采集](UNIFIED_DEBUGGING.md)、[模型传输边界](MODEL-TRANSPORT-BOUNDARIES.md) |
| 通过 API / MCP 查询共同观测证据 | [只读调试服务](DEBUG-SERVICE.md)、[观测模型](OBSERVATION-MODEL.md) |
| 检查 Kun 内部状态、单步、断点与面板 | [Kun 调试手册](KUN-DEBUG.md) |
| 解释问题、核对副作用并继续任务 | [独立诊断](KUN-DIAGNOSIS.md)、[错误与恢复](RECOVERY.md)、[Kun 安全检查点](KUN-CHECKPOINTS.md) |

## 2. 应用接入 / Application integration

| 任务 | 文档 |
| --- | --- |
| 注册应用并授权 | [应用连接](APPLICATION-CONNECT.md)、[应用凭据](APPLICATION-CREDENTIALS.md)、[用户授权](USERS.md) |
| 提交任务、跟踪进度、获取制品 | [RDCA v1](API-V1.md)、[队列](QUEUE.md)、[定时任务](SCHEDULES.md)、[文件与产物](PERSONAL-FILES.md) |
| 选择应用可用环境及项目隔离 | [运行预设](APPLICATION-RUNTIME-PRESETS.md)、[项目环境](APPLICATION-PROJECT-ENVIRONMENTS.md) |
| 从配方准备镜像，再创建 Agent | [构建中心完整指南](BUILD-CENTER.md)、[配方 CLI](../recipes/README.md) |
| 修改配方与下载源、复用 Codex | [配方与源设置](BUILD-SOURCES.md)、[构建运行时](BUILD-RUNTIME.md) |
| 下载或发布预构建镜像 | [镜像分发](IMAGE-DISTRIBUTION.md) |
| 配置容器、模型与插件 | [Docker 环境](DOCKER-ENVIRONMENTS.md)、[Codex 模型服务](CODEX-PROVIDERS.md)、[插件边界](NATIVE-PLUGINS.md) |
| 使用 UHP 接口 | [UHP 服务、接入与限制](UHP.md)、[Python 客户端](../clients/python/uhp_client.py) |
| 参考领域应用 | [Video App 接入](VIDEO-APP.md)、[独立视频环境](UPSTREAM-VIDEO-ENVIRONMENTS.md)、[新闻采集工具](../recipes/news2douyin/AGENT-REACH.md) |

RDCA `/api/v1` 与 UHP `/v1` 共用任务核心；平台定位和协议边界见[系统架构](ARCHITECTURE.md#api-position)。机器契约 `/api/v1/openapi.json` 按权限标记区分管理与应用接口。

## 3. Harness 开发与研究 / Harness research

保留的研究 API、记录及既有分支查看继续可用；Kun 分叉、评测任务集、K4 和离线实验已撤下日常启动入口，旧按钮说明不代表当前导航。

| 任务 | 文档 |
| --- | --- |
| 理解 Kun 与宿主边界 | [Kun 导航](KUN.md)、[独立工程与 SDK](KUN-SEPARATION.md)、[核心工具](KUN-PIG-CORE.md) |
| 试验循环策略与模块状态 | [Harness 组合与切换](KUN-HARNESS.md)、[离线记录实验](KUN-EXPERIMENTS.md) |
| 从同一基线创建、假设和比较分支 | [分叉、假设与对照](KUN-FORKS.md) |
| 固定输入、产物规则、评分与重复采样 | [评测完整指南](KUN-EVALUATION.md) |
| 将验证过的轨迹用于应用 | [K4 参数化 Skill 与快速路径](KUN-K4.md) |

## 部署与维护 / Operations and development

- [启动与部署](SETUP.md)、[升级](UPGRADING.md)、[当前能力与缺口](STATUS.md)、[内部维护](MAINTENANCE.md)。
- [系统架构](ARCHITECTURE.md)、[后端能力](BACKENDS.md)、[资源契约](RESOURCE-CONTRACTS.md)、[任务资源视图](TASK-RESOURCES.md)。
- [WebUI 与会话](WEBUI.md)、[协作预览](COLLABORATION.md)。
- [验证与复验](VALIDATION.md)、[版本变更](../CHANGELOG.md)、[上游来源](UPSTREAM.md)、[安全](../SECURITY.md)。

当前验证与历史证据分别查阅[验证范围](VALIDATION.md)和[历史资料说明](validation/README.md)。旧源码包保留原始记录，本版不继承旧通过数。
