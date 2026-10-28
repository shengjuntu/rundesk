# 构建运行时复用

RunDesk 0.58.0 将下载网络与 Codex 来源分开配置。中国网络和国际网络使用同一套配方、版本约束和 smoke；网络分组只提供可修改的下载地址。已有可用镜像仍可直接下载或检查，无须经过本机构建。

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

One recipe set serves China and international network configurations. Codex runtime selection is independent: reuse a matching managed Linux installation, select a verified compatible local base, or install the exact release through npm. Native files and base-image identities are frozen for submitted builds; existing custom recipes retain their import-time contract. Node/npm and the recipe user remain part of the base requirements. Reusing Codex does not make every dependency available offline, and passing tool checks does not establish application-level success.

The six successful Docker builds described above were reported by the user on their deployment server. Current release test evidence is listed separately in [validation](VALIDATION.md). Download selection and archive caching are described in [build sources](BUILD-SOURCES.md).
