# Changelog

## 0.66.0 — 2026-11-06

This release cleans the active source tree after the recipe consolidation in 0.65.0. It removes old validation output, superseded documentation and unused helpers, keeps generated QA results outside published source, and adds checks to prevent those files from returning. Canonical recipes, reviewed dependencies, licenses and useful tests remain.

RunDesk metadata and OpenAPI identify 0.66.0. This cleanup retains the existing API contracts, external Kun 0.17.0 and worker protocol 16. Existing user data, frozen build contexts and runtime bindings keep their existing meaning.

For deployment and compatibility, see [upgrading](docs/UPGRADING.md). Current behavior is described in [status](docs/STATUS.md), and actual checks and limitations in [validation](docs/VALIDATION.md). Complete earlier release notes and raw evidence remain in the preserved 0.65.0 source package; see [historical provenance](docs/validation/README.md).
