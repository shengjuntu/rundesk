# Security and execution boundaries

RunDesk is a self-hosted Agent platform. Its application credentials scope API access; they do not provide complete multi-tenant operating-system isolation. The administrator controls the service account and its execution backends.

- Bind to loopback by default. Remote mode requires a strong RUNDESK_TOKEN, HTTPS termination and an appropriate public URL. Setup, package installation, runtime configuration and global management are administrator-only.
- Application keys bind application, instance, workspace and scopes. Revocation blocks subsequent requests and pending dispatch; already-running actions require explicit cancellation. Never embed administration or provider keys in browser applications.
- Codex execution follows its native sandbox, approval and configured environment. Kun file tools use rooted workspace paths, while opt-in Bash runs with the host account's permissions and may access outside the workspace and network. allowWrite does not constrain Bash or MCP. Kun-managed containers are not implemented.
- Setup never enables Bash or installs software automatically. Optional Codex installation requires explicit consent and a pinned package release, installs into a new data-directory prefix, disables package install scripts and checks the native executable version. It does not switch the active binary until settings are saved. Local administrators remain responsible for package trust and service dependencies.
- MCP and Skills can launch code with their configured permissions. Only trusted administrators should configure them. Instance boundaries are not OS user boundaries; environment variables, projects and credential stores may be shared.
- File API access is limited by path and ownership checks. HTML/SVG outputs are downloaded rather than run in the administrator origin. Trace content is rendered as data, not instructions or executable HTML.
- Structured journal redaction covers recognized secret fields. Prompts, arguments, stderr and arbitrary tool output can still contain private content. Protect databases, backups, snapshots and exported traces; the database is not encrypted.
- Idempotency records prevent duplicate accepted submissions for the same credential and key. They do not guarantee exactly-once remote side effects. Timeout, cancellation and process failure are not transaction rollback; unknown outcomes require evidence review, not a new key or automatic backend fallback.
- A disconnected SSE client does not stop a task. Use cancellation controls. Service restart does not automatically replay uncertain work. Hybrid replays recorded tool results; Live forks execute real tools after explicit authorization.
- Use reliable local filesystems and protect the data-directory lock. One service owns each data directory. Do not delete a live lock or run as a privileged account unnecessarily.

No independent security audit or production penetration test is claimed. Public security contact information has not been configured; before external publication, the repository owner should provide a private reporting channel. Do not include live credentials in public issues.
