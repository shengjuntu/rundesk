# Changelog

## 0.30.0

- Administrator review for Kun steering suggestions: resolve the retained proposal event, edit text, preview the current run/revision, and explicitly dispatch the immutable command.
- Reject stale state, changed runs, foreign reviews and unsupported suggestion kinds. Diagnostic models retain their seven-tool catalog and cannot apply suggestions.
- Persist review/command records before dispatch; reuse the worker request ID on retry, reconcile queued/applied/rejected receipts from retained events, and provide evidence navigation after reload or host restart.
- New default-denied administrator API and browser review dialog; text edits invalidate previews. No automatic resume, MCP approval or configuration edits.
- Kun remains 0.7.0 / worker protocol v7. No new third-party runtime dependencies.

## 0.29.0

- Kun 0.7.0 / worker protocol v7: separate diagnostic conversations on a fixed source snapshot, with an exclusive seven-tool trace/suggestion catalog. No inherited file tools, MCP servers, Skills, project notes or task-specific system prompt.
- Shared `trace_propose` validates historical run/event references and returns a typed suggestion with no apply operation. Kun suggestions render as reviewable cards with fixed-cursor evidence links.
- Trace-analysis creation accepts an explicit `through`; follow-up questions retain source scope and separate model usage. Sources remain untouched, including while paused. Diagnostic sessions use text inputs and follow-up questions instead of checkpoint resume.
- Bound snapshot exports before reading payloads; strict per-tool argument checks shared by native trace MCP and Kun diagnostics.
- Local Chromium diagnostic/source-isolation regression added; full Go, focused race, existing UI and Node checks retained. Real providers and Codex services remain unverified.
- Upgrade both binaries together. Old engine checkpoints are readable as history but cannot resume under the new engine version.

## 0.28.0 — 跨后端步骤检查与浏览器修复

- 统一 runs/steps/step/issues/statistics 查询与只读 MCP 工具，复用既有 tracequery；固定 through，支持筛选、分页和来源证据。
- Codex/Kun 生命周期配对显式标识归属、缺失边界、配对歧义、预览截断及两套事件序号；缺失耗时不补零。
- 新增主工作台及轨迹页“调试检查”，按固定历史只读浏览，过期响应不覆盖新筛选；事件支持 Unicode 分块。
- 索引前总量/单事件上限、超时与取消，脱敏先于预览和搜索；旧分析会话增加 trace_find_issues。
- 修复 Kun 调试辅助脚本未嵌入二进制、初次配置的零值默认数值和主会话审批刷新作用域错误。
- 增加真实浏览器本地协议桩回归。真实 Codex/模型/MCP 联调和完整自然语言诊断仍待完成。

## 0.27.0 — Codex / Kun 统一只读调试

- 新增 DebugService、GET debug/capabilities 与 debug/query；区分宿主事件 ID、Kun 序号、支持状态和当前可用性。
- Codex 支持宿主运行概况、保留事件分页及脱敏分块读取；内部单步/断点/完整上下文/快照差异仍不可用。
- Kun 复用已有只读查询、快照、证据与差异；Console 和差异界面使用统一入口，保留旧 Kun 查询返回结构。
- 新增 `rundesk debug-mcp`：固定服务器与会话、环境变量凭据、14 个只读工具；经相同 HTTP 授权路径访问，无控制或数据库接口。
- 新增 MCP 生命周期/边界、跨应用/成员授权、Codex 分页/分块、Kun 新旧入口一致性与离线检查回归。
- Kun 执行引擎与协议未变；浏览器、真实 Codex/模型/MCP 验收及 K3/K4 仍待完成。

## 0.26.0 — Kun 四面板结构化检查

- Sources 展示当前控制状态、审批、断点、控制回执和动作账本；按状态禁用不可用操作，增加提交忙碌保护与状态版本防回退。
- Performance 显示预算卡片、活动/等待时间和三类完成记录；缺失/旧版未知预算不补零，嵌套耗时不求总和。
- Application 展示 MCP 服务、工具规则、Schema 与历史审批；Layers 展示四模块版本、记录事实和事件依据。
- 共享事件跳转保持快照与运行归属；展开状态按内容身份保存，控制编辑器与恢复区可折叠。
- 新增纯视图与实际 DevTools 入口的模拟 DOM 回归，更新 Playwright 场景。
- Kun 执行引擎保持 0.6.0 / 协议 v6；未新增执行动作或改变恢复校验。浏览器和真实服务验收仍未完成。

## 0.25.0 — Kun 调用证据与快照差异

- Kun 0.6 / 协议 v6：精确事件 evidence 查询、双快照 diff 查询，复用 read 权限与会话归属。
- context 对模型开始快照返回实际请求及来源，其余明确标为状态上下文。
- Network 关联同一运行/步骤的模型、工具和 MCP 证据；历史详情不混入未来结果。
- Elements 有序消息、字符计数、技能来源、工具定义和折叠原始证据；快照差异界面只读。
- 差异先按统一字段规则脱敏，精确保留 JSON 大整数；路径/值预览及遍历有界，明确标记截断。
- 增量刷新、事件/快照缓存上限、保留展开/滚动状态；修复 Performance 历史视图包含未来事件。
- 生产服务与浏览器验收仍待完成；未实现分叉、回滚、自然语言诊断或 K4 优化。

## 0.24.0 — Kun 条件断点与 Console

- Kun 0.5 / 协议 v5：模型和工具前后条件断点，最多 16 条类型化规则，一次性命中与版本化更新。
- 可选调试暂停超时；到期停止，审批独立，单步单位保持不变。
- Console 七类只读查询与控制提案，历史快照查询、固定当前 run/revision，复用原有权限和回执。
- 断点规则与命中记录随检查点恢复；查询不改状态、不写事件、不增加模型调用。
- Sources 与配置页规则编辑器；补充安全边界、审批、恢复和 HTTP 授权测试。
- 自然语言诊断、调试 MCP 服务、分叉及生产/浏览器完整验收仍待完成。


## 0.23.0 — Kun 安全检查点恢复

- Kun 0.4 / worker 协议 v4：最近安全检查点与状态、事件事务提交；发出模型/工具请求前使旧检查点失效。
- 显式恢复创建新 run，保留消息、模块状态、已完成工具和剩余预算；检查点消费与新 run 原子提交。
- 恢复校验 worker epoch、revision、配置/技能/项目版本；MCP 重连比对工具目录并清除旧逐次审批。
- HTTP 检查/恢复端点与 Sources 入口，复用应用权限、并发限制、请求幂等及事件审计。
- 增加真实 worker 强制终止与宿主重启测试；未知副作用、旧记录、过期恢复选择和预算耗尽拒绝恢复。
- 仍不支持历史回滚、分叉、Console 或轨迹编译；浏览器及生产模型/MCP 验收未完成。


## 0.22.0 — Kun 0.3 / K1 核心补强

- 固定 Memory/Planning/Action/Capability 接口与可序列化模块状态，tool-loop-v1 和 Provider 接口；没有额外规划模型调用。
- 内置工具/MCP 统一执行入口；参数验证先于审批与派发。支持有界 JSON Schema 2020-12 子集，本地引用，不支持的关键字明确拒绝。
- 独立工具调用数、活动时间、连续工具失败与可选已报告 token 阈值；审批/调试等待单列，超限阻止后续动作。
- Layers 基础模块检查、配置预算表单；固定历史快照在 Elements/Layers/Application 间保留，Sources 控制当前运行。
- worker 协议升级为 3，Kun 0.3.0；状态 schema 仍为 1，旧记录可检查。无新生产依赖。
- 尚无检查点恢复/分叉、摘要压缩、费用预算或生产模型/MCP 验收，不宣称 K1/K2 全量完成。

# 0.21.0 — Kun 0.2 / MCP

- Kun 每轮独立连接 MCP：stdio、Streamable HTTP JSON/SSE、分页发现、工具过滤和稳定别名。
- 复用 RunDesk MCP 配置、导入导出及工具权限页面；Kun 配置按实例独立保存，下一轮生效。
- 默认逐次审批；显式“始终允许”直接执行；无交互模式拒绝需要询问的工具。应用凭据审批需 approvals scope。
- 会话审批卡片、Sources 审批控制、Network 请求/响应、Elements 工具定义、Application MCP 状态。
- 工具派发后发生超时/断连时标记 outcome_unknown 并停止，禁止自动重试；重启清除失效审批。
- 选择性复制 PiG MCP SSE 解码文件并记录原始哈希与许可证。worker 协议升级为 2，两个二进制须配套更新。
- 未实现 OAuth、旧 SSE 传输、MCP tasks、插件、分叉、JEV/JIT；详见 docs/KUN.md。

# 0.20.0 — Kun 0.1

- Kun 独立 worker、共享仓库、JSONL 协议和 SQLite 状态/事件/快照。
- OpenAI 兼容模型循环、工作区文本文件工具、显式 Skills 注入。
- RunDesk 实例引擎配置、会话/队列/停止/补充指令和事件投影接入。
- DevTools 首版 Network、Elements、Sources、Performance；断点、单步和状态版本校验。
- 选择性复制 PiG SSE 源码和回归测试；保留 PiG / Pi MIT 声明并记录固定 commit 和文件哈希。
- 需 Go 1.25.12+。MCP、插件、分叉和轨迹 JIT 尚未实现，详见 docs/KUN.md。

# 0.15.0

- 应用主动注册并复用旧实例；连接不覆盖后台配置。
- Skills / MCP / 模型集中在 RunDesk 管理；应用只提交业务任务。
- 会话查看入口与默认协作配置简化。
- 详见 docs/APPLICATION-CONNECT.md。

# 0.14.0 — 协作预览

- 无独立角色层；Agent 能力登记关联现有配置或远程 A2A。
- 持久负责人委派、P2P补查回传、预算、暂停、取消与恢复。
- Gitea Issue 黑板：结果同步、评论轮询、模糊提交核对。
- A2A 0.3 JSON-RPC 查询式子集，凭据隔离及入站去重。
- 管理员协作界面与详细错误；未开放成员跨应用协作。
- 协议、范围和真实环境验证限制见 docs/COLLABORATION.md。

# 0.13.0 · 2026-10-04

- 记录已确认的简化需求：个人文件库，不增加知识库或共享库配置。
- 上传与会话产物自动保存、按发起人归属、跨会话从「＋」选择附件。
- 文件搜索、预览、下载、删除与历史引用墓碑；删除会话保留库文件。
- 管理员和普通成员共用简单操作方式，服务端校验个人所有权。
- OpenAPI 1.12.0、隔离与产物生命周期测试、桌面和移动浏览器验收。

# 0.12.0 · 2026-10-04

- 独立应用构建页，支持目录 ZIP、Dockerfile 路径、超时与缓存选项。
- 持久化构建队列、串行执行、增量日志、取消、重启状态恢复与记录清理。
- 成功镜像按固定 ID 登记，保留应用目标和已有容器。
- 管理员权限校验、ZIP 安全检查、日志与上下文容量限制。
- OpenAPI 1.11.0、构建测试及浏览器验收；真实 Docker 仍需部署侧验证。

# 0.11.0 · 2026-10-04

- 用户管理、个人访问码、12 小时浏览器登录、账号停用与访问码重置。
- Docker 应用项目授权、只读/可执行角色、服务端默认拒绝越权。
- 独立成员入口：任务对话、执行过程、审批、上传、下载和复制。
- 凭据变更撤销登录及事件流；不同用户的幂等提交分别记录。
- OpenAPI 1.10.0、权限与浏览器验收。完整组织权限、SSO 和内置镜像构建仍待后续版本。

# 0.10.1 · 2026-10-04

- 独立镜像与构建信息页、应用镜像摘要、版本登记及可用性检查。
- 同标签版本历史、固定应用 Image ID、来源/能力标签、环境使用与升级影响。
- 旧环境继续使用原镜像及资源快照，显式更新前检查目标，并校验应用 revision。
- 元数据不启动容器或模型；管理员 API、OpenAPI 1.9.0、测试与使用文档。
- 内置镜像构建队列、完整多用户保持为后续工作。

# 0.8.4 · 2026-10-03

## 0.10.0

- 应用 Docker 运行环境、项目级持久挂载与环境管理页面。
- 镜像固定、资源限额、空闲回收、可取消的 App Server 进程监管。
- Skills/MCP/认证与原生历史按项目环境绑定；只读轨迹快照。
- Docker 管理 API、结构化错误、示例 Dockerfile 与实机验收脚本。
- 普通/Rootless Docker 与旧本机模式兼容；真实 Docker 验收仍需部署侧完成。

- 失败任务核对与继续：两次原生状态读取、来源和 revision 防冲突、已执行步骤及文件预览、显式确认。
- 原会话中创建关联的恢复轮次，保留原任务、附件、技能与失败事件；时间线支持跳转到来源轮次。
- 应用 API 新增 recovery/check 与 recover；恢复提交沿用持久化幂等回执，响应丢失可重试同一请求。
- 展示 Codex willRetry 状态，进展或结束后清除；上一轮迟到事件不结束新恢复轮次。
- 核对结果每会话仅保留最近一次，15 分钟有效；删除会话同时清理核对数据。无自动任务重跑或审批绕过。

# 0.8.3 · 2026-10-03

- 排查显式 500、API panic 兜底、RPC 传输和任务启动路径，详见 `docs/HTTP-FAILURE-AUDIT-0.8.3.md`。
- RPC 写入受调用期限约束；已取消调用不发送；损坏 JSON 立即断开并保留原因；超长 stderr 持续排空且截断展示，避免堵塞子进程。
- 初始化失败与损坏的配置连接移出缓存；下一次显式操作建立新连接，不自动重发失败任务。
- turn/start 缺少有效编号时标明提交不确定；事件记录失效时拒绝新任务，输入落盘失败不会调用 Codex。
- v1 区分 Codex RPC 502、不可用 503、超时 504；请求记录读取失败为 503 并保留原因；不完整回执不会触发 WriteHeader panic 或重复执行。
- 保留真正的内部 500 和技能回滚失败 500；技能回滚保留安装与恢复两次失败原因。

# 0.8.2 · 2026-10-03

- 增加错误记录与诊断详情，覆盖页面 API、技能导入、文本文件预览和已收到的 Codex 运行错误。
- 保留非 JSON、空响应和响应格式错误，区分网络失败与实际 HTTP 状态；支持复制、刷新恢复和重复记录归组。
- API 错误增加可选 details，保留错误链和可用的 RPC data；5xx 日志带请求编号。未提交响应的 API panic 返回可关联的 JSON 错误；已发送的流不会追加错误 JSON。
- 登录、主界面、设置弹窗和轨迹页均可查看诊断。请求正文、认证头和上传内容不进入诊断记录。
- 保持幂等提交、事件流、单轴时间轴和独立分析流程；无新增运行依赖。

# 0.8.1 · 2026-10-03

- 轨迹改为单一纵向 Turn 时间轴，按问题和实际回复摘录浏览整段会话。
- 节点原地展开；结果先显示、步骤按需查看；长回复可展开完整文本。
- 跨轮搜索、仅需关注、下一处需关注；空返回有独立标记。
- 新轮次更新不抢占历史选择；“最新一轮”显式跳转；刷新恢复展开及筛选状态。
- 保留 0.8.0 的独立分析 session 与五个只读 MCP 查询工具；无新增运行依赖。

# 0.8.0

- 轨迹重设计为按问题分轮次的过程页：实际步骤、回复、工具返回链接、输入输出和原始事件；保留实时更新、历史恢复、长过程分页及导出。
- 空结果、访问失败和执行完成分别呈现；公开阶段说明与最终回复区分；技能提交不冒充技能已执行。
- 轨迹提问新建关联的独立分析会话与原生线程；后续问答留在分析会话，原任务不重跑。
- 内置 Go stdio MCP：列出轮次、筛选步骤、读取步骤、读取完整事件、统计。SQLite 以只读方式打开，查询限定来源会话与固定快照，无任意 SQL 或写操作工具。
- 分析工具按会话挂载，不改写长期配置；分析记录保留来源链接与游标，支持双向跳转。
- 完成真实 Codex 0.159.2 MCP 协议验证（不调用模型）、Go race、桌面/手机及 2,001 工具步骤验收。

# 0.7.0

- 首页提供通用助手，专用配置统一收在应用页；普通用户无需选择 instance。
- 应用登记与一对一绑定、历史来源迁移、任务和模型配置；兼容现有应用 API 客户端。
- Skills、MCP 独立管理页，明确助手/应用及项目归属；收起编辑表单，改善桌面和手机布局。
- 支持完整技能文件夹和 ZIP 导入、目录浏览、完整导出；保留脚本和资源；同名替换与移除前备份完整目录。
- 保留单文件技能 API；新增技能目录 API、边界与大小校验、并发操作保护。
- 修复会话加载期间输入竞态，以及已发送消息残留为新对话草稿的问题。
- 通过 Go race、产品与回复操作浏览器测试、0.6.1 实际二进制升级和既有应用客户端协议验证；详见 docs/VALIDATION-0.7.0.md。

# 0.6.0

- 版本化应用 API、OpenAPI、结构化错误及关联编号。
- 创建与提交的持久化幂等 Key、回执查询与不确定状态；停止绑定 runId。
- 应用来源标签及筛选，v1 实例 PATCH 支持字段部分更新。
- 实例配置总览、会话能力面板、模型列表入口与配置作用域说明。
- WebUI 使用 v1；旧 /api 兼容。轨迹和消息操作改造留到后续版本。

# Changelog

## 0.28.0 — 跨后端步骤检查与浏览器修复

- 统一 runs/steps/step/issues/statistics 查询与只读 MCP 工具，复用既有 tracequery；固定 through，支持筛选、分页和来源证据。
- Codex/Kun 生命周期配对显式标识归属、缺失边界、配对歧义、预览截断及两套事件序号；缺失耗时不补零。
- 新增主工作台及轨迹页“调试检查”，按固定历史只读浏览，过期响应不覆盖新筛选；事件支持 Unicode 分块。
- 索引前总量/单事件上限、超时与取消，脱敏先于预览和搜索；旧分析会话增加 trace_find_issues。
- 修复 Kun 调试辅助脚本未嵌入二进制、初次配置的零值默认数值和主会话审批刷新作用域错误。
- 增加真实浏览器本地协议桩回归。真实 Codex/模型/MCP 联调和完整自然语言诊断仍待完成。

## 0.27.0 — Codex / Kun 统一只读调试

- 新增 DebugService、GET debug/capabilities 与 debug/query；区分宿主事件 ID、Kun 序号、支持状态和当前可用性。
- Codex 支持宿主运行概况、保留事件分页及脱敏分块读取；内部单步/断点/完整上下文/快照差异仍不可用。
- Kun 复用已有只读查询、快照、证据与差异；Console 和差异界面使用统一入口，保留旧 Kun 查询返回结构。
- 新增 `rundesk debug-mcp`：固定服务器与会话、环境变量凭据、14 个只读工具；经相同 HTTP 授权路径访问，无控制或数据库接口。
- 新增 MCP 生命周期/边界、跨应用/成员授权、Codex 分页/分块、Kun 新旧入口一致性与离线检查回归。
- Kun 执行引擎与协议未变；浏览器、真实 Codex/模型/MCP 验收及 K3/K4 仍待完成。

## v0.5.4 — 长文本自动附件

- 粘贴达到 8,000 Unicode 字符的文本，自动生成本地待发送的 UTF-8 `.txt` 附件；输入框已有的说明保留。
- 直接输入达到阈值时，在发送阶段转换。发送前可点击附件卡片预览或移除，点击发送才上传。
- 文件保存完整原文、空白和换行；单附件上限沿用 32 MiB，超限明确报错，不截断。
- 上传失败保留文本附件及草稿，已上传成功的附件重试时复用路径。没有正文时使用简短的阅读附件指令。
- 兼容普通发送与运行中补充；对话历史展示附件文件名。修复首次创建会话后请求失败可能丢失待发送附件的问题。
- 新增 `scripts/longtext-smoke.cjs` 浏览器回归。


## v0.5.3 — 运行中补充指令

- 运行中的输入框和发送按钮可继续使用，发送通过 `turn/steer` 追加到当前任务；空闲时仍调用 `turn/start`，停止操作独立。
- 新增 `POST /api/sessions/{sid}/steer`，明确要求目标 turn ID 和 request ID。不会在任务结束后自动改为启动新任务。
- 持久化提交回执用于重复提交去重；结果不明确时不自动重发，输入保留并提示。
- 补充消息显示接收状态，保存到事件历史、轨迹及 Markdown 导出；等待审批时补充消息不代替审批。
- 测试覆盖并发重复请求、过期轮次、已结束任务、未确认回执、刷新回放、发送失败、停止及后续新任务。


## v0.5.2 — 对话阅读与折叠稳定性

- 移除每条助手消息上重复的 `r. CODEX` 标签。
- 正文 16px、工具标题 15px、代码及输出 14px，改善行距和对比度。
- 按会话、轮次和 item 标识复用消息节点；流式更新和完成事件不再重建工具折叠项，保留展开、焦点、嵌套折叠和输出滚动位置。
- 命令卡优先展示命令、目录、输出、退出码；推理卡展示公开摘要，协议原始数据保留在二级折叠中。
- 阅读展开项时暂停自动追随底部；手动关闭后可继续追随。
- 增加对话浏览器回归脚本 `scripts/conversation-smoke.cjs`。


## 0.5.1 — 轨迹定位与时间边界修正

- 新增步骤列表耗时排序和最长步骤定位；仅比较完整且无时间冲突的区间，未知耗时排在后面。时间轴保持发生顺序。
- 修复旧运行缺少结束事件时，观察范围随后续运行延长的问题；中断标记保留，不伪造结束时间。
- 修复异常导航到边界后跳回的行为；到达边界时禁用按钮。
- 完成一个快照的分页读取后再用会话状态校正轨迹，避免把尚未加载结束事件的步骤过早标记为中断。
- 静止轨迹空轮询保留 DOM 与键盘焦点；运行选项无变化时不重建下拉框。
- 范围计算改为迭代并缓存，避免超大索引展开为函数参数造成异常；浏览器整页规模仍以已验证的 2,001 步为准。


## 0.5.0 — 轨迹工作台

- 新增独立轨迹页面：全程总览、六类轨道、滚轮缩放、拖动定位、按步骤模式、同步选择线和详情。
- 按运行、轨道、文本与异常筛选；上一/下一步骤及异常导航；专注模式、移动端详情抽屉、当前筛选 JSON 导出。
- 读取 SQLite 原有生命周期事件并归并 item 与审批；增量分页排除逐字 delta，保留源事件 ID 与完整记录入口。长步骤列表虚拟滚动。
- 区分原生时间和接收时间；缺失端点不伪造结束时间；审批区间去重并取并集，拒绝结果不被后续 resolved 通知覆盖。
- 展示已记录的项目笔记和显式 Skills 输入、上下文压缩与线程累计 Token，不推测完整模型请求或上下文占用。
- 刷新恢复运行选择、选中步骤、筛选与视窗；原有审批、运行权限和 Skills/MCP 管理继续保留。


## 0.4.0 — 审批与运行可靠性

- 按 availableDecisions 提供审批选项；接受服务端原样规则对象，拒绝未提供的决定和扩大的规则；审批决策、范围和时间保留。
- 权限请求支持本轮或本会话；MCP 拒绝不再因无效表单 JSON 受阻；原生异步清理请求后同步等待状态。
- 实例配置沙箱、审批策略、审批处理方和网络覆盖；运行中的任务保留原策略，下一轮重建连接并恢复同一 thread；省略新字段的旧客户端不重置权限。
- 会话运行状态展示实际模型、Provider、工作目录、沙箱及网络权限；连接告警去重，显示原始错误和历史状态；删除会话同时删除其运行摘要。
- 诊断通过本机 sandbox --help 区分平台子命令与直接命令格式，实际执行最小命令并保留退出码和输出；不调用模型，不绕过沙箱，不修改内核配置。
- 更新 Ubuntu bubblewrap/AppArmor 排障、升级说明、API 与原生/浏览器测试。轨迹工作台保持后续独立设计。

## 0.3.0 — 多实例配置

- 新增实例创建、切换、名称/说明/新会话默认模型配置；独立 CODEX_HOME 与 SQLite 实例元数据。
- 旧会话自动绑定默认实例，保留工作区绝对路径、原生 thread ID 与事件。
- 会话创建时固定绑定实例；配置 RPC 连接按实例与工作区区分。
- Skills 区分实例/项目/其他来源，实例 Skill 支持编辑、导入导出和备份移除。MCP 所有操作与诊断按实例路由。
- 账号状态、针对实例的登录命令、空闲连接重新加载；保留活跃运行和待审批任务。
- 演示配置在实例目录持久化，支持跨进程验证配置一致性。
- 增加实例配置隔离、旧数据迁移、环境继承、重载恢复测试，以及原生和浏览器回归；提供 Ubuntu 升级指南。

## 0.2.0 — RunDesk

- 正式更名 RunDesk：命令、界面、module、客户端标识、文档和示例统一；保留旧数据目录与 CODEX_BASE_TOKEN 兼容入口。
- 会话搜索、重命名、置顶、归档/恢复和删除；运行中的会话不能归档/删除；Markdown 消息导出。
- 调试事件按方向、分类、方法、关键词、时间范围查询完整持久日志；分页、暂停显示、复制 JSON；显示当前运行耗时。
- MCP stdio/HTTP 表单与完整 JSON 编辑；启停开关；配置 JSON 导出和经版本校验的合并导入。
- 项目 Skill 的 Markdown 导入/导出、备份后移除；原子保存 SKILL.md，支持 CRLF。
- 连接诊断页检查可执行文件、工作区、配置/Skills/MCP 接口，不调用模型。
- 修正元数据操作占用连接额度的问题；改进结构化敏感字段识别；拆分前端配置逻辑并格式化源码。
- 补充升级与回归测试、原生协议 smoke 测试、浏览器操作测试。

## 0.1.0 — Codex Base（初始名称）

Go + SQLite 运行后台，直连 Codex App Server；流式对话、审批、恢复、基础配置与调试、项目笔记、文件上传与产物预览。
