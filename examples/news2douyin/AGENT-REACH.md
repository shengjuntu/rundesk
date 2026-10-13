# Agent-Reach 安装与基础镜像评估

核对日期：2026-10-13。检查用户指定的 [silently0801/agent-reach](https://github.com/silently0801/agent-reach)，HEAD 为 `cf70302e928155703829439115a18132928f8913`、Python 包版本 1.1.0。

## 结论

此前 `--with-agent-reach` 只将 Python 包加入 requirements，没有补齐其安装流程。该 fork 的当前源码没有 Dockerfile、镜像发布 workflow 或文档声明的预构建镜像，因此不能采用一个尚未确认存在的官方镜像作为基础。搜索中同名 `bojieli/agentreach` 属于另一种 SSH 执行工具，不是这里的采集项目。

当前使用 Node 22 / Debian 基础镜像，在明确的 Docker 层中完成可重复检查的工具安装；后续可以将这一工具层发布为 RunDesk 自有基础镜像，再叠加 Codex。当前不预置不存在的公共镜像地址。

## 安装覆盖

| 上游流程 | 当前实现 |
| --- | --- |
| pip 安装 Agent-Reach | 固定 fork 提交，安装 CLI 及 Python 基础依赖 |
| 安装 Node、gh、mcporter、bird、undici | Node 基础镜像 + apt gh + npm 锁文件；工具可执行检查失败则构建失败 |
| 配置 Exa | Codex 原生 Exa seed；mcporter 在运行时 HOME 首次调用时初始化 |
| 安装 Skill | 从已安装包复制到 Codex `/etc/codex/skills/agent-reach` |
| 检查环境 | 镜像准备后断网、只读根目录、非特权用户检查；启动后另跑 `agent-reach doctor` |
| Cookie/代理/登录 | 在实际应用环境中配置，不烘焙到镜像 |
| 小红书、LinkedIn、Boss 等可选平台服务 | 不内嵌；按需求独立部署 MCP 并配置可达地址与登录 |

不直接运行 `agent-reach install --env=auto`：该版本会尝试未锁定的系统/npm 安装、探测外网，将 Skill/配置写入构建用户 HOME；部分安装失败只输出提示，退出成功并不能证明工具完整。Dockerfile 实现其基础安装步骤，并用明确失败的离线检查验收。联网渠道检查与业务可用性分开。

## 依赖选择及限制

- npm mcporter 0.14.3 当前声明 Node >=24；本配方固定与 Node 22 兼容的 0.7.3，并锁定 npm 传递依赖。
- bird 固定 0.8.0；npm 已标记该包不再维护。保留以兼容此 fork 的工具/Skill，但不保证 X 渠道可用，Exa 研究不依赖其在线状态。
- undici 固定 7.16.0，Agent-Reach 及 Python 直接依赖固定版本；apt、基础镜像及 Python 传递依赖仍可能变化。
- 本轮未在真实 Docker Engine 中构建或执行联网模型研究；本地 npm 安装/CLI 版本检查和代码测试不能替代这一验收。

源码依据：[安装指南](https://github.com/silently0801/agent-reach/blob/cf70302e928155703829439115a18132928f8913/docs/install.md)、[installer](https://github.com/silently0801/agent-reach/blob/cf70302e928155703829439115a18132928f8913/agent_reach/cli.py)、[Python 依赖](https://github.com/silently0801/agent-reach/blob/cf70302e928155703829439115a18132928f8913/pyproject.toml)。许可与来源见 [THIRD_PARTY](../_shared/THIRD_PARTY.md)。
