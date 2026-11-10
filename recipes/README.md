# 可编辑的运行配方

`recipes/` 是 RunDesk 配方的唯一来源。每个配方目录保存 Dockerfile、Skills、配置和 `recipe.json`；重复的工具与已审阅的本地依赖由 [`common/`](common/README.md) 保存，配方通过顶层 `sharedFiles` 明确引用。Go 在新建构建时直接读取这些普通文件。编辑配方不需要重编译或重启 RunDesk。

| 配方 | 用途与工具 |
| --- | --- |
| [news2douyin](news2douyin/README.md) | Exa、Agent-Reach、公开新闻证据、字幕及素材准备 |
| [people-search](people-search/README.md) | 公开职业资料研究、同名消歧和证据报告 |
| [video-build](video-build/README.md) | 自研 HyperFrames、GSAP、Chromium、FFmpeg 视频工具环境 |
| [hyperframes](hyperframes/README.md) | 上游原生 Skills、HyperFrames CLI 和 HTML 工程 |
| [video-use](video-use/README.md) | 上游原生 Skill、EDL 与剪辑 helpers |
| [openmontage](openmontage/README.md) | 上游指南、pipelines、tools 和 Video App provider 接入 |
| [generic](generic/README.md) | 通用 Codex Docker 模板 |

这些目录准备 Codex 与工具环境，不包含业务应用、数据库或账号。Kun 是独立的内部维护引擎，可诊断失败并生成新的修复配方。工具检查通过不等于真实模型、业务服务或最终制品已验收。

## 初始化与构建中心

初始化第三步可以下载现成镜像、从配方本机构建，或检查本机已有镜像；Docker 是可选步骤。镜像下载需要真实的已发布地址，随包公开目录尚未提供官方镜像。

v0.68 起，六份托管配方会先自动准备或复用公共基础镜像，再构建所选环境，仍使用同一个构建入口。系统层包含 Node 22、Python venv、Git、curl 和 ripgrep；媒体层增加 FFmpeg 和中文字体；浏览器层增加 Chromium。Codex、业务依赖、Skills 与配置留在具体环境中。

| 配方 | 公共基础层 |
| --- | --- |
| people-search | system |
| news2douyin、video-use | system → media |
| video-build、hyperframes、openmontage | system → media → browser |

本机构建时选择需要的配方、精确 Codex 版本和下载源。构建、失败、Kun 修复及检查结果保留在对应准备任务内。关闭页面不会取消已提交的任务，重新打开后可查看对应记录。运行环境的认证、Cookie、代理和业务 MCP 在运行时配置。

构建中心可预览 Dockerfile、导出完整上下文 ZIP，再修改或导入为自定义配方。`RUNDESK_RECIPES_DIR` 可以指定另一份配方根目录；显式目录无效时直接报错。来源分组、字段和边界见[下载源配置](../docs/BUILD-SOURCES.md)。

## 唯一声明与构建快照

`recipe.json` 保存该配方的身份、组件选择、依赖及来源声明，是源码中的唯一可编辑描述。`profile.lock.json` 只在导出或构建上下文中生成；不要在同一来源目录维护两份声明。旧部署中只有 `profile.lock.json` 的外置配方继续可读。

新建构建会冻结当前文件、精确 Codex 版本和下载源。修改源目录不改变历史任务的 ZIP；“按原配置重建”仍使用原冻结输入。默认目录与可选变体共用同一个来源树，构建输出写入指定的新目录，不覆盖配方来源。

news2douyin、people-search 的轻量变体位于各自 `variants/`；video-build 保留 `--with-video-use` 的旧项目兼容变体。默认仍包含本地 Agent-Reach 源码和 wheel 打包修复，不能将其替换成构建时从 GitHub 下载的未审阅依赖。

## 统一 CLI

开发和导出 CLI 使用 Python 3.10+ 标准库。RunDesk 程序本身读取配方不依赖 Python；下面的 `X.Y.Z` 必须替换为实际选择的 Codex 精确版本。

```bash
python3 scripts/recipes.py list
python3 scripts/recipes.py inspect news2douyin
python3 scripts/recipes.py validate news2douyin
python3 scripts/recipes.py prepare news2douyin --codex-version X.Y.Z --output ./build/news-context
python3 scripts/recipes.py pack news2douyin --codex-version X.Y.Z --output ./build/news2douyin.zip
python3 scripts/recipes.py build news2douyin --codex-version X.Y.Z --image rundesk-news2douyin:local
python3 scripts/recipes.py doctor news2douyin --image rundesk-news2douyin:local
python3 scripts/recipes.py pack news2douyin --without-agent-reach --codex-version X.Y.Z --output ./build/news2douyin-lite.zip
python3 scripts/recipes.py prepare video-build --with-video-use --codex-version X.Y.Z --output ./build/video-legacy-context
```

`build` 自动按 `bases/plan.json` 构建所需的公共层，固定每层的实际镜像 ID，再让当前预设继承；后续构建复用相同输入、APT 源、父镜像 ID 与平台的缓存。CLI 将自己的 ID 记录保存在 `${XDG_CACHE_HOME:-~/.cache}/rundesk/build-bases`，可用 `RUNDESK_BUILD_BASE_CACHE` 指定位置。每次 CLI 调用都是新构建；缓存检查确认镜像已被清理时，会从本次固定的父镜像与输入重新准备，随后固定新 ID。Docker 连接错误、身份或标签不匹配、缓存元数据损坏会直接报错。CLI 与服务端各自管理记录，不共享任务历史；服务端历史重建仍要求原来固定的镜像 ID，不能由这条新构建规则替换。

修改 `recipes/common/bases/` 中的 Dockerfile、安装脚本或对应配方的同名覆盖文件可调整公共层；CLI 的 APT 默认源来自该公共层 Dockerfile，构建中心选定的源则由服务端冻结并传入。npm/pip 源与 Codex 版本不进入公共系统层的缓存键。安装脚本被各层共用，修改脚本也会使引用该脚本的层重新构建。

`prepare` 和 `pack` 生成可单独交付的完整上下文，不调用 Docker、模型或业务服务。`build` 和 `doctor` 才需要对应运行条件。`scripts/embed-profiles.py` 保留为非覆盖式检查／导出兼容入口；不再从另一棵源码树反向生成并覆盖 `recipes/`。

共用输入在导出时复制到配方原有的目标位置，`profile.lock.json` 不含 `sharedFiles`；得到的目录或 ZIP 可单独构建。普通 `docker build` 未传入托管基础层参数时，会使用随上下文提供的安装脚本完成相同的系统工具安装；`generic/` 与不含 `bases/plan.json` 的历史配方沿用原构建流程。服务端显式选择 Codex `base` 来源时保留该兼容路径。需要只调整某个配方的工具时，在该配方目录创建同名普通文件即可覆盖共用版本；声明的共用来源仍须存在。具体规则见 [共用输入说明](common/README.md)。

六份托管配方目录用于编辑来源文件；请通过构建中心或上述 CLI 构建，或先用 `prepare`／`pack` 导出完整上下文后运行 Docker。`generic/` 继续保留完整工具，可直接使用其 `build.sh` 或 Docker 命令。

## 构建与运行边界

- npm 工具使用固定版本和锁文件；Agent-Reach 使用已审阅、修复后的本地源码。固定来源、补丁、许可证和摘要随相关配方保留。APT、pip、npm 及构建后端仍可能需要下载，不宣称完全离线或位级可重现。
- Skills 放在 `/etc/codex/skills`；种子配置只初始化缺失文件，不覆盖已有 HOME 配置。Codex MCP 与 mcporter 的配置分别管理。
- Cookie、密钥和业务数据不进入镜像；不挂载 Docker socket，不启动嵌套 Docker。平台账号、模型认证和可选 MCP 服务由部署者配置。
- Windows 可以生成 ZIP；托管 Docker 构建仍要求支持的 Linux 本机 Docker Engine。真实部署还需一次模型、MCP 和代表性业务任务验收。

应用客户端移至 [clients/python](../clients/python/README.md)，systemd 部署文件移至 [deploy/systemd](../deploy/systemd/README.md)。
