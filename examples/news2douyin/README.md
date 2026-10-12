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

## 配套 news2douyin 0.13.0 一键研究

RunDesk 0.47.0 已提供应用运行环境准备接口。先用本目录的 `pack`/`build` 构建固定 Codex 版本的镜像；在新闻应用 `/research/settings` 选择 Docker、填写已构建镜像、选择匹配的 Exa/Wigolo，点击“连接并配置环境”。该操作登记应用，并在实际 Docker 配置中安装缺失的业务 MCP 与研究 Skill。首次模型认证仍在 RunDesk 该应用环境中完成。

新闻服务必须可被容器访问。同机 localhost 默认换成 host.docker.internal，服务仍需监听可达接口；跨主机在“Docker 回调地址”显式填写可达基础地址。之后新闻研究页“启动环境并深度研究”会启动/复用容器、检查认证和工具并开始任务，重复点击复用活动运行。镜像构建、账号登录和镜像更新属于首次部署或管理操作，不在每次研究时自动执行。

业务研究使用 news-research Skill 和受运行编号约束的 MCP 保存结果；本目录的 collect-news-evidence 仍可单独执行文件型收集。业务模式先保存检索票据，再调用 Exa/Wigolo 并记录结果，随后由新闻服务读取原文、核对引用。搜索摘要不是证据；Wigolo 浏览器正文不会自动导入业务证据库。完整步骤随新闻源码的 `docs/DOCKER-RESEARCH.md` 交付。
