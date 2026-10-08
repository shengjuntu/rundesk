# RunDesk 基础 Codex 镜像

固定目录：`examples/docker/`。这是 RunDesk 自有运行模板，不是 OpenAI 发布的服务镜像。
它复用 Node Debian 基础镜像，安装指定版本的官方 `@openai/codex`，并提供 Python 3、venv、Git、curl、ripgrep。
Codex 安装参考：https://developers.openai.com/codex/cli/

## 构建

在 RunDesk 项目根目录执行：

```bash
# 先查看当前已验证的 Codex 版本
codex --version
# 将 X.Y.Z 替换为上面输出的数字版本，不要使用 latest
bash examples/docker/build.sh X.Y.Z
```

结果为 `rundesk-codex:X.Y.Z`。第二个参数可指定自己的镜像标签。
脚本在 Docker 构建成功后，以非 root、只读根目录和禁用网络执行 CLI 启动检查。
Dockerfile 本身也检查 `codex --version` 与 `codex app-server --help`。
这不代表模型认证、app-server 协议握手和业务 MCP 已通过验收。
需要宿主机 Docker CLI/Engine 和可下载 Debian/npm 依赖的网络；默认构建宿主机架构。

也可以直接构建：

```bash
docker build --build-arg CODEX_VERSION=X.Y.Z \
  --build-arg IMAGE_VERSION=X.Y.Z \
  -t rundesk-codex:X.Y.Z examples/docker
```

`BASE_IMAGE` 支持指定兼容 Node Debian 基础镜像及其 digest。例如通过环境变量向 build.sh 传入。
固定 Codex 版本不等于完全可重现构建：默认基础镜像标签及 apt 软件源仍可能变化。

## 在 RunDesk 使用

在应用的 Docker 配置中选择构建后的镜像，再启动对应项目环境。
RunDesk 覆盖镜像入口，以宿主机 UID/GID、挂载的工作目录、HOME 和 CODEX_HOME 启动；
通过 `docker exec -i` 运行 `codex app-server` 并使用 stdio 通信，不需要暴露 app-server 端口。
本次模板更新不会改变应用注册默认使用本机 Codex 的行为，也不会自动构建或切换应用镜像。

不把密钥、登录状态、历史记录和业务数据放进镜像。不声明匿名 VOLUME，也不挂载 Docker socket。
账户认证仍需为实际使用的挂载 CODEX_HOME 配置，模板不会自动继承宿主机登录。

## Skills 和 MCP

- 完整技能目录可放到 `seed/skills/<技能名>/`，包含 SKILL.md、脚本、模板等。
- `seed/config.toml` 只放无密钥的初始配置。RunDesk 首次初始化时复制缺失文件，已有文件不会覆盖；后续调整使用应用配置页。
- 应用提供的 HTTP MCP 在运行配置中注册，不需要把应用服务装进镜像。地址必须能从容器访问；容器中的 localhost 指向容器自身。
- stdio MCP 所需可执行程序应在派生镜像中安装并固定版本。
- `.dockerignore` 只减少 Docker 构建输入，不会清洗上传给 RunDesk 的 ZIP；打包前自行排除凭据。

## 使用网页构建

目前网页构建不传 build args。先将 Dockerfile 中的 `ARG CODEX_VERSION` 设置为已验证版本的默认值，
再把本目录内容打成 ZIP，确保 ZIP 根目录是 Dockerfile 和 seed/。按 `docs/BUILDS.md` 上传构建。

## 本次验证范围

已检查现有 RunDesk 容器挂载、seed 初始化和入口约定，检查构建脚本语法。
制作此模板的环境没有 Docker CLI/Engine，尚未实际拉取、构建或运行镜像。
