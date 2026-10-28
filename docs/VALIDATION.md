# 验证范围与复验

本页区分 0.58.0 区域源与复用运行时、0.57.0 公开镜像分发、0.56.1 三步向导、0.56.0 初始化与修复、0.55.0 外置配方与构建源、0.54.1 研究预置构建修复、0.54.0 WebUI、0.53.0 UHP 加固、此前 0.52.0 文档整理与功能验证，以及更早的发布记录。测试替身通过不等于真实模型、Docker 或业务制品已验收。

## 0.58.0 区域下载源与复用运行时

本轮在 Linux amd64 通过 `go test ./...` 全套回归和 `go vet ./...`；单独运行了原生归档冻结、篡改拒绝、构建快照绑定，以及源码归档缓存、错误摘要、取消和大小上限测试。`scripts/test_build_helpers.py` 6 项、配方测试与上游配方测试、向导和构建中心 JS 回归均通过。运行实际编译的 RunDesk 与独立 Kun 0.17.0，在 Chromium 中检查三步初始向导、390px 页面无横向溢出和控制台无异常；新源目录与运行时可用性 API 返回 200。Windows 仅交叉编译，未进行 Windows 本机启动。

本次没有在用户服务器执行六套真实 Docker 构建，也没有对 ghfast.top、中国镜像、正式仓库下载速度作实机承诺。运行时基础镜像的 Docker 离线验证由系统在任务执行时进行；测试环境没有 Docker Engine。用户提供的 6/6 构建与 smoke/available 成绩是其部署环境记录，与本版测试分开。

## 0.57.0 公开镜像分发

本轮[总验证报告](validation/current/public-images-057.json)记录全量 Go 回归 **809 个通过事件（含子测试）、0 跳过、0 失败、17 个包通过**，并分别记录公开目录/发布专项 race、前端行为和实际服务联调。公开目录专项 39 个通过事件、发布专项 18 个通过事件（均含子测试），验证版本和平台匹配、仓库摘要核对、原来源重试、请求去重、测试证据绑定、取消与重启、空认证配置下的匿名 manifest 检查。

[向导行为](validation/current/onboarding-js-057.json) 40 项检查通过；[构建中心行为](validation/current/build-center-js-057.json)覆盖发布条件、丢失响应重试、日志脱敏、取消、匿名检查、目录冲突和导出。[Chromium](validation/current/onboarding-browser-057.json) 25 项检查覆盖默认下载、空目录、手工地址、固定版本与源选择，以及中英文桌面/手机布局；[实际服务联调](validation/current/onboarding-live-http-057.json) 15 项检查使用真实 RunDesk、独立 Kun、脚本模型及实际目录 CAS 保存。

[Linux 可运行包](validation/current/portable-smoke-057.json)解压后通过 10 项检查，包含最小 PATH、旁置 Kun、外置配方与空公开目录、发布路由及重启保留初始化状态。Windows amd64 仅交叉编译。测试中的镜像地址和 Docker 进程均为测试替身；未上传真实公共仓库、下载真实镜像层或构建实际容器。匿名 manifest 可读也不等于所有镜像层或业务任务已经验收。

## 0.56.1 三步启动向导

本轮[验证报告](validation/current/onboarding-0561.json)记录三步界面的实际复验：[前端行为](validation/current/onboarding-js-0561.json) 30 项、[Chromium 界面](validation/current/onboarding-browser-0561.json) 14 项、[编译服务 HTTP 联调](validation/current/onboarding-live-http-0561.json) 13 项通过。覆盖首次启动、同页选择与配置 LLM、Codex 安装入口、Docker 页直接完成、可选跳过、手工重开，以及中英文桌面/手机布局。

Go 首次启动、OpenAPI 与嵌入资源的 4 项针对性检查通过；Linux/Windows amd64 编译成功。独立 Kun 握手真实运行，模型响应使用脚本服务；本轮没有重复下载 Codex，也未执行真实 Docker 构建或 Windows 原生运行。[Linux 可运行包](validation/current/portable-smoke-0561.json)解压启动的 8 项检查通过，包含最小 PATH、旁置 Kun、外置配方、嵌入界面及重启保留状态。0.56.0 的全量测试保留为历史证据，不计为本轮重跑。

## 0.56.0 首次初始化与修复

本轮[总验证报告](validation/current/onboarding-056.json)记录 Go 全套 **764 个通过事件（含子测试）、0 跳过、0 失败**，初始化专项 race 50 个通过事件，`go vet` 通过；轻量 SDK 三个包测试通过。Python 配方 20 项、独立上游 7 项通过，外置配方与生成器、SDK 文件完整性核对通过。这些数字只属于本轮执行，不继承早期报告。

[原生 Codex 实测](validation/current/codex-native-live-056.json)在本轮 Linux amd64 环境中通过官方 npm registry 下载 `0.157.1-linux-x64` 平台包（150,336,017 字节），由产品安装代码核对 SHA-512、解包伴随工具并实际执行 `codex-cli 0.157.1`。一次记录耗时约 10.34 秒，只代表当时环境；未激活该路径、修改全局安装或调用真实模型。它不证明中国镜像下载、其他平台运行或模型认证已验收。

[Kun 修复专项 race](validation/current/setup-agent-race-056.json)通过 11 项测试、无跳过，其中一项使用真实独立 Kun 0.17.0 worker、脚本模型 HTTP 服务、真实任务 MCP 和本地 registry，走通 403 失败→切换受管原生安装→完整性/版本核对→自动保存已验证经验→后续任务读取匹配经验。模型响应是测试脚本，不证明真实 LLM 的诊断质量或公网安装成功率。

[Kun 构建助手专项 race](validation/current/build-assistant-kun-race-056.json)使用真实独立 Kun 与 HTTP MCP，重复三次通过，覆盖读取失败快照、实际文件补丁、打包/导入新配方、构建状态等待、offline/smoke 记录及自动验证经验和差异。模型为 16 轮脚本响应，Docker 使用进程测试替身；这些结果不等于真实模型自主诊断或实际容器构建验收。

[前端行为](validation/current/onboarding-js-056.json)通过 23 组检查，使用产品 JavaScript 与实际 `api()` 请求辅助函数，覆盖首次入口、推理/工具状态、幂等头、丢失响应重试、后台自动交接、历史失败不自动提交、Docker 多选与源编辑保留、跳过和完成。

[真实 Chromium](validation/current/onboarding-browser-056.json)通过 18 项检查：完整产品 HTML/CSS/JS 搭配 HTTP fixture，在 1440px/390px 检查五步页面、390px 英文布局、首次登录与初始化先于工作台加载；没有水平溢出或页面异常。QA 字体不属于产品运行依赖，也不随包交付截图。

[编译程序 HTTP 与浏览器闭环](validation/current/onboarding-live-http-056.json)通过 12 项检查，使用实际 RunDesk 服务、完整未改动 WebUI、独立 Kun worker 和脚本模型 HTTP，覆盖登录、三轮模型探测、worker 握手、跳过 Codex/Docker、完成初始化、刷新保留 Kun 选择与默认 Codex 实例独立。模型目录可达、完整工具往返通过、Agent 任务完成和业务产物质量仍分别判断。

[Linux 可运行包](validation/current/portable-smoke-056.json)实际解压后，在包含空格的目录、不同工作目录和不含 Node/npm/Codex/Docker 的最小 PATH 下启动，通过 8 项检查：旁置 Kun、嵌入界面、六套配方、八类源、最终 OpenAPI、跳过可选组件及服务重启后的状态保留。

本环境没有完整 Docker 构建验收或真实供应商模型调用。Windows 交叉编译也不代表原生运行通过。目标机器的 GPU 驱动、vLLM/llama.cpp 工具调用配置、Docker Engine 权限、下载源连通性和代表性应用任务仍需实际验收。

## 0.55.0 外置配方与构建源

本轮实际检查见 [build-sources-055.json](validation/current/build-sources-055.json)。Go 全套 668 个通过事件（含子测试）、14 项外部 Kun 测试跳过；源专项 race 通过。覆盖外置文件实时读取、通用模板、源目录错误不回退、配方快照、Dockerfile/ZIP 导出、源类型扩展、源列表并发保存、凭据拒绝、参数实际传入 Docker 命令、冻结定义及原配置重建。外部文件和源列表编辑不会重新解释既有构建的身份。

Python 配方测试 20 项、独立上游 7 项通过，六套配方和通用模板目录与生成器一致。Agent-Reach wheel 实际断开包索引构建/本地安装并逐字节核对 7 个指南/Skill 资源；使用 `--no-deps`，不代表运行依赖或整个容器可用。APT fixture 验证 deb822/传统格式、保留 Signed-By、默认 HTTP 和显式 HTTPS 缺 CA 提示。

真实编译后的 Linux 程序以独立目录部署，完成[本地 HTTP 检查](validation/current/build-sources-live-055.json)：从程序旁读取资源、无重启编辑 Dockerfile、源默认组保存/409 冲突、实际 GitHub 参数识别及带修复 vendor 的完整目录导出。该检查没有 Docker 或模型。Windows amd64 仅交叉编译，未运行。

实际 WebUI JavaScript 的 JSDOM 测试通过；[Chromium 布局](validation/current/build-sources-browser-055.json)使用 HTTP fixture，检查 1440px/390px 两个宽度的构建源表单和源列表编辑器，没有水平溢出或页面异常。QA 字体不作为产品依赖，也不随包保存浏览器截图。

没有执行完整 Docker 构建、镜像站连通性/测速、联网模型或业务任务。国际/中国分组及地址核对不能推导为所有下载均已切换；任意脚本需要声明并使用相应参数。旧版本报告继续保留原范围。

## 0.54.1 研究预置构建修复

本轮结果见 [profile-build-0541.json](validation/current/profile-build-0541.json)。实际复现原始固定提交在 hatchling 1.32.4 下的重复 wheel 路径错误，并对修复源码执行禁用包索引的 wheel 构建、pip 本地安装及全部 7 个指南/Skill 资源逐字节比较。此安装使用 `--no-deps`，不代表运行依赖或完整镜像已经验收。

生成器测试 17 项、独立上游配置测试 7 项通过；六套内嵌 ZIP 与生成器一致。Go 全套 656 个通过事件（含子测试）、14 项外部 Kun 测试跳过；最终内嵌上下文另行检查了本地安装路径、复制顺序、Codex 0.157.1 的 Dockerfile/标签/lock 一致性、源码摘要、完整自定义导入和旧镜像拒绝。Linux/Windows amd64 均编译成功，Linux 程序版本检查通过；未运行 Windows 程序。

本环境没有 Docker，因此没有重跑完整镜像构建、容器 smoke、联网采集或真实模型。用户报告的 news2douyin 1.1.2 构建/smoke 成功单独列作用户部署记录；people-search 的 Docker 验收仍待执行。apt、pip/npm 包及构建后端仍需下载，不能由 Agent-Reach 源码本地化推导为完全离线构建。

## 0.54.0 实例中心 WebUI

本轮实际执行、结果与未覆盖范围由 [webui-054.json](validation/current/webui-054.json) 单独记录。检查应区分实例/工作区上下文、配置导航、构建入口、研究入口收敛、历史记录访问、布局以及 Go 构建/回归；没有在本轮报告中记录为已执行的项目，不计为已通过。

研究 API 与数据保留不代表本轮重新验收了所有研究执行链路。离线实验浏览器脚本已改为先验证日常入口撤下，再通过显式研究访问检查保留的查看组件；运行仍需独立 Kun 和浏览器依赖。跨工作区配置同步、统一能力库收集和 Agent 格式转换未实现，不能由 WebUI 检查推导为可用。

下方 0.53.0 UHP 与 0.52.0 报告保持原版本、原测试替身及原执行范围，不复用其数字作为本轮结果。真实 Kun、Docker、供应商模型和业务结果需在目标环境单独验收。

## 0.53.0 UHP 加固（历史）

该阶段结果以 [uhp-hardening.json](validation/current/uhp-hardening.json) 中实际完成的检查为准。验证分别记录公共任务 deadline、UHP 能力披露与严格准入、预算忽略和旧活动记录处理、指令历史语义、进度/审批只读投影、客户端 HTTP fixture，以及官方套件；不能用其中一类结果替代另一类。

该阶段官方套件基线为 `2026.10.19.post3`，固定提交 `038d4d27afed078db339b107f8bf6031ebb37872`，线协议仍为 `2026-10-19`。Extended 的 50 项与新增 Full T-12 的覆盖不同：T-12 依赖 `createHarness`，本服务不提供该写入接口，不能直接用它验收。0.53.0 对硬工具轮次预算的承诺是明确不支持及严格拒绝，专用测试验证这一边界，不宣称通过 T-12 或完整 Full。

该阶段 Go 专项使用原生协议模拟器和已提交事件覆盖运行身份、到期/取消竞争、队列暂停、重启和审批识别；Python 客户端分别通过 HTTP fixture 和已编译 RunDesk 的隔离 demo 服务复验。真实 Kun、Docker 和模型凭据链路当时不可用，仍待目标部署环境完成 APP–Harness 任务、审批、取消与产物闭环验收。具体 pass、skip 和构建结果只以 0.53.0 报告为准，不继承下方更早报告的数字。

回归中发现并修复了原版 HTTP/1 观测代理的提前响应竞争；报告保留普通全量首轮失败、基线复现和修复后分包复验，区分全量与增量结果。

## 0.52.0 文档整理（历史）

2026-10-25 的修改仅涉及文档、历史证据目录、已合入的补丁文件和发行包 `SHA256SUMS`。检查包括中英文使命与能力边界复核、Markdown 本地链接及围栏、原始源码逐文件比对、归档证据完整性，以及重新打包后的文件校验。

该文档整理阶段未重跑 Go 功能回归、浏览器、Docker 或真实模型测试，不把下面的既有结果计作该阶段新测试。机器检查结果见 [documentation-cleanup.json](validation/current/documentation-cleanup.json)，具体清理范围见[整理记录](DOCUMENTATION-CLEANUP.md)。这些记录不回写为 0.53.0 的检查结果。

## 0.52.0 既有功能证据

`validation/current/` 保留构成本源码基础的资源重构、UHP、观测模型和阅读器各阶段报告。它们按实际先后解释，较早报告描述的是当时实现，不能覆盖后续阶段。

| 阶段 | 随包记录及范围 |
| --- | --- |
| 0.52.0 轨迹阅读器 | [报告](validation/current/trace-reader.json)：完整 Go 套件记录 604 项通过、14 项外部 Kun 测试跳过；核心 race 40 项通过，应用定向 race 62 项通过、4 项跳过；vet 与 Linux/Windows 构建通过。JSDOM 和实际 Chromium 配合明确服务/原生协议测试替身检查阅读、固定历史、证据分页与窄屏布局。 |
| 统一观测模型 | [报告](validation/current/unified-observation.json)：记录 594 项通过、14 项跳过；核心 race 73 项通过，应用定向 race 80 项通过、4 项跳过。验证多源身份、迟到事件、完整性、固定分页和权限；当时未做新的浏览器验收。 |
| UHP 服务 | [实现报告](validation/current/uhp-implementation.json)、[套件报告](validation/current/uhp-conformance-demo.json)：当时固定 post2 套件在原生 Codex 协议模拟后端上 50/50 通过；不代表真实后端或全部规范语义已符合，差异见 [UHP](UHP.md)。 |
| 资源与运行模块重构 | [报告](validation/current/resource-refactor.json)：记录宿主/SDK 回归、定向 race、vet、构建和资源边界检查。该阶段早于 UHP 实现，其中“UHP 未实现”是当时状态。 |

这些功能阶段没有真实供应商模型调用，部分 Kun 集成依赖缺失；Windows 为交叉编译。Docker、原生后端实际认证、生产负载和领域结果质量仍需在目标部署环境验收。不能用早期版本的外部 Kun 通过结果抵消后续报告中的跳过项。

## 本地复验

构建工具链与依赖见 [SETUP](SETUP.md)。从源码根目录执行：

```bash
make build
make test
make check
```

`make test` 包含 RunDesk 和轻量 Kun SDK；`make check` 包含 race 和 `go vet`。外部 Kun 集成测试需显式提供独立 Kun 与新编译宿主的绝对路径，例如：

```bash
RUNDESK_TEST_KUN=/absolute/path/to/kun/bin/kun \
RUNDESK_TEST_HOST=/absolute/path/to/rundesk/bin/rundesk \
go test ./... -count=1 -timeout=300s
```

未提供外部依赖时，应保留实际 skip 清单，不能只统计 pass。前端及协议专项脚本的依赖、测试替身和复验范围分别见[轨迹阅读器](TRACE-READER.md#validation)、[统一调试](UNIFIED_DEBUGGING.md)和 [UHP](UHP.md#验证与复现)。

UHP 应用接入和公共任务时限定向复验：

```bash
go test ./internal/app -run 'TestUHP|TestTaskDeadline' -count=1 -timeout=300s
python3 scripts/test_uhp_client.py
python3 scripts/test_uhp_client_integration.py --binary ./bin/rundesk --output-dir ./build/uhp-client-checks
```

这些命令不是完整 Go 回归或真实环境验收。官方套件还需外部固定 checkout 和已启动服务，命令及报告范围见 [UHP 验证](UHP.md#验证与复现)。

## 历史发布证据

旧版本报告、原始日志、首次失败、重试及摘要集中在[验证归档](validation/archive/README.md)，原始 JSON/JSONL/日志保持字节不变。原长篇发布验证说明保存在[历史记录](validation/archive/RELEASES.md)，不再与当前入口混排。
