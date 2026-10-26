# 启动与部署

当前组合：RunDesk 0.54.0、独立 Kun 0.17.0、worker 协议 16。两者分别构建与发布，接口和旧版本兼容规则见 [Kun 拆分](KUN-SEPARATION.md)。

## 构建

安装 Go 1.25.12 或兼容版本，在 RunDesk 目录执行 `make build`，只生成 `bin/rundesk`。在独立 Kun 目录执行 `make build`，或 `go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun`。使用 Kun 不需要 Node.js、npm 或 Codex；前端已嵌入 RunDesk。宿主只通过附带的轻量 SDK 编译，不需要相邻 Kun 源码目录或公开模块仓库。

Windows PowerShell：

```powershell
$env:CGO_ENABLED = "0"
go build -buildvcs=false -trimpath -o bin/rundesk.exe ./cmd/rundesk
# 切换到独立 Kun 项目目录后构建
go build -buildvcs=false -trimpath -o bin/kun.exe ./cmd/kun
# 回到 RunDesk 项目目录
.\bin\rundesk.exe --data .\data --kun C:\absolute\path\to\kun\bin\kun.exe
```

Linux/macOS：

```bash
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

默认 http://127.0.0.1:3210。`--kun /absolute/path/to/kun` 指定 worker；否则在 RunDesk 可执行文件旁查找。路径属于服务器。缺少或协议不兼容时明确报错，不改用 Codex。`--data` 在启动时确定，向导不迁移数据。已有 Kun 执行目录不需要搬迁；替换二进制前先停止相关执行，保持原数据目录。

## 环境向导

主工作台每次打开会检查默认实例的本机依赖；首次使用或必需项缺失时自动显示向导。当前浏览器标签页主动关闭后不反复弹窗，可以从环境设置重新打开。检测结果区分必需/可选、可用/缺失/未配置/未检查。检查工作目录会创建并删除一个临时探针文件，不执行模型推理。

1. 选择默认实例使用 Kun 或 Codex、工作目录和默认模型。两个后端可在不同实例同时使用。
2. Kun：填写 Chat Completions 基础地址（例如 `http://127.0.0.1:8000/v1`）、模型名，以及服务进程中的密钥环境变量名称；不需要认证时可留空。保存草稿允许依赖尚未就绪；完成设置要求 Kun 可执行文件和已配置的密钥变量存在。
3. Codex：沿用当前可执行文件与原生配置，或展开可选安装。模型提供方、认证、Skills 和 MCP 继续在实例设置维护。安装/保存不替代 `codex login` 或提供方认证。
4. 保存与实例配置在同一事务内完成。向导同时核对 setup 和 instance 的 revision，拒绝过期表单；修改运行后端前需先结束当前实例的任务。旧会话不迁移后端，新会话使用新配置。

环境列表不连接模型；可执行文件存在不证明版本、认证、Docker Engine 或模型可用。版本、模型协议、账户、MCP 的专项检查按需执行，最终用一条允许执行的实际任务验收。应用运行检查使用应用自己的实例。

## 可选 Codex 安装

服务账户先安装 Node.js 与 npm。在向导填写精确版本 `X.Y.Z`，勾选下载安装后点击按钮。服务调用 npm 的 JavaScript 入口安装 `@openai/codex@X.Y.Z`；不接受 `latest`、任意包名或安装脚本参数。

安装位置为 `<data>/tools/codex/<版本>-<唯一目录>`，不覆盖系统全局包或当前 Codex。使用 npm 官方 registry，禁用包安装脚本；完成后查找当前平台 native binary 并核对 `--version`。安装最长 5 分钟，结束后显示最多 64 KiB 日志；确认路径后再保存配置才生效。不自动登录、不自动提升权限。失败目录保留供检查，管理员可在确认无使用者后自行删除。

安装请求受管理员认证、明确 confirm 和持久化 Idempotency-Key 保护。超时或断网先核对原回执，不能换 Key 重发；服务中断不会自动重试。代理、离线环境或不受支持的平台可手工安装，然后填写 executable 路径：

```bash
npm install -g @openai/codex@X.Y.Z
```

Windows 手工 npm 安装若只提供 `.cmd` 启动器，应填写包内原生 `codex.exe` 路径；向导安装会寻找原生可执行文件。原生 Windows 运行和实际 npm 下载仍需部署环境验收。

安装依据：[Codex 官方仓库](https://github.com/openai/codex)。固定版本由管理员选择；本项目不把“最新版”视为已测试版本。

## 权限与执行环境

Kun 内置写入和 Bash 分别开启。Bash 必须先明确选择完整宿主访问权限，默认逐次询问；不会随安装或向导保存自动开启。配置方法和边界见 [核心工具](KUN-PIG-CORE.md)。

Docker 是可选执行环境，当前仅 Codex 托管路径可用；详见 [Docker 环境](DOCKER-ENVIRONMENTS.md)。向导不会把容器缺失当成 Kun 本机执行的阻塞条件。

## 远程部署与运维

远程监听需至少 24 字符的随机管理员令牌，通过服务环境提供：

```bash
export RUNDESK_TOKEN='替换为足够长的随机密钥'
./bin/rundesk --listen 0.0.0.0:3210 --data ./data --public-url https://rundesk.example.com
```

使用 HTTPS 反向代理。应用后端使用独立应用凭据；不要将管理员 Token 或模型密钥放在前端。服务账户的环境才是运行时环境，修改环境变量后重启服务。使用本地可靠文件系统；同一数据目录只运行一个服务。

[systemd 示例](../examples/systemd/README.md) · [升级](UPGRADING.md) · [当前验证范围](VALIDATION.md)

## 初始化应用 Docker 镜像

运行后端可选“仅使用 Docker 应用”，完成设置不再要求宿主机安装 Codex；通用助手的后端设置保留，后续若要使用本机助手再配置。

展开“应用 Docker 环境”，选择 news2douyin、people-search、video-build、hyperframes、video-use 或 openmontage，输入 Codex 精确版本和镜像地址。支持后台构建、从真实仓库拉取、检查本地镜像。主机只需要 Linux 本机 Docker Engine，内置配方不依赖主机 Python/npm。首次安装 Docker 仍由部署者完成。

任务提交后立即返回，关闭向导不停止；再次打开可以继续查看进度和日志。相同在途配置不会重复入队，冲突的同名镜像准备会拒绝。构建按依赖层缓存；相同配方和相同已验收 image ID 会跳过构建，重新做离线工具检查。成功只意味着工具环境检查通过，仍需应用级模型认证、MCP 连通性和真实研究验收。

此入口可在应用注册之前使用，产出镜像是全局可复用资源，不会更改通用助手或任何已有应用的镜像绑定。日常配方、构建、镜像和检查也可在常驻 [构建中心](BUILD-CENTER.md) 管理。

镜像配方与应用运行预设是两种资源。管理员先完成镜像、模型认证和 Skills/MCP 配置，再登记专用实例/工作区及应用预设，签发对应凭据；应用随后选择预设、提交任务、跟踪进度并获取产物。登记预设本身不构建镜像，原生插件检查也不等于安装。详见 [典型配置](../examples/README.md)、[应用连接](APPLICATION-CONNECT.md)、[运行预设](APPLICATION-RUNTIME-PRESETS.md) 和 [插件支持](NATIVE-PLUGINS.md)。
