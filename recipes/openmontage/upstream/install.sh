#!/bin/sh
set -eu
src=/opt/upstreams/openmontage
overlay=/opt/rundesk-upstream
test -f "$src/LICENSE"
test -f "$src/AGENT_GUIDE.md"
python3 -m pip install --retries 3 --timeout 60 --no-cache-dir -r "$src/requirements.txt"
python3 -m pip check
npm ci --prefix "$src/remotion-composer" --no-audit --no-fund
cp "$overlay/overlays/rundesk_video_app.py" "$src/lib/rundesk_video_app.py"
cp "$overlay/overlays/video_app_tts.py" "$src/tools/audio/video_app_tts.py"
cp "$overlay/overlays/video_app_image.py" "$src/tools/graphics/video_app_image.py"
cp "$overlay/overlays/video_app_video.py" "$src/tools/video/video_app_video.py"
cp "$overlay/LICENSE-AGPL-3.0.txt" "$src/RUNDESK-INTEGRATION-LICENSE.txt"
python3 "$overlay/patch_runtime.py" "$src"
install -m 755 "$overlay/openmontage-python" /usr/local/bin/openmontage-python
# Retain both native composition choices without fetching tools during a run.
mkdir -p /opt/hyperframes
cp "$overlay/npm/package.json" "$overlay/npm/package-lock.json" /opt/hyperframes/
npm ci --prefix /opt/hyperframes --omit=dev --no-audit --no-fund
install -m 755 "$overlay/hyperframes" /usr/local/bin/hyperframes
npm cache clean --force
# The launch wrapper copies the installed source and modules into the persistent
# workspace before native tools write Remotion staging/public/cache files.
chmod -R a+rX "$src" /opt/hyperframes
hyperframes --version
chromium --version
