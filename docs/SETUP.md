# 启动与部署

当前组合：RunDesk 0.59.0、独立 Kun 0.17.0、worker 协议 16。两者独立构建、以独立程序运行，也可在可运行包中一起分发；接口和旧版本兼容规则见 [Kun 拆分](KUN-SEPARATION.md)。

## 可运行包

选择与你的系统匹配的 Linux amd64 或 Windows amd64 包，完整解压，保留以下目录布局：

```text
rundesk / rundesk.exe
kun / kun.exe
recipes/
config/
start-rundesk.sh / start-rundesk.cmd
README.txt
```

Linux：

```bash
chmod +x ./start-rundesk.sh
./start-rundesk.sh
```

Windows 双击 `start-rundesk.cmd`。启动脚本以自身目录为基准使用 `data/`，无需从指定当前目录启动；不要让多个服务共用同一数据目录。也可直接执行 `./rundesk --data ./data` 或 `.\rundesk.exe --data .\data`。打开 <http://127.0.0.1:3210> 开始初始化。

可运行包无需 Go、Node.js 或 npm。Kun 与 RunDesk 仍是两个独立程序；仅提供模型服务地址不会自动下载模型权重或启动 vLLM/llama.cpp。拉取、检查、构建和运行 Docker 镜像需另外安装受支持的 Docker Engine；不需要 Docker 时可以跳过。其他系统或架构可从源码分别构建。

## 从源码构建

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

## 外置构建文件

WebUI 仍嵌入程序；Docker 构建配方从磁盘读取。若只在源码根目录启动，上级目录查找可以定位本仓库的 `recipes/` 和默认源配置；发布到其他目录时，把 `recipes/`、`config/` 与 RunDesk 程序一起部署。

```bash
export RUNDESK_RECIPES_DIR=/srv/rundesk/recipes
export RUNDESK_BUILD_SOURCES=/srv/rundesk/build-sources.json
./bin/rundesk --data ./data
```

`RUNDESK_BUILD_SOURCES` 指向要使用的完整源列表文件；可先复制发行包中的 `config/build-sources.json`。不设置该变量时，默认源列表首次复制到 `<data>/config/build-sources.json`。Windows 可用 PowerShell 的 `$env:RUNDESK_RECIPES_DIR`、`$env:RUNDESK_BUILD_SOURCES` 设置绝对路径。

显式配方目录有误会报错，不会悄悄使用另一份默认配方。目录内完整上下文可直接修改，新构建会读取并冻结当前内容；历史重建继续使用原快照。源列表、构建前选择、Dockerfile 参数与镜像拉取边界见[下载源配置](BUILD-SOURCES.md)。宿主运行构建仍不要求 Python/npm；Docker 镜像中安装工具需要相应软件源。

## 首次初始化

新安装首次打开时，先显示全屏初始化页面，再进入工作台。已完成初始化的安装继续进入 Agent 列表；左侧导航栏底部“环境设置”可以重新打开向导。主动选择稍后配置时，本标签页不反复拦截；指向具体会话的链接可直接查看修复轨迹。Demo 不自动启动首次配置，也不安装软件或调用真实模型。

初始化只分三步，每一步是一页，可以返回或直接切换；暂时不用的 Codex、Docker 可以跳过。扫描与配置不要求先安装 Codex。第二步可直接“完成基本设置，稍后准备 Docker”，第三步也可保存并进入工作台。

1. **寻找和配置 LLM**：自动读取运行 RunDesk 的服务器平台、CPU、内存、可获得的 GPU 信息，以及 Kun、Codex、Docker、Node/npm、vLLM、llama-server、Ollama 的路径和版本。在同一页选择发现的模型服务，或手工填写 Chat Completions 基础地址、模型 ID 和服务进程中的密钥环境变量名称。点击“测试并启用 Kun”才会进行小型真实推理与函数调用测试，然后保存专用 `setup-kun` 初始化实例。Kun worker 还须通过协议握手；文件缺失或不兼容会显示具体原因。
2. **配置和安装 Codex**：检查已有可执行文件；未安装时，选择精确版本和下载源启动受管安装。默认版本可修改，不接受 `latest`。本次新安装或对应 Kun 修复成功后，会自动填入已核对的路径；在最后保存配置时选用。历史安装可点击“使用此安装”选择。安装完成不代表模型认证完成。
3. **准备 Docker 环境（可选）**：检查 Docker Engine，默认从公开镜像目录下载已发布的预设，也可填写真实镜像地址。没有发布条目时会明确提示，可切换本机构建或检查已有本地镜像。拉取任务核对平台、预设身份、Codex 版本和离线工具；本机构建继续提供预设多选、国际/中国源组及逐项修改。提交后可查看进度、日志和失败原因；关闭页面不停止后台任务。也可在此页保存并进入工作台。

0.58.0 的本机构建同时选择 Codex 来源：默认优先复用该服务器上精确版本匹配的受管 Linux 原生包，没有匹配包时才通过 npm 安装；也可明确要求原生包、指定已有兼容基础镜像，或选择 npm。Windows 宿主程序不能直接放入 Linux 容器。版本、平台与实际来源检查见[构建运行时复用](BUILD-RUNTIME.md)。网络分组与运行时来源独立，不维护两套国家版 Dockerfile。

硬件扫描中，NVIDIA 使用 `nvidia-smi`；ROCm/MUSA 驱动工具可用时保留有界摘要。没有这些工具不证明机器没有 GPU。专用 Kun 实例具有独立工作区和文件写入能力，不启用宿主 Bash，不更改默认 Codex 实例。若它已就绪，本次进入工作台会选中它，现有实例后端和会话身份不变。仅标记初始化完成不代表未检查的软件、模型或 Docker 已就绪；后续仍可从“环境检查”重开向导。

自动模型发现只探测本机 loopback 常见端口；Linux 另读取监听端口表并有界探测，不扫描局域网。支持 OpenAI 兼容的 `/v1/models` 目录，包括使用该接口部署的 vLLM、llama.cpp 等服务。非默认端口、反向代理路径、需要鉴权或远端服务可以手工添加。扫描不会安装推理框架、下载模型、启动模型服务或执行推理；目录返回模型名称只表示发现。

模型测试分别报告 `inferenceOK`、`roundTripOK` 与 `toolsOK`：先做文本推理，再请求自动函数调用，最后回送固定无副作用的 ping 结果并检查完整往返。只有完整往返通过才标记 toolsOK。只会回答文本、不能自动调用工具或不能接收工具结果的服务不能被判为 Kun 初始化助手已就绪，需按该推理服务的工具调用要求配置。测试证据绑定服务地址、模型、密钥变量及当时凭据，保留 30 分钟；更改这些设置或证据过期后重新测试。测试通过是小型能力检查，业务任务质量仍需实际验收。

## Codex 受管安装与失败修复

新初始化页面默认使用原生安装：从所选 npm registry 读取精确版本的 Codex 平台包，核对 SHA-512 完整性后解包，并执行 `--version` 比较请求版本。**这条路径无需预先安装 Node.js 或 npm**。可选 npm 方式需要它们已在服务账户的 PATH；兼容的旧同步安装接口省略 method 时仍使用 npm。

每次安装使用 `<data>/tools/codex/<版本>-<唯一目录>`，不覆盖系统全局包或当前安装。可使用国际/中国组里的 npm 条目或管理员填写的公开 registry；源列表 revision 和解析后的地址在任务准入时固定。安装最长 5 分钟，有界保存日志。新后台任务立即返回编号，页面刷新后仍可查询；服务中断的未确认记录标记 interrupted，不自动重复安装。结果未知先核对原任务和幂等回执。

本向导新提交的安装和对应修复成功后会自动填入已验证路径；最后完成初始化时保存。历史安装仍由“使用此安装”选择。认证通过 Codex 自己的提供方配置、`codex login` 或服务环境完成。受管原生安装同时保留包内伴随工具；Windows 使用实际 `codex.exe`，不依赖 `.cmd` 启动器。

Kun 就绪后，新提交安装和本机构建默认勾选“失败时交给 Kun 自动诊断并修复”，可以关闭。授权仅绑定这次来源任务及当时 Kun 配置，关闭页面后后台仍可完成交接；同一来源至多创建一个修复任务，不自动扫描历史失败或递归提交 Agent 的重试。可在记录中关闭尚未派发的交接；已创建任务通过普通取消入口管理。自动交接授权最长两小时；长构建超过模型测试证据的 30 分钟期限时，若同一 worker、配置和凭据保持不变，授权内可自动做一次无副作用的完整能力复测，再交接修复。这不会把全局模型测试的有效期从 30 分钟改为两小时。配置/凭据/来源变化、授权到期或复测失败会显示 blocked 和原因，重新核对后再启用。

失败时可选择“交给 Kun 修复”。需要专用 Kun worker、有效模型测试和工具调用已就绪。修复以普通任务执行，能读取原安装及自身重试的日志、查询已有下载源、改用原生/npm方式并重试同一精确版本，最多三次、最长 30 分钟。运行中的安装可通过最长 45 秒的有界等待工具查询，避免长下载密集消耗模型步骤。它执行独立版本核对后保留结果和经验，使用者可在会话轨迹中检查过程。它不能安装显卡驱动、自动提权、任意执行宿主命令或修复范围之外的系统问题；这类阻塞会保留明确原因和日志。

需要手工安装时，也可填写现有可执行文件路径：

```bash
npm install -g @openai/codex@X.Y.Z
```

安装依据：[Codex 官方仓库](https://github.com/openai/codex)。本地受管安装需要选定下载源提供对应平台和版本的包；不把“最新版”视为已测试版本，也不声称所有镜像均已测速或可达。

## 修复经验与持续改进

安装及镜像构建的修复可形成版本化经验：记录失败指纹、适用平台/版本、实际改动、失败与成功任务编号以及验证结果。相同问题的后续证据追加为新版本，原记录保留。Kun 在修复前可检索已经验证、与本次版本和条件匹配的经验；日志和经验文字作为证据，不直接作为执行指令。

已验证状态由服务端记录决定，Agent 不能直接提交分数。Codex 安装需要同平台、同精确版本的成功记录和版本核对，原生安装还须有完整性证据。镜像经验需要可追溯的修复来源链、同平台/同 Codex 版本、保留原 smoke 验收条件，并有精确绑定同一镜像及冻结上下文的成功 offline 和 smoke。证据不足保留候选并说明缺项。已验证代表这些检查通过，不代表所写原因已被形式证明或业务效果可以普遍复用。

这种持续改进积累配方修复案例与证据；不自动改写全局 Skill、默认源或已有配方，不修改模型权重。经验列表中的 `currentApplicable` 只比较宿主系统和架构，使用前仍须核对精确版本、预设和问题条件。

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

首次页面第三步默认使用预构建镜像。公开目录中的条目绑定预设版本、Codex 版本、平台及仓库 Digest；目录初始为空时需填写真实地址，或切换本机构建。目录文件、公开仓库发布和分发流程见[镜像分发](IMAGE-DISTRIBUTION.md)。本机构建模式可同时勾选多套预设，在 Docker 检查通过后分别提交后台构建，保留每套任务和源快照。主机只需要受支持的 Linux 本机 Docker Engine，外置配方不要求宿主 Python/npm 或 Codex。Docker Engine 的首次安装和权限配置仍由部署者完成。

中国网络配置可选择阿里云 Debian、清华 PyPI 等条目；已有数据目录的源列表不会被升级覆盖。管理员可从发行版默认列表补充缺失项，再审阅保存。OpenMontage 可指定完整源码归档 URL，仍核对配方固定 SHA-256，并复用已验证缓存。源选择和缓存不代表已经完成目标服务器的联网验收；详见[下载源配置](BUILD-SOURCES.md)。

失败构建可按原冻结配置重试，也可交给已就绪的 Kun 助手诊断：助手读取明确选中的失败快照，在专用工作区导出、修改和打包新的配方，再构建与检查，不覆盖原配方或已有镜像。回执、日志、补丁说明和 offline/smoke 结果保留在任务与经验记录中。初始化 Kun 的构建任务使用两小时预算，并由服务端限制最多三次镜像构建；长构建使用有界等待工具而非密集轮询，已有其他 Kun 实例保留自己的预算。具体能力见[构建中心](BUILD-CENTER.md)。

拉取失败应先核对地址、登录或网络并选择其他实际来源；不会自动退回长时间本机构建，也不会自动把拉取失败交给 Kun 改写配方。

已有“应用运行检查”继续提供单个镜像准备入口，支持构建、从真实仓库拉取或检查本地镜像。随包公开目录不预填尚未发布的地址；可维护真实发布条目，或手工填写实际存在的地址并核对身份。任务关闭页面后继续，重开可以查看进度和日志。成功只意味着该任务声明的离线工具检查通过，仍需应用级模型认证、MCP 连通性和真实研究验收。

产出镜像是全局可复用资源；初始化不会更改已有应用的镜像绑定。镜像配方与应用运行预设是两种资源：管理员准备镜像、认证、Skills/MCP 和专用实例/工作区后登记运行预设并签发凭据，应用随后选择预设、提交任务、跟踪进度、获取产物。详见[典型配置](../examples/README.md)、[应用连接](APPLICATION-CONNECT.md)、[运行预设](APPLICATION-RUNTIME-PRESETS.md)与[插件支持](NATIVE-PLUGINS.md)。
