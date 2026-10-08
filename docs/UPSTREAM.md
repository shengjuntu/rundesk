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
| `ai/sse_parity_test.go` | `6f67137fd03a985700dd311726b0f73b0bb3ab94b6250f11000e608427bd532d` | `internal/kun/sse_upstream_test.go`; only `TestSSEDecoderAcceptsSSELineEndingsAndDispatchesAtEOF` |

The test hash identifies the complete original file, not the extracted function. Source headers carry attribution; original license text is redistributed. PiG's repository-level provenance identifies its `ai/` family as translating/adapting Pi; both copyright notices are retained.

Local modifications:
- Package renamed to `kun`, with imports and size constant supplied by Kun.
- Bounded accumulated SSE event size and reset comment-only records to prevent unbounded retention.
- Added source notices and linked this provenance ledger.
- Selected test changes only package/imports and notices.
- Kun's provider, loop, JSONL protocol, journal, tools and host integration are new local code, not copied PiG modules.

PiG's TUI, extension/Node runtime, providers/model catalogue, tools, MCP/configuration layers and generated browser assets were not imported. No claim of full Pi behavior or session-format compatibility is made. Future copied files must be added to this ledger with the exact revision, original hash, license and local modifications; review updates individually.

## Verification

SSE newline / EOF parity tests run in `go test ./internal/kun`. Kun's own tests separately cover tool-call fragment assembly, incomplete streams, write boundaries, snapshots, request deduplication, pause/step/steer, cancellation and interrupted-action recovery. RunDesk integration tests build and launch the actual worker against a local model fixture.
