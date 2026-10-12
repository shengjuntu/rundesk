# news2douyin

为 news2douyin 提供新闻采集、原文/字幕提取、证据记录、文案草拟与基础媒体处理的 Codex 环境。应用本体、TTS 服务和发布接口需要在业务侧接入。

统一入口：`python3 examples/news2douyin/run.py <命令>`。命令和完整部署流程见 [配置目录说明](../README.md)。

- 默认 Exa MCP 搜索；Jina Reader 用于公共网页正文，RSS 用 feedparser，视频元数据/可用字幕用 yt-dlp。
- `--search-backend wigolo` 切到本地 MCP 搜索、提取和缓存，Exa 配置保留但禁用，避免隐式双重检索；见 [Wigolo](WIGOLO.md)。
- `--with-agent-reach` 安装用户指定 fork 的固定提交，作为辅助 CLI，不运行其全自动 installer，不配置 Cookie、代理或所有社交渠道。实际搜索直接走所选 MCP，未配置的平台不宣称可用。
- 附带 `collect-news-evidence` Skill：输出 sources.json、brief.md、script.md 和素材目录，区分发布时间、事件时间、采集时间及未证实信息。
- 附带 FFmpeg、Pillow 和 Noto CJK 字体，可用于明确要求的素材处理；视频生产验收可使用 [video-build](../video-build/README.md) 配置。

部署后可提出：使用 collect-news-evidence 收集过去 24 小时某一技术领域的三条新闻，优先官方来源，保存原文证据，再生成一条 60 秒短视频脚本。真实新闻结果须核对事件日期、至少必要的独立来源、中文/英文覆盖和失败原因。

依赖版本见 [dependencies.json](../_shared/dependencies.json)。公开检索依赖搜索服务和目标网站；平台访问状态与服务限额会变化。

`agent-reach doctor` 检查的是它自己的上游工具配置（如 mcporter），并不等同于 Codex 的 MCP 状态。本配置直接连接 Exa/Wigolo，没有自动安装 mcporter 或其他社交渠道；部署时以 Codex 工具列表和实际读取结果为准。
