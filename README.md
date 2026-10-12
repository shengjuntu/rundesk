# RunDesk

[简体中文](README.zh-CN.md) · [Documentation](docs/README.md)

RunDesk is an Agent execution platform for applications: one API connects Kun and Codex and manages tasks, sessions, tool approvals, and artifacts. Its independently developed Kun runtime makes execution traces debuggable through step controls, breakpoints, replay, and comparative evaluation. Independently validated successful traces can become reusable Skills, carrying debugging results into real application workflows.

## Current release

**RunDesk 0.45.0 · Kun 0.16.0 · worker protocol 16**. This release consolidates setup, API documentation, and host backend dispatch; the Kun execution kernel and wire protocol are unchanged.

- Integrate applications with scoped credentials, durable queues, sessions, streaming events, files, and idempotency receipts.
- Inspect recorded traces from both backends. Kun additionally exposes internal state, step controls, conditional breakpoints, snapshots, diagnostic sessions, and Hybrid/Live forks.
- Evaluate fixed cases and artifacts, validate parameterized Skills independently, and compare repeated runs with explicit missing evidence.
- Check local prerequisites on opening the workbench. Configure Kun without Codex, or optionally install a pinned Codex release into the data directory.

## Quick start

Use Go 1.25.12 or a compatible newer toolchain. The WebUI is embedded; no frontend build is required.

```bash
make build
./bin/rundesk --data ./data
```

Open http://127.0.0.1:3210 and choose Kun or Codex in the setup wizard. Kun uses a tool-capable Chat Completions service; Codex uses its native provider and authentication configuration. Different instances can use different backends; existing sessions retain their backend. `--demo` explores the interface without real model calls or software installation.

This is a source distribution. Build both executables and keep them together. See [setup and deployment](docs/SETUP.md) for Windows and optional Codex installation.

## Documentation

[Application API v1](docs/API-V1.md) · [Architecture](docs/ARCHITECTURE.md) · [Backend capabilities](docs/BACKENDS.md) · [Trace debugging](docs/DEBUG.md) · [Current status](docs/STATUS.md) · [Validation](docs/VALIDATION.md) · [Upgrading](docs/UPGRADING.md)

The machine-readable contract is served at `/api/v1/openapi.json`. [Python client](examples/application_client.py) and [minimal application example](examples/application_quickstart.py) use the same contract.

## Execution boundaries

Kun Bash is opt-in host execution, not an OS sandbox. Managed Docker environments currently support Codex; Kun container hosting remains pending. Codex does not expose Kun's internal step controls or full context snapshots. An accepted task, a completed tool call, and a successful business outcome are distinct; uncertain side effects are not silently replayed.

## Acknowledgements

Thanks to [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), its contributors, Mario Zechner / Pi, and [HarnessRouter](https://github.com/HarnessRouter/harnessrouter). Kun selectively references PiG while developing independently; HarnessRouter/UHP informs the application/backend boundary. [UPSTREAM](docs/UPSTREAM.md) distinguishes incorporated code from design references and preserves the original [notices](LICENSES). No complete PiG or UHP compatibility is claimed.

[License](LICENSE)
