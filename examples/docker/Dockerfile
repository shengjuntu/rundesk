# RunDesk base runtime. Build context: examples/docker (see README.md).
ARG BASE_IMAGE=node:22-bookworm-slim
FROM ${BASE_IMAGE}
ARG CODEX_VERSION
ARG IMAGE_VERSION
ARG SOURCE_URL
ARG SOURCE_REVISION
ARG BUILD_DATE
ARG BUILD_URL
LABEL org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      io.rundesk.build.dockerfile="examples/docker/Dockerfile" \
      io.rundesk.build.url="${BUILD_URL}" \
      io.rundesk.codex.version="${CODEX_VERSION}" \
      io.rundesk.capabilities.tools="Node.js, Python 3, Git, curl, ripgrep"
# Add io.rundesk.capabilities.skills / .mcp when baking those capabilities.

# Require an explicit release instead of silently following latest.
RUN node -e 'if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(process.argv[1] || "")) { console.error("Set CODEX_VERSION to an exact release (see README.md)"); process.exit(1); }' "${CODEX_VERSION}" \
 && apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git curl ripgrep python3 python3-venv \
 && rm -rf /var/lib/apt/lists/* \
 && npm install --global "@openai/codex@${CODEX_VERSION}" \
 && npm cache clean --force \
 && codex --version \
 && codex app-server --help >/dev/null
# Add approved tools / MCP executables here, with explicit versions.
# Do not declare VOLUME or bake credentials into the image.
RUN mkdir -p /opt/rundesk/bin /opt/rundesk-seed/skills
COPY seed/ /opt/rundesk-seed/
# RunDesk supplies its own read-only helper and overrides ENTRYPOINT/CMD.

# Standalone use defaults to an unprivileged user. RunDesk supplies the host UID/GID
# and mounted HOME/CODEX_HOME itself; do not hard-code either path here.
USER node
WORKDIR /home/node
CMD ["codex", "app-server"]
