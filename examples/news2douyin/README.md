# news2douyin

新闻研究工具镜像默认使用 **Exa + Agent-Reach**。移除 Wigolo、浏览器下载与本地重排模型；不包含 news2douyin 应用本体、TTS、发布接口或业务数据。

推荐先在 RunDesk 初始化向导准备镜像，再到 news2douyin `/research/settings` 填入 `rundesk-news2douyin:exa-agent-reach`（或自己的实际标签），点击“连接并配置环境”。完成此环境的模型认证后，即可启动研究。

包含 Python、Node、Git、curl、ripgrep、feedparser、yt-dlp、FFmpeg、Pillow、中文字体，以及 Agent-Reach CLI、gh、mcporter、bird、undici 和上游 Skill。Codex 原生 MCP 与 mcporter 分别配置 Exa。需要 Cookie、代理或额外 MCP 服务的渠道，仍需在运行时配置，不能宣称所有平台开箱可用。

统一入口：`python3 examples/news2douyin/run.py <命令>`，详见[配置目录说明](../README.md)和[Agent-Reach 评估](AGENT-REACH.md)。

配套 news2douyin 0.13.2 的新研究默认 Exa，重连会启用 Exa 并停用旧 Wigolo MCP，同时保留自定义地址与认证信息。已有研究保留原检索来源快照；不要在旧任务执行中更换环境。业务研究先取得检索票据，再调用 Exa 并记录结果，最终由新闻服务保存原文、引用和报告；搜索摘要不作为原文证据。

容器必须能访问新闻服务的 MCP 回调地址。同机 localhost 会转换为 host.docker.internal，但新闻服务仍需监听可达接口；跨主机应显式填写地址。
