# 升级到当前版本

当前版本 RunDesk 0.52.0，配套独立 Kun 0.17.0、worker 协议 16、fork schema 3；视频工具 profile 为 video-build 1.4.0。

1. 等待活动任务结束，停止原服务，备份数据目录、工作区、Codex HOME/配置、独立 Kun 执行日志及服务环境设置。
2. 在 RunDesk 源码执行 `make build`，本包只构建宿主；Kun 在其独立源码目录执行 `go build -o bin/kun ./cmd/kun`。将 Kun 放在 RunDesk 可执行文件旁，或通过 `--kun /absolute/path/to/kun` 指定。RunDesk 使用轻量 SDK 快照构建，不再包含 `cmd/kun` 或引擎源码。
3. 保留原系统用户、`--data`、`--codex`、`--kun`、CODEX_HOME 与服务参数。启动后用 `rundesk --version` 和 `kun --version` 核对，再刷新浏览器；旧会话继续读取，缺少或不兼容 Kun 明确报错，不自动切到 Codex。
4. 本版不会自动安装 Codex、切换后端、启用 Bash 或开启 HTTP 观测。更换引擎后新建会话，历史会话保持原归属。HTTP 观测保留已有会话开关和默认关闭语义。

0.49.4 已将 Kun 拆分为独立工程；从更早版本升级要分别部署新宿主与独立 Kun。Kun 0.17.0 对既有 0.16.0 执行目录、checkpoint/fork 的兼容范围见 [拆分说明](KUN-SEPARATION.md)，不手工改 manifest 或绕过版本守卫。需要重新生成的预览或独立验证按当前内核重做，历史只读证据保留。

重启不会重放已开始或结果未确认的任务；排队任务保留。先检查原回执、任务状态与副作用证据再决定后续操作。一个可靠本地文件系统的数据目录只运行一个 RunDesk；不要删除运行中的 server.lock 来并行启动服务。

验证记录按实际版本与执行范围保存；0.49.4 的普通全量测试和局部 race 结果不是 0.49.5 全量 race 验收。当前能力与本版实际测试见 [STATUS](STATUS.md) 和 [VALIDATION](VALIDATION.md)。

## 0.52.0：构建中心与原生插件准备

保留原数据目录升级并刷新浏览器。构建中心读取原 setup 和应用构建记录，原有应用/实例、工作区、HOME、配方及制作入口不自动迁移。自定义配方保存在数据目录，无需重编译；删除构建记录不删除 Docker 镜像，删除镜像不删除项目或 HOME。旧历史若没有冻结 context.zip，重建会明确失败，应重新导入原输入。

“让 Codex 添加”需要已经可用的 Codex 实例和模型认证，使用新专用工作区。Docker 助手需要配置容器可达的 `RUNDESK_BUILD_MCP_ORIGIN` 或 `--public-url`，不得把容器 localhost 当作宿主。构建权限限当前任务并到期失效；不传 Docker socket 或宿主管理员令牌。

插件页和 ZIP 检查均只读。本版不调用尚未稳定的 plugin/list/read/install/uninstall，也不把展开 Skill 当作原生插件已安装。Skill bootstrap 会在回执明确标注。详见[构建中心](BUILD-CENTER.md)与[原生插件](NATIVE-PLUGINS.md)。

## 0.51.0：三个独立上游视频配置

自研 `examples/video-build` 及已有实例不迁移、不切换。新增 hyperframes、video-use、openmontage 三套独立配置，构建新镜像后建立专用实例和工作区，运行时配置其 `video_app_media` MCP。完整上游代码或必要 Skill 快照按各自固定来源保留。OpenMontage 的完整源码在镜像构建时下载并验证摘要，prepare/pack 不代表镜像已构建。

调用任意文本 TTS 需升级 Video App 至 0.26.0；已有配音、生成服务、项目和凭据保留。三个上游从 RunDesk 独立会话运行，App 自研导演入口保持原流程。详见 [独立视频环境](UPSTREAM-VIDEO-ENVIRONMENTS.md)。

## 0.50.0：应用多运行环境预设

原应用的默认实例/工作区、会话、HOME、认证和凭据保持原绑定，不自动创建或迁移预设。管理员在应用配置内登记命名预设，为每个预设配置独立 Codex Docker 实例和工作区。省略 presetId 的客户端保持原行为；旧应用凭据不会自动获得新预设权限，需要重新签发包含 presetIds 的凭据。

配套 Video App 0.25.0 可用 --production-profile 或 VIDEO_PRODUCTION_PROFILE 指定新项目默认预设，项目会保存选择与会话绑定；改变启动默认不迁移旧项目。App 自身渲染容器、IndexTTS与ComfyUI独立运行。预设名称不自动安装OpenMontage/video-use或转换既有剪辑工程；管理员需提供实际可用镜像和适配工具。

具体配置、禁用规则和权限见 [多运行环境预设](APPLICATION-RUNTIME-PRESETS.md)。本轮具体验证见 validation-0.50.0.json，旧日志保留原版本边界。

## 0.49.5：HyperFrames 自主导演工具镜像

配套 Video App 0.23.0、video-build 1.4.0。新工具镜像默认提供 HyperFrames 0.8.87、GSAP 3.15.0、Node.js、FFmpeg 和字体，原创 Skill 支持 Codex 自主分镜、选材、生成、动画和检查。App 渲染镜像与 RunDesk 工具镜像分别更新；新的主流程通过 App MCP 保存源工程与素材 ID，无需共享 App 私人数据或额外生产目录。

重新构建或选择真实已有的 video-build 1.4.0 镜像，在 RunDesk 显式选择并更新/重建应用运行环境；保留原工作区、HOME、Codex 认证和 MCP 配置。仅替换源码包、App 或镜像标签不会更新已运行容器，不会自动迁移旧工程后端。

video-use 改为可选。已有 video_use EDL 工作流继续使用 adapter contract 2、固定上游 MIT 资源及原专用共享目录；需要它时按 profile 配方显式使用 `--with-video-use` 构建。默认 HyperFrames-only 镜像没有这项可选依赖，不能用于误判旧视频工具已齐备。实际构建选项和检查见 [video-build](../examples/video-build/README.md)。

环境诊断和离线媒体烟测分别验证工具、路径/权限、图片传输或本地短动画，不会启动模型，也不证明模型视觉、IndexTTS 音质或真实新闻制作通过。真实部署、生成服务与自主创作仍需用材料验收；不将历史日志当成本轮实际测试。

---
以下保留历史版本升级说明。

## 0.47.0 升级到 0.47.1

先停止研究并保留原数据目录，替换 RunDesk/Kun 程序；Kun 内核版本不变。升级 news2douyin 到 0.13.1，在研究设置选择 Docker 完整访问并“连接并配置环境”，为既有环境补齐运行权限及持久 MCP 审批。仅更新程序不会自动放宽旧实例权限，也不会自动接受已弹出的审批。现有镜像中的 Codex 若支持服务级审批配置，无需仅为此次设置修改重建镜像；不支持时构建明确版本后显式更新环境。

## 0.48.0：Exa 与初始化镜像准备

替换 RunDesk 二进制；Kun 0.16.0 不变。停止旧服务并使用相同数据目录启动，打开初始化向导准备 `news2douyin` 镜像。镜像工具层已改变，旧镜像不会被原地补齐；首次请构建或拉取新配方 1.1.0 的镜像。

配套 news2douyin 升级到 0.13.2。新研究设置从 Wigolo 迁移为 Exa；“连接并配置环境”会启用 Exa 并停用旧 Wigolo，同时保留其他 MCP、自定义 Exa 地址和凭据。正在执行的旧任务仍保留历史后端快照，应先结束后再切换环境。若已有环境绑定另一镜像，需在 RunDesk 显式更新，不会通过重连悄悄替换。

首次无需公共仓库账户即可本机构建；公开镜像仍需由发布者实际构建、验收并推送，本源码包未发布公共镜像。

## 0.49.0：统一调试与直通观测

替换 RunDesk 程序并刷新浏览器，原来独立的轨迹、只读检查和 Kun DevTools 入口统一为“调试工作台”。历史数据继续读取，Kun 版本和协议不变；旧版本未保存的模型 HTTP 正文不会补回。新观测以附加事件写入现有数据库，并增加事件索引。

0.49.0 的代理默认启用并监听随机回环端口；0.49.1 的默认值与会话开关见下一节。支持的 Codex provider 使用线程级地址覆盖，不永久修改其模型配置。内置 provider 使用原生 `openai_base_url`，明确认证路由后才推导默认上游；无法确认时保留原连接并标记缺口。要关闭新增正文采集，启动时传 `--observe-http=false`。正文副本可能包含提示词、业务资料和工具参数，应纳入原数据目录的访问与保留策略。

Docker 场景显式配置 `--observe-listen` 与 `--observe-advertise`，确认容器到宿主的私有网络可达；没有配置时保留原模型连接并标明采集缺口。不要把“configured”当作真实模型链路已验证。完成一轮后检查请求、响应、采集标记，再判断该认证/网络路径是否满足调试要求。详细操作见 [统一调试与采集](UNIFIED_DEBUGGING.md)。

## 0.49.1：默认关闭与会话采集设置

HTTP 采集默认改为关闭，代理按需启动。`--observe-http=true` 仅设置以后新建 Codex 会话的默认值；它不覆盖已有会话保存的 `observeHTTP`，重启也不改变保存值。0.49.0 的旧会话没有该字段时按关闭处理；其历史观测仍可查询。

需要采集时，在当前 Codex 会话的调试工作台开启，或 PATCH 会话的 `observeHTTP`。运行中可以保存偏好，当前请求保持原设置继续；空闲修改也在下一轮提交生效。下一轮自动重连并恢复原 Codex thread，应用新设置。`observation.pending` 表示偏好待应用，`appliedEnabled` 表示原生线程已确认配置代理；即使保存为开启，不支持采集时后者仍为 false。等待下一轮不代表正在采集。

关闭的会话不进行采集配置发现、不设置代理地址。服务启动后，在首次处理已启用线程之前，仅指定监听参数不会创建代理监听器。Kun 不允许启用这个 Codex 专用开关，其原生调试证据继续使用既有路径。无需迁移旧证据或重建数据库；Kun 版本与 worker 协议不变。
