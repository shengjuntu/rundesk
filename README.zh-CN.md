# RunDesk

[English](README.md) · [文档导航](docs/README.md)

RunDesk 是面向应用的 Agent 执行平台，通过一套 API 接入 Kun 与 Codex，统一管理任务、会话、工具审批和执行产物。独立项目 Kun 以执行轨迹为核心，支持逐步检查模型交互与工具调用，通过单步调试、回放和对照评测定位失败原因、验证改进效果。经过独立验证的成功轨迹还可沉淀为可复用 Skill，让调试成果持续用于实际业务。

## 当前版本

**RunDesk 0.52.0 · 外部 Kun 0.17.0 · worker 协议 16**。新增常驻[构建中心](docs/BUILD-CENTER.md)，统一管理跨应用 Docker 配方、构建、镜像、环境和离线检查。原生 Codex 构建插件提供 `add-app-docker-image` Skill 与限于当前任务的构建 MCP；[插件支持](docs/NATIVE-PLUGINS.md)展示配置和已观察到的 Skill/MCP 来源，不调用尚未稳定的原生安装接口。任务自由组合这些资源，不增加工作流层级。Kun 继续独立构建，通过轻量 JSONL SDK 连接，见[拆分与兼容说明](docs/KUN-SEPARATION.md)。

- 应用环境：按绑定启动/复用容器，检查认证和工具；开始研究不改镜像，不断开现有活动会话。
- 应用接入：独立凭据、项目与操作范围、持久任务队列、会话、事件订阅、文件与幂等回执。
- 统一调试：一个工作台检查两个后端的执行轨迹、调用证据与模型交互；Kun 另有单步、条件断点、上下文快照、独立诊断、Hybrid/Live 分叉及对照评测。
- 验证与复用：固定案例、产物规则、独立验证、参数化 Skill、守卫回退和重复采样对照；业务收益由实际任务评测确认。
- 启动配置：自动检查所选后端的本机依赖；Kun 可独立完成初始化；Codex 可选安装到数据目录，精确固定版本。


独立视频环境：保留自研 `examples/video-build`，另增 `examples/hyperframes`、`examples/video-use` 和 `examples/openmontage`，分别运行上游原生制作流程。三者通过 Video App 0.26.0 的生成 MCP 获取配音、图像和视频素材。见 [四套独立制作环境](docs/UPSTREAM-VIDEO-ENVIRONMENTS.md)。

## 快速开始

Go 1.25.12 或兼容的更新版本；WebUI 已嵌入，无前端构建步骤。

```bash
make build
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

打开 http://127.0.0.1:3210，按向导选择 Kun 或 Codex。Kun 连接支持工具调用的 Chat Completions 服务；Codex 使用其原生模型与认证配置。两个后端可分别配置在不同实例，旧会话保持原后端。只体验界面可用 `--demo`，不调用真实模型、不安装软件。

HTTP 采集默认关闭。`--observe-http=true` 只设置新建 Codex 会话的默认值，已有会话保存的开关不被覆盖。关闭的会话不启动观测代理，也不为采集发现模型地址。操作与生效时机见 [采集设置](docs/UNIFIED_DEBUGGING.md)。

本包仅构建 RunDesk。请在独立 Kun 项目中执行 `go build -o bin/kun ./cmd/kun`，然后通过 `--kun` 指定绝对路径，也可将 Kun 放在 RunDesk 可执行文件旁。缺少或不兼容时明确报错，不自动切换到 Codex。`third_party/kun-sdk` 只包含轻量 SDK，构建宿主无需 Kun 内核源码。`your-org` 模块路径仍是未发布占位，由本包 `go.mod` 本地替换解析。Windows、模型配置与安装说明见 [启动与部署](docs/SETUP.md)。

## 典型运行配置

[examples](examples/README.md) 提供 news2douyin、people-search、video-build 三套配置和统一 CLI，可生成现有构建页可用的 ZIP；初始化向导可直接选配置并后台构建、拉取或检查镜像；研究配置默认包含 Exa 与固定版本 Agent-Reach 工具链。

视频环境配套 Video App 0.23.0 和 video-build 1.4.0，默认提供 HyperFrames 0.8.87、GSAP 3.15.0、FFmpeg 与字体，由 Codex 自主分镜、取材、生成和动画。主流程通过 App MCP 保存/渲染工程，不要求共享生产目录。固定 MIT video-use 与 adapter 协议 2 改为显式可选，供旧 EDL 工程继续使用。App 渲染镜像与 Codex 工具镜像分别更新；需重建、选择并明确更新运行环境，保留工作区和认证。离线渲染与诊断不证明真实模型已完成制作或理解图片。见 [VIDEO-APP](docs/VIDEO-APP.md) 和 [video-build](examples/video-build/README.md)。

## 应用接入与架构

- [RunDesk Control API（RDCA）v1](docs/API-V1.md)：自身 API 名称，继续使用 `/api/v1`；认证、任务、会话、事件、文件、错误、幂等与最小示例。
- [API 定位与兼容计划](docs/API-STRATEGY.md)：RunDesk 独立，不依赖 HarnessRouter；未来提供独立 UHP 服务入口，供原接 UHP 服务的客户端切换并调试新运行。当前未实现或声明 UHP 兼容。
- [系统架构](docs/ARCHITECTURE.md)：平台、后端适配、Kun 内核与执行环境的职责。
- [后端能力](docs/BACKENDS.md)：Kun/Codex 差异及当前扩展边界。
- [轨迹与调试](docs/DEBUG.md)：从查看证据到单步、分叉和验证。
- [统一工作台与采集](docs/UNIFIED_DEBUGGING.md)：Codex 请求/响应、结构化查询与直通代理的配置和边界。
- [当前状态](docs/STATUS.md)、[验证记录](docs/VALIDATION.md)、[升级说明](docs/UPGRADING.md)。

## 执行边界

Kun 的 Bash 默认关闭，启用后使用宿主账户权限；文件工具的路径限制不构成 OS 沙箱。当前托管 Docker 路径适用于 Codex，Kun 容器适配仍待实现。Codex 没有 Kun 的内部单步或完整上下文快照能力。任务完成、工具成功与业务目标达成都分别记录；未知结果不会被当作成功或自动重放。

## 致谢

感谢 [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG)、Michael Kinsy 及其贡献者、Mario Zechner / Pi，以及 [HarnessRouter](https://github.com/HarnessRouter/harnessrouter)。独立 Kun 项目保留既有 PiG SSE 代码、选取测试与原文 MIT 许可并继续独立发展；运行层抽象参考 HarnessRouter/UHP 的应用与后端边界。具体源码来源、设计参考和许可见 [UPSTREAM](docs/UPSTREAM.md) 与 [LICENSES](LICENSES)。没有声明完整 PiG 或 UHP 兼容。

[许可协议](LICENSE)
