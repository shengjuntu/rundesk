# Kun 独立工程与 RunDesk 接口

当前配套版本为 **RunDesk 0.49.4、Kun 0.17.0、轻量 SDK 0.17.0、JSONL 协议 16**。本次先固定项目和进程边界，不引入新的 PiG 模块或模型能力。

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

两个 `make build` 各自产出自己的程序。RunDesk 默认仍可查找其可执行文件旁的 Kun；建议部署时显式指定绝对路径。缺失、启动失败或协议不兼容会明确报错，不回退 Codex。宿主实际连接时读取 `hello`，校验协议和所需能力；静态能力目录不能代替当前引擎的握手结果。

仅运行 Codex 的部署无需安装 Kun。只构建 RunDesk 也无需 Kun 的源码。Kun 可单独通过 JSONL 使用，不依赖 RunDesk、Node.js 或 Codex。

## 兼容范围

| 组合或数据 | 规则 |
| --- | --- |
| RunDesk 0.49.4 + Kun 0.17.0 | 本次配套组合；诊断使用宿主只读证据桥 |
| RunDesk 0.49.3 + Kun 0.17.0 | 协议 16 的普通核心方法可兼容；旧宿主的 snapshot-path 诊断需要升级宿主 |
| RunDesk 0.49.4 + 旧协议 16 worker | 以实际握手能力决定是否可用；缺少新诊断 provider 能力时不得启动新诊断 |
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

感谢 MichaelKinsy/PiG、Michael Kinsy 与 PiG 贡献者，以及 Mario Zechner / Pi。独立 Kun 保留既有 SSE 代码与选取测试的原文 MIT 许可、版权和逐文件来源。本次没有增加 PiG 代码。宿主和引擎各自列明所含部分，不把整个 Kun 称为 PiG 派生实现；见 [UPSTREAM](UPSTREAM.md)。

## 后续多引擎与公共资源方向

本轮完成 Kun 拆分。RunDesk 保持独立，不依赖 HarnessRouter；向下继续使用原生 Codex App Server、原生 Kun JSONL，并自行适配后续引擎。自身 API 定名为 RunDesk Control API（RDCA），保持 `/api/v1` 契约。UHP 是后续面向应用的独立兼容入口，而非必须调用的第三个执行后端；目标是让原接 HarnessRouter 的业务客户端可改接 RunDesk，并使用其调试工作台。当前未实现或声明 UHP 兼容，实施前需固定规范、Schema 和测试版本。完整范围与迁移边界见 [API 定位与兼容计划](API-STRATEGY.md)。

当前 Kun 仍拥有模型与工具循环、执行日志、检查点和分叉。进一步把 Kun 变成决策内核属于后续单独设计，不是本次拆分或 UHP 兼容已完成的能力。

Skills、MCP 和插件配置应由公共 ResourceCatalog 管理，按应用、实例和项目建立有范围的绑定，在每次运行前生成实际生效的冻结清单。各适配器把同一清单转换成引擎原生配置，并保留来源、版本、摘要与不支持项；不得静默丢弃配置。Skill 的公共资源对象需保留完整目录，包括 references、scripts、assets，不能只传 SKILL.md。插件作为带版本的资源包管理；其 Skills、MCP 和文件保持可追溯的组合关系。密钥使用受作用域约束的引用，冻结清单不复制明文密钥。现有 Kun `Start` 中的 Skills/MCP 公共类型继续使用，本次不为未来讨论修改协议 16。

公共调试层统一事件、证据查询和请求/响应查看，同时保留原始后端载荷及实际能力。单步、内部快照、检查点和分叉只对提供这些能力的引擎开放，不为其他引擎伪造内部状态。现有模型 HTTP 直通观测继续保留原协议和字节证据，不变成统一模型网关。

## 本版验证记录

配套独立 Kun 和 SDK 已通过完整 race 检查，Linux 程序实际运行、Windows amd64 交叉编译通过。宿主使用两个分别构建的真实程序执行集成检查，具体结果见发布时附带的 `validation-0.49.4.json`、`kun-coding-http-0.49.4.json` 和 `kun-k4-http-0.49.4.json`；本地模型夹具不代表真实供应商、部署或图形浏览器已验收。

本次宿主普通 Go 回归 411 项通过；独立 Kun、SDK 完整 race 与新增宿主边界 race 检查通过。两次完整宿主 race 的测试超时/生命周期夹具问题及针对性复核均如实记录在 validation-0.49.4.json，不将其描述为完整 race 全通过。
