# 自研 Video Builder 与三个独立上游环境

RunDesk 0.51.0 新增三个独立运行配置，不改变 `examples/video-build` 的制作流程和目录内容。

| 目录 | 使用的制作方式 | 与 Video App 的关系 |
| --- | --- | --- |
| `examples/video-build` | 我们自己的 Video Builder：Codex、领域 MCP、工程和归档流程 | 保留现有一键初稿、对话修改和导出 |
| `examples/hyperframes` | 上游 HyperFrames 的原生 Skills、HTML 工程和 CLI 渲染 | 调用 App 的生成服务，下载素材进入原生工程 |
| `examples/video-use` | 上游 video-use 的 Skill、原生 EDL 与剪辑 helpers | 调用 App 的生成服务，不走自研 adapter 2 |
| `examples/openmontage` | 上游 OpenMontage 的指南、pipelines、tools 和 selectors | 通过独立 provider 模块调用 App 的生成 MCP |

三套上游环境分别构建镜像，使用独立 Codex Docker 实例、工作区和认证配置。它们用于直接运行、比较和研究上游方法；自己的 Video Builder 以后按需要吸收优点，不自动随上游更新。

## 生成服务与制作入口

本轮三个上游的制作入口是 **RunDesk 中的独立实例与会话**。选择各自的接入 Skill，再读取已固定版本的上游说明。不要从 Video App 的自研导演按钮启动后，就认定已经切换为上游完整流程。

Video App 在这里承担生成服务：IndexTTS 配音、已配置的图像生成、ComfyUI 或其他已配置的视频生成。生成出的媒体归档在指定 App 项目内，随后按认证下载为上游工程中的本地素材。上游继续使用自己的剪辑、工程格式与渲染器，成片与中间文件保存在该实例工作区，可通过 RunDesk 查看。

独立文本配音需要 Video App **0.26.0**：`video_job_start` 的 `tts` 支持 `options={"source":"text","text":"要合成的文字"}`。它不会替换项目正文、镜头或剪辑工程，也不允许把该独立任务直接采用为项目主播稿。图片和视频生成继续使用既有任务接口。

没有配置服务时明确失败，不自动切到 Piper、HeyGen、fal、本地大模型或其他生成提供方。配音不是 ASR；video-use 的原生逐词转写如需 ElevenLabs Scribe，仍需另行配置，或提供已有转写。App 未提供的音乐、音效和转写生成能力不在本轮承诺内。

## 构建与实例配置

在初始化向导选择对应运行配置，或在源码根目录使用各目录的 `run.py`。以 HyperFrames 为例，将版本换成实际选择的 Codex 精确版本：

```bash
python3 examples/hyperframes/run.py prepare --codex-version X.Y.Z --output ./hyperframes-context
python3 examples/hyperframes/run.py build --codex-version X.Y.Z --image rundesk-hyperframes:local
python3 examples/hyperframes/run.py doctor --image rundesk-hyperframes:local
```

`video-use` 和 `openmontage` 使用各自目录，不能用 `video-build --with-video-use` 代替新的独立配置。`prepare/pack` 生成上下文；构建依赖网络和 Docker，构建成功后仍须在 RunDesk 显式选择镜像、建立专用实例和工作区，并完成 Codex 认证。可复用 0.50.0 的命名运行预设机制，但登记名字本身不会构建镜像。

每个实例只配置它自己的 `video_app_media` MCP。镜像不包含 App Token；运行时在该实例的 MCP 配置中设置：

```toml
[mcp_servers.video_app_media]
command = "python3"
args = ["/opt/rundesk-profile/tools/video_app_media.py", "serve-mcp"]

[mcp_servers.video_app_media.env]
VIDEO_APP_MCP_URL = "http://host.docker.internal:3220/api/mcp"
VIDEO_APP_TOKEN = "运行时填写实际 App Token"
VIDEO_APP_PROJECT_ID = "运行时填写指定 App 项目 ID"
```

地址必须能从该工具容器访问 App。宿主机的环境变量不会自动进入容器，不能只在宿主执行 export 就认为配置完成。共享客户端只读取本实例的上述 MCP 配置；接入客户端的工具与下载限制在指定项目，不暴露自研 DSL、导演流程或渲染操作。这是客户端约束；App 仍使用已有全局 Token，不等于服务端项目级凭据或操作系统隔离。已有实例的配置不会被镜像升级覆盖，需要管理员明确应用配置。

## 上游源码、许可和验收

每个目录记录独立上游来源、精确提交、依赖和源码摘要。原生 Skill 与必要来源文件保留在镜像中；接入层另行维护。OpenMontage 的完整固定源码在构建时按指定摘要取得，源码和对应接入模块保留其许可。不要把这些源码当成自研 Video Builder 的同名实现。

离线 doctor 检查工具、版本、源码、Skill 和原生工具启动，不运行完整渲染、模型或真实生成服务。实际宿主渲染测试另列在对应 validation 中。实际验证范围见 [validation-0.51.0.json](validation/archive/validation-0.51.0.json)。构建配方或本地测试通过不代表 Docker 镜像、Codex 认证、GPU 服务和完整上游制作任务已经实机验收。
