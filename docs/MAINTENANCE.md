# 内部维护 / Internal maintenance

自 0.61.0 起收敛职责；本页描述 0.67.1 的维护边界：**Codex 执行日常会话和应用任务，Kun 负责内部维护与轨迹问答**。三个使命保持不变：应用连接、执行调试、Harness 研究实验。

## 当前分工

| 场景 | 执行者与入口 |
| --- | --- |
| 日常会话 | 工作区 → 会话；选择 Codex 配置与工具环境 |
| 应用任务、队列、定时任务、UHP/A2A 任务 | Codex；继续使用已有应用权限与预设绑定 |
| 发现模型服务、配置维护模型 | 环境设置；Kun 配置不出现在日常引擎选项中 |
| Codex 安装修复 | 初始化中的 Kun 维护任务；保留已有预算与验证规则 |
| Docker 配方准备与失败修复 | 构建中心的 Kun 维护入口；独立目录、候选与测试记录 |
| Codex 轨迹问答 | 原轨迹的问答入口；单独 Kun 会话读取固定来源快照 |
| Skill 提取、Skill/MCP 优化 | 后续阶段；本次未接入自动发布或全局自动修改 |

后台拒绝日常接口创建或续写普通 Kun 会话，返回 `409 kun_maintenance_only`。公共任务提交不能设置内部维护标志。维护目录不能作为普通工作区提交，返回 `maintenance_workspace_reserved`。此前保存的 Kun 记录、配置和研究 API 保留；研究接口不等于日常任务入口。既有运行不因本次界面收敛而被删除或停止。

普通会话树隐藏 Kun 与维护目录，旧记录仍可通过任务记录或原有链接查看。会话层级仍是工作区 → 会话。Kun 配置保存在原来的环境设置中，不新增配置入口。

## Docker 维护闭环

借鉴 [HoH 论文](https://arxiv.org/pdf/2609.01481) 中基于证据迭代的方向，本版实现的是构建领域的有限闭环，并非完整复现论文，也不训练模型或自动改写基础 Harness。

1. 读取选定失败构建的冻结上下文和日志，在独立维护目录中工作。
2. 用 `build_plan_record` 保存一个目标、诊断假设、保留能力和验收条件。计划不可原地改写；同一请求重试复用原记录。
3. 准备候选配方，用相同 `requestId` 调用 `build_start`。每项维护任务最多三次构建；未完成当前构建或未检查成功候选时，不能开始下一份计划。
4. 对候选执行服务端 offline 与 smoke 检查。只有所属任务、来源构建、镜像身份、上下文摘要、配方摘要、平台和 Codex 运行时快照匹配的成功记录才计入验证。修复须保留原构建声明的 smoke 命令与产物断言。
5. 失败后保留证据，再提出下一轮计划。通过后停止继续构建，调用已有经验记录能力保存失败指纹、文件差异和验证记录。经验的 `verified` 状态仍由宿主验证。

`assistant-runs` 的新增 `maintenance` 字段展示每轮计划、候选和检查结果。状态包括 planning、building、verifying、replanning、verified、blocked。**任务 completed 仅表示执行结束；缺少完整检查时仍显示“尚未通过验证”。** 新 Kun 任务使用 `protocolVersion:1`；已有任务保持原协议。旧 Codex 构建助手 API 作为兼容路径保留，当前界面使用已配置的 Kun。

计划中的验收文字是声明，不是机器证明。宿主验证冻结身份和已有 smoke 合同，但不能证明任意候选脚本具有充分的业务覆盖。复杂行为仍需要维护者提供足够的测试。候选镜像与经验保存不会自动替换应用当前使用的镜像、修改全局下载源或发布到公共仓库。

## 轨迹问答

正常运行模式下，Codex 轨迹问题交给已配置的 Kun 维护助手；缺少配置时明确返回 `maintenance_kun_not_configured`，原始轨迹仍可查看。问答记录保存来源实例、工作区、轮次和事件游标，使用独立诊断目录与只读 trace MCP。请求禁用写文件与 Bash，不继承维护工作区的自定义指令，不续跑原业务任务。

Demo 保留模拟问答行为。底层同引擎分析方法留给研究与兼容测试；公共创建入口使用上述维护路径。

## English summary

Codex runs everyday conversations and application tasks. Kun serves internal installation/build maintenance, read-only trace Q&A and retained research APIs. Ordinary conversation and task endpoints reject Kun; old records remain readable. Configure Kun through Environment setup. The daily hierarchy remains workspace → conversation → selected Codex environment.

New Kun build tasks require an immutable, bounded plan before each candidate and allow at most three builds. The host derives verification from matching frozen-image offline and smoke records, preserves the original declared smoke contract, and stops further builds once verified. Task completion is distinct from repair verification. Existing experience storage retains evidence for later attempts. No candidate is automatically deployed or published. This is a build-focused implementation inspired by HoH, not a reproduction of its full method or automatic model/Harness training.

Normal Codex trace Q&A uses a dedicated Kun session with read-only source evidence and no file-write or Bash capability. Automatic Skill extraction, Skill optimization and MCP optimization are not implemented by this revision.
