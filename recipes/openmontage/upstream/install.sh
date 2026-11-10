#!/bin/sh
set -eu
src=/opt/upstreams/openmontage
overlay=/opt/rundesk-upstream
test -f "$src/LICENSE"
test -f "$src/AGENT_GUIDE.md"
# Ordinary/manual execution still prepares dependencies. The Dockerfile prepares
# them in an earlier layer so overlay-only edits keep that expensive cache.
if [ "${RD_OPENMONTAGE_DEPS_READY:-0}" != "1" ]; then
    sh "$overlay/install-dependencies.sh"
fi
cp "$overlay/overlays/rundesk_video_app.py" "$src/lib/rundesk_video_app.py"
cp "$overlay/overlays/video_app_tts.py" "$src/tools/audio/video_app_tts.py"
cp "$overlay/overlays/video_app_image.py" "$src/tools/graphics/video_app_image.py"
cp "$overlay/overlays/video_app_video.py" "$src/tools/video/video_app_video.py"
cp "$overlay/LICENSE-AGPL-3.0.txt" "$src/RUNDESK-INTEGRATION-LICENSE.txt"
python3 "$overlay/patch_runtime.py" "$src"
install -m 755 "$overlay/openmontage-python" /usr/local/bin/openmontage-python
install -m 755 "$overlay/hyperframes" /usr/local/bin/hyperframes
# The launch wrapper copies the installed source and modules into the persistent
# workspace before native tools write Remotion staging/public/cache files.
chmod -R a+rX "$src" /opt/hyperframes
hyperframes --version
chromium --version
