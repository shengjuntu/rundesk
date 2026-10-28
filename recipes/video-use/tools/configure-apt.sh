#!/bin/sh
# Build-time Debian mirror configuration. Keep suites, components and keyrings.
set -eu

root=${1:-}
main=${RD_APT_MIRROR:-}
security=${RD_APT_SECURITY_MIRROR:-}
[ -n "$main$security" ] || exit 0

if [ ! -f "$root/etc/os-release" ]; then
    echo 'APT source settings require a Debian base with /etc/os-release.' >&2
    exit 64
fi
. "$root/etc/os-release"
if [ "${ID:-}" != debian ]; then
    echo 'This recipe supports APT source overrides only for Debian bases; edit the external Dockerfile for another distribution.' >&2
    exit 64
fi

case "$main $security" in
    *https://*)
        if [ ! -s "$root/etc/ssl/certs/ca-certificates.crt" ]; then
            echo 'HTTPS APT source requires CA certificates in the base image. Use a base image with ca-certificates, or bootstrap it through a reachable signed HTTP Debian source; TLS verification is never disabled.' >&2
            exit 65
        fi
        ;;
esac

escape_replacement() {
    case "$1" in
        http://*|https://*) ;;
        *) echo 'APT source must be an HTTP or HTTPS URL.' >&2; exit 64 ;;
    esac
    case "$1" in
        *[[:space:]]*) echo 'APT source URL must not contain whitespace.' >&2; exit 64 ;;
    esac
    printf '%s' "${1%/}" | sed 's/[\\&|#]/\\&/g'
}

# Remember only origins RunDesk itself selected. This lets a derived runtime
# change network groups without rewriting unrelated third-party repositories.
state="$root/var/lib/rundesk/apt-sources"
previous_main=
previous_security=
if [ -f "$state" ]; then
    previous_main=$(sed -n '1p' "$state")
    previous_security=$(sed -n '2p' "$state")
fi
for value in "$main" "$security" "$previous_main" "$previous_security"; do
    [ -z "$value" ] || escape_replacement "$value" >/dev/null
done

# Only complete URI tokens are replaced; /debian must never match
# /debian-security, and comments, suites, components and Signed-By are retained.
found=0
replace_origin() {
    origin=$1
    replacement=$2
    [ -n "$origin" ] || return 0
    pattern=$(printf '%s' "${origin%/}" | sed 's/[][\\.^$*+?(){}|#]/\\&/g')
    if grep -Ev '^[[:space:]]*(#|$)' "$file" | grep -Eq "$pattern/?([[:space:]]|$)"; then
        found=1
        if [ -n "$replacement" ]; then
            escaped=$(escape_replacement "$replacement")
            sed -i -E "/^[[:space:]]*#/! s#$pattern/?([[:space:]]|\$)#$escaped\\1#g" "$file"
        fi
    fi
}
for file in "$root"/etc/apt/sources.list "$root"/etc/apt/sources.list.d/*.list "$root"/etc/apt/sources.list.d/*.sources; do
    [ -f "$file" ] || continue
    for scheme in http https; do
        replace_origin "$scheme://deb.debian.org/debian-security" "$security"
        replace_origin "$scheme://security.debian.org/debian-security" "$security"
        replace_origin "$scheme://mirrors.aliyun.com/debian-security" "$security"
        replace_origin "$scheme://mirrors.tuna.tsinghua.edu.cn/debian-security" "$security"
        replace_origin "$scheme://deb.debian.org/debian" "$main"
        replace_origin "$scheme://mirrors.aliyun.com/debian" "$main"
        replace_origin "$scheme://mirrors.tuna.tsinghua.edu.cn/debian" "$main"
    done
    replace_origin "$previous_security" "$security"
    replace_origin "$previous_main" "$main"
    # A base already using the selected source is a successful no-op.
    replace_origin "$security" "$security"
    replace_origin "$main" "$main"
done
if [ "$found" -eq 0 ]; then
    echo 'No recognized Debian archive source was found; edit this recipe source file rather than silently replacing an unknown repository.' >&2
    exit 64
fi
mkdir -p "$(dirname "$state")"
temporary=$(mktemp "$state.XXXXXX")
printf '%s\n%s\n' "${main:-$previous_main}" "${security:-$previous_security}" > "$temporary"
mv "$temporary" "$state"
