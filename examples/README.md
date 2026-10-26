# 典型运行配置

RunDesk 0.51.0 的初始化向导可在应用注册前准备可复用镜像。镜像包含 Codex 和工具环境，不包含 news2douyin 应用本体、业务数据库或账号。Kun 内核不变。

| 配置 | 默认工具 | 搜索 |
| --- | --- | --- |
| [news2douyin](news2douyin/README.md) | Agent-Reach、gh、mcporter、bird、undici、Python、RSS、yt-dlp、FFmpeg、Pillow、中文字体 | Exa |
| [people-search](people-search/README.md) | Agent-Reach 工具链、Python、公开网页/RSS | Exa |
| [video-build](video-build/README.md) | FFmpeg、ffprobe、Pillow、中文字体 | 无预置搜索 MCP |
| [hyperframes](hyperframes/README.md) | 上游原生 Skills、HyperFrames、GSAP、Chromium | Video App 生成 MCP |
| [video-use](video-use/README.md) | 上游原生 Skill、EDL 与剪辑 helpers | Video App 生成 MCP |
| [openmontage](openmontage/README.md) | 上游指南、pipelines、tools、生成 provider 接入 | Video App 生成 MCP |

自研 video-build 保持原样。三个新目录分别对应上游项目，独立构建镜像和创建实例；不会自动把 App 一键制作改成这些上游流程。入口、服务配置和边界见 [独立视频环境](../docs/UPSTREAM-VIDEO-ENVIRONMENTS.md)。

## 推荐：初始化向导

1. 在 RunDesk 打开“开始使用 RunDesk”。仅使用容器时，运行后端选“仅使用 Docker 应用”，无需宿主机 Codex、Node 或 Python。
2. 展开“应用 Docker 环境”，选新闻研究、Codex 精确版本和镜像标签。默认标签为 `rundesk-news2douyin:exa-agent-reach`。
3. 选择“本机构建”“从仓库拉取”或“检查已有镜像”。拉取必须填写真实已发布地址；本项目尚未预置公共镜像。
4. 点击“准备镜像”。后台串行执行，日志持续可见，关闭页面不取消；重开向导可以继续查看、取消、删除终态记录，失败后按原配置重试。服务重启会将未完成任务标为 interrupted，不自动重复执行。
5. 显示“镜像就绪”后，将镜像地址填入 news2douyin 研究设置，连接并完成应用环境的模型认证，再做真实搜索和研究验收。

准备成功不修改任何已有应用的镜像、认证或业务目录。旧环境换镜像仍需显式更新；更换镜像不会覆盖现有 MCP 配置。

初始化只在用户点击时构建/拉取，不在每次启动时强制下载。相同配方、相同本机镜像 ID 已验收时跳过重复构建并重新检查工具；拉取模式优先检查兼容本地缓存。需要仓库更新时使用新版本标签或 digest。

## 统一 CLI

CLI 要求 Python 3.10+；生成上下文无需 Docker、网络或凭据。将 `X.Y.Z` 换成实际选择的 Codex 版本，拒绝 latest。

```bash
python3 examples/profiles.py list
python3 examples/profiles.py inspect news2douyin
python3 examples/profiles.py validate news2douyin
python3 examples/news2douyin/run.py pack --codex-version X.Y.Z --output ./build/news2douyin.zip
python3 examples/news2douyin/run.py build --codex-version X.Y.Z --image rundesk-news2douyin:exa-agent-reach
python3 examples/news2douyin/run.py doctor --image rundesk-news2douyin:exa-agent-reach
# 仅需 Exa 和基础新闻工具时，可显式生成较轻配置（不适用向导的完整工具验收）
python3 examples/news2douyin/run.py pack --without-agent-reach --codex-version X.Y.Z --output ./build/news2douyin-lite.zip
```

news2douyin 和 people-search 默认包含 Agent-Reach；保留 `--with-agent-reach` 兼容旧命令。Wigolo 选项已移除。Windows 可用 Python 生成 ZIP，RunDesk 托管 Docker 仍要求 Linux 本机 Engine。CLI build 默认超时 1800 秒，可用 `--timeout` 调整；初始化任务上限 60 分钟。

## 构建与配置

- npm 工具使用固定版本及 `package-lock.json` / `npm ci`；Agent-Reach 固定源码提交。apt、pip、npm 配置有限重试和下载超时。工具依赖层位于 Codex 层之前，升级 Codex 时复用缓存。
- `profile.lock.json` 记录组件选择，`SHA256SUMS` 记录上下文摘要。基础镜像、apt 与 Python 传递依赖未完全锁定，不宣称离线构建或位级可重现。
- Skills 放在 `/etc/codex/skills`，不会被挂载 HOME 遮挡。Codex Exa 配置放在 `/opt/rundesk-seed/config.toml`，只初始化缺失配置。
- mcporter 第一次调用时在当前运行用户的 `HOME/.mcporter` 建立 Exa 配置，保留已有 JSON/JSONC 配置。它与 Codex 原生 MCP 配置独立，修改认证地址时需分别设置。
- Cookie、代理、平台登录和可选独立 MCP 服务留到运行时配置；不往镜像中写入账号，不挂载 Docker socket，不启动嵌套 Docker。

## 验证与开发

初始化的离线检查使用只读根目录、无网络、非特权 UID，检查 Codex、工具版本、Skill 路径和配置。`agent-reach doctor` 是运行时联网渠道检查，不等于模型调用、证据质量或业务 MCP 验收。真实部署仍需检查认证、Exa 工具列表、一次检索和原文引用。

内置构建 ZIP 由同一 CLI 编译生成；Go 二进制直接嵌入，无需在用户主机运行 Python。修改配方后执行：

```bash
python3 scripts/embed-profiles.py
python3 scripts/embed-profiles.py --check
python3 scripts/test_profiles.py
```

[Agent-Reach 评估](news2douyin/AGENT-REACH.md) · [初始化说明](../docs/SETUP.md) · [镜像构建](../docs/BUILDS.md) · [应用 API](../docs/API-V1.md)
