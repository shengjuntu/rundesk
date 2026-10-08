# RunDesk 0.32.0 / Kun 0.8.0

本版推进 K3-B 首批安全运行时分叉。K3-A 离线记录实验保留；K3-C 与 K4 尚未实现。

- 从最近已停止的普通 Kun 运行中列出安全边界，校验 epoch/revision/through、环境、目录与配置后保存固定私有包。
- 管理员可预览继承状态、剩余预算、补充指令和录制数量，再显式创建独立 Hybrid 会话。后续模型重新调用，工具严格按身份/schema/参数/顺序回放；未命中停止，无真实工具回退。
- 分支使用独立执行数据库和空目录。来源不修改、项目不回滚、MCP 不重连。事件显示 replayed / executed=false 和来源序号；继承与新增用量分列。
- 一份预览绑定一次运行；重复提交、宿主重启不重复运行；删除目标后保持删除标记。普通新轮次、普通恢复和嵌套分叉均拒绝。
- 新增 5 个管理员 API、全局入口和 DevTools 入口；OpenAPI 共 150 个路径、180 个操作。
- 修复分叉恢复空动作账本的初始化；规范化空 Skills/MCP 列表的协议指纹比较。修正原有 stderr 测试对两个并发读取回调顺序的错误假定，未改变 RPC 生产逻辑。

两个二进制需一起升级：worker 协议升为 v8，Kun 升为 0.8.0。旧 0.7 检查点不能跨版本恢复/分叉，历史记录仍可检查。Codex 内部单步、完整快照等能力没有因此增加。

内核、HTTP、真实 worker、本地模型/MCP fixture、Node 界面交互与专项 race 验证见 [KUN-VALIDATION.md](KUN-VALIDATION.md)。真实 Chromium 在当前环境因本地套接字权限被阻止，不能声明本版视觉/移动端验收通过；真实远程模型、Codex/MCP、生产压力同样未验收。使用边界见 [KUN-FORKS.md](KUN-FORKS.md)，开发完成度见 [KUN-PROGRESS.md](KUN-PROGRESS.md)。
