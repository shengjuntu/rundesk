# Third-party notices

RunDesk project code is MIT licensed. Dependencies are pinned in go.mod/go.sum and retain their upstream licenses. This source package does not vendor the Go dependency sources or redistribute the Go toolchain, test browsers, Node.js, npm or Codex. Building resolves the pinned dependencies through the Go toolchain; deployment of separately installed tools remains subject to their own licenses.

## Incorporated PiG source

Kun incorporates selected SSE decoders and a regression test from [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), commit `827932db70e545b34c3f1d9c04f58a70eacd3b28`, under MIT.

Copyright Hewlett Packard Enterprise Development LP. Copyright (c) 2025 Mario Zechner (upstream Pi, as attributed by PiG). Original texts are preserved in LICENSES/PiG-MIT.txt and LICENSES/Pi-MIT.txt. Exact file mappings, original hashes and local changes are recorded in [UPSTREAM](docs/UPSTREAM.md).

PiG browser assets, Node extensions and full runtime are not included. Thanks to the PiG and Pi contributors; no upstream endorsement is implied.

## HarnessRouter design reference

HarnessRouter/UHP informed the application-to-backend abstraction and capability documentation. No source, schemas or assets from that project are distributed here. See [UPSTREAM](docs/UPSTREAM.md) for the scope of the reference and acknowledgements.
