# Kun 独立工程与 RunDesk 接口

当前配套版本为 **RunDesk 0.54.1、Kun 0.17.0、轻量 SDK 0.17.0、JSONL 协议 16**。Kun 自 0.49.4 起拆分为独立项目；本源码只分发 RunDesk 宿主和轻量 SDK。

## 职责与依赖

| 组件 | 拥有的职责与数据 |
| --- | --- |
| Kun | 模型与工具循环、执行预算、审批状态、执行数据库、事件、快照、检查点和分叉 |
| Kun SDK | 公共消息、进程客户端、纯校验；独立 Go module |
| RunDesk | 用户、应用、项目、会话、队列、工作区、凭据、宿主事件投影、统一调试工作台 |
| 诊断桥 | RunDesk 固定来源快照和查询语义，通过现有 `__trace_mcp` 提供只读证据 |

RunDesk 不再保留或构建 Kun 内核。`third_party/kun-sdk` 是明确版本的 SDK 快照，`go.mod` 通过本地 `replace` 引用；这里不包含引擎实现。Kun 同样不导入 RunDesk 代码或读取其数据库格式。`github.com/your-org/kun` 及 SDK 路径是未发布的命名占位，本包可独立本地构建，不要求先创建公开仓库。

## 构建与启动

分别在两个项目目录执行：

```bash
# Kun 项目
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun

# RunDesk 项目
make build
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

以上命令分别构建两个项目自己的程序。RunDesk 默认仍可查找其可执行文件旁的 Kun；建议部署时显式指定绝对路径。缺失、启动失败或协议不兼容会明确报错，不回退 Codex。宿主实际连接时读取 `hello`，校验协议和所需能力；静态能力目录不能代替当前引擎的握手结果。

仅运行 Codex 的部署无需安装 Kun。只构建 RunDesk 也无需 Kun 的源码。Kun 可单独通过 JSONL 使用，不依赖 RunDesk、Node.js 或 Codex。

## 兼容范围

| 组合或数据 | 规则 |
| --- | --- |
| 本源码 RunDesk 0.54.1 + Kun 0.17.0 | 当前配套组合；诊断使用宿主只读证据桥 |
| RunDesk 0.49.3 + Kun 0.17.0 | 协议 16 的普通核心方法可兼容；旧宿主的 snapshot-path 诊断需要升级宿主 |
| RunDesk 0.49.4 及后续宿主 + 旧协议 16 worker | 以实际握手能力决定是否可用；缺少新诊断 provider 能力时不得启动新诊断 |
| Kun 0.16.0 执行目录、检查点与分叉清单 | 0.17.0 显式兼容既有格式，并继续执行结构、哈希与状态校验 |
| 其他历史引擎、手工改过的清单 | 不自动声明兼容，不通过改版本号绕过校验 |

Kun 执行目录继续沿用原路径和 journal 结构，不迁移为 RunDesk 平台数据库。停止相关执行后更换二进制并保持原 `--data`。只读历史查看与重新执行是不同操作；可查看历史并不证明可恢复。

协议版本、引擎版本、SDK 版本分别管理。保持原方法语义的可选字段和能力可以增量演进；必填字段、控制回执或恢复语义的破坏性变更需明确升级规则与契约测试。

## 诊断边界

宿主创建固定来源快照、控制认证和事件上界，将受限工具目录通过 `__trace_mcp` 提供给 Kun。Kun 只能使用这份诊断证据目录，不继承业务 MCP、Skills、任意文件访问、写入或 Bash。`trace_propose` 仍只是待审核建议，不执行修改。原始来源、建议卡片与人工审核语义保留，见 [独立诊断](KUN-DIAGNOSIS.md)。

普通运行的核心 JSONL 方法保持协议 16；诊断是有能力守卫的扩展边界。不能仅凭旧宿主能启动新 worker，就推断其旧诊断参数仍受支持。

## 验证入口

RunDesk 的根 module 和 SDK 快照分别测试。跨进程测试使用明确指定的外部引擎，不在测试时从宿主源码重新编译隐藏副本：

```bash
RUNDESK_TEST_KUN=/absolute/path/to/kun/bin/kun \
RUNDESK_TEST_HOST=/absolute/path/to/rundesk/bin/rundesk go test ./...
python3 scripts/validate-kun.py --kun /absolute/path/to/kun/bin/kun
```

验证脚本只构建 RunDesk，记录外部 Kun 的路径、文件摘要和 `--version` 结果，并把 `RUNDESK_TEST_HOST` / `RUNDESK_TEST_KUN` 传给跨进程检查。Kun 自身测试在独立项目运行。局部模型夹具、交叉编译和真实部署的验收范围分别记录，不相互替代。

## 上游来源

感谢 MichaelKinsy/PiG、Michael Kinsy 与 PiG 贡献者，以及 Mario Zechner / Pi。独立 Kun 保留既有 SSE 代码与选取测试的原文 MIT 许可、版权和逐文件来源。本源码包不包含 Kun 引擎实现。宿主和引擎各自列明所含部分，不把整个 Kun 称为 PiG 派生实现；见 [UPSTREAM](UPSTREAM.md)。

## 平台接口与公共资源

RunDesk 保持独立，不依赖 HarnessRouter；向下使用原生 Codex App Server 和 Kun JSONL。自身 RunDesk Control API（RDCA）继续使用 `/api/v1`；本源码另有固定 UHP `2026-10-19` 的 `/v1` 服务入口，复用任务核心。该入口不替换 Kun 的内部协议，也不声明完整 UHP 一致性。覆盖范围与迁移边界见 [UHP 服务](UHP.md)和 [API 定位](API-STRATEGY.md)。

当前 Kun 仍拥有模型与工具循环、执行日志、检查点和分叉。进一步把 Kun 变成决策内核属于后续单独设计，不是本次拆分或 UHP 兼容已完成的能力。

资源目录、绑定、解析和策略服务已经接入任务准入与独立后端模块，记录 Skills、MCP、插件等声明的来源、版本和作用域。任务记录的是资源元数据；原生目录仍可能由外部工具修改，不能把声明清单当成已加载证明、内容快照或配置备份。完整资源写入管理、内容冻结和所有后端插件装载仍待扩展。Kun 的 `Start` Skills/MCP 公共类型继续使用；接口和边界见 [资源契约](RESOURCE-CONTRACTS.md)。

公共调试层统一事件、证据查询和请求/响应查看，同时保留原始后端载荷及实际能力。单步、内部快照、检查点和分叉只对提供这些能力的引擎开放，不为其他引擎伪造内部状态。现有模型 HTTP 直通观测继续保留原协议和字节证据，不变成统一模型网关。

## 验证范围

Kun 跨进程测试需要外部可执行文件；仅编译宿主或 SDK 不能证明该链路通过。验证记录按原执行版本和环境区分，最新重构记录中缺少外部 Kun 的用例明确跳过。历史本地模型夹具、交叉编译和真实供应商验收互不替代，结果与复验入口统一见 [验证记录](VALIDATION.md)。
