# 公开职业资料研究

检索人物公开职业资料、核对同名身份、整理带来源的履历和作品；不收集私人联系方式或自动联系他人。

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
