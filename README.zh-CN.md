视频制作环境：video-build1.3.0使用adapter协议2，保留固定MIT video-use源码、连续外部配音和离线烟测；EDL镜头可选精确textRefs，配合Video App0.22按文字定位与局部调整，旧无引用EDL继续兼容。新增运行诊断仅检查已选运行容器、共享目录双向读写及当前MCP配置的真实PNG传输，不调用模型、不证明模型理解图片、不自动重建环境。需要重建工具镜像、在RunDesk选择并明确更新应用执行环境；保留原工作区与模型认证。详见[视频制作环境](examples/video-build/README.md)。

# RunDesk

[English](README.md) · [文档导航](docs/README.md)

RunDesk 是面向应用的 Agent 执行平台，通过一套 API 接入 Kun 与 Codex，统一管理任务、会话、工具审批和执行产物。自研 Kun 以执行轨迹为核心，支持逐步检查模型交互与工具调用，通过单步调试、回放和对照评测定位失败原因、验证改进效果。经过独立验证的成功轨迹还可沉淀为可复用 Skill，让调试成果持续用于实际业务。

## 当前版本

**RunDesk 0.49.3 · Kun 0.16.0 · worker 协议 16**。新增有应用范围约束的运行环境诊断，配套video-build1.3.0支持文字驱动视频制作与明确的证据边界。Codex HTTP采集继续默认关闭、按会话保存开关，需要时在调试工作台开启。Kun内核和worker协议保持兼容。

- 应用环境：按绑定启动/复用容器，检查认证和工具；开始研究不改镜像，不断开现有活动会话。
- 应用接入：独立凭据、项目与操作范围、持久任务队列、会话、事件订阅、文件与幂等回执。
- 统一调试：一个工作台检查两个后端的执行轨迹、调用证据与模型交互；Kun 另有单步、条件断点、上下文快照、独立诊断、Hybrid/Live 分叉及对照评测。
- 验证与复用：固定案例、产物规则、独立验证、参数化 Skill、守卫回退和重复采样对照；业务收益由实际任务评测确认。
- 启动配置：自动检查所选后端的本机依赖；Kun 可独立完成初始化；Codex 可选安装到数据目录，精确固定版本。

## 快速开始

Go 1.25.12 或兼容的更新版本；WebUI 已嵌入，无前端构建步骤。

```bash
make build
./bin/rundesk --data ./data
```

打开 http://127.0.0.1:3210，按向导选择 Kun 或 Codex。Kun 连接支持工具调用的 Chat Completions 服务；Codex 使用其原生模型与认证配置。两个后端可分别配置在不同实例，旧会话保持原后端。只体验界面可用 `--demo`，不调用真实模型、不安装软件。

HTTP 采集默认关闭。`--observe-http=true` 只设置新建 Codex 会话的默认值，已有会话保存的开关不被覆盖。关闭的会话不启动观测代理，也不为采集发现模型地址。操作与生效时机见 [采集设置](docs/UNIFIED_DEBUGGING.md)。

Windows、模型配置与安装说明见 [启动与部署](docs/SETUP.md)。本包是源码包；按上述命令构建，两个可执行文件放在同一目录。

## 典型运行配置

[examples](examples/README.md) 提供 news2douyin、people-search、video-build 三套配置和统一 CLI，可生成现有构建页可用的 ZIP；初始化向导可直接选配置并后台构建、拉取或检查镜像；研究配置默认包含 Exa 与固定版本 Agent-Reach 工具链。

视频环境与联调见 [VIDEO-APP](docs/VIDEO-APP.md)。

## 应用接入与架构

- [应用 API v1](docs/API-V1.md)：认证、任务、会话、事件、文件、错误、幂等与最小示例。
- [系统架构](docs/ARCHITECTURE.md)：平台、后端适配、Kun 内核与执行环境的职责。
- [后端能力](docs/BACKENDS.md)：Kun/Codex 差异及当前扩展边界。
- [轨迹与调试](docs/DEBUG.md)：从查看证据到单步、分叉和验证。
- [统一工作台与采集](docs/UNIFIED_DEBUGGING.md)：Codex 请求/响应、结构化查询与直通代理的配置和边界。
- [当前状态](docs/STATUS.md)、[验证记录](docs/VALIDATION.md)、[升级说明](docs/UPGRADING.md)。

## 执行边界

Kun 的 Bash 默认关闭，启用后使用宿主账户权限；文件工具的路径限制不构成 OS 沙箱。当前托管 Docker 路径适用于 Codex，Kun 容器适配仍待实现。Codex 没有 Kun 的内部单步或完整上下文快照能力。任务完成、工具成功与业务目标达成都分别记录；未知结果不会被当作成功或自动重放。

## 致谢

感谢 [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG) 及其贡献者、Mario Zechner / Pi，以及 [HarnessRouter](https://github.com/HarnessRouter/harnessrouter)。Kun 选择性参考 PiG 并独立发展；运行层抽象参考 HarnessRouter/UHP 的应用与后端边界。具体源码来源、设计参考和许可见 [UPSTREAM](docs/UPSTREAM.md) 与 [LICENSES](LICENSES)。没有声明完整 PiG 或 UHP 兼容。

[许可协议](LICENSE)
