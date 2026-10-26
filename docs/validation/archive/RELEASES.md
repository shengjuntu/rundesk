# 历史发布验证说明

以下为原包保留的历史发布验证记录，仅代表各自测试时的源码和环境；本次整理没有重新执行这些检查。归档文件及原始证据的路径说明见 [README](README.md)。

## 0.52.0 验证记录

本轮实现公共构建中心、动态冻结配方、镜像操作、构建助手和 Codex 原生插件只读识别。完整 Go 回归 **516 项含子测试 / 361 项顶层通过，0 失败、0 测试跳过**，逐包终止结果完整，使用独立 Kun 0.17.0 与新编译 RunDesk host。新增能力定向 race 58 项通过；镜像来源归档补丁及旧上传、执行、日志、取消和重启另有 10 项 race 回归。首轮 511 项通过后完成收尾，随后对最终冻结源码重新执行完整回归，原日志独立保留。Go vet、轻量 SDK 测试、Linux/Windows amd64 构建及文档/OpenAPI 检查通过，Windows 仅交叉编译。命令、统计和证据见 [validation-0.52.0.json](validation-0.52.0.json)。

已发布 JavaScript 的 JSDOM 测试覆盖构建中心与原有运行预设，包括上传/响应丢失重试、启动部分失败保留身份、镜像删除与历史区分、检查失败状态、助手任务链接、插件来源及只读包检查；47 个脚本语法通过。没有运行真实浏览器布局验收。

真实 Codex **0.163.0** 在临时 HOME、合成令牌和本地 MCP fixture 上验证了线程配置注入、initialize、tools/list、connected 状态与纯 POST 传输，**0 模型请求、0 plugin RPC**。该结果验证原生连接格式，不代表实际 RunDesk capability→Docker→模型完整任务通过。原生插件包检查与独立 Skill bootstrap 均不等于安装；稳定生命周期接口保持明确拒绝。

自研 video-build 21 个文件和三套旧内置 ZIP 共 24 项逐字节保持。Docker 构建/删除/启动、真实容器回调、模型认证、GPU 与新闻成片质量仍待部署机验收。历史结果保留原版本范围。

---

## 0.51.0 验证记录

新增独立 HyperFrames、video-use、OpenMontage 环境，共享生成 MCP 和配套 App 0.26.0 独立文本 TTS。自研 video-build 目录保持原样。完整 Go 回归 **458 项含子测试 / 319 项顶层通过，0 失败、0 测试跳过**；逐包终止结果完整，使用独立 Kun 0.17.0 与新编译 RunDesk host。Go vet、Linux/Windows amd64 构建和生成模板一致性检查通过，Windows 仅交叉编译。配套 App 完整 race 444 项通过，真实 App HTTP/MCP 生成联测 6 项通过。具体命令、计数和原始日志见 [validation-0.51.0.json](validation-0.51.0.json)。

验证分别记录源码/依赖/构建上下文、原生工具、受限生成桥、实际 App HTTP/MCP、宿主媒体处理与完整 Go 回归。推理使用本地测试服务；没有运行 Docker Engine、真实 Codex 认证或 GPU 模型，不能将目录/镜像配方准备成功表述为三个上游完整制作流程实机验收。HyperFrames 本轮无新的 Chromium 渲染；video-use 有独立标注的实际宿主剪辑小样，OpenMontage 接入以原生 registry/selector 和可写工作区等接口回归为准。

以下保留历史验证。

---

## 0.50.0 验证记录

本轮验证 RunDesk **0.50.0**，使用独立 Kun **0.17.0**、worker 协议 **16** 和 Video App **0.25.0**。新增应用命名运行预设、精确绑定授权、成员授权、只读技能查询与多环境管理界面。详细命令和原始证据见 [validation-0.50.0.json](validation-0.50.0.json)。

- 全量 Go 回归 **454 项含子测试 / 318 项顶层通过，0 失败、0 测试跳过**，使用单独编译的 RunDesk host 和外部 Kun；逐包核对终止结果。预设、成员及文件访问的定向 race 回归通过，完整宿主 race 未运行。
- 两端实际 HTTP 服务的 10 项集成检查通过，覆盖两个命名预设、不同实例/工作区/镜像、保留旧默认、限定预设凭据与只读技能访问。Docker CLI 和 Codex 工具发现使用协议测试替身，不代表实际 Docker 或模型验收。
- RunDesk 已发布 JavaScript 的 JSDOM 行为检查通过，包括配置重试、预设停用历史、精确 key/member grants。Video App 的简洁工作台 24 项 HTTP/DOM 检查另见 App 仓库。
- Go vet、OpenAPI/文档一致性、修改的 JavaScript 语法及 Linux/Windows amd64 构建通过。Windows 仅交叉编译，未运行。

首轮完整回归发现旧 RPC 测试夹具 `TestOwnedGroupCleanupAndFinalFrames/invalid` 的 `unrelated process affected` 失败。检查发现该夹具在固定等待期间读取非原子心跳，可能把空文件或调度延迟误判为进程终止；原用例独立重跑三次未复现。仅测试夹具改为原子写入，并从清理后的基线等待有界进展；生产 RPC 未修改。修前窄用例三次通过，修后 close/exit/invalid 整组连续三次通过，再重跑完整回归。首轮失败日志完整保留，不计为成功回归。

没有运行生产 Docker Engine、真实 Codex 认证、GPU 模型或新的媒体成片验收；未构建 OpenMontage/video-use 上游镜像，也未自动转换其工程。已有 0.49.x 媒体及协议证据保留原版本标识。

以下保留历史验证记录。

---

## 0.49.1 验证记录

本轮验证日期：2026-10-17。RunDesk **0.49.1** / Kun **0.16.0** / worker 协议 **16**。环境为 Go 1.25.12、Node 24.19.0、Linux amd64 和 Chromium 155.0.8059.84。交付为源码，未发布容器镜像。

## 代码与构建

- 最终 `go test ./... -count=1 -timeout=300s` 全部通过。
- `go vet ./...` 通过。
- `go test -race ./internal/observeproxy -count=1 -timeout=300s` 通过。
- `go test -race ./internal/app -run 'TestObservation(Switch|Manager)' -count=1` 通过。
- `CGO_ENABLED=0` 构建 `rundesk` 与 `kun` 通过，版本分别为 0.49.1 和 0.16.0。
- 91 个 JavaScript 文件语法检查通过；文档链接、OpenAPI 版本与既有权限契约检查通过。

命令、时长与结果见 [checks.json](validation/current/0.49.1/checks.json)、[完整 Go 测试](validation/current/0.49.1/go-test.log)、[代理 race](validation/current/0.49.1/go-race-proxy.log)、[会话开关 race](validation/current/0.49.1/observation-switch-race.log)。

## 会话采集开关

本轮测试确认：默认关闭；CLI 只影响以后创建的 Codex 会话；开关持久保存，重启不覆盖已有值；缺少新字段的旧会话按关闭加载；关闭时不读取采集配置、不启动代理监听器、不改原生线程参数。Kun 不接受开启，应用只读凭据与跨应用凭据不能修改开关。

子进程 App Server 测试替身贯通真实 Manager、HTTP 代理和 SQLite，覆盖运行中保存偏好不取消当前请求、下一轮重连恢复原 thread、开启和关闭的双向切换、状态与记录更新。旧连接延迟结束的记录仍保存，但只有匹配当前 `captureId` 的记录才可将新连接标记为 `observed`。

测试中的 App Server 是明确的协议替身；真实 HTTP 网络、保存与读取路径由本机实现执行。此结果不替代真实 Codex 跨进程恢复验证。

## 浏览器

- [采集设置](validation/current/0.49.1/capture-settings-browser.log)：实际 Chromium + HTTP 会话 API 测试替身。覆盖菜单入口、默认关闭、保存偏好与当前代理状态分离、刷新读取、失败回滚、错误会话响应拒绝、响应丢失后的 GET 核对、切换会话后的迟到响应、Kun 限制与移动布局。
- [统一工作台回归](validation/current/0.49.1/unified-browser.log)：实际 Chromium + DebugService 测试替身。覆盖全部来源事件、精确整数与 Unicode 分页、HTTP/SSE 字节重组、WebSocket 可读消息与压缩标记、共享固定范围、单个 Kun 面板、历史控制保护与只读检查。

两组浏览器检查均通过，无页面异常。桌面与移动端界面已检查；测试没有调用线上模型。

## 真实 Codex 与恢复边界

本轮执行：

```bash
python3 scripts/codex-observation-smoke.py \
  --codex /opt/codex/versions/0.158.0/bin/codex \
  --include-openai --persistent
```

Codex **0.158.0** 使用临时隔离配置、合成 API Key 和本地上游。持久 `thread/start` 使用 `environments: []`；自定义 provider 的 HTTP 基线/地址覆盖，以及内置 provider 的原生 WebSocket 基线/地址覆盖四项均通过。核对模型、instructions、input、六项工具 schema、stream 与认证头保持，见 [持久线程 CLI 日志](validation/current/0.49.1/codex-observation-persistent-smoke.log)。0.49.0 的持久线程创建超时已定位到测试请求漏传 `environments: []`，该历史限制已由本轮测试更正。

**真实新进程 `thread/resume` 在当前环境仍超时，动态恢复验证尚未完成。** 对精确 `rust-v0.158.0` 官方源码的核实显示：冷恢复[重新加载当前配置](https://github.com/openai/codex/blob/rust-v0.158.0/codex-rs/app-server/src/request_processors/thread_processor.rs#L3895-L3934)，[持久化模型字段合并](https://github.com/openai/codex/blob/rust-v0.158.0/codex-rs/app-server/src/request_processors/thread_processor.rs#L228-L245)不包含端点 URL，配置构建[合并本进程 CLI 与本次请求覆盖](https://github.com/openai/codex/blob/rust-v0.158.0/codex-rs/app-server/src/config_manager.rs#L429-L463)。由此确认新进程不会从旧线程恢复代理地址。同进程已加载线程会忽略配置覆盖，因此实现通过重连 App Server 应用开关。源码依据和 Manager 集成测试不能替代真实 CLI 冷恢复验收。

## 历史结果与目标环境验收

[0.49.0 验证记录](validation/current/0.49.0/VALIDATION.md)保留当时的完整采集、查询、RPC、Kun 浏览器与临时线程 CLI 结果；本轮没有重跑所有历史浏览器场景，也没有将旧日志改标为 0.49.1。

没有使用真实 ChatGPT/OpenAI 账户或真实模型凭据；未完成 news2douyin 真实研究、Docker 到宿主的观测链路、托管工作区路由、生产负载或真实搜索质量验收。正文与队列上限、连接结束后入库、压缩帧及认证路由边界见 [统一调试与采集](../../UNIFIED_DEBUGGING.md)。部署后需用目标 Codex 版本和认证核对开关前后的连接恢复、请求正文与逐条完整性，再确认业务侧回执和报告。
