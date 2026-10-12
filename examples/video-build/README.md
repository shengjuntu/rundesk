# video-build

提供 FFmpeg、ffprobe、Pillow、Noto CJK 字体，以及 `build-video-artifacts` Skill。把已有图像、音频和字幕整理为视频，保存命令、输入指纹、输出与验收记录。没有预置搜索 MCP、TTS 或生成视频模型。

统一入口：`python3 examples/video-build/run.py <命令>`。完整说明见 [配置目录说明](../README.md)。

部署后先执行无需外部素材的媒体实测：

```bash
python3 /opt/rundesk-profile/tools/video_smoke.py --out ./video-smoke-001
```

这会生成两秒 360×640 H.264/AAC 视频及 render.json、probe.json，并核对分辨率、编码、音频流和时长。输出目录已存在时拒绝覆盖。

业务任务示例：把已提供的三张图片、一段旁白和字幕合成为 1080×1920 视频，保存实际渲染命令并检查时长和中文字体。还应抽帧检查字幕布局、试听音频；媒体元数据通过不能代替视觉/听觉验收。
