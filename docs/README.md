# RunDesk 文档

当前版本 RunDesk 0.49.0 / Kun 0.16.0 / worker 协议 16。按当前实现维护使用说明，历史过程与旧验收副本不再放在当前源码中。

| 需要了解 | 入口 |
| --- | --- |
| 安装、环境检测、可选 Codex 安装 | [启动与部署](SETUP.md) |
| 应用如何接入 | [应用 API v1](API-V1.md) |
| 系统分层与后端扩展 | [架构](ARCHITECTURE.md)、[能力矩阵](BACKENDS.md) |
| 如何调试 Agent 执行轨迹 | [统一工作台与采集](UNIFIED_DEBUGGING.md)、[轨迹调试](DEBUG.md) |
| 如何复用验证有效的轨迹 | [K4](KUN-K4.md) |
| 当前完成度与缺口 | [状态](STATUS.md) |
| 如何复验与升级 | [验证](VALIDATION.md)、[升级](UPGRADING.md) |
| 参考项目与许可证 | [上游来源和致谢](UPSTREAM.md) |

## 专题说明

- 应用：[注册](APPLICATION-CONNECT.md)、[凭据](APPLICATION-CREDENTIALS.md)、[队列](QUEUE.md)、[定时任务](SCHEDULES.md)、[用户授权](USERS.md)。
- 调试：[只读服务与 MCP](DEBUG-SERVICE.md)、[Kun 入口](KUN.md)、[核心编程工具](KUN-PIG-CORE.md)。各调试专题由 DEBUG 导航。
- 运维：[Docker](DOCKER-ENVIRONMENTS.md)、[镜像](IMAGES.md)、[构建](BUILDS.md)、[错误诊断](DIAGNOSTICS.md)。
- 工作台：[对话](CONVERSATION.md)、[文件库](PERSONAL-FILES.md)、[协作](COLLABORATION.md)。

机器契约 `/api/v1/openapi.json` 同时包含应用与管理 API；请依据权限标记和应用 API 文档选择接口。

典型运行配置与统一 CLI：[examples](../examples/README.md)。Agent-Reach 评估与初始化流程：[配置说明](../examples/news2douyin/AGENT-REACH.md)。

- [视频应用与 Docker 联调](VIDEO-APP.md)：Video App 0.7.0 与 RunDesk 0.48.1 的双容器职责、启动、认证、素材和验收。
