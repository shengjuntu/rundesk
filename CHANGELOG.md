# 0.51.0

- 保留自研 video-build，新增 HyperFrames、video-use、OpenMontage 三套独立上游配置和 Codex Docker 构建入口。
- 保留固定上游 Skills/原生工程与渲染流程，增加统一 Video App 生成 MCP 接入及精确源码、依赖、许可记录。
- 接入初始化构建目录、镜像身份与离线检查；生成配置和密钥在运行时提供，不打包到镜像。
- 配套 App 0.26.0 独立文本 TTS，不以切换环境代替自研工程转换。

# 0.50.0

- Add administrator-approved named runtime presets for one application. Each preset binds a dedicated Codex Docker instance and workspace; names and enabled state may change, while the execution identity remains fixed. Registration does not start containers, install tools or replace the application's default image.
- Extend connect, runtime preparation, diagnostics, sessions, queued tasks and schedules with explicit preset selection. Disabled presets retain history and reject new execution; cancellation and uncertain submissions keep their original session, binding and receipt.
- Freeze application-key preset bindings and match member grants by application, optional preset and workspace. New presets never expand old credentials; legacy default-workspace grants do not authorize dedicated preset workspaces.
- Add management cards for creating/binding presets, scoped configuration and task history. Creation retries reuse accepted resources; renaming or disabling a preset does not interrupt running environments.
- Pair with Video App 0.25.0: project creation freezes the selected production profile, and connection records the exact runtime binding. Changing the App startup default does not migrate existing projects or sessions; tool profiles and rendering backends remain separate.
- Keep Kun as independent 0.17.0, worker protocol 16, RDCA `/api/v1`, native Codex and default-off HTTP observation. Historical release validation remains intact; real Docker, model authentication and autonomous production remain deployment acceptance work.
- See [application runtime presets](docs/APPLICATION-RUNTIME-PRESETS.md) and [0.50.0 validation scope](docs/validation-0.50.0.json).

# 0.49.5

- Update video-build to 1.4.0: HyperFrames 0.8.87, GSAP 3.15.0, Node.js, FFmpeg and fonts are the default animation toolchain. Pinned MIT video-use and adapter contract 2 remain an explicit optional compatibility path.
- Ship original Codex-directed production guidance for Video App 0.23: autonomous storyboarding, asset selection/generation, TTS, HTML/CSS/JavaScript animation and actual output review. Stage artifacts do not add repeated approval gates.
- Keep App MCP authoring/rendering separate from the managed tool image; the HyperFrames main path requires no shared production directory. Old video_use exchange and archived EDLs retain their own prerequisites.
- Add offline HyperFrames media checks and explicit profile dependency reporting. Installation, PNG transport and local smoke evidence do not prove real model production or visual understanding.
- Retain the independent Kun 0.17.0 boundary, worker protocol 16, native Codex adapter and default-off HTTP observation. RunDesk Control API (RDCA) retains /api/v1; UHP compatibility remains planned and unimplemented.
- Preserve validation scope by release; earlier 0.49.4 checks are historical evidence, not 0.49.5 full race acceptance.

# 0.49.4

- Extract Kun 0.17.0 into an independently buildable project. RunDesk no longer contains or builds `cmd/kun` or the Kun execution kernel; it links a checked lightweight SDK snapshot under `third_party/kun-sdk`.
- Keep JSONL protocol 16 core methods, negotiate the selected engine version and actual capabilities, and report missing/incompatible workers explicitly without falling back to Codex.
- Move diagnostic evidence access to the existing host-provided `__trace_mcp` read-only bridge. Kun no longer knows the RunDesk trace database schema. Old host/core compatibility and the diagnostic provider requirement are documented separately.
- Preserve execution-directory layout and explicit 0.16.0 checkpoint/fork compatibility in Kun 0.17.0. No manifest editing or blanket migration is performed.
- Retain PiG/Pi acknowledgements, source notices and original MIT license texts. This release adds no new PiG modules, provider, HTTP service or multimodal capability.

# 0.49.3

- 新增应用范围的runtime/diagnostics：仅探测已选且运行中的Codex Docker容器，核对绑定与实际环境，不自动启动、重建、换镜像或回退宿主机。
- 固定挑战验证专用共享工作区双向读写；通过当前MCP配置取得nonce绑定的真实PNG并校验载荷，配置不可读或未探测时明确记录，不把工具发现当作图像传输或模型视觉证明。
- video-build升级1.3.0、adapter协议2，EDL镜头可选精确原文textRefs，绑定冻结UTF-8原文和指纹；旧无引用EDL保留兼容。
- 适配器验证与报告保存引用范围、覆盖计数和未映射镜头；保持固定MIT video-use版本、连续外部配音与独立归档，内容不强制分句或分场景。
- 使用新增镜像资源需重建、选择并更新应用执行环境；保留原工作区、HOME与认证。真实Docker与模型制作仍需部署验收。
- Kun0.16.0、worker协议16及Codex HTTP观测默认关闭的行为保持不变。

# 0.49.2

- video-build1.2.0打包固定MIT video-use源码、准确Python依赖锁、连续配音脚本素材适配器、离线烟测及三阶段Skill。
- 内置镜像按profile独立版本判断，video-build核对上游commit/adapter标签；保留现有工作区与认证。
- 与Video App0.21通过现有应用工作目录交换冻结输入及可校验产物，无需新增任意目录挂载权限。
- Kun及workerprotocol不变；Codex HTTP观测默认关闭的设置保留。

# Changelog

## RunDesk 0.49.1

- Default Codex HTTP observation to off, persist `observeHTTP` per session, and expose its toggle in the unified debugging workbench.
- Make `--observe-http` a creation default for new Codex sessions. Restarting with a different flag never overwrites saved session preferences.
- Start the pass-through proxy lazily for an enabled thread. Disabled sessions skip observation configuration discovery and endpoint routing.
- Save changes during active work without canceling requests; reconnect and resume the same Codex thread on the next submitted turn. Report requested settings, pending changes and applied state separately.
- Keep Kun's native model evidence unchanged and reject enabling the Codex-only capture setting for Kun sessions. Existing captured history remains readable.

## RunDesk 0.49.0

- Unify trace inspection, LLM request/response evidence, summaries, diagnostic questions and embedded Kun DevTools in one debugging workbench with a shared session and fixed history scope.
- Add an in-process pass-through HTTP observation proxy for supported Codex endpoint configuration. Forward original payloads and streams without API conversion, model replacement, additional retries or failover; record bounded copies and explicit capture-loss markers.
- Add indexed read-only `llm_requests` / `llm_request` queries, exact byte pagination, chunk timing metadata, and matching trace/debug MCP tools. Keep model capture bodies out of ordinary conversation event polling while preserving database evidence.
- Preserve unknown Codex RPC fields and JSON integer precision, and load all source events for full tool argument/result inspection. Keep incomplete association and evidence explicit.
- Keep Kun 0.16.0 / worker protocol 16 unchanged. Proxy capture defaults to enabled on loopback; Docker needs an explicitly reachable private endpoint. Built-in provider authentication paths remain subject to the documented capture coverage limits.

## RunDesk 0.48.1

- Add Video App 0.7.0 deployment guide and a uniform Docker lifecycle entry point under video-build.
- Document separate renderer/App and managed Codex images, persistent storage, HTTP MCP callbacks and initialization-time image preparation.
- Generalize the runtime binding error for research and video applications. Existing API, runtime image format, Kun and worker protocol are unchanged.
- Video App performs Docker/account/MCP readiness checks before new submissions; recovery keeps its original operation receipt.


## RunDesk 0.47.1

- Preserve and validate native Codex `default_tools_approval_mode`; advertise the configuration capability.
- Distinguish empty MCP tool approval from real data-entry forms, so it no longer asks for meaningless JSON.
- Support news2douyin 0.13.1 explicit Docker full-access setup and durable MCP approval configuration.
- Keep Docker mounts, container isolation, Kun 0.16.0 and worker protocol 16 unchanged. Kun rejects the unsupported server-wide Codex setting instead of silently ignoring it.

## RunDesk 0.47.0

- Add scoped application runtime status and prepare APIs, with expected-binding and environment-update guards.
- Reuse/start the configured Codex Docker environment without disconnecting active sessions or changing images.
- Expose sanitized account readiness and MCP tool discovery; do not run a model during preparation.
- Integrate with news2douyin 0.13.0 one-click deep research, container business MCP setup, search evidence records and guarded session binding.
- Add race, permission and concurrent reuse tests; validate the two live HTTP services with Docker/Codex/search fixtures.
- Keep Kun 0.16.0 and worker protocol 16 unchanged. Real container/model/search deployment still needs acceptance.

## RunDesk 0.46.0

- Add three example runtime profiles: news2douyin, people-search, video-build.
- Add a stdlib Python CLI and uniform per-directory entry points for inspect, validate, prepare, pack, build and doctor.
- Generate standalone contexts with fixed Codex versions, profile manifests and SHA256SUMS; compatible with the existing ZIP build UI.
- Add optional Wigolo 0.2.1 stdio MCP integration and Agent-Reach at a pinned user-selected fork commit.
- Include task Skills, source-evidence capture and a real FFmpeg media smoke script.
- Keep server behavior, Kun 0.16.0 and worker protocol 16 unchanged; Docker deployment and live search require environment acceptance.
