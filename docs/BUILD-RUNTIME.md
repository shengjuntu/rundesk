# 构建运行时复用

RunDesk 0.58.0 将下载网络与 Codex 来源分开配置。中国网络和国际网络使用同一套配方、版本约束和 smoke；网络分组只提供可修改的下载地址。已有可用镜像仍可直接下载或检查，无须经过本机构建。

## 公共工具基础镜像

0.68.0 在同一个构建任务内先准备配方需要的公共工具层，再构建该配方。初始化和构建中心沿用原入口，不需要先手动制作基础镜像；准备、构建、复用与失败都写入同一构建日志。

| 层 | 在父层基础上增加 | 使用它的配方 |
| --- | --- | --- |
| `system` | Node 22 镜像上的 ca-certificates、curl、Git、Python 3、python3-venv、ripgrep，以及 `/opt/rundesk-python` 虚拟环境 | people-search；后两层的父层 |
| `media` | FFmpeg、fontconfig、Noto CJK 字体 | news2douyin、video-use；browser 的父层 |
| `browser` | Chromium | video-build、HyperFrames、OpenMontage |

Codex、`gh`、领域 Python/npm 依赖、Skills、MCP seed 和配方锁文件留在各自配方。改变 Codex 版本不会仅因此重建已有的公共工具层，修改一个视频配方的 Python 依赖也不会使 people-search 安装该依赖。基础层构建使用 root 安装系统包，最终配方仍以普通 `node` 用户运行。

各层的 Dockerfile、安装脚本和声明都是外部普通文件，随配方完整导出。直接执行导出的普通 `docker build` 时保留配方自行安装公共工具的路径，不要求手工猜测受管基础镜像标签。没有声明公共层的旧导入配方和冻结历史输入继续原有行为；修改新配方不会重写它们。

来源文件位于 `recipes/common/bases/`；各配方通过 `sharedFiles` 生成独立上下文中的 `bases/plan.json`、相应 Dockerfile、`install.sh` 和 `configure-apt.sh`。计划的 `schemaVersion: 1`、`target`、`stages` 描述目标层和有序父层；每层声明 `id`、`dockerfile`、`parent`、`inputs` 与 `arguments`。只有目标及其父层会构建。维护者新增工具或修改安装步骤时，应同时审阅对应文件和声明，而不是编辑 Go 字符串。

复用以实际父镜像 ID、目标 Linux 架构、当前层文件及该层使用的有效源参数确定。中国／国际分组名称本身不是身份；APT 地址变化会影响相关工具层，pip/npm 地址只影响实际使用它们的配方步骤。基础镜像标签先解析为实际身份，再用于构建，不用标签名称冒充固定输入。

0.75.1 起，交给 `FROM` 的 `RD_BASE_IMAGE` 使用 `rundesk-base/image:sha256-<64-hex>` 本地专用标签。宿主与 Python CLI 从已核验的内容 ID 创建该标签，再检查标签解析结果等于原 ID；根层、层间、缓存复用和最终配方均遵守此规则。`sha256:<64-hex>` 裸内容 ID 继续用于身份、缓存和锁文件，不直接当 Dockerfile 镜像引用。显式 Codex `base` 模式也采用相同转换，其离线能力检查仍直接使用内容 ID。

这些标签是本机 Docker Engine 镜像存储中的派生引用，不需要推送到仓库，会作为本地构建引用保留。标签缺失或被改动时按已冻结的本机 ID 重新建立并核验；固定 ID 本身缺失则明确失败，不向仓库拉取裸 ID，不降级到移动标签。这里沿用本机 Docker Engine 的构建方式，不声明支持任意远程或 `docker-container` builder。此修复不改写用户 Dockerfile；管理员直接填写的自定义镜像引用仍按原配置处理，已有多标签镜像的删除保护也继续保留。

任务的 `ImageBuild.baseImages` 及 `<data>/image-builds/<id>/build-bases.lock.json` 保存声明摘要、平台、根引用、已确定的根镜像 ID 和逐层输入摘要、有效参数、父镜像 ID、镜像 ID。此锁单独保存，不修改已冻结的 `context.zip`。本机公共层缓存位于 `<data>/image-bases/<key>/image.json`，临时构建目录用后清理。

按原配置重建复制原基础层记录，不重新选择当前基础镜像标签；已有冻结镜像 ID 被删除时明确失败，不能悄悄使用另一个镜像。要接受新父镜像或新下载源，从当前配方发起新构建。新任务尚未固定某层镜像、而共享缓存镜像已清理时，可在日志说明后根据该任务固定的父镜像和输入重新构建该层。缓存复用只减少公共层工作，仍须对最终镜像执行原 offline/smoke，不等于整个构建或业务任务已经离线可用。

新任务优先使用本机已有根镜像，仅在明确缺失时拉取；新建任务和“不使用缓存”不会自动刷新远程 Node 标签。需要新上游父镜像时，应先以服务账户拉取或导入所需镜像，或在源配置中指定明确 Digest，再从当前配方新建构建。

Kun 修复提交的新候选与“按原配置重建”不同：新候选按它的新上下文和选定源解析基础层，可命中匹配缓存，但不承诺继承来源构建的实际基础镜像 ID。原失败记录、上下文和已确定的基础层身份继续保留；本版不改变这项修复候选语义。

新建构建选择“不使用缓存”时会重新构建所需公共层；按原配置重建时，此选项保留已冻结的实际基础镜像 ID，只对配方层及尚未完成的基础层禁用缓存，不重新解析后来移动的上游标签。带公共层计划的新任务不直接采用旧的整镜像准备结果，以免遗漏本次基础层的来源记录；普通 Docker 层缓存仍可使用。构建详情可展开公共层的实际身份，初始化准备卡显示已准备层数，仍在原构建入口内查看。

## 构建时选择 Codex 来源

初始化第三步切换到“本机构建”，或在构建中心选择配方后，填写精确 Codex 版本，再选择来源：

| 模式 | 行为 |
| --- | --- |
| `auto` | 优先复用匹配的受管 Linux 原生安装；没有合适安装时使用 npm 下载 |
| `native` | 必须复用匹配的受管原生安装；缺失或校验失败时明确报错 |
| `base` | 使用明确指定的本地基础镜像，并核对实际 Codex 版本和基础工具 |
| `npm` | 保留按精确版本、所选 registry 在线安装的方式 |

原生复用针对 RunDesk 成功记录的受管安装，不扫描任意用户目录，也不会复制 Codex HOME、登录信息或整个宿主安装环境。目标必须是构建所需的 Linux 平台；Windows 可执行文件不能放入 Linux 镜像。复用 Codex 仅减少这部分下载，APT、Python/npm 业务依赖仍可能需要联网。

系统重新核对该安装原始平台包的 SHA-512，再以 SHA-256 保存到 `<data>/build-runtime-cache/`。构建复制发行包中的平台目录、工具及必要随附文件，而不是只复制一个可能缺少资源的可执行文件。每次使用缓存都会再次核对摘要。压缩包上限 256 MiB，解包仍受受管安装的路径与大小限制。

基础镜像须保留配方需要的 Node.js 22+、npm、`node` 用户和目录，以及可执行的 Codex 和 App Server。只提供一个 Codex 二进制的纯 Debian 镜像，不满足当前六套配方的共同约定。显式选定的基础镜像会记录固定身份；同名标签后来移动不会让历史重建自动采用另一个镜像。

`base` 模式只接受本机 Docker 已有镜像，不隐式拉取远程标签；它与下载源中的 `RD_BASE_IMAGE` 冲突时会拒绝提交。系统在实际构建前执行离线基础检查。`auto` 在没有匹配受管安装时选择 npm；已经选定的原生包损坏、或明确要求的 `native`/`base` 无效时，不悄悄改成另一种下载方式。

明确选择 Codex `base` 模式时，保留它原有的冻结父镜像语义，跳过自动公共工具基础层，日志会说明原因；配方在该镜像上执行独立的工具安装路径。这里的 Codex 来源与自动准备的系统／媒体／浏览器层是两种用途。

OpenMontage 1.1.1 的源码解包要求全新 `/opt/upstreams/openmontage` 目录，不合并父镜像中已有源码。高级 `base` 模式应选择干净、兼容的 Codex 父镜像；也可使用默认来源与公共工具层。已有完整 OpenMontage 镜像仍可作为成品使用，不能直接作为新版配方合并安装的父层。旧冻结任务继续使用原 Dockerfile 和解包指令。

已有自定义配方保留导入时的冻结语义。只有声明并使用运行时复用契约的配方才能接收复用输入；普通 Dockerfile 不会被自动改造成另一种安装方式。需要调整时，下载完整目录、修改并导入新版本。

提交后，`ImageBuild.codexRuntime` 和上下文中的 `build-runtime.lock.json` 保留实际来源。原配置重建继续使用这些身份和缓存；更换来源应提交新配置。可用来源通过管理员接口 `GET /api/v1/build-center/runtime-options?codexVersion=X.Y.Z` 查看，候选存在不代替提交与执行时的完整性检查。

## 精确版本与完整配方导出

内置配方的“查看配方”支持填写精确 Codex 版本后下载完整构建目录。系统同时填写 Dockerfile 的 `CODEX_VERSION`、Codex 标签和 `profile.lock.json`，无需分别编辑三处。自定义配方构建继续使用导入时的版本，不接受额外版本参数覆盖。

版本声明不是执行验证。构建时仍须运行 `codex --version` 与 App Server 检查，随后通过配方原有离线与 smoke 检查；不会因采用本地运行时而降低验收条件。

## 手工制作基础镜像时的上下文

如果由管理员自行制作基础镜像，先把已核验的 Linux 运行时及必要随附文件放进专用目录。Dockerfile 的 `COPY` 只能访问构建上下文中的文件，不能读取任意宿主机绝对路径。使用 stdin 提供 Dockerfile 时，应同时传入实际目录：

```bash
# ./runtime-context/runtime/bin/codex 已由管理员准备并核验。
# runtime/ 还应包含该发行包需要的其他文件；不要复制账号配置。
docker build -t rundesk-codex:0.157.1 -f- ./runtime-context <<'DOCKERFILE'
FROM node:22-bookworm-slim
COPY runtime/ /opt/codex/
ENV PATH="/opt/codex/bin:${PATH}"
RUN node --version && npm --version && getent passwd node \
 && test "$(codex --version)" = "codex-cli 0.157.1" \
 && codex app-server --help >/dev/null
DOCKERFILE
```

这是构建上下文的示例；所选二进制、架构、动态库及随附文件仍须匹配基础镜像。RunDesk 的基础镜像检查与后续业务 smoke 会分别核对它们。参见 [Docker build context](https://docs.docker.com/build/concepts/context/)。

## 这次实机修复的来源

2026-10-28 用户提供的《RunDesk 配方修复步骤总结》报告：news2douyin 1.1.2、people-search 1.1.2、video-build 1.4.1、HyperFrames 1.0.1、video-use 1.0.1、OpenMontage 1.0.1，已在其 Linux 服务器以导入自定义配方方式构建成功，smoke 通过且镜像检查为 available。这是用户部署环境的实测记录；不是本版源码的六套 Docker 构建复测，也不代表已验证模型认证、外部 MCP 或完整业务产物。

本版吸收可复用的修复机制：复用精确版本 Codex、保持业务基础工具、可配置下载源、固定归档校验与缓存。Agent-Reach vendored 源码及 wheel 打包修复已在旧版合并，继续保留。不把该服务器的路径、镜像标签或一次代理测速写成所有安装的默认值。

## English summary

The six managed recipes automatically prepare and reuse system, media and browser tool bases in the existing build job. Stage files, effective source arguments, parent image identity and Linux architecture determine reuse. Codex and domain dependencies stay in the recipe; exported contexts retain a standalone installation path. Historical rebuilds keep frozen image identities and fail explicitly if a required image has been removed.

One recipe set serves China and international network configurations. Codex runtime selection is independent: reuse a matching managed Linux installation, select a verified compatible local base, or install the exact release through npm. Native files and base-image identities are frozen for submitted builds; existing custom recipes retain their import-time contract. Node/npm and the recipe user remain part of the base requirements. Reusing Codex does not make every dependency available offline, and passing tool checks does not establish application-level success.

The six successful Docker builds described above were reported by the user on their deployment server. Current release test evidence is listed separately in [validation](VALIDATION.md). Download selection and archive caching are described in [build sources](BUILD-SOURCES.md).
