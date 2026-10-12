# Current release

## RunDesk 0.45.0

- Backend-aware startup wizard with local prerequisite checks, independent Kun initialization, explicit optional Codex installation into a managed directory, and atomic revision-guarded settings.
- Host backend catalog and a common run/stop dispatch boundary. Backend-specific configuration, approval, recovery and execution environments remain explicit; no dynamic plugin or UHP compatibility claim.
- Rewritten English/Chinese README emphasizing execution-trace debugging; current application API, architecture, backend matrix, setup, status and upgrade documentation. Removed historical release/validation copies from this source distribution.
- Retained Kun 0.16.0, worker protocol 16, fork schema 3 and all existing K4 guards. No new runtime dependencies.

Current validation and limitations: [VALIDATION](docs/VALIDATION.md). Upgrade instructions: [UPGRADING](docs/UPGRADING.md). Earlier history remains in previously delivered source packages.
