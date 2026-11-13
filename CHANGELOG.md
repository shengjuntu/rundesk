# Changelog

## 0.75.1 — 2026-11-12

Fix shared tool-base builds passing bare Docker content IDs to `RD_BASE_IMAGE`. Bind each verified frozen image ID to a content-named local tag, verify the tag resolves to that ID, and use the tag for `FROM` in root, intermediate, reused and final recipe builds. Explicit Codex `base` mode uses the same conversion. Frozen image IDs, cache keys, recipe inputs and historical build records retain their meaning.

The standalone Python recipe CLI follows the same local-reference contract. A missing fixed image ID fails locally rather than being sent to a registry as an image name. Existing offline and smoke checks remain required. See [build runtime](docs/BUILD-RUNTIME.md), [upgrading](docs/UPGRADING.md) and [validation](docs/VALIDATION.md).

## 0.75.0 — 2026-11-12

Acquire local ZIPs, browser-selected directories and installed Skills into fixed Skill Center snapshots before explicit installation or effect tests. Preserve complete relative paths and source provenance; existing installed content is read without being changed. Add explicit bounded environment self-diagnostics that distinguish missing commands, unsupported versions, failed protocols and unknown model readiness. Opening saved diagnostics does not start a probe.

See [Skill Center](docs/SKILL-CENTER.md) for local inputs, execution-bit limits, diagnostic setup and scope, and [validation](docs/VALIDATION.md) for actual checks.

## 0.74.0 — 2026-11-12

Add Skill-Up effect checks to the Skill Center: explicit container-only validation and execution, persistent task recovery/cancellation, bounded report ingestion, per-case outcomes and cross-version comparisons. Freeze source, evaluation-suite and image identities. Missing cases, changed inputs, unknown configurations and externally imported reports cannot silently pass regression gates. Existing security scanning remains a separate result.

Tests are never launched on the host as a fallback. Container execution requires a prepared Linux Docker environment and explicit confirmation; networking is off by default. See [effect checks](docs/SKILL-CHECKS.md) and [validation](docs/VALIDATION.md) for setup and the distinction between CLI protocol tests and target-model acceptance.

## 0.73.1 — 2026-11-12

Validate the external SkillSpector path against the real NVIDIA 2.13.4 CLI. Reject zero-file and inconsistent coverage claims, match the scanner inventory to the frozen directory, preserve file-level exclusion reasons, and detect content changes during inspection. Keep Go and NVIDIA reports separately visible after navigation and restore active scans after a browser refresh. Static fixture checks are separate from LLM, target Windows and business-workload acceptance.

See [Skill Center setup](docs/SKILL-CENTER.md) and [validation](docs/VALIDATION.md).

## 0.73.0 — 2026-11-12

Add an administrator Skill Center with native Go search across ClawHub, SkillsMP, skills.sh and GitHub, source snapshots, full-directory previews, bounded static checks and optional external NVIDIA SkillSpector jobs. Reports bind file paths, bytes and executable flags; installation verifies the snapshot and reuses workspace/environment targeting and complete-directory backups. Risky or incomplete inspections require explicit review and a recorded reason. Existing ZIP/directory imports and application Skill seeding/copying use the same built-in review gate.

Search sources fail independently. Missing or failed external scanners never count as completed checks. External SkillSpector protocol fixtures and a simulated CLI are verified separately from a real NVIDIA installation. Skill-Up and runtime enforcement remain future work. See the [Skill Center guide](docs/SKILL-CENTER.md) and [validation](docs/VALIDATION.md).

## 0.70.0 — 2026-11-08

Restore a usable everyday conversation context after setup. Kun maintenance preferences no longer select the daily Agent, implicit stale maintenance workspace choices recover to an ordinary workspace, and the ordinary startup route opens conversations with a valid Codex context. Valid workspace-specific Codex choices, explicit application bindings and existing session/deep-link identities remain.

Improve Codex effective-configuration reads with bounded connection admission, concurrent reads on an initialized connection, exclusive writes/lifecycle changes and coalescing only for identical reads still in flight. No completed configuration cache or local-TOML substitute is introduced. The read control disables duplicate clicks and shows local progress, failures and retry. First-time Docker environment preparation retains its separate command limits. OpenMontage 1.1.1, existing recipes, external Kun 0.17.0 and worker protocol 16 are unchanged. See [upgrading](docs/UPGRADING.md) and [validation](docs/VALIDATION.md).

## 0.69.1 — 2026-11-08

Further reduce the bundled OpenMontage source by excluding reviewed documentation-preview images and an unreferenced example logo. Record the exact omission list and retain the source bytes, paths and executable permission semantics of the remaining code, Skills, templates, fonts, audio and tests. No native template dependency is removed merely because it has a duplicate copy.

The shipped archive changes from gzip to XZ using a 32 MiB dictionary, reducing 17,846,107 bytes to 10,296,296 bytes while leaving 2,163 retained files unchanged. Extraction uses the existing Python standard library and needs no extra xz system package. The OpenMontage recipe becomes 1.1.1. Existing imported recipes, frozen contexts and images keep their original inputs; start a new build from the updated recipe to use the smaller bundle. Source preparation and package checks remain separate from actual Docker and business-task acceptance. See [build sources](docs/BUILD-SOURCES.md), [upgrading](docs/UPGRADING.md) and [validation](docs/VALIDATION.md).

## 0.69.0 — 2026-11-08

Package the pinned OpenMontage runtime source with its recipe instead of downloading the complete upstream repository archive during every cold build. Retain runtime code, required assets, upstream guides and licenses; omit only reviewed demonstration media with recorded provenance and archive identities. OpenMontage remains an independent recipe, and Python/npm dependencies still use their configured sources.

Archive downloads retained by existing frozen recipes can preserve validated partial transfers, resume when the server supports the required protocol, and report progress. Unverified or mismatched bytes never become usable cached archives. Shared base reuse, historical build identities and offline/smoke acceptance remain unchanged; no new build entry or public registry is introduced. See [build sources](docs/BUILD-SOURCES.md), [upgrading](docs/UPGRADING.md) and [validation](docs/VALIDATION.md).

## 0.68.0 — 2026-11-08

Build the six managed recipes on reusable tool bases: system packages and a Python virtual environment, then media tools and fonts, then Chromium where needed. Codex and domain-specific Python/npm dependencies stay in the recipe. Base Dockerfiles and their manifest are ordinary editable files, materialized with exported contexts; a direct standalone Docker build retains its installation path.

The existing build job prepares the required base stages before the recipe and records their progress in the same log. Reuse binds the parent image identity, platform, stage files and effective source arguments. Historical rebuilds retain frozen base identities and fail explicitly when a required frozen image is unavailable. Source capability detection includes declared base Dockerfiles, so APT settings remain available after extraction. No separate build entry or public registry is introduced.

Existing frozen/imported recipes keep their original inputs. The 0.67.1 temporary-space fix, offline/smoke checks, unprivileged recipe runtime and separate Codex runtime selection remain. External Kun stays at 0.17.0 with worker protocol 16. See [runtime reuse](docs/BUILD-RUNTIME.md), [upgrading](docs/UPGRADING.md) and [validation](docs/VALIDATION.md) for current checks and target-machine limits.

## 0.67.1 — 2026-11-08

Correct the temporary-space allowance used by the host's video-build image verification. A successfully built image could fail the offline HyperFrames precheck under the previous 768 MiB `/tmp` limit. The recipe, upstream binaries, doctor thresholds and offline/smoke acceptance conditions remain unchanged. Existing images can be rechecked after a normal host upgrade; readiness is recorded only after the checks pass.

The video-build offline probe and the general Build Center smoke container now use a 2 GiB tmpfs. The Python CLI's video-build doctor probe receives the same allowance. Other managed profile probes retain their previous sizes, and normal conversation containers retain their existing configuration.

The user reported that `rundesk-video-build:0.157.1` built successfully and passed 17/17 checks when only the check container's `/tmp` allowance was changed to 2 GiB. That is target-machine evidence, separate from this release's regression checks. See the [archived investigation](docs/VALIDATION.md#video-build-tmpfs-investigation-2026-11-08), [upgrade procedure](docs/UPGRADING.md) and [current validation scope](docs/VALIDATION.md).

## 0.67.0 — 2026-11-06

This release consolidates related guides and test suites, and shares common recipe inputs through explicit `sharedFiles` declarations. Recipes remain editable files. Preparing, packing or building a recipe materializes a complete context of ordinary files; local recipe files override declared shared targets, and a missing shared source fails explicitly. Frozen contexts and lock files remain self-contained.

The source tree is reduced from 896 to 801 files. Documentation is consolidated into 52 files, related validation scripts share fixtures, and five small Kun store query files become one. The English translation catalog is also reconciled with all 1,800 active runtime translations, and two secondary pages no longer load the translation runtime twice.

The shared Agent-Reach source retains the reviewed wheel-packaging patch and local installation. Generic templates remain self-contained. Existing user data, task behavior, API contracts, runtime bindings and frozen historical builds retain their existing semantics. External Kun remains 0.17.0 with worker protocol 16.

For deployment and compatibility, see [upgrading](docs/UPGRADING.md). Current behavior is described in [status](docs/STATUS.md), and actual checks and limitations in [validation](docs/VALIDATION.md). Earlier raw evidence remains in the preserved 0.65.0 source package; see [historical provenance](docs/validation/README.md).
