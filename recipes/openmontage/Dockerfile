# Editable independent upstream Dockerfile. Inspect upstream.json and install.sh before building.
ARG RD_BASE_IMAGE=node:22-bookworm-slim
FROM ${RD_BASE_IMAGE}
USER root
# All managed profiles require the Node runtime and unprivileged node user.
RUN node -e 'if (Number(process.versions.node.split(".")[0]) < 22) process.exit(1)' && npm --version >/dev/null && id node >/dev/null
ARG RD_APT_MIRROR=http://deb.debian.org/debian
ARG RD_APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
COPY tools/configure-apt.sh /opt/rundesk-profile/tools/configure-apt.sh
ARG RD_TOOL_BASE_READY=0
COPY bases/install.sh /opt/rundesk-bases/install.sh
COPY bases/configure-apt.sh /opt/rundesk-bases/configure-apt.sh
RUN if [ "${RD_TOOL_BASE_READY}" != "1" ]; then RD_APT_MIRROR="${RD_APT_MIRROR}" RD_APT_SECURITY_MIRROR="${RD_APT_SECURITY_MIRROR}" sh /opt/rundesk-bases/install.sh all-browser; fi
ARG RD_NPM_REGISTRY=https://registry.npmjs.org
ARG RD_PIP_INDEX_URL=https://pypi.org/simple
ENV npm_config_fetch_retries=3 npm_config_fetch_timeout=120000 PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
ENV PATH="/opt/rundesk-python/bin:${PATH}"
# Reviewed upstream commit; source bytes and omitted demo media are recorded in source-bundle.json.
COPY vendor/source.tar.xz /opt/rundesk-upstream/source.tar.xz
COPY upstream/extract-source.py /opt/rundesk-upstream/extract-source.py
RUN python3 /opt/rundesk-upstream/extract-source.py --archive /opt/rundesk-upstream/source.tar.xz --sha256 a46a636050a56fd86975d4429a0ea51c7ea4c164349e7e848f3ff6aa974c233d --root OpenMontage-c8066452325cb57c8fc1ba8c2a71b6e8842e6b65 --destination /opt/upstreams/openmontage
# Keep dependency installation ahead of the integration overlays and metadata.
COPY upstream/install-dependencies.sh /opt/rundesk-upstream/install-dependencies.sh
COPY upstream/npm/ /opt/rundesk-upstream/npm/
RUN npm_config_registry="${RD_NPM_REGISTRY}" PIP_INDEX_URL="${RD_PIP_INDEX_URL}" sh /opt/rundesk-upstream/install-dependencies.sh
COPY upstream/ /opt/rundesk-upstream/
RUN RD_OPENMONTAGE_DEPS_READY=1 sh /opt/rundesk-upstream/install.sh
RUN test -f /opt/upstreams/openmontage/LICENSE
RUN test -f /opt/upstreams/openmontage/AGENTS.md
RUN test -f /opt/upstreams/openmontage/AGENT_GUIDE.md
RUN test -f /opt/upstreams/openmontage/tools/base_tool.py
RUN test -f /opt/upstreams/openmontage/tools/tool_registry.py
RUN test -f /opt/upstreams/openmontage/tools/audio/tts_selector.py
RUN test -f /opt/upstreams/openmontage/tools/graphics/image_selector.py
RUN test -f /opt/upstreams/openmontage/tools/video/video_selector.py
RUN test -f /opt/upstreams/openmontage/remotion-composer/package-lock.json
RUN test -f /opt/upstreams/openmontage/pipeline_defs/animated-explainer.yaml
ARG CODEX_VERSION=0.0.0
ARG RD_CODEX_MODE=auto
COPY runtime/ /opt/rundesk-runtime/
COPY tools/install-codex.sh /opt/rundesk-profile/tools/install-codex.sh
RUN CODEX_VERSION="${CODEX_VERSION}" RD_CODEX_MODE="${RD_CODEX_MODE}" RD_NPM_REGISTRY="${RD_NPM_REGISTRY}" sh /opt/rundesk-profile/tools/install-codex.sh
COPY skills/ /etc/codex/skills/
COPY tools/ /opt/rundesk-profile/tools/
LABEL org.opencontainers.image.title="RunDesk openmontage"
LABEL org.opencontainers.image.version="1.1.1"
LABEL io.rundesk.profile="openmontage"
LABEL io.rundesk.profile.schema="1"
LABEL io.rundesk.codex.version="0.0.0"
LABEL io.rundesk.agent-reach="false"
LABEL io.rundesk.upstream.repository="https://github.com/calesthio/OpenMontage"
LABEL io.rundesk.upstream.commit="c8066452325cb57c8fc1ba8c2a71b6e8842e6b65"
LABEL io.rundesk.upstream.source.sha256="a46a636050a56fd86975d4429a0ea51c7ea4c164349e7e848f3ff6aa974c233d"
LABEL io.rundesk.video-app-media.contract="1"
LABEL io.rundesk.capabilities.skills="use-openmontage"
LABEL io.rundesk.capabilities.tools="Python 3.11+, OpenMontage native pipelines, OpenMontage provider registry, Remotion, HyperFrames 0.8.87, GSAP 3.15.0, FFmpeg, Chromium, Video App media MCP bridge"
LABEL io.rundesk.capabilities.mcp="video_app_media"
COPY seed/ /opt/rundesk-seed/
COPY profile.lock.json /opt/rundesk-profile/profile.lock.json
COPY THIRD_PARTY.md /opt/rundesk-profile/THIRD_PARTY.md
RUN mkdir -p /opt/rundesk/bin && chmod -R a+rX /opt/upstreams /opt/rundesk-upstream /opt/rundesk-profile /etc/codex/skills /opt/rundesk-seed
USER node
WORKDIR /home/node
CMD ["codex", "app-server"]
