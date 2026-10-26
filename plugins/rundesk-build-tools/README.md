# RunDesk build tools

This is a native-format plugin source package with a portable `plugin.json`, a Codex compatibility manifest and the `add-app-docker-image` Skill. It does not install itself, publish a marketplace entry or call experimental `plugin/*` App Server methods.

RunDesk's **Let Codex add** action creates a dedicated task workspace, copies only this Skill to `.agents/skills`, and supplies the `rundesk_build_center` MCP connection in the native thread configuration. The UI reports `skillBootstrap: true` and `nativePluginInstalled: false`. Native plugin installation and per-task Skill bootstrap are different operations.

The MCP endpoint is `POST /api/build-center/mcp`. Its bearer capability expires after two hours and becomes unusable when its exact task completes, is canceled, or is revoked. It can import recipe artifacts from its own workspace, build those recipes or built-ins, inspect and cancel its own builds, and run bounded checks on its own resulting images. It cannot delete images, start or change instances, install plugins, read arbitrary configuration or access another task's artifacts. RunDesk's administrator token and Docker socket are never supplied to the task.

No static `mcp.json` is distributed: each connection needs the running server's reachable address and an ephemeral task capability. Do not put live credentials into a plugin manifest, image layer, build context, prompt, or persistent Codex config. External native-plugin hosts must obtain the task connection through an explicit RunDesk integration; installing this source package alone does not grant build access.

For a host Codex task, RunDesk uses its HTTP origin. For Docker Codex tasks, configure `RUNDESK_BUILD_MCP_ORIGIN` (for example `http://host.docker.internal:3210` when RunDesk listens on an interface reachable from Docker), or use the configured public HTTPS origin. Existing containers already map `host.docker.internal` to Docker's host gateway. A loopback-only server is not reachable through that mapping; RunDesk rejects a Docker assistant run without an explicit non-loopback callback. Network-disabled Docker instances cannot use this HTTP MCP.

The assistant is an administrator-authorized coding task, subject to the configured approval policy and an additional workspace-write sandbox. The HTTP capability narrows RunDesk API access; it does not make upstream code or Docker builds trustworthy. Build contexts and smoke commands remain reviewable artifacts and use RunDesk's existing bounded queue.

Format reference: [OpenAI plugin packaging](https://developers.openai.com/plugins/build/plugins). The native plugin management surface remains version dependent; this package does not report an installation that has not happened.
