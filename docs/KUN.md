# Kun 运行与调试

Kun 0.17.0 是独立项目、独立进程的 Agent 执行内核，与本源码 RunDesk 0.53.0使用 worker 协议 16。RunDesk 只链接轻量 SDK，通过 `--kun` 选择外部程序；普通核心方法与既有协议兼容，诊断使用 provider 能力守卫。当前包含模型/工具循环、MCP 与审批、七类编程工具、执行轨迹、单步与断点、快照、Hybrid/Live 分叉、规则评测和经过独立验证的 Skill 复用。

工程拆分、独立构建与 0.16.0 检查点/分叉兼容规则见 [KUN-SEPARATION](KUN-SEPARATION.md)。

首次使用见 [启动向导](SETUP.md)，功能差异见 [后端能力](BACKENDS.md)，完整进程关系见 [架构](ARCHITECTURE.md)。

- [核心工具和 Bash](KUN-PIG-CORE.md)
- [轨迹调试与分叉入口](DEBUG.md)
- [模块、执行预算与两套内置循环策略](KUN-HARNESS.md)
- [K4 验证和复用](KUN-K4.md)
- [当前状态](STATUS.md)与[验证](VALIDATION.md)

Kun 不依赖 Codex 或 Node.js。模型服务应支持 Chat Completions 工具调用；当前不支持图像输入、完整 PiG 扩展生态或托管容器。Bash 为显式授权的宿主执行，固定文件评测目录不是 OS 沙箱。
