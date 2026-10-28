# RunDesk 文档 / Documentation

[English overview](../README.md) · [中文概览](../README.zh-CN.md)

RunDesk 的三个使命：为 Agent 落地提供调试工作台、连接领域应用与 Agent Harness、为 Harness 研究提供实验场。下面按使用者的任务导航，专题文档描述当前行为；旧版本经过与测试证据单独归档。

当前源码：**RunDesk 0.55.0 / 独立 Kun 0.17.0 / worker 协议 16**。日常界面从[实例管理与 WebUI](WEBUI.md)开始；构建中心准备运行基础，应用接入管理外部调用。

## 1. Agent 落地与调试 / Agent debugging

| 任务 | 文档 |
| --- | --- |
| 从任务失败定位到执行证据 | [调试入门](DEBUG.md)、[轨迹阅读器](TRACE-READER.md)、[轨迹与问答](TRACE.md) |
| 检查模型通信与工具调用 | [统一工作台与采集](UNIFIED_DEBUGGING.md)、[统一观测模型](OBSERVATION-MODEL.md)、[模型传输边界](MODEL-TRANSPORT-BOUNDARIES.md) |
| 通过 API / MCP 查询证据 | [只读调试服务](DEBUG-SERVICE.md) |
| 检查内部状态、单步与断点 | [Kun 调试控制](KUN-DEBUG.md)、[调用与快照](KUN-INSPECT.md)、[结构化面板](KUN-PANELS.md) |
| 诊断与恢复任务 | [独立诊断](KUN-DIAGNOSIS.md)、[错误诊断](DIAGNOSTICS.md)、[失败核对与继续](RECOVERY.md)、[安全检查点](KUN-CHECKPOINTS.md) |

## 2. 应用接入 / Application integration

| 任务 | 文档 |
| --- | --- |
| 注册应用并授权 | [应用连接](APPLICATION-CONNECT.md)、[应用凭据](APPLICATION-CREDENTIALS.md)、[用户授权](USERS.md) |
| 提交任务、跟踪进度、获取制品 | [RDCA v1](API-V1.md)、[队列](QUEUE.md)、[定时任务](SCHEDULES.md)、[文件与产物](PERSONAL-FILES.md) |
| 准备可复用的运行能力 | [命名运行预设](APPLICATION-RUNTIME-PRESETS.md)、[示例配置](../examples/README.md)、[原生插件](NATIVE-PLUGINS.md) |
| 构建与维护工具环境 | [构建中心](BUILD-CENTER.md)、[构建任务](BUILDS.md)、[镜像](IMAGES.md)、[Docker 环境](DOCKER-ENVIRONMENTS.md) |
| 使用 UHP 接口 | [API 定位](API-STRATEGY.md)、[UHP 服务与限制](UHP.md)、[Python 客户端](../examples/uhp_client.py) |
| 参考领域应用 | [Video App 接入](VIDEO-APP.md)、[独立视频环境](UPSTREAM-VIDEO-ENVIRONMENTS.md)、[新闻采集工具](../examples/news2douyin/AGENT-REACH.md) |

RDCA 的 `/api/v1/openapi.json` 包含应用与管理 API；按权限标记选择接口。UHP `/v1` 与 RDCA 共享任务核心，具体协议范围以 UHP 专题为准。

## 3. Harness 开发与研究 / Harness research

以下包含保留的研究 API 与历史资料。0.54.0 已撤下 Kun 分叉、评测任务集、K4 Skill 工作台和离线实验的日常启动入口；旧专题中的顶部按钮与创建流程不再代表当前 WebUI 导航。API 和保存的研究记录继续保留，当前界面边界见 [WebUI](WEBUI.md)。

| 任务 | 文档 |
| --- | --- |
| 理解 Kun 与宿主边界 | [Kun 导航](KUN.md)、[独立工程与 SDK](KUN-SEPARATION.md)、[核心编程工具](KUN-PIG-CORE.md) |
| 试验循环策略与模块状态 | [Harness 组合与切换](KUN-HARNESS.md)、[离线记录实验](KUN-EXPERIMENTS.md) |
| 从同一基线比较改动 | [Hybrid/Live 分叉](KUN-FORKS.md)、[工具结果假设](KUN-HYPOTHESES.md)、[来源与分支对照](KUN-COMPARISON.md) |
| 评估质量与执行成本 | [固定任务集](KUN-TASK-SETS.md)、[规则评分](KUN-EVALUATION.md)、[重复采样](KUN-BENCHMARKS.md)、[产物规则](KUN-ARTIFACTS.md)、[独立文件目录](KUN-FIXTURES.md) |
| 将验证过的轨迹用于应用 | [参数化 Skill 与快速路径](KUN-K4.md) |

## 部署与维护 / Operations and development

- [启动与部署](SETUP.md)、[升级](UPGRADING.md)、[当前能力与缺口](STATUS.md)。
- [系统架构](ARCHITECTURE.md)、[后端能力](BACKENDS.md)、[资源契约](RESOURCE-CONTRACTS.md)、[任务资源视图](TASK-RESOURCES.md)。
- [实例与 WebUI](WEBUI.md)、[对话界面](CONVERSATION.md)、[协作预览](COLLABORATION.md)。
- [验证与复验](VALIDATION.md)、[版本变更](../CHANGELOG.md)、[上游来源](UPSTREAM.md)、[安全](../SECURITY.md)。

文档维护以当前行为为准，版本沿革写入 CHANGELOG，历史证据进入 [验证归档](validation/archive/README.md)。0.52.0 的文档清理范围见[整理记录](DOCUMENTATION-CLEANUP.md)，本轮界面调整见 [WebUI](WEBUI.md)。
