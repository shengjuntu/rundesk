# Kun 运行与调试

Kun 0.16.0 是独立进程的 Agent 执行内核，与 RunDesk 0.45.0 使用 worker 协议 16。当前包含模型/工具循环、MCP 与审批、七类编程工具、执行轨迹、单步与断点、快照、Hybrid/Live 分叉、规则评测和经过独立验证的 Skill 复用。

首次使用见 [启动向导](SETUP.md)，功能差异见 [后端能力](BACKENDS.md)，完整进程关系见 [架构](ARCHITECTURE.md)。

- [核心工具和 Bash](KUN-PIG-CORE.md)
- [轨迹调试与分叉入口](DEBUG.md)
- [模块与执行预算](KUN-K1-CORE.md)
- [两套内置循环策略](KUN-HARNESS.md)
- [K4 验证和复用](KUN-K4.md)
- [当前状态](STATUS.md)与[验证](VALIDATION.md)

Kun 不依赖 Codex 或 Node.js。模型服务应支持 Chat Completions 工具调用；当前不支持图像输入、完整 PiG 扩展生态或托管容器。Bash 为显式授权的宿主执行，固定文件评测目录不是 OS 沙箱。
