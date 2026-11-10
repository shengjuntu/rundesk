# Editable common RunDesk base layer. No Codex, Skills or business dependencies.
ARG RD_BASE_IMAGE=node:22-bookworm-slim
FROM ${RD_BASE_IMAGE}
USER root
ARG RD_APT_MIRROR=http://deb.debian.org/debian
ARG RD_APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
COPY bases/configure-apt.sh /opt/rundesk-bases/configure-apt.sh
COPY bases/install.sh /opt/rundesk-bases/install.sh
RUN RD_APT_MIRROR="${RD_APT_MIRROR}" RD_APT_SECURITY_MIRROR="${RD_APT_SECURITY_MIRROR}" sh /opt/rundesk-bases/install.sh media
ENV PATH="/opt/rundesk-python/bin:${PATH}" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
LABEL io.rundesk.base.layer="media"
