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

# Both Debian's deb822 (.sources) and traditional (.list) formats contain URIs.
# Replace only the reviewed Debian archive origins, leaving Signed-By and all
# non-Debian repositories untouched. The source list is never made trusted=yes.
found=0
for file in "$root"/etc/apt/sources.list "$root"/etc/apt/sources.list.d/*.list "$root"/etc/apt/sources.list.d/*.sources; do
    [ -f "$file" ] || continue
    if grep -Eq 'https?://(deb\.debian\.org/debian|security\.debian\.org/debian-security)' "$file"; then
        found=1
    fi
    if [ -n "$security" ]; then
        replacement=$(escape_replacement "$security")
        sed -i -E "s#https?://deb\.debian\.org/debian-security/?#$replacement#g; s#https?://security\.debian\.org/debian-security/?#$replacement#g" "$file"
    fi
    if [ -n "$main" ]; then
        replacement=$(escape_replacement "$main")
        # The delimiter after /debian prevents changing /debian-security.
        sed -i -E "s#https?://deb\.debian\.org/debian([[:space:]]|$)#$replacement\\1#g; s#https?://deb\.debian\.org/debian/([[:space:]]|$)#$replacement\\1#g" "$file"
    fi
done
if [ "$found" -eq 0 ]; then
    echo 'No standard Debian archive source was found; edit this recipe source file rather than silently replacing an unknown repository.' >&2
    exit 64
fi
