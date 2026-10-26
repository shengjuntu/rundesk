# Third-party notices

RunDesk project code is MIT licensed. Dependencies are pinned in go.mod/go.sum and retain their upstream licenses. This source package includes a checked, engine-free Kun SDK snapshot under third_party/kun-sdk with its own license and source manifest. It does not vendor other Go dependency sources or redistribute the Go toolchain, Kun engine, test browsers, Node.js, npm or Codex. Building resolves the pinned dependencies through the Go toolchain; deployment of separately installed tools remains subject to their own licenses.

## Incorporated PiG source

RunDesk retains the MCP SSE decoder in internal/mcp/sse.go from [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG), commit `827932db70e545b34c3f1d9c04f58a70eacd3b28`, under MIT. The provider SSE decoder and selected regression test moved with the independent Kun project and are not part of this host source package.

Copyright Hewlett Packard Enterprise Development LP. Copyright (c) 2025 Mario Zechner (upstream Pi, as attributed by PiG). Original texts are preserved in LICENSES/PiG-MIT.txt and LICENSES/Pi-MIT.txt. Exact file mappings, original hashes and local changes are recorded in [UPSTREAM](docs/UPSTREAM.md).

PiG browser assets, Node extensions and full runtime are not included. Thanks to the PiG and Pi contributors; no upstream endorsement is implied.

## HarnessRouter design reference

HarnessRouter/UHP informed the application-to-backend abstraction and capability documentation. No source, schemas or assets from that project are distributed here. See [UPSTREAM](docs/UPSTREAM.md) for the scope of the reference and acknowledgements.

## Runtime profile dependencies

The news and people-search recipes install Agent-Reach from a pinned MIT fork. The original video-build recipe prepares HyperFrames/GSAP and can optionally include the pinned video-use source. These tools are separate from RunDesk's host code; built images must retain their component notices. Exa/Jina are external hosted services. The [shared dependency notices](examples/_shared/THIRD_PARTY.md) describe these original profiles.

The independent [HyperFrames](examples/hyperframes/README.md), [video-use](examples/video-use/README.md), and [OpenMontage](examples/openmontage/README.md) profiles have their own pinned sources and licensing records. The OpenMontage recipe includes its [AGPL license text](examples/openmontage/LICENSE-AGPL-3.0.txt) and fetches the pinned implementation at image build time; it is separate from the original video-build profile. See [upstream environments](docs/UPSTREAM-VIDEO-ENVIRONMENTS.md) for provenance and redistribution boundaries.

Wigolo was evaluated in earlier versions and is no longer included in the current recipes. Its removed integration guide is not a current setup dependency.
