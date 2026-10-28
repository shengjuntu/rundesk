# Video Use · 上游原生剪辑

独立 browser-use/video-use 上游 Skill 与原生 EDL/FFmpeg 剪辑环境；TTS、图片与视频生成走 Video App MCP，不改变我们自己的 video-build。

This is a build context, not an image or a model acceptance test. Build dependencies and any pinned sourceURL download occur at build time. Use RunDesk to create a dedicated Codex Docker instance/workspace. Configure VIDEO_APP_MCP_URL, VIDEO_APP_TOKEN and VIDEO_APP_PROJECT_ID in this instance's video_app_media MCP runtime environment; no credential is embedded. RunDesk does not automatically forward host exports into a managed Docker container. The pinned upstream sources (scope: complete-repository) and native skills remain available under /opt/upstreams/video-use. Existing mounted HOME configurations are not silently rewritten: explicitly add the video_app_media MCP entry from seed/config.toml to an existing environment.

Run python3 /opt/rundesk-profile/tools/profile_doctor.py --json for offline checks. Native rendering stays in the upstream environment; generated media comes from Video App.
