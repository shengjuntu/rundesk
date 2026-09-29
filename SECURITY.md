# Security

RunDesk v0.5 assumes one trusted operator. Its token grants control over the same operating-system account that runs Codex. It is not a tenant isolation boundary.

- Listen on loopback unless remote access is intentional. Remote mode requires RUNDESK_TOKEN; terminate TLS using a trusted reverse proxy and configure SSE correctly.
- Default native threads use `workspace-write` and `on-request`, with human approval. This application does not implement a separate sandbox. Codex and the OS enforce execution boundaries.
- Instance permission edits take effect on the next turn. Full access removes sandbox boundaries; never only disables approval prompts. Managed requirements remain authoritative. Command rule objects must exactly match offered decisions. RunDesk never broadens a suggested rule itself.
- Diagnostics run a fixed harmless command in a workspace-write sandbox with networking disabled, never falling back to unsandboxed execution. They do not change OS security settings.
- MCP configuration can launch local programs. Only trusted administrators should configure servers or edit skills. Editing user MCP configuration affects other Codex clients using the same CODEX_HOME.
- File API access is limited to uploads/outputs inside the registered workspace and uses Go's rooted filesystem API to prevent traversal and symlink escapes. The agent itself has the permissions granted to Codex.
- Do not register directories controlled by untrusted local users. Do not run the service as root in production. Protect CODEX_HOME, the data directory and workspace directories with OS permissions.
- Browser code uses DOM text nodes, not raw Markdown HTML. HTML and SVG artifacts are downloaded rather than executed in the admin origin. File links and protocol events can still reveal private information.
- Journal redaction covers recognized structured fields such as env, headers, tokens and passwords. Prose, prompts, command arguments, arbitrary tool output and stderr can contain secrets; exported journals require review before sharing.
- Stopping an HTTP stream does not stop a run. Use the stop endpoint. Restart does not automatically retry a run. Submission requests have no idempotency key in v0.5: after a client timeout, check the session and journal before resubmitting.
- Instances separate Codex configuration homes, not OS users or permissions. Global/project Skills, inherited environment secrets, project files, and OS credential stores may remain shared. Managed instances remove inherited CODEX_SQLITE_HOME overrides; the default instance preserves the service environment.
- The database is not encrypted. Protect backups, attached files and outputs as you would project source code.
- No production penetration test or independent audit has been performed.

The project has no configured public security contact yet. Before publishing the repository, enable GitHub private vulnerability reporting and replace this paragraph with the chosen contact policy. Do not submit live credentials in public issues.

Trace previews inherit journal redaction and authentication. Source-event lookup is scoped to the requested session. Exported traces may contain command arguments, output, paths and user text; do not assume all prose secrets are removed. The trace UI renders event text as text, keeps the strict CSP, and does not execute trace content.
