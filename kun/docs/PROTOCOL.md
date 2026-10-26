# Kun worker protocol 16

The public contract lives in the independent `github.com/your-org/kun/sdk` module. The package path is a publication placeholder; the source distributions use local module replacements. The SDK contains serializable protocol types, a subprocess client and pure validation helpers. It does not depend on either RunDesk or Kun's execution implementation.

## Transport and handshake

Start `kun --data <exclusive-session-directory>`. Requests and responses are UTF-8 JSON, one object per line, on stdin/stdout. stderr is reserved for process diagnostics. Messages are bounded by `protocol.MaxMessage` (8 MiB). A host must retain the request ID and distinguish a protocol error from a successful result.

```json
{"version":16,"id":"1","method":"hello","params":{}}
```

The response uses the same ID and has `result` or `error`. `hello.result` identifies `engine`, `engineVersion`, `protocolVersion`, `pid` and `capabilities`. Capabilities describe supported implementation features; the current session state and configuration determine whether an operation is available. An unknown capability is not permission to use it. Hosts must not substitute static catalog entries for the response from the selected executable.

Protocol version 16 preserves the existing core method shapes. The 0.17.0 handshake adds a typed, versioned capability description including the diagnostic provider capability. A protocol mismatch or missing required capability fails explicitly; it does not select another engine.

## Methods

| Method | Responsibility |
| --- | --- |
| `hello` | Version and capability negotiation; no model call |
| `start` | Begin or continue a run using explicit configuration, source and permissions |
| `state` | Read current execution and control state |
| `events` | Read execution events after a worker sequence |
| `snapshot` | Read a retained execution snapshot by sequence |
| `query` | Bounded structured inspection and evidence queries |
| `control` | Revision-checked pause, step, continue, steer, cancellation or approval operations |
| `checkpoint` | Inspect a compatible checkpoint for a proposed start |
| `fork.points` | List safe fork points |
| `fork.export` | Export a validated fork package |

Use the SDK types for the complete fields and limits. Worker sequences, host event IDs and state revisions are different identifiers. A successful control submission is not proof the control was applied; read its recorded receipt. Timeouts, process termination and unknown action outcomes must not be treated as authorization to replay side effects.

## Diagnostic provider boundary

Kun 0.17.0 does not import RunDesk's trace-query implementation or open a RunDesk trace database. A diagnostic start declares an immutable source scope and a provider using the host's existing `__trace_mcp` read-only MCP bridge. The host owns its snapshot format, authentication, fixed event upper bound and evidence-query implementation. Kun receives only the advertised, restricted trace tools and validates their use.

The bridge is diagnostic evidence access, not general business MCP access. Diagnostic mode continues to reject ordinary Skills, arbitrary file writes, Bash and changing the selected source. Suggestions remain reviewable evidence-backed proposals; no diagnostic tool applies a source-run modification. Hosts must check the diagnostic provider capability before starting this mode.

RunDesk 0.49.4 provides this bridge. RunDesk 0.49.3 can use compatible ordinary core protocol methods with Kun 0.17.0, but its legacy snapshot-path diagnostic start must be upgraded to use the new provider contract. The old `snapshotPath` shape is not silently reinterpreted as a provider.

## Persistence and version compatibility

`--data` remains an exclusive Kun execution directory. The journal layout is retained; no RunDesk platform database is moved into this directory. Kun 0.17.0 explicitly accepts 0.16.0 checkpoint and fork manifests through the existing structural and integrity checks. Do not edit manifest versions to bypass validation. Other engine generations require their own compatibility decision.

Host and engine persist different evidence and are not a distributed transaction. On reconnect, hosts read retained events after their last imported sequence and deduplicate projection. A missing response does not prove an operation failed. Preserve the same request identity for a permitted retry, and treat an unknown dispatched side effect as unknown.

## Release discipline

Additive optional fields and capabilities may remain on protocol 16 when existing method semantics remain valid. Incompatible changes to required fields, control semantics, receipts or journal interpretation require an explicit compatibility decision and tests. Keep engine version, SDK version and wire protocol version separate. This release stabilizes the process boundary; it does not promise a dynamic plugin ABI or arbitrary internal-state migrations.
