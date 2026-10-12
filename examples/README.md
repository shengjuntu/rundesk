# 典型运行配置

`examples` 是 RunDesk 的典型运行配置目录，同时保留通用 Docker 模板、systemd 部署和应用客户端示例。配置描述的是 **Codex 的工具环境和工作方法**，不包含 news2douyin 等应用本体、业务数据或账号。Kun 内核和服务端 API 不因这些配置而改变。

| 配置 | 内置工具 | 搜索后端 | 适用任务 |
| --- | --- | --- | --- |
| [news2douyin](news2douyin/README.md) | Python、Node、RSS、yt-dlp、FFmpeg、Pillow、中文字体 | 默认 Exa；可选 Wigolo | 新闻、原文、字幕、证据包与短视频准备 |
| [people-search](people-search/README.md) | Python、Node、公开网页/RSS 辅助脚本 | 默认 Exa；可选 Wigolo | 人物公开职业资料与同名消歧 |
| [video-build](video-build/README.md) | FFmpeg、ffprobe、Pillow、中文字体 | 无预置搜索 MCP | 图像、音频、字幕合成与媒体验收 |

## 统一入口

宿主只需 Python **3.10+**。查看、校验、生成和打包无需 Docker、网络或模型凭据。所有成功输出都是 JSON，便于以后由 WebUI 调用；构建日志写到 stderr。

```bash
python3 examples/profiles.py list
python3 examples/profiles.py inspect news2douyin --search-backend wigolo
python3 examples/profiles.py validate news2douyin
python3 examples/profiles.py doctor news2douyin
```

每个目录提供相同的 `run.py`；以下两条等价：

```bash
python3 examples/profiles.py inspect people-search
python3 examples/people-search/run.py inspect
```

Windows 可将 `python3` 换成 `python`。此 CLI 能在 Windows 生成 ZIP；RunDesk 当前托管 Docker 环境仍要求 Linux 本机 Engine。

## 生成、构建与部署

先用你部署环境的 `codex --version` 确认具体版本，把下面的 `X.Y.Z` 换成该版本。CLI 拒绝 `latest` 和未指定版本，不会联网替你选择或升级 Codex。

```bash
# 生成可检查的构建目录；输出位置必须不存在
python3 examples/profiles.py prepare news2douyin --codex-version X.Y.Z --output ./build/news2douyin

# 生成 ZIP，根目录直接包含 Dockerfile；可上传现有 RunDesk 镜像构建页
python3 examples/profiles.py pack news2douyin --codex-version X.Y.Z --output ./build/news2douyin.zip

# Wigolo 模式，可另加 --with-agent-reach 安装固定提交的辅助 CLI
python3 examples/news2douyin/run.py pack --search-backend wigolo --codex-version X.Y.Z --output ./build/news2douyin-wigolo.zip

# 宿主已有 Docker 时，直接构建并执行断网运行检查
python3 examples/profiles.py build video-build --codex-version X.Y.Z --image rundesk-video:local
python3 examples/profiles.py doctor video-build --image rundesk-video:local
```

`build` 使用当前 Docker CLI 上下文；如设置 `RUNDESK_DOCKER_HOST` 则显式使用该端点。默认超时 1800 秒，可设置 `--timeout`（1–7200）。超时/取消不保证 daemon 端回滚，不自动重试或清理 Docker 缓存。构建后按不可变 image ID 检查，不自动登记到 RunDesk、不切换应用镜像、不启动业务任务。

在 RunDesk 中上传 `pack` 输出，Dockerfile 路径填 `Dockerfile`，构建成功后显式选择镜像版本并更新项目环境。生成文件已填好 Codex 版本，因此现有页面无需增加 build args。详见 [镜像构建](../docs/BUILDS.md)。

## 配置与持久化

- 工具装在镜像中，Skills 放在 `/etc/codex/skills`，不会被运行时 HOME 挂载遮挡。这是 Codex 当前官方 admin skill 目录；较早版本若不支持，应更新版本或通过 RunDesk 的 Skill 管理导入原目录，并验证 `skills/list`。
- MCP 初始配置放在 `/opt/rundesk-seed/config.toml`。RunDesk 只初始化缺失文件，且已完成 seed 的环境不重复复制；**更换镜像不等于覆盖已有 MCP 配置**。新建环境或在应用配置中显式调整；从 `prepare` 结果的 `seed/config.toml` 查看所需配置。
- Codex 登录、API Key、Cookie 和业务文件在运行时配置，不写入 profile、Dockerfile、ZIP 或镜像。默认 Exa 为免 Key 限流模式；付费/认证连接通过应用 MCP 配置完成，不自动继承宿主环境变量。
- Wigolo 使用当前挂载 HOME 下的 `.wigolo`，保存缓存和按需下载模型。镜像预装浏览器；无 `/data` 匿名卷、不挂载 Docker socket、不启动嵌套 Docker。首次模型下载仍需外网和磁盘空间。
- 网络使用 RunDesk 的 `bridge` 模式；`video-build` 的素材处理本身可离线，但 Codex 模型仍需要其网络服务。资源建议见 manifest，Wigolo 建议从 4 GiB 内存开始按实测调整。

## 面向未来 WebUI 的契约

每个 `profile.json` 遵循 [profile.schema.json](profile.schema.json)。`schemaVersion: 1` 描述标识、版本、运行后端、工具、搜索选项、技能、产物和资源建议。新 UI 可先读取 `list/inspect`，让用户选择版本与组件，再提交 `pack` 的构建 ZIP；本轮没有新增配置市场页面或服务端执行接口。

生成目录包含 `profile.lock.json`（选择及固定版本）、`SHA256SUMS`（逐文件摘要）和第三方说明。`lock` 不包含网络锁定后的传递依赖，不能据此宣称完全可重现或离线构建。CLI 从白名单文件生成上下文，不打包整个工作区，也不会捎带本地凭据。

退出码：0 成功；1 doctor 或已构建镜像运行检查不通过；2 输入、依赖或构建失败。不要只根据生成 ZIP 成功判断镜像可用。

## 验收

先跑宿主 `doctor`，再构建，再跑镜像 `doctor`。后者断网验证 Codex 启动、依赖、字体/浏览器等；不调用模型、不做 MCP 真实握手，也不代表新闻搜索质量通过。部署后依次确认认证、Skills 可见、MCP 工具出现、一个真实搜索及带证据的业务任务。

[Wigolo 适配说明](news2douyin/WIGOLO.md) · [当前验证](../docs/VALIDATION.md) · [基础 Docker](docker/README.md) · [systemd](systemd/README.md) · [应用 API](../docs/API-V1.md)
