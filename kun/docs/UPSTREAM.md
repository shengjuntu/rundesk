# Kun upstream provenance

Kun 0.17.0 is an independent source project extracted from the Kun worker in RunDesk 0.49.3 / Kun 0.16.0. The extraction changes module paths and the host interface; it does not add PiG implementation modules. [UPSTREAM-RUNDESK-BASELINE.md](UPSTREAM-RUNDESK-BASELINE.md) is the archived pre-separation ledger. Paths below describe the current Kun project.

## Incorporated PiG source

Thank you to [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), Michael Kinsy and the PiG contributors, and to Mario Zechner / Pi. No endorsement or complete compatibility is claimed.

- Fixed upstream commit: `827932db70e545b34c3f1d9c04f58a70eacd3b28`.
- PiG MIT copyright: `Copyright Hewlett Packard Enterprise Development LP`; original text in [PiG-MIT.txt](../LICENSES/PiG-MIT.txt).
- Pi attribution carried by PiG: `Copyright (c) 2025 Mario Zechner`; original text in [Pi-MIT.txt](../LICENSES/Pi-MIT.txt).

| Original upstream path | Original SHA-256 | Current Kun path and incorporated scope |
| --- | --- | --- |
| `ai/sse.go` | `8fea8c49a7ead126ce93542465b380c2d98dc4d01b65040242e20f783b8f3e38` | `internal/engine/sse.go`; SSE decoder and stream-error helpers |
| `mcp/sse.go` | `258ebec94ca2e9c74ed1360ffd1fc0e7297e1a383260ad9189cf908599b33f5d` | `internal/mcp/sse.go`; MCP SSE decoder |
| `ai/sse_parity_test.go` | `6f67137fd03a985700dd311726b0f73b0bb3ab94b6250f11000e608427bd532d` | `internal/engine/sse_upstream_test.go`; only `TestSSEDecoderAcceptsSSELineEndingsAndDispatchesAtEOF` |

The test hash identifies the original complete file, not the extracted function. Existing local changes rename the package, supply imports and size constants, bound accumulated SSE event size, reset comment-only records, and add notices. The MCP decoder shares the message limit and permits its callback to stop with an error after the matching response. Extraction moves `internal/kun` to `internal/engine`; notices and original license text are retained.

Kun's provider, loop, JSONL protocol, journal, tools, approval and host integration remain independent local implementation. PiG's TUI, extension/Node runtime, provider/model catalog, tools and full MCP configuration were not imported. No additional PiG source was incorporated in 0.17.0. Future source incorporation must record its revision, exact files, original hashes, licenses and local changes here.

## Path mapping from the RunDesk baseline

| Archived RunDesk path | Independent Kun path |
| --- | --- |
| `cmd/kun` | `cmd/kun` |
| `internal/kun` | `internal/engine` |
| `internal/kunproto` | `sdk/protocol` |
| `internal/adapters/kun` | `sdk/client` |
| Kun pure validation helpers | `sdk/validation` |
| `internal/mcp` | `internal/mcp` |
| `LICENSES/PiG-MIT.txt`, `LICENSES/Pi-MIT.txt` | Same paths; original notices unchanged |

RunDesk consumes a checked snapshot of the SDK in `third_party/kun-sdk`; it does not vendor the Kun engine. The current RunDesk provenance ledger separately describes any source retained by the host.

## Design references

The archived ledger records the PiG tool behavior references and the HarnessRouter/UHP boundary reference used during RunDesk development. Those entries distinguish inspected designs from copied implementation. The extraction does not introduce UHP compatibility, PiG session compatibility or additional upstream code. Historical upstream hashes remain in the archived ledger; its old local paths and RunDesk runtime-profile entries are historical context rather than current Kun dependencies.

## Verification

Run `go test ./internal/engine ./internal/mcp` for SSE parity and transport tests, `go test ./...` for the engine and CLI, and `(cd sdk && go test ./...)` for the public contract. Cross-process compatibility is verified separately against an explicitly selected external engine in RunDesk. A successful unit or local-fixture test is not a claim of live provider or deployed runtime acceptance.
