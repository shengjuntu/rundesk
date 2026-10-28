#!/bin/sh
# Same pinned bytes for every network route. Never accept a changed checksum.
set -eu

[ "$#" -eq 4 ] || { echo 'Usage: download-archive.sh SHA256 DESTINATION URL ARCHIVE_PATH' >&2; exit 64; }
expected=$1
destination=$2
official=$3
archive_path=$4
printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || { echo 'Invalid archive SHA256.' >&2; exit 64; }
verify() { printf '%s  %s\n' "$expected" "$1" | sha256sum -c -; }

if [ -f "$destination" ]; then
    verify "$destination"
    echo 'Reused verified source archive.'
    exit 0
fi

url=${RD_GITHUB_ARCHIVE_URL:-}
if [ -z "$url" ]; then
    if [ -n "${RD_GITHUB_ARCHIVE_BASE:-}" ]; then
        url="${RD_GITHUB_ARCHIVE_BASE%/}$archive_path"
    else
        url=$official
    fi
fi
case "$url" in http://*|https://*) ;; *) echo 'Archive URL must use HTTP(S).' >&2; exit 64 ;; esac
case "$url" in *[[:space:]]*) echo 'Archive URL must not contain whitespace.' >&2; exit 64 ;; esac
mkdir -p "$(dirname "$destination")"
temporary=$(mktemp "$destination.part.XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM
curl --fail --location --proto '=http,https' --proto-redir '=http,https' --retry 3 --connect-timeout 30 --max-time 900 --output "$temporary" "$url"
verify "$temporary"
mv "$temporary" "$destination"
trap - EXIT HUP INT TERM
