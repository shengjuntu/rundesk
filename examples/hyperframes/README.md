# HyperFrames 上游独立环境

本目录直接对应 [heygen-com/hyperframes](https://github.com/heygen-com/hyperframes)，与我们自己的 `../video-build` 并列、分别演进。它运行原生 HyperFrames HTML/CSS/JavaScript 工程与上游 Skills；Video App 只提供共享素材与生成服务，不接管这里的剪辑工程或强制转换成自研 DSL。

## 包含什么

- 固定上游 **v0.8.87 / commit `82ae73a03156f782e48d631907db08232aeed817`**，保留 Apache-2.0 许可证与完整21个发布 Skill 的原始内容、scripts、references 和 assets。
- `vendor/hyperframes-skills-*.tar.gz` 是该提交的 **selected upstream skill source snapshot**，包括 LICENSE、README、package.json、skills-manifest.json 和完整 skills 目录；不是完整仓库。它不包含大体积测试影片、生产回归输出和文档站点。没有修改所选上游文件。
- 实际 CLI/渲染引擎通过独立 `npm/package-lock.json` 安装 HyperFrames **0.8.87** 和 GSAP **3.15.0**，直接与传递依赖锁定。容器包含系统 Chromium、FFmpeg 和中文字体，不放入生成模型或服务凭据。
- 额外 `rundesk-hyperframes` Skill 只规定 RunDesk/Video App 接入方式：TTS、图像生成、视频生成走 `video_app_media` MCP；上游分镜、动画、预览与渲染保持原方式。

`upstream.json` 记录精确来源、快照 SHA-256、安装文件和 native Skill 映射。原始资料安装至 `/opt/upstreams/hyperframes`，native Skills 映射至 `/etc/codex/skills`。安装脚本和集成层单独位于 `/opt/rundesk-upstream`。运行时不修改 source snapshot 或上游 Skill。

## 构建与登记

```bash
python3 examples/hyperframes/run.py prepare --codex-version X.Y.Z --output ./build/hyperframes
python3 examples/hyperframes/run.py build --codex-version X.Y.Z --image rundesk-hyperframes:1.0.0
python3 examples/hyperframes/run.py doctor --image rundesk-hyperframes:1.0.0
```

把 X.Y.Z 替换为实际选择的 Codex 精确版本。上下文生成不下载依赖；显式 Docker 构建安装 npm/apt 包。镜像构建和模型认证是不同步骤。将完成构建的镜像登记为 RunDesk 应用的专用运行预设，使用独立实例、工作区与认证，不接管原 `video-build` 实例。

## Video App 生成服务

运行时配置完整 `VIDEO_APP_MCP_URL`（例如 `http://host.docker.internal:3220/api/mcp`）、`VIDEO_APP_TOKEN` 与真实 `VIDEO_APP_PROJECT_ID`。由独立容器内 Codex 调用 `video_app_media` MCP；其 stdio bridge 通过真实 Video App MCP 执行能力发现、项目/素材读取、TTS、图片/视频生成与下载，不额外实现 Agent 编排循环。

```bash
python3 /opt/rundesk-profile/tools/video_app_media.py discovery
python3 /opt/rundesk-profile/tools/video_app_media.py tts --text '主播原稿' --revision REVISION --request-id REQUEST_ID --output ./media/narration.wav
```

生成素材下载到当前原生工程目录后，由上游 HyperFrames 安排媒体、字幕和动画并渲染。保留实际 job/request/asset 身份和哈希。服务未配置或不支持时明确报错，不回退到上游 HeyGen、Kokoro、mflux 或 LTX 提供方。已有素材和原生 media-use 本地 ingest 仍可使用。CLI wrapper 会在默认 `resolve --type voice/image/video` 进入上游提供方前明确拒绝并提示 Video App 接口，`--from`、缓存复用和候选查询保留原生方式。App 未提供 BGM/SFX 生成，默认解析同样不允许隐式选择其他生成模型；可使用已有本地音乐/音效。

CLI wrapper 直接运行已安装的精确包，设置 `HYPERFRAMES_SKIP_SKILLS=1` 与 `HYPERFRAMES_NO_UPDATE_CHECK=1` 等常量，避免原生 init 自动刷新到 main。Skill 内的 `npx hyperframes` 示例在本环境使用 `hyperframes`，不要在任务中执行 skills update。独立 Node helper 可通过 `HYPERFRAMES_SKILL_NODE_MODULES` 使用已安装包；少数工作流的额外外部工具仍按真实能力检查，不宣称21种工作流全部依赖已齐备。

## 检查范围

`doctor.py` 只检查实际安装的命令、包版本、native Skills 和集成 Skill，不联网、不调用模型、不生成素材，也不渲染视频。本地原生工程的最终 MP4 不自动等于 Video App 已归档的成片版本；没有实际导入回执时不能宣称已进入该历史。

当前交付的实际验证结果由总发布记录说明。源码/锁文件/上下文检查不代替真实 Docker Engine、Codex 认证、IndexTTS、ComfyUI 和新闻视频质量验收。已有 `video-build` 与它的历史验证保持原样。
