# Editable independent upstream Dockerfile. Inspect upstream.json and install.sh before building.
ARG RD_BASE_IMAGE=node:22-bookworm-slim
FROM ${RD_BASE_IMAGE}
ARG RD_APT_MIRROR=http://deb.debian.org/debian
ARG RD_APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
COPY tools/configure-apt.sh /opt/rundesk-profile/tools/configure-apt.sh
RUN RD_APT_MIRROR="${RD_APT_MIRROR}" RD_APT_SECURITY_MIRROR="${RD_APT_SECURITY_MIRROR}" sh /opt/rundesk-profile/tools/configure-apt.sh && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 update && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 install -y --no-install-recommends ca-certificates chromium curl ffmpeg fontconfig fonts-noto-cjk git python3 python3-venv ripgrep && rm -rf /var/lib/apt/lists/*
ARG RD_NPM_REGISTRY=https://registry.npmjs.org
ARG RD_PIP_INDEX_URL=https://pypi.org/simple
ENV npm_config_fetch_retries=3 npm_config_fetch_timeout=120000 PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN python3 -m venv /opt/rundesk-python
ENV PATH="/opt/rundesk-python/bin:${PATH}"
COPY upstream/ /opt/rundesk-upstream/
# Reviewed upstream source: https://codeload.github.com/calesthio/OpenMontage/tar.gz/c8066452325cb57c8fc1ba8c2a71b6e8842e6b65
ARG RD_GITHUB_ARCHIVE_BASE=https://codeload.github.com
RUN curl --fail --location --retry 3 --connect-timeout 30 --max-time 900 --output /opt/rundesk-upstream/source.tar.gz "${RD_GITHUB_ARCHIVE_BASE}/calesthio/OpenMontage/tar.gz/c8066452325cb57c8fc1ba8c2a71b6e8842e6b65"
RUN echo 'dff7cd1cdf2cb0522398e00b3f0b75f32336b695a8be34ac8e6b6913055aab25  /opt/rundesk-upstream/source.tar.gz' | sha256sum -c - && mkdir -p /opt/upstreams/openmontage && tar -xzf /opt/rundesk-upstream/source.tar.gz --strip-components=1 -C /opt/upstreams/openmontage
RUN npm_config_registry="${RD_NPM_REGISTRY}" PIP_INDEX_URL="${RD_PIP_INDEX_URL}" sh /opt/rundesk-upstream/install.sh
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
COPY skills/ /etc/codex/skills/
COPY tools/ /opt/rundesk-profile/tools/
ARG CODEX_VERSION=0.0.0
RUN npm_config_registry="${RD_NPM_REGISTRY}" npm install --global "@openai/codex@${CODEX_VERSION}" && npm cache clean --force && codex --version && codex app-server --help >/dev/null
LABEL org.opencontainers.image.title="RunDesk openmontage"
LABEL org.opencontainers.image.version="1.0.0"
LABEL io.rundesk.profile="openmontage"
LABEL io.rundesk.profile.schema="1"
LABEL io.rundesk.codex.version="0.0.0"
LABEL io.rundesk.agent-reach="false"
LABEL io.rundesk.upstream.repository="https://github.com/calesthio/OpenMontage"
LABEL io.rundesk.upstream.commit="c8066452325cb57c8fc1ba8c2a71b6e8842e6b65"
LABEL io.rundesk.upstream.source.sha256="dff7cd1cdf2cb0522398e00b3f0b75f32336b695a8be34ac8e6b6913055aab25"
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
