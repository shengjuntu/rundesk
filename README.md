# RunDesk

[简体中文](README.zh-CN.md) · [Documentation](docs/README.md)

**A debugging workbench, application connector, and research environment for Agent Harnesses.**

RunDesk makes agent execution observable and usable by applications. It connects to Codex and the independently developed Kun engine, providing a web workbench and APIs for tasks, progress, approvals, execution traces, and artifacts.

An **Agent Harness** is the runtime around a model: it manages context, the execution loop, tools, and runtime state. RunDesk connects applications and developers to that runtime. Skills, plugins, MCP services, and execution environments can be combined for each use case without imposing a fixed workflow.

## Three missions

### 1. Debug agents being deployed in real applications

This is RunDesk's starting point. Engineers bringing agents into enterprise applications need to see what their agents actually did, where a task stalled, and why it failed.

RunDesk brings user requests, model interactions, tool calls, approvals, errors, and artifacts into one debugging workbench. Developers can inspect recorded behavior and execution traces, follow the evidence, and diagnose problems. Both Codex and Kun have trace inspection; Kun additionally exposes internal state, stepping, breakpoints, checkpoints, and runtime forks. Capabilities remain explicit for each backend.

Start with [trace debugging](docs/DEBUG.md), the [trace reader](docs/TRACE-READER.md), and [backend capabilities](docs/BACKENDS.md).

### 2. Connect focused applications to Agent Harnesses

Application developers should be able to concentrate on their own domain—news research, media production, people search, or another focused product—while RunDesk handles the connection to the agent runtime.

The intended application contract is simple: **submit a task → track its progress → retrieve its artifacts**. RunDesk supplies scoped application credentials, sessions, a durable queue, events, approvals, cancellation, and artifact access. Applications still own their business data, domain services, user experience, and acceptance of results.

**Runtime Presets** lower the setup burden by giving an application a prepared, reusable runtime binding. The goal is to assemble the needed Skills, plugins, MCP services, and tools in advance. In this version, named presets bind administrator-approved Codex Docker instances and workspaces; build recipes and configuration prepare the tools, Skills, and MCP integrations. Native plugin support currently covers inspection and observed component origins; stable native install/uninstall is not implemented. Registering a preset alone does not build an image, install a plugin, or authenticate a model account.

Start with the [application API](docs/API-V1.md), [runtime presets](docs/APPLICATION-RUNTIME-PRESETS.md), [example profiles](examples/README.md), and [build center](docs/BUILD-CENTER.md).

### 3. Provide an experimental environment for Harness researchers

Harness developers and researchers need a place to test execution strategies, inspect their effects, and compare results against recorded evidence.

RunDesk's Kun integration supports module-state inspection and loop-policy selection. Research APIs retain offline record experiments, Hybrid/Live forks, fixed task sets, and comparative evaluation; validated traces can become candidate reusable tool flows. These remain primarily Kun-specific research capabilities, with business effects still requiring validation. RunDesk does not yet expose a dynamically loadable SDK for arbitrary Harness modules.

Start with [Harness controls](docs/KUN-HARNESS.md). The [runtime fork](docs/KUN-FORKS.md) and [repeated comparison](docs/KUN-BENCHMARKS.md) documents describe retained research APIs and historical workflows, not current navigation entries.

## Work with instances

An **instance** is the main object for daily work: choose a Codex or Kun instance, inspect its model and capabilities, select a workspace, then run tasks and inspect artifacts and traces. The build center turns a build preset into an image; an image can support multiple independent instances. Application registration remains a separate integration step and is not required to manage an instance.

Version 0.54.0 removes the Kun forks, evaluation task sets, K4 Skill workbench, and offline experiment launchers from everyday navigation. Their APIs and stored data remain, as does inspection of existing fork previews. K4's parameterized tool flows are distinct from ordinary `SKILL.md` management.

This release reorganizes the WebUI; it does **not** yet synchronize instance configuration across workspaces. Docker configuration still lives in separate instance/workspace environments. Automatic collection into a shared capability catalog and translation between Agent configuration formats are also pending. See [WebUI behavior and limits](docs/WEBUI.md).

## Current source version

**RunDesk 0.54.0 source · external Kun 0.17.0 · worker protocol 16**

This source includes runtime/resource contracts, a unified observation model, the conversation trace reader, the build center, and two API entry points over the same task core:

| Entry point | Purpose |
| --- | --- |
| **RDCA v1** — `/api/v1` | RunDesk's application and management API; machine-readable contract at `/api/v1/openapi.json` |
| **UHP** — `/v1` | An additional service interface targeting protocol `2026-10-19`; implemented with [documented limits](docs/UHP.md), without a full conformance claim |

Version 0.54.0 focuses on instance navigation and configuration context without changing the storage or API schemas. It retains the persistent deadlines, progress and approval handoff, explicit capabilities, and [Python UHP client](examples/uhp_client.py) introduced in 0.53.0. Unsupported step/token budgets remain disclosed; applications can require supported capabilities before submission.

RunDesk runs independently of HarnessRouter. See [current status](docs/STATUS.md), [architecture](docs/ARCHITECTURE.md), and [validation scope](docs/VALIDATION.md). Video and research integrations are examples of the application mission; their component versions belong in the [profile documentation](examples/README.md).

## Quick start

Use **Go 1.25.12** or a compatible newer toolchain. The WebUI is embedded; no frontend build is required.

```bash
make build
./bin/rundesk --data ./data
```

Open <http://127.0.0.1:3210> and configure Codex or Kun in the setup wizard. To explore the interface without real model calls:

```bash
./bin/rundesk --data ./demo-data --demo
```

For Kun, build the independent Kun project, then pass its executable:

```bash
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

This package builds RunDesk only. The lightweight SDK in `third_party/kun-sdk` allows the host to build without Kun engine sources. The `your-org` module paths are unpublished placeholders resolved locally by `go.mod`. Missing or incompatible Kun is reported explicitly.

Kun uses a tool-capable Chat Completions service; Codex uses its native provider and authentication configuration. Instances may use different backends, while existing sessions retain their original backend. See [setup and deployment](docs/SETUP.md) for Windows, Docker, and optional Codex installation.

## Choose your path

| Audience | Documentation |
| --- | --- |
| Agent deployment engineers | [Debugging](docs/DEBUG.md) · [Observation and capture](docs/UNIFIED_DEBUGGING.md) · [Read-only debug API/MCP](docs/DEBUG-SERVICE.md) |
| Application developers | [Connect an application](docs/APPLICATION-CONNECT.md) · [API](docs/API-V1.md) · [Presets](docs/APPLICATION-RUNTIME-PRESETS.md) · [Python example](examples/application_quickstart.py) |
| Harness developers and researchers | [Kun integration](docs/KUN.md) · [Harness controls](docs/KUN-HARNESS.md) · [Research APIs and historical workflows](docs/README.md#3-harness-开发与研究--harness-research) |
| Maintainers and operators | [Architecture](docs/ARCHITECTURE.md) · [Build center](docs/BUILD-CENTER.md) · [Upgrading](docs/UPGRADING.md) · [Security](SECURITY.md) |

For local development, `make test` runs the host and SDK tests; `make check` adds race checks and `go vet`. Some integration checks require an independent Kun executable. Reproduction instructions and historical results are separated in [validation](docs/VALIDATION.md).

## Execution boundaries

- Codex HTTP observation is off by default and enabled per session. `--observe-http=true` changes the default for new Codex sessions; it does not overwrite saved settings. Capture covers only traffic routed through the configured proxy and reports incomplete evidence explicitly.
- Managed Docker environments currently support Codex. Kun Bash is opt-in host execution; file-tool path limits are not an OS sandbox. Kun container hosting remains pending.
- A completed task, a successful tool call, and an accepted business outcome are separate facts. Unknown side effects are not silently replayed. Recorded evidence does not reveal unexposed model reasoning.
- Native plugin installation, arbitrary backend modules, and full UHP conformance are not implied by the three missions. See [status](docs/STATUS.md) for implemented capabilities and remaining work.

## Acknowledgements and license

Thanks to [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), Michael Kinsy and contributors, Mario Zechner / Pi, and [HarnessRouter](https://github.com/HarnessRouter/harnessrouter). The independent Kun project retains selectively incorporated PiG SSE code and its original MIT notices; HarnessRouter/UHP informs the application/backend boundary. [Upstream provenance](docs/UPSTREAM.md) distinguishes incorporated code from design references.

[License](LICENSE) · [Third-party notices](THIRD_PARTY_NOTICES.md) · [License texts](LICENSES)
