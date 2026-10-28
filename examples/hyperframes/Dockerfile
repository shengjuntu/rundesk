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
ENV HYPERFRAMES_BROWSER_PATH="/usr/bin/chromium"
ENV HYPERFRAMES_NO_TELEMETRY="1"
ENV HYPERFRAMES_NO_UPDATE_CHECK="1"
ENV HYPERFRAMES_SKIP_SKILLS="1"
ENV HYPERFRAMES_SKILL_PKG_VERSION="0.8.87"
ENV HYPERFRAMES_SKILL_NODE_MODULES="/opt/hyperframes/node_modules"
ENV DO_NOT_TRACK="1"
COPY vendor/source.tar.gz /opt/rundesk-upstream/source.tar.gz
RUN echo '492a80be9507b50abae44b6f10df4f5107feeab71d1dccfacd81c7f371c87fc2  /opt/rundesk-upstream/source.tar.gz' | sha256sum -c - && mkdir -p /opt/upstreams/hyperframes && tar -xzf /opt/rundesk-upstream/source.tar.gz --strip-components=1 -C /opt/upstreams/hyperframes
RUN npm_config_registry="${RD_NPM_REGISTRY}" PIP_INDEX_URL="${RD_PIP_INDEX_URL}" sh /opt/rundesk-upstream/install.sh
RUN test -f /opt/upstreams/hyperframes/LICENSE
RUN test -f /opt/upstreams/hyperframes/README.md
RUN test -f /opt/upstreams/hyperframes/package.json
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes/SKILL.md
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-core/SKILL.md
RUN test -f /opt/upstreams/hyperframes/skills/media-use/SKILL.md
COPY skills/ /etc/codex/skills/
COPY tools/ /opt/rundesk-profile/tools/
RUN test -f /opt/upstreams/hyperframes/skills/embedded-captions/SKILL.md && test ! -e /etc/codex/skills/embedded-captions && ln -s /opt/upstreams/hyperframes/skills/embedded-captions /etc/codex/skills/embedded-captions
RUN test -f /opt/upstreams/hyperframes/skills/faceless-explainer/SKILL.md && test ! -e /etc/codex/skills/faceless-explainer && ln -s /opt/upstreams/hyperframes/skills/faceless-explainer /etc/codex/skills/faceless-explainer
RUN test -f /opt/upstreams/hyperframes/skills/figma/SKILL.md && test ! -e /etc/codex/skills/figma && ln -s /opt/upstreams/hyperframes/skills/figma /etc/codex/skills/figma
RUN test -f /opt/upstreams/hyperframes/skills/general-video/SKILL.md && test ! -e /etc/codex/skills/general-video && ln -s /opt/upstreams/hyperframes/skills/general-video /etc/codex/skills/general-video
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes/SKILL.md && test ! -e /etc/codex/skills/hyperframes && ln -s /opt/upstreams/hyperframes/skills/hyperframes /etc/codex/skills/hyperframes
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-animation/SKILL.md && test ! -e /etc/codex/skills/hyperframes-animation && ln -s /opt/upstreams/hyperframes/skills/hyperframes-animation /etc/codex/skills/hyperframes-animation
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-audio/SKILL.md && test ! -e /etc/codex/skills/hyperframes-audio && ln -s /opt/upstreams/hyperframes/skills/hyperframes-audio /etc/codex/skills/hyperframes-audio
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-cli/SKILL.md && test ! -e /etc/codex/skills/hyperframes-cli && ln -s /opt/upstreams/hyperframes/skills/hyperframes-cli /etc/codex/skills/hyperframes-cli
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-core/SKILL.md && test ! -e /etc/codex/skills/hyperframes-core && ln -s /opt/upstreams/hyperframes/skills/hyperframes-core /etc/codex/skills/hyperframes-core
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-creative/SKILL.md && test ! -e /etc/codex/skills/hyperframes-creative && ln -s /opt/upstreams/hyperframes/skills/hyperframes-creative /etc/codex/skills/hyperframes-creative
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-keyframes/SKILL.md && test ! -e /etc/codex/skills/hyperframes-keyframes && ln -s /opt/upstreams/hyperframes/skills/hyperframes-keyframes /etc/codex/skills/hyperframes-keyframes
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-registry/SKILL.md && test ! -e /etc/codex/skills/hyperframes-registry && ln -s /opt/upstreams/hyperframes/skills/hyperframes-registry /etc/codex/skills/hyperframes-registry
RUN test -f /opt/upstreams/hyperframes/skills/hyperframes-studio/SKILL.md && test ! -e /etc/codex/skills/hyperframes-studio && ln -s /opt/upstreams/hyperframes/skills/hyperframes-studio /etc/codex/skills/hyperframes-studio
RUN test -f /opt/upstreams/hyperframes/skills/media-use/SKILL.md && test ! -e /etc/codex/skills/media-use && ln -s /opt/upstreams/hyperframes/skills/media-use /etc/codex/skills/media-use
RUN test -f /opt/upstreams/hyperframes/skills/motion-graphics/SKILL.md && test ! -e /etc/codex/skills/motion-graphics && ln -s /opt/upstreams/hyperframes/skills/motion-graphics /etc/codex/skills/motion-graphics
RUN test -f /opt/upstreams/hyperframes/skills/music-to-video/SKILL.md && test ! -e /etc/codex/skills/music-to-video && ln -s /opt/upstreams/hyperframes/skills/music-to-video /etc/codex/skills/music-to-video
RUN test -f /opt/upstreams/hyperframes/skills/pr-to-video/SKILL.md && test ! -e /etc/codex/skills/pr-to-video && ln -s /opt/upstreams/hyperframes/skills/pr-to-video /etc/codex/skills/pr-to-video
RUN test -f /opt/upstreams/hyperframes/skills/product-launch-video/SKILL.md && test ! -e /etc/codex/skills/product-launch-video && ln -s /opt/upstreams/hyperframes/skills/product-launch-video /etc/codex/skills/product-launch-video
RUN test -f /opt/upstreams/hyperframes/skills/remotion-to-hyperframes/SKILL.md && test ! -e /etc/codex/skills/remotion-to-hyperframes && ln -s /opt/upstreams/hyperframes/skills/remotion-to-hyperframes /etc/codex/skills/remotion-to-hyperframes
RUN test -f /opt/upstreams/hyperframes/skills/slideshow/SKILL.md && test ! -e /etc/codex/skills/slideshow && ln -s /opt/upstreams/hyperframes/skills/slideshow /etc/codex/skills/slideshow
RUN test -f /opt/upstreams/hyperframes/skills/talking-head-recut/SKILL.md && test ! -e /etc/codex/skills/talking-head-recut && ln -s /opt/upstreams/hyperframes/skills/talking-head-recut /etc/codex/skills/talking-head-recut
ARG CODEX_VERSION=0.0.0
RUN npm_config_registry="${RD_NPM_REGISTRY}" npm install --global "@openai/codex@${CODEX_VERSION}" && npm cache clean --force && codex --version && codex app-server --help >/dev/null
LABEL org.opencontainers.image.title="RunDesk hyperframes"
LABEL org.opencontainers.image.version="1.0.0"
LABEL io.rundesk.profile="hyperframes"
LABEL io.rundesk.profile.schema="1"
LABEL io.rundesk.codex.version="0.0.0"
LABEL io.rundesk.agent-reach="false"
LABEL io.rundesk.upstream.repository="https://github.com/heygen-com/hyperframes"
LABEL io.rundesk.upstream.commit="82ae73a03156f782e48d631907db08232aeed817"
LABEL io.rundesk.upstream.source.sha256="492a80be9507b50abae44b6f10df4f5107feeab71d1dccfacd81c7f371c87fc2"
LABEL io.rundesk.video-app-media.contract="1"
LABEL io.rundesk.capabilities.skills="rundesk-hyperframes, embedded-captions, faceless-explainer, figma, general-video, hyperframes, hyperframes-animation, hyperframes-audio, hyperframes-cli, hyperframes-core, hyperframes-creative, hyperframes-keyframes, hyperframes-registry, hyperframes-studio, media-use, motion-graphics, music-to-video, pr-to-video, product-launch-video, remotion-to-hyperframes, slideshow, talking-head-recut"
LABEL io.rundesk.capabilities.tools="Node.js 22+, HyperFrames 0.8.87, GSAP 3.15.0, Chromium, FFmpeg, ffprobe, Noto CJK"
LABEL io.rundesk.capabilities.mcp="video_app_media"
COPY seed/ /opt/rundesk-seed/
COPY profile.lock.json /opt/rundesk-profile/profile.lock.json
COPY THIRD_PARTY.md /opt/rundesk-profile/THIRD_PARTY.md
RUN mkdir -p /opt/rundesk/bin && chmod -R a+rX /opt/upstreams /opt/rundesk-upstream /opt/rundesk-profile /etc/codex/skills /opt/rundesk-seed
USER node
WORKDIR /home/node
CMD ["codex", "app-server"]
