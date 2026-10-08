# Third-party notices

Project code is MIT licensed. Go dependencies retain their own licenses. Corresponding license and notice files are reproduced under `third_party/`. The bundled browser UI has no external JavaScript/CSS runtime dependency.

| Module | Version |
| --- | --- |
| github.com/dustin/go-humanize | v1.0.1 |
| github.com/google/pprof | v0.0.0-20260802141513-ef3492d7dac3 |
| github.com/google/uuid | v1.6.0 |
| github.com/hashicorp/golang-lru/v2 | v2.0.7 |
| github.com/mattn/go-isatty | v0.0.24 |
| github.com/ncruces/go-strftime | v1.0.0 |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec |
| golang.org/x/mod | v0.38.0 |
| golang.org/x/sync | v0.22.0 |
| golang.org/x/sys | v0.47.0 |
| golang.org/x/tools | v0.48.0 |
| modernc.org/cc/v4 | v4.29.2 |
| modernc.org/ccgo/v4 | v4.35.0 |
| modernc.org/fileutil | v1.4.0 |
| modernc.org/gc/v2 | v2.6.5 |
| modernc.org/gc/v3 | v3.1.5 |
| modernc.org/goabi0 | v0.2.0 |
| modernc.org/libc | v1.75.7 |
| modernc.org/mathutil | v1.7.1 |
| modernc.org/memory | v1.12.1 |
| modernc.org/opt | v0.2.0 |
| modernc.org/sortutil | v1.2.1 |
| modernc.org/sqlite | v1.59.0 |
| modernc.org/strutil | v1.2.1 |
| modernc.org/token | v1.1.0 |

Codex is an independently installed external executable and is not included in this distribution. The Go toolchain, test browser and Python/Node test tools are not redistributed. Go runtime license is included in `third_party/go-runtime/`.

## Kun selected PiG source

Kun incorporates the model SSE decoder, MCP SSE decoder, and a selected regression test from
[MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), commit
`827932db70e545b34c3f1d9c04f58a70eacd3b28`, under MIT.

Copyright Hewlett Packard Enterprise Development LP.
Copyright (c) 2025 Mario Zechner (upstream Pi, as attributed by PiG).

License texts: `LICENSES/PiG-MIT.txt` and `LICENSES/Pi-MIT.txt`.
Exact file mapping, original hashes, and adaptations: `docs/UPSTREAM.md`.
Thanks to PiG and Pi contributors. Neither project endorses Kun or RunDesk.
PiG's embedded third-party browser assets and Node extensions are not included.
