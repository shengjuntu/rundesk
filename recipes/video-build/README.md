# 文字驱动视频制作

Codex自主分镜、选材与动画制作；默认HyperFrames、GSAP、Chromium、FFmpeg与中文字体。TTS、图片/视频生成及工程提交/检查/渲染由Video App MCP提供；video-use为可选旧项目兼容。

This is a generated Docker build context, not a built image. Dependencies are downloaded during build.
The Codex version is fixed in Dockerfile; no build arguments are needed for RunDesk WebUI.
Upload this ZIP with Dockerfile at its root, build, then explicitly select the image for the application.
Skills live in /etc/codex/skills; seed/config.toml initializes missing config only. Existing config is not merged.
Use a new environment or apply the selected MCP configuration explicitly. Configure Codex authentication at runtime.
Run python3 /opt/rundesk-profile/tools/profile_doctor.py --json inside the deployed container.
Agent-Reach uses reviewed vendored source with the duplicate wheel force-include removed; no GitHub source download is needed during build.
Other Python/npm/apt dependencies still require their package registries; this is not a fully offline image build.
Agent-Reach includes gh, mcporter, bird, undici and its Codex skill; mcporter seeds Exa into the runtime HOME on first use.
Cookies, proxies and optional external platform MCP services are configured at runtime; no Docker socket is bundled. video-build includes system Chromium for local HyperFrames checks.
Run agent-reach doctor after startup to check live channels; it is not a model or research acceptance test.
