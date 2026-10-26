# Third-party notices

RunDesk project code is MIT licensed. Dependencies are pinned in go.mod/go.sum and retain their upstream licenses. This source package includes a checked, engine-free Kun SDK snapshot under third_party/kun-sdk with its own license and source manifest. It does not vendor other Go dependency sources or redistribute the Go toolchain, Kun engine, test browsers, Node.js, npm or Codex. Building resolves the pinned dependencies through the Go toolchain; deployment of separately installed tools remains subject to their own licenses.

## Incorporated PiG source

RunDesk retains the MCP SSE decoder in internal/mcp/sse.go from [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), commit `827932db70e545b34c3f1d9c04f58a70eacd3b28`, under MIT. The provider SSE decoder and selected regression test moved with the independent Kun project and are not part of this host source package.

Copyright Hewlett Packard Enterprise Development LP. Copyright (c) 2025 Mario Zechner (upstream Pi, as attributed by PiG). Original texts are preserved in LICENSES/PiG-MIT.txt and LICENSES/Pi-MIT.txt. Exact file mappings, original hashes and local changes are recorded in [UPSTREAM](docs/UPSTREAM.md).

PiG browser assets, Node extensions and full runtime are not included. Thanks to the PiG and Pi contributors; no upstream endorsement is implied.

## HarnessRouter design reference

HarnessRouter/UHP informed the application-to-backend abstraction and capability documentation. No source, schemas or assets from that project are distributed here. See [UPSTREAM](docs/UPSTREAM.md) for the scope of the reference and acknowledgements.

## Optional runtime profile dependencies

The example Docker recipes can install Agent-Reach from the user-selected MIT fork at a fixed commit, or Wigolo 0.2.1 under AGPL-3.0-only. These are optional external tools, not incorporated RunDesk source. Built images must retain each component's notices and applicable corresponding-source provisions. Exa/Jina are hosted services. See the [profile dependency notices](examples/_shared/THIRD_PARTY.md) and [Wigolo assessment](examples/news2douyin/WIGOLO.md).
