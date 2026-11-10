# 构建配方文件与下载源

RunDesk 0.55.0 将构建配方放在磁盘目录中。用户可以修改 Dockerfile、依赖、Skills 和 vendor，再从配方发起新构建；不需要修改 Go 源码、重新编译或重启。构建中心提供 Dockerfile 预览、完整上下文 ZIP 下载，以及构建前的下载源选择。

## 文件与作用范围

| 文件或目录 | 用途 |
| --- | --- |
| `recipes/<profile>/` | 各配方的 Dockerfile、声明与独有资源；同名本地普通文件可覆盖共享目标 |
| `recipes/common/` | 各配方通过 `sharedFiles` 显式引用的公共工具、Skills 与本地依赖 |
| `recipes/common/bases/` | 公共工具层 Dockerfile、安装脚本及目标计划；导出为独立上下文的 `bases/` |
| `recipes/generic/` | 初始化页面提供的通用 Docker 模板，同样从文件读取 |
| `recipes/<profile>/recipe.json` | 唯一配方声明；导出和构建时生成快照内的 `profile.lock.json` |
| `recipes/<profile>/variants/` | 明确选择的轻量或旧项目兼容变体 |
| `config/build-sources.json` | 发行包默认源列表 |
| `<data>/config/build-sources.json` | 首次使用复制的运行列表；页面和文件编辑都作用于这一份 |
| `<data>/image-builds/<id>/context.zip` | 本次构建已冻结的完整输入；历史重建使用它 |
| `<data>/image-builds/<id>/build-bases.lock.json` | 本次公共层的输入与实际镜像身份；历史重建复制原记录 |
| `<data>/image-bases/<key>/image.json` | 本机构建的公共工具层复用记录 |

`RUNDESK_RECIPES_DIR` 指定配方根目录。没有指定时，先查找可执行文件旁的 `recipes/`，再从当前工作目录向上查找。显式目录缺失或无效直接报错，不回退。独立部署时，应同时发布程序、`recipes/` 和 `config/`。

`RUNDESK_BUILD_SOURCES` 指定另一份源列表 JSON。默认列表首次复制到数据目录后，修改发行包里的默认文件不会覆盖用户维护的列表。源配置属于构建节点；已有 Agent 实例使用成品镜像，不会因为换源而自动改变。

维护者和部署者都编辑同一份 `recipes/` 来源。使用 `python3 scripts/recipes.py validate <profile>` 检查，使用 `prepare` 或 `pack` 导出独立上下文。`scripts/embed-profiles.py` 只保留非覆盖式检查／导出兼容入口，不会从另一套模板反向覆盖当前配方。旧外置配方若只有 `profile.lock.json` 仍可读取；同一来源目录同时出现两个声明会被拒绝。

<a id="shared-recipe-files"></a>

## 配方共享输入

配方的 `recipe.json` 可在顶层声明 `sharedFiles` 字典：键是本次构建上下文中的目标相对路径，值是 `recipes/common/` 内的来源相对路径。只有明确列出的内容参与该配方；common 不是自动扫描后全部复制的资源库。

- 来源树保留一份公共工具、Skills 或本地依赖；修改共享来源只影响之后从引用它的配方新建的上下文。
- 配方内同名的本地普通文件覆盖共享目标，便于单独定制；不使用链接覆盖。缺失的共享来源明确失败，不回退到别处。
- `prepare`、`pack` 和构建中心导出会物化所需内容，产物全部是普通文件。导出的 Dockerfile 和本地安装路径继续读取完整上下文，Docker 构建不需要回到源码的 common 目录。
- 冻结快照与 `profile.lock.json` 不保留 `sharedFiles`。历史重建只使用原上下文，即使 common 后来修改也不改变旧输入。
- `generic/` 仍为自含模板。部署运行时保留完整的 `recipes/`；只交付单套配方时，先导出独立上下文。

共享 Agent-Reach 仍是已审阅的完整本地源码，保留去除重复 wheel 包含项的补丁；镜像通过本地路径安装，不重新下载该 GitHub 源码。共享并不意味着 APT、pip/npm 运行依赖也已离线。

<a id="openmontage-bundled-source"></a>

## OpenMontage 随包源码

0.69.1 的 OpenMontage 配方版本为 **1.1.1**，仍固定上游提交 `c8066452325cb57c8fc1ba8c2a71b6e8842e6b65`。`vendor/source.tar.xz` 随配方提供，构建先核对固定 SHA-256，再使用已有 Python 3 标准库解包，不再请求 GitHub 完整仓库归档，也不需要新增 `xz` 命令行工具或对应 APT 下载。来源、原完整归档摘要、压缩方式及省略项保存在 `upstream/source-bundle.json`；`sourceScope` 继续明确为 `runtime-without-demo-media`。

| 审阅对象 | 压缩大小 | 普通文件数 |
| --- | ---: | ---: |
| 原完整归档 | 75,957,235 字节（约 76.0 MB） | 2,171 |
| 0.69.0 随包 gzip 归档 | 17,846,107 字节（约 17.8 MB） | 2,168 |
| 0.69.1 随包 XZ 归档 | 10,296,296 字节（约 10.3 MB） | 2,163 |

本版比 0.69.0 的归档再减少约 **42.3%**，比原完整归档减少约 **86.4%**。这同时来自更明确的预览素材省略范围和 XZ 压缩；32 MiB 字典可利用跨文件重复字节，重复 Skills、音效与模板仍各自保留原路径，未改为硬链接或符号链接。

省略范围精确到以下八个文件：

- `assets/signal-from-tomorrow-demo.mp4`
- `.agents/skills/hyperframes-animation/examples/assets/hyperframes-showcase-hypecard.mp4`
- `.agents/skills/hyperframes-animation/examples/assets/background-tech-data-flow.mp4`
- `docs/images/backlot/board-live.png`
- `docs/images/backlot/library.png`
- `docs/images/backlot/script-gate.png`
- `docs/images/backlot/storyboard.png`
- `.agents/skills/hyperframes-animation/examples/assets/logo-hyperframes.png`

前三项为上版已省略的演示视频，四张 Backlot 图片用于文档展示，最后一项是未被其余源码引用的示例 Logo。其余 **2,163 个普通文件**的内容、路径和可执行权限语义保持一致，包含代码、Skills、指南、pipelines、纹理、音效、字体、图标和测试；许可文件保留。原文档字节不改写，因此其中仍可能引用已省略的预览图；这些预览可从记录的原归档获取，不属于运行依赖。

当前发布包只携带这一份约 10.3 MB 的运行源码归档。首次构建也无需从 GitHub 单独获取它。上述度量是归档的字节比较，不是 Docker 构建耗时、成品镜像大小或业务渲染性能的测试。

解包入口为配方内的 [`upstream/extract-source.py`](../recipes/openmontage/upstream/extract-source.py)。它在解码前校验固定压缩文件摘要和 128 MiB 文件上限，限制 XZ 解码内存为 64 MiB，并核对流结束及校验信息；解压内容上限为 512 MiB、100,000 项。只接受规范的单一源码根目录和普通文件／目录，拒绝链接、特殊文件、重复／大小写别名及路径穿越。解包使用私有暂存目录，完成后才形成新的目标源码目录；已有目标不会被混合覆盖。

维护者可使用[重打包工具](../scripts/package_openmontage_source.py)复现随包归档。该工具不联网，只接受与原固定 SHA-256 相符的完整上游归档，并要求新的输出路径；不会覆盖已有配方：

```sh
python3 scripts/package_openmontage_source.py \
  --source /path/to/original.tar.gz \
  --output /new/vendor/source.tar.xz \
  --manifest /new/upstream/source-bundle.json
```

Dockerfile 先复制、校验并解包源码，再单独复制依赖安装脚本及 npm 锁文件，安装 Python、Remotion 与 HyperFrames 依赖；最后复制并应用 Video App 接入 overlays 和来源记录。修改 overlays、Skills 或来源说明不会仅因此重做业务依赖安装层；修改源码、解包／依赖安装脚本或对应锁文件仍会影响缓存。实际命中取决于 Docker 缓存是否保留，不代表所有依赖都已在离线包中。

导出、`prepare` 和 `pack` 都携带同一归档和解包脚本，普通独立 Docker 构建也可读取它。部署时应一并更新完整 `recipes/`。旧的 gzip 冻结配方仍保持原文件、摘要和指令，不会被改名或静默替换。缺失或摘要不符应恢复正确配方输入，不回退到不同的源码。Python/npm、APT、基础镜像及模型服务仍可能需要网络；源列表继续配置实际使用的下载通道。

## 仍需联网的归档

旧 OpenMontage 冻结配方及其他声明 `build-downloads.json` 的配方继续使用它们原来的 URL、SHA-256 和目标路径。宿主先检查随上下文提供的归档，再检查 `<data>/build-download-cache/sha256/<SHA>.archive`；仅完整校验通过的文件可以进入构建目录。

0.69.0 将可恢复的中间传输保存在同一缓存目录的 `<SHA>.archive.part` 和 `<SHA>.archive.part.json`。记录绑定预期摘要及原 URL 的摘要；同 URL 重试可请求 `Range`，有强 ETag 或有效 Last-Modified 时附带 `If-Range`。服务器正确返回匹配的 `206` 才追加；忽略 Range 返回 `200` 时安全重头下载，不把两个响应拼在一起。换 URL／镜像通道会丢弃不匹配的中间记录，服务器不支持 Range 时也不能保证续传。

下载日志约每十秒显示已收字节、已知总量时的百分比及传输速率。下载不再额外受固定十五分钟总限时截断，但仍受外层构建任务期限、30 秒响应头等待和 120 秒持续无新字节的空闲限时约束。取消或传输中断可以保留受限中间文件供后续恢复；最终完整文件必须重新通过固定 SHA-256 才会原子转成可用缓存。摘要、大小或协议不匹配不会修改配方的预期摘要来迁就错误内容。这些设置只作用于归档传输，不改变 Kun 模型请求超时。

## 构建前操作

1. 在构建中心打开“下载源设置”，维护源列表 JSON。页面保存携带列表 revision；另一处已修改时会拒绝覆盖，应重新读取后合并。
2. 选择配方并点击“构建镜像”。内置配方仍需填写精确 Codex 版本。
3. 选择“国际（美国/官方为主）”或“中国镜像”，查看配方涉及的下载类型；每类仍可从任一分组选择条目或手工填写地址。
4. 核对最终值并开始构建。没有声明参数支持的类型会显示限制，不会通过字符串替换强改 Dockerfile。
5. 在历史详情查看本次源列表摘要与实际参数。修改列表或配方后需要新设置时，新建构建任务；“按原配置重建”继续使用原快照。

分组用于组织条目和快速填表，不保证服务器物理位置、网络可达性或下载速度，也不是自动故障转移规则。列表中的地址经文档核对，不代表已从用户构建节点完成下载验收。

## 类型与参数契约

构建中心根据 Dockerfile 的命令和参数识别类型。0.68.0 同时检查配方声明的公共基础层 Dockerfile，APT 移到公共层后仍可在原构建页面配置。只有声明并实际引用对应标准参数的配方或基础层才支持该项设置；不能仅添加一个列表条目，就使任意脚本自动接受新源。

| 类型 | 字段 | Dockerfile 参数 |
| --- | --- | --- |
| Docker | `baseImage` | `RD_BASE_IMAGE` |
| APT | `mirror`、`securityMirror` | `RD_APT_MIRROR`、`RD_APT_SECURITY_MIRROR` |
| npm | `registry` | `RD_NPM_REGISTRY` |
| pip | `indexUrl` | `RD_PIP_INDEX_URL` |
| conda | `channels`（空格分隔 URL） | `RD_CONDA_CHANNELS` |
| YUM/DNF | `baseUrl` | `RD_YUM_BASEURL` |
| Hugging Face | `endpoint` | `RD_HF_ENDPOINT` |
| GitHub | `archiveBase`、`rawBase`、`releaseBase`、`gitBase`、`apiBase` | 对应 `RD_GITHUB_ARCHIVE_BASE`、`RD_GITHUB_RAW_BASE`、`RD_GITHUB_RELEASE_BASE`、`RD_GITHUB_GIT_BASE`、`RD_GITHUB_API_BASE` |

六套默认配方使用 Node/Debian 基础镜像，提供基础镜像、Debian APT、npm 和 pip 参数。内置 APT 应用脚本针对 Debian；将基础镜像换成其他发行版时，不会盲目写入 Debian 仓库配置。OpenMontage 1.1.1 的运行源码已随配方提供，不再使用 GitHub 归档参数；旧版或自定义配方若仍声明下载，可继续使用它实际支持的参数。conda、YUM/DNF、Hugging Face 与其他 GitHub 参数由使用它们的配方声明并引用。

自定义配方仍是普通 Docker 上下文。Dockerfile 可以直接使用标准 ARG、自己的配置文件或脚本。未声明标准参数的自定义配方保留原始行为；可以下载完整 ZIP，修改 Dockerfile 后重新导入。界面目前提供内容预览和导出，不是完整文件编辑器。

## 扩展源列表

源文件的 `groups` 管理分组和条目，`types` 管理类型名称、字段、字段格式、Dockerfile 识别表达式和对应的 `RD_` 参数。可以给已有类型增加源，也可为 Go Modules、Cargo、Maven/Gradle、APK 等新增类型定义；配方同时声明并实际引用这些参数后才会应用。新增类型定义本身不会安装相应包管理器或修复第三方脚本中的硬编码地址。

字段格式当前为单个 HTTP(S) URL、空格分隔的 URL 列表或完整镜像引用。source ID 在目录中唯一；保存时保留其余分组和类型，并携带最新 revision，避免另一处修改被覆盖。参数设置仍受同一地址/凭据校验与冻结机制约束。

## 不同来源的边界

- **Docker**：`baseImage` 是完整镜像引用，例如企业仓库中的 Node 镜像，不能填写成 HTTPS 包下载 URL。它不配置 Engine/BuildKit 的 registry mirror，不改写 `daemon.json`，也不重启 Docker。中国组不硬编码未经核实的公共加速器；可以保留原镜像或填写自己的企业镜像引用。
- **APT**：主仓与安全更新仓分开配置，保留发行版、组件和签名验证。Debian 中国镜像条目的安全仓默认仍用官方来源，可单独修改。内置脚本处理 Debian 的 `.sources`/`.list` 格式，不把关闭签名验证当作换源方案。列表明确提供 HTTP 初始安装与 HTTPS 两种选项；HTTPS 需要基础镜像已具备 CA 文件，缺失时给出错误，不静默降级。
- **npm**：继续使用锁文件和完整性校验。registry 设置不等于所有 Git、外部 tarball 或安装脚本下载地址都被替换；应核对实际日志和配方脚本。
- **pip**：`indexUrl` 选择主索引。`extra-index-url` 是同时搜索的附加索引，不是主源失败后才启用的备用。Agent-Reach 本地源码仍可能需要 hatchling 等隔离构建依赖，因此源码已打包不等于整个镜像离线可构建。
- **conda**：频道是一组地址；conda-forge 与 Anaconda defaults 分别维护。配方应明确是否保留其他频道，不能把换一个地址理解为所有包均由它提供。
- **YUM/DNF**：实际 base URL 需要与发行版、版本、仓库和架构匹配。Rocky/Alma/CentOS 的目录不能互换；需补填的条目不是已验收可用的完整仓库。
- **Hugging Face**：官方 Hub 与第三方 HF-Mirror 分别标明。endpoint 只影响实际读取它的下载工具，不保证覆盖全部 Hub API、Xet/LFS、受限模型、推理接口或数据集脚本中的其他 URL。
- **GitHub**：源码归档、raw 文件、Release 资源、Git 克隆及 API 分别配置。一个下载代理未必支持其他入口，也不能以首页连通证明 Release 附件下载成功。

源列表只接受公开地址和有限构建参数。URL 中的账号密码、访问令牌、查询参数及控制字符会被拒绝；本版没有构建 secret 配置界面。需要私有仓库凭据时，应另行使用适当的 Docker 登录或构建秘密机制，不把令牌写入 Dockerfile、源列表或可下载 ZIP。

## 快照与复验

提交时保存有效源设置、列表 SHA、条目选择和参数，并将非敏感快照写入 `build-sources.lock.json`。它参与上下文摘要，因此换源不会被误认为相同输入而直接复用 RunDesk 的已完成构建记录。Docker 本身仍按层规则复用缓存。

公共基础层只接收自身声明并使用的源参数。例如 APT 主仓／安全仓变化会改变系统工具层的复用身份；pip/npm 配置变化不会仅因此重建没有使用它们的公共工具层。完整源快照仍随主任务保存，基础镜像 ID 和阶段输入另行固定。中国与国际镜像站的包内容不保证相同，源配置与摘要并不承诺两种网络下得到相同 Image ID。

历史重建读取冻结的 ZIP 和参数，不重新解析今天的源列表。Docker daemon、DNS、远程仓库内容仍是外部条件，快照不承诺位级可重复构建。旧记录没有源设置时继续沿用旧输入，不会补上新默认值。

真实验收需要在目标构建节点检查基础镜像拉取、各包管理器实际下载、构建完成后的离线 smoke，以及应用自己的模型/MCP/业务任务。代码测试、源地址文档核对和一次 HTTP 成功不能替代这些检查。

## 管理 API

下列为 RDCA `/api/v1` 下的管理员路径；旧 `/api` 路径保留对应入口。

| 路径 | 操作 |
| --- | --- |
| `/build-center/sources` | GET 源列表与类型字段；PUT 携带 `schemaVersion`、`groups`、`types`、`revision` 保存 |
| `/build-center/recipes/{rid}/sources` | GET 配方识别结果和参数支持情况 |
| `/build-center/recipes/{rid}/dockerfile` | GET Dockerfile 内容；内置配方可用 `codexVersion` 预览实际版本 |
| `/build-center/recipes/{rid}/context.zip` | GET 完整 ZIP；内置配方需要精确 `codexVersion`，自定义配方导出原冻结输入 |
| `/build-center/recipes/{rid}/build` | POST 构建；可选 `buildSources` 包含 group、catalogRevision、selections、overrides |

字段和错误契约以 [OpenAPI](../internal/app/openapi.json) 为准。构建幂等键、原始输入限制和镜像管理见[构建中心](BUILD-CENTER.md#build-workbench)。

## 配置依据

[Docker 构建参数](https://docs.docker.com/build/building/variables/) · [BuildKit 镜像源](https://docs.docker.com/build/buildkit/configure/) · [APT 源格式](https://manpages.debian.org/bookworm/apt/sources.list.5.en.html) · [npm 锁文件](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) · [pip 索引选择](https://pip.pypa.io/en/stable/cli/pip_install/#finding-packages) · [pip 隔离构建](https://pip.pypa.io/en/stable/reference/build-system/)
