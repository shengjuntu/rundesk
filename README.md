# RunDesk

[简体中文](README.zh-CN.md) · [Documentation](docs/README.md)

RunDesk is an Agent execution platform for applications: one API connects Kun and Codex and manages tasks, sessions, tool approvals, and artifacts. The independent Kun engine makes execution traces debuggable through step controls, breakpoints, replay, and comparative evaluation. Independently validated successful traces can become reusable Skills, carrying debugging results into real application workflows.

Video build integration: the built-in video-build profile is now 1.3.0 with adapter contract 2, pinned MIT video-use sources, continuous external narration and optional exact-text references on EDL clips for Video App 0.22. Runtime diagnostics inspect the selected running container, verify shared-workspace roundtrips and receive a nonce-bound PNG through the current MCP configuration; they do not call a model, prove model vision or rebuild environments. Existing unreferenced EDLs remain compatible. Rebuild the tool image, select it and explicitly update the application execution environment while retaining its workspace and model credentials. See [video-build](examples/video-build/README.md).

## Current release

**RunDesk 0.49.4 · external Kun 0.17.0 · worker protocol 16**. Kun now builds and ships as an independent project. RunDesk links only a lightweight SDK snapshot and connects to the selected executable through JSONL; the engine owns its execution journal, while RunDesk owns platform data and the debugging workbench. Existing video-build and default-off Codex HTTP observation behavior are retained. See [Kun separation and compatibility](docs/KUN-SEPARATION.md).

- Prepare and reuse an application’s configured Docker environment before work, without changing its image or disconnecting active sessions.
- Integrate applications with scoped credentials, durable queues, sessions, streaming events, files, and idempotency receipts.
- Inspect recorded traces and model exchanges from one debugging workbench. Kun additionally exposes internal state, step controls, conditional breakpoints, snapshots, diagnostic sessions, and Hybrid/Live forks.
- Evaluate fixed cases and artifacts, validate parameterized Skills independently, and compare repeated runs with explicit missing evidence.
- Check local prerequisites on opening the workbench. Configure Kun without Codex, or optionally install a pinned Codex release into the data directory.

## Quick start

Use Go 1.25.12 or a compatible newer toolchain. The WebUI is embedded; no frontend build is required.

```bash
make build
./bin/rundesk --data ./data --kun /absolute/path/to/kun/bin/kun
```

Open http://127.0.0.1:3210 and choose Kun or Codex in the setup wizard. Kun uses a tool-capable Chat Completions service; Codex uses its native provider and authentication configuration. Different instances can use different backends; existing sessions retain their backend. `--demo` explores the interface without real model calls or software installation.

HTTP observation stays off until enabled for a Codex session. `--observe-http=true` changes the default for newly created Codex sessions; saved settings of existing sessions are retained. When off, no observation proxy starts and no endpoint discovery is performed for that session. See [capture settings](docs/UNIFIED_DEBUGGING.md).

This source distribution builds RunDesk only. Build Kun separately in its own project with `go build -o bin/kun ./cmd/kun`, then pass its absolute path with `--kun`, or place it beside RunDesk. Missing or incompatible Kun is an explicit error, with no fallback to Codex. The SDK snapshot under `third_party/kun-sdk` permits a standalone host build without Kun engine sources. Module paths using `your-org` remain unpublished placeholders resolved locally by `go.mod`. See [setup and deployment](docs/SETUP.md) for Windows and optional Codex installation.

## Documentation

[Application API v1](docs/API-V1.md) · [Architecture](docs/ARCHITECTURE.md) · [Backend capabilities](docs/BACKENDS.md) · [Unified debugging and capture](docs/UNIFIED_DEBUGGING.md) · [Trace debugging](docs/DEBUG.md) · [Current status](docs/STATUS.md) · [Validation](docs/VALIDATION.md) · [Upgrading](docs/UPGRADING.md)

RunDesk's own contract is named **RunDesk Control API (RDCA)**; v1 stays at `/api/v1`, with its machine-readable contract at `/api/v1/openapi.json`. [Python client](examples/application_client.py) and [minimal application example](examples/application_quickstart.py) use the same contract. RunDesk remains independent of HarnessRouter. A future UHP server interface will share RunDesk's task and trace core so existing UHP clients can switch to RunDesk and inspect new runs in its debugging workbench; it is not implemented or claimed today. See [API strategy and compatibility plan](docs/API-STRATEGY.md).

## Execution boundaries

Kun Bash is opt-in host execution, not an OS sandbox. Managed Docker environments currently support Codex; Kun container hosting remains pending. Codex does not expose Kun's internal step controls or full context snapshots. An accepted task, a completed tool call, and a successful business outcome are distinct; uncertain side effects are not silently replayed.

## Acknowledgements

Thanks to [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), Michael Kinsy and its contributors, Mario Zechner / Pi, and [HarnessRouter](https://github.com/HarnessRouter/harnessrouter). The independent Kun project retains the selectively incorporated PiG SSE code and its original MIT notices; HarnessRouter/UHP informs the application/backend boundary. [UPSTREAM](docs/UPSTREAM.md) distinguishes incorporated code from design references and preserves the original [notices](LICENSES). No complete PiG or UHP compatibility is claimed.

[License](LICENSE)

## Runtime profiles

See [examples](examples/README.md) for news2douyin, people-search and video-build. The shared CLI inspects, validates, prepares, packages and builds standalone Docker contexts. Research profiles use Exa and include a pinned Agent-Reach toolchain by default; the setup wizard can prepare them before application registration.

Video App 0.7.0 deployment: [VIDEO-APP](docs/VIDEO-APP.md). The renderer image is separate from the managed Codex image.
