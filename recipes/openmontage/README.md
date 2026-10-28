# OpenMontage（独立上游）

保留 OpenMontage 原生 pipeline、导演 Skills、Remotion/HyperFrames 制作方式；TTS、图像和视频生成经 Video App MCP。独立于自研 video-build。

This is a build context, not an image or a model acceptance test. Build dependencies and any pinned sourceURL download occur at build time. Use RunDesk to create a dedicated Codex Docker instance/workspace. Configure VIDEO_APP_MCP_URL, VIDEO_APP_TOKEN and VIDEO_APP_PROJECT_ID in this instance's video_app_media MCP runtime environment; no credential is embedded. RunDesk does not automatically forward host exports into a managed Docker container. The pinned upstream sources (scope: complete-repository) and native skills remain available under /opt/upstreams/openmontage. Existing mounted HOME configurations are not silently rewritten: explicitly add the video_app_media MCP entry from seed/config.toml to an existing environment.

Run python3 /opt/rundesk-profile/tools/profile_doctor.py --json for offline checks. Native rendering stays in the upstream environment; generated media comes from Video App.
