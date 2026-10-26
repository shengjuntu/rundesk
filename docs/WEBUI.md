# WebUI：以实例为中心 / Instance-centered WebUI

本文描述 RunDesk 0.54.0（2026-10-26）的界面与当前后端边界。升级不改变存储布局，也不迁移或删除原有实例、工作区和历史数据。

RunDesk 的日常操作围绕实例展开：选择一个 Codex 或 Kun 实例，检查它的模型、Skills、MCP 和插件能力，选择工作区，再执行任务、查看制品与运行轨迹。应用接入和构建中心分别负责外部调用与运行环境准备，不要求用户先注册应用才能管理实例。

## 实例、工作区和构建预设

| 对象 | 职责 |
| --- | --- |
| 构建预设 | 指定 Agent、系统依赖、内置能力和初始配置 |
| Docker 镜像 | 构建结果，可以用于创建多个实例 |
| 实例 | 长期管理的 Agent 身份，关联运行方式、镜像和配置入口 |
| 工作区 | 项目文件、项目规则及制品所在的目录 |
| 运行环境 | 某个实例在某个工作区使用的容器及持久状态 |

默认实例保留本机使用方式。通过构建中心创建的 Docker 实例可以直接进入实例页面管理；重建容器不等于重新创建实例。Kun 当前仍仅支持本机 worker，不能通过界面布局变化获得 Docker 托管能力。

实例页面集中展示配置入口，并明确当前工作区。切换工作区后，应核对配置所属的运行环境；任务与轨迹保留原来的实例和工作区归属。应用及其命名运行预设继续作为接入绑定使用，详情见[运行预设](APPLICATION-RUNTIME-PRESETS.md)。

## 当前配置边界

本轮调整统一了操作入口，**尚未实现实例配置自动同步到所有工作区**。当前 Docker 运行环境仍按“实例 × 工作区”保存独立的 `CODEX_HOME`、`HOME` 和状态；在其中一个工作区添加的 Skills 或 MCP 配置，不会自动复制到另一个工作区。项目目录中的 Skills 也只属于该项目。配置页必须显示实际作用范围，不能将入口集中解释为配置已经全局共享。

镜像内置能力、环境追加配置和项目提供的能力仍来自不同位置。原生插件安装/卸载的支持范围见[插件能力](NATIVE-PLUGINS.md)；资源被观察到或包检查成功，不等于已经安装可用。

后续的实例配置版本、跨工作区应用、自动收集可复用资源以及 Agent 配置格式适配，需要独立实现和验证。本版不把现有能力页面声明为已经完成的全局资源库。

## 研究界面收敛

以下四个研究工具暂时撤出日常工具栏和创建入口，减少与实例配置、普通任务及 Skills 管理的混淆：

| 工具 | 原有用途 |
| --- | --- |
| Kun 分叉 | 从已停止运行的安全边界创建 Hybrid 回放或 Live 执行分支 |
| 评测任务集 | 固定输入、验收规则与案例执行定义 |
| K4 Skill 工作台 | 提炼参数化工具轨迹，独立验证后用于 Kun 快速路径；不是普通 `SKILL.md` 管理 |
| 离线实验 | 编辑冻结记录中的观察文本并比较差异，不重新运行模型或工具 |

这些功能已有部分执行闭环和测试，但仍主要面向 Kun 研究者；真实业务效果及目标运行环境需要进一步验收。本轮保留后端 API、保存的数据、研究查看组件和已有分支会话的“查看固定预览”，不清除历史研究记录。日常轨迹查看、调试问答、Kun 状态检查、单步、断点和检查点恢复继续保留。

历史专题文档中的顶部按钮或创建入口说明不再代表当前导航。研究 API 与数据语义仍见[分叉](KUN-FORKS.md)、[任务集](KUN-TASK-SETS.md)、[K4](KUN-K4.md)和[离线实验](KUN-EXPERIMENTS.md)。

## English summary

The WebUI centers daily work on an Agent instance: inspect its capabilities, select a workspace, run tasks, and inspect artifacts and traces. Build presets produce images; images can support multiple independent instances. Application registration remains an integration concern.

This navigation change does not yet synchronize configuration across workspaces. Docker environments still have separate persistent homes for each instance/workspace pair. The four Kun research launchers have been withdrawn from everyday navigation while their APIs, stored records, and historical branch inspection remain available.
