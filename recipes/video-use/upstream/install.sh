#!/bin/sh
set -eu
# Build runs in the compiler's /opt/rundesk-python virtual environment.
python3 -m pip install --retries 3 --timeout 60 --no-cache-dir --no-deps -r /opt/rundesk-upstream/requirements.lock
python3 -m pip check
test -f /opt/upstreams/video-use/LICENSE
test -f /opt/upstreams/video-use/helpers/render.py
# Keep the upstream project and its native Skill intact; do not run install.md
# (interactive machine setup, credential requests and mutable git pulls).
python3 /opt/upstreams/video-use/helpers/render.py --help >/dev/null
python3 /opt/upstreams/video-use/helpers/pack_transcripts.py --help >/dev/null
chmod -R a+rX /opt/upstreams/video-use
