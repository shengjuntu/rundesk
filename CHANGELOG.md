# Changelog

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
