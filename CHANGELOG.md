# Changelog

## 0.67.1 — 2026-11-08

Correct the temporary-space allowance used by the host's video-build image verification. A successfully built image could fail the offline HyperFrames precheck under the previous 768 MiB `/tmp` limit. The recipe, upstream binaries, doctor thresholds and offline/smoke acceptance conditions remain unchanged. Existing images can be rechecked after a normal host upgrade; readiness is recorded only after the checks pass.

The video-build offline probe and the general Build Center smoke container now use a 2 GiB tmpfs. The Python CLI's video-build doctor probe receives the same allowance. Other managed profile probes retain their previous sizes, and normal conversation containers retain their existing configuration.

The user reported that `rundesk-video-build:0.157.1` built successfully and passed 17/17 checks when only the check container's `/tmp` allowance was changed to 2 GiB. That is target-machine evidence, separate from this release's regression checks. See the [archived investigation](docs/VALIDATION.md#video-build-tmpfs-investigation-2026-11-08), [upgrade procedure](docs/UPGRADING.md) and [current validation scope](docs/VALIDATION.md).

## 0.67.0 — 2026-11-06

This release consolidates related guides and test suites, and shares common recipe inputs through explicit `sharedFiles` declarations. Recipes remain editable files. Preparing, packing or building a recipe materializes a complete context of ordinary files; local recipe files override declared shared targets, and a missing shared source fails explicitly. Frozen contexts and lock files remain self-contained.

The source tree is reduced from 896 to 801 files. Documentation is consolidated into 52 files, related validation scripts share fixtures, and five small Kun store query files become one. The English translation catalog is also reconciled with all 1,800 active runtime translations, and two secondary pages no longer load the translation runtime twice.

The shared Agent-Reach source retains the reviewed wheel-packaging patch and local installation. Generic templates remain self-contained. Existing user data, task behavior, API contracts, runtime bindings and frozen historical builds retain their existing semantics. External Kun remains 0.17.0 with worker protocol 16.

For deployment and compatibility, see [upgrading](docs/UPGRADING.md). Current behavior is described in [status](docs/STATUS.md), and actual checks and limitations in [validation](docs/VALIDATION.md). Earlier raw evidence remains in the preserved 0.65.0 source package; see [historical provenance](docs/validation/README.md).
