# Editable independent upstream Dockerfile. Inspect upstream.json and install.sh before building.
ARG RD_BASE_IMAGE=node:22-bookworm-slim
FROM ${RD_BASE_IMAGE}
# All managed profiles require the Node runtime and unprivileged node user.
RUN node -e 'if (Number(process.versions.node.split(".")[0]) < 22) process.exit(1)' && npm --version >/dev/null && id node >/dev/null
ARG RD_APT_MIRROR=http://deb.debian.org/debian
ARG RD_APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
COPY tools/configure-apt.sh /opt/rundesk-profile/tools/configure-apt.sh
RUN RD_APT_MIRROR="${RD_APT_MIRROR}" RD_APT_SECURITY_MIRROR="${RD_APT_SECURITY_MIRROR}" sh /opt/rundesk-profile/tools/configure-apt.sh && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 update && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 install -y --no-install-recommends ca-certificates curl ffmpeg fontconfig fonts-noto-cjk git python3 python3-venv ripgrep && rm -rf /var/lib/apt/lists/*
ARG RD_NPM_REGISTRY=https://registry.npmjs.org
ARG RD_PIP_INDEX_URL=https://pypi.org/simple
ENV npm_config_fetch_retries=3 npm_config_fetch_timeout=120000 PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN python3 -m venv /opt/rundesk-python
ENV PATH="/opt/rundesk-python/bin:${PATH}"
COPY upstream/ /opt/rundesk-upstream/
COPY vendor/source.tar.gz /opt/rundesk-upstream/source.tar.gz
RUN echo 'b7c181c855265f5e88a2bacde46959b3a07034ee7dce00aed1f4b00bce6a3367  /opt/rundesk-upstream/source.tar.gz' | sha256sum -c - && mkdir -p /opt/upstreams/video-use && tar -xzf /opt/rundesk-upstream/source.tar.gz --strip-components=1 -C /opt/upstreams/video-use
RUN npm_config_registry="${RD_NPM_REGISTRY}" PIP_INDEX_URL="${RD_PIP_INDEX_URL}" sh /opt/rundesk-upstream/install.sh
RUN test -f /opt/upstreams/video-use/LICENSE
RUN test -f /opt/upstreams/video-use/SKILL.md
RUN test -f /opt/upstreams/video-use/install.md
RUN test -f /opt/upstreams/video-use/pyproject.toml
RUN test -f /opt/upstreams/video-use/helpers/render.py
RUN test -f /opt/upstreams/video-use/helpers/transcribe.py
RUN test -f /opt/upstreams/video-use/helpers/pack_transcripts.py
RUN test -f /opt/upstreams/video-use/helpers/timeline_view.py
RUN test -f /opt/upstreams/video-use/tests/test_render_fps.py
COPY skills/ /etc/codex/skills/
COPY tools/ /opt/rundesk-profile/tools/
RUN test -f /opt/upstreams/video-use/SKILL.md && test ! -e /etc/codex/skills/video-use && ln -s /opt/upstreams/video-use /etc/codex/skills/video-use
ARG CODEX_VERSION=0.0.0
ARG RD_CODEX_MODE=auto
COPY runtime/ /opt/rundesk-runtime/
COPY tools/install-codex.sh /opt/rundesk-profile/tools/install-codex.sh
RUN CODEX_VERSION="${CODEX_VERSION}" RD_CODEX_MODE="${RD_CODEX_MODE}" RD_NPM_REGISTRY="${RD_NPM_REGISTRY}" sh /opt/rundesk-profile/tools/install-codex.sh
LABEL org.opencontainers.image.title="RunDesk video-use"
LABEL org.opencontainers.image.version="1.0.0"
LABEL io.rundesk.profile="video-use"
LABEL io.rundesk.profile.schema="1"
LABEL io.rundesk.codex.version="0.0.0"
LABEL io.rundesk.agent-reach="false"
LABEL io.rundesk.upstream.repository="https://github.com/browser-use/video-use"
LABEL io.rundesk.upstream.commit="43cfc566833548093455c9d4a46bd69913b7e552"
LABEL io.rundesk.upstream.source.sha256="b7c181c855265f5e88a2bacde46959b3a07034ee7dce00aed1f4b00bce6a3367"
LABEL io.rundesk.video-app-media.contract="1"
LABEL io.rundesk.capabilities.skills="edit-with-video-use, video-use"
LABEL io.rundesk.capabilities.tools="Python 3.11, FFmpeg, ffprobe, video-use native helpers, NumPy, librosa, matplotlib, Pillow, Video App generation MCP"
LABEL io.rundesk.capabilities.mcp="video_app_media"
COPY seed/ /opt/rundesk-seed/
COPY profile.lock.json /opt/rundesk-profile/profile.lock.json
COPY THIRD_PARTY.md /opt/rundesk-profile/THIRD_PARTY.md
RUN mkdir -p /opt/rundesk/bin && chmod -R a+rX /opt/upstreams /opt/rundesk-upstream /opt/rundesk-profile /etc/codex/skills /opt/rundesk-seed
USER node
WORKDIR /home/node
CMD ["codex", "app-server"]
