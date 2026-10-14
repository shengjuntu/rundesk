#!/usr/bin/env bash
# The App remains independently versioned; pass the unpacked v0.7.0 directory.
set -euo pipefail
app_dir="${VIDEO_APP_DIR:?请设置 VIDEO_APP_DIR 为 rundesk-video-app-v0.7.0 的绝对路径}"
[[ -f "$app_dir/scripts/docker.sh" ]] || { echo '未找到 Video App Docker 入口'; exit 1; }
exec bash "$app_dir/scripts/docker.sh" "${1:-help}"
