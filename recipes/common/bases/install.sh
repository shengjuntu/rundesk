#!/bin/sh
# Shared system packages. Called by isolated base stages and standalone recipes.
set -eu
layer=${1:?Pass system, media, browser or all-<layer>}
case "$layer" in
    system) packages='ca-certificates curl git python3 python3-venv ripgrep' ;;
    media) packages='ffmpeg fontconfig fonts-noto-cjk' ;;
    browser) packages='chromium' ;;
    all-system) packages='ca-certificates curl git python3 python3-venv ripgrep' ;;
    all-media) packages='ca-certificates curl git python3 python3-venv ripgrep ffmpeg fontconfig fonts-noto-cjk' ;;
    all-browser) packages='ca-certificates curl git python3 python3-venv ripgrep ffmpeg fontconfig fonts-noto-cjk chromium' ;;
    *) echo "Unknown RunDesk base layer: $layer" >&2; exit 64 ;;
esac
node -e 'if (Number(process.versions.node.split(".")[0]) < 22) process.exit(1)'
npm --version >/dev/null
id node >/dev/null
sh /opt/rundesk-bases/configure-apt.sh
apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 update
# Package list is a reviewed constant above; shell splitting is intentional.
apt-get -o Acquire::Retries=3 -o Acquire::http::Timeout=60 install -y --no-install-recommends $packages
rm -rf /var/lib/apt/lists/*
case "$layer" in
    system|all-*)
        # Preserve a venv inherited from an explicit Codex base image.
        if [ ! -x /opt/rundesk-python/bin/python3 ]; then
            python3 -m venv /opt/rundesk-python
        fi
        /opt/rundesk-python/bin/python3 -m pip --version >/dev/null
        ;;
esac
