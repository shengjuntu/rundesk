# Kun upstream provenance

Kun is developed in the RunDesk repository and runs as a separate process. This repository is not a fork or a complete distribution of PiG.

## PiG source actually incorporated

- Repository: https://github.com/MichaelKinsy/PiG
- Fixed commit: `827932db70e545b34c3f1d9c04f58a70eacd3b28`
- License: MIT. Original HPE copyright and permission text: [PiG-MIT.txt](../LICENSES/PiG-MIT.txt).
- Upstream Pi attribution carried by PiG: Copyright (c) 2025 Mario Zechner. Preserved in [Pi-MIT.txt](../LICENSES/Pi-MIT.txt).
- Thanks to MichaelKinsy/PiG, its contributors, and Mario Zechner / Pi. Kun is an independent project, with no upstream endorsement implied.

| Original path | Original SHA-256 | Local path / scope |
| --- | --- | --- |
| `ai/sse.go` | `8fea8c49a7ead126ce93542465b380c2d98dc4d01b65040242e20f783b8f3e38` | `internal/kun/sse.go`; SSE decoder and stream-error helpers |
| `mcp/sse.go` | `258ebec94ca2e9c74ed1360ffd1fc0e7297e1a383260ad9189cf908599b33f5d` | `internal/mcp/sse.go`; MCP SSE decoder |
| `ai/sse_parity_test.go` | `6f67137fd03a985700dd311726b0f73b0bb3ab94b6250f11000e608427bd532d` | `internal/kun/sse_upstream_test.go`; only `TestSSEDecoderAcceptsSSELineEndingsAndDispatchesAtEOF` |

The test hash identifies the complete original file, not the extracted function. Source headers carry attribution; original license text is redistributed. PiG's repository-level provenance identifies its `ai/` and `mcp/` families as translating/adapting Pi; both copyright notices are retained.

Local modifications:
- Package renamed to `kun`, with imports and size constant supplied by Kun.
- Bounded accumulated SSE event size and reset comment-only records to prevent unbounded retention.
- Added source notices and linked this provenance ledger.
- Selected test changes only package/imports and notices.
- MCP SSE decoder uses the shared `Limit` constant; its callback returns errors so the HTTP client can stop immediately after its matching RPC response. MCP transport and loop tests are new Kun tests, not copied PiG tests.
- Kun's provider, loop, JSONL protocol, journal, tools and host integration are new local code, not copied PiG modules.

PiG's TUI, extension/Node runtime, providers/model catalogue, tools, full MCP client/configuration layers and generated browser assets were not imported. No claim of full Pi behavior or session-format compatibility is made. Future copied files must be added to this ledger with the exact revision, original hash, license and local modifications; review updates individually.

## Verification

SSE newline / EOF parity tests run in `go test ./internal/kun`; MCP chunking, bounds, callback stop and transport tests run in `go test ./internal/mcp`. Kun's own tests separately cover tool-call fragment assembly, incomplete streams, write boundaries, snapshots, request deduplication, pause/step/steer, cancellation and interrupted-action recovery. RunDesk integration tests build and launch the actual worker against a local model fixture.

## Core tool behavior references in 0.44.0

The following files at the same fixed commit were inspected to align core actions and explicit differences. Their implementation was **not copied** into Kun. Kun's Bash, exact-edit and search implementation and tests are original local work integrated with Kun's approval, journal, budget and replay contracts. Tool names and schemas intentionally follow Kun's own protocol; this is functional coverage, not wire-format or feature-complete parity.

| Referenced upstream path | Original SHA-256 |
| --- | --- |
| `internal/codingagent/tools/bash.go` | `98f3dc6b829b7387493fa733a06e2ed1b36f0ba8f327e848b8bfe0d0fe4413bc` |
| `internal/codingagent/tools/bash_executor.go` | `e16d90dfb0e62092d8ba821cdc60a833085d7377614abaab623bbc1933dc209c` |
| `internal/codingagent/tools/edit.go` | `415ef37ca48bd842b9f95c424a6fc0e3b07ce89a44bff4c3a91e5b7340038aab` |
| `internal/codingagent/tools/find.go` | `4c911f5a2fecb30fdd898f1c7a37d2d7e66b268af1a16a5c566a36421e251ee0` |
| `internal/codingagent/tools/grep.go` | `9fc09eb56c36907a26f2cad10a1a670dd1ce12f3df40ba27a0dce9e669b78ddb` |
| `internal/codingagent/tools/read.go` | `ca39ad608a1fcc7f49322c1ad0720673ac738e8a431d58235853f51af9ea2f2a` |
| `internal/codingagent/tools/shell_config.go` | `c477325750c4cde443260db025f0088e6ecc039d7cd4a77093384b22a1565db1` |

PiG's published [security documentation](https://pi-in-go.dev/docs/latest/security/) and [container guide](https://pi-in-go.dev/docs/latest/containerization/) were checked on 2026-10-11. They describe current-user execution and external container/VM isolation, not an embedded OS sandbox. Those rolling docs are contextual references; the code audit above remains pinned. See [alignment matrix](KUN-PIG-CORE.md) for Kun's actual limits.

## HarnessRouter design reference

Thanks to [HarnessRouter](https://github.com/HarnessRouter/harnessrouter) and its contributors. Its README and the [Unified Harness Protocol](https://unifiedharnessprotocol.org/) were consulted on 2026-10-11 for the boundary between applications and complete agent runtimes, configured-backend discovery, and capability/conformance documentation. The protocol site identified version 2026-10-04 as a draft standard at review time.

RunDesk 0.45.0 introduces its own Go host lifecycle interface and capability catalog. No HarnessRouter source, schemas, assets, or conformance code were incorporated. This is a design reference, not an implementation dependency, fork, endorsement, or UHP compatibility claim. Any future source incorporation must separately record the exact commit, files, license, notices, and modifications.

## Runtime profiles in 0.46.0

Thanks to Agent-Reach and Wigolo contributors. On 2026-10-12, the user-selected Agent-Reach fork at `https://github.com/silently0801/agent-reach`, commit `cf70302e928155703829439115a18132928f8913` (package 1.1.0, MIT), was inspected for installation and skill-path behavior. The recipe can install that exact source archive, but does not run its automatic installer or copy its Skill.

Wigolo's official Dockerfile, configuration, package 0.2.1 metadata and licensing guide were consulted for an independently written stdio MCP launcher and optional image recipe. Wigolo is AGPL-3.0-only with alternative commercial terms offered by upstream; no Wigolo code is copied into RunDesk. The package is downloaded only when the selected image is built. See [assessment and source links](../examples/news2douyin/WIGOLO.md) and [dependency notices](../examples/_shared/THIRD_PARTY.md).
