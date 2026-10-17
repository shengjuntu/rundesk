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
