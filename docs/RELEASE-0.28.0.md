# RunDesk 0.28.0 / Kun 0.6.0

本版补齐跨后端只读轨迹检查：把 Codex 与 Kun 的宿主保留事件重建为可核对的轮次、调用步骤、异常信号与统计。会话顶部和轨迹页提供“调试检查”，固定历史范围后可筛选、分页、查看详情及分块读取原始证据；“读取最新记录”显式推进范围。

- 统一 HTTP 和 `debug-mcp` 新增 runs、steps、step、issues、statistics；MCP 共 19 个只读工具。沿用应用及成员项目权限，离线 Kun 也可检查宿主已保留记录。
- Codex item、Kun 模型/工具/MCP 调用按各自身份关联；缺少标识、缺失端点、配对歧义、时钟逆序和预览截断明确标出，不把未知耗时补成零。宿主事件 ID 与 worker sequence 分开显示。
- 脱敏发生在预览、搜索和 Unicode 分块前，保留大整数；重建前检查 20000 条生命周期记录、64 MiB 总载荷及单事件 8 MiB 上限，超限拒绝而不伪装完整统计。旧分析会话复用同一重建器。
- 修复原有 Kun 调试脚本未嵌入二进制、首次引擎配置的零值导致表单无效、审批刷新函数作用域错误；加入嵌入资源回归检查。

Kun 引擎保持 0.6.0 / worker 协议 v6，没有新增运行时依赖。Codex 内部单步、条件断点、完整上下文快照仍未提供；自然语言自动诊断、历史分叉与 K3/K4 后续阶段未完成。主工作台已有新检查按钮，成员端可调用授权 API，尚未另加成员页面入口。

全量 Go 测试、静态检查、改动范围竞态检查、56 项 JS/CJS 语法检查、两组 Node 回归、Linux/Windows 构建及真实 CLI 协议冒烟通过。三组真实 Chromium 回归通过，覆盖 Codex 协议演示、Kun 调试控制和 MCP 审批；模型/MCP 使用本地服务夹具，不代表真实服务验收。

验证细节见 [KUN-VALIDATION.md](KUN-VALIDATION.md)，使用方式见 [DEBUG-SERVICE.md](DEBUG-SERVICE.md)，剩余工作见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。源码不含二进制，使用 Go 1.25.12+ 执行 `make build`。
