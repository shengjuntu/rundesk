# Kun

Kun 是可独立构建和运行的 Agent 执行引擎，负责模型与工具循环、执行预算、审批状态、事件日志、调试控制、检查点和分叉。RunDesk 通过进程协议使用 Kun，并负责应用、用户、项目、任务队列和工作台。

**当前版本：Kun 0.17.0，JSONL 协议 16。** 本次先完成工程拆分与接口稳定，没有引入新的 PiG 模块，也没有新增 HTTP 服务、模型 Provider 或多模态能力。

感谢 [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG)、Michael Kinsy 及其贡献者，以及 Mario Zechner / Pi。Kun 保留了来自 PiG 的 SSE 解码器和一项选取测试，原始版权和 MIT 许可随源码分发。Kun 其余模型循环、JSONL 协议、日志、工具和宿主集成是本地独立开发的实现；具体来源见 [UPSTREAM](docs/UPSTREAM.md) 和 [第三方说明](THIRD_PARTY_NOTICES.md)。

## 独立构建

需要 Go 1.25.12 或兼容版本。首次构建需要获取 `go.mod` 中的依赖；已准备依赖缓存的环境可离线构建。无需 RunDesk 源码、Node.js 或 Codex。

```bash
make build
./bin/kun --version
```

也可直接构建：

```bash
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun
```

`github.com/your-org/kun` 和 `github.com/your-org/kun/sdk` 是尚未发布的模块路径占位符。本源码包通过 `replace ... => ./sdk` 使用附带的 SDK，可在本地独立构建，不要求这些路径已有公开仓库。

## 运行和接入

Kun 是通过 stdin/stdout 接受 JSONL 请求的 worker，不自带聊天 WebUI。`--data` 指向独占的会话执行目录；日志写到 stderr，stdout 仅用于协议消息。

```bash
./bin/kun --data ./data/example-session
```

例如先发送握手，每条 JSON 占一行：

```json
{"version":16,"id":"hello-1","method":"hello","params":{}}
```

`hello` 返回引擎版本、协议版本和当前实现的能力。宿主须先检查版本及所需能力，再发送运行或调试请求。一次工具或模型调用完成，并不证明业务目标完成。协议、审批、恢复和诊断桥约定见 [PROTOCOL](docs/PROTOCOL.md)。

使用 RunDesk 0.49.4 时，两个项目分别构建：

```bash
# 在 Kun 项目目录中
make build

# 在 RunDesk 项目目录中
make build
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

也可将 Kun 放在 RunDesk 可执行文件旁。缺少或不兼容的 Kun 会明确报错，不会自动改用 Codex。模型服务、密钥环境变量、工作区和工具权限由调用方配置；仅查询版本与握手不调用模型。

## 工程边界

| 路径 | 用途 |
| --- | --- |
| `cmd/kun` | 独立 CLI 和 JSONL 分派 |
| `internal/engine` | 模型循环、工具、日志、控制、检查点与分叉 |
| `internal/mcp` | 引擎使用的 MCP 传输 |
| `sdk/protocol` | 可序列化的公共消息与纯校验 |
| `sdk/client` | 启动并连接外部 Kun 的轻量客户端 |
| `sdk/validation` | 宿主与引擎共用的纯校验函数 |

SDK 是独立 Go module，不导入执行内核或 RunDesk 的数据库实现。Kun 自己管理执行数据库；宿主负责自己的事件投影。两者没有跨进程原子事务。自然语言诊断通过宿主提供的、固定范围的只读 MCP 证据桥完成，Kun 不读取 RunDesk 数据库格式。

现有执行目录保留 `state.db`、锁和日志结构。0.17.0 明确支持既有 0.16.0 检查点与分叉清单；版本相同不等于任意状态均可恢复，仍需通过对应清单、哈希和运行状态校验。其他历史版本不自动迁移。

## 验证与限制

```bash
make test
make check
make windows
```

发布包含 Linux amd64 和 Windows amd64 程序。Windows 可通过 `rundesk.exe --kun C:\path\kun-windows-amd64.exe` 显式选择；Windows 程序仅交叉编译，未实机运行。

根 module 与 SDK module 分别测试。跨项目集成由 RunDesk 使用显式外部 Kun 二进制执行；验证范围见 [本版记录](docs/validation-0.17.json) 与 [真实 0.16→0.17 升级检查](docs/legacy-binary-compat-0.17.json)，结果以实际运行记录为准，不把本地模型夹具测试当成真实供应商或 Windows 原生运行验收。

Kun 的 Bash 是明确启用的宿主执行，审批、超时和工作区路径约束不构成 OS 沙箱。当前没有托管 Kun Docker、远程 worker、完整 PiG 扩展生态或可动态加载的模块插件。先固定公共协议，再逐项评估新的上游能力。

[MIT License](LICENSE)
