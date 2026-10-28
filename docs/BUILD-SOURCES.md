# 构建配方文件与下载源

RunDesk 0.55.0 将构建配方放在磁盘目录中。用户可以修改 Dockerfile、依赖、Skills 和 vendor，再从配方发起新构建；不需要修改 Go 源码、重新编译或重启。构建中心提供 Dockerfile 预览、完整上下文 ZIP 下载，以及构建前的下载源选择。

## 文件与作用范围

| 文件或目录 | 用途 |
| --- | --- |
| `recipes/<profile>/` | 六套默认配方的完整可编辑构建目录 |
| `recipes/generic/` | 初始化页面提供的通用 Docker 模板，同样从文件读取 |
| `examples/<profile>/Dockerfile` 及变体文件 | 维护者使用的实际 Dockerfile；CLI 从这些文件生成上下文 |
| `config/build-sources.json` | 发行包默认源列表 |
| `<data>/config/build-sources.json` | 首次使用复制的运行列表；页面和文件编辑都作用于这一份 |
| `<data>/image-builds/<id>/context.zip` | 本次构建已冻结的完整输入；历史重建使用它 |

`RUNDESK_RECIPES_DIR` 指定配方根目录。没有指定时，先查找可执行文件旁的 `recipes/`，再从当前工作目录向上查找。显式目录缺失或无效直接报错，不回退。独立部署时，应同时发布程序、`recipes/` 和 `config/`。

`RUNDESK_BUILD_SOURCES` 指定另一份源列表 JSON。默认列表首次复制到数据目录后，修改发行包里的默认文件不会覆盖用户维护的列表。源配置属于构建节点；已有 Agent 实例使用成品镜像，不会因为换源而自动改变。

维护者修改 examples 输入后可运行 `python3 scripts/embed-profiles.py` 同步发行目录，再用 `--check` 检查一致性。脚本名是兼容入口，当前行为不再将 ZIP 编译入 Go。

## 构建前操作

1. 在构建中心打开“下载源设置”，维护源列表 JSON。页面保存携带列表 revision；另一处已修改时会拒绝覆盖，应重新读取后合并。
2. 选择配方并点击“构建镜像”。内置配方仍需填写精确 Codex 版本。
3. 选择“国际（美国/官方为主）”或“中国镜像”，查看配方涉及的下载类型；每类仍可从任一分组选择条目或手工填写地址。
4. 核对最终值并开始构建。没有声明参数支持的类型会显示限制，不会通过字符串替换强改 Dockerfile。
5. 在历史详情查看本次源列表摘要与实际参数。修改列表或配方后需要新设置时，新建构建任务；“按原配置重建”继续使用原快照。

分组用于组织条目和快速填表，不保证服务器物理位置、网络可达性或下载速度，也不是自动故障转移规则。列表中的地址经文档核对，不代表已从用户构建节点完成下载验收。

## 类型与参数契约

构建中心根据 Dockerfile 的命令和参数识别类型。只有声明并实际引用对应标准参数的配方才支持该项设置；不能仅添加一个列表条目，就使任意脚本自动接受新源。

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

六套默认配方使用 Node/Debian 基础镜像，提供基础镜像、Debian APT、npm 和 pip 参数。内置 APT 应用脚本针对 Debian；将基础镜像换成其他发行版时，不会盲目写入 Debian 仓库配置。OpenMontage 还实际使用 `RD_GITHUB_ARCHIVE_BASE` 下载其固定源码并校验 SHA。conda、YUM/DNF、Hugging Face 与其他 GitHub 参数由使用它们的配方声明并引用。

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

字段和错误契约以 [OpenAPI](../internal/app/openapi.json) 为准。构建幂等键、原始输入限制和镜像管理见[构建中心](BUILD-CENTER.md)。

## 配置依据

[Docker 构建参数](https://docs.docker.com/build/building/variables/) · [BuildKit 镜像源](https://docs.docker.com/build/buildkit/configure/) · [APT 源格式](https://manpages.debian.org/bookworm/apt/sources.list.5.en.html) · [npm 锁文件](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) · [pip 索引选择](https://pip.pypa.io/en/stable/cli/pip_install/#finding-packages) · [pip 隔离构建](https://pip.pypa.io/en/stable/reference/build-system/)
