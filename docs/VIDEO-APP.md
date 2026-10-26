# Video App 与 RunDesk：自主导演和部署

配套版本：RunDesk 0.49.5、Video App 0.23.0、video-build 1.4.0。RunDesk 保持独立的控制平台与 RDCA，Video App 提供制作工具和专业审阅，Codex 自主分镜、选材、生成、动画和检查。OpenMontage 只作设计参考，没有引入其代码、Schema 或 Skill。

| 组件 | 职责与持久数据 |
| --- | --- |
| RunDesk | 应用、环境、会话、配置、任务与轨迹；原 RunDesk data |
| video-build 工具容器 | Codex、HyperFrames/GSAP、FFmpeg、Pillow、字体与原创 Skill；工作区及 Codex home |
| Video App | HTTP MCP、素材/工程/输出、HyperFrames 渲染与多轨审阅；独立 App data |
| 可选生成服务 | IndexTTS、图像/视频生成；独立配置、模型和凭据 |

## 主制作路径

App 保存主播正文、素材与要求，一键提交原有 RunDesk 专用会话。Codex 使用 video_project MCP 获取项目、能力、真实素材与配音，再自行制作原创 HyperFrames HTML/CSS/JavaScript/GSAP。通过 App 工具保存工程、检查并提交完整渲染；返回真实输出及检查记录。

素材准备与对齐、剪辑、精修是可查看产物，阶段可交错并局部返回，不逐镜头重复确认。文字为内容依据，禁止用示意图冒充现场事实。完整模型交互、工具参数、异常定位和轨迹问答保留在 RunDesk；视频、配音、字幕、音乐的专业审阅在 App。

主流程不要求 App 与 Codex 共享私人项目目录，也不依赖 video-use。素材用真实 assetId 映射到工程相对路径，由 App 冻结与渲染；Agent 不需要读写 project.json。可在工具容器做本地 HyperFrames 动画实验，App 成片仍以实际保存的工程和独立渲染输出为准。

## 升级与运行

1. 备份并保留两边 data、App access-token、RunDesk 身份/会话、Codex HOME/认证与服务配置，等待活动任务结束。
2. 更新 RunDesk 0.49.5。准备 video-build 1.4.0，填写 Codex 精确版本，构建或选择真实已有镜像，再明确更新运行环境；不会在任务开始时自动构建。
3. 更新 Video App 0.23.0 或重建其独立镜像，旧 docker/.env 显式更新 App 镜像标签；保持已有数据挂载。
4. 在 App 连接/更新本版工具，核对当前运行环境实际启用的版本化 Skill、video_project MCP 和模型认证。MCP 地址为可达的 App /api/mcp，Authorization Header 使用 App Token，凭据不写入工程。
5. 用短稿与已有图片做完整制作和局部修改，查看真实工具轨迹、revision、输出和审阅记录。依赖诊断、图片传输和离线烟测分别证明自己的检查范围，不证明模型已理解图像或完成自主制作。

工具镜像默认 HyperFrames 0.8.87、GSAP 3.15.0，不内置图像/视频/TTS 模型或密钥。App 渲染镜像与 Codex 工具镜像分别更新，仅替换源码包或标签不会更换运行中的容器。Profile 使用与本地离线检查见 [video-build](../examples/video-build/README.md)。

旧 native/video_use/镜头项目不自动切换。video_use 兼容工程需要保留带可选 video-use 的镜像、adapter contract 2 及原有专用共享生产工作目录，按原冻结/渲染/归档流程使用；该目录不是 App 私人数据库。新 HyperFrames 路径不用添加这类挂载。

## 网络与检查

沿用现有 Linux host/bridge 网络与回调地址：App 到 RunDesk 通常使用 http://127.0.0.1:3210；Codex 容器通过 http://host.docker.internal:3220/api/mcp 回调 App。远程浏览器入口、端口或网络改变时配置真实可达地址，不把示例当成所有部署通用值。App 无需 Docker socket。

video-build 的离线 doctor/smoke 可以验证本地依赖与真实短动画渲染；不调用模型。App check 是冻结源工程后的浏览器校验，完整渲染再验证实际媒体。多轨是同一工程/输出的审阅，HyperFrames 时间线为作者声明，并非解析动画或实测音频。实际质量仍需播放、抽帧及人审，最终记录绑定具体完整输出。

本次实际执行过的检查见发布验证记录，未执行的 Docker、远端生成和真实模型验收不得由文档推定通过。历史版本技术说明见 Video App 对应版本文档。
