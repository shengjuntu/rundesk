# Kun 0.1 / RunDesk 0.20.0

首个可运行实现。采用已确认的结构：**分进程、同仓库、选择性复制 PiG 源码并自主发展**。本版完成基础模型／工具循环、运行记录、上下文检查和基础调试控制；不代表 KUN-DESIGN-v0.2 的所有阶段已经实现。

## 构建和启动

要求 Go **1.25.12 或更新兼容版本**。无需 Node.js；使用 Kun 时无需安装 Codex。

```bash
make build
# 仅当模型服务需要认证时，先在服务进程环境中设置 KUN_API_KEY
./bin/rundesk --data ./data
```

打开 http://127.0.0.1:3210。在配置页选择 **Agent 引擎**，选择 Kun，设置：
- OpenAI 兼容 API 基础地址，例如 `http://127.0.0.1:8000/v1`。
- 服务支持的模型名称。
- API Key 的环境变量名称，例如 `KUN_API_KEY`，而不是密钥内容；无需认证可留空。
- 是否允许写文件、每轮最多模型调用次数、请求超时、模型调用前断点。

保存后新建会话并发送消息。服务进程环境变更需要重启 RunDesk。配置按实例生效；已有会话固定后端归属，切换实例后端不会迁移旧会话。

默认在 RunDesk 可执行文件旁查找 `kun`（Windows 为 `kun.exe`）。开发时可显式指定 `--kun /absolute/path/to/kun`。两个二进制应来自同一源码版本。

Windows 可直接用 PowerShell 构建：

```powershell
$env:CGO_ENABLED = "0"
go build -buildvcs=false -trimpath -o bin/rundesk.exe ./cmd/rundesk
go build -buildvcs=false -trimpath -o bin/kun.exe ./cmd/kun
.\bin\rundesk.exe --data .\data
```

`--demo` 保留原有无真实模型调用语义，禁止启用 Kun。首次设置中的 Codex 安装检查可跳过；Kun 配置位于配置页的 Agent 引擎入口。

## 代码与进程边界

| 路径 | 职责 |
| --- | --- |
| `cmd/rundesk`、`internal/app` | 网页、授权、实例、会话、产物、任务队列及事件投影 |
| `internal/adapters/kun` | 启动独立 worker、握手和 JSONL 请求关联 |
| `internal/kunproto` | 仅共享可序列化的协议与状态类型 |
| `cmd/kun` | worker 命令入口、目录锁、stdin/stdout JSONL 协议 |
| `internal/kun` | 模型调用、工具执行、状态机、控制命令、SQLite 记录 |
| `internal/web/kun.js` | 引擎配置和 DevTools 面板 |

每个加载的 Kun 会话对应一个 worker，可在同一进程中串行进行多轮对话。任务并发仍由 RunDesk 队列控制。worker 空闲回收或 RunDesk 退出时关闭；不在后台继续充当独立守护进程。

Kun 独占 `<data>/kun/sessions/<sessionId>/state.db`，RunDesk 的业务数据库保存消费游标与事件投影。两侧独立事务，通过单调序号补取和去重衔接；不存在跨进程共享内存或跨库原子提交。

## 首版能力

| 能力 | 实现范围 |
| --- | --- |
| 模型 | OpenAI 兼容 Chat Completions；SSE 工具参数增量拼接，也接收完整 JSON 响应 |
| 工具 | `read_file`、`list_files`、可显式启用的 `write_file`；相对项目路径 |
| Skills | 扫描项目 `.agents/skills/*/SKILL.md` 和实例 Skills 目录；显式选取后注入，保存内容和 SHA-256 |
| 会话 | 多轮文本历史；系统提示词按当前配置重新构建 |
| Network | 模型请求体、完整模型结果、工具参数与结果、耗时 |
| Elements | 请求时的有效消息、工具定义，以及对应持久化状态快照 |
| Sources | 请求暂停、继续、执行一个模型／工具步骤、文本补充、停止 |
| Performance | 模型／工具耗时和服务实际报告的 token 用量；不推算未报告数据或价格 |
| 记录 | 状态、事件、快照、命令回执、工具执行台账；API Key 字段不进入记录 |

不支持：MCP、Shell、PiG 插件/Node 扩展、图像输入、自动 Skills 激活、压缩/记忆管理、条件断点、替换工具结果、检查点继续执行、分叉、确定性代码生成、JEV/JIT、Docker worker。四模块健康度、Application/Layers 面板尚未实现。Codex 仍沿用现有后端，Kun 控制接口不会控制 Codex 的循环。

模型文本在请求结束后显示，目前没有逐 token UI。模型服务调用仍会发送任务上下文到配置的服务地址；“本地记录”不代表模型离线运行。

## 调试语义

- **暂停**：命令先持久化为 `queued`，在下一个模型／工具边界生效；不会冻结正在执行的 HTTP 请求。
- **单步**：在暂停状态允许一个模型调用或一个工具执行，随后在下一边界暂停；如果该步骤直接结束任务，则显示完成。
- **补充指令**：在下一个模型请求前应用。模型返回最终回答期间收到的补充指令会继续触发下一步；暂停时补充后再继续，下一次请求即包含它。
- **停止**：取消在途模型 HTTP 请求，工具不在中途回滚，任务标记为 interrupted。取消请求也有独立的生效回执。
- **幂等**：相同 requestId 与相同内容返回既有回执；不同内容复用 ID 拒绝。调试 API 要求当前 runId 与 expectedStateRevision，陈旧状态拒绝，需刷新。
- **失联**：重开 worker 时，未完成运行标为 interrupted；已派发但无结果的工具标为 outcome_unknown。禁止自动重试或自动继续。后续新轮次会看到中断提示。
- **持久化失败**：停止 worker，RunDesk 结束当前任务并报告错误；不继续执行未被记录的下一步。

DevTools 每 1.5 秒刷新。Elements 点击模型步骤时读取该步骤快照。当前只查询仍在线 worker 的完整快照；worker 回收后，RunDesk 轨迹保留模型请求/响应和工具事件。重新启动同一会话 worker 后可查询旧快照。本版不提供独立离线数据库浏览器。界面每次最多加载 20,000 条会话事件；API 可继续分页。

删除 RunDesk 会话会关闭 worker，但保留其执行记录目录以便调查；本版没有自动清理这些目录的策略。状态与上下文默认保存在本机，包含任务内容；不要把真实 data 目录加入源码仓库。

## 工具边界与限制

`os.OpenRoot` 将工具路径解析限制在工作区，拒绝绝对路径、上级穿越和逃逸符号链接；这不是隔离不可信本机进程的操作系统沙箱。工具只处理普通 UTF-8 文本，每文件最多 128 KiB；列目录最多 500 项。写入会覆盖目标内容，部分写入失败不会自动恢复原文件。

允许写文件是实例级明确授权，不逐个弹出审批。实例为 read-only 时强制禁用写入。Kun 不执行 Shell，也不将 Codex 的审批与网络配置宣传为自己的沙箱能力。

有效上下文上限 2 MiB，模型完成结果上限 1 MiB，单请求流读取上限 8 MiB；达到限制时明确报错，当前没有自动截断或压缩。快照采用完整复制，尚无内容寻址去重和保留期管理，长会话可能占用较多磁盘。

## 协议与 API

worker 协议版本 1，每行一个 JSON 对象；stdout 仅输出协议，stderr 输出诊断。方法包括 `hello`、`start`、`state`、`events`、`snapshot`、`control`。`hello` 宣告 `resumeCheckpoint=false`、`fork=false`、`mcp=false`。最大消息为 8 MiB。

HTTP 同时支持 `/api` 和 `/api/v1`：
- `PUT /instances/{iid}/agent-runtime`：管理员提交 `{revision, config}`。
- `GET /sessions/{sid}/kun/state`：当前状态。
- `GET /sessions/{sid}/kun/snapshots/{sequence}`：事件序号对应快照。
- `POST /sessions/{sid}/kun/control`：控制请求及 queued/applied/rejected 回执。
- 既有 `/sessions/{sid}/events`、SSE、轨迹和会话导出沿用，Kun 事件使用 `kun/*` 命名。

应用凭据仍受实例、项目和 read/run scopes 限制；控制要求 run scope，检查要求 read scope。管理配置只对管理员开放。

```json
{
  "requestId": "unique-command-id",
  "runId": "current-run-id",
  "expectedStateRevision": 8,
  "operation": "step"
}
```

精确请求结构见 `internal/kunproto/protocol.go` 和生成的 OpenAPI。

## 参考与后续

本版实际复制范围是 PiG SSE 解码实现和一项协议回归测试；Loop、持久化、RunDesk 适配与调试控制由 Kun 自行实现。来源、固定版本、许可证与改动见 [UPSTREAM.md](UPSTREAM.md)。

后续按设计推进独立工具/提供商接口、MCP、模块快照、检查点与分叉，再考虑轨迹归纳代码与去优化守卫。AgentJIT 仅为前期提供资料中的研究参考，本版没有实现或验证其效果。

验证范围、复现命令及已知测试时序问题见 [KUN-VALIDATION.md](KUN-VALIDATION.md)。
