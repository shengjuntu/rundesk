# 可编辑的运行配方

`recipes/` 是 RunDesk 配方的唯一来源。每个目录保存 Dockerfile、工具、Skills、已审阅的本地依赖和 `recipe.json`；Go 在新建构建时直接读取这些文件。编辑配方不需要重编译或重启 RunDesk。

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

`prepare` 和 `pack` 生成可单独交付的完整上下文，不调用 Docker、模型或业务服务。`build` 和 `doctor` 才需要对应运行条件。`scripts/embed-profiles.py` 保留为非覆盖式检查／导出兼容入口；不再从另一棵源码树反向生成并覆盖 `recipes/`。

## 构建与运行边界

- npm 工具使用固定版本和锁文件；Agent-Reach 使用已审阅、修复后的本地源码。固定来源、补丁、许可证和摘要随相关配方保留。APT、pip、npm 及构建后端仍可能需要下载，不宣称完全离线或位级可重现。
- Skills 放在 `/etc/codex/skills`；种子配置只初始化缺失文件，不覆盖已有 HOME 配置。Codex MCP 与 mcporter 的配置分别管理。
- Cookie、密钥和业务数据不进入镜像；不挂载 Docker socket，不启动嵌套 Docker。平台账号、模型认证和可选 MCP 服务由部署者配置。
- Windows 可以生成 ZIP；托管 Docker 构建仍要求支持的 Linux 本机 Docker Engine。真实部署还需一次模型、MCP 和代表性业务任务验收。

应用客户端移至 [clients/python](../clients/python/README.md)，systemd 部署文件移至 [deploy/systemd](../deploy/systemd/README.md)。
