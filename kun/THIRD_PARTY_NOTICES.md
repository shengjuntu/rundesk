# Third-party notices

Thank you to MichaelKinsy/PiG, Michael Kinsy and the PiG contributors, and to Mario Zechner / Pi.

The SSE implementations and selected parity test listed in [the provenance ledger](docs/UPSTREAM.md) were incorporated from PiG at commit `827932db70e545b34c3f1d9c04f58a70eacd3b28`. PiG identifies its relevant source families as translating or adapting Pi. The original notices remain in the source files, and both original MIT license texts are redistributed unchanged:

- [PiG-MIT.txt](LICENSES/PiG-MIT.txt): `Copyright Hewlett Packard Enterprise Development LP`.
- [Pi-MIT.txt](LICENSES/Pi-MIT.txt): `Copyright (c) 2025 Mario Zechner`.

These notices apply to the incorporated portions. They do not attribute all of Kun to PiG, imply upstream endorsement, or claim compatibility with PiG's APIs, session format or extension ecosystem. This separation release incorporates no additional PiG source.

Kun's original implementation is covered by the repository [LICENSE](LICENSE), retaining its existing copyright notice. Go dependency versions are recorded in `go.mod`, `go.sum` and the SDK module; redistributed dependencies remain subject to their own licenses.

## Compiled binary dependencies

The Linux and Windows binaries include Go runtime and selected pinned Go modules. Original notices and copyright texts for the actual dependency closure on both platforms are retained in [go-dependencies](LICENSES/go-dependencies), with versions and source-file hashes in its INDEX.json. The Go runtime license is retained in [Go-BSD.txt](LICENSES/Go-BSD.txt). These notices accompany the compiled binaries; source-only build and development tools are not included.
