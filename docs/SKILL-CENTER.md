# 技能中心 / Skill center

RunDesk 0.75.0 的技能中心供管理员搜索公开 Skill、从本地上传或已安装目录获取固定内容、查看静态检查报告，再安装到当前工作区或运行环境。搜索和内置检查由 Go 实现，页面使用随程序嵌入的 HTML、CSS 和 JavaScript，无需前端构建，也不调用 Skills Manager 或 `npx skills add`。

技能中心是安装前的开发与管理入口。获取、预览和内置检查不会执行 Skill 代码；安装后 Agent 使用 Skill 时的行为仍取决于目标运行环境。

## 使用流程

1. 以管理员身份打开导航中的“技能中心”，在“搜索技能”中输入关键词并选择来源，也可直接填写支持的源码链接。
2. 点击获取源码。仓库包含多个 `SKILL.md` 时，先选择一个具体技能目录；来源要求转到 GitHub 时，按提示继续获取已固定版本的源码。
3. 在详情页核对来源、固定版本、内容摘要和完整文件清单，选择文件预览，阅读自动生成的 Go 快速检查报告。
4. 如已部署外部 SkillSpector，可手动启动额外检查。详情页可切换“内置快速检查”和“SkillSpector”各自保存的报告；刷新或重新打开详情会恢复服务端记录的在途任务。检查可以取消，离开页面不会把后台任务误记为成功。
5. 点击“安装到工作区”，核对当前工作区、运行环境、目录名和范围。默认仅当前项目，也可选择当前运行环境；替换同名技能需要另行勾选。
6. 有发现项或检查未完成时，阅读报告，勾选确认并填写复核理由，再决定是否继续安装。服务端要求理由至少 4 个字符、最多 2000 字节。

“已获取技能”保存获取到的内容与报告，不代表已经安装或启用；其过滤器按名称和来源链接匹配，不是全文或语义搜索。

## 从本地或已安装目录获取

“从本地获取”支持 ZIP 与浏览器文件夹选择。两种方式都先保存固定内容并运行内置检查；它们不会自动写入工作区或启用 Skill，也不会执行上传目录中的脚本。获取后进入详情页，继续沿用源码预览、安全复核、效果测试与明确安装的步骤。

- ZIP 使用 `application/zip` 上传，保留完整相对目录；根部可为 `SKILL.md`，也可有一层技能文件夹。压缩包中的符号链接、特殊文件、越界路径、重复或大小写冲突路径都会拒绝。
- 文件夹使用 `multipart/form-data`，每个字段名为浏览器提供的相对路径；不会把 `scripts/`、`references/`、`assets/` 或 `evals/` 的附件扁平化。浏览器提供文件字节和相对路径，不是让服务器读取浏览器电脑上的任意目录。
- 每次只获取一个完整 Skill，上传请求封装最多 34 MiB，最多 1000 个文件、完整内容最多 32 MiB；根部仍需 UTF-8、以 YAML frontmatter 标记开头的 `SKILL.md`。风险项和二进制等未覆盖内容不会阻止暂存，但会保留需要复核或检查不完整的报告，安装时继续执行原有门槛。

可选名称只能使用安全目录名。省略时优先采用 `SKILL.md` 中可识别的安全 `name`；无法采用时使用内容摘要生成名称。来源为 `local-upload://<name>`，不是市场来源；`revision` 是内容摘要，不是虚构的 Git 提交或发布版本。相同来源、版本和内容可复用已有快照。

“从已安装 Skill 创建快照”使用当前工作区、运行环境和范围。选择目标名称后，服务端通过已有完整目录读取机制复制文件与执行位，再生成快照和检查报告；不会修改、移动或重新安装原目录。随后手工编辑已安装内容，不会改变已经取得的快照。列表读取不跟随符号链接，超过可列出数量会提示截断；实际获取仍校验整个目录。

执行位取决于输入方式：ZIP 尽量保留其文件权限标记；浏览器文件夹上传不能携带原始 POSIX 权限，沿用现有 `#!` 首行推断可执行文件的规则。已安装目录快照记录服务端实际读到的执行位。Windows 上的 POSIX 执行位保留与校验仍以该平台及快照清单的能力为限，不能理解为跨系统权限完全相同。


## 搜索来源与失败处理

| 来源 | 当前能力 | 访问配置与限制 |
| --- | --- | --- |
| ClawHub | 公开关键词搜索；获取具备完整清单与哈希的技能版本，或提示继续获取固定的 GitHub 来源 | 搜索无需令牌；获取内容可选 `CLAWHUB_TOKEN` |
| SkillsMP | 关键词搜索；结果提供可信的源码链接时，可继续获取 GitHub 源码 | 匿名访问受来源配额约束，可选 `SKILLSMP_API_KEY`；不直接下载 SkillsMP 任意页面 |
| skills.sh | 官方 CLI 使用的公开搜索接口；从具体技能页面解析到 GitHub 仓库，再选择技能目录 | 可选 `SKILLS_SH_TOKEN`，配置后使用该来源的令牌接口 |
| GitHub | 搜索 README 提及 `SKILL.md` 的候选仓库；获取仓库或 `/tree/版本/目录` 链接 | 可选 `GITHUB_TOKEN`；仓库搜索结果不等于已经验证的 Skill |

默认选中 ClawHub、SkillsMP 和 skills.sh；GitHub 可单独勾选。搜索词为 1–200 字符，skills.sh 至少需要 2 字符。每个来源最多取 12 条，合并结果保留来源与来源提供的指标；下载量、安装量和 GitHub stars 分别展示，不合成统一分数，市场标签也不会变成本地安全结论。

选中的来源并发查询。网络错误、认证问题、配额限制、响应缺项或部分条目无法解析，会显示相应来源状态与不完整提示；其他成功来源的结果仍可查看。完全失败不会显示为“确定没有匹配技能”。只有完整查询结果进入 10 分钟缓存，页面会标明使用缓存。已获取内容的本地列表仍可查看，跨市场接口变化需要维护对应 Go 客户端。

## 固定内容、目录预览与安装

GitHub 获取先把分支或标签解析为提交 SHA，再读取完整目录清单和该版本的文件，校验 Git blob 内容；ClawHub 获取要求固定版本的完整文件清单、大小和 SHA-256。缺失清单、截断目录、符号链接、子模块、特殊文件以及不安全路径会拒绝获取，不能只下载 `SKILL.md` 就当作完整技能。

远程获取当前限制为 200 个文件、单文件 2 MiB、总量 20 MiB。技能中心本地 ZIP／文件夹获取及原有安装导入上限为 1000 个文件、完整内容 32 MiB；根目录需有带 YAML frontmatter 的 `SKILL.md`，也可接受一层外包装目录。超过内置检查单文件分析上限的内容会明确记为未检查。

完整获取内容保存于 RunDesk 数据目录的 `skill-center/packs/<id>`，复用现有对象存储记录元数据。内容摘要使用确定性的 SHA-256，覆盖相对路径、文件字节与执行位。重新检查、预览和安装读取这份内容；文件或执行位发生变化时会拒绝复用旧摘要，提示重新获取。

详情页列出全部保存文件，显示大小与执行位。文本以纯文本预览，单次最多显示 128 KiB，截断时会提示；二进制只显示文件信息。预览截断不会缩小用于摘要的内容，检查是否覆盖完整内容需另看报告的覆盖范围。

安装请求必须提交当前快照的摘要，服务端再次核对内容并重新运行内置检查。安装复用既有完整目录安装器，目标为 RunDesk 管理的项目或运行环境目录；不会写入浏览器所在电脑的任意 Agent 目录，也不承担多 Agent 中央库同步。

同名目录默认拒绝覆盖。显式替换时，原技能整个目录移到目标根目录下的 `.rundesk/skill-backups/<name>-<id>`，新目录由暂存目录重命名就位；失败时尝试恢复原目录。既有删除接口同样先把技能目录移入备份。该备份范围是被替换或删除的技能目录，不是整个工作区、运行环境或会话；它也不保证安装后的人工修改与获取快照持续同步。

Windows 文件系统不保留 POSIX 执行位，固定快照的清单保留来源执行位用于摘要；Unix 还核对磁盘实际权限。本次验证包含 Windows 交叉编译，未包含 Windows 实机运行。

## 检查报告与安装门槛

内置快速检查扫描文本中的 12 类信号：指令覆盖、隐瞒行为、凭据访问、下载后执行、递归破坏、权限弱化、持久化、可能的数据外传、环境变量收集、编码后动态执行、内嵌密钥和父目录引用。它不执行脚本，也不进行完整语义、数据流、图片内容或依赖漏洞分析。普通网络请求不会单独判为数据外传。

报告按引擎持久保存，包含引擎与版本、内容摘要、严重程度、规则编号、文件和行号、脱敏证据、原因与建议，以及已检查和跳过文件的数量及原因。详情页切换引擎时读取对应报告，不会把内置报告当成外部工具结果；页面可按严重程度筛选并下载规范化 JSON。凭据形式的证据会尽量脱敏，原始外部报告不直接对页面开放；源码预览仍是技能本身的完整文本内容。

| 报告状态 | 含义与界面处理 |
| --- | --- |
| `completed` + `no_findings` | 本次检查完成，未命中所覆盖的规则；不等于证明安全 |
| `completed` + `review` | 检查完成但有待人工复核项；显示发现项与原因 |
| `incomplete` | 存在未覆盖文件、范围不明、超时等情况；显示缺口与警告，不当作通过 |
| `unavailable` | 引擎缺失或配置的程序不可用；显示“引擎未配置”及配置说明 |
| `error` | 执行失败或报告格式不可用；保留错误说明，不当作无风险 |

新的外部扫描请求会立即使该引擎的旧结论失效；失败、取消或中断都不会恢复旧的通过结论。检查运行时安装暂不可用。

检查任务自身还有排队、运行、完成、失败、取消、中断等状态。任务“已完成”仅表示已取得结果，需继续核对报告状态与建议；后台重启后，未结束任务标为中断，不自动重跑。

安装时，内置报告必须为 `completed` 且建议为 `no_findings` 才可免风险确认；对同一摘要已经运行的其他引擎报告，只要仍需复核，也会进入确认流程。管理员必须明确勾选确认、填写理由。回执记录安装目标、摘要、来源、版本、安装时间及实际使用的复核理由。原有 ZIP／文件夹 HTTP 导入也经过内置检查与确认门槛；本次没有把外部 SkillSpector 设为必装前提。

## 工具与测试环境诊断

环境诊断与 Skill 内容检查分别记录。打开页面或读取最近诊断结果只读取已保存的记录，不自动运行工具、创建容器、读取账号凭据或调用模型。点击诊断后才对选中的工具执行受控探测；页面不接受任意命令或可执行路径。

SkillSpector 诊断针对 RunDesk 服务主机的可信命令，先确认版本，再对程序生成的最小文本样本做静态 canary 检查，以核对依赖、JSON 报告和覆盖协议。它不扫描当前用户 Skill，固定关闭 LLM；成功只说明该版本的静态调用与报告协议在这次样本上可用，不是用户 Skill 的安全证书。

Skill-Up 诊断使用所选受管 Docker 镜像，在独立容器内核对 CLI 版本和程序自带的可信用例校验。它不在宿主执行用户 YAML，不启动 Agent 测试，也不自动挂入日常账号凭据。诊断沿用效果测试的执行与清理边界；若已有任务或清理未确认，不能额外启动诊断容器。需要重试清理时，通过记录中的关联任务返回既有任务入口处理。

版本核对按当前已接入的兼容窗口进行：SkillSpector `2.13.x`、Skill-Up `0.15.x`。命令存在、Docker 可连接、版本匹配、静态协议通过及模型实际可用是不同证据，不能互相替代。其他版本会明确标为不兼容或尚未验证，不会仅凭退出码为 0 就宣称可用。

| 检查状态 | 含义 |
| --- | --- |
| `ok` | 该项受控检查本次完成，具体范围见项目说明 |
| `missing` | 所需命令、工具或运行依赖未发现 |
| `incompatible` | 版本或已接入协议不在支持范围内 |
| `failed` | 实际探测失败，保留脱敏原因 |
| `unknown` | 本次没有取得该能力的有效证据，例如模型认证或实际网络连接 |
| `skipped` | 该项按本次诊断范围没有执行，例如静态模式下的 LLM 检查 |

SkillSpector 的 `ready` 仅指静态工具协议已验证。Skill-Up 即使版本与 canary 用例校验都成功，仍不自动证明模型认证、网络或 Agent 可执行；未取得这些证据时总体保留为 `incomplete`。诊断不自动安装、升级、登录或执行付费模型请求。结果带有本次检查时间，后续环境变化需要重新诊断。

## 可选 SkillSpector

SkillSpector 是单独部署的外部 CLI，不包含在 Go 内置检查器中。由管理员在运行 RunDesk 的主机上安装并配置，RunDesk 不自动安装该工具、依赖或模型。页面只能选择检查引擎，不能输入任意可执行命令。

| 服务进程环境变量 | 用途 |
| --- | --- |
| `RUNDESK_SKILLSPECTOR_BIN` | 可信 SkillSpector 可执行文件的路径或命令名；留空时查找 `PATH` 中的 `skillspector` |
| `GITHUB_TOKEN` | GitHub 搜索与固定版本文件获取的可选令牌 |
| `CLAWHUB_TOKEN` | ClawHub 内容获取的可选令牌 |
| `SKILLSMP_API_KEY` | SkillsMP 搜索的可选密钥 |
| `SKILLS_SH_TOKEN` | skills.sh 搜索的可选令牌 |

环境变量应配置到 RunDesk 服务进程，修改后重启服务；不是设置到浏览器。发现命令只表示可执行文件存在，实际可用性以扫描结果为准。

调用使用参数数组，默认等价于：

```text
skillspector scan <受限的临时目录> --format json --fail-on-incomplete --no-llm
```

临时目录由固定快照复制生成，不把用户工作区作为扫描器工作目录。取消与超时会清理扫描进程及其普通子进程：Linux/macOS 使用独立进程组，Windows 使用 Job Object；这不是执行不可信程序的操作系统沙箱。外部进程默认最多运行 2 分钟，JSON 输出最多 8 MiB，错误日志最多 64 KiB。当前页面固定关闭 LLM；管理员 API 的 `allowLLM: true` 可省略 `--no-llm`，依赖与模型需自行准备。`--no-llm` 仅关闭该工具的 LLM 层，不保证外部工具完全离线，例如其依赖检查可能访问漏洞数据库。

适配器同时读取工具的 `risk_assessment`、`execution_successful` 与 `analysis_completeness`。退出码为 0 不会自动变成“安全”；`CAUTION` 和 `DO_NOT_INSTALL` 都需要复核，缺少完整度或执行信息会标记不完整。只有工具明确返回完整、成功的 `SAFE`、没有发现项，并且实际文件覆盖核对通过时，才归一化为 `no_findings`。

覆盖核对同时检查文件数量、完整／部分／未检查计数、`ledger_exceptions` 与 `scope_exclusions`，并把工具的组件清单与准备的实际目录逐项比对。零文件、零已检查文件、账本矛盾、遗漏文件、无效或重复路径以及明确的范围排除，都不能算完整通过。上游的 `coverage_percent: 100` 不能覆盖这些缺口；二进制已登记但未做内容分析时，仍保留未检查文件与原因，不会显示成“全部已检查”。

### 在运行 RunDesk 的主机安装

本版固定验证 [NVIDIA SkillSpector 2.13.4](https://github.com/NVIDIA/SkillSpector/releases/tag/v2.13.4)，源码提交为 `54824463814a0be01360792523ec7245a69190fc`。该版要求 Python `>=3.12,<3.15`；使用独立虚拟环境，不向 RunDesk 的 Go 源码、Go 依赖或系统 Python 中复制这些包。以下命令假定已安装 `uv`；本次 Linux 验证实际使用 `uv pip install` 安装了同一固定提交归档。

Linux 示例：

```sh
skillspector_env="$HOME/.local/share/rundesk-tools/skillspector-2.13.4"
uv venv --python 3.12 "$skillspector_env"
uv pip install --python "$skillspector_env/bin/python" \
  "https://github.com/NVIDIA/SkillSpector/archive/54824463814a0be01360792523ec7245a69190fc.tar.gz"
"$skillspector_env/bin/python" -c "from importlib.metadata import version; print(version('skillspector'))"
export RUNDESK_SKILLSPECTOR_BIN="$skillspector_env/bin/skillspector"
```

将变量配置给实际启动 RunDesk 的进程。若使用 systemd，在该服务的环境设置中填写服务账户可访问的绝对路径，例如：

```ini
[Service]
Environment="RUNDESK_SKILLSPECTOR_BIN=/home/rundesk/.local/share/rundesk-tools/skillspector-2.13.4/bin/skillspector"
```

按现有部署方式重载服务配置并重启 RunDesk；在交互终端执行 `export` 不会修改已运行的服务。重新打开技能详情，确认页面发现了命令，再启动一次静态检查以确认依赖和报告协议确实可用。

Windows PowerShell 示例：

```powershell
$SkillSpectorEnv = Join-Path $env:LOCALAPPDATA "RunDesk\tools\skillspector-2.13.4"
uv venv --python 3.12 $SkillSpectorEnv
uv pip install --python (Join-Path $SkillSpectorEnv "Scripts\python.exe") `
  "https://github.com/NVIDIA/SkillSpector/archive/54824463814a0be01360792523ec7245a69190fc.tar.gz"
& (Join-Path $SkillSpectorEnv "Scripts\python.exe") -c "from importlib.metadata import version; print(version('skillspector'))"
$env:RUNDESK_SKILLSPECTOR_BIN = Join-Path $SkillSpectorEnv "Scripts\skillspector.exe"
```

随后从该 PowerShell 启动 RunDesk；已有进程需重启。若 RunDesk 作为 Windows 服务运行，将变量设置到该服务的实际启动环境，并确保服务账户有权访问虚拟环境后再重启。此配置示例未在 Windows 实机验收。浏览器运行在 Windows、RunDesk 服务运行在 Linux 时，应使用 Linux 主机的可执行路径。

### 已有固定版本的真实静态验证

0.73.1 阶段在 Linux 独立虚拟环境中安装并运行了上述真实 SkillSpector，使用默认关闭 LLM 的静态参数检查三个受控样本：

| 样本 | 真实 CLI 报告 | 结论边界 |
| --- | --- | --- |
| 普通文本 Skill | `SAFE`，执行成功且范围完整 | 在本次静态分析覆盖内未发现问题 |
| 含凭据外传命令的文本及脚本 | `CAUTION`，存在高严重程度发现项 | 需要复核；样本脚本没有执行 |
| 含不透明二进制的 Skill | `DO_NOT_INSTALL`，`is_complete: false`、范围为 `partial` | 即使原始覆盖百分比为 100，也不能当成完整检查 |

这些是三个样本的真实 CLI 静态验证，不代表总体恶意召回率或安全保证，也不包含 LLM 语义层、Windows 实机或目标 Agent 执行效果验收。RunDesk 任务、报告和安装流程的实际回归结果另见[本版验证范围](VALIDATION.md)。

已完成同三类样本的 RunDesk 后台任务、持久报告与安装门禁验证：普通样本可安装，外传与不完整样本返回 `skill_review_required`。在源码根目录可显式运行真实工具复验（未设置变量时该测试跳过）：

```sh
RUNDESK_TEST_SKILLSPECTOR_BIN="$RUNDESK_SKILLSPECTOR_BIN" \
  go test ./internal/app -run '^TestSkillCenterLiveSkillSpector$' -v -count=1 -timeout=180s
```

## 管理 API

以下为管理员接口，使用 RunDesk 现有认证；应用凭据和普通成员不能访问技能中心管理接口。路径都以 `/api/skill-center` 开头。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /sources`、`GET /engines` | 来源说明与扫描命令可用性 |
| `GET /diagnostics` | 读取各工具／环境最近保存的诊断结果，不启动探测 |
| `POST /diagnostics` | 以 `target` 和可选 `instanceId` 显式发起可信 canary 诊断 |
| `GET /search?q=…&sources=clawhub,skillsmp,skills.sh,github` | 搜索指定来源；省略 `sources` 使用默认三个市场 |
| `POST /fetch` | 以 `{"url":"https://…"}` 获取一个完整技能并自动快速检查 |
| `POST /import?name=…` | 上传 ZIP 或 multipart 文件夹，先获取与检查，不安装 |
| `GET /installed?workspaceId=…&instanceId=…&scope=project` | 枚举当前受管范围内可选择的已安装技能名称 |
| `POST /from-installed` | 按 `workspaceId / instanceId / scope / name` 取得已安装完整目录的固定快照 |
| `GET /packs?q=…`、`GET /packs/{id}` | 已获取技能列表与详情 |
| `GET /packs/{id}/file?path=…` | 预览保存的文件 |
| `POST /packs/{id}/scan` | 以 `{"engine":"builtin"}` 或 `{"engine":"skillspector","allowLLM":false}` 创建检查任务 |
| `GET /jobs/{id}`、`POST /jobs/{id}/cancel` | 查询或取消任务 |
| `POST /packs/{id}/install` | 提交工作区、运行环境、范围、目录名、摘要与可选复核信息后安装 |
| `GET /installations` | 安装回执列表 |

诊断请求的 `target` 为 `skillspector` 或 `skillup`。SkillSpector 在服务主机执行，不接受非空 `instanceId`；Skill-Up 使用已配置的受管 Docker 环境。响应的 `checks` 分别说明版本、协议与未知能力，`overall` 不是安全等级。结果按工具及环境保存最近一份，带有检查时间。若返回 `cleanupJobId`，它引用内部工具 canary 的清理任务，可使用既有 `POST /check-jobs/{id}/cancel` 入口重试回收；该任务没有用户 `packId/digest/suiteDigest`，不能当作 Skill 效果报告。

安装 JSON 字段为 `workspaceId`、`instanceId`、`scope`（`project` 或 `instance`）、`name`、`digest`、`replace`、`acknowledge` 和 `reason`。`skill_review_required` 表示需要人工确认，`skill_digest_mismatch`／`skill_snapshot_changed` 表示内容身份不符，不能用确认风险跳过内容核对。

## 本次边界

搜索、固定内容获取、目录预览、静态检查与安装继续保留；0.74.0 新增独立的 [Skill-Up 效果测试](SKILL-CHECKS.md)。效果测试需显式发起，结果不等于安全结论。自动修复与市场发布仍未实现。

技能中心不提供运行时流量代理、权限强制或 sidecar 防护。获取快照用于绑定源码、报告与此次安装，也没有把历史会话引用的 Skill 内容冻结成可重放快照；相关运行与存储边界仍见[存储契约](STORAGE.md)和[资源契约](RESOURCE-CONTRACTS.md)。
