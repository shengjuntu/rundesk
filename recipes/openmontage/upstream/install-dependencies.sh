#!/bin/sh
# Dependency layer is independent of integration overlays, Skills and provenance.
set -eu
src=/opt/upstreams/openmontage
overlay=/opt/rundesk-upstream
test -f "$src/LICENSE"
python3 -m pip install --retries 3 --timeout 60 --no-cache-dir -r "$src/requirements.txt"
python3 -m pip check
npm ci --prefix "$src/remotion-composer" --no-audit --no-fund
# Retain both native composition choices without fetching tools during a run.
mkdir -p /opt/hyperframes
cp "$overlay/npm/package.json" "$overlay/npm/package-lock.json" /opt/hyperframes/
npm ci --prefix /opt/hyperframes --omit=dev --no-audit --no-fund
npm cache clean --force
