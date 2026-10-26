# Kun SDK v0.17.0

Kun SDK is the small host integration module for the independently released Kun engine.
It is a normal Go library with only standard library dependencies; it contains no agent
execution engine, model provider, SQLite implementation, or RunDesk application code.

- `protocol`: stable JSONL protocol 16 types, the actual `Hello` identity and capability set,
  plus serializable run, control, event, checkpoint and fork contracts.
- `client`: launches an external `kun` binary, correlates concurrent calls, verifies the
  handshake and enforces an 8 MiB JSONL payload limit. `Info()` returns the actual engine
  version; `RequireCapabilities(...)` checks optional extensions before they are used.
- `validation`: the shared, side-effect-free bounded JSON Schema subset, fast-program
  validation and runtime/catalog fingerprints. Hosts and the engine use one implementation.

```go
import (
    "context"
    kun "github.com/your-org/kun/sdk/client"
)

client, err := kun.Launch(context.Background(), "/path/to/kun", "/path/to/data")
if err != nil { /* report a startup or protocol error */ }
defer client.Close()
info := client.Info() // actual engineVersion, protocolVersion and capabilities
_ = info
```

The core protocol remains compatible with Kun v0.16.0. The optional
`diagnosticProvider` capability is required before sending a diagnostic provider;
legacy engines lacking that capability can still perform ordinary core calls.
Canceling a client call stops waiting, but does not undo an action already accepted by
Kun. Cancel an engine run using its `control` operation. An unknown wire protocol
version is rejected instead of being guessed compatible.

`CompatibleEngineVersion` allows only the explicitly verified v0.16.0 and v0.17.0
unchanged durable checkpoint formats. Fast runtime guards continue to match exact
engine versions and fall back when a version or environment changes.

Use a fixed SDK release (this module: `github.com/your-org/kun/sdk`, v0.17.0).
The `your-org` prefix is a local distribution namespace until the public repository
location is chosen; offline source packages may use a local Go `replace` directive.
