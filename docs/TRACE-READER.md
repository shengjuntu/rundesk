# Conversation trace reader

The trace entry point now opens a reading view over the unified observation
model. The existing step inspector, HTTP/WebSocket inspector, analysis panel and
capability-gated Kun controls remain in the same dialog and share its history
cursor. No agent source, task execution or observation storage changes are
required for this view.

## Reading and inspection

- The round outline selects a run from its original user request. On narrow
  screens it starts collapsed. The existing project/session navigation is reused.
- User input and assistant messages are readable in chronological order. Tool
  start/end records form one action using the existing lifecycle projector.
  Tool arguments/results and recorded reasoning start folded. Failures, pending
  approvals and incomplete lifecycle records are expanded and marked.
- Text blocks over 4,000 characters show an 800-character excerpt with an
  explicit expansion control. Expansion uses already-loaded text, does not query
  the model, and is distinct from a source-content truncation warning.
- Markdown headings, lists, tables and code are rendered with DOM nodes; HTML is
  literal. Only HTTP(S) text links are active. Images and files are source paths,
  not auto-fetched copies. Native diffs show added/removed lines.
- Evidence opens on demand. The drawer shows source identity, clocks,
  associations, completeness, and references to original journal records.
  Each event is read in 4,000-character pages. Exact JSON integers are retained.
  HTTP references open the existing byte-preserving transport inspector.
- Network records can be shown in the reading view but remain independent of
  runtime actions. Sharing a run or timestamp does not prove a model-call match.
- Inspection is read-only. To approve, resume, step or otherwise control a task,
  use the existing supported controls; browsing cannot issue those operations.

## Presentation query

`GET /api/v1/sessions/{id}/debug/query?kind=transcript&through=99&runId=...&offset=0&limit=40`

The response uses the same DebugService authorization and fixed snapshot as
`activity`. Its entries carry the canonical `observation.Activity` plus a role,
display title and bounded blocks. Storage remains the event journal. This is a
reading projection, not another source of truth or a full model transcript.

The API defaults to 20 entries, caps requests at 50, caps content at 12,000 Unicode
characters and 32 blocks per entry, and caps source reads at 16 MiB per page. The
browser replaces a page of 40 entries rather than accumulating the entire
conversation or fetching every raw payload. Missing, bounded and partial content
are disclosed separately through `contentUnavailable`, `contentTruncated` and
`contentPartial`. Original saved evidence remains accessible through its existing
read APIs and their limits.

Open Codex message/reasoning deltas are scoped by exact native identifiers and
the same history cutoff. Completed items use their retained native content.
Host input/native input echoes are deduplicated only where retained content is
known to match. Kun plan/act calls retain their distinct purpose and native IDs.

## Deliberate first-version boundaries

- History advances with **Read latest records**. There is no implicit live follow
  that could change evidence under an open inspection.
- Search covers loaded content on the current page. It is not global full-text
  search. Pagination remains available when the display filters hide a page.
- Content is structurally redacted under the existing policy; arbitrary natural
  language is not a universal credential detector.
- Child-agent trees are not inferred from message order or timestamps. No new
  hierarchy is asserted without explicit native relationships.
- Images/files remain references. This version does not render external image
  URLs, execute Mermaid/HTML, or fetch linked artifacts automatically.
- Transcript display does not add token counters from different observation
  channels or calculate billing.

## Validation

- `scripts/trace-reader-ui-test.cjs`: DOM-only safety, folding, pages, fixed
  cursors, lazy evidence, stale responses and teardown with JSDOM.
- `scripts/unified-debug-ui-test.cjs`: actual Chromium with explicit service
  fixtures; trace and narrow layout plus legacy step, HTTP/WebSocket byte paging,
  historical Kun control protection, one dialog and read-only inspection.
- `internal/tracequery/transcript_test.go`: retained Codex/Kun fixtures, native
  pairing, snapshot boundaries, content limits/redaction and exact identifiers.
- API/service tests cover selector validation, capability exposure and ownership.

The browser fixture is not a live provider run. Screenshots used for development
are not shipped as session evidence.
