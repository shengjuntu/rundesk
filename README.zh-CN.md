# RunDesk

**面向日常对话和应用 Agent 的轻量 Codex 工作台。**

[English](README.md) · [简体中文](README.zh-CN.md)

人通过网页与通用助手交互，应用通过 API 提交任务。RunDesk 集中展示对话、工具过程和运行状态，并管理 Skills、MCP 和应用运行环境。

采用 Go + 原生 HTML/JavaScript，运行 RunDesk 本身不需要 Node.js。Codex 的安装方式和第三方工具可能有各自的依赖。

![RunDesk 对话界面](docs/screenshots/0.16.0/chat-zh.png)

## 快速开始

1. 安装 Codex，完成登录或模型服务配置。参考 [Codex 官方文档](https://developers.openai.com/codex/cli/)。
2. 解压发行包。在 Linux 中运行：

   ```bash
   chmod +x bin/rundesk
   ./bin/rundesk --data ./data
   ```

   Windows 运行 `bin\rundesk-windows-amd64.exe --data .\data`。
3. 打开 **http://127.0.0.1:3210**。新安装自动打开初始化页面，确认 Codex 路径、工作项目和默认模型，保存后逐项检查。
4. 开始对话。左下角语言按钮可以切换中文和英文。

服务进程需要能找到 Codex。可以指定 `--codex /绝对路径/codex`，也可以在初始化页面保存路径；已保存的路径优先。默认沿用现有 Codex 的模型提供方与认证配置。通用助手跟随服务进程的 `CODEX_HOME`，未指定时使用 `~/.codex`。

以后可通过左下角的「环境检查」重新进入。没有安装 Codex 也能打开配置页。协议检查成功不等于模型推理成功；Docker、认证和 MCP 分项显示结果，不笼统标为“可运行”。

### 不调用模型，先体验界面

```bash
./bin/rundesk --demo --data ./demo-data
```

演示模式使用协议模拟器和独立数据目录，不验证真实账户、模型服务或容器执行。

## 界面与职责

| 入口 | 用途 |
|---|---|
| 对话 | 本机 Codex 通用助手；独立、可调整宽度的会话列表 |
| 应用 | 应用主动接入、任务记录、管理员配置 |
| 任务 | 后台任务队列与定时执行 |
| 我的文件 | 个人上传和生成的文件 |
| 协作 | 负责人委派与结果回收；直接交互或可选的 Gitea 黑板 |
| 环境检查 | Codex、目录、协议、认证、MCP，以及可选 Docker 检查 |

RunDesk 通过 Codex **app-server** 工作。管理 Docker 环境时，以 `docker exec -i` 启动 app-server，通过 stdio 通信，不暴露额外服务端口。

## 接入应用

在 news2douyin 或 rundesk-video-app 中填写 RunDesk 地址与管理员凭据，由应用调用：

```text
POST /api/v1/applications/{appId}/connect
```

RunDesk 创建或复用绑定，应用自动出现在「应用」页。初始 Skills 和 MCP 只写入一次，重连保留管理员修改。任务和对应会话在 RunDesk 可见。后续业务操作可使用受限的应用凭据；首次注册仍是管理员操作。

参考 [应用接入说明](docs/APPLICATION-CONNECT.md) 和 [API 文档](docs/API-V1.md)。RunDesk 不负责安装或启动业务应用本身。

## 可选 Docker 环境

通用助手使用本机 Codex。应用可以切换到 Linux 主机上的本地 Docker 环境。

基础模板固定放在 **[`examples/docker/`](examples/docker/README.md)**：

```bash
# X.Y.Z 替换成已验证的 Codex 数字版本
bash examples/docker/build.sh X.Y.Z
```

也可进入「应用 → 配置与运行环境 → 镜像与构建 → 构建任务 → 使用基础模板」，填写 Codex 版本，生成待启动的构建记录；检查后开始构建，再选择镜像版本、更新项目环境。

构建成功不会自动替换运行中的应用。当前应用自动注册仍默认使用本机 Codex，本版未实现注册时自动创建 Docker 环境。数据挂载在宿主机。隔离单位是应用与工作项目，不会自动按用户分别创建容器。

参考 [Docker 环境](docs/DOCKER-ENVIRONMENTS.md)、[镜像管理](docs/IMAGES.md) 和 [构建说明](docs/BUILDS.md)。

## 模型、认证和目录

- 默认沿用现有 Codex 配置，可在初始化页设置通用助手的默认模型。
- 高级设置支持兼容 **Responses API** 的自定义模型服务。填写密钥的环境变量名称，密钥通过服务进程环境提供；环境变化后重启服务。保存模型服务会修改通用助手的原生 Codex 配置。
- `--data` 在启动前决定数据库、托管工作区和应用状态的位置；初始化页面仅显示，不迁移运行中的数据目录。
- 可以添加工作目录；这里的路径属于服务器，不是浏览器所在电脑。
- 登录文件、密钥和业务数据不放入镜像或构建 ZIP。容器中的 localhost 指向容器自身，应用 MCP 地址必须能从容器访问。

## 远程访问

默认仅监听本机。远程监听需要至少 24 字符的管理员令牌：

```bash
export RUNDESK_TOKEN='替换为足够长的随机密钥'
./bin/rundesk --listen 0.0.0.0:3210 --data ./data
```

远程部署使用 HTTPS。反向代理部署可配置 `--public-url https://rundesk.example.com`。初始化沿用现有管理员认证，没有匿名安装后门。首次管理令牌从服务进程环境中设置。

## 从源码构建

使用满足 `go.mod` 要求的 Go 工具链：

```bash
go build -o bin/rundesk ./cmd/rundesk
go test ./...
```

前端和 Docker 模板嵌入可执行程序，修改后需重新编译。部署不需要前端打包步骤或 Node.js。浏览器测试使用 Playwright，仅用于开发验证。

## 升级和验证范围

停止旧进程，备份数据目录及 Codex 配置，再替换程序。继续使用原来的 `--data`，保留已有应用和会话。

**0.16.0** 新增导航与会话分栏、中英文界面基础设施与说明、初始化与环境检查、任务优先的应用详情和基础 Docker 模板入口。用户内容、模型回复与技术日志保留原文，部分旧版详细诊断信息仍保留原有措辞。

本版执行 Go 测试和浏览器场景验证。开发环境没有真实 Docker 引擎，未实际构建基础镜像；真实模型服务与容器运行需要在部署环境验证。详见 [版本说明](docs/RELEASE-0.16.0.md)。

[许可协议](LICENSE)
