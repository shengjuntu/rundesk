# Video Use：独立上游剪辑环境

此目录直接封装 [browser-use/video-use](https://github.com/browser-use/video-use) 的原生 Skill、helpers、EDL 和 FFmpeg 制作流程。它与我们自己的 [`video-build`](../video-build/README.md) 平行，后者保持不变。这里不使用自研 `video_use_adapter.py` 协议2，也不把编辑任务改写成 Video App 的私有 DSL 或 HyperFrames 工程。

上游固定在 `43cfc566833548093455c9d4a46bd69913b7e552`；完整36文件源码归档在 `vendor/`，SHA-256 写入 `upstream.json`。归档包含 MIT LICENSE 与 Browser Use 版权声明；镜像中保留完整源码与许可。接入说明独立维护，上游源码不打补丁。

## 构建和启动

```bash
python3 examples/video-use/run.py prepare --codex-version X.Y.Z --output ./video-use-context
python3 examples/video-use/run.py build --codex-version X.Y.Z --image rundesk-video-use:1.0.0
python3 examples/video-use/run.py doctor --image rundesk-video-use:1.0.0
```

将 `X.Y.Z` 换成实际部署的精确 Codex 版本。`prepare` 只生成可检查上下文；`build` 才运行 Docker 构建。源码随上下文提供，Python 直接和传递依赖在 `requirements.lock` 锁定；Debian 基础镜像及 apt 包未完整固定。默认不安装可选 Manim、HyperFrames 或 Remotion；按上游方式在实际需要的动画工程内单独准备，不在每次任务启动时安装。

在 RunDesk 为此镜像建立专用 Codex Docker 实例和工作区，也可以登记为应用运行预设。到 **RunDesk 的该实例中发起对话**，使用 `$edit-with-video-use`；它加载 `/opt/upstreams/video-use/SKILL.md`，原生 Skill 同时以 `/etc/codex/skills/video-use` 注册。模型认证和工作区沿用 RunDesk 管理；无需嵌套 Docker 或 Docker socket。

Video App 的“一键初稿”仍是我们自研 Video Builder 导演流程；本目录没有把该按钮自动切换为 video-use。此独立实例从 RunDesk 发起，Video App 在这里承担生成服务。原生结果先保存在该工作区和 RunDesk 可查看的产物目录，不声称自动成为 Video App 的成片版本。

## 生成服务接到 Video App

镜像预置 `video_app_media` stdio MCP，由共享 `video_app_media.py serve-mcp` 提供；其生成调用指向已运行的 Video App HTTP MCP。在 RunDesk 该 MCP 的运行配置中提供：

| 运行变量 | 内容 |
| --- | --- |
| `VIDEO_APP_MCP_URL` | 从 Codex 容器可访问的完整地址，例如 `http://host.docker.internal:3220/api/mcp` |
| `VIDEO_APP_TOKEN` | Video App 访问令牌；运行时注入，不写入镜像或源码 |
| `VIDEO_APP_PROJECT_ID` | 已存在、供这些生成作业归档的 Video App 项目 ID |

这三个值只控制生成连接，不把视频编辑状态迁入 Video App。先读取实际能力、项目和工具 schema，再发起 TTS、图片或视频作业；CLI/MCP 读取真实作业和文件，使用稳定 request ID，下载后记录来源与 SHA-256。IndexTTS、图像和视频模型由 Video App 当前部署配置决定，不再在此实例另接生成厂商。

脚本也可使用：

```bash
python3 /opt/rundesk-profile/tools/video_app_media.py discovery
python3 /opt/rundesk-profile/tools/video_app_media.py call video_project_get --arguments '{}'
```

生成配音、图片与视频返回真实本地文件，再由原生 video-use/FFmpeg 编辑。静态图片需先转为实际片段；新的配音需检查真实时长后与素材合成，不虚构原生 EDL 的轨道字段。详细调用和恢复说明随接入 Skill 提供。

## 保留原生剪辑与 ASR 边界

输入是工作区里的真实素材。上游按 `transcripts` → `takes_packed.md` → 自主剪辑决策 → `edl.json` → `helpers/render.py` → 输出自评工作，结果位于 `<素材目录>/edit/`，会话记忆位于 `edit/project.md`。

video-use 的转写依赖 **ElevenLabs Scribe ASR**；它与 TTS 是两种服务。可以复用真实已有的逐词转写；确需转写时，另外在运行环境配置 `ELEVENLABS_API_KEY`。Video App 生成 MCP 当前不提供 ASR，不能用原稿字数冒充词级时间戳，也不为没有转写需求的任务要求 ASR Key。安装检查不会发起付费转写。

上游策略确认、自评和艺术自由继续保留；已明确批准的任务按实际授权执行。TTS/图片/视频生成来源由接入 Skill 指向 Video App，实际编辑、动画选择、字幕和产物仍服从上游工作方式。

## 已验证范围

- 上游原生26项单元测试通过，日志在 `validation/upstream-tests.log`。
- 未修改的 `helpers/render.py` 使用原生 EDL 在宿主实际剪切和拼接测试素材，生成 H.264/AAC、1280×720 视频；抽样帧不同，解码音频非静音。报告和测试视频在 `validation/native-render.json`、`validation/native-final.mp4`。这是测试音调与测试画面，不是新闻制作质量验收。
- 接入 Skill 元数据、Python 语法和安装脚本语法已检查；容器内 `doctor.py` 只核对完整源码、固定依赖和原生 helpers 启动，不调用生成或 ASR。

当前环境没有真实 Docker Engine；未实际构建或启动该镜像，也未调用真实 Codex、IndexTTS、图像/视频模型或 Scribe。后续部署需分别验收容器工具、生成连接和一条完整剪辑任务。
