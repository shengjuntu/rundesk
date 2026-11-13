# RunDesk

[简体中文](README.zh-CN.md) · [Documentation](docs/README.md)

**A debugging workbench, application connector, and research environment for Agent Harnesses.**

RunDesk makes agent execution observable and usable by applications. Codex executes everyday conversations and application tasks. The independently developed Kun engine handles internal maintenance and trace questions. A shared web workbench and APIs expose tasks, progress, approvals, execution traces, and artifacts.

An **Agent Harness** is the runtime around a model: it manages context, the execution loop, tools, and runtime state. RunDesk connects applications and developers to that runtime. Skills, plugins, MCP services, and execution environments can be combined for each use case without imposing a fixed workflow.

## Three missions

### 1. Debug agents being deployed in real applications

This is RunDesk's starting point. Engineers bringing agents into enterprise applications need to see what their agents actually did, where a task stalled, and why it failed.

RunDesk brings user requests, model interactions, tool calls, approvals, errors, and artifacts into one debugging workbench. Developers can inspect recorded behavior and execution traces, follow the evidence, and diagnose problems. Both Codex and Kun have trace inspection; Kun additionally exposes internal state, stepping, breakpoints, checkpoints, and runtime forks. Capabilities remain explicit for each backend.

Start with [trace debugging](docs/TRACE.md#debug-overview), the [trace reader](docs/TRACE.md#trace-reader), and [backend capabilities](docs/BACKENDS.md).

### 2. Connect focused applications to Agent Harnesses

Application developers should be able to concentrate on their own domain—news research, media production, people search, or another focused product—while RunDesk handles the connection to the agent runtime.

The intended application contract is simple: **submit a task → track its progress → retrieve its artifacts**. RunDesk supplies scoped application credentials, sessions, a durable queue, events, approvals, cancellation, and artifact access. Applications still own their business data, domain services, user experience, and acceptance of results.

**Runtime Presets** lower the setup burden by giving an application a prepared, reusable runtime binding. The goal is to assemble the needed Skills, plugins, MCP services, and tools in advance. In this version, named presets bind administrator-approved Codex Docker instances and workspaces; build recipes and configuration prepare the tools, Skills, and MCP integrations. Native plugin support currently covers inspection and observed component origins; stable native install/uninstall is not implemented. Registering a preset alone does not build an image, install a plugin, or authenticate a model account.

Start with the [application API](docs/API-V1.md), [runtime presets](docs/APPLICATION-RUNTIME-PRESETS.md), [build recipes](recipes/README.md), and [build center](docs/BUILD-CENTER.md#build-workbench).

### 3. Provide an experimental environment for Harness researchers

Harness developers and researchers need a place to test execution strategies, inspect their effects, and compare results against recorded evidence.

RunDesk's Kun integration supports module-state inspection and loop-policy selection. Research APIs retain offline record experiments, Hybrid/Live forks, fixed task sets, and comparative evaluation; validated traces can become candidate reusable tool flows. These remain primarily Kun-specific research capabilities, with business effects still requiring validation. RunDesk does not yet expose a dynamically loadable SDK for arbitrary Harness modules.

Start with [Harness controls](docs/KUN-HARNESS.md). The [runtime fork](docs/KUN-FORKS.md#runtime-forks) and [repeated comparison](docs/KUN-EVALUATION.md#repeated-benchmarks) documents describe retained research APIs and historical workflows, not current navigation entries.

## Conversations and production environments

Open a workspace and start a conversation with the default Codex. Applications use their authorized runtime presets. Build Center manages recipes, images and reusable production environments. Model, Skills, MCP and plugin settings apply to the environment and workspace shown in the editor.

An execution instance still has a persistent ID for authorization, configuration and audit. Users can start everyday conversations directly from their workspace. See the [storage contract](docs/STORAGE.md) and [WebUI guide](docs/WEBUI.md).

Administrators can use the [Skill center](docs/SKILL-CENTER.md) to search ClawHub, SkillsMP, skills.sh and GitHub, retrieve fixed source content, inspect complete file lists and static reports, and install reviewed Skills into a workspace or runtime environment. Search and the built-in inspection run in Go; an external SkillSpector CLI is optional. Findings and incomplete checks require an explicit acknowledgment and review reason before installation.

## Current source version

**RunDesk 0.73.0 source · external Kun 0.17.0 · worker protocol 16**

Version 0.73.0 adds the administrator Skill center described above. Its source snapshots bind retrieved content, inspection reports and installation receipts; historical session Skill content snapshots and runtime protection remain outside this change. The external SkillSpector adapter has protocol-fixture and simulated-CLI coverage; NVIDIA's tool has not been installed and run for this integration.

Version 0.72.0 records the configuration declarations and execution identity at admission and ties generated file records to their producing run when evidence is available. Inspect a selected run in the trace view to see what RunDesk recorded at that time. Existing workspaces, session IDs, native histories and application preset bindings remain available. The [upgrade guide](docs/UPGRADING.md) explains compatibility with older records.

The 0.67.1 video-build temporary-space fix remains: verification uses a 2 GiB `/tmp` allowance while preserving the upstream doctor checks. The user's same-image result and this release's checks are recorded separately in [validation](docs/VALIDATION.md#video-build-tmpfs-investigation-2026-11-08). An image must still pass verification before it is marked ready.

Version 0.67.0 introduced the consolidated guides and test suites and explicit shared recipe inputs through `sharedFiles`. Exported contexts contain ordinary files and remain independently buildable; local recipe files can override shared targets. Generated QA results stay under `build/qa/`, and earlier raw evidence remains in the preserved 0.65.0 source package.

`recipes/` is the single editable recipe source. Shared tools, Skills and reviewed local dependencies live under `recipes/common/`; each recipe declares the files it uses. Missing declared sources fail explicitly, while generic templates remain self-contained. Production readers live under `internal/`, application clients under `clients/python/`, and service files under `deploy/systemd/`. Build snapshots generate their own lock files. Reviewed local Agent-Reach sources and the wheel packaging fix remain in news2douyin and people-search 1.1.2; other dependencies still require their download channels. Use the [recipe CLI](recipes/README.md) and [deployment instructions](docs/SETUP.md).

New installations use a three-step guide: **find and configure LLM → configure and install Codex → prepare Docker environments**. Docker is optional and setup can be reopened from **Environment setup**. Downloading a ready image, building locally and checking an existing image are distinct choices. A preparation task keeps its failure, Kun repair, candidate builds and verification together; repair completion alone does not prove an image is verified.

Daily conversations and application tasks use Codex; Kun handles installation and recipe repair plus read-only trace questions. Skill extraction and MCP optimization remain future work. Workspaces organize conversations. Ordinary new conversations use the default Codex. Applications may bind several approved environments and create isolated project workspaces. See [WebUI](docs/WEBUI.md), [internal maintenance](docs/MAINTENANCE.md) and [application presets](docs/APPLICATION-RUNTIME-PRESETS.md).

Build Center connects **recipe → image → production environment**. Dockerfiles and source lists are editable files, with international and China mirror groups covering pip, conda, npm, Docker, APT, YUM/DNF, Hugging Face and GitHub; each recipe must declare the arguments it supports. Builds freeze their inputs and can reuse matching managed Codex runtimes. Codex provider settings are separate from Kun model discovery and apply to the selected Agent/workspace environment. See [Build Center](docs/BUILD-CENTER.md#build-workbench), [build sources](docs/BUILD-SOURCES.md) and [model providers](docs/CODEX-PROVIDERS.md).

Publishers can distribute fixed image digests after matching offline and smoke checks. **The bundled public-image catalog is empty until real images are published**; no official image namespace or anonymous-download acceptance is implied. See [image distribution](docs/IMAGE-DISTRIBUTION.md).

Two API entry points share the same task core:

| Entry point | Purpose |
| --- | --- |
| **RDCA v1** — `/api/v1` | Application and management API; machine-readable contract at `/api/v1/openapi.json` |
| **UHP** — `/v1` | Additional interface targeting protocol `2026-10-19`, with [documented limits](docs/UHP.md) and no full conformance claim |

RunDesk runs independently of HarnessRouter. Unsupported UHP step/token budgets remain disclosed. Current behavior, execution boundaries and recipe component versions are maintained in [status](docs/STATUS.md), [architecture](docs/ARCHITECTURE.md) and the [profile documentation](recipes/README.md).

## Quick start

A staged Linux or Windows package includes RunDesk, `recipes/`, `config/` and a platform launcher. The standalone Kun executable is included when explicitly provided for packaging. The resulting package does not require Go, Python, Node.js or npm on the host.

On Linux, extract the package and run:

```bash
chmod +x ./start-rundesk.sh
./start-rundesk.sh
```

On Windows, extract the package and double-click `start-rundesk.cmd`. Open <http://127.0.0.1:3210>; a new installation starts with the three-step wizard. Reopen it later from the gear button labeled “Environment check” at the bottom of the navigation rail. Use an existing local model service or enter a remote endpoint. Docker Engine is optional and must be installed separately when you want to pull, build, check or run images.

### Build from source

Use **Go 1.25.12** or a compatible newer toolchain. The WebUI is embedded; no frontend build is required.

```bash
make build
./bin/rundesk --data ./data
```

Use `make package PACKAGE_DIR=../rundesk-portable` to stage a new deployment directory outside this checkout; add `KUN_BINARY=/absolute/path/to/kun` when needed. When deploying the binary outside this source checkout, also deploy `recipes/` and `config/` beside it. Set `RUNDESK_RECIPES_DIR` for another recipe directory and `RUNDESK_BUILD_SOURCES` for a chosen source-list JSON file. The WebUI remains embedded; editable build assets are external. See [deployment](docs/SETUP.md).

Open <http://127.0.0.1:3210> to initialize the installation. Discovery checks the machine running RunDesk, which may be a remote server. You can enter a model endpoint manually and skip Codex or Docker when they are not needed yet. To explore the interface without real model calls:

```bash
./bin/rundesk --data ./demo-data --demo
```

For Kun, build the independent Kun project, then pass its executable:

```bash
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

This package builds RunDesk only. The lightweight SDK in `third_party/kun-sdk` allows the host to build without Kun engine sources. The `your-org` module paths are unpublished placeholders resolved locally by `go.mod`. Missing or incompatible Kun is reported explicitly.

Kun uses a tool-capable Chat Completions service; Codex uses its native provider and authentication configuration. Daily execution uses Codex environments; Kun configurations serve maintenance and retained research APIs. Existing records retain their identity, while ordinary Kun conversations can no longer receive new daily turns. See [setup and deployment](docs/SETUP.md) for Windows, Docker, and optional Codex installation.

## Choose your path

| Audience | Documentation |
| --- | --- |
| Agent deployment engineers | [Debugging](docs/TRACE.md#debug-overview) · [Observation and capture](docs/UNIFIED_DEBUGGING.md) · [Read-only debug API/MCP](docs/DEBUG-SERVICE.md) |
| Application developers | [Connect an application](docs/APPLICATION-CONNECT.md) · [API](docs/API-V1.md) · [Presets](docs/APPLICATION-RUNTIME-PRESETS.md) · [Python example](clients/python/application_quickstart.py) |
| Harness developers and researchers | [Kun integration](docs/KUN.md) · [Harness controls](docs/KUN-HARNESS.md) · [Research APIs and historical workflows](docs/README.md#3-harness-开发与研究--harness-research) |
| Maintainers and operators | [Architecture](docs/ARCHITECTURE.md) · [Skill center](docs/SKILL-CENTER.md) · [Build center](docs/BUILD-CENTER.md#build-workbench) · [Download sources](docs/BUILD-SOURCES.md) · [Upgrading](docs/UPGRADING.md) · [Security](SECURITY.md) |

For local development, `make test` runs the host and SDK tests; `make check` adds race checks and `go vet`. Some integration checks require an independent Kun executable. See [validation](docs/VALIDATION.md) for reproduction and current scope, and [provenance](docs/validation/README.md) for preserved historical results.

## Execution boundaries

- Codex HTTP observation is off by default and enabled per session. `--observe-http=true` changes the default for new Codex sessions; it does not overwrite saved settings. Capture covers only traffic routed through the configured proxy and reports incomplete evidence explicitly.
- Managed Docker environments currently support Codex. Kun Bash is opt-in host execution; file-tool path limits are not an OS sandbox. Kun container hosting remains pending.
- A completed task, a successful tool call, and an accepted business outcome are separate facts. Unknown side effects are not silently replayed. Recorded evidence does not reveal unexposed model reasoning.
- Native plugin installation, arbitrary backend modules, and full UHP conformance are not implied by the three missions. See [status](docs/STATUS.md) for implemented capabilities and remaining work.

## Acknowledgements and license

Thanks to [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), Michael Kinsy and contributors, Mario Zechner / Pi, and [HarnessRouter](https://github.com/HarnessRouter/harnessrouter). The independent Kun project retains selectively incorporated PiG SSE code and its original MIT notices; HarnessRouter/UHP informs the application/backend boundary. [Upstream provenance](docs/UPSTREAM.md) distinguishes incorporated code from design references.

[License](LICENSE) · [Third-party notices](THIRD_PARTY_NOTICES.md) · [License texts](LICENSES)
