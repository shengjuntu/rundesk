# Editable RunDesk Dockerfile. Keep profile.lock.json and source settings with this recipe.
ARG RD_BASE_IMAGE=node:22-bookworm-slim
FROM ${RD_BASE_IMAGE}
ARG RD_APT_MIRROR=http://deb.debian.org/debian
ARG RD_APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
COPY tools/configure-apt.sh /opt/rundesk-profile/tools/configure-apt.sh
RUN RD_APT_MIRROR="${RD_APT_MIRROR}" RD_APT_SECURITY_MIRROR="${RD_APT_SECURITY_MIRROR}" sh /opt/rundesk-profile/tools/configure-apt.sh && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 update && apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 install -y --no-install-recommends ca-certificates curl ffmpeg fontconfig fonts-noto-cjk git python3 python3-venv ripgrep gh && rm -rf /var/lib/apt/lists/*
ARG RD_NPM_REGISTRY=https://registry.npmjs.org
ARG RD_PIP_INDEX_URL=https://pypi.org/simple
ENV npm_config_fetch_retries=3 npm_config_fetch_timeout=120000
RUN python3 -m venv /opt/rundesk-python
ENV PATH="/opt/rundesk-python/bin:${PATH}" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
COPY requirements.txt /opt/rundesk-profile/requirements.txt
COPY vendor/ /opt/rundesk-profile/vendor/
RUN PIP_INDEX_URL="${RD_PIP_INDEX_URL}" python3 -m pip install --retries 3 --timeout 60 --no-cache-dir -r /opt/rundesk-profile/requirements.txt && python3 -m pip check
COPY agent-tools/ /opt/agent-tools/
RUN npm_config_registry="${RD_NPM_REGISTRY}" npm ci --prefix /opt/agent-tools --omit=dev --no-audit --no-fund && npm cache clean --force
ENV PATH="/opt/agent-tools/node_modules/.bin:${PATH}" NODE_PATH=/opt/agent-tools/node_modules
RUN mkdir -p /etc/codex/skills/agent-reach && python3 -c "from importlib.resources import files; from pathlib import Path; Path('/etc/codex/skills/agent-reach/SKILL.md').write_text(files('agent_reach').joinpath('skill/SKILL.md').read_text())"
RUN agent-reach --version && gh --version && bird --version && mcporter --version
ARG CODEX_VERSION=0.0.0
RUN npm_config_registry="${RD_NPM_REGISTRY}" npm install --global "@openai/codex@${CODEX_VERSION}" && npm cache clean --force && codex --version && codex app-server --help >/dev/null
LABEL org.opencontainers.image.title="RunDesk news2douyin"
LABEL org.opencontainers.image.version="1.1.2"
LABEL io.rundesk.profile="news2douyin"
LABEL io.rundesk.profile.schema="1"
LABEL io.rundesk.codex.version="0.0.0"
LABEL io.rundesk.agent-reach="true"
LABEL io.rundesk.capabilities.tools="Python 3, Node.js, Git, curl, ripgrep, feedparser, yt-dlp, FFmpeg, Pillow, Noto CJK, agent-reach"
LABEL io.rundesk.capabilities.skills="collect-news-evidence"
LABEL io.rundesk.capabilities.mcp="exa"
COPY skills/ /etc/codex/skills/
COPY seed/ /opt/rundesk-seed/
COPY tools/ /opt/rundesk-profile/tools/
COPY profile.lock.json /opt/rundesk-profile/profile.lock.json
COPY THIRD_PARTY.md /opt/rundesk-profile/THIRD_PARTY.md
RUN mkdir -p /opt/rundesk/bin && chmod -R a+rX /opt/rundesk-profile /etc/codex/skills /opt/rundesk-seed
COPY tools/mcporter /usr/local/bin/mcporter
ENV PATH="/usr/local/bin:${PATH}"
USER node
WORKDIR /home/node
CMD ["codex", "app-server"]
