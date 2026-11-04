#!/usr/bin/env bash
set -euo pipefail
if [[ $# -lt 1 || $# -gt 2 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'Usage: bash build.sh CODEX_VERSION [IMAGE_TAG] (exact release required)' >&2
  exit 2
fi
runtime_version="$1"
runtime_tag="${2:-rundesk-codex:$runtime_version}"
runtime_context="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
command -v docker >/dev/null || { echo 'Docker CLI is required' >&2; exit 1; }
docker info >/dev/null
docker build --build-arg "CODEX_VERSION=$runtime_version" \
  --build-arg "IMAGE_VERSION=$runtime_version" \
  --build-arg "BASE_IMAGE=${BASE_IMAGE:-node:22-bookworm-slim}" \
  -f "$runtime_context/Dockerfile" -t "$runtime_tag" "$runtime_context"
# Check the actual executable under a read-only root, without credentials/network.
# This verifies CLI startup only, not model authentication or MCP connectivity.
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --user 1000:1000 \
  --tmpfs /tmp:rw,nosuid,nodev,mode=1777 \
  -e HOME=/tmp -e CODEX_HOME=/tmp/codex \
  --entrypoint sh "$runtime_tag" \
  -c 'codex --version && codex app-server --help >/dev/null'
echo "Built and CLI startup checked: $runtime_tag"
